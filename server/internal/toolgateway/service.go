package toolgateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrDenied        = errors.New("tool invocation denied")
	ErrInvalidTicket = errors.New("invalid or expired tool ticket")
)

type ToolgatewayService struct {
	db           *pgxpool.Pool
	box          *secretbox.Box
	signingKey   []byte
	allowPrivate bool
	httpClient   *http.Client
}

type ticketClaims struct {
	TenantID string `json:"tenantId"`
	UserID   string `json:"userId"`
	RunID    string `json:"runId"`
	ToolCode string `json:"toolCode"`
	Nonce    string `json:"nonce"`
	Expires  int64  `json:"expires"`
}

// NewToolgatewayService 创建工具网关服务；allowPrivate 仅应在开发或可信内网环境开启。
func NewToolgatewayService(db *pgxpool.Pool, box *secretbox.Box, signingSecret string, allowPrivate bool) *ToolgatewayService {
	return &ToolgatewayService{db: db, box: box, signingKey: []byte(signingSecret), allowPrivate: allowPrivate, httpClient: &http.Client{Timeout: 2 * time.Minute}}
}

// CreateTool 创建工具目录项和首个不可变版本，并加密保存调用凭据。
func (s *ToolgatewayService) CreateTool(ctx context.Context, input CreateToolInput) (Tool, error) {
	// 入库前校验端点以阻止明显的 SSRF 目标。DNS 和网络路由可能在配置后变化，
	// 因此真正调用时还会再次校验。
	if err := validateEndpoint(ctx, input.Endpoint, s.allowPrivate); err != nil {
		return Tool{}, err
	}
	method := input.Method
	if method == "" {
		method = http.MethodPost
	}
	if input.TimeoutMS == 0 {
		input.TimeoutMS = 10000
	}
	if input.MaxResponseBytes == 0 {
		input.MaxResponseBytes = 1 << 20
	}
	toolID, _ := ids.New()
	versionID, _ := ids.New()
	var encrypted []byte
	var err error
	if input.Credential != "" {
		// 凭据属于服务端秘密，不写入票据，也不通过列表接口返回；只有网关在调用前临时解密。
		encrypted, err = s.box.Seal(strings.TrimSpace(input.Credential))
		if err != nil {
			return Tool{}, err
		}
	}
	inputSchema, _ := json.Marshal(input.InputSchema)
	outputSchema, _ := json.Marshal(input.OutputSchema)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Tool{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item := Tool{ID: toolID, Code: strings.ToLower(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Protocol: input.Protocol, Status: "active", Version: 1, Endpoint: strings.TrimSpace(input.Endpoint), CreatedAt: time.Now().UTC()}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tool_catalog (id,code,name,description,protocol,status) VALUES ($1,$2,$3,$4,$5,'active')
	`, item.ID, item.Code, item.Name, item.Description, item.Protocol); err != nil {
		return Tool{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tool_versions (id,tool_id,version,endpoint,method,encrypted_credentials,input_schema,output_schema,timeout_ms,max_response_bytes,status)
		VALUES ($1,$2,1,$3,$4,$5,$6,$7,$8,$9,'active')
	`, versionID, toolID, item.Endpoint, method, encrypted, inputSchema, outputSchema, input.TimeoutMS, input.MaxResponseBytes); err != nil {
		return Tool{}, err
	}
	return item, tx.Commit(ctx)
}

// ListTools 返回工具目录及各工具最新的有效版本，不返回任何凭据。
func (s *ToolgatewayService) ListTools(ctx context.Context) ([]Tool, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id::text,t.code,t.name,t.description,t.protocol,t.status,v.version,v.endpoint,t.created_at
		FROM tool_catalog t JOIN LATERAL (
			SELECT * FROM tool_versions candidate WHERE candidate.tool_id=t.id AND candidate.status='active'
			ORDER BY candidate.version DESC LIMIT 1
		) v ON TRUE ORDER BY t.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Tool, 0)
	for rows.Next() {
		var item Tool
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Protocol, &item.Status, &item.Version, &item.Endpoint, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Grant 创建或更新租户对指定工具的授权、智能体版本白名单和单次运行调用上限。
func (s *ToolgatewayService) Grant(ctx context.Context, toolID string, input GrantInput) error {
	grantID, _ := ids.New()
	maxCalls := input.MaxCallsPerRun
	if maxCalls == 0 {
		maxCalls = 10
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO tenant_tool_grants (id,tenant_id,tool_id,allowed_agent_version_ids,max_calls_per_run,status)
		SELECT $1,$2,t.id,$4,$5,'active' FROM tool_catalog t WHERE t.id=$3 AND t.status='active'
		ON CONFLICT (tenant_id,tool_id) DO UPDATE SET allowed_agent_version_ids=EXCLUDED.allowed_agent_version_ids,
		max_calls_per_run=EXCLUDED.max_calls_per_run,status='active',updated_at=NOW()
	`, grantID, input.TenantID, toolID, input.AllowedAgentVersionIDs, maxCalls)
	return err
}

// IssueTicket 为一次有效运行签发短期、不可篡改的工具调用票据。
func (s *ToolgatewayService) IssueTicket(ctx context.Context, tenantID, userID, runID, toolCode string) (string, time.Time, error) {
	// 授权绑定到具体运行和智能体版本。短有效期降低重放风险，但授权或运行状态可能在签发后变化，
	// 因此 Invoke 仍会依据数据库重新鉴权。
	var allowed bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM ai_runs r
			JOIN tool_catalog t ON t.code=$4 AND t.status='active'
			JOIN tenant_tool_grants g ON g.tenant_id=r.tenant_id AND g.tool_id=t.id AND g.status='active'
			WHERE r.id=$1 AND r.tenant_id=$2 AND r.user_id=$3
			  AND r.status IN ('queued','dispatching','running')
			  AND (cardinality(g.allowed_agent_version_ids)=0 OR r.agent_version_id=ANY(g.allowed_agent_version_ids))
		)
	`, runID, tenantID, userID, strings.ToLower(strings.TrimSpace(toolCode))).Scan(&allowed)
	if err != nil || !allowed {
		return "", time.Time{}, ErrDenied
	}
	nonce, _ := ids.New()
	expires := time.Now().UTC().Add(5 * time.Minute)
	claims := ticketClaims{TenantID: tenantID, UserID: userID, RunID: runID, ToolCode: strings.ToLower(strings.TrimSpace(toolCode)), Nonce: nonce, Expires: expires.Unix()}
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(encoded))
	signature := hex.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, expires, nil
}

// Invoke 校验票据和实时授权后调用 HTTP/MCP 工具，并记录完整调用审计。
func (s *ToolgatewayService) Invoke(ctx context.Context, ticket string, arguments map[string]any) (map[string]any, error) {
	// 租户、用户、运行和工具身份只能来自签名票据；调用方只提供业务参数，
	// 无法把调用切换到其他租户或记到其他运行名下。
	claims, err := s.parseTicket(ticket)
	if err != nil {
		return nil, err
	}
	var versionID, protocol, endpoint, method string
	var encrypted []byte
	var timeoutMS, maxBytes, maxCalls int
	// 签名有效只是必要条件，调用时还要重新校验运行状态、租户授权、智能体白名单和最新有效工具版本。
	err = s.db.QueryRow(ctx, `
		SELECT v.id::text,t.protocol,v.endpoint,v.method,v.encrypted_credentials,v.timeout_ms,v.max_response_bytes,g.max_calls_per_run
		FROM ai_runs r
		JOIN tool_catalog t ON t.code=$4 AND t.status='active'
		JOIN tenant_tool_grants g ON g.tenant_id=r.tenant_id AND g.tool_id=t.id AND g.status='active'
		JOIN LATERAL (
			SELECT * FROM tool_versions candidate WHERE candidate.tool_id=t.id AND candidate.status='active'
			ORDER BY candidate.version DESC LIMIT 1
		) v ON TRUE
		WHERE r.id=$1 AND r.tenant_id=$2 AND r.user_id=$3 AND r.status IN ('dispatching','running')
		  AND (cardinality(g.allowed_agent_version_ids)=0 OR r.agent_version_id=ANY(g.allowed_agent_version_ids))
	`, claims.RunID, claims.TenantID, claims.UserID, claims.ToolCode).Scan(&versionID, &protocol, &endpoint, &method, &encrypted, &timeoutMS, &maxBytes, &maxCalls)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDenied
	}
	if err != nil {
		return nil, err
	}
	// 失败调用也计入上限，否则异常或恶意工具可通过持续失败绕过 maxCallsPerRun。
	// 当前限制适用于顺序调用；若需要严格限制并发调用，应改为原子计数器或事务级咨询锁。
	var callCount int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM tool_invocations WHERE run_id=$1 AND tool_version_id=$2 AND status<>'denied'`, claims.RunID, versionID).Scan(&callCount); err != nil {
		return nil, err
	}
	if callCount >= maxCalls {
		return nil, ErrDenied
	}
	if err := validateEndpoint(ctx, endpoint, s.allowPrivate); err != nil {
		return nil, err
	}
	invocationID, _ := ids.New()
	requestPayload, _ := json.Marshal(arguments)
	if _, err := s.db.Exec(ctx, `
		INSERT INTO tool_invocations (id,tenant_id,run_id,user_id,tool_version_id,request_payload,status)
		VALUES ($1,$2,$3,$4,$5,$6,'running')
	`, invocationID, claims.TenantID, claims.RunID, claims.UserID, versionID, requestPayload); err != nil {
		return nil, err
	}
	started := time.Now()
	result, invokeErr := s.call(ctx, protocol, endpoint, method, encrypted, timeoutMS, maxBytes, claims.ToolCode, arguments)
	duration := time.Since(started).Milliseconds()
	if invokeErr != nil {
		_, _ = s.db.Exec(ctx, `UPDATE tool_invocations SET status='failed',duration_ms=$2,error_message=$3,finished_at=NOW() WHERE id=$1`, invocationID, duration, invokeErr.Error())
		return nil, invokeErr
	}
	responsePayload, _ := json.Marshal(result)
	_, err = s.db.Exec(ctx, `UPDATE tool_invocations SET status='succeeded',duration_ms=$2,response_payload=$3,finished_at=NOW() WHERE id=$1`, invocationID, duration, responsePayload)
	return result, err
}

// call 根据协议构造请求，在超时、凭据和响应体大小限制下执行实际远程调用。
func (s *ToolgatewayService) call(parent context.Context, protocol, endpoint, method string, encrypted []byte, timeoutMS, maxBytes int, toolCode string, arguments map[string]any) (map[string]any, error) {
	body := any(arguments)
	if protocol == "mcp_http" {
		// MCP HTTP 使用 JSON-RPC 封装，普通 HTTP 工具直接接收 arguments；两种协议共用安全限制。
		body = map[string]any{"jsonrpc": "2.0", "id": time.Now().UnixNano(), "method": "tools/call", "params": map[string]any{"name": toolCode, "arguments": arguments}}
	}
	encoded, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if len(encrypted) > 0 {
		credential, err := s.box.Open(encrypted)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	// 多读一个字节即可区分“刚好达到上限”和“超出上限”，无需把无限响应全部读入内存。
	limited := io.LimitReader(response.Body, int64(maxBytes)+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(responseBody) > maxBytes {
		return nil, fmt.Errorf("tool response exceeded limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("tool returned HTTP %d", response.StatusCode)
	}
	var result map[string]any
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return map[string]any{"content": string(responseBody)}, nil
	}
	return result, nil
}

// parseTicket 验证 HMAC 签名、票据有效期及必要身份字段。
func (s *ToolgatewayService) parseTicket(value string) (ticketClaims, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return ticketClaims{}, ErrInvalidTicket
	}
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(parts[0]))
	expected := hex.EncodeToString(mac.Sum(nil))
	// 使用常量时间比较，避免泄露签名前缀匹配信息。
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return ticketClaims{}, ErrInvalidTicket
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ticketClaims{}, ErrInvalidTicket
	}
	var claims ticketClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Expires < time.Now().Unix() || claims.TenantID == "" || claims.RunID == "" {
		return ticketClaims{}, ErrInvalidTicket
	}
	return claims, nil
}

// validateEndpoint 校验工具地址协议并在生产模式阻止内网、回环和链路本地目标。
func validateEndpoint(ctx context.Context, rawURL string, allowPrivate bool) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("invalid tool endpoint")
	}
	if allowPrivate {
		// 开发环境和自托管 MCP 可能需要私网地址；生产环境创建服务时必须关闭此开关。
		return nil
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("production tool endpoint must use HTTPS")
	}
	// 解析域名的全部地址，只要有一个地址指向私网就拒绝；调用时重新解析可缩小 DNS 重绑定攻击窗口。
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve tool endpoint: %w", err)
	}
	for _, address := range addresses {
		if address.IP.IsPrivate() || address.IP.IsLoopback() || address.IP.IsLinkLocalUnicast() || address.IP.IsUnspecified() {
			return fmt.Errorf("private tool endpoint is not allowed")
		}
	}
	return nil
}

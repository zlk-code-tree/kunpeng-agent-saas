package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"gin-dify-saas/server/internal/platform/ids"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OperationsService struct{ db *pgxpool.Pool }

// NewOperationsService 创建租户数据运维服务。
func NewOperationsService(db *pgxpool.Pool) *OperationsService { return &OperationsService{db: db} }

// ExportTenant 在一致性快照中导出经过白名单筛选的租户业务数据并生成校验和。
func (s *OperationsService) ExportTenant(ctx context.Context, tenantID, actorUserID string) (ExportResult, error) {
	// REPEATABLE READ 使各数据分区来自同一租户快照，避免导出中途新增会话导致会话与消息不一致；
	// 只读事务还能防止导出查询意外修改数据。
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ExportResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	manifest := map[string]any{"formatVersion": 1, "tenantId": tenantID}
	// 只导出明确批准的业务数据。密码哈希、API Key、集成凭据和邀请令牌等秘密均被排除；
	// 以后在此增加表必须先评估是否会造成敏感数据披露。
	queries := []struct {
		key string
		sql string
	}{
		{"tenant", `SELECT to_jsonb(t)-'updated_at' FROM tenants t WHERE id=$1`},
		{"members", `SELECT COALESCE(jsonb_agg(jsonb_build_object('username',u.username,'displayName',u.display_name,'role',m.role,'status',m.status,'joinedAt',m.created_at) ORDER BY m.created_at),'[]') FROM tenant_members m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1`},
		{"agentConfigs", `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.created_at),'[]') FROM tenant_agent_configs c WHERE c.tenant_id=$1`},
		{"knowledgeBases", `SELECT COALESCE(jsonb_agg(to_jsonb(k) ORDER BY k.created_at),'[]') FROM knowledge_bases k WHERE k.tenant_id=$1`},
		{"files", `SELECT COALESCE(jsonb_agg(to_jsonb(f) ORDER BY f.created_at),'[]') FROM files f WHERE f.tenant_id=$1`},
		{"documents", `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY d.created_at),'[]') FROM documents d WHERE d.tenant_id=$1`},
		{"conversations", `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.created_at),'[]') FROM conversations c WHERE c.tenant_id=$1`},
		{"messages", `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY m.created_at),'[]') FROM messages m WHERE m.tenant_id=$1`},
		{"runs", `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.created_at),'[]') FROM ai_runs r WHERE r.tenant_id=$1`},
		{"usage", `SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY u.created_at),'[]') FROM usage_records u WHERE u.tenant_id=$1`},
		{"walletLedger", `SELECT COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.created_at),'[]') FROM wallet_ledger l WHERE l.tenant_id=$1`},
	}
	for _, query := range queries {
		var raw []byte
		if err := tx.QueryRow(ctx, query.sql, tenantID).Scan(&raw); err != nil {
			return ExportResult{}, fmt.Errorf("export %s: %w", query.key, err)
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return ExportResult{}, err
		}
		if query.key == "tenant" && value == nil {
			return ExportResult{}, fmt.Errorf("tenant not found")
		}
		manifest[query.key] = value
	}
	exportedAt := time.Now().UTC()
	manifest["exportedAt"] = exportedAt
	// 对实际返回的 JSON 表示计算摘要，后续导入或迁移前可以验证归档是否被篡改。
	encoded, _ := json.Marshal(manifest)
	digest := sha256.Sum256(encoded)
	checksum := hex.EncodeToString(digest[:])
	if err := tx.Commit(ctx); err != nil {
		return ExportResult{}, err
	}
	// 只读快照提交后再写导出审计；审计写入失败时整个请求失败，不能返回未留痕的租户数据。
	exportID, _ := ids.New()
	report, _ := json.Marshal(map[string]any{"formatVersion": 1, "checksumSha256": checksum, "bytes": len(encoded)})
	_, err = s.db.Exec(ctx, `
		INSERT INTO tenant_exports (id,tenant_id,requested_by,status,checksum_sha256,report,created_at,finished_at)
		VALUES ($1,$2,$3,'succeeded',$4,$5,NOW(),NOW())
	`, exportID, tenantID, actorUserID, checksum, report)
	if err != nil {
		return ExportResult{}, err
	}
	return ExportResult{ExportID: exportID, Checksum: checksum, Manifest: manifest, ExportedAt: exportedAt}, nil
}

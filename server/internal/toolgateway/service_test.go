package toolgateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// signedTicket 为测试用例构造合法的 HMAC 工具票据。
func signedTicket(t *testing.T, secret string, claims ticketClaims) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + hex.EncodeToString(mac.Sum(nil))
}

// TestMCPInvocationUsesToolsCallEnvelope 验证 MCP 调用使用标准 tools/call JSON-RPC 封装。
func TestMCPInvocationUsesToolsCallEnvelope(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","result":{"content":"ok"}}`))
	}))
	defer upstream.Close()
	gateway := &ToolgatewayService{httpClient: upstream.Client()}
	result, err := gateway.call(context.Background(), "mcp_http", upstream.URL, http.MethodPost, nil, 1000, 4096, "weather", map[string]any{"city": "Shanghai"})
	if err != nil {
		t.Fatalf("MCP invocation failed: %v", err)
	}
	if received["method"] != "tools/call" {
		t.Fatalf("unexpected MCP method: %v", received["method"])
	}
	params, ok := received["params"].(map[string]any)
	if !ok || params["name"] != "weather" {
		t.Fatalf("unexpected MCP params: %#v", received["params"])
	}
	if _, ok := result["result"]; !ok {
		t.Fatalf("unexpected MCP response: %#v", result)
	}
}

// TestParseTicketRejectsTamperingAndExpiry 验证篡改和过期票据都会被拒绝。
func TestParseTicketRejectsTamperingAndExpiry(t *testing.T) {
	gateway := &ToolgatewayService{signingKey: []byte("a-signing-secret-that-is-long-enough")}
	valid := signedTicket(t, string(gateway.signingKey), ticketClaims{
		TenantID: "tenant", UserID: "user", RunID: "run", ToolCode: "weather", Expires: time.Now().Add(time.Minute).Unix(),
	})
	if _, err := gateway.parseTicket(valid); err != nil {
		t.Fatalf("valid ticket rejected: %v", err)
	}
	if _, err := gateway.parseTicket(valid + "tampered"); err == nil {
		t.Fatal("tampered ticket accepted")
	}
	expired := signedTicket(t, string(gateway.signingKey), ticketClaims{
		TenantID: "tenant", UserID: "user", RunID: "run", ToolCode: "weather", Expires: time.Now().Add(-time.Minute).Unix(),
	})
	if _, err := gateway.parseTicket(expired); err == nil {
		t.Fatal("expired ticket accepted")
	}
}

// TestValidateEndpointBlocksPrivateTargetsInProduction 验证生产模式阻止私网工具端点。
func TestValidateEndpointBlocksPrivateTargetsInProduction(t *testing.T) {
	if err := validateEndpoint(context.Background(), "http://127.0.0.1:8080/healthz", true); err != nil {
		t.Fatalf("development private endpoint rejected: %v", err)
	}
	if err := validateEndpoint(context.Background(), "https://127.0.0.1/internal", false); err == nil {
		t.Fatal("production private endpoint accepted")
	}
}

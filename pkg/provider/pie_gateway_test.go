package provider_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"mini-code/pkg/provider"
)

var _ provider.Provider = (*provider.PieGatewayProvider)(nil)

func TestNewPieGatewayProvider_ReturnsProvider(t *testing.T) {
	p := provider.NewPieGatewayProvider(provider.PieGatewayConfig{
		AppID:       "app_test123",
		AppSecret:   "sk_live_secret",
		GatewayPath: "https://pie-gateway.example.com",
		Model:       "deepseek-v3-2",
	})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Model() != "deepseek-v3-2" {
		t.Fatalf("expected model deepseek-v3-2, got %s", p.Model())
	}
}

func TestComputeHMACSignature_KnownVector(t *testing.T) {
	sig := provider.ComputeHMACSignature("POST", "/v2/extend/chat/completions", 1706745600, "a1b2c3d4e5f6", "app_xxxxx", "secret123")
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if len(sig) != 64 {
		t.Fatalf("expected 64-char hex signature, got %d chars", len(sig))
	}

	sig2 := provider.ComputeHMACSignature("POST", "/v2/extend/chat/completions", 1706745600, "a1b2c3d4e5f6", "app_xxxxx", "secret123")
	if sig != sig2 {
		t.Fatal("same inputs should produce same signature")
	}

	sig3 := provider.ComputeHMACSignature("POST", "/v2/extend/chat/completions", 1706745601, "a1b2c3d4e5f6", "app_xxxxx", "secret123")
	if sig == sig3 {
		t.Fatal("different timestamp should produce different signature")
	}
}

func TestHmacHTTPDoer_SetsAuthHeaders(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"choices": []map[string]interface{}{{"index": 0, "message": map[string]string{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := provider.NewPieGatewayProvider(provider.PieGatewayConfig{
		AppID:       "app_test_abc",
		AppSecret:   "sk_live_test_secret",
		GatewayPath: server.URL,
		Model:       "test-model",
	})

	_, err := p.Chat(t.Context(), provider.MakeTestChatRequest("test-model", "hello"))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if capturedHeaders.Get("X-App-Id") != "app_test_abc" {
		t.Fatalf("expected X-App-Id=app_test_abc, got %s", capturedHeaders.Get("X-App-Id"))
	}
	if capturedHeaders.Get("X-Timestamp") == "" {
		t.Fatal("expected non-empty X-Timestamp")
	}
	if capturedHeaders.Get("X-Nonce") == "" {
		t.Fatal("expected non-empty X-Nonce")
	}
	authHeader := capturedHeaders.Get("Authorization")
	if len(authHeader) < 12 || authHeader[:12] != "HMAC-SHA256 " {
		t.Fatalf("expected Authorization starting with 'HMAC-SHA256 ', got %s", authHeader)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if body["model"] != "test-model" {
		t.Fatalf("expected model=test-model in body, got %v", body["model"])
	}
}

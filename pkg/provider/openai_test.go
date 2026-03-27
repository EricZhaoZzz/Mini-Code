package provider_test

import (
	"testing"

	"mini-code/pkg/provider"

	"github.com/sashabaranov/go-openai"
)

// 验证 OpenAIProvider 实现了 Provider 接口（编译期检查）
var _ provider.Provider = (*provider.OpenAIProvider)(nil)

func TestNewOpenAIProvider_ReturnsProvider(t *testing.T) {
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = "http://localhost:9999/v1"
	p := provider.NewOpenAIProvider(cfg, "test-model")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

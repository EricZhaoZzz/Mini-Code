package provider

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

// Provider 抽象 LLM 调用，解耦具体 SDK
type Provider interface {
	Chat(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
	ChatStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error)
}

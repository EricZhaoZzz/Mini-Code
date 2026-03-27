package agent

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

// StreamChunkHandler 流式响应回调（保持与原有签名一致）
type StreamChunkHandler func(content string, done bool) error

// Agent 接口：接受完整消息历史，返回最终响应内容
// 消息历史由调用方（Orchestrator）管理，Agent 不持有状态
type Agent interface {
	Run(ctx context.Context, messages []openai.ChatCompletionMessage, handler StreamChunkHandler) (reply string, newMessages []openai.ChatCompletionMessage, err error)
	Name() string
	AllowedTools() []string
}

package agent

import (
	"context"
	"fmt"
	"log"
	"mini-code/pkg/memory"
	"mini-code/pkg/provider"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"

	"github.com/sashabaranov/go-openai"
)

const DefaultMaxTurns = 50

type BaseAgent struct {
	provider             provider.Provider
	model                string
	allowedTools         []string
	debugLogger          *log.Logger
	maxConcurrency       int
	verbose              bool
	maxTurns             int
	systemPromptOverride string // Phase 3: specialized agent system prompt
	agentName            string // Phase 3: agent name
	legacyMessages       []openai.ChatCompletionMessage // 兼容旧 ClawEngine 接口，管理内部消息历史
	memStore             *memory.Store // Phase 2: 记忆存储
}

func NewBaseAgent(p provider.Provider, model string, allowedTools []string) *BaseAgent {
	verbose := os.Getenv("LM_VERBOSE") != ""
	maxTurns := DefaultMaxTurns
	if v := os.Getenv("LM_MAX_TURNS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 0 {
			maxTurns = n
		}
	}
	return &BaseAgent{
		provider:       p,
		model:          model,
		allowedTools:   allowedTools,
		debugLogger:    newDebugLogger(),
		maxConcurrency: 5,
		verbose:        verbose,
		maxTurns:       maxTurns,
	}
}

func (a *BaseAgent) Name() string {
	if a.agentName != "" {
		return a.agentName
	}
	return "base"
}

func (a *BaseAgent) AllowedTools() []string { return a.allowedTools }

func (a *BaseAgent) filteredDefinitions() []openai.Tool {
	if len(a.allowedTools) == 0 {
		return tools.Definitions
	}
	allowed := make(map[string]bool, len(a.allowedTools))
	for _, t := range a.allowedTools {
		allowed[t] = true
	}
	var filtered []openai.Tool
	for _, def := range tools.Definitions {
		if allowed[def.Function.Name] {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func (a *BaseAgent) Run(ctx context.Context, messages []openai.ChatCompletionMessage, handler StreamChunkHandler) (string, []openai.ChatCompletionMessage, error) {
	// Apply system prompt override (Phase 3 specialized agents)
	if a.systemPromptOverride != "" && len(messages) > 0 && messages[0].Role == "system" {
		overrideMsgs := make([]openai.ChatCompletionMessage, len(messages))
		copy(overrideMsgs, messages)
		overrideMsgs[0].Content = a.systemPromptOverride
		messages = overrideMsgs
	}

	var newMessages []openai.ChatCompletionMessage

	for i := 0; ; i++ {
		if a.maxTurns > 0 && i >= a.maxTurns {
			return "", newMessages, fmt.Errorf("达到最大工具调用轮数 (%d)", a.maxTurns)
		}

		allMessages := append(messages, newMessages...)

		req := openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: allMessages,
			Tools:    a.filteredDefinitions(),
		}

		var reply string
		var toolCalls []openai.ToolCall
		var finishReason openai.FinishReason
		var err error

		if handler == nil {
			// Non-streaming path (for testing)
			var resp openai.ChatCompletionResponse
			resp, err = a.provider.Chat(ctx, req)
			if err != nil {
				return "", newMessages, fmt.Errorf("chat error: %w", err)
			}
			if len(resp.Choices) > 0 {
				reply = resp.Choices[0].Message.Content
				toolCalls = resp.Choices[0].Message.ToolCalls
				finishReason = resp.Choices[0].FinishReason
			}
		} else {
			// Streaming path (production)
			var stream *openai.ChatCompletionStream
			stream, err = a.provider.ChatStream(ctx, req)
			if err != nil {
				return "", newMessages, fmt.Errorf("stream error: %w", err)
			}
			reply, toolCalls, finishReason, err = a.consumeStream(ctx, stream, handler)
			if err != nil {
				return "", newMessages, err
			}
		}

		assistantMsg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: reply,
		}
		if len(toolCalls) > 0 {
			assistantMsg.ToolCalls = toolCalls
		}
		newMessages = append(newMessages, assistantMsg)

		if len(toolCalls) > 0 {
			fmt.Println()
			a.displayToolCallsStart(toolCalls)
			results := a.executeToolCallsConcurrently(ctx, toolCalls)
			a.displayToolCallsResults(results)
			ui.PrintDim("  等待响应...")

			for _, result := range results {
				newMessages = append(newMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result.resultStr,
					ToolCallID: result.toolCallID,
				})
			}
			continue
		}

		switch finishReason {
		case openai.FinishReasonStop, "":
			return reply, newMessages, nil
		case openai.FinishReasonLength:
			return reply, newMessages, fmt.Errorf("响应因达到 token 上限而被截断")
		default:
			return reply, newMessages, nil
		}
	}
}

package agent

import (
	"context"
	"fmt"
	"mini-code/pkg/memory"
	"mini-code/pkg/provider"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"

	"github.com/sashabaranov/go-openai"
)

// legacyClientAdapter wraps old chatCompletionClient as provider.Provider
// Only for keeping engine_test.go compilable — deleted at end of Phase 1
type legacyClientAdapter struct {
	client interface {
		CreateChatCompletion(context.Context, openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
		CreateChatCompletionStream(context.Context, openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error)
	}
}

func (a *legacyClientAdapter) Chat(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return a.client.CreateChatCompletion(ctx, req)
}

func (a *legacyClientAdapter) ChatStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return a.client.CreateChatCompletionStream(ctx, req)
}

var _ provider.Provider = (*legacyClientAdapter)(nil)

// ClawEngine compat alias — deleted at end of Phase 1
type ClawEngine = BaseAgent

// chatCompletionClient 旧接口（仅供兼容垫片使用）
type chatCompletionClient interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
	CreateChatCompletionStream(ctx context.Context, request openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error)
}

func NewClawEngine(client chatCompletionClient, model string) *ClawEngine {
	p := &legacyClientAdapter{client: client}
	e := NewBaseAgent(p, model, nil)
	// 初始化系统消息到内部消息历史
	e.legacyMessages = []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: buildSystemPrompt()},
	}
	return e
}

// SetMemoryStore 设置记忆存储（需要在创建后调用）
func (e *ClawEngine) SetMemoryStore(store *memory.Store) {
	e.memStore = store
	// 重新构建带记忆的系统提示
	e.refreshSystemPrompt()
}

// refreshSystemPrompt 刷新系统提示（在记忆变化后调用）
func (e *ClawEngine) refreshSystemPrompt() {
	if len(e.legacyMessages) > 0 && e.legacyMessages[0].Role == openai.ChatMessageRoleSystem {
		e.legacyMessages[0].Content = buildSystemPromptWithMemory(e.memStore)
	}
}

// buildSystemPrompt 构建系统提示（保留供兼容）
func buildSystemPrompt() string {
	return BuildSystemPrompt()
}

// buildSystemPromptWithMemory 构建带记忆的系统提示
func buildSystemPromptWithMemory(memStore *memory.Store) string {
	return BuildSystemPromptWithMemory(memStore)
}

// AddUserMessage 向内部历史追加用户消息（兼容旧接口）
func (e *ClawEngine) AddUserMessage(message string) {
	e.legacyMessages = append(e.legacyMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: message,
	})
}

// Reset 重置对话历史，保留系统消息（兼容旧接口）
func (e *ClawEngine) Reset() {
	e.legacyMessages = []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: buildSystemPromptWithMemory(e.memStore)},
	}
}

// GetMessageCount 返回当前消息数量（兼容旧接口）
func (e *ClawEngine) GetMessageCount() int {
	return len(e.legacyMessages)
}

// SetVerbose 设置详细模式（兼容旧接口）
func (e *ClawEngine) SetVerbose(verbose bool) {
	e.verbose = verbose
}

// SetMaxTurns 设置最大轮次限制（兼容旧接口）
func (e *ClawEngine) SetMaxTurns(maxTurns int) {
	e.maxTurns = maxTurns
}

// GetMaxTurns 获取当前最大轮次限制（兼容旧接口）
func (e *ClawEngine) GetMaxTurns() int {
	return e.maxTurns
}

// RunTurn 非流式执行一轮对话（兼容旧接口）
func (e *ClawEngine) RunTurn(ctx context.Context) (string, error) {
	for i := 0; ; i++ {
		if e.maxTurns > 0 && i >= e.maxTurns {
			return "", fmt.Errorf("达到最大工具调用轮数 (%d)，任务中止。可通过设置环境变量 LM_MAX_TURNS 调整限制", e.maxTurns)
		}

		req := openai.ChatCompletionRequest{
			Model:    e.model,
			Messages: e.legacyMessages,
			Tools:    tools.Definitions,
		}
		e.debugLog("LM INPUT", req)

		resp, err := e.provider.Chat(ctx, req)
		if err != nil {
			return "", fmt.Errorf("chat completion error: %w", err)
		}
		e.debugLog("LM OUTPUT", resp)

		choice := resp.Choices[0]
		msg := choice.Message
		e.legacyMessages = append(e.legacyMessages, msg)

		if len(msg.ToolCalls) > 0 {
			e.displayToolCallsStart(msg.ToolCalls)
			results := e.executeToolCallsConcurrently(ctx, msg.ToolCalls)
			e.displayToolCallsResults(results)

			for _, result := range results {
				e.legacyMessages = append(e.legacyMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result.resultStr,
					ToolCallID: result.toolCallID,
				})
			}
			continue
		}

		switch choice.FinishReason {
		case openai.FinishReasonStop, "":
			return msg.Content, nil
		case openai.FinishReasonLength:
			return msg.Content, fmt.Errorf("响应因达到 token 上限而被截断")
		case openai.FinishReasonContentFilter:
			return msg.Content, fmt.Errorf("响应被内容安全过滤器拦截")
		default:
			return msg.Content, nil
		}
	}
}

// RunTurnStream 流式执行一轮对话（兼容旧接口）
func (e *ClawEngine) RunTurnStream(ctx context.Context, handler StreamChunkHandler) (string, error) {
	for i := 0; ; i++ {
		if e.maxTurns > 0 && i >= e.maxTurns {
			return "", fmt.Errorf("达到最大工具调用轮数 (%d)，任务中止。可通过设置环境变量 LM_MAX_TURNS 调整限制", e.maxTurns)
		}

		if e.maxTurns > 0 && i >= e.maxTurns-5 && i < e.maxTurns {
			ui.PrintWarning("[警告] 剩余轮次: %d", e.maxTurns-i)
		}

		req := openai.ChatCompletionRequest{
			Model:    e.model,
			Messages: e.legacyMessages,
			Tools:    tools.Definitions,
		}
		e.debugLog("LM INPUT", req)

		stream, err := e.provider.ChatStream(ctx, req)
		if err != nil {
			return "", fmt.Errorf("chat completion stream error: %w", err)
		}

		reply, toolCalls, finishReason, err := e.consumeStream(ctx, stream, handler)
		if err != nil {
			return "", err
		}

		e.debugLog("LM OUTPUT STREAM", map[string]any{
			"content":       reply,
			"tool_calls":    len(toolCalls),
			"finish_reason": finishReason,
		})

		msg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: reply,
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		e.legacyMessages = append(e.legacyMessages, msg)

		if len(toolCalls) > 0 {
			fmt.Println()
			e.displayToolCallsStart(toolCalls)
			results := e.executeToolCallsConcurrently(ctx, toolCalls)
			e.displayToolCallsResults(results)
			ui.PrintDim("  等待响应...")

			for _, result := range results {
				e.legacyMessages = append(e.legacyMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result.resultStr,
					ToolCallID: result.toolCallID,
				})
			}
			continue
		}

		switch finishReason {
		case openai.FinishReasonStop, "":
			// 任务完成后异步更新 Session Memory（不阻塞响应）
			if e.memStore != nil && reply != "" {
				go func() {
					workspace, _ := os.Getwd()
					// 取响应前 200 字作为摘要
					summary := reply
					if len([]rune(summary)) > 200 {
						runes := []rune(summary)
						summary = string(runes[:200])
					}
					_ = e.memStore.SaveSessionMemory(workspace, summary, 72)
				}()
			}
			return msg.Content, nil
		case openai.FinishReasonLength:
			return msg.Content, fmt.Errorf("响应因达到 token 上限而被截断")
		case openai.FinishReasonContentFilter:
			return msg.Content, fmt.Errorf("响应被内容安全过滤器拦截")
		default:
			return msg.Content, nil
		}
	}
}

// RunStream 流式执行对话（兼容旧接口）
func (e *ClawEngine) RunStream(ctx context.Context, prompt string, handler StreamChunkHandler) error {
	e.AddUserMessage(prompt)

	ui.PrintAssistantLabel()
	reply, err := e.RunTurnStream(ctx, handler)
	if err != nil {
		ui.PrintError("错误: %v", err)
		return err
	}

	fmt.Printf("\n\n")
	if reply != "" && handler == nil {
		fmt.Printf("%s\n", reply)
	}
	return nil
}

// RunLegacy 非流式执行（兼容旧接口的 Run 方法）
func (e *ClawEngine) RunLegacy(ctx context.Context, prompt string) error {
	e.AddUserMessage(prompt)

	reply, err := e.RunTurn(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("\nMini-Code: %s\n\n", reply)
	return nil
}

package agent

import (
	"context"
	"fmt"
	"mini-code/pkg/provider"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"runtime"

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

// buildSystemPrompt 构建系统提示（保留供兼容）
func buildSystemPrompt() string {
	osName := runtime.GOOS
	var shellHint string
	switch osName {
	case "windows":
		shellHint = "如果必须执行 shell 命令，请使用 Windows CMD 语法。"
	default:
		shellHint = "如果必须执行 shell 命令，请使用 Unix shell 语法。"
	}

	return fmt.Sprintf(`你是一个专业的中文 AI 编程助手 Mini-Code。你的核心职责是帮助用户完成各类软件开发任务。

## 运行环境
- 操作系统: %s
- %s

## 核心工作流程

### 1. 理解项目（必须首先执行）
在开始任何代码修改之前，你必须先理解项目结构：
- 使用 list_files 浏览目录结构，了解项目组织方式
- 使用 search_in_files 搜索关键代码，定位相关模块
- 使用 read_file 阅读关键文件，理解现有实现

### 2. 分析需求
- 仔细理解用户的任务目标
- 识别需要修改的文件和范围
- 考虑对现有代码的影响

### 3. 执行修改
- 优先使用 replace_in_file 进行最小化修改，保持代码的一致性和可追溯性
- 只有在创建新文件或文件需要大规模重写时才使用 write_file
- 保持代码风格与项目现有风格一致

### 4. 验证结果
- 修改完成后，运行 go test ./... 验证代码正确性
- 如果测试失败，分析错误并修复

## 工具使用规范

### 文件操作
- 所有文件路径必须是工作区内的相对路径，禁止访问工作区外的文件
- 使用 replace_in_file 时，old 参数必须与文件中的原始文本完全匹配
- 写入文件时保持合理的缩进和格式

### Shell 命令
- 仅在必要时使用 shell 命令
- 命令应该简洁明确，避免复杂的管道操作
- 注意处理命令的输出和错误

### 搜索操作
- 使用 search_in_files 时，query 应该精确匹配目标文本
- 可以通过 path 参数限定搜索范围，提高效率

## 代码质量要求

1. **保持一致性**: 新代码应与现有代码风格保持一致
2. **最小修改原则**: 只修改必要的部分，避免不必要的重构
3. **错误处理**: 适当处理边界情况和错误
4. **代码注释**: 在复杂逻辑处添加必要的注释

## 沟通规范

1. 用中文回复用户
2. 说明你正在执行的操作和原因
3. 如果遇到问题，清晰地描述问题并提供解决建议
4. 完成任务后简要总结所做的修改`, osName, shellHint)
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
		{Role: openai.ChatMessageRoleSystem, Content: buildSystemPrompt()},
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

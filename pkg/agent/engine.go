package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// StreamChunkHandler 流式响应处理回调
// content 是累积的内容，done 表示是否完成
type StreamChunkHandler func(content string, done bool) error

type chatCompletionClient interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
	CreateChatCompletionStream(ctx context.Context, request openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error)
}

type ClawEngine struct {
	client         chatCompletionClient
	model          string
	messages       []openai.ChatCompletionMessage
	debugLogger    *log.Logger
	maxConcurrency int  // 最大并发工具调用数
	verbose        bool
	maxTurns       int  // 最大轮次限制，0 表示无限制
}

// toolCallResult 工具调用结果
type toolCallResult struct {
	index      int   // 原始顺序索引
	toolCallID string
	toolName   string
	resultStr  string
	err        error
}

// executeToolCall 执行单个工具调用
func (e *ClawEngine) executeToolCall(toolCall openai.ToolCall) (resultStr string) {
	executor := tools.Executors[toolCall.Function.Name]

	if executor == nil {
		return fmt.Sprintf("错误: 未知工具 %s", toolCall.Function.Name)
	}

	result, err := executor(toolCall.Function.Arguments)
	output, _ := result.(string)

	if err != nil {
		if output != "" {
			resultStr = fmt.Sprintf("输出: %s\n错误: %s", output, err)
		} else {
			resultStr = fmt.Sprintf("错误: %s", err)
		}
	} else {
		if output != "" {
			resultStr = output
		} else {
			resultStr = fmt.Sprintf("%v", result)
		}
	}

	return resultStr
}

// executeToolCallsConcurrently 并发执行工具调用，结果按原始顺序返回
func (e *ClawEngine) executeToolCallsConcurrently(ctx context.Context, toolCalls []openai.ToolCall) []toolCallResult {
	results := make([]toolCallResult, len(toolCalls))

	// 确定并发数
	maxConcurrency := e.maxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 5 // 默认最大并发数
	}
	if maxConcurrency > len(toolCalls) {
		maxConcurrency = len(toolCalls)
	}

	// 使用信号量控制并发
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(index int, toolCall openai.ToolCall) {
			defer wg.Done()

			// 获取信号量
			sem <- struct{}{}
			defer func() { <-sem }()

			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				results[index] = toolCallResult{
					index:      index,
					toolCallID: toolCall.ID,
					toolName:   toolCall.Function.Name,
					resultStr:  fmt.Sprintf("错误: 操作已取消 - %s", ctx.Err()),
					err:        ctx.Err(),
				}
				return
			default:
			}

			// 执行工具调用
			resultStr := e.executeToolCall(toolCall)
			results[index] = toolCallResult{
				index:      index,
				toolCallID: toolCall.ID,
				toolName:   toolCall.Function.Name,
				resultStr:  resultStr,
			}
		}(i, tc)
	}

	wg.Wait()
	return results
}

// LogLevel 日志级别
type LogLevel int

const (
	LogLevelMinimal LogLevel = iota // 最精简日志
	LogLevelNormal                  // 正常日志
	LogLevelVerbose                 // 详细日志
)

var currentLogLevel = LogLevelNormal

func init() {
	// 通过环境变量设置日志级别
	switch os.Getenv("LM_LOG_LEVEL") {
	case "minimal", "0":
		currentLogLevel = LogLevelMinimal
	case "normal", "1":
		currentLogLevel = LogLevelNormal
	case "verbose", "2":
		currentLogLevel = LogLevelVerbose
	}
}

func newDebugLogger() *log.Logger {
	if os.Getenv("LM_DEBUG") == "" {
		return nil
	}

	f, err := os.OpenFile("lm_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[debug] 无法打开 lm_debug.log: %v\n", err)
		return nil
	}

	return log.New(f, "", 0)
}

func (e *ClawEngine) debugLog(tag string, v any) {
	if e.debugLogger == nil {
		return
	}

	data, _ := json.MarshalIndent(v, "", "  ")
	e.debugLogger.Printf("[%s] %s\n%s\n", time.Now().Format("15:04:05.000"), tag, data)
}

// 默认最大轮次限制
const DefaultMaxTurns = 50

func NewClawEngine(client chatCompletionClient, model string) *ClawEngine {
	systemMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: buildSystemPrompt(),
	}

	verbose := os.Getenv("LM_VERBOSE") != "" || currentLogLevel >= LogLevelVerbose

	// 可通过环境变量 LM_MAX_TURNS 设置最大轮次
	maxTurns := DefaultMaxTurns
	if envMaxTurns := os.Getenv("LM_MAX_TURNS"); envMaxTurns != "" {
		var val int
		if _, err := fmt.Sscanf(envMaxTurns, "%d", &val); err == nil && val >= 0 {
			maxTurns = val
		}
	}

	return &ClawEngine{
		client:         client,
		model:          model,
		messages:       []openai.ChatCompletionMessage{systemMessage},
		debugLogger:    newDebugLogger(),
		maxConcurrency: 5, // 默认最大并发工具调用数
		verbose:        verbose,
		maxTurns:       maxTurns,
	}
}

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

func (e *ClawEngine) AddUserMessage(message string) {
	e.messages = append(e.messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: message,
	})
}

// Reset 重置对话历史，保留系统消息
func (e *ClawEngine) Reset() {
	systemMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: buildSystemPrompt(),
	}
	e.messages = []openai.ChatCompletionMessage{systemMessage}
}

// GetMessageCount 返回当前消息数量
func (e *ClawEngine) GetMessageCount() int {
	return len(e.messages)
}

// SetVerbose 设置详细模式
func (e *ClawEngine) SetVerbose(verbose bool) {
	e.verbose = verbose
}

// SetMaxTurns 设置最大轮次限制，0 表示无限制
func (e *ClawEngine) SetMaxTurns(maxTurns int) {
	e.maxTurns = maxTurns
}

// GetMaxTurns 获取当前最大轮次限制
func (e *ClawEngine) GetMaxTurns() int {
	return e.maxTurns
}

// printVerbose 详细模式下打印信息
func (e *ClawEngine) printVerbose(format string, args ...any) {
	if e.verbose {
		ui.PrintDim(format, args...)
	}
}

func (e *ClawEngine) RunTurn(ctx context.Context) (string, error) {
	for i := 0; ; i++ {
		// 安全兜底：防止异常情况下的无限循环
		if e.maxTurns > 0 && i >= e.maxTurns {
			return "", fmt.Errorf("达到最大工具调用轮数 (%d)，任务中止。可通过设置环境变量 LM_MAX_TURNS 调整限制", e.maxTurns)
		}
		if e.maxTurns > 0 && i >= e.maxTurns-5 && i < e.maxTurns {
			e.printVerbose("[警告] 剩余轮次: %d", e.maxTurns-i)
		}

		e.printVerbose("[turn] step=%d message_count=%d", i+1, len(e.messages))

		req := openai.ChatCompletionRequest{
			Model:    e.model,
			Messages: e.messages,
			Tools:    tools.Definitions,
		}
		e.debugLog("LM INPUT", req)

		resp, err := e.client.CreateChatCompletion(ctx, req)
		if err != nil {
			return "", fmt.Errorf("chat completion error: %w", err)
		}
		e.debugLog("LM OUTPUT", resp)

		choice := resp.Choices[0]
		msg := choice.Message
		e.printVerbose("[assistant] finish_reason=%s content=%q tool_calls=%d",
			choice.FinishReason, truncateForLog(msg.Content, 400), len(msg.ToolCalls))
		e.messages = append(e.messages, msg)

		// 如果有工具调用，执行工具并继续对话
		if len(msg.ToolCalls) > 0 {
			// 显示工具调用摘要
			e.displayToolCallsStart(msg.ToolCalls)

			// 并发执行工具调用
			results := e.executeToolCallsConcurrently(ctx, msg.ToolCalls)
			e.displayToolCallsResults(results)

			for _, result := range results {
				e.messages = append(e.messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result.resultStr,
					ToolCallID: result.toolCallID,
				})
			}
			continue
		}

		// 没有工具调用，根据 finish_reason 决定是否继续
		switch choice.FinishReason {
		case openai.FinishReasonStop, "":
			// 正常结束（部分模型 finish_reason 可能为空字符串）
			return msg.Content, nil
		case openai.FinishReasonLength:
			// 达到 token 上限，返回已有内容并提示
			return msg.Content, fmt.Errorf("响应因达到 token 上限而被截断")
		case openai.FinishReasonContentFilter:
			return msg.Content, fmt.Errorf("响应被内容安全过滤器拦截")
		default:
			// 其他情况，返回内容
			return msg.Content, nil
		}

		// 显示工具调用摘要
		e.displayToolCallsStart(msg.ToolCalls)

		// 并发执行工具调用
		results := e.executeToolCallsConcurrently(ctx, msg.ToolCalls)
		e.displayToolCallsResults(results)

		for _, result := range results {
			e.messages = append(e.messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    result.resultStr,
				ToolCallID: result.toolCallID,
			})
		}
	}
}

// RunTurnStream 流式执行一轮对话，支持流式输出
// handler 用于处理流式内容块，当工具调用发生时会先完成流式输出，执行工具后再返回
func (e *ClawEngine) RunTurnStream(ctx context.Context, handler StreamChunkHandler) (string, error) {
	for i := 0; ; i++ {
		// 检查轮次限制
		if e.maxTurns > 0 && i >= e.maxTurns {
			return "", fmt.Errorf("达到最大工具调用轮数 (%d)，任务中止。可通过设置环境变量 LM_MAX_TURNS 调整限制", e.maxTurns)
		}

		// 在接近限制时显示警告
		if e.maxTurns > 0 && i >= e.maxTurns-5 && i < e.maxTurns {
			ui.PrintWarning("[警告] 剩余轮次: %d", e.maxTurns-i)
		}

		e.printVerbose("[turn-stream] step=%d message_count=%d", i+1, len(e.messages))

		req := openai.ChatCompletionRequest{
			Model:    e.model,
			Messages: e.messages,
			Tools:    tools.Definitions,
		}
		e.debugLog("LM INPUT", req)

		stream, err := e.client.CreateChatCompletionStream(ctx, req)
		if err != nil {
			return "", fmt.Errorf("chat completion stream error: %w", err)
		}

		var contentBuilder strings.Builder
		var toolCalls []openai.ToolCall
		var toolCallsDelta = make(map[int]*strings.Builder)
		var finishReason openai.FinishReason

		for {
			response, err := stream.Recv()
			if err != nil {
				// io.EOF 表示流正常结束，不是错误
				if errors.Is(err, io.EOF) {
					stream.Close()
					break
				}
				stream.Close()
				return "", fmt.Errorf("stream recv error: %w", err)
			}

			if len(response.Choices) == 0 {
				continue
			}

			choice := response.Choices[0]
			delta := choice.Delta

			// 处理内容
			if delta.Content != "" {
				contentBuilder.WriteString(delta.Content)
				if handler != nil {
					if err := handler(delta.Content, false); err != nil {
						return "", err
					}
				}
			}

			// 处理工具调用
			for _, tc := range delta.ToolCalls {
				if tc.Index == nil {
					continue
				}
				idx := *tc.Index
				if _, exists := toolCallsDelta[idx]; !exists {
					toolCallsDelta[idx] = &strings.Builder{}
					toolCalls = append(toolCalls, openai.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: openai.FunctionCall{
							Name: tc.Function.Name,
						},
					})
				}
				if tc.Function.Arguments != "" {
					toolCallsDelta[idx].WriteString(tc.Function.Arguments)
				}
			}

			// 捕获 finish_reason 并结束流
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
				stream.Close()
				break
			}
		}

		// 组装最终的 tool calls
		for idx, builder := range toolCallsDelta {
			if idx < len(toolCalls) {
				toolCalls[idx].Function.Arguments = builder.String()
			}
		}

		// 通知流结束
		if handler != nil {
			if err := handler(contentBuilder.String(), true); err != nil {
				return "", err
			}
		}

		e.debugLog("LM OUTPUT STREAM", map[string]any{
			"content":       contentBuilder.String(),
			"tool_calls":    len(toolCalls),
			"finish_reason": finishReason,
		})

		// 构建消息
		msg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: contentBuilder.String(),
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}

		e.printVerbose("[assistant-stream] finish_reason=%s content=%q tool_calls=%d",
			finishReason, truncateForLog(msg.Content, 400), len(msg.ToolCalls))
		e.messages = append(e.messages, msg)

		// 如果有工具调用，执行工具并继续对话
		if len(toolCalls) > 0 {
			// 显示工具调用摘要
			fmt.Println()
			e.displayToolCallsStart(toolCalls)

			// 并发执行工具调用
			results := e.executeToolCallsConcurrently(ctx, toolCalls)
			e.displayToolCallsResults(results)

			// 继续对话前显示提示
			ui.PrintDim("  等待响应...")

			for _, result := range results {
				e.messages = append(e.messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result.resultStr,
					ToolCallID: result.toolCallID,
				})
			}
			continue
		}

		// 没有工具调用，根据 finish_reason 决定是否继续
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

// displayToolCallsStart 显示工具调用开始
func (e *ClawEngine) displayToolCallsStart(toolCalls []openai.ToolCall) {
	ui.PrintHeader("工具调用")
	for i, tc := range toolCalls {
		name := tc.Function.Name
		displayName, ok := ui.ToolNames[name]
		if !ok {
			displayName = name
		}
		icon := ui.ToolIcons[name]
		if icon == "" {
			icon = ui.IconTool
		}

		// 解析参数以获取更友好的显示
		var args map[string]any
		json.Unmarshal([]byte(tc.Function.Arguments), &args)

		var argsSummary string
		if path, ok := args["path"].(string); ok {
			argsSummary = path
		} else if query, ok := args["query"].(string); ok {
			argsSummary = truncateForLog(query, 30)
		} else if cmd, ok := args["command"].(string); ok {
			argsSummary = truncateForLog(cmd, 30)
		}

		if argsSummary != "" {
			ui.PrintDim("  %d. %s %s %s", i+1, icon, ui.SprintColor(ui.Bold, displayName), ui.SprintColor(ui.Dim, argsSummary))
		} else {
			ui.PrintDim("  %d. %s %s", i+1, icon, ui.SprintColor(ui.Bold, displayName))
		}
	}
}

// displayToolCallsResults 显示工具调用结果
func (e *ClawEngine) displayToolCallsResults(results []toolCallResult) {
	// 清除"等待响应..."提示
	ui.ClearLine()
	ui.MoveUp(1)
	ui.ClearLine()

	success := 0
	failed := 0
	for _, result := range results {
		if result.err != nil {
			failed++
			ui.PrintToolResult(false, result.err.Error())
		} else {
			success++
			ui.PrintToolResult(true, result.resultStr)
		}
	}

	// 显示摘要
	ui.PrintDim("  完成: %d 成功", success)
	if failed > 0 {
		ui.PrintError("  %d 失败", failed)
	}
}

func (e *ClawEngine) Run(ctx context.Context, prompt string) error {
	e.AddUserMessage(prompt)

	reply, err := e.RunTurn(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("\nMini-Code: %s\n\n", reply)
	return nil
}

// RunStream 流式执行对话
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
		// 如果没有提供 handler，默认打印最终内容
		fmt.Printf("%s\n", reply)
	}
	return nil
}

func truncateForLog(s string, limit int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}

	return s[:limit] + "..."
}
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mini-code/pkg/textutil"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// toolCallResult 工具调用结果
type toolCallResult struct {
	index      int // 原始顺序索引
	toolCallID string
	toolName   string
	resultStr  string
	err        error
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

func truncateForLog(s string, limit int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	return textutil.TruncateWithEllipsis(s, limit)
}

// executeToolCall 执行单个工具调用。
//
// 返回的 error 必须非 nil 当且仅当工具执行失败 —— UI 摘要与上层统计都依赖该字段。
// 不能只把错误写进结果字符串：那样调用方会把它当成成功（回归缺陷）。
func (a *BaseAgent) executeToolCall(toolCall openai.ToolCall) (string, error) {
	executor := tools.Executors[toolCall.Function.Name]
	if executor == nil {
		return fmt.Sprintf("错误: 未知工具 %s", toolCall.Function.Name),
			fmt.Errorf("未知工具 %s", toolCall.Function.Name)
	}

	result, err := executor(toolCall.Function.Arguments)
	output, _ := result.(string)

	if err != nil {
		if output != "" {
			return fmt.Sprintf("输出: %s\n错误: %s", output, err), err
		}
		return fmt.Sprintf("错误: %s", err), err
	}

	if output != "" {
		return output, nil
	}
	if result == nil {
		return "", nil
	}

	// 非字符串结果（结构体 / map）必须序列化为 JSON：
	// 直接用 %v 会输出 Go 语法串（map[path:a.go]），模型无法解析。
	encoded, encErr := json.Marshal(result)
	if encErr != nil {
		return fmt.Sprintf("%v", result), nil
	}
	return string(encoded), nil
}

// executeToolCallsConcurrently 并发执行工具调用，结果按原始顺序返回
func (a *BaseAgent) executeToolCallsConcurrently(ctx context.Context, toolCalls []openai.ToolCall) []toolCallResult {
	results := make([]toolCallResult, len(toolCalls))

	// 确定并发数
	maxConcurrency := a.maxConcurrency
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
			resultStr, execErr := a.executeToolCall(toolCall)
			results[index] = toolCallResult{
				index:      index,
				toolCallID: toolCall.ID,
				toolName:   toolCall.Function.Name,
				resultStr:  resultStr,
				err:        execErr,
			}
		}(i, tc)
	}

	wg.Wait()
	return results
}

// displayToolCallsStart 显示工具调用开始
func (a *BaseAgent) displayToolCallsStart(toolCalls []openai.ToolCall) {
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
func (a *BaseAgent) displayToolCallsResults(results []toolCallResult) {
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

	// 显示摘要：失败数必须如实呈现，不能只报成功数
	if failed > 0 {
		ui.PrintError("  完成: %d 成功, %d 失败", success, failed)
		return
	}
	ui.PrintDim("  完成: %d 成功", success)
}

func (a *BaseAgent) debugLog(tag string, v any) {
	if a.debugLogger == nil {
		return
	}

	data, _ := json.MarshalIndent(v, "", "  ")
	a.debugLogger.Printf("[%s] %s\n%s\n", time.Now().Format("15:04:05.000"), tag, data)
}

// toolCallAccumulator 按 delta 的 Index 累积单个工具调用的分片字段。
type toolCallAccumulator struct {
	call openai.ToolCall
	args strings.Builder
}

// assembleToolCalls 把流式返回的工具调用分片组装成完整的调用列表。
//
// 关键点：不能用「slice 下标」代替 delta 的 Index。旧实现假定两者相等，
// 并在 Index==nil 时直接丢弃整个分片，导致后续调用的参数错位甚至丢失
// （模型会拿着空参数去调工具，且收不到任何错误提示）。
// 当 Index 缺失时退化为「下一个到达位置」，保证参数不丢。
func assembleToolCalls(deltas []openai.ToolCall) []openai.ToolCall {
	accumulators := make(map[int]*toolCallAccumulator, len(deltas))
	order := make([]int, 0, len(deltas))

	for _, tc := range deltas {
		idx := len(order)
		if tc.Index != nil {
			idx = *tc.Index
		}

		acc, ok := accumulators[idx]
		if !ok {
			acc = &toolCallAccumulator{
				call: openai.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: openai.FunctionCall{
						Name: tc.Function.Name,
					},
				},
			}
			accumulators[idx] = acc
			order = append(order, idx)
		}

		// 后续分片可能补齐首块缺失的字段
		if tc.ID != "" {
			acc.call.ID = tc.ID
		}
		if tc.Function.Name != "" {
			acc.call.Function.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			acc.args.WriteString(tc.Function.Arguments)
		}
	}

	toolCalls := make([]openai.ToolCall, 0, len(order))
	for _, idx := range order {
		call := accumulators[idx].call
		call.Function.Arguments = accumulators[idx].args.String()
		toolCalls = append(toolCalls, call)
	}
	return toolCalls
}

// consumeStream 消费流式响应，返回内容、工具调用、完成原因
func (a *BaseAgent) consumeStream(ctx context.Context, stream *openai.ChatCompletionStream, handler StreamChunkHandler) (reply string, toolCalls []openai.ToolCall, finishReason openai.FinishReason, err error) {
	var contentBuilder strings.Builder
	var toolCallDeltas []openai.ToolCall

	for {
		response, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				stream.Close()
				break
			}
			stream.Close()
			return "", nil, "", fmt.Errorf("stream recv error: %w", recvErr)
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
				if handlerErr := handler(delta.Content, false); handlerErr != nil {
					return "", nil, "", handlerErr
				}
			}
		}

		// 处理工具调用：先收集全部分片，流结束后统一按 index 组装
		toolCallDeltas = append(toolCallDeltas, delta.ToolCalls...)

		// 捕获 finish_reason 并结束流
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
			stream.Close()
			break
		}
	}

	// 组装最终的 tool calls
	toolCalls = assembleToolCalls(toolCallDeltas)

	// 通知流结束
	if handler != nil {
		if handlerErr := handler(contentBuilder.String(), true); handlerErr != nil {
			return "", nil, "", handlerErr
		}
	}

	return contentBuilder.String(), toolCalls, finishReason, nil
}

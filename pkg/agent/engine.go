package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mini-claw/pkg/tools"
	"os"
	"runtime"
	"strings"
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
	client      chatCompletionClient
	model       string
	messages    []openai.ChatCompletionMessage
	debugLogger *log.Logger
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

func NewClawEngine(client chatCompletionClient, model string) *ClawEngine {
	systemMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: buildSystemPrompt(),
	}

	return &ClawEngine{
		client:      client,
		model:       model,
		messages:    []openai.ChatCompletionMessage{systemMessage},
		debugLogger: newDebugLogger(),
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

	return fmt.Sprintf(
		"你是一个中文 AI 编程助手 Mini-Claw。当前运行环境是 %s。%s "+
			"处理代码任务时，必须优先使用 list_files、search_in_files、read_file 理解项目，再进行修改。"+
			"所有文件路径必须使用工作区内的相对路径，不能访问工作区外的文件。"+
			"修改代码时，优先使用 replace_in_file 做最小修改，只有在必要时才使用 write_file 整体重写文件。"+
			"修改后优先运行 run_shell 执行 go test ./... 做验证。",
		osName,
		shellHint,
	)
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

func (e *ClawEngine) RunTurn(ctx context.Context) (string, error) {
	for i := 0; i < 10; i++ {
		fmt.Printf("[turn] step=%d message_count=%d\n", i+1, len(e.messages))

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

		msg := resp.Choices[0].Message
		fmt.Printf("[assistant] content=%q tool_calls=%d\n", truncateForLog(msg.Content, 400), len(msg.ToolCalls))
		e.messages = append(e.messages, msg)

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		for _, toolCall := range msg.ToolCalls {
			fmt.Printf("[tool-call] name=%s args=%s\n", toolCall.Function.Name, truncateForLog(toolCall.Function.Arguments, 400))

			executor := tools.Executors[toolCall.Function.Name]
			var resultStr string

			if executor == nil {
				resultStr = fmt.Sprintf("错误: 未知工具 %s", toolCall.Function.Name)
			} else {
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
			}

			fmt.Printf("[tool-result] name=%s result=%s\n", toolCall.Function.Name, truncateForLog(resultStr, 400))

			e.messages = append(e.messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultStr,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return "", fmt.Errorf("达到最大工具调用轮数，任务中止")
}

// RunTurnStream 流式执行一轮对话，支持流式输出
// handler 用于处理流式内容块，当工具调用发生时会先完成流式输出，执行工具后再返回
func (e *ClawEngine) RunTurnStream(ctx context.Context, handler StreamChunkHandler) (string, error) {
	for i := 0; i < 10; i++ {
		fmt.Printf("[turn-stream] step=%d message_count=%d\n", i+1, len(e.messages))

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
		defer stream.Close()

		var contentBuilder strings.Builder
		var toolCalls []openai.ToolCall
		var toolCallsDelta = make(map[int]*strings.Builder)

		for {
			response, err := stream.Recv()
			if err != nil {
				return "", fmt.Errorf("stream recv error: %w", err)
			}

			if len(response.Choices) == 0 {
				continue
			}

			delta := response.Choices[0].Delta

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

			// 检查是否完成
			if response.Choices[0].FinishReason != "" {
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

		e.debugLog("LM OUTPUT STREAM", map[string]interface{}{
			"content":    contentBuilder.String(),
			"tool_calls": len(toolCalls),
		})

		// 构建消息
		msg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: contentBuilder.String(),
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}

		fmt.Printf("[assistant-stream] content=%q tool_calls=%d\n", truncateForLog(msg.Content, 400), len(msg.ToolCalls))
		e.messages = append(e.messages, msg)

		if len(toolCalls) == 0 {
			return msg.Content, nil
		}

		// 执行工具调用
		for _, toolCall := range msg.ToolCalls {
			fmt.Printf("[tool-call] name=%s args=%s\n", toolCall.Function.Name, truncateForLog(toolCall.Function.Arguments, 400))

			executor := tools.Executors[toolCall.Function.Name]
			var resultStr string

			if executor == nil {
				resultStr = fmt.Sprintf("错误: 未知工具 %s", toolCall.Function.Name)
			} else {
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
			}

			fmt.Printf("[tool-result] name=%s result=%s\n", toolCall.Function.Name, truncateForLog(resultStr, 400))

			e.messages = append(e.messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultStr,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return "", fmt.Errorf("达到最大工具调用轮数，任务中止")
}

func (e *ClawEngine) Run(ctx context.Context, prompt string) error {
	e.AddUserMessage(prompt)

	reply, err := e.RunTurn(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("\nMini-Claw: %s\n\n", reply)
	return nil
}

// RunStream 流式执行对话
func (e *ClawEngine) RunStream(ctx context.Context, prompt string, handler StreamChunkHandler) error {
	e.AddUserMessage(prompt)

	fmt.Printf("\nMini-Claw: ")
	reply, err := e.RunTurnStream(ctx, handler)
	if err != nil {
		fmt.Printf("\n错误: %v\n\n", err)
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
	if len(s) <= limit {
		return s
	}

	return s[:limit] + "...(truncated)"
}

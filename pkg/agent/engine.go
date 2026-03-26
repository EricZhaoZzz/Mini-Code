package agent

import (
	"context"
	"fmt"
	"mini-claw/pkg/tools"
	"runtime"

	"github.com/sashabaranov/go-openai"
)

type ClawEngine struct {
	client   *openai.Client
	model    string
	messages []openai.ChatCompletionMessage
}

func NewClawEngine(client *openai.Client, model string) *ClawEngine {
	systemMessage := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: buildSystemPrompt()}

	return &ClawEngine{
		client:   client,
		model:    model,
		messages: []openai.ChatCompletionMessage{systemMessage},
	}
}

func buildSystemPrompt() string {
	osName := runtime.GOOS
	var shellHint string
	switch osName {
	case "windows":
		shellHint = "shell 命令请使用 Windows CMD 语法（例如用 mkdir test_data 而非 mkdir -p test_data）。"
	default:
		shellHint = "shell 命令请使用 Unix/bash 语法。"
	}
	return fmt.Sprintf(
		"你是一个全栈开发助手 Mini-Claw。当前运行环境是 %s，%s优先使用 write_file 工具创建文件（它会自动创建目录），避免不必要的 shell 命令。",
		osName, shellHint,
	)
}

func (e *ClawEngine) AddUserMessage(message string) {
	e.messages = append(e.messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: message,
	})
}

func (e *ClawEngine) RunTurn(ctx context.Context) (string, error) {
	for i := 1; i < 10; i++ {
		resp, err := e.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    e.model,
			Messages: e.messages,
			Tools:    tools.Definitions,
		})

		if err != nil {
			return "", fmt.Errorf("chat completion error: %w", err)
		}

		msg := resp.Choices[0].Message
		e.messages = append(e.messages, msg)

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		for _, toolCall := range msg.ToolCalls {
			fmt.Printf("[tool] 正在执行: %s\n", toolCall.Function.Name)

			executor := tools.Executors[toolCall.Function.Name]

			var resultStr string
			if executor != nil {
				resultStr = fmt.Sprintf("错误: 位置的工具 %s", toolCall.Function.Name)
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

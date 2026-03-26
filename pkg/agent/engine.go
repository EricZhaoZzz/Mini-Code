package agent

import (
	"context"
	"fmt"
	"mini-claw/pkg/tools"
	"runtime"

	"github.com/sashabaranov/go-openai"
)

type ClawEngine struct {
	client *openai.Client
	model  string
}

func NewClawEngine(client *openai.Client, model string) *ClawEngine {
	return &ClawEngine{
		client: client,
		model:  model,
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

func (e *ClawEngine) Run(ctx context.Context, prompt string) error {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: buildSystemPrompt()},
		{Role: openai.ChatMessageRoleUser, Content: prompt},
	}

	for i := 0; i < 10; i++ {
		resp, err := e.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    e.model,
			Messages: messages,
			Tools:    tools.Definitions,
		})

		if err != nil {
			fmt.Printf("Chat Completion error: %v\n", err)
			return err
		}

		fmt.Printf("🤖 [Agent response]: %s\n", resp.Choices[0].Message.Content)

		msg := resp.Choices[0].Message

		if len(msg.ToolCalls) == 0 {
			fmt.Printf("\n🦞 MinClaw: %s\n", msg.Content)
			break
		}

		messages = append(messages, msg)
		for _, toolCall := range msg.ToolCalls {
			fmt.Printf("🛠️  [Go 函数执行] 正在执行: %s\n", toolCall.Function.Name)
			executor := tools.Executors[toolCall.Function.Name]

			var resultStr string
			if executor == nil {
				resultStr = fmt.Sprintf("错误: 未知工具 %s", toolCall.Function.Name)
			} else {
				result, err := executor(toolCall.Function.Arguments)
				output, _ := result.(string)
				if err != nil {
					fmt.Printf("❌ Error executing tool %s: %s\n", toolCall.Function.Name, err)
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

			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultStr,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return nil
}

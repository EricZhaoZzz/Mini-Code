package agent

import (
	"context"
	"fmt"
	"mini-claw/pkg/tools"

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

func (e *ClawEngine) Run(ctx context.Context, prompt string) error {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "你是一个全栈开发助手 Mini-Claw。"},
		{Role: openai.ChatMessageRoleUser, Content: prompt},
	}

	for i := 0; i < 10; i++ {
		resp, err := e.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    e.model,
			Messages: messages,
			Tools:    tools.Definitions,
		})

		if err != nil {
			fmt.Errorf("Chat Completion error: %v", err)
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
			executor := tools.Executors[toolCall.Function.Name]
			result, err := executor(toolCall.Function.Arguments)

			if err != nil {
				fmt.Printf("❌ Error executing tool: %s\n", err)
				continue
			}

			fmt.Printf("🛠️  [Go 函数执行] 正在执行: %s\n", toolCall.Function.Name)
			resultStr, ok := result.(string)

			if !ok {
				resultStr = fmt.Sprintf("%v", result)
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

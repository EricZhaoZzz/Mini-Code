package provider

import "github.com/sashabaranov/go-openai"

func MakeTestChatRequest(model, content string) openai.ChatCompletionRequest {
	return openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: content},
		},
	}
}

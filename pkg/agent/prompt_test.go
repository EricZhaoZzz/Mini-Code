package agent

import (
	"context"
	"strings"
	"testing"

	"mini-code/pkg/provider"

	"github.com/sashabaranov/go-openai"
)

type promptCaptureProvider struct {
	requests []openai.ChatCompletionRequest
}

func (p *promptCaptureProvider) Chat(_ context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	p.requests = append(p.requests, req)
	return openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "ok",
				},
			},
		},
	}, nil
}

func (p *promptCaptureProvider) ChatStream(context.Context, openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return nil, nil
}

var _ provider.Provider = (*promptCaptureProvider)(nil)

func TestBuildSystemPromptWithMemory_ContainsCoreRules(t *testing.T) {
	prompt := BuildSystemPromptWithMemory(nil)

	for _, snippet := range []string{
		"你是一个专业的中文 AI 编程助手 Mini-Code",
		"在开始任何代码修改之前，你必须先理解项目结构",
		"用中文回复用户",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", snippet, prompt)
		}
	}
}

func TestCoderAgent_RunPreservesBasePromptAndAddsRoleRules(t *testing.T) {
	capture := &promptCaptureProvider{}
	ag := NewCoderAgent(capture, "test-model")

	basePrompt := "基础系统提示：请始终基于已读代码行动。"
	_, _, err := ag.Run(context.Background(), []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: basePrompt},
		{Role: openai.ChatMessageRoleUser, Content: "优化 agent 提示词"},
	}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(capture.requests))
	}

	systemPrompt := capture.requests[0].Messages[0].Content
	if !strings.Contains(systemPrompt, basePrompt) {
		t.Fatalf("expected specialized prompt to keep base prompt, got:\n%s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "搜索相似/相关文件中的同类模式") {
		t.Fatalf("expected coder role rules in prompt, got:\n%s", systemPrompt)
	}
}

func TestReviewerAgentPrompt_ContainsFindingsFirstRules(t *testing.T) {
	capture := &promptCaptureProvider{}
	ag := NewReviewerAgent(capture, "test-model")

	_, _, err := ag.Run(context.Background(), []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "基础提示"},
		{Role: openai.ChatMessageRoleUser, Content: "review 这段代码"},
	}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	systemPrompt := capture.requests[0].Messages[0].Content
	for _, snippet := range []string{
		"findings 优先",
		"按严重程度排序",
		"不直接修改文件",
	} {
		if !strings.Contains(systemPrompt, snippet) {
			t.Fatalf("expected reviewer prompt to contain %q, got:\n%s", snippet, systemPrompt)
		}
	}
}

func TestResearcherAgentPrompt_ContainsEvidenceRules(t *testing.T) {
	capture := &promptCaptureProvider{}
	ag := NewResearcherAgent(capture, "test-model")

	_, _, err := ag.Run(context.Background(), []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "基础提示"},
		{Role: openai.ChatMessageRoleUser, Content: "调研一下提示词系统"},
	}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	systemPrompt := capture.requests[0].Messages[0].Content
	for _, snippet := range []string{
		"区分已验证事实与基于证据的推断",
		"给出推荐方案",
		"不要把未经验证的猜测写入记忆",
	} {
		if !strings.Contains(systemPrompt, snippet) {
			t.Fatalf("expected researcher prompt to contain %q, got:\n%s", snippet, systemPrompt)
		}
	}
}

package orchestrator_test

import (
	"testing"

	"mini-code/pkg/orchestrator"

	"github.com/sashabaranov/go-openai"
)

func TestSessionSnapshotRoundTrip(t *testing.T) {
	orch := orchestrator.New(nil)
	session := orch.GetOrCreateSession("cli", "local")
	session.AppendUserMessage("请更新自己")
	session.AppendMessages([]openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleAssistant, Content: "收到"},
		{Role: openai.ChatMessageRoleTool, Content: "构建完成", ToolCallID: "tool-1"},
	})

	snapshot := session.ExportSnapshot()
	if snapshot.ChannelID != "cli" {
		t.Fatalf("expected channel id cli, got %q", snapshot.ChannelID)
	}
	if len(snapshot.Messages) != 4 {
		t.Fatalf("expected 4 messages in snapshot, got %d", len(snapshot.Messages))
	}

	restored := orch.RestoreSession(snapshot)
	if restored == nil {
		t.Fatal("expected restored session")
	}
	if restored.MessageCount() != 4 {
		t.Fatalf("expected restored message count 4, got %d", restored.MessageCount())
	}

	msgs := restored.Messages()
	if msgs[1].Content != "请更新自己" {
		t.Fatalf("expected user message to survive snapshot, got %q", msgs[1].Content)
	}
	if msgs[3].ToolCallID != "tool-1" {
		t.Fatalf("expected tool call id tool-1, got %q", msgs[3].ToolCallID)
	}
}

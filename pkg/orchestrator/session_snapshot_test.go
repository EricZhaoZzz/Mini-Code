package orchestrator_test

import (
	"encoding/json"
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

// 热重启后历史摘要必须保留，否则下次压缩无法与旧摘要合并，早期上下文永久丢失。
func TestSessionSnapshotPreservesSummary(t *testing.T) {
	orch := orchestrator.New(nil)
	defer orch.Close()
	session := orch.GetOrCreateSession("cli", "local")
	session.SetSummary("已修改 pkg/foo.go，待补测试")

	snapshot := session.ExportSnapshot()
	if snapshot.Summary != "已修改 pkg/foo.go，待补测试" {
		t.Fatalf("快照未包含摘要: %q", snapshot.Summary)
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded orchestrator.SessionSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	restored := orchestrator.New(nil)
	defer restored.Close()
	if got := restored.RestoreSession(decoded).Summary(); got != snapshot.Summary {
		t.Errorf("恢复后摘要 = %q, 期望 %q", got, snapshot.Summary)
	}
}

// 旧版本 worker 写出的快照没有 summary 字段，必须仍能正常恢复。
func TestSessionSnapshotWithoutSummaryField(t *testing.T) {
	var snap orchestrator.SessionSnapshot
	if err := json.Unmarshal([]byte(`{"id":"cli:local","channel_id":"cli","user_id":"local","messages":[]}`), &snap); err != nil {
		t.Fatal(err)
	}
	orch := orchestrator.New(nil)
	defer orch.Close()
	if got := orch.RestoreSession(snap).Summary(); got != "" {
		t.Errorf("无摘要快照恢复后摘要应为空, 得到 %q", got)
	}
}

package orchestrator_test

import (
	"context"
	"testing"

	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/orchestrator"

	"github.com/sashabaranov/go-openai"
)

type fakeChannel struct {
	msgs chan channel.IncomingMessage
	sent []string
}

func (f *fakeChannel) ChannelID() string                        { return "test" }
func (f *fakeChannel) Start(context.Context) error              { return nil }
func (f *fakeChannel) Messages() <-chan channel.IncomingMessage { return f.msgs }
func (f *fakeChannel) Send(msg channel.OutgoingMessage) (string, error) {
	f.sent = append(f.sent, msg.Text)
	return "", nil
}
func (f *fakeChannel) SendFile(_, _ string) error      { return nil }
func (f *fakeChannel) EditMessage(_, _ string) error   { return nil }
func (f *fakeChannel) NotifyDone(_, text string) error { f.sent = append(f.sent, text); return nil }

type fakeAgent struct {
	reply   string
	newMsgs []openai.ChatCompletionMessage
}

func (f *fakeAgent) Run(_ context.Context, _ []openai.ChatCompletionMessage, handler agent.StreamChunkHandler) (string, []openai.ChatCompletionMessage, error) {
	if handler != nil && f.reply != "" {
		if err := handler(f.reply, false); err != nil {
			return "", nil, err
		}
		if err := handler(f.reply, true); err != nil {
			return "", nil, err
		}
	}
	return f.reply, f.newMsgs, nil
}

func (f *fakeAgent) Name() string           { return "fake" }
func (f *fakeAgent) AllowedTools() []string { return nil }

type fakeRestartRuntime struct {
	pending bool
	applied []orchestrator.SessionSnapshot
}

func (f *fakeRestartRuntime) HasPendingRestart() bool {
	return f.pending
}

func (f *fakeRestartRuntime) ApplyPendingRestart(snapshot orchestrator.SessionSnapshot) error {
	f.applied = append(f.applied, snapshot)
	f.pending = false
	return nil
}

func TestOrchestrator_CreatesSessionPerUser(t *testing.T) {
	orch := orchestrator.New(nil) // nil memory store (Phase 1 无记忆)
	s1 := orch.GetOrCreateSession("cli", "user1")
	s2 := orch.GetOrCreateSession("cli", "user1")
	s3 := orch.GetOrCreateSession("cli", "user2")

	if s1 != s2 {
		t.Error("same user should get same session")
	}
	if s1 == s3 {
		t.Error("different users should get different sessions")
	}
}

func TestSession_ResetClearsMessages(t *testing.T) {
	orch := orchestrator.New(nil)
	s := orch.GetOrCreateSession("cli", "user1")
	s.AppendUserMessage("hello")
	s.AppendUserMessage("world")

	if len(s.Messages()) < 2 {
		t.Fatalf("expected messages, got %d", len(s.Messages()))
	}

	s.Reset()
	// Reset 后应只保留 system 消息
	for _, msg := range s.Messages() {
		if msg.Role != "system" {
			t.Errorf("after reset, expected only system messages, found role=%q", msg.Role)
		}
	}
}

func TestSession_AppendMessages(t *testing.T) {
	orch := orchestrator.New(nil)
	s := orch.GetOrCreateSession("cli", "user1")
	initialCount := len(s.Messages())

	s.AppendUserMessage("test message")
	if len(s.Messages()) != initialCount+1 {
		t.Errorf("expected %d messages, got %d", initialCount+1, len(s.Messages()))
	}
}

func TestSession_MessageCount(t *testing.T) {
	orch := orchestrator.New(nil)
	s := orch.GetOrCreateSession("cli", "user1")

	// 初始有 1 条 system 消息
	if s.MessageCount() != 1 {
		t.Errorf("expected 1 message (system), got %d", s.MessageCount())
	}

	s.AppendUserMessage("hello")
	if s.MessageCount() != 2 {
		t.Errorf("expected 2 messages, got %d", s.MessageCount())
	}
}

func TestOrchestratorHandleAppliesPendingRestartAfterReply(t *testing.T) {
	orch := orchestrator.New(nil)
	restartRuntime := &fakeRestartRuntime{pending: true}
	orch.SetRestartRuntime(restartRuntime)

	ch := &fakeChannel{msgs: make(chan channel.IncomingMessage, 1)}
	factory := func(string) agent.Agent {
		return &fakeAgent{
			reply: "已构建新版本，准备切换",
			newMsgs: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleAssistant, Content: "已构建新版本，准备切换"},
			},
		}
	}

	err := orch.Handle(context.Background(), channel.IncomingMessage{
		ChannelID: "cli",
		UserID:    "local",
		Text:      "请更新并重启",
	}, factory, ch)
	if err != nil {
		t.Fatalf("expected handle to succeed, got %v", err)
	}
	if len(restartRuntime.applied) != 1 {
		t.Fatalf("expected restart runtime to be applied once, got %d", len(restartRuntime.applied))
	}
	if got := restartRuntime.applied[0].Messages[len(restartRuntime.applied[0].Messages)-1].Content; got != "已构建新版本，准备切换" {
		t.Fatalf("expected latest assistant reply in snapshot, got %q", got)
	}
}

func TestOrchestratorSessionUsesSharedSystemPromptBuilder(t *testing.T) {
	orch := orchestrator.New(nil)
	session := orch.GetOrCreateSession("cli", "prompt-user")

	if got, want := session.Messages()[0].Content, agent.BuildSystemPromptWithMemory(nil); got != want {
		t.Fatalf("expected shared system prompt builder, got different prompt")
	}
}

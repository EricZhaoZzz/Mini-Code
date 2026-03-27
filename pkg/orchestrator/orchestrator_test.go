package orchestrator_test

import (
	"testing"

	"mini-code/pkg/channel"
	"mini-code/pkg/orchestrator"
)

type fakeChannel struct {
	msgs chan channel.IncomingMessage
	sent []string
}

func (f *fakeChannel) ChannelID() string                             { return "test" }
func (f *fakeChannel) Messages() <-chan channel.IncomingMessage      { return f.msgs }
func (f *fakeChannel) Send(msg channel.OutgoingMessage) error        { f.sent = append(f.sent, msg.Text); return nil }
func (f *fakeChannel) SendFile(_, _ string) error                    { return nil }
func (f *fakeChannel) EditMessage(_, _ string) error                 { return nil }
func (f *fakeChannel) NotifyDone(_, text string) error               { f.sent = append(f.sent, text); return nil }

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
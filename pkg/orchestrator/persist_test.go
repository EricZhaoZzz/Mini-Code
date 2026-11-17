package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/orchestrator"

	"github.com/sashabaranov/go-openai"
)

type failingAgent struct {
	msgs []openai.ChatCompletionMessage
}

func (f *failingAgent) Run(context.Context, []openai.ChatCompletionMessage, agent.StreamChunkHandler) (string, []openai.ChatCompletionMessage, error) {
	return "", f.msgs, errors.New("模型请求失败")
}
func (f *failingAgent) Name() string           { return "failing" }
func (f *failingAgent) AllowedTools() []string { return nil }

func handle(t *testing.T, orch *orchestrator.Orchestrator, text string, a agent.Agent) error {
	t.Helper()
	ch := &fakeChannel{msgs: make(chan channel.IncomingMessage, 1)}
	return orch.Handle(context.Background(), channel.IncomingMessage{ChannelID: "cli", UserID: "local", Text: text},
		func(string) agent.Agent { return a }, ch)
}

func TestSaveLoadSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "last.json")
	snap := orchestrator.SessionSnapshot{
		ChannelID: "cli", UserID: "local", Summary: "摘要",
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "你好"}},
	}
	if err := orchestrator.SaveSnapshot(path, snap); err != nil {
		t.Fatal(err)
	}
	got, err := orchestrator.LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "摘要" || len(got.Messages) != 1 || got.Messages[0].Content != "你好" {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("不应残留临时文件")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("会话文件权限 = %v, 期望 0600", info.Mode().Perm())
		}
	}
}

func TestHandlePersistsSessionAfterTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last.json")
	orch := orchestrator.New(nil)
	defer orch.Close()
	orch.SetPersistPath(path)

	err := handle(t, orch, "实现登录", &fakeAgent{
		reply:   "好的",
		newMsgs: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleAssistant, Content: "好的"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := orchestrator.LoadSnapshot(path)
	if err != nil {
		t.Fatalf("任务结束后应已保存会话: %v", err)
	}
	last := snap.Messages[len(snap.Messages)-1]
	if last.Content != "好的" {
		t.Errorf("保存的会话应包含最新回复, 得到 %q", last.Content)
	}
}

// 任务失败时已产生的消息（如已执行的工具调用）同样要保存。
func TestHandlePersistsEvenWhenAgentFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last.json")
	orch := orchestrator.New(nil)
	defer orch.Close()
	orch.SetPersistPath(path)

	err := handle(t, orch, "修复 bug", &failingAgent{msgs: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleAssistant, Content: "已修改 a.go"},
	}})
	if err == nil {
		t.Fatal("期望返回 Agent 错误")
	}
	snap, loadErr := orchestrator.LoadSnapshot(path)
	if loadErr != nil {
		t.Fatalf("失败时也应保存: %v", loadErr)
	}
	if snap.Messages[len(snap.Messages)-1].Content != "已修改 a.go" {
		t.Error("失败前产生的消息应被保存")
	}
}

func TestHandleWithoutPersistPathWritesNothing(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)

	orch := orchestrator.New(nil)
	defer orch.Close()
	if err := handle(t, orch, "hi", &fakeAgent{}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("未启用持久化时不应写文件: %v", entries)
	}
}

func TestResumeSessionRestoresConversation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last.json")
	err := orchestrator.SaveSnapshot(path, orchestrator.SessionSnapshot{
		ChannelID: "old-channel", UserID: "old-user", Summary: "早期摘要",
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "过期的系统提示"},
			{Role: openai.ChatMessageRoleUser, Content: "第一轮"},
			{Role: openai.ChatMessageRoleAssistant, Content: "回复一"},
			{Role: openai.ChatMessageRoleUser, Content: "第二轮"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	orch := orchestrator.New(nil)
	defer orch.Close()
	orch.SetPersistPath(path)

	turns, err := orch.ResumeSession("cli", "local")
	if err != nil {
		t.Fatal(err)
	}
	if turns != 2 {
		t.Errorf("turns = %d, 期望 2", turns)
	}
	s := orch.GetOrCreateSession("cli", "local")
	msgs := s.Messages()
	if len(msgs) != 4 || msgs[3].Content != "第二轮" {
		t.Fatalf("会话内容未恢复到当前 channel/user: %+v", msgs)
	}
	if msgs[0].Content == "过期的系统提示" || !strings.Contains(msgs[0].Content, "Mini-Code") {
		t.Errorf("系统提示应按当前状态重建, 得到 %q", msgs[0].Content)
	}
	if s.Summary() != "早期摘要" {
		t.Errorf("摘要应一并恢复, 得到 %q", s.Summary())
	}
}

func TestResumeSessionErrors(t *testing.T) {
	orch := orchestrator.New(nil)
	defer orch.Close()

	if _, err := orch.ResumeSession("cli", "local"); err == nil || !strings.Contains(err.Error(), "未启用") {
		t.Errorf("未启用持久化时应报错, 得到 %v", err)
	}

	orch.SetPersistPath(filepath.Join(t.TempDir(), "missing.json"))
	if _, err := orch.ResumeSession("cli", "local"); err == nil || !strings.Contains(err.Error(), "没有可恢复的会话") {
		t.Errorf("文件不存在时应提示无可恢复会话, 得到 %v", err)
	}
}

func TestInspectSavedSession(t *testing.T) {
	dir := t.TempDir()
	if _, ok := orchestrator.InspectSavedSession(filepath.Join(dir, "none.json")); ok {
		t.Error("文件不存在时 ok 应为 false")
	}

	onlySystem := filepath.Join(dir, "sys.json")
	orchestrator.SaveSnapshot(onlySystem, orchestrator.SessionSnapshot{Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "s"},
	}})
	if _, ok := orchestrator.InspectSavedSession(onlySystem); ok {
		t.Error("没有用户消息的会话不应提示恢复")
	}

	corrupt := filepath.Join(dir, "bad.json")
	os.WriteFile(corrupt, []byte("{not json"), 0o600)
	if _, ok := orchestrator.InspectSavedSession(corrupt); ok {
		t.Error("损坏的文件不应提示恢复")
	}
}

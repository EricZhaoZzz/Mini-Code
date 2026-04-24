package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mini-code/pkg/orchestrator"

	"github.com/sashabaranov/go-openai"
)

func TestWorkerRuntimePrepareAndApplyPendingRestart(t *testing.T) {
	workspace := t.TempDir()

	var built bool
	var requested RestartRequest
	runtime := NewWorkerRuntime(WorkerRuntimeConfig{
		WorkspaceRoot: workspace,
		BuildBinary: func(root string) (string, error) {
			built = true
			if root != workspace {
				t.Fatalf("expected workspace %q, got %q", workspace, root)
			}
			return filepath.Join(workspace, ".mini-code", "releases", "next", "mini-code.exe"), nil
		},
		RequestRestart: func(req RestartRequest) error {
			requested = req
			return nil
		},
	})

	message, err := runtime.PrepareRestart()
	if err != nil {
		t.Fatalf("expected restart build to succeed, got %v", err)
	}
	if !built {
		t.Fatal("expected build callback to run")
	}
	if !runtime.HasPendingRestart() {
		t.Fatal("expected runtime to have pending restart after successful build")
	}
	if message == "" {
		t.Fatal("expected prepare restart message")
	}

	session := orchestrator.SessionSnapshot{
		ChannelID: "cli",
		UserID:    "local",
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "system"},
			{Role: openai.ChatMessageRoleUser, Content: "请重启"},
		},
	}

	if err := runtime.ApplyPendingRestart(session); err != nil {
		t.Fatalf("expected apply pending restart to succeed, got %v", err)
	}
	if requested.ExecutablePath == "" {
		t.Fatal("expected restart request to include executable path")
	}
	if requested.SnapshotPath == "" {
		t.Fatal("expected restart request to include snapshot path")
	}
	if _, err := os.Stat(requested.SnapshotPath); err != nil {
		t.Fatalf("expected snapshot file to be written, got %v", err)
	}
	if runtime.HasPendingRestart() {
		t.Fatal("expected pending restart to be cleared after apply")
	}
}

func TestWorkerRuntimePrepareRestartDoesNotMarkPendingOnBuildFailure(t *testing.T) {
	workspace := t.TempDir()
	runtime := NewWorkerRuntime(WorkerRuntimeConfig{
		WorkspaceRoot: workspace,
		BuildBinary: func(string) (string, error) {
			return "", errors.New("boom")
		},
	})

	if _, err := runtime.PrepareRestart(); err == nil {
		t.Fatal("expected prepare restart to fail when build fails")
	}
	if runtime.HasPendingRestart() {
		t.Fatal("expected no pending restart after build failure")
	}
}

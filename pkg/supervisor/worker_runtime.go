package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"mini-code/pkg/orchestrator"
)

type RestartRequest struct {
	ExecutablePath string `json:"executable_path"`
	SnapshotPath   string `json:"snapshot_path"`
	WorkspaceRoot  string `json:"workspace_root"`
}

type WorkerRuntimeConfig struct {
	WorkspaceRoot  string
	BuildBinary    func(workspaceRoot string) (string, error)
	RequestRestart func(request RestartRequest) error
}

type pendingRestart struct {
	executablePath string
}

type WorkerRuntime struct {
	workspaceRoot  string
	buildBinary    func(workspaceRoot string) (string, error)
	requestRestart func(request RestartRequest) error

	mu      sync.Mutex
	pending *pendingRestart
}

func NewWorkerRuntime(config WorkerRuntimeConfig) *WorkerRuntime {
	return &WorkerRuntime{
		workspaceRoot:  config.WorkspaceRoot,
		buildBinary:    config.BuildBinary,
		requestRestart: config.RequestRestart,
	}
}

func (r *WorkerRuntime) PrepareRestart() (string, error) {
	if r == nil {
		return "", fmt.Errorf("worker runtime 未初始化")
	}
	if r.buildBinary == nil {
		return "", fmt.Errorf("构建器未初始化")
	}

	executablePath, err := r.buildBinary(r.workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("构建新版本失败: %w", err)
	}

	r.mu.Lock()
	r.pending = &pendingRestart{executablePath: executablePath}
	r.mu.Unlock()

	return fmt.Sprintf("新版本已构建完成，等待当前任务结束后重启：%s", executablePath), nil
}

func (r *WorkerRuntime) HasPendingRestart() bool {
	if r == nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending != nil
}

func (r *WorkerRuntime) ApplyPendingRestart(snapshot orchestrator.SessionSnapshot) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	pending := r.pending
	r.mu.Unlock()

	if pending == nil {
		return nil
	}
	if r.requestRestart == nil {
		return fmt.Errorf("重启请求器未初始化")
	}

	snapshotPath, err := r.writeSnapshot(snapshot)
	if err != nil {
		return err
	}

	request := RestartRequest{
		ExecutablePath: pending.executablePath,
		SnapshotPath:   snapshotPath,
		WorkspaceRoot:  r.workspaceRoot,
	}
	if err := r.requestRestart(request); err != nil {
		return err
	}

	r.mu.Lock()
	r.pending = nil
	r.mu.Unlock()
	return nil
}

func (r *WorkerRuntime) writeSnapshot(snapshot orchestrator.SessionSnapshot) (string, error) {
	runtimeDir := filepath.Join(r.workspaceRoot, ".mini-code", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return "", fmt.Errorf("创建 runtime 目录失败: %w", err)
	}

	baseName := snapshot.ChannelID + "-" + snapshot.UserID
	baseName = strings.ReplaceAll(baseName, string(filepath.Separator), "_")
	if baseName == "-" || baseName == "" {
		baseName = "session"
	}

	path := filepath.Join(runtimeDir, baseName+".json")
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化会话快照失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入会话快照失败: %w", err)
	}
	return path, nil
}

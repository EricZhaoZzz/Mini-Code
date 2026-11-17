package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEvent 是一条审计记录。
type AuditEvent struct {
	Time      time.Time `json:"time"`
	Command   string    `json:"command"`
	Dir       string    `json:"dir"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason,omitempty"`
	Mechanism string    `json:"mechanism,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
	PID       int       `json:"pid,omitempty"`
	TimedOut  bool      `json:"timed_out,omitempty"`
}

// auditLogger 追加写入审计日志。
type auditLogger struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// auditLog 是全局审计器，默认未初始化（未初始化时 Log 为空操作）。
var auditLog = &auditLogger{}

// InitAudit 在工作区下初始化审计日志。
//
// 日志位置：<workspaceRoot>/.mini-code/audit.log，JSONL 格式追加写入。
// 初始化失败**不**阻断主流程——审计是记录手段，不是安全边界；
// 但调用方应把错误展示给用户，避免误以为「有审计」。
func InitAudit(workspaceRoot string) error {
	if workspaceRoot == "" {
		return nil
	}

	dir := filepath.Join(workspaceRoot, ".mini-code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(dir, "audit.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}

	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	if auditLog.f != nil {
		_ = auditLog.f.Close()
	}
	auditLog.f = f
	auditLog.path = path
	return nil
}

// CloseAudit 关闭审计日志文件，使 auditLog 回到「未初始化」状态。
//
// 路径一并清空：关闭后 AuditPath() 应返回空串，
// 否则调用方会误以为审计仍在生效。
func CloseAudit() {
	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	if auditLog.f != nil {
		_ = auditLog.f.Close()
		auditLog.f = nil
	}
	auditLog.path = ""
}

// AuditPath 返回当前审计日志路径，未初始化时返回空串。
func AuditPath() string {
	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	return auditLog.path
}

// Log 记录一条审计事件（失败不影响主流程）。
//
// 设计取舍：审计写入失败静默丢弃，不向上抛错。因为审计本身不承担
// 安全职责，为它中断用户的命令反而得不偿失。
func Log(ev AuditEvent) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}

	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()

	if auditLog.f == nil {
		return
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = auditLog.f.Write(append(data, '\n'))
}

package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuditLog_WritesJSONL 审计日志必须是合法 JSONL。
//
// 审计日志的用途是事后追溯「agent 到底执行了什么」，
// 格式不合法就等于没记。
func TestAuditLog_WritesJSONL(t *testing.T) {
	ws := t.TempDir()
	if err := InitAudit(ws); err != nil {
		t.Fatal(err)
	}
	defer CloseAudit()

	Log(AuditEvent{Command: "go test ./...", Dir: ws, Allowed: true, ExitCode: 0})

	data, err := os.ReadFile(filepath.Join(ws, ".mini-code", "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "go test") {
		t.Errorf("审计日志未记录命令: %s", data)
	}

	line := strings.TrimSpace(string(data))
	var ev AuditEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Errorf("审计日志不是合法 JSON: %v", err)
	}
	if ev.Command != "go test ./..." {
		t.Errorf("命令回读不一致: %q", ev.Command)
	}
}

// TestAuditLog_RecordsDenied 被拦截的命令也要落审计，且标记 Allowed=false。
func TestAuditLog_RecordsDenied(t *testing.T) {
	ws := t.TempDir()
	if err := InitAudit(ws); err != nil {
		t.Fatal(err)
	}
	defer CloseAudit()

	Log(AuditEvent{
		Command: "rm -rf /",
		Dir:     ws,
		Allowed: false,
		Reason:  "递归删除根目录或家目录",
	})

	data, err := os.ReadFile(filepath.Join(ws, ".mini-code", "audit.log"))
	if err != nil {
		t.Fatal(err)
	}

	var ev AuditEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Allowed {
		t.Error("被拒绝的事件 Allowed 应为 false")
	}
	if ev.Reason == "" {
		t.Error("被拒绝的事件应记录原因")
	}
}

// TestAuditLog_UninitializedIsNoop 未初始化时 Log 不能 panic。
func TestAuditLog_UninitializedIsNoop(t *testing.T) {
	// 关闭全局日志后调用不应报错
	CloseAudit()
	Log(AuditEvent{Command: "echo hi", Allowed: true})
	if AuditPath() != "" {
		t.Error("未初始化时 AuditPath 应为空")
	}
}

// TestAuditLog_AppendsMultiple 多条记录应逐行追加，不互相覆盖。
func TestAuditLog_AppendsMultiple(t *testing.T) {
	ws := t.TempDir()
	if err := InitAudit(ws); err != nil {
		t.Fatal(err)
	}
	defer CloseAudit()

	Log(AuditEvent{Command: "first", Allowed: true})
	Log(AuditEvent{Command: "second", Allowed: true})

	data, err := os.ReadFile(filepath.Join(ws, ".mini-code", "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("应有 2 行记录，得到 %d: %s", len(lines), data)
	}
}

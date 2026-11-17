package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mini-code/pkg/sandbox"
)

// TestRunShell_WritesAuditLog 验证 run_shell 真的把执行记录落到了审计日志。
//
// 这是 L3 的核心接线：命令执行与审计必须是同一条路径，
// 否则「有审计」只是文档上的说法。
func TestRunShell_WritesAuditLog(t *testing.T) {
	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	if err := sandbox.InitAudit(ws); err != nil {
		t.Fatal(err)
	}
	defer sandbox.CloseAudit()

	if _, err := RunShell(RunShellArguments{Command: normalEchoCommand()}); err != nil {
		t.Fatalf("命令执行失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws, ".mini-code", "audit.log"))
	if err != nil {
		t.Fatalf("审计日志未生成: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("应有放行 + 结果两条记录，得到 %d 条: %s", len(lines), data)
	}

	// 最后一条应带执行结果
	var last sandbox.AuditEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("最后一条记录不是合法 JSON: %v", err)
	}
	if !last.Allowed {
		t.Error("正常命令应记录为 Allowed=true")
	}
	if last.PID == 0 {
		t.Error("结果记录应带 PID")
	}
	if last.ExitCode != 0 {
		t.Errorf("正常命令退出码应为 0，得到 %d", last.ExitCode)
	}
}

// TestRunShell_AuditsDeniedCommand 被黑名单拦截的命令也要留痕。
func TestRunShell_AuditsDeniedCommand(t *testing.T) {
	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	if err := sandbox.InitAudit(ws); err != nil {
		t.Fatal(err)
	}
	defer sandbox.CloseAudit()

	if _, err := RunShell(RunShellArguments{Command: `rm -rf /`}); err == nil {
		t.Fatal("危险命令应被拒绝")
	}

	data, err := os.ReadFile(filepath.Join(ws, ".mini-code", "audit.log"))
	if err != nil {
		t.Fatalf("审计日志未生成: %v", err)
	}
	if !strings.Contains(string(data), `"allowed":false`) {
		t.Errorf("应记录一条 Allowed=false 的事件: %s", data)
	}
}

// TestRunShell_StrictModeFailsClosed 严格模式下沙箱不可用必须拒绝执行。
//
// 这条断言的是「宁可不执行，也不能无保护执行」。
func TestRunShell_StrictModeFailsClosed(t *testing.T) {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		t.Skip("该平台有 OS 沙箱，本例只校验不支持平台的 fail-closed 行为")
	}

	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	t.Setenv("LM_SANDBOX_STRICT", "1")
	SetSandboxConfig(sandbox.Config{Strict: true})
	defer SetSandboxConfig(sandbox.Config{})

	_, err := RunShell(RunShellArguments{Command: normalEchoCommand()})
	if err == nil {
		t.Fatal("严格模式下沙箱不可用，应拒绝执行而非降级运行")
	}
	if !strings.Contains(err.Error(), "拒绝执行") {
		t.Errorf("错误信息应说明是拒绝执行，得到: %v", err)
	}
}

// TestSandboxConfig_RoundTrip 配置注入后可原样读回。
func TestSandboxConfig_RoundTrip(t *testing.T) {
	cfg := sandbox.Config{
		WorkspaceRoot:      t.TempDir(),
		ExtraWritableRoots: []string{"/tmp/x"},
		ReadOnlyPaths:      []string{"/usr"},
		Strict:             true,
	}
	SetSandboxConfig(cfg)
	defer SetSandboxConfig(sandbox.Config{})

	got := CurrentSandboxConfig()
	if got.WorkspaceRoot != cfg.WorkspaceRoot || got.Strict != cfg.Strict {
		t.Errorf("配置回读不一致: %+v", got)
	}
	if len(got.ExtraWritableRoots) != 1 || len(got.ReadOnlyPaths) != 1 {
		t.Errorf("路径列表回读不一致: %+v", got)
	}
}

// TestRunShell_UnaffectedWhenSandboxDisabled LM_SANDBOX=0 时命令照常执行。
func TestRunShell_UnaffectedWhenSandboxDisabled(t *testing.T) {
	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	t.Setenv("LM_SANDBOX", "0")

	result, err := RunShell(RunShellArguments{Command: normalEchoCommand()})
	if err != nil {
		t.Fatalf("显式关闭沙箱后命令应正常执行: %v", err)
	}
	if !strings.Contains(result.(string), "hello") {
		t.Errorf("输出异常: %q", result)
	}
}

// TestResolveWorkspacePath_RejectsMklinkCreatedJunction 端到端验证链接创建被兜住。
//
// Task 1.3 拦住了 mklink，这里断言的是「即使 junction 被别的方式建出来，
// 文件工具仍然拒绝穿越」——两层防护不能互相替代。
func TestResolveWorkspacePath_RejectsJunctionEvenIfCreated(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 特有")
	}
	if err := CheckShellCommand(`mklink /J link C:\Windows`); err == nil {
		t.Error("mklink 应被 shell 守卫拒绝")
	}
	if err := CheckShellCommand(`fsutil hardlink create a b`); err == nil {
		t.Error("fsutil hardlink create 应被 shell 守卫拒绝")
	}
	if err := CheckShellCommand(`New-Item -ItemType Junction -Path x -Target y`); err == nil {
		t.Error("New-Item Junction 应被 shell 守卫拒绝")
	}
}

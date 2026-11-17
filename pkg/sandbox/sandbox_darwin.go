//go:build darwin

// ⚠️ 维护风险：sandbox-exec 虽被 Apple 标为 DEPRECATED，但底层 Seatbelt
// 子系统功能完好，且被大量组件依赖（Safari、Mail、Chromium、Firefox、
// SwiftPM、Homebrew、Bazel、Nix，以及 Claude Code / Codex / Cursor），
// 移除概率极低。
//
// 但 SDK 27 确实移除了内置 profile 常量（kSBXProfile*）。
// 本实现只使用自定义 profile 字符串，不受影响。
// 若未来 Apple 移除 Seatbelt，此文件需改用 App Sandbox 或废弃。
//
// ⚠️ 测试状态：本项目开发机为 Windows，本文件**未在 macOS 上实测**。
// 若 profile 过严导致命令失败，可用 LM_SANDBOX=0 关闭 L2 层。

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// darwinMechanism 标识 Seatbelt。
const darwinMechanism = "seatbelt"

// sandboxExecPath 是 sandbox-exec 的绝对路径。
const sandboxExecPath = "/usr/bin/sandbox-exec"

// sandboxProfile 生成 SBPL profile。
//
// 只允许工作区内读写 + 系统只读路径，其余一律拒绝。
// 注意：必须用自定义 profile，不要用内置 kSBXProfile* 常量
// —— 它们在 macOS SDK 27 已被移除，进程会在尝试时被杀。
func sandboxProfile(cfg Config) string {
	readWrite := append([]string{cfg.WorkspaceRoot}, cfg.ExtraWritableRoots...)

	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")

	// 只读：系统目录与工具链
	for _, p := range append(append([]string{}, cfg.ReadOnlyPaths...), systemReadOnlyPaths()...) {
		b.WriteString(fmt.Sprintf("(allow file-read* (subpath %s))\n", quoteSBPL(p)))
	}

	// 可读写：工作区与缓存目录
	for _, p := range readWrite {
		b.WriteString(fmt.Sprintf("(allow file-read* file-write* (subpath %s))\n", quoteSBPL(p)))
	}

	// 进程与系统服务：构建工具链需要 fork/exec 与少量 sysctl
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow signal (target self))\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow iokit-open)\n")
	// 允许出站网络：go mod / npm install 等依赖网络
	b.WriteString("(allow network-outbound)\n")

	return b.String()
}

// systemReadOnlyPaths 返回必须允许只读的系统路径。
func systemReadOnlyPaths() []string {
	paths := []string{"/usr", "/bin", "/sbin", "/etc", "/System", "/Library", "/dev", "/private/etc"}
	// 临时目录既需读也需写，单独处理
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		paths = append(paths, tmp)
	}
	return paths
}

// quoteSBPL 生成 SBPL 中的带引号字符串，转义引号与反斜杠。
func quoteSBPL(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// GetStatus 探测 sandbox-exec 是否可用。
func GetStatus(cfg Config) Status {
	st := Status{
		Mechanism:     darwinMechanism,
		WritableRoots: cfg.ExtraWritableRoots,
	}

	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		st.Reason = "sandbox-exec 不可用（Seatbelt 已被移除或不在 PATH 中）"
		return st
	}

	st.Available = true
	return st
}

// Wrap 通过 sandbox-exec 包装命令。
//
// 与 Landlock 不同，Seatbelt 只能作用于**子进程**，
// 因此这里改写 cmd 的 Path/Args 指向 sandbox-exec。
func Wrap(_ context.Context, cmd *exec.Cmd, cfg Config) error {
	if cfg.WorkspaceRoot == "" {
		return fmt.Errorf("沙箱配置缺少 WorkspaceRoot")
	}

	profilePath := filepath.Join(os.TempDir(),
		fmt.Sprintf("mini-code-sandbox-%d.sb", os.Getpid()))
	if err := os.WriteFile(profilePath, []byte(sandboxProfile(cfg)), 0o600); err != nil {
		return fmt.Errorf("写入 sandbox profile 失败: %w", err)
	}
	registerProfileCleanup(profilePath)

	originalPath := cmd.Path
	originalArgs := append([]string(nil), cmd.Args...)

	cmd.Path = sandboxExecPath
	cmd.Args = append([]string{"sandbox-exec", "-f", profilePath, originalPath}, originalArgs[1:]...)

	return nil
}

// RunSandboxed 在 macOS 上不使用 runner 子进程。
//
// Seatbelt 只作用于子进程，由父进程直接通过 sandbox-exec 包装即可，
// 不需要像 Landlock 那样先收紧再 exec。
func RunSandboxed(_ []string) error {
	return fmt.Errorf("macOS 使用 sandbox-exec 直接包装，无需 runner 子进程")
}

// sandboxProfileCleanup 收集待删除的 profile 文件。
var sandboxProfileCleanup sync.Map

func registerProfileCleanup(path string) {
	sandboxProfileCleanup.Store(path, struct{}{})
}

// CleanupProfiles 删除已生成的 profile 文件。
//
// 应在命令执行结束后调用，避免临时目录堆积。
func CleanupProfiles() {
	sandboxProfileCleanup.Range(func(k, _ any) bool {
		if path, ok := k.(string); ok {
			_ = os.Remove(path)
			sandboxProfileCleanup.Delete(k)
		}
		return true
	})
}

// DefaultWritableRoots 返回默认额外可写目录。
//
// 与系统只读路径一致，临时目录既需读也需写；TMPDIR 未必设置，故一并兜底。
func DefaultWritableRoots() []string {
	var roots []string
	for _, env := range []string{"GOCACHE", "GOMODCACHE", "TMPDIR", "TEMP", "TMP"} {
		if p := os.Getenv(env); p != "" {
			roots = append(roots, p)
		}
	}
	for _, p := range []string{"/tmp", "/private/tmp"} {
		if _, err := os.Stat(p); err == nil {
			roots = append(roots, p)
		}
	}
	return roots
}

// DefaultReadOnlyPaths 返回默认只读工具链目录。
func DefaultReadOnlyPaths() []string {
	var paths []string
	if goroot := runtime.GOROOT(); goroot != "" {
		paths = append(paths, goroot)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		paths = append(paths, gopath)
	}
	return paths
}

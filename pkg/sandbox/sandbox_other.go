//go:build !linux && !darwin

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// GetStatus 返回当前平台的沙箱能力。
//
// 这些平台上没有可行的低成本 OS 级文件沙箱，因此 Available 恒为 false，
// 并且**必须**给出面向用户的原因——不能留空，否则调用方无法判断是否该告警。
func GetStatus(cfg Config) Status {
	return Status{
		Available: false,
		Mechanism: MechanismNone,
		Reason: fmt.Sprintf(
			"%s 上没有可行的低成本 OS 级文件沙箱。"+
				"Job Objects 不限制文件访问；AppContainer 要求事先穷举进程需要的能力，"+
				"与 coding agent 开放的行为空间不匹配。"+
				"本平台仅提供 L1 路径校验 + L3 命令审批与审计。",
			runtime.GOOS),
		WritableRoots: cfg.ExtraWritableRoots,
	}
}

// Wrap 在不支持的平台不做任何注入，返回 nil。
//
// 这里不是「静默降级」：调用方应先查 GetStatus().Available，
// 由 Config.Strict 决定是硬失败还是继续无沙箱执行。
func Wrap(_ context.Context, _ *exec.Cmd, _ Config) error {
	return nil
}

// RunSandboxed 在不支持的平台没有 runner 子进程。
func RunSandboxed(_ []string) error {
	return fmt.Errorf("%s 不支持 OS 级沙箱 runner", runtime.GOOS)
}

// DefaultWritableRoots 返回默认额外可写目录。
func DefaultWritableRoots() []string {
	var roots []string
	for _, env := range []string{"GOCACHE", "GOMODCACHE", "TMPDIR", "TEMP", "TMP"} {
		if p := os.Getenv(env); p != "" {
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

// CleanupProfiles 在不支持的平台无操作（不生成 profile 文件）。
func CleanupProfiles() {}

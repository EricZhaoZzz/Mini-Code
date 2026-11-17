// Package sandbox 为 shell 命令执行提供 OS 级文件系统边界。
//
// 分三层，且**刻意不对称**——不要为了追求三平台一致而假装等价：
//
//   - Linux   : Landlock，内核强制，无需额外依赖。本项目唯一同时满足
//     「内核强制」+「零外部依赖」的选项（bubblewrap 需 apt install，
//     nsjail 需 root，gVisor 需 Docker）。
//   - macOS   : sandbox-exec（Seatbelt），系统自带，只作用于子进程。
//   - Windows : 无可行的低成本 OS 机制。Job Objects 不限制文件访问；
//     AppContainer 要求事先穷举进程需要的能力，与 coding agent 开放的
//     行为空间不匹配（OpenAI 在 Codex 上同样否决）。故此平台只有 L1+L3。
//
// 各层职责：
//   - L1 路径边界加固（pkg/tools/workspace.go）：全平台
//   - L2 OS 强制（本包）：Linux/macOS，fail-closed
//   - L3 审批与审计（pkg/sandbox/audit.go）：全平台
//
// 关键原则：绝不静默降级。沙箱起不来必须让调用方知道，
// 否则用户会误以为有保护而实际没有。
//
// 本包对外只暴露四个函数（按平台分文件实现）：
//
//	GetStatus(cfg Config) Status
//	   返回当前平台的沙箱能力（不实际启用），供提示词与启动日志展示。
//
//	Wrap(ctx context.Context, cmd *exec.Cmd, cfg Config) error
//	   就地改写 cmd（Path/Args/Env 等）使其在沙箱内执行，调用方之后只需 Start。
//	   返回 error 表示沙箱无法启用——调用方**必须**处理，不能静默忽略，
//	   否则用户会误以为有保护而实际没有。
//
//	RunSandboxed(args []string) error
//	   runner 子进程入口（仅 Linux 需要）：收紧沙箱后 exec 目标命令，成功时不返回。
package sandbox

import (
	"os"
	"strings"
)

// Status 描述当前平台的沙箱能力。
type Status struct {
	// Available 表示本平台是否支持 OS 级强制
	Available bool
	// Mechanism 说明使用的机制（landlock / seatbelt / none）
	Mechanism string
	// Reason 在 Available=false 时说明原因（面向用户展示）
	Reason string
	// WritableRoots 是允许写入的额外根目录
	WritableRoots []string
}

// Config 是沙箱配置。
type Config struct {
	// WorkspaceRoot 工作区根目录（唯一允许读写的项目目录）
	WorkspaceRoot string
	// ExtraWritableRoots 额外允许读写的目录（如 GOCACHE、TMPDIR）
	ExtraWritableRoots []string
	// ReadOnlyPaths 额外允许只读访问的路径（如 GOROOT、工具链）
	ReadOnlyPaths []string
	// Strict 严格模式下沙箱不可用即报错；否则降级为无沙箱
	Strict bool
}

// MechanismNone 标识无 OS 级沙箱。
const MechanismNone = "none"

// RunnerFlag 是沙箱 runner 子进程的命令行标志。
//
// 为什么需要它：Landlock 作用于**调用线程**且不可逆。若在 mini-code
// 主进程里收紧，主进程自己写记忆库（~/.mini-code/memory.db）、审计日志、
// 构建产物（.mini-code/releases/）也都会被拒绝——等于把自己锁死。
// 正确做法是把 shell 命令交给一个专用的 runner 子进程，由它在 exec
// 之前收紧，之后限制自动继承给 shell 及其全部子孙进程。
const RunnerFlag = "--sandbox-run"

// IsRunnerInvocation 判断 argv 是否为 sandbox runner 子进程调用。
func IsRunnerInvocation(args []string) bool {
	return len(args) >= 2 && args[1] == RunnerFlag
}

// splitPathList 拆分 PATH 风格的分隔列表。
func splitPathList(s string) []string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, p := range strings.Split(s, string(os.PathListSeparator)) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

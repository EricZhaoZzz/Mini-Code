package tools

import (
	"os"
	"strings"

	"mini-code/pkg/sandbox"
)

// sandboxCfg 是 shell 命令使用的沙箱配置，由 cmd/agent 启动时注入。
//
// 默认值是「关闭 L2、不严格」：未注入配置时（如单测）保持既有行为，
// 不会因为拿不到配置就把所有命令拒掉。
var sandboxCfg sandbox.Config

// SetSandboxConfig 设置 shell 命令使用的沙箱配置。
// 必须在执行任何 run_shell 之前调用（cmd/agent 已接入）。
func SetSandboxConfig(cfg sandbox.Config) {
	sandboxCfg = cfg
}

// CurrentSandboxConfig 返回当前的沙箱配置。
func CurrentSandboxConfig() sandbox.Config {
	return sandboxCfg
}

// CurrentWorkspaceRoot 返回当前固定的工作区根目录。
func CurrentWorkspaceRoot() (string, error) {
	return workspaceRoot()
}

// sandboxEnabled 判断 L2 OS 沙箱是否启用。
//
// LM_SANDBOX=0 可显式关闭——这是逃生舱：万一某个平台的沙箱策略
// 过严导致正常构建被拒，用户能立刻退回 L1+L3，而不必改代码。
// 注意关闭时会打印醒目提示，不是静默降级。
func sandboxEnabled() bool {
	return os.Getenv("LM_SANDBOX") != "0"
}

// SandboxDisabledReason 在沙箱被显式关闭时返回非空说明。
func SandboxDisabledReason() string {
	if os.Getenv("LM_SANDBOX") == "0" {
		return "LM_SANDBOX=0：已显式关闭 OS 级沙箱"
	}
	return ""
}

// extraWritableRootsFromEnv 从环境变量读取额外可写目录。
func extraWritableRootsFromEnv() []string {
	raw := os.Getenv("LM_SANDBOX_WRITABLE")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

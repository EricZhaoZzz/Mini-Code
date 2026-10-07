package tools

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRunShell_BlocksDangerousCommand 端到端验证危险命令被真正拒绝。
func TestRunShell_BlocksDangerousCommand(t *testing.T) {
	res, err := RunShell(map[string]string{"command": "rm -rf /"})
	if err == nil {
		t.Fatalf("危险命令未被拒绝，返回: %v", res)
	}
	if !strings.Contains(err.Error(), "安全策略") {
		t.Errorf("错误信息应说明是安全策略拒绝，得到: %v", err)
	}
}

// TestRunShell_AllowsNormalCommand 普通命令仍可执行（策略不能误伤开发流程）。
func TestRunShell_AllowsNormalCommand(t *testing.T) {
	_, err := RunShell(map[string]string{"command": "echo mini-code-ok"})
	if err != nil {
		t.Fatalf("普通命令被拒绝: %v", err)
	}
}

// TestShellTimeout_DefaultAndOverride 验证超时配置解析。
func TestShellTimeout_DefaultAndOverride(t *testing.T) {
	t.Run("默认 120 秒", func(t *testing.T) {
		t.Setenv("LM_SHELL_TIMEOUT", "")
		if got := shellTimeout(); got != 120*time.Second {
			t.Errorf("默认超时 = %v, 期望 120s", got)
		}
	})

	t.Run("环境变量覆盖", func(t *testing.T) {
		t.Setenv("LM_SHELL_TIMEOUT", "30")
		if got := shellTimeout(); got != 30*time.Second {
			t.Errorf("覆盖超时 = %v, 期望 30s", got)
		}
	})

	t.Run("非法值回退默认", func(t *testing.T) {
		for _, v := range []string{"abc", "0", "-5"} {
			t.Setenv("LM_SHELL_TIMEOUT", v)
			if got := shellTimeout(); got != 120*time.Second {
				t.Errorf("非法值 %q 得到 %v, 期望回退 120s", v, got)
			}
		}
	})
}

// TestRunShell_TimesOut 验证长驻命令会被超时终止，而不是永久挂死。
func TestRunShell_TimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过超时测试：会真实等待")
	}

	t.Setenv("LM_SHELL_TIMEOUT", "1")

	start := time.Now()
	res, err := RunShell(map[string]string{"command": sleepLongCommand()})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("超时命令应返回错误，得到: %v", res)
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Errorf("错误信息应说明超时，得到: %v", err)
	}
	// 给足余量：1 秒超时 + 进程终止开销，3 秒内必须返回
	if elapsed > 3*time.Second {
		t.Errorf("超时后未及时返回，耗时 %v", elapsed)
	}
}

// sleepLongCommand 返回一个跨平台的长时间阻塞命令。
func sleepLongCommand() string {
	if runtime.GOOS == "windows" {
		return "ping -n 30 127.0.0.1 > nul"
	}
	return "sleep 30"
}

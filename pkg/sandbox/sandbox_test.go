package sandbox

import (
	"os"
	"runtime"
	"testing"
)

// TestGetStatus 校验各平台沙箱能力字段自洽。
//
// 核心断言：不可用时必须给出面向用户的原因——否则调用方无从判断该不该告警，
// 用户会在毫无提示的情况下「以为有沙箱」。
func TestGetStatus(t *testing.T) {
	st := GetStatus(Config{WorkspaceRoot: t.TempDir()})

	switch runtime.GOOS {
	case "linux", "darwin":
		if st.Mechanism == "" {
			t.Error("Mechanism 不应为空")
		}
		if !st.Available && st.Reason == "" {
			t.Error("不可用时必须给出原因（面向用户）")
		}
	default:
		if st.Available {
			t.Errorf("%s 不应声称有 OS 沙箱", runtime.GOOS)
		}
		if st.Reason == "" {
			t.Error("必须说明为何不可用")
		}
	}
}

// TestGetStatus_ReportsWritableRoots 配置中的额外可写目录应原样回传。
func TestGetStatus_ReportsWritableRoots(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:      t.TempDir(),
		ExtraWritableRoots: []string{"/tmp/cache"},
	}
	st := GetStatus(cfg)
	if len(st.WritableRoots) != 1 || st.WritableRoots[0] != "/tmp/cache" {
		t.Errorf("WritableRoots 应回传配置值，得到 %v", st.WritableRoots)
	}
}

// TestDefaultRootsNotEmpty 默认缓存目录推导应给出可用结果。
//
// 若返回空，Landlock 收紧后 go build 会因写不了 GOCACHE 直接失败。
func TestDefaultRootsNotEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 无 L2 沙箱，不需要推导")
	}
	t.Setenv("GOCACHE", "/tmp/gocache")
	if got := DefaultWritableRoots(); len(got) == 0 {
		t.Error("DefaultWritableRoots 不应为空")
	}
	if got := DefaultReadOnlyPaths(); len(got) == 0 {
		t.Error("DefaultReadOnlyPaths 不应为空")
	}
}

// TestIsRunnerInvocation 校验 runner 子进程标志识别。
func TestIsRunnerInvocation(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"/path/mini-code", RunnerFlag, "sh", "-c", "ls"}, true},
		{[]string{"/path/mini-code", "--worker"}, false},
		{[]string{"/path/mini-code"}, false},
		{[]string{}, false},
	}
	for _, c := range cases {
		if got := IsRunnerInvocation(c.args); got != c.want {
			t.Errorf("IsRunnerInvocation(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestRunSandboxed_UnsupportedPlatform 不支持的平台必须硬失败，而不是静默执行。
func TestRunSandboxed_UnsupportedPlatform(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅校验无 OS 沙箱的平台")
	}
	if err := RunSandboxed([]string{RunnerFlag, "cmd", "/C", "echo pwned"}); err == nil {
		t.Error("无 OS 沙箱的平台应拒绝运行 runner，而不是静默执行")
	}
}

// TestSplitPathList 校验 PATH 风格列表的拆分与空值处理。
func TestSplitPathList(t *testing.T) {
	if got := splitPathList(""); got != nil {
		t.Errorf("空串应返回 nil，得到 %v", got)
	}
	got := splitPathList("/a" + string(os.PathListSeparator) + " /b ")
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Errorf("splitPathList 结果错误: %v", got)
	}
}

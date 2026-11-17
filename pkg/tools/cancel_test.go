package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 任务取消时 run_shell 必须立即终止命令，而不是等到 120 秒超时。
func TestRunShellContext_CancelTerminatesCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过：会真实启动进程")
	}
	t.Setenv("LM_SHELL_TIMEOUT", "60")

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)

	start := time.Now()
	_, err := RunShellContext(ctx, map[string]string{"command": sleepLongCommand()})
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("取消后应返回取消错误, 得到 %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("错误应包装 context.Canceled: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("取消后未及时返回，耗时 %v", elapsed)
	}
}

func TestWebSearchContext_CancelAbortsRequest(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	t.Setenv("EXA_MCP_URL", server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	_, err := WebSearchContext(ctx, map[string]string{"query": "go"})
	if err == nil {
		t.Fatal("取消后应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("取消后未及时返回，耗时 %v", elapsed)
	}
}

func TestDispatchAgentContext_PassesContextToFactory(t *testing.T) {
	type key struct{}
	var got any
	SetDispatchFactory(func(ctx context.Context, role, task string) (string, error) {
		got = ctx.Value(key{})
		return "ok", nil
	})
	t.Cleanup(func() { SetDispatchFactory(nil) })

	ctx := context.WithValue(context.Background(), key{}, "parent")
	if _, err := DispatchAgentContext(ctx, map[string]string{"role": "reviewer", "task": "t"}); err != nil {
		t.Fatal(err)
	}
	if got != "parent" {
		t.Errorf("子 Agent 未收到父任务的 ctx, 得到 %v", got)
	}
}

// 注册表中的执行器必须把 ctx 透传给需要感知取消的工具。
func TestExecutorsPassContext(t *testing.T) {
	type key struct{}
	var got any
	SetDispatchFactory(func(ctx context.Context, role, task string) (string, error) {
		got = ctx.Value(key{})
		return "ok", nil
	})
	t.Cleanup(func() { SetDispatchFactory(nil) })

	ctx := context.WithValue(context.Background(), key{}, "v")
	if _, err := Executors["dispatch_agent"](ctx, `{"role":"researcher","task":"t"}`); err != nil {
		t.Fatal(err)
	}
	if got != "v" {
		t.Error("Executors 未透传 ctx")
	}
}

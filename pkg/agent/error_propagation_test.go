package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
	"mini-code/pkg/tools"
)

// TestExecuteToolCall_PropagatesError 验证工具失败能被上层统计到。
//
// 回归背景：旧实现把错误编码进 resultStr 字符串却返回 err==nil，
// 而 displayToolCallsResults 只看 r.err，导致 UI 摘要永远显示全成功。
func TestExecuteToolCall_PropagatesError(t *testing.T) {
	const (
		failTool    = "test_zz_fail"
		badJSONTool = "test_zz_badjson"
		okTool      = "test_zz_ok"
		strTool     = "test_zz_str"
	)

	tools.Executors[failTool] = func(args interface{}) (interface{}, error) {
		return "部分输出", errors.New("boom")
	}
	tools.Executors[badJSONTool] = func(args interface{}) (interface{}, error) {
		return nil, errors.New("invalid character 'i' looking for beginning of object key")
	}
	tools.Executors[okTool] = func(args interface{}) (interface{}, error) {
		return "一切正常", nil
	}
	// 非字符串结果：工具返回结构体时不应变成 Go 语法串
	tools.Executors[strTool] = func(args interface{}) (interface{}, error) {
		return map[string]interface{}{"path": "a.go", "size": 42}, nil
	}
	t.Cleanup(func() {
		for _, n := range []string{failTool, badJSONTool, okTool, strTool} {
			delete(tools.Executors, n)
		}
	})

	a := &BaseAgent{maxConcurrency: 1}

	tests := []struct {
		toolName  string
		wantErr   bool
		wantInStr string
	}{
		{failTool, true, "错误"},
		{badJSONTool, true, "错误"},
		{okTool, false, "一切正常"},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			res := a.executeToolCallsConcurrently(context.Background(), []openai.ToolCall{{
				ID:       "call_" + tt.toolName,
				Function: openai.FunctionCall{Name: tt.toolName, Arguments: `{}`},
			}})

			if len(res) != 1 {
				t.Fatalf("结果数量 = %d, 期望 1", len(res))
			}
			if tt.wantErr && res[0].err == nil {
				t.Errorf("err = nil, 期望非 nil（失败必须能被 UI 统计到）")
			}
			if !tt.wantErr && res[0].err != nil {
				t.Errorf("err = %v, 期望 nil（成功不应被记为失败）", res[0].err)
			}
			if !strings.Contains(res[0].resultStr, tt.wantInStr) {
				t.Errorf("resultStr = %q, 期望包含 %q", res[0].resultStr, tt.wantInStr)
			}
		})
	}
}

// TestExecuteToolCall_UnknownTool 未知工具必须报错，不能静默成功。
func TestExecuteToolCall_UnknownTool(t *testing.T) {
	a := &BaseAgent{maxConcurrency: 1}

	res := a.executeToolCallsConcurrently(context.Background(), []openai.ToolCall{{
		ID:       "call_unknown",
		Function: openai.FunctionCall{Name: "test_zz_nonexistent", Arguments: `{}`},
	}})

	if res[0].err == nil {
		t.Error("未知工具: err = nil, 期望非 nil")
	}
	if !strings.Contains(res[0].resultStr, "未知工具") {
		t.Errorf("resultStr = %q, 期望包含「未知工具」", res[0].resultStr)
	}
}

// TestExecuteToolCall_StructResultIsJSON 非字符串结果应序列化为 JSON。
func TestExecuteToolCall_StructResultIsJSON(t *testing.T) {
	const structTool = "test_zz_struct"
	tools.Executors[structTool] = func(args interface{}) (interface{}, error) {
		return map[string]interface{}{"path": "a.go"}, nil
	}
	t.Cleanup(func() { delete(tools.Executors, structTool) })

	a := &BaseAgent{maxConcurrency: 1}
	res := a.executeToolCallsConcurrently(context.Background(), []openai.ToolCall{{
		ID:       "call_struct",
		Function: openai.FunctionCall{Name: structTool, Arguments: `{}`},
	}})

	if res[0].err != nil {
		t.Fatalf("err = %v, 期望 nil", res[0].err)
	}
	if !strings.Contains(res[0].resultStr, `"path":"a.go"`) {
		t.Errorf("resultStr = %q, 期望是 JSON 序列化结果", res[0].resultStr)
	}
}

// TestExecuteToolCall_CancelledContext 上下文取消时应报错。
func TestExecuteToolCall_CancelledContext(t *testing.T) {
	a := &BaseAgent{maxConcurrency: 1}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := a.executeToolCallsConcurrently(ctx, []openai.ToolCall{{
		ID:       "call_cancelled",
		Function: openai.FunctionCall{Name: "test_zz_fail", Arguments: `{}`},
	}})

	if res[0].err == nil {
		t.Error("已取消的上下文: err = nil, 期望非 nil")
	}
	if !strings.Contains(res[0].resultStr, "取消") {
		t.Errorf("resultStr = %q, 期望提到取消", res[0].resultStr)
	}
}

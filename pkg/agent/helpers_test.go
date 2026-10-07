package agent

import (
	"testing"

	"github.com/sashabaranov/go-openai"
)

// TestAssembleToolCalls 覆盖流式 tool_call 分片的组装。
//
// 回归背景：旧实现假设 slice 下标等于 delta 的 Index，并在 Index==nil 时
// 直接 continue 丢弃整个分片。一旦首个 chunk 缺 Index，后续所有工具调用
// 的参数就会错位甚至被静默丢弃（模型拿到空参数调用工具且无任何提示）。
func TestAssembleToolCalls(t *testing.T) {
	intPtr := func(i int) *int { return &i }

	tests := []struct {
		name      string
		deltas    []openai.ToolCall
		wantNames []string
		wantArgs  []string
		wantIDs   []string
	}{
		{
			name: "index 正常递增，参数分片拼接",
			deltas: []openai.ToolCall{
				{Index: intPtr(0), ID: "call_0", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "read_file", Arguments: `{"path":`}},
				{Index: intPtr(0),
					Function: openai.FunctionCall{Arguments: `"a.go"}`}},
				{Index: intPtr(1), ID: "call_1", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "list_files", Arguments: `{"path":"."}`}},
			},
			wantNames: []string{"read_file", "list_files"},
			wantArgs:  []string{`{"path":"a.go"}`, `{"path":"."}`},
			wantIDs:   []string{"call_0", "call_1"},
		},
		{
			// 修复前会丢失 args
			name: "首个 chunk 缺 Index（回归）",
			deltas: []openai.ToolCall{
				{Index: nil, ID: "call_0", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "read_file", Arguments: `{"path":"a"}`}},
				{Index: intPtr(1), ID: "call_1", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "list_files", Arguments: `{"path":"."}`}},
			},
			wantNames: []string{"read_file", "list_files"},
			wantArgs:  []string{`{"path":"a"}`, `{"path":"."}`},
			wantIDs:   []string{"call_0", "call_1"},
		},
		{
			name: "全部缺 Index，退化为到达顺序",
			deltas: []openai.ToolCall{
				{Index: nil, ID: "call_0", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "git_status", Arguments: `{}`}},
				{Index: nil, ID: "call_1", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "git_log", Arguments: `{"n":5}`}},
			},
			wantNames: []string{"git_status", "git_log"},
			wantArgs:  []string{`{}`, `{"n":5}`},
			wantIDs:   []string{"call_0", "call_1"},
		},
		{
			name: "ID 与 Name 在后续 chunk 才补齐",
			deltas: []openai.ToolCall{
				{Index: intPtr(0), Function: openai.FunctionCall{Arguments: `{}`}},
				{Index: intPtr(0), ID: "call_late",
					Function: openai.FunctionCall{Name: "git_diff"}},
			},
			wantNames: []string{"git_diff"},
			wantArgs:  []string{`{}`},
			wantIDs:   []string{"call_late"},
		},
		{
			name: "index 乱序到达仍按 index 归位",
			deltas: []openai.ToolCall{
				{Index: intPtr(1), ID: "call_1", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "list_files", Arguments: `{"path":"."}`}},
				{Index: intPtr(0), ID: "call_0", Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: "read_file", Arguments: `{"path":"b.go"}`}},
			},
			wantNames: []string{"list_files", "read_file"},
			wantArgs:  []string{`{"path":"."}`, `{"path":"b.go"}`},
			wantIDs:   []string{"call_1", "call_0"},
		},
		{
			name:      "无工具调用",
			deltas:    nil,
			wantNames: nil,
			wantArgs:  nil,
			wantIDs:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assembleToolCalls(tt.deltas)

			if len(got) != len(tt.wantNames) {
				t.Fatalf("工具调用数量 = %d, 期望 %d", len(got), len(tt.wantNames))
			}
			for i := range got {
				if got[i].Function.Name != tt.wantNames[i] {
					t.Errorf("[%d] Name = %q, 期望 %q", i, got[i].Function.Name, tt.wantNames[i])
				}
				if got[i].Function.Arguments != tt.wantArgs[i] {
					t.Errorf("[%d] Arguments = %q, 期望 %q", i, got[i].Function.Arguments, tt.wantArgs[i])
				}
				if tt.wantIDs != nil && got[i].ID != tt.wantIDs[i] {
					t.Errorf("[%d] ID = %q, 期望 %q", i, got[i].ID, tt.wantIDs[i])
				}
			}
		})
	}
}

// TestAssembleToolCalls_NoArgumentLoss 专项验证：任何分片都不能丢参数。
func TestAssembleToolCalls_NoArgumentLoss(t *testing.T) {
	intPtr := func(i int) *int { return &i }

	// 模拟 provider 省略首个 chunk 的 Index
	deltas := []openai.ToolCall{
		{Index: nil, ID: "call_0", Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{Name: "read_file", Arguments: `{"path":"x"}`}},
		{Index: intPtr(1), ID: "call_1", Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{Name: "git_diff", Arguments: `{"path":"y"}`}},
		{Index: intPtr(2), ID: "call_2", Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{Name: "git_log", Arguments: `{"n":3}`}},
	}

	got := assembleToolCalls(deltas)
	if len(got) != 3 {
		t.Fatalf("工具调用数量 = %d, 期望 3", len(got))
	}

	want := []string{`{"path":"x"}`, `{"path":"y"}`, `{"n":3}`}
	for i, w := range want {
		if got[i].Function.Arguments != w {
			t.Errorf("[%d] 参数丢失: 得到 %q, 期望 %q", i, got[i].Function.Arguments, w)
		}
	}
}

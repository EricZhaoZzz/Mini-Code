package agent_test

import (
	"mini-code/pkg/agent"
	"mini-code/pkg/provider"
	"testing"
)

func newTestProvider() provider.Provider {
	// 返回一个不实际调用的空 Provider（仅用于工具集测试）
	return nil
}

func TestCoderAgent_HasAllTools(t *testing.T) {
	a := agent.NewCoderAgent(newTestProvider(), "model")
	tools := a.AllowedTools()
	// CoderAgent 传 nil 表示允许全部工具
	if tools != nil {
		t.Error("CoderAgent should allow all tools (nil allowedTools)")
	}
}

func TestReviewerAgent_OnlyHasReadOnlyTools(t *testing.T) {
	a := agent.NewReviewerAgent(newTestProvider(), "model")
	allowed := make(map[string]bool)
	for _, t := range a.AllowedTools() {
		allowed[t] = true
	}

	// 必须有的只读工具
	for _, required := range []string{"read_file", "git_diff", "git_log", "recall"} {
		if !allowed[required] {
			t.Errorf("ReviewerAgent missing required tool: %s", required)
		}
	}
	// 禁止写操作
	for _, forbidden := range []string{"write_file", "run_shell", "delete_file", "remember"} {
		if allowed[forbidden] {
			t.Errorf("ReviewerAgent should NOT have tool: %s", forbidden)
		}
	}
}

func TestResearcherAgent_HasSearchTools(t *testing.T) {
	a := agent.NewResearcherAgent(newTestProvider(), "model")
	allowed := make(map[string]bool)
	for _, t := range a.AllowedTools() {
		allowed[t] = true
	}

	for _, required := range []string{"search_in_files", "web_search", "git_log", "recall", "remember"} {
		if !allowed[required] {
			t.Errorf("ResearcherAgent missing tool: %s", required)
		}
	}
	// 只读调研：不能写工作区，也不能执行任意命令
	for _, forbidden := range []string{"write_file", "replace_in_file", "delete_file", "run_shell", "download_file"} {
		if allowed[forbidden] {
			t.Errorf("ResearcherAgent should NOT have tool: %s", forbidden)
		}
	}
}

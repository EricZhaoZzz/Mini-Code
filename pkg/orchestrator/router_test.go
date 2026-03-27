package orchestrator_test

import (
	"testing"
	"mini-code/pkg/orchestrator"
)

func TestRouter_DefaultsToCoderAgent(t *testing.T) {
	r := orchestrator.NewRouter()
	agentType := r.Route("帮我实现一个函数")
	if agentType != "coder" {
		t.Errorf("expected coder, got %q", agentType)
	}
}

func TestRouter_ReviewKeywordsRouteToReviewer(t *testing.T) {
	r := orchestrator.NewRouter()
	cases := []string{
		"帮我审查这段代码",
		"review 一下 main.go",
		"检查代码质量",
		"这段代码有什么问题",
	}
	for _, msg := range cases {
		agentType := r.Route(msg)
		if agentType != "reviewer" {
			t.Errorf("msg %q: expected reviewer, got %q", msg, agentType)
		}
	}
}

func TestRouter_ResearchKeywordsRouteToResearcher(t *testing.T) {
	r := orchestrator.NewRouter()
	cases := []string{
		"调研一下 gRPC 和 REST 的区别",
		"搜索 Go 中如何实现连接池",
		"了解一下 Redis Sentinel 的工作原理",
	}
	for _, msg := range cases {
		agentType := r.Route(msg)
		if agentType != "researcher" {
			t.Errorf("msg %q: expected researcher, got %q", msg, agentType)
		}
	}
}
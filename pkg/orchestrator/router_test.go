package orchestrator_test

import (
	"mini-code/pkg/orchestrator"
	"testing"
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
		"了解一下 Redis Sentinel 的工作原理",
		"什么是闭包",
		"对比 REST 和 gRPC",
		"go 和 rust 哪个更适合这个项目",
	}
	for _, msg := range cases {
		agentType := r.Route(msg)
		if agentType != "researcher" {
			t.Errorf("msg %q: expected researcher, got %q", msg, agentType)
		}
	}
}

// TestRouter_WriteIntentWins 验证写入意图优先。
//
// 回归背景：旧关键词表把「搜索」「比较」当强信号，实测 8 条典型请求误判 4 条，
// 例如「实现一个搜索框的搜索功能」被判为 researcher —— 而 researcher 没有
// 写工具，任务无法完成且无任何报错提示。
//
// coder 是唯一持有全部工具的角色，误判代价最低，因此写入意图一票否决。
func TestRouter_WriteIntentWins(t *testing.T) {
	r := orchestrator.NewRouter()

	writeCases := []string{
		"实现一个搜索框的搜索功能",
		"添加搜索历史的本地缓存",
		"帮我对比一下这两个方案并修改代码",
		"给我看看代码哪里写错了",
		"重构 list_files 支持忽略规则",
		"修复 run_shell 的超时问题",
		"实现用户登录接口",
		"写一个单元测试覆盖 evictLoop",
		"优化 session 的锁粒度",
		"把 README 翻译成英文",
		"搜索 Go 中如何实现连接池", // 含「实现」，应归 coder
		"删除 build 目录里的临时文件",
		"更新 AGENTS.md 里的开发命令",
	}
	for _, msg := range writeCases {
		if got := r.Route(msg); got != "coder" {
			t.Errorf("Route(%q) = %q, 期望 coder（写入意图必须归 coder）", msg, got)
		}
	}
}

// TestRouter_UnknownInputFallsBackToCoder 无法判断时归 coder。
func TestRouter_UnknownInputFallsBackToCoder(t *testing.T) {
	r := orchestrator.NewRouter()
	for _, msg := range []string{"", "   ", "asdfghjkl", "????"} {
		if got := r.Route(msg); got != "coder" {
			t.Errorf("Route(%q) = %q, 期望 coder（兜底）", msg, got)
		}
	}
}

// TestRouter_AllRolesAreReachable 确认三个角色都仍可被路由到。
func TestRouter_AllRolesAreReachable(t *testing.T) {
	r := orchestrator.NewRouter()
	seen := map[string]bool{}

	for _, msg := range []string{
		"实现一个功能", "审查这段代码", "什么是闭包", "调研一下架构",
	} {
		seen[r.Route(msg)] = true
	}

	for _, role := range []string{"coder", "reviewer", "researcher"} {
		if !seen[role] {
			t.Errorf("角色 %s 从未被路由到，路由表可能过度偏向某一角色", role)
		}
	}
}

// TestRouter_EnglishWordBoundary 英文关键词按整词匹配。
//
// 回归背景：子串匹配让 add 命中 address、fix 命中 prefix、patch 命中
// dispatch_agent、error 命中 errorf，只读请求被误判为写入意图。
func TestRouter_EnglishWordBoundary(t *testing.T) {
	r := orchestrator.NewRouter()
	cases := map[string]string{
		"review the address parsing":                 "reviewer",
		"review how prefix matching works":           "reviewer",
		"review dispatch_agent":                      "reviewer",
		"what is the errorf convention, research it": "researcher",
		"什么是 preview 模式":                             "researcher", // preview 不应命中 review
	}
	for msg, want := range cases {
		if got := r.Route(msg); got != want {
			t.Errorf("Route(%q) = %q, 期望 %q", msg, got, want)
		}
	}
}

// TestRouter_EnglishInflections 常见词形变化仍应命中。
func TestRouter_EnglishInflections(t *testing.T) {
	r := orchestrator.NewRouter()
	coder := []string{
		"fixing the login flow", "fixed?", "added a flag but", "creating a new handler",
		"updates needed", "removing dead code", "Fix一下这里", "tests are failing",
		"debug this panic", "it crashes on start", "Bug in parser",
	}
	for _, msg := range coder {
		if got := r.Route(msg); got != "coder" {
			t.Errorf("Route(%q) = %q, 期望 coder", msg, got)
		}
	}
	for _, msg := range []string{"reviewing main.go", "please Review一下", "code review"} {
		if got := r.Route(msg); got != "reviewer" {
			t.Errorf("Route(%q) = %q, 期望 reviewer", msg, got)
		}
	}
}

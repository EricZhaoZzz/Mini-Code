package tools_test

import (
	"encoding/json"
	"testing"
	"mini-code/pkg/tools"
)

func TestDispatchAgent_RequiresRoleAndTask(t *testing.T) {
	// 缺少必填参数时应返回错误
	args, _ := json.Marshal(map[string]interface{}{
		"role": "reviewer",
		// 缺少 task
	})
	_, err := tools.Executors["dispatch_agent"](string(args))
	if err == nil {
		t.Error("expected error for missing task")
	}
}

func TestDispatchAgent_InvalidRoleReturnsError(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{
		"role": "unknown_role",
		"task": "do something",
	})
	_, err := tools.Executors["dispatch_agent"](string(args))
	if err == nil {
		t.Error("expected error for invalid role")
	}
}
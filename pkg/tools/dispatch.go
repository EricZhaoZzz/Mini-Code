package tools

import (
	"fmt"
)

// AgentFactory 创建子 Agent 的工厂函数类型
// 返回 Run 方法的响应和错误
type AgentFactory func(role string, task string) (string, error)

// dispatchFactory 通过闭包注入，由 main.go 在启动时设置
var dispatchFactory AgentFactory

// SetDispatchFactory 注入 Agent 工厂函数（由 main.go 调用）
func SetDispatchFactory(factory AgentFactory) {
	dispatchFactory = factory
}

// DispatchAgentArguments dispatch_agent 工具参数
type DispatchAgentArguments struct {
	Role string `json:"role" validate:"required" jsonschema:"required" jsonschema_description:"要调用的 Agent 角色：reviewer | researcher"`
	Task string `json:"task" validate:"required" jsonschema:"required" jsonschema_description:"交给子 Agent 的任务描述"`
}

func DispatchAgent(args interface{}) (interface{}, error) {
	var params DispatchAgentArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if dispatchFactory == nil {
		return nil, fmt.Errorf("dispatch factory 未初始化")
	}

	// 验证角色
	switch params.Role {
	case "reviewer", "researcher":
		// 有效角色
	default:
		return nil, fmt.Errorf("未知 Agent 角色 %q，支持：reviewer | researcher", params.Role)
	}

	// 调用工厂函数执行子 Agent
	reply, err := dispatchFactory(params.Role, params.Task)
	if err != nil {
		return nil, fmt.Errorf("[%s Agent] 执行失败: %w", params.Role, err)
	}
	return fmt.Sprintf("[%s Agent 结果]\n%s", params.Role, reply), nil
}
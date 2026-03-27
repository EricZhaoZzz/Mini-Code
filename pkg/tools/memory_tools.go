package tools

import (
	"fmt"
	"mini-code/pkg/memory"
	"strings"
)

var globalMemoryStore *memory.Store

// SetMemoryStore 注入 Memory Store（由 main.go 在启动时调用）
func SetMemoryStore(store *memory.Store) {
	globalMemoryStore = store
}

// RememberArguments remember 工具参数
type RememberArguments struct {
	Content string   `json:"content" validate:"required" jsonschema:"required" jsonschema_description:"要记住的事实，自然语言描述"`
	Scope   string   `json:"scope" validate:"required" jsonschema:"required" jsonschema_description:"记忆范围：project（当前项目）| user（用户偏好）| global（通用知识）"`
	Tags    []string `json:"tags" jsonschema_description:"可选标签，便于检索"`
}

// Remember 工具执行函数
func Remember(args interface{}) (interface{}, error) {
	var params RememberArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if globalMemoryStore == nil {
		return nil, fmt.Errorf("memory store 未初始化")
	}

	workspace, _ := workspaceRoot()
	id, err := globalMemoryStore.Remember(params.Content, params.Scope, workspace, params.Tags)
	if err != nil {
		return nil, fmt.Errorf("保存记忆失败: %w", err)
	}
	return fmt.Sprintf("已记住（ID: %d）: %s", id, params.Content), nil
}

// ForgetArguments forget 工具参数
type ForgetArguments struct {
	MemoryID int64 `json:"memory_id" validate:"required" jsonschema:"required" jsonschema_description:"要删除的记忆 ID（由 remember 工具返回）"`
}

// Forget 工具执行函数
func Forget(args interface{}) (interface{}, error) {
	var params ForgetArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if globalMemoryStore == nil {
		return nil, fmt.Errorf("memory store 未初始化")
	}
	if err := globalMemoryStore.Forget(params.MemoryID); err != nil {
		return nil, fmt.Errorf("删除记忆失败: %w", err)
	}
	return fmt.Sprintf("已删除记忆 ID: %d", params.MemoryID), nil
}

// RecallArguments recall 工具参数
type RecallArguments struct {
	Query string `json:"query" validate:"required" jsonschema:"required" jsonschema_description:"自然语言检索词"`
	Scope string `json:"scope" jsonschema_description:"限定检索范围：project | user | global，不填则全局搜索"`
}

// Recall 工具执行函数
func Recall(args interface{}) (interface{}, error) {
	var params RecallArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if globalMemoryStore == nil {
		return nil, fmt.Errorf("memory store 未初始化")
	}

	workspace, _ := workspaceRoot()
	results, err := globalMemoryStore.Recall(params.Query, params.Scope, workspace, 10)
	if err != nil {
		return nil, fmt.Errorf("检索记忆失败: %w", err)
	}
	if len(results) == 0 {
		return "未找到相关记忆", nil
	}

	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("[ID:%d][%s] %s\n", r.ID, r.Scope, r.Content))
	}
	return strings.TrimSpace(sb.String()), nil
}
package tools

import (
	"fmt"
	"mini-code/pkg/memory"
	"strings"
	"sync"
)

var (
	memoryMu  sync.RWMutex
	memoryCtx *memory.Context
)

// SetMemory 注入记忆上下文（由 main.go 在启动时调用）；传 nil 关闭记忆工具。
func SetMemory(ctx *memory.Context) {
	memoryMu.Lock()
	defer memoryMu.Unlock()
	memoryCtx = ctx
}

func currentMemory() *memory.Context {
	memoryMu.RLock()
	defer memoryMu.RUnlock()
	return memoryCtx
}

func autoMemory() (*memory.AutoMemory, error) {
	if m := currentMemory(); m != nil && m.Auto != nil {
		return m.Auto, nil
	}
	return nil, fmt.Errorf("记忆未初始化")
}

// withLazyInstructions 在文件工具结果末尾附上因访问 absPath 而新加载的子目录指令与条件规则。
func withLazyInstructions(absPath string, result string) string {
	m := currentMemory()
	if m == nil {
		return result
	}
	if extra := m.Lazy.OnFileAccess(absPath); extra != "" {
		return result + "\n\n" + extra
	}
	return result
}

// RememberArguments remember 工具参数
type RememberArguments struct {
	Name        string `json:"name" validate:"required" jsonschema:"required" jsonschema_description:"记忆的唯一名称，小写 kebab-case（如 prefer-pnpm）；与已有记忆同名即覆盖更新"`
	Description string `json:"description" validate:"required" jsonschema:"required" jsonschema_description:"一行摘要，写入 MEMORY.md 索引，用于以后判断相关性"`
	Type        string `json:"type" validate:"required" jsonschema:"required,enum=user,enum=feedback,enum=project,enum=reference" jsonschema_description:"user=用户信息与偏好；feedback=对你工作方式的纠正/认可；project=推导不出的项目背景与约束；reference=外部资源指针"`
	Content     string `json:"content" validate:"required" jsonschema:"required" jsonschema_description:"记忆正文；feedback/project 类型附 **Why:** 与 **How to apply:** 两行"`
}

// Remember 工具执行函数
func Remember(args interface{}) (interface{}, error) {
	var params RememberArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	auto, err := autoMemory()
	if err != nil {
		return nil, err
	}
	path, updated, err := auto.Save(params.Name, params.Description, params.Type, params.Content)
	if err != nil {
		return nil, fmt.Errorf("保存记忆失败: %w", err)
	}
	verb := "已记住"
	if updated {
		verb = "已更新记忆"
	}
	return fmt.Sprintf("%s %s（%s）", verb, params.Name, path), nil
}

// ForgetArguments forget 工具参数
type ForgetArguments struct {
	Name string `json:"name" validate:"required" jsonschema:"required" jsonschema_description:"要删除的记忆名称（MEMORY.md 索引中的 name）"`
}

// Forget 工具执行函数
func Forget(args interface{}) (interface{}, error) {
	var params ForgetArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	auto, err := autoMemory()
	if err != nil {
		return nil, err
	}
	if err := auto.Delete(params.Name); err != nil {
		return nil, fmt.Errorf("删除记忆失败: %w", err)
	}
	return fmt.Sprintf("已删除记忆 %s", params.Name), nil
}

// RecallArguments recall 工具参数
type RecallArguments struct {
	Name  string `json:"name,omitempty" jsonschema_description:"读取指定名称记忆的完整内容"`
	Query string `json:"query,omitempty" jsonschema_description:"关键词检索（空格分隔多个词）；name 与 query 都不填时列出全部记忆"`
}

// Recall 工具执行函数
func Recall(args interface{}) (interface{}, error) {
	var params RecallArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	auto, err := autoMemory()
	if err != nil {
		return nil, err
	}

	if name := strings.TrimSpace(params.Name); name != "" {
		content, err := auto.Read(name)
		if err != nil {
			return nil, err
		}
		return content, nil
	}

	entries, err := auto.Search(params.Query)
	if err != nil {
		return nil, fmt.Errorf("检索记忆失败: %w", err)
	}
	if len(entries) == 0 {
		return "未找到相关记忆", nil
	}

	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "- %s [%s] — %s\n", e.Name, e.Type, e.Description)
	}
	sb.WriteString("\n用 recall(name=...) 查看完整内容")
	return sb.String(), nil
}

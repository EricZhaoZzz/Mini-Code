package tools_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"mini-code/pkg/memory"
	"mini-code/pkg/tools"
)

func setupMemoryTools(t *testing.T) *memory.Store {
	dir := t.TempDir()
	store, err := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	tools.SetMemoryStore(store) // 注入 store 到工具层
	return store
}

func TestRememberTool_StoresAndReturnsID(t *testing.T) {
	setupMemoryTools(t)

	args, _ := json.Marshal(map[string]interface{}{
		"content": "使用 Go 1.22",
		"scope":   "project",
	})

	result, err := tools.Executors["remember"](string(args))
	if err != nil {
		t.Fatalf("remember failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestForgetTool_DeletesMemory(t *testing.T) {
	store := setupMemoryTools(t)

	id, _ := store.Remember("临时记忆", "user", "", nil)
	args, _ := json.Marshal(map[string]interface{}{"memory_id": id})

	_, err := tools.Executors["forget"](string(args))
	if err != nil {
		t.Fatalf("forget failed: %v", err)
	}

	results, _ := store.Recall("临时记忆", "", "", 10)
	if len(results) != 0 {
		t.Error("expected memory to be deleted")
	}
}

func TestRecallTool_FindsRelevantMemory(t *testing.T) {
	store := setupMemoryTools(t)
	store.Remember("项目使用 PostgreSQL 数据库", "project", ".", nil)

	args, _ := json.Marshal(map[string]interface{}{"query": "数据库"})
	result, err := tools.Executors["recall"](string(args))
	if err != nil {
		t.Fatalf("recall failed: %v", err)
	}
	// Executors 返回 interface{}，转为字符串比较
	resultStr, ok := result.(string)
	if !ok || resultStr == "" || resultStr == "未找到相关记忆" {
		t.Errorf("expected non-empty recall result, got: %v", result)
	}
}
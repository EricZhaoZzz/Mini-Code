package memory_test

import (
	"path/filepath"
	"testing"
	"mini-code/pkg/memory"
)

func TestStore_OpenCreatesTablesAutomatically(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "memory.db")
	projectPath := filepath.Join(dir, "project.db")

	store, err := memory.Open(globalPath, projectPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// 验证表存在：写入一条 session_memory 不报错
	err = store.SaveSessionMemory("test-workspace", "test content", 72)
	if err != nil {
		t.Fatalf("SaveSessionMemory failed: %v", err)
	}
}

func TestStore_RememberAndRecall(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	id, err := store.Remember("这个项目使用 pnpm 而不是 npm", "project", dir, []string{"tooling"})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	results, err := store.Recall("pnpm", "project", dir, 10)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one recall result")
	}
	if results[0].Content != "这个项目使用 pnpm 而不是 npm" {
		t.Errorf("unexpected content: %q", results[0].Content)
	}
}

func TestStore_Forget(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	id, _ := store.Remember("临时记忆", "user", "", nil)
	err = store.Forget(id)
	if err != nil {
		t.Fatalf("Forget failed: %v", err)
	}

	results, _ := store.Recall("临时记忆", "", "", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results after forget, got %d", len(results))
	}
}
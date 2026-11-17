package tools_test

import (
	"context"
	"encoding/json"
	"mini-code/pkg/memory"
	"mini-code/pkg/tools"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupMemoryTools(t *testing.T) *memory.Context {
	t.Helper()
	mem, err := memory.NewContext(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	tools.SetMemory(mem)
	t.Cleanup(func() { tools.SetMemory(nil) })
	return mem
}

func runTool(t *testing.T, name string, args map[string]interface{}) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	result, err := tools.Executors[name](context.Background(), string(raw))
	s, _ := result.(string)
	return s, err
}

func TestRememberTool_WritesFileAndIndex(t *testing.T) {
	mem := setupMemoryTools(t)

	_, err := runTool(t, "remember", map[string]interface{}{
		"name": "go-version", "description": "项目使用 Go 1.22", "type": "project", "content": "使用 Go 1.22",
	})
	if err != nil {
		t.Fatalf("remember failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mem.Auto.Dir(), "go-version.md")); err != nil {
		t.Fatalf("memory file missing: %v", err)
	}
	if !strings.Contains(mem.Auto.Index(), "[go-version](go-version.md)") {
		t.Errorf("index missing entry: %q", mem.Auto.Index())
	}
}

func TestRememberTool_RejectsInvalidType(t *testing.T) {
	setupMemoryTools(t)
	_, err := runTool(t, "remember", map[string]interface{}{
		"name": "x", "description": "d", "type": "global", "content": "c",
	})
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestForgetTool_DeletesMemory(t *testing.T) {
	mem := setupMemoryTools(t)
	mem.Auto.Save("temp-note", "临时记忆", "user", "临时记忆")

	if _, err := runTool(t, "forget", map[string]interface{}{"name": "temp-note"}); err != nil {
		t.Fatalf("forget failed: %v", err)
	}
	if entries, _ := mem.Auto.List(); len(entries) != 0 {
		t.Errorf("expected memory deleted, got %d", len(entries))
	}
	if strings.Contains(mem.Auto.Index(), "temp-note") {
		t.Error("index still contains deleted entry")
	}
}

func TestRecallTool_SearchAndRead(t *testing.T) {
	mem := setupMemoryTools(t)
	mem.Auto.Save("db-choice", "数据库选型", "project", "项目使用 PostgreSQL 数据库")

	out, err := runTool(t, "recall", map[string]interface{}{"query": "postgresql"})
	if err != nil || !strings.Contains(out, "db-choice") {
		t.Fatalf("search: out=%q err=%v", out, err)
	}

	out, err = runTool(t, "recall", map[string]interface{}{"name": "db-choice"})
	if err != nil || !strings.Contains(out, "PostgreSQL") {
		t.Fatalf("read: out=%q err=%v", out, err)
	}
}

func TestReadFile_InjectsSubdirInstructionsOnce(t *testing.T) {
	root := t.TempDir()
	setWorkspaceRootForTest(t, root)
	mem, err := memory.NewContext(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	tools.SetMemory(mem)
	t.Cleanup(func() { tools.SetMemory(nil) })

	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "MINICODE.md"), []byte("子目录规则：禁止 panic"), 0o644)
	os.WriteFile(filepath.Join(root, "sub", "a.go"), []byte("package sub\n"), 0o644)

	first, err := runTool(t, "read_file", map[string]interface{}{"path": "sub/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "禁止 panic") {
		t.Errorf("expected subdir instructions injected, got %q", first)
	}
	second, _ := runTool(t, "read_file", map[string]interface{}{"path": "sub/a.go"})
	if strings.Contains(second, "禁止 panic") {
		t.Error("instructions should be injected only once per session")
	}
}

func setWorkspaceRootForTest(t *testing.T, dir string) {
	t.Helper()
	original, origErr := tools.CurrentWorkspaceRoot()
	if err := tools.SetWorkspaceRoot(dir); err != nil {
		t.Fatalf("SetWorkspaceRoot: %v", err)
	}
	t.Cleanup(func() {
		if origErr == nil {
			_ = tools.SetWorkspaceRoot(original)
		}
	})
}

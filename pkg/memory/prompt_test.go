package memory_test

import (
	"path/filepath"
	"strings"
	"testing"
	"mini-code/pkg/memory"
)

func TestBuildPromptSuffix_ContainsRememberedFacts(t *testing.T) {
	dir := t.TempDir()
	store, _ := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	defer store.Close()

	store.Remember("使用 pnpm 而不是 npm", "project", dir, nil)
	store.Remember("回复语言：中文", "user", "", nil)
	store.SaveSessionMemory(dir, "昨天完成了认证模块", 72)

	suffix := store.BuildPromptSuffix(dir)

	if !strings.Contains(suffix, "pnpm") {
		t.Error("expected project memory in suffix")
	}
	if !strings.Contains(suffix, "中文") {
		t.Error("expected user preference in suffix")
	}
	if !strings.Contains(suffix, "认证模块") {
		t.Error("expected session memory in suffix")
	}
	if len(suffix) > 2000 {
		t.Errorf("suffix too long: %d chars (max 2000)", len(suffix))
	}
}
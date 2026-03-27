package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceInFileReplacesOnlyFirstMatch(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	path := filepath.Join(workspace, "sample.txt")
	original := "hello x\nhello x\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "sample.txt",
		Old:  "x",
		New:  "y",
	})
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	gotResult := result.(string)
	if gotResult != "已替换文件内容: sample.txt" {
		t.Fatalf("unexpected replace result: %q", gotResult)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	got := string(data)
	want := "hello y\nhello x\n"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReplaceInFileFailsWhenOldNotFound(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	path := filepath.Join(workspace, "sample.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "sample.txt",
		Old:  "missing",
		New:  "new",
	})
	if err == nil {
		t.Fatal("expected replace to fail when old text is missing")
	}

	if !strings.Contains(err.Error(), "未找到要替换的内容") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReplaceInFileFailsWhenOldIsEmpty(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	path := filepath.Join(workspace, "sample.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "sample.txt",
		Old:  "",
		New:  "new",
	})
	if err == nil {
		t.Fatal("expected replace to fail when old is empty")
	}

	if !strings.Contains(err.Error(), "old 不能为空") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReplaceInFileBlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	_, err := ReplaceInFile(ReplaceInFileArguments{
		Path: filepath.Join("..", "escape.txt"),
		Old:  "a",
		New:  "b",
	})
	if err == nil {
		t.Fatal("expected replace outside workspace to fail")
	}

	if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	return func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore chdir failed: %v", err)
		}
	}
}

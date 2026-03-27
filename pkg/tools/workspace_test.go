package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkspacePathAllowsPathInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	got, err := resolveWorkspacePath(filepath.Join("notes", "todo.txt"))
	if err != nil {
		t.Fatalf("expected inside path to succeed, got error: %v", err)
	}

	want := filepath.Join(workspace, "notes", "todo.txt")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveWorkspacePathBlocksParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	_, err := resolveWorkspacePath(filepath.Join("..", "escape.txt"))
	if err == nil {
		t.Fatal("expected parent traversal to be blocked")
	}

	if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteFileBlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	result, err := WriteFile(WriteFileArguments{
		Path:    filepath.Join("docs", "guide.txt"),
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("expected write inside workspace to succeed, got error: %v", err)
	}

	gotResult := result.(string)
	expectedResult := "文件已写入: " + filepath.Join("docs", "guide.txt")
	if gotResult != expectedResult {
		t.Fatalf("expected result %q, got %q", expectedResult, gotResult)
	}

	data, err := os.ReadFile(filepath.Join(workspace, "docs", "guide.txt"))
	if err != nil {
		t.Fatalf("expected file to be created, got error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected file content to be hello, got %q", string(data))
	}

	_, err = WriteFile(WriteFileArguments{
		Path:    filepath.Join("..", "escape.txt"),
		Content: "blocked",
	})
	if err == nil {
		t.Fatal("expected write outside workspace to fail")
	}

	if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListAndSearchUseWorkspaceRelativePaths(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	if err := os.MkdirAll(filepath.Join(workspace, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "docs", "readme.txt"), []byte("alpha\nbeta keyword"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	listResult, err := ListFiles(ListFilesArguments{Path: "."})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	listText := listResult.(string)
	expectedPath := filepath.Join("docs", "readme.txt")
	if !strings.Contains(listText, expectedPath) {
		t.Fatalf("expected relative path in list result, got %q", listText)
	}
	if strings.Contains(listText, workspace) {
		t.Fatalf("expected list result to hide absolute workspace path, got %q", listText)
	}

	searchResult, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "keyword"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	searchText := searchResult.(string)
	if !strings.Contains(searchText, expectedPath+":2: beta keyword") {
		t.Fatalf("expected relative search result, got %q", searchText)
	}
	if strings.Contains(searchText, workspace) {
		t.Fatalf("expected search result to hide absolute workspace path, got %q", searchText)
	}
}

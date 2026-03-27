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

func TestWriteFileCreatesDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试写入到不存在的子目录
	result, err := WriteFile(WriteFileArguments{
		Path:    filepath.Join("deep", "nested", "dir", "file.txt"),
		Content: "nested content",
	})
	if err != nil {
		t.Fatalf("expected write to nested directory to succeed, got error: %v", err)
	}

	gotResult := result.(string)
	expectedResult := "文件已写入: " + filepath.Join("deep", "nested", "dir", "file.txt")
	if gotResult != expectedResult {
		t.Fatalf("expected result %q, got %q", expectedResult, gotResult)
	}

	// 验证文件内容和目录创建
	data, err := os.ReadFile(filepath.Join(workspace, "deep", "nested", "dir", "file.txt"))
	if err != nil {
		t.Fatalf("expected file to be created, got error: %v", err)
	}
	if string(data) != "nested content" {
		t.Fatalf("expected file content to be 'nested content', got %q", string(data))
	}
}

func TestWriteFileOverwritesExisting(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 先创建一个文件
	testFile := filepath.Join(workspace, "existing.txt")
	if err := os.WriteFile(testFile, []byte("original content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// 覆盖写入
	result, err := WriteFile(WriteFileArguments{
		Path:    "existing.txt",
		Content: "new content",
	})
	if err != nil {
		t.Fatalf("expected overwrite to succeed, got error: %v", err)
	}

	gotResult := result.(string)
	expectedResult := "文件已写入: existing.txt"
	if gotResult != expectedResult {
		t.Fatalf("expected result %q, got %q", expectedResult, gotResult)
	}

	// 验证内容已被覆盖
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("expected file to exist, got error: %v", err)
	}
	if string(data) != "new content" {
		t.Fatalf("expected file content to be 'new content', got %q", string(data))
	}
}

func TestWriteFileWithEmptyContent(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试写入空内容
	result, err := WriteFile(WriteFileArguments{
		Path:    "empty.txt",
		Content: "",
	})
	if err != nil {
		t.Fatalf("expected write empty file to succeed, got error: %v", err)
	}

	gotResult := result.(string)
	expectedResult := "文件已写入: empty.txt"
	if gotResult != expectedResult {
		t.Fatalf("expected result %q, got %q", expectedResult, gotResult)
	}

	// 验证文件已创建（即使是空文件）
	data, err := os.ReadFile(filepath.Join(workspace, "empty.txt"))
	if err != nil {
		t.Fatalf("expected file to be created, got error: %v", err)
	}
	if string(data) != "" {
		t.Fatalf("expected empty file, got %q", string(data))
	}
}

func TestWriteFileWithJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试使用 JSON 字符串作为参数
	jsonArgs := `{"path": "json_test.txt", "content": "from json args"}`
	result, err := WriteFile(jsonArgs)
	if err != nil {
		t.Fatalf("expected write with json args to succeed, got error: %v", err)
	}

	gotResult := result.(string)
	expectedResult := "文件已写入: json_test.txt"
	if gotResult != expectedResult {
		t.Fatalf("expected result %q, got %q", expectedResult, gotResult)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, "json_test.txt"))
	if err != nil {
		t.Fatalf("expected file to be created, got error: %v", err)
	}
	if string(data) != "from json args" {
		t.Fatalf("expected file content to be 'from json args', got %q", string(data))
	}
}

func TestWriteFileWithSpecialContent(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	tests := []struct {
		name    string
		path    string
		content string
	}{
		{"multiline", "multi.txt", "line1\nline2\nline3"},
		{"unicode", "unicode.txt", "你好世界 🌍\nこんにちは"},
		{"tabs", "tabs.txt", "col1\tcol2\tcol3"},
		{"json_content", "data.json", `{"key": "value", "nested": {"a": 1}}`},
		{"code", "code.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := WriteFile(WriteFileArguments{
				Path:    tt.path,
				Content: tt.content,
			})
			if err != nil {
				t.Fatalf("expected write to succeed, got error: %v", err)
			}

			// 验证内容
			data, err := os.ReadFile(filepath.Join(workspace, tt.path))
			if err != nil {
				t.Fatalf("expected file to be created, got error: %v", err)
			}
			if string(data) != tt.content {
				t.Fatalf("content mismatch: expected %q, got %q", tt.content, string(data))
			}

			_ = result
		})
	}
}

func TestWriteFileWithInvalidJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试无效 JSON 参数
	_, err := WriteFile("invalid json")
	if err == nil {
		t.Fatal("expected error for invalid json args")
	}
}

func TestWriteFileRootPath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试在根目录写入文件
	result, err := WriteFile(WriteFileArguments{
		Path:    "root.txt",
		Content: "root file",
	})
	if err != nil {
		t.Fatalf("expected write at root to succeed, got error: %v", err)
	}

	gotResult := result.(string)
	expectedResult := "文件已写入: root.txt"
	if gotResult != expectedResult {
		t.Fatalf("expected result %q, got %q", expectedResult, gotResult)
	}

	// 验证文件
	data, err := os.ReadFile(filepath.Join(workspace, "root.txt"))
	if err != nil {
		t.Fatalf("expected file to be created, got error: %v", err)
	}
	if string(data) != "root file" {
		t.Fatalf("expected file content to be 'root file', got %q", string(data))
	}
}

func TestWriteFileCurrentDir(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试使用 "." 路径前缀
	result, err := WriteFile(WriteFileArguments{
		Path:    filepath.Join(".", "dot_test.txt"),
		Content: "dot path test",
	})
	if err != nil {
		t.Fatalf("expected write with dot path to succeed, got error: %v", err)
	}

	// 验证文件
	data, err := os.ReadFile(filepath.Join(workspace, "dot_test.txt"))
	if err != nil {
		t.Fatalf("expected file to be created, got error: %v", err)
	}
	if string(data) != "dot path test" {
		t.Fatalf("content mismatch, got %q", string(data))
	}

	_ = result
}

// ================== ListFiles Tests ==================

func TestListFilesBlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 尝试列出工作区外的目录
	_, err := ListFiles(ListFilesArguments{Path: ".."})
	if err == nil {
		t.Fatal("expected list outside workspace to fail")
	}
	if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("expected path outside workspace error, got: %v", err)
	}
}

func TestListFilesEmptyDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 列出空目录
	result, err := ListFiles(ListFilesArguments{Path: "."})
	if err != nil {
		t.Fatalf("expected list empty dir to succeed, got error: %v", err)
	}

	gotResult := result.(string)
	if gotResult != "未找到任何文件" {
		t.Fatalf("expected '未找到任何文件', got: %q", gotResult)
	}
}

func TestListFilesNestedDirectories(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录结构
	dirs := []string{
		"src",
		filepath.Join("src", "pkg"),
		filepath.Join("src", "pkg", "utils"),
		"docs",
		"tests",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(workspace, dir), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}

	// 创建多个文件
	files := []string{
		"root.txt",
		filepath.Join("src", "main.go"),
		filepath.Join("src", "pkg", "utils", "helper.go"),
		filepath.Join("docs", "readme.md"),
		filepath.Join("tests", "test.go"),
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(workspace, file), []byte("content"), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	// 列出所有文件
	result, err := ListFiles(ListFilesArguments{Path: "."})
	if err != nil {
		t.Fatalf("expected list to succeed, got error: %v", err)
	}

	listText := result.(string)

	// 验证所有文件都在结果中（使用 filepath.Join 来处理平台差异）
	for _, file := range files {
		if !strings.Contains(listText, file) {
			t.Fatalf("expected file %q in list result, got: %s", file, listText)
		}
	}

	// 验证不包含绝对路径
	if strings.Contains(listText, workspace) {
		t.Fatalf("list result should not contain absolute workspace path, got: %s", listText)
	}
}

func TestListFilesSkipsGitAndNodeModules(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建 .git 和 node_modules 目录
	dirs := []string{
		".git",
		filepath.Join(".git", "objects"),
		"node_modules",
		filepath.Join("node_modules", "package"),
		"src",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(workspace, dir), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}

	// 在各目录创建文件
	files := []string{
		filepath.Join(".git", "config"),
		filepath.Join(".git", "objects", "abc"),
		filepath.Join("node_modules", "package", "index.js"),
		filepath.Join("src", "main.go"),
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(workspace, file), []byte("content"), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	result, err := ListFiles(ListFilesArguments{Path: "."})
	if err != nil {
		t.Fatalf("expected list to succeed, got error: %v", err)
	}

	listText := result.(string)

	// 验证 .git 和 node_modules 中的文件不在结果中
	if strings.Contains(listText, ".git") {
		t.Fatalf("list result should not contain .git files, got: %s", listText)
	}
	if strings.Contains(listText, "node_modules") {
		t.Fatalf("list result should not contain node_modules files, got: %s", listText)
	}

	// 验证其他文件在结果中（使用 filepath.Join 处理平台差异）
	expectedFile := filepath.Join("src", "main.go")
	if !strings.Contains(listText, expectedFile) {
		t.Fatalf("expected %q in list result, got: %s", expectedFile, listText)
	}
}

func TestListFilesSubdirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建目录结构
	if err := os.MkdirAll(filepath.Join(workspace, "src", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	files := []string{
		"root.txt",
		"src/main.go",
		"src/pkg/helper.go",
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(workspace, file), []byte("content"), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	// 只列出 src 目录
	result, err := ListFiles(ListFilesArguments{Path: "src"})
	if err != nil {
		t.Fatalf("expected list src to succeed, got error: %v", err)
	}

	listText := result.(string)

	// 验证 src 目录下的文件在结果中
	if !strings.Contains(listText, "src/main.go") && !strings.Contains(listText, "main.go") {
		t.Fatalf("expected main.go in list result, got: %s", listText)
	}

	// 验证根目录文件不在结果中
	if strings.Contains(listText, "root.txt") {
		t.Fatalf("list result should not contain root.txt, got: %s", listText)
	}
}

func TestListFilesWithJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// 使用 JSON 字符串作为参数
	jsonArgs := `{"path": "."}`
	result, err := ListFiles(jsonArgs)
	if err != nil {
		t.Fatalf("expected list with json args to succeed, got error: %v", err)
	}

	listText := result.(string)
	if !strings.Contains(listText, "test.txt") {
		t.Fatalf("expected test.txt in list result, got: %s", listText)
	}
}

func TestListFilesInvalidJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试无效 JSON 参数
	_, err := ListFiles("invalid json")
	if err == nil {
		t.Fatal("expected error for invalid json args")
	}
}

func TestListFilesNonExistentPath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 列出不存在的目录
	_, err := ListFiles(ListFilesArguments{Path: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

// ================== ListAndSearchUseWorkspaceRelativePaths ==================

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

// ================== SearchInFiles Tests ==================

func TestSearchInFilesBlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 尝试搜索工作区外的目录
	_, err := SearchInFiles(SearchInFilesArguments{Path: "..", Query: "test"})
	if err == nil {
		t.Fatal("expected search outside workspace to fail")
	}
	if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("expected path outside workspace error, got: %v", err)
	}
}

func TestSearchInFilesFindsMatch(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("hello world\nfoo bar\nhello again"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// 搜索匹配内容
	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "hello"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证找到两处匹配
	if !strings.Contains(searchText, "test.txt:1: hello world") {
		t.Fatalf("expected first match, got: %s", searchText)
	}
	if !strings.Contains(searchText, "test.txt:3: hello again") {
		t.Fatalf("expected second match, got: %s", searchText)
	}
}

func TestSearchInFilesNoMatch(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// 搜索不存在的内容
	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "notfound"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)
	if searchText != "未找到匹配内容" {
		t.Fatalf("expected '未找到匹配内容', got: %q", searchText)
	}
}

func TestSearchInFilesEmptyQuery(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// 空查询应该返回错误
	_, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query 不能为空") {
		t.Fatalf("expected empty query error, got: %v", err)
	}

	// 空白查询也应该返回错误
	_, err = SearchInFiles(SearchInFilesArguments{Path: ".", Query: "   "})
	if err == nil {
		t.Fatal("expected error for whitespace query")
	}
}

func TestSearchInFilesSubdirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建目录结构
	if err := os.MkdirAll(filepath.Join(workspace, "src", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// 创建多个文件
	files := map[string]string{
		"root.txt":           "findme in root",
		filepath.Join("src", "main.go"):        "findme in src",
		filepath.Join("src", "pkg", "helper.go"): "findme in pkg",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	// 只搜索 src 目录
	result, err := SearchInFiles(SearchInFilesArguments{Path: "src", Query: "findme"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证找到 src 目录下的文件
	if !strings.Contains(searchText, "src") {
		t.Fatalf("expected src files in result, got: %s", searchText)
	}

	// 验证不包含根目录文件
	if strings.Contains(searchText, "root.txt") {
		t.Fatalf("search result should not contain root.txt, got: %s", searchText)
	}
}

func TestSearchInFilesSkipsGitAndNodeModules(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建目录结构
	dirs := []string{
		".git",
		filepath.Join(".git", "objects"),
		"node_modules",
		filepath.Join("node_modules", "package"),
		"src",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(workspace, dir), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}

	// 在各目录创建包含搜索关键词的文件
	files := map[string]string{
		filepath.Join(".git", "config"):               "findme in git",
		filepath.Join(".git", "objects", "abc"):        "findme in git objects",
		filepath.Join("node_modules", "package", "index.js"): "findme in node_modules",
		filepath.Join("src", "main.go"):               "findme in src",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	// 搜索
	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "findme"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证 .git 和 node_modules 中的内容不在结果中
	if strings.Contains(searchText, ".git") {
		t.Fatalf("search result should not contain .git files, got: %s", searchText)
	}
	if strings.Contains(searchText, "node_modules") {
		t.Fatalf("search result should not contain node_modules files, got: %s", searchText)
	}

	// 验证 src 文件在结果中
	expectedFile := filepath.Join("src", "main.go")
	if !strings.Contains(searchText, expectedFile) {
		t.Fatalf("expected %q in search result, got: %s", expectedFile, searchText)
	}
}

func TestSearchInFilesReturnsRelativePath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录和文件
	if err := os.MkdirAll(filepath.Join(workspace, "deep", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "deep", "nested", "file.txt"), []byte("searchme"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "searchme"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证结果使用相对路径
	expectedPath := filepath.Join("deep", "nested", "file.txt")
	if !strings.Contains(searchText, expectedPath) {
		t.Fatalf("expected relative path %q in result, got: %s", expectedPath, searchText)
	}

	// 验证不包含绝对路径
	if strings.Contains(searchText, workspace) {
		t.Fatalf("search result should not contain absolute workspace path, got: %s", searchText)
	}
}

func TestSearchInFilesMultipleFiles(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建多个文件
	files := map[string]string{
		"file1.txt": "keyword in file1",
		"file2.txt": "no match here",
		"file3.txt": "keyword in file3",
		"file4.go":  "keyword in file4",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "keyword"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证找到 file1, file3, file4
	if !strings.Contains(searchText, "file1.txt") {
		t.Fatalf("expected file1.txt in result, got: %s", searchText)
	}
	if !strings.Contains(searchText, "file3.txt") {
		t.Fatalf("expected file3.txt in result, got: %s", searchText)
	}
	if !strings.Contains(searchText, "file4.go") {
		t.Fatalf("expected file4.go in result, got: %s", searchText)
	}

	// 验证 file2 不在结果中
	if strings.Contains(searchText, "file2.txt") {
		t.Fatalf("search result should not contain file2.txt, got: %s", searchText)
	}
}

func TestSearchInFilesWithJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("json keyword test"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// 使用 JSON 字符串作为参数
	jsonArgs := `{"path": ".", "query": "keyword"}`
	result, err := SearchInFiles(jsonArgs)
	if err != nil {
		t.Fatalf("expected search with json args to succeed, got error: %v", err)
	}

	searchText := result.(string)
	if !strings.Contains(searchText, "test.txt") {
		t.Fatalf("expected test.txt in search result, got: %s", searchText)
	}
}

func TestSearchInFilesInvalidJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试无效 JSON 参数
	_, err := SearchInFiles("invalid json")
	if err == nil {
		t.Fatal("expected error for invalid json args")
	}
}

func TestSearchInFilesNonExistentPath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 搜索不存在的目录
	_, err := SearchInFiles(SearchInFilesArguments{Path: "nonexistent", Query: "test"})
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestSearchInFilesSkipsBinaryFilesByExtension(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建一个模拟的二进制文件（使用扩展名）
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 'k', 'e', 'y', 'w', 'o', 'r', 'd', 0xFF, 0xFE}
	if err := os.WriteFile(filepath.Join(workspace, "image.png"), binaryContent, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// 创建文本文件
	if err := os.WriteFile(filepath.Join(workspace, "text.txt"), []byte("keyword in text"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "keyword"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证文本文件在结果中
	if !strings.Contains(searchText, "text.txt") {
		t.Fatalf("expected text.txt in search result, got: %s", searchText)
	}

	// 验证二进制文件不在结果中（根据扩展名跳过）
	if strings.Contains(searchText, "image.png") {
		t.Fatalf("search result should not skip .png files based on extension, got: %s", searchText)
	}
}

func TestSearchInFilesShowsLineNumber(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建多行文件
	content := "line 1\nline 2 with keyword\nline 3\nline 4 with keyword again\nline 5"
	if err := os.WriteFile(filepath.Join(workspace, "multi.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "keyword"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证显示正确的行号
	if !strings.Contains(searchText, "multi.txt:2: line 2 with keyword") {
		t.Fatalf("expected line 2 match, got: %s", searchText)
	}
	if !strings.Contains(searchText, "multi.txt:4: line 4 with keyword again") {
		t.Fatalf("expected line 4 match, got: %s", searchText)
	}
}

func TestSearchInFilesUnicodeContent(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建包含 Unicode 的文件
	content := "你好世界\n这是关键词测试\nこんにちは世界"
	if err := os.WriteFile(filepath.Join(workspace, "unicode.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "关键词"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)
	if !strings.Contains(searchText, "unicode.txt:2: 这是关键词测试") {
		t.Fatalf("expected unicode match, got: %s", searchText)
	}
}

// ================== Parent Traversal (../) Path Tests ==================

// TestParentTraversalPathsBlocked 测试所有带 ../ 的路径都会失败
func TestParentTraversalPathsBlocked(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 各种形式的路径遍历攻击
	traversalPaths := []struct {
		name string
		path string
	}{
		// 基本的父目录遍历
		{"single_parent", ".."},
		{"parent_with_file", filepath.Join("..", "escape.txt")},
		{"double_parent", filepath.Join("..", "..")},
		{"double_parent_with_file", filepath.Join("..", "..", "escape.txt")},
		{"triple_parent", filepath.Join("..", "..", "..")},

		// 混合路径遍历
		{"down_then_up", filepath.Join("subdir", "..", "..", "escape.txt")},
		{"up_down_up", filepath.Join("..", "subdir", "..", "..", "escape.txt")},
		{"complex_traversal", filepath.Join("a", "b", "..", "..", "..", "escape.txt")},

		// 尝试伪装的路径遍历
		{"hidden_parent", filepath.Join(".", "..", "escape.txt")},

		// Windows 特有的路径遍历
		{"windows_parent", "..\\escape.txt"},
		{"windows_double_parent", "..\\..\\escape.txt"},
	}

	for _, tt := range traversalPaths {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveWorkspacePath(tt.path)
			if err == nil {
				t.Errorf("path %q should be blocked", tt.path)
			} else if !strings.Contains(err.Error(), "路径超出工作区") {
				t.Errorf("expected '路径超出工作区' error for path %q, got: %v", tt.path, err)
			}
		})
	}
}

// TestWriteFileBlocksParentTraversal 测试 WriteFile 阻止所有带 ../ 的路径
func TestWriteFileBlocksParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	traversalPaths := []struct {
		name string
		path string
	}{
		{"single_parent", filepath.Join("..", "escape.txt")},
		{"double_parent", filepath.Join("..", "..", "escape.txt")},
		{"deep_traversal", filepath.Join("..", "..", "..", "..", "escape.txt")},
		{"mixed_traversal", filepath.Join("subdir", "..", "..", "escape.txt")},
		{"hidden_traversal", filepath.Join(".", "..", "escape.txt")},
	}

	for _, tt := range traversalPaths {
		t.Run(tt.name, func(t *testing.T) {
			_, err := WriteFile(WriteFileArguments{
				Path:    tt.path,
				Content: "malicious content",
			})
			if err == nil {
				t.Errorf("WriteFile should block path: %q", tt.path)
			} else if !strings.Contains(err.Error(), "路径超出工作区") {
				t.Errorf("expected '路径超出工作区' error for path %q, got: %v", tt.path, err)
			}
		})
	}
}

// TestReadFileBlocksParentTraversal 测试 ReadFile 阻止所有带 ../ 的路径
func TestReadFileBlocksParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	traversalPaths := []struct {
		name string
		path string
	}{
		{"single_parent", filepath.Join("..", "escape.txt")},
		{"double_parent", filepath.Join("..", "..", "escape.txt")},
		{"deep_traversal", filepath.Join("..", "..", "..", "escape.txt")},
		{"mixed_traversal", filepath.Join("subdir", "..", "..", "escape.txt")},
	}

	for _, tt := range traversalPaths {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadFile(ReadFileArguments{Path: tt.path})
			if err == nil {
				t.Errorf("ReadFile should block path: %q", tt.path)
			} else if !strings.Contains(err.Error(), "路径超出工作区") {
				t.Errorf("expected '路径超出工作区' error for path %q, got: %v", tt.path, err)
			}
		})
	}
}

// TestListFilesBlocksParentTraversal 测试 ListFiles 阻止所有带 ../ 的路径
func TestListFilesBlocksParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	traversalPaths := []struct {
		name string
		path string
	}{
		{"single_parent", ".."},
		{"parent_with_path", filepath.Join("..", "subdir")},
		{"double_parent", filepath.Join("..", "..")},
		{"deep_traversal", filepath.Join("..", "..", "..")},
		{"mixed_traversal", filepath.Join("subdir", "..", "..")},
	}

	for _, tt := range traversalPaths {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ListFiles(ListFilesArguments{Path: tt.path})
			if err == nil {
				t.Errorf("ListFiles should block path: %q", tt.path)
			} else if !strings.Contains(err.Error(), "路径超出工作区") {
				t.Errorf("expected '路径超出工作区' error for path %q, got: %v", tt.path, err)
			}
		})
	}
}

// TestSearchInFilesBlocksParentTraversal 测试 SearchInFiles 阻止所有带 ../ 的路径
func TestSearchInFilesBlocksParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	traversalPaths := []struct {
		name string
		path string
	}{
		{"single_parent", ".."},
		{"parent_with_path", filepath.Join("..", "subdir")},
		{"double_parent", filepath.Join("..", "..")},
		{"deep_traversal", filepath.Join("..", "..", "..")},
		{"mixed_traversal", filepath.Join("subdir", "..", "..")},
	}

	for _, tt := range traversalPaths {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SearchInFiles(SearchInFilesArguments{Path: tt.path, Query: "test"})
			if err == nil {
				t.Errorf("SearchInFiles should block path: %q", tt.path)
			} else if !strings.Contains(err.Error(), "路径超出工作区") {
				t.Errorf("expected '路径超出工作区' error for path %q, got: %v", tt.path, err)
			}
		})
	}
}

// TestReplaceInFileBlocksParentTraversal 测试 ReplaceInFile 阻止所有带 ../ 的路径
func TestReplaceInFileBlocksParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	traversalPaths := []struct {
		name string
		path string
	}{
		{"single_parent", filepath.Join("..", "escape.txt")},
		{"double_parent", filepath.Join("..", "..", "escape.txt")},
		{"deep_traversal", filepath.Join("..", "..", "..", "escape.txt")},
		{"mixed_traversal", filepath.Join("subdir", "..", "..", "escape.txt")},
		{"hidden_traversal", filepath.Join(".", "..", "escape.txt")},
	}

	for _, tt := range traversalPaths {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReplaceInFile(ReplaceInFileArguments{
				Path: tt.path,
				Old:  "old",
				New:  "new",
			})
			if err == nil {
				t.Errorf("ReplaceInFile should block path: %q", tt.path)
			} else if !strings.Contains(err.Error(), "路径超出工作区") {
				t.Errorf("expected '路径超出工作区' error for path %q, got: %v", tt.path, err)
			}
		})
	}
}

// TestValidPathsWithDots 测试包含点号但不构成路径遍历的合法路径
func TestValidPathsWithDots(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试目录和文件
	if err := os.MkdirAll(filepath.Join(workspace, "normal.dir"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "normal.dir", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "..file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "file..txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	validPaths := []struct {
		name string
		path string
	}{
		{"dot_dir", filepath.Join("normal.dir", "file.txt")},
		{"dot_prefix", "..file.txt"},
		{"dot_suffix", "file..txt"},
		{"current_dir", "."},
		{"current_dir_file", filepath.Join(".", "file.txt")},
	}

	for _, tt := range validPaths {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveWorkspacePath(tt.path)
			if err != nil {
				t.Errorf("path %q should be valid, got error: %v", tt.path, err)
			}
		})
	}
}

// TestSymlinkEscape 测试符号链接不能逃出工作区
func TestSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 在工作区外创建一个文件
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside content"), 0o644); err != nil {
		t.Fatalf("write outside file failed: %v", err)
	}

	// 在工作区内创建一个指向外部的符号链接
	symlinkPath := filepath.Join(workspace, "escape_link")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		// 在某些系统上可能没有权限创建符号链接
		t.Skipf("cannot create symlink: %v", err)
	}

	// 尝试通过符号链接读取/写入文件应该被阻止
	_, err := ReadFile(ReadFileArguments{Path: "escape_link"})
	if err == nil {
		t.Error("ReadFile should block symlink pointing outside workspace")
	} else if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Logf("ReadFile error (expected): %v", err)
	}

	_, err = WriteFile(WriteFileArguments{Path: "escape_link", Content: "malicious"})
	if err == nil {
		t.Error("WriteFile should block symlink pointing outside workspace")
	} else if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Logf("WriteFile error (expected): %v", err)
	}
}

// TestAllToolsBlockParentTraversal 综合测试所有工具都阻止父目录遍历
func TestAllToolsBlockParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 统一测试路径
	maliciousPath := filepath.Join("..", "malicious.txt")

	// WriteFile
	_, err := WriteFile(WriteFileArguments{Path: maliciousPath, Content: "test"})
	if err == nil || !strings.Contains(err.Error(), "路径超出工作区") {
		t.Error("WriteFile should block parent traversal")
	}

	// ReadFile
	_, err = ReadFile(ReadFileArguments{Path: maliciousPath})
	if err == nil || !strings.Contains(err.Error(), "路径超出工作区") {
		t.Error("ReadFile should block parent traversal")
	}

	// ListFiles
	_, err = ListFiles(ListFilesArguments{Path: ".."})
	if err == nil || !strings.Contains(err.Error(), "路径超出工作区") {
		t.Error("ListFiles should block parent traversal")
	}

	// SearchInFiles
	_, err = SearchInFiles(SearchInFilesArguments{Path: "..", Query: "test"})
	if err == nil || !strings.Contains(err.Error(), "路径超出工作区") {
		t.Error("SearchInFiles should block parent traversal")
	}

	// ReplaceInFile
	_, err = ReplaceInFile(ReplaceInFileArguments{Path: maliciousPath, Old: "a", New: "b"})
	if err == nil || !strings.Contains(err.Error(), "路径超出工作区") {
		t.Error("ReplaceInFile should block parent traversal")
	}
}

// ================== All Tools Return Relative Paths Tests ==================

// TestWriteFileReturnsRelativePath 测试 WriteFile 返回相对路径
func TestWriteFileReturnsRelativePath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录文件
	nestedPath := filepath.Join("deep", "nested", "dir", "file.txt")
	result, err := WriteFile(WriteFileArguments{
		Path:    nestedPath,
		Content: "test content",
	})
	if err != nil {
		t.Fatalf("expected write to succeed, got error: %v", err)
	}

	gotResult := result.(string)

	// 验证不包含绝对路径
	if strings.Contains(gotResult, workspace) {
		t.Errorf("result should not contain absolute workspace path, got: %s", gotResult)
	}

	// 验证包含相对路径
	if !strings.Contains(gotResult, nestedPath) {
		t.Errorf("result should contain relative path %q, got: %s", nestedPath, gotResult)
	}

	// 验证返回格式
	expectedResult := "文件已写入: " + nestedPath
	if gotResult != expectedResult {
		t.Errorf("expected %q, got: %q", expectedResult, gotResult)
	}
}

// TestReadFileReturnsRelativePath 测试 ReadFile 返回内容（不包含路径）
func TestReadFileReturnsRelativePath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录文件
	nestedPath := filepath.Join("deep", "nested", "file.txt")
	if err := os.MkdirAll(filepath.Join(workspace, "deep", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, nestedPath), []byte("file content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := ReadFile(ReadFileArguments{Path: nestedPath})
	if err != nil {
		t.Fatalf("expected read to succeed, got error: %v", err)
	}

	// ReadFile 返回文件内容，不是路径
	gotResult := result.(string)
	if gotResult != "file content" {
		t.Errorf("expected file content, got: %q", gotResult)
	}
}

// TestListFilesReturnsRelativePaths 测试 ListFiles 返回相对路径
func TestListFilesReturnsRelativePaths(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录结构
	dirs := []string{
		filepath.Join("src", "pkg", "utils"),
		filepath.Join("docs", "api"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(workspace, dir), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}

	// 创建多个文件
	files := []string{
		"root.txt",
		filepath.Join("src", "main.go"),
		filepath.Join("src", "pkg", "utils", "helper.go"),
		filepath.Join("docs", "readme.md"),
		filepath.Join("docs", "api", "spec.json"),
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(workspace, file), []byte("content"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	result, err := ListFiles(ListFilesArguments{Path: "."})
	if err != nil {
		t.Fatalf("expected list to succeed, got error: %v", err)
	}

	listText := result.(string)

	// 验证不包含绝对路径
	if strings.Contains(listText, workspace) {
		t.Errorf("list result should not contain absolute workspace path, got: %s", listText)
	}

	// 验证所有文件使用相对路径
	for _, file := range files {
		if !strings.Contains(listText, file) {
			t.Errorf("expected relative path %q in result, got: %s", file, listText)
		}
	}
}

// TestSearchInFilesReturnsRelativePaths 测试 SearchInFiles 返回相对路径
func TestSearchInFilesReturnsRelativePaths(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录结构
	if err := os.MkdirAll(filepath.Join(workspace, "src", "services"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// 创建包含关键词的文件
	files := map[string]string{
		"root.txt":                     "findme in root",
		filepath.Join("src", "main.go"):        "findme in main",
		filepath.Join("src", "services", "api.go"): "findme in api",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "findme"})
	if err != nil {
		t.Fatalf("expected search to succeed, got error: %v", err)
	}

	searchText := result.(string)

	// 验证不包含绝对路径
	if strings.Contains(searchText, workspace) {
		t.Errorf("search result should not contain absolute workspace path, got: %s", searchText)
	}

	// 验证所有匹配使用相对路径
	for path := range files {
		if !strings.Contains(searchText, path) {
			t.Errorf("expected relative path %q in result, got: %s", path, searchText)
		}
	}
}

// TestReplaceInFileReturnsRelativePath 测试 ReplaceInFile 返回相对路径
func TestReplaceInFileReturnsRelativePaths(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录文件
	nestedPath := filepath.Join("config", "settings.json")
	if err := os.MkdirAll(filepath.Join(workspace, "config"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, nestedPath), []byte(`{"debug": false}`), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: nestedPath,
		Old:  "false",
		New:  "true",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	gotResult := result.(string)

	// 验证不包含绝对路径
	if strings.Contains(gotResult, workspace) {
		t.Errorf("result should not contain absolute workspace path, got: %s", gotResult)
	}

	// 验证包含相对路径
	if !strings.Contains(gotResult, nestedPath) {
		t.Errorf("result should contain relative path %q, got: %s", nestedPath, gotResult)
	}

	// 验证返回格式
	expectedResult := "已替换文件内容: " + nestedPath
	if gotResult != expectedResult {
		t.Errorf("expected %q, got: %q", expectedResult, gotResult)
	}
}

// TestAllToolsReturnRelativePaths 综合测试所有工具返回相对路径
func TestAllToolsReturnRelativePaths(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试文件
	testFile := filepath.Join("subdir", "test.txt")
	if err := os.MkdirAll(filepath.Join(workspace, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, testFile), []byte("hello world keyword"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// WriteFile - 写入新文件
	newFile := filepath.Join("subdir", "new.txt")
	result, err := WriteFile(WriteFileArguments{Path: newFile, Content: "new content"})
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if strings.Contains(result.(string), workspace) {
		t.Errorf("WriteFile result should not contain absolute path: %s", result)
	}

	// ReadFile - 读取文件
	result, err = ReadFile(ReadFileArguments{Path: testFile})
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	// ReadFile 返回内容，不需要检查路径

	// ListFiles - 列出文件
	result, err = ListFiles(ListFilesArguments{Path: "."})
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if strings.Contains(result.(string), workspace) {
		t.Errorf("ListFiles result should not contain absolute path: %s", result)
	}

	// SearchInFiles - 搜索文件
	result, err = SearchInFiles(SearchInFilesArguments{Path: ".", Query: "keyword"})
	if err != nil {
		t.Fatalf("SearchInFiles failed: %v", err)
	}
	if strings.Contains(result.(string), workspace) {
		t.Errorf("SearchInFiles result should not contain absolute path: %s", result)
	}

	// ReplaceInFile - 替换文件内容
	result, err = ReplaceInFile(ReplaceInFileArguments{Path: testFile, Old: "keyword", New: "replaced"})
	if err != nil {
		t.Fatalf("ReplaceInFile failed: %v", err)
	}
	if strings.Contains(result.(string), workspace) {
		t.Errorf("ReplaceInFile result should not contain absolute path: %s", result)
	}
}

// TestDisplayPathHidesAbsoluteWorkspacePath 测试 displayPath 函数隐藏绝对路径
func TestDisplayPathHidesAbsoluteWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试各种路径
	testCases := []struct {
		input    string
		expected string
	}{
		{filepath.Join(workspace, "file.txt"), "file.txt"},
		{filepath.Join(workspace, "subdir", "file.txt"), filepath.Join("subdir", "file.txt")},
		{filepath.Join(workspace, "deep", "nested", "path", "file.txt"), filepath.Join("deep", "nested", "path", "file.txt")},
		{workspace, "."},
	}

	for _, tc := range testCases {
		got := displayPath(tc.input)
		if got != tc.expected {
			t.Errorf("displayPath(%q) = %q, expected %q", tc.input, got, tc.expected)
		}

		// 确保不包含工作区绝对路径
		if strings.Contains(got, workspace) {
			t.Errorf("displayPath result should not contain workspace path: %s", got)
		}
	}
}

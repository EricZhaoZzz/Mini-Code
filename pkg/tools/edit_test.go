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

// ================== More ReplaceInFile Tests ==================

func TestReplaceInFileEmptyPath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 空路径应该返回错误
	_, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "",
		Old:  "content",
		New:  "new",
	})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "path 不能为空") {
		t.Fatalf("expected empty path error, got: %v", err)
	}

	// 空白路径也应该返回错误
	_, err = ReplaceInFile(ReplaceInFileArguments{
		Path: "   ",
		Old:  "content",
		New:  "new",
	})
	if err == nil {
		t.Fatal("expected error for whitespace path")
	}
}

func TestReplaceInFileNestedDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套目录和文件
	nestedPath := filepath.Join("src", "pkg", "utils", "helper.go")
	if err := os.MkdirAll(filepath.Join(workspace, "src", "pkg", "utils"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, nestedPath), []byte("old code"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 替换嵌套目录中的文件
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: nestedPath,
		Old:  "old",
		New:  "new",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证结果
	gotResult := result.(string)
	expectedResult := "已替换文件内容: " + nestedPath
	if gotResult != expectedResult {
		t.Fatalf("expected result %q, got %q", expectedResult, gotResult)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, nestedPath))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "new code" {
		t.Fatalf("expected 'new code', got: %q", string(data))
	}
}

func TestReplaceInFileWithJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 使用 JSON 字符串作为参数
	jsonArgs := `{"path": "test.txt", "old": "world", "new": "golang"}`
	result, err := ReplaceInFile(jsonArgs)
	if err != nil {
		t.Fatalf("expected replace with json args to succeed, got error: %v", err)
	}

	// 验证结果
	gotResult := result.(string)
	if gotResult != "已替换文件内容: test.txt" {
		t.Fatalf("unexpected result: %q", gotResult)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, "test.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "hello golang" {
		t.Fatalf("expected 'hello golang', got: %q", string(data))
	}
}

func TestReplaceInFileInvalidJSONArgs(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 测试无效 JSON 参数
	_, err := ReplaceInFile("invalid json")
	if err == nil {
		t.Fatal("expected error for invalid json args")
	}
}

func TestReplaceInFileNonExistentFile(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 替换不存在的文件
	_, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "nonexistent.txt",
		Old:  "old",
		New:  "new",
	})
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "读取文件失败") {
		t.Fatalf("expected read file error, got: %v", err)
	}
}

func TestReplaceInFileWithEmptyNew(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 替换为空字符串（删除内容）
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "test.txt",
		Old:  "world",
		New:  "",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, "test.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "hello " {
		t.Fatalf("expected 'hello ', got: %q", string(data))
	}

	_ = result
}

func TestReplaceInFileMultilineContent(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建多行文件
	content := `package main

func main() {
	oldFunction()
}
`
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 替换多行内容
	oldBlock := `func main() {
	oldFunction()
}`
	newBlock := `func main() {
	newFunction()
}`
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "main.go",
		Old:  oldBlock,
		New:  newBlock,
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, "main.go"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(data), "newFunction()") {
		t.Fatalf("expected newFunction in content, got: %q", string(data))
	}
	if strings.Contains(string(data), "oldFunction()") {
		t.Fatalf("should not contain oldFunction, got: %q", string(data))
	}

	_ = result
}

func TestReplaceInFileUnicodeContent(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建包含 Unicode 的文件
	if err := os.WriteFile(filepath.Join(workspace, "unicode.txt"), []byte("你好世界，这是旧内容"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 替换 Unicode 内容
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "unicode.txt",
		Old:  "旧内容",
		New:  "新内容 🎉",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, "unicode.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "你好世界，这是新内容 🎉" {
		t.Fatalf("expected '你好世界，这是新内容 🎉', got: %q", string(data))
	}

	_ = result
}

func TestReplaceInFileSpecialCharacters(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	tests := []struct {
		name     string
		content  string
		old      string
		new      string
		expected string
	}{
		{"tabs", "col1\tcol2", "\t", ",", "col1,col2"},
		{"quotes", `say "hello"`, `"hello"`, `"hi"`, `say "hi"`},
		{"backslash", `path\file`, `\`, `/`, `path/file`},
		{"dollar", "price: $100", "$100", "$200", "price: $200"},
		{"regex_chars", "a.b*c+d", "a.b", "xyz", "xyz*c+d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := tt.name + ".txt"
			if err := os.WriteFile(filepath.Join(workspace, filename), []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write failed: %v", err)
			}

			_, err := ReplaceInFile(ReplaceInFileArguments{
				Path: filename,
				Old:  tt.old,
				New:  tt.new,
			})
			if err != nil {
				t.Fatalf("expected replace to succeed, got error: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(workspace, filename))
			if err != nil {
				t.Fatalf("read failed: %v", err)
			}
			if string(data) != tt.expected {
				t.Fatalf("expected %q, got: %q", tt.expected, string(data))
			}
		})
	}
}

func TestReplaceInFileOnlyFirstMatch(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建包含多个匹配的文件
	content := "foo bar foo bar foo"
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 替换，应该只替换第一个匹配
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "test.txt",
		Old:  "foo",
		New:  "baz",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证只替换了第一个
	data, err := os.ReadFile(filepath.Join(workspace, "test.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	expected := "baz bar foo bar foo"
	if string(data) != expected {
		t.Fatalf("expected %q, got: %q", expected, string(data))
	}

	_ = result
}

func TestReplaceInFileReturnsRelativePath(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建嵌套文件
	nestedPath := filepath.Join("deep", "nested", "file.txt")
	if err := os.MkdirAll(filepath.Join(workspace, "deep", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, nestedPath), []byte("old content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 替换
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: nestedPath,
		Old:  "old",
		New:  "new",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证返回相对路径
	gotResult := result.(string)
	if strings.Contains(gotResult, workspace) {
		t.Fatalf("result should not contain absolute workspace path, got: %s", gotResult)
	}
	if !strings.Contains(gotResult, nestedPath) {
		t.Fatalf("result should contain relative path %q, got: %s", nestedPath, gotResult)
	}
}

func TestReplaceInFileLongContent(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建长内容文件
	var content strings.Builder
	for i := 0; i < 1000; i++ {
		content.WriteString("line of content\n")
	}
	content.WriteString("UNIQUE_MARKER\n")
	for i := 0; i < 1000; i++ {
		content.WriteString("more content\n")
	}

	if err := os.WriteFile(filepath.Join(workspace, "large.txt"), []byte(content.String()), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 替换
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "large.txt",
		Old:  "UNIQUE_MARKER",
		New:  "REPLACED_MARKER",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, "large.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(data), "REPLACED_MARKER") {
		t.Fatal("expected REPLACED_MARKER in content")
	}
	if strings.Contains(string(data), "UNIQUE_MARKER") {
		t.Fatal("should not contain UNIQUE_MARKER")
	}

	_ = result
}

func TestReplaceInFileMultipleCalls(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("a b c d e"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 第一次替换
	_, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "test.txt",
		Old:  "a",
		New:  "1",
	})
	if err != nil {
		t.Fatalf("first replace failed: %v", err)
	}

	// 第二次替换
	_, err = ReplaceInFile(ReplaceInFileArguments{
		Path: "test.txt",
		Old:  "b",
		New:  "2",
	})
	if err != nil {
		t.Fatalf("second replace failed: %v", err)
	}

	// 验证最终内容
	data, err := os.ReadFile(filepath.Join(workspace, "test.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "1 2 c d e" {
		t.Fatalf("expected '1 2 c d e', got: %q", string(data))
	}
}

func TestReplaceInFileCurrentDir(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("old content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 使用 "." 路径前缀
	result, err := ReplaceInFile(ReplaceInFileArguments{
		Path: filepath.Join(".", "test.txt"),
		Old:  "old",
		New:  "new",
	})
	if err != nil {
		t.Fatalf("expected replace to succeed, got error: %v", err)
	}

	// 验证内容
	data, err := os.ReadFile(filepath.Join(workspace, "test.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "new content" {
		t.Fatalf("expected 'new content', got: %q", string(data))
	}

	_ = result
}

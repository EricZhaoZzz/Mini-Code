package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEditFixture(t *testing.T, content string) string {
	t.Helper()
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	t.Cleanup(restore)
	path := filepath.Join(workspace, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	return path
}

func readEditResult(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	return string(data)
}

// 多处匹配时必须报错且不修改文件，避免悄悄改错位置。
func TestReplaceInFileRejectsAmbiguousMatch(t *testing.T) {
	original := "foo bar foo bar foo"
	path := writeEditFixture(t, original)

	_, err := ReplaceInFile(ReplaceInFileArguments{Path: "test.txt", Old: "foo", New: "baz"})
	if err == nil {
		t.Fatal("多处匹配应报错")
	}
	if !strings.Contains(err.Error(), "3 处") || !strings.Contains(err.Error(), "replace_all") {
		t.Errorf("错误信息应说明匹配数并提示解决方式: %v", err)
	}
	if got := readEditResult(t, path); got != original {
		t.Errorf("报错时不应修改文件, 得到 %q", got)
	}
}

func TestReplaceInFileUniqueWithMoreContext(t *testing.T) {
	path := writeEditFixture(t, "hello x\nhello x\n")

	if _, err := ReplaceInFile(ReplaceInFileArguments{Path: "test.txt", Old: "hello x\nhello", New: "hello y\nhello"}); err != nil {
		t.Fatalf("唯一匹配应成功: %v", err)
	}
	if got := readEditResult(t, path); got != "hello y\nhello x\n" {
		t.Errorf("got %q", got)
	}
}

func TestReplaceInFileReplaceAll(t *testing.T) {
	path := writeEditFixture(t, "foo bar foo bar foo")

	result, err := ReplaceInFile(ReplaceInFileArguments{Path: "test.txt", Old: "foo", New: "baz", ReplaceAll: true})
	if err != nil {
		t.Fatalf("replace_all 失败: %v", err)
	}
	if got := readEditResult(t, path); got != "baz bar baz bar baz" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(result.(string), "共 3 处") {
		t.Errorf("结果应报告替换数量: %q", result)
	}
}

func TestReplaceInFileReplaceAllSingleMatch(t *testing.T) {
	path := writeEditFixture(t, "a b c")
	if _, err := ReplaceInFile(ReplaceInFileArguments{Path: "test.txt", Old: "b", New: "2", ReplaceAll: true}); err != nil {
		t.Fatal(err)
	}
	if got := readEditResult(t, path); got != "a 2 c" {
		t.Errorf("got %q", got)
	}
}

// 模型给出 LF 的多行 old，文件是 CRLF：应能匹配，且写回后保持 CRLF。
func TestReplaceInFileCRLF(t *testing.T) {
	path := writeEditFixture(t, "line1\r\nline2\r\nline3\r\n")

	_, err := ReplaceInFile(ReplaceInFileArguments{
		Path: "test.txt",
		Old:  "line1\nline2",
		New:  "first\nsecond\nextra",
	})
	if err != nil {
		t.Fatalf("CRLF 文件替换失败: %v", err)
	}
	want := "first\r\nsecond\r\nextra\r\nline3\r\n"
	if got := readEditResult(t, path); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// LF 文件不应被转换。
func TestReplaceInFileLFUnchanged(t *testing.T) {
	path := writeEditFixture(t, "a\nb\nc\n")
	if _, err := ReplaceInFile(ReplaceInFileArguments{Path: "test.txt", Old: "a\nb", New: "x\ny"}); err != nil {
		t.Fatal(err)
	}
	if got := readEditResult(t, path); got != "x\ny\nc\n" {
		t.Errorf("got %q", got)
	}
}

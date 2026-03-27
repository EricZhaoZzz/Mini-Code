package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListFilesHonorsGitIgnore(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	if err := os.MkdirAll(filepath.Join(workspace, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "ignored.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write ignored file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "visible.txt"), []byte("public"), 0o644); err != nil {
		t.Fatalf("write visible file failed: %v", err)
	}

	result, err := ListFiles(ListFilesArguments{Path: "."})
	if err != nil {
		t.Fatalf("list files failed: %v", err)
	}

	listText := result.(string)
	if strings.Contains(listText, "ignored.txt") {
		t.Fatalf("expected ignored.txt to be filtered, got: %s", listText)
	}
	if !strings.Contains(listText, "visible.txt") {
		t.Fatalf("expected visible.txt in result, got: %s", listText)
	}
}

func TestSearchInFilesSkipsBinaryFiles(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	if err := os.WriteFile(filepath.Join(workspace, "text.txt"), []byte("keyword in text"), 0o644); err != nil {
		t.Fatalf("write text file failed: %v", err)
	}
	binaryContent := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'k', 'e', 'y', 'w', 'o', 'r', 'd'}
	if err := os.WriteFile(filepath.Join(workspace, "image.bin"), binaryContent, 0o644); err != nil {
		t.Fatalf("write binary file failed: %v", err)
	}

	result, err := SearchInFiles(SearchInFilesArguments{Path: ".", Query: "keyword"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	searchText := result.(string)
	if strings.Contains(searchText, "image.bin") {
		t.Fatalf("expected binary file to be skipped, got: %s", searchText)
	}
	if !strings.Contains(searchText, "text.txt:1: keyword in text") {
		t.Fatalf("expected text match in result, got: %s", searchText)
	}
}

func TestReadFileRejectsBinaryFiles(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	binaryContent := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x01, 0x02}
	if err := os.WriteFile(filepath.Join(workspace, "image.bin"), binaryContent, 0o644); err != nil {
		t.Fatalf("write binary file failed: %v", err)
	}

	_, err := ReadFile(ReadFileArguments{Path: "image.bin"})
	if err == nil {
		t.Fatal("expected binary read to fail")
	}
	if !strings.Contains(err.Error(), "不支持读取二进制文件") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateSchemaUsesJSONSchemaRequiredTags(t *testing.T) {
	schema, ok := generateSchema(WriteFileArguments{}).(map[string]interface{})
	if !ok {
		t.Fatalf("expected schema map, got %T", schema)
	}

	required, ok := schema["required"].([]interface{})
	if !ok {
		t.Fatalf("expected required array, got %T", schema["required"])
	}

	requiredFields := make(map[string]bool, len(required))
	for _, field := range required {
		requiredFields[field.(string)] = true
	}

	if !requiredFields["path"] {
		t.Fatal("expected path to be required")
	}
	if !requiredFields["content"] {
		t.Fatal("expected content to be required")
	}

	listSchema := generateSchema(ListFilesArguments{}).(map[string]interface{})
	if required, exists := listSchema["required"]; exists && len(required.([]interface{})) > 0 {
		t.Fatalf("expected list_files path to stay optional, got: %#v", required)
	}
}

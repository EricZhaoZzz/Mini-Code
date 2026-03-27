package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ================== Rename File Tests ==================

func TestRenameFile(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试文件
	if err := os.WriteFile(filepath.Join(workspace, "old.txt"), []byte("test content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 重命名文件
	result, err := RenameFile(RenameFileArguments{
		OldPath: "old.txt",
		NewPath: "new.txt",
	})
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	gotResult := result.(string)
	if !strings.Contains(gotResult, "文件已重命名") {
		t.Fatalf("unexpected result: %q", gotResult)
	}

	// 验证文件已被重命名
	if _, err := os.Stat(filepath.Join(workspace, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("old file should not exist")
	}
	if _, err := os.Stat(filepath.Join(workspace, "new.txt")); err != nil {
		t.Fatal("new file should exist")
	}
}

func TestRenameFileToNestedDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试文件
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 重命名到不存在的嵌套目录
	result, err := RenameFile(RenameFileArguments{
		OldPath: "file.txt",
		NewPath: "nested/deep/file.txt",
	})
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	_ = result

	// 验证文件已被移动
	if _, err := os.Stat(filepath.Join(workspace, "nested", "deep", "file.txt")); err != nil {
		t.Fatal("file should exist in nested directory")
	}
}

func TestRenameFileNonExistentSource(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	_, err := RenameFile(RenameFileArguments{
		OldPath: "nonexistent.txt",
		NewPath: "new.txt",
	})
	if err == nil {
		t.Fatal("expected error for non-existent source")
	}
	if !strings.Contains(err.Error(), "源文件不存在") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenameFileEmptyPaths(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 空旧路径
	_, err := RenameFile(RenameFileArguments{
		OldPath: "",
		NewPath: "new.txt",
	})
	if err == nil {
		t.Fatal("expected error for empty old path")
	}

	// 空新路径
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_, err = RenameFile(RenameFileArguments{
		OldPath: "test.txt",
		NewPath: "",
	})
	if err == nil {
		t.Fatal("expected error for empty new path")
	}
}

// ================== Delete File Tests ==================

func TestDeleteFile(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 删除文件（带确认）
	result, err := DeleteFile(DeleteFileArguments{
		Path:    "test.txt",
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if !strings.Contains(result.(string), "文件已删除") {
		t.Fatalf("unexpected result: %q", result)
	}

	// 验证文件已被删除
	if _, err := os.Stat(filepath.Join(workspace, "test.txt")); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestDeleteFileWithoutConfirm(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 不确认删除
	_, err := DeleteFile(DeleteFileArguments{
		Path:    "test.txt",
		Confirm: false,
	})
	if err == nil {
		t.Fatal("expected error when confirm is false")
	}
	if !strings.Contains(err.Error(), "需要确认") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteEmptyDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建空目录
	if err := os.Mkdir(filepath.Join(workspace, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// 删除空目录
	result, err := DeleteFile(DeleteFileArguments{
		Path:    "empty",
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if !strings.Contains(result.(string), "目录已删除") {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestDeleteNonEmptyDirectoryWithoutRecursive(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建非空目录
	if err := os.Mkdir(filepath.Join(workspace, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "dir", "file.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 尝试不递归删除
	_, err := DeleteFile(DeleteFileArguments{
		Path:      "dir",
		Confirm:   true,
		Recursive: false,
	})
	if err == nil {
		t.Fatal("expected error for non-empty directory without recursive")
	}
	if !strings.Contains(err.Error(), "目录不为空") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteNonEmptyDirectoryWithRecursive(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建非空目录
	if err := os.Mkdir(filepath.Join(workspace, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "dir", "file.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 递归删除
	result, err := DeleteFile(DeleteFileArguments{
		Path:      "dir",
		Confirm:   true,
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if !strings.Contains(result.(string), "目录已删除") {
		t.Fatalf("unexpected result: %q", result)
	}
}

// ================== Copy File Tests ==================

func TestCopyFile(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建源文件
	if err := os.WriteFile(filepath.Join(workspace, "source.txt"), []byte("test content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 复制文件
	result, err := CopyFile(CopyFileArguments{
		Source:      "source.txt",
		Destination: "dest.txt",
	})
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}

	if !strings.Contains(result.(string), "文件已复制") {
		t.Fatalf("unexpected result: %q", result)
	}

	// 验证文件内容
	data, err := os.ReadFile(filepath.Join(workspace, "dest.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "test content" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestCopyFileToNestedDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建源文件
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 复制到不存在的嵌套目录
	result, err := CopyFile(CopyFileArguments{
		Source:      "file.txt",
		Destination: "nested/deep/file.txt",
	})
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}

	_ = result

	// 验证目标文件存在
	if _, err := os.Stat(filepath.Join(workspace, "nested", "deep", "file.txt")); err != nil {
		t.Fatal("destination file should exist")
	}
}

func TestCopyFileOverwrite(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建源文件和目标文件
	if err := os.WriteFile(filepath.Join(workspace, "source.txt"), []byte("new content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "dest.txt"), []byte("old content"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 不覆盖
	_, err := CopyFile(CopyFileArguments{
		Source:      "source.txt",
		Destination: "dest.txt",
		Overwrite:   false,
	})
	if err == nil {
		t.Fatal("expected error when file exists without overwrite")
	}

	// 覆盖
	_, err = CopyFile(CopyFileArguments{
		Source:      "source.txt",
		Destination: "dest.txt",
		Overwrite:   true,
	})
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}

	// 验证内容已更新
	data, err := os.ReadFile(filepath.Join(workspace, "dest.txt"))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "new content" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

// ================== Create Directory Tests ==================

func TestCreateDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	result, err := CreateDirectory(CreateDirectoryArguments{
		Path:    "newdir",
		Parents: false,
	})
	if err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if !strings.Contains(result.(string), "目录已创建") {
		t.Fatalf("unexpected result: %q", result)
	}

	// 验证目录存在
	if info, err := os.Stat(filepath.Join(workspace, "newdir")); err != nil || !info.IsDir() {
		t.Fatal("directory should exist")
	}
}

func TestCreateDirectoryWithParents(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	result, err := CreateDirectory(CreateDirectoryArguments{
		Path:    "a/b/c/d",
		Parents: true,
	})
	if err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	_ = result

	// 验证嵌套目录存在
	if info, err := os.Stat(filepath.Join(workspace, "a", "b", "c", "d")); err != nil || !info.IsDir() {
		t.Fatal("nested directory should exist")
	}
}

func TestCreateDirectoryWithoutParents(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 尝试创建嵌套目录但不创建父目录
	_, err := CreateDirectory(CreateDirectoryArguments{
		Path:    "a/b/c",
		Parents: false,
	})
	if err == nil {
		t.Fatal("expected error for missing parent directory")
	}
	if !strings.Contains(err.Error(), "父目录不存在") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ================== Get File Info Tests ==================

func TestGetFileInfo(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试文件
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := GetFileInfo(GetFileInfoArguments{
		Path: "test.txt",
	})
	if err != nil {
		t.Fatalf("get info failed: %v", err)
	}

	info := result.(*FileInfo)
	if info.Name != "test.txt" {
		t.Fatalf("unexpected name: %q", info.Name)
	}
	if !info.IsFile {
		t.Fatal("should be a file")
	}
	if info.IsDirectory {
		t.Fatal("should not be a directory")
	}
	if info.SizeBytes != 11 {
		t.Fatalf("unexpected size: %d", info.SizeBytes)
	}
	if info.SHA256 == "" {
		t.Fatal("should have SHA256")
	}
}

func TestGetFileInfoDirectory(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	// 创建测试目录
	if err := os.Mkdir(filepath.Join(workspace, "testdir"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "testdir", "file.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := GetFileInfo(GetFileInfoArguments{
		Path: "testdir",
	})
	if err != nil {
		t.Fatalf("get info failed: %v", err)
	}

	info := result.(*FileInfo)
	if info.Name != "testdir" {
		t.Fatalf("unexpected name: %q", info.Name)
	}
	if !info.IsDirectory {
		t.Fatal("should be a directory")
	}
	if info.IsFile {
		t.Fatal("should not be a file")
	}
}

func TestGetFileInfoNonExistent(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	_, err := GetFileInfo(GetFileInfoArguments{
		Path: "nonexistent.txt",
	})
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// ================== Download File Tests ==================

func TestDownloadFileInvalidURL(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	_, err := DownloadFile(DownloadFileArguments{
		URL: "invalid-url",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestDownloadFileUnsupportedProtocol(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	_, err := DownloadFile(DownloadFileArguments{
		URL: "ftp://example.com/file.txt",
	})
	if err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
	if !strings.Contains(err.Error(), "只支持 HTTP/HTTPS") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadFileEmptyURL(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	_, err := DownloadFile(DownloadFileArguments{
		URL: "",
	})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}
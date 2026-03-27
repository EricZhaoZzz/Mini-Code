package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

// ================== Rename File ==================

type RenameFileArguments struct {
	OldPath string `json:"old_path" validate:"required" jsonschema:"required" jsonschema_description:"原文件路径"`
	NewPath string `json:"new_path" validate:"required" jsonschema:"required" jsonschema_description:"新文件路径"`
}

func RenameFile(args interface{}) (interface{}, error) {
	var params RenameFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	oldPath, err := resolveWorkspacePath(params.OldPath)
	if err != nil {
		return nil, err
	}

	newPath, err := resolveWorkspacePath(params.NewPath)
	if err != nil {
		return nil, err
	}

	// 检查源文件是否存在
	info, err := os.Stat(oldPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("源文件不存在: %s", displayPath(oldPath))
		}
		return nil, fmt.Errorf("获取源文件信息失败: %w", err)
	}

	// 确保目标目录存在
	targetDir := filepath.Dir(newPath)
	if targetDir != "." && targetDir != string(filepath.Separator) {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建目标目录失败: %w", err)
		}
	}

	// 执行重命名/移动
	if err := os.Rename(oldPath, newPath); err != nil {
		// 如果跨设备移动失败，尝试复制后删除
		if strings.Contains(err.Error(), "invalid argument") || strings.Contains(err.Error(), "cross-device") {
			if err := copyFileRecursive(oldPath, newPath); err != nil {
				return nil, fmt.Errorf("移动文件失败: %w", err)
			}
			if err := os.RemoveAll(oldPath); err != nil {
				return nil, fmt.Errorf("删除源文件失败: %w", err)
			}
		} else {
			return nil, fmt.Errorf("重命名文件失败: %w", err)
		}
	}

	fileType := "文件"
	if info.IsDir() {
		fileType = "目录"
	}

	return fmt.Sprintf("%s已重命名: %s -> %s", fileType, displayPath(oldPath), displayPath(newPath)), nil
}

// ================== Delete File ==================

type DeleteFileArguments struct {
	Path     string `json:"path" validate:"required" jsonschema:"required" jsonschema_description:"要删除的文件或目录路径"`
	Confirm  bool   `json:"confirm" jsonschema_description:"确认删除（必须为 true 才能执行删除）"`
	Recursive bool   `json:"recursive" jsonschema_description:"递归删除目录（删除目录时需要）"`
}

func DeleteFile(args interface{}) (interface{}, error) {
	var params DeleteFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	if !params.Confirm {
		return nil, fmt.Errorf("删除操作需要确认，请将 confirm 参数设为 true")
	}

	targetPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	// 检查文件/目录是否存在
	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件或目录不存在: %s", displayPath(targetPath))
		}
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 安全检查：防止删除工作区根目录
	root, err := workspaceRoot()
	if err != nil {
		return nil, err
	}
	if targetPath == root {
		return nil, fmt.Errorf("不能删除工作区根目录")
	}

	// 检查是否为目录
	if info.IsDir() {
		// 检查目录是否为空
		isEmpty, err := isDirEmpty(targetPath)
		if err != nil {
			return nil, fmt.Errorf("检查目录失败: %w", err)
		}
		
		if !isEmpty && !params.Recursive {
			return nil, fmt.Errorf("目录不为空，需要设置 recursive=true 来递归删除")
		}

		if err := os.RemoveAll(targetPath); err != nil {
			return nil, fmt.Errorf("删除目录失败: %w", err)
		}
		return fmt.Sprintf("目录已删除: %s", displayPath(targetPath)), nil
	}

	// 删除文件
	if err := os.Remove(targetPath); err != nil {
		return nil, fmt.Errorf("删除文件失败: %w", err)
	}

	return fmt.Sprintf("文件已删除: %s", displayPath(targetPath)), nil
}

func isDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}

// ================== Copy File ==================

type CopyFileArguments struct {
	Source      string `json:"source" validate:"required" jsonschema:"required" jsonschema_description:"源文件路径"`
	Destination string `json:"destination" validate:"required" jsonschema:"required" jsonschema_description:"目标文件路径"`
	Overwrite   bool   `json:"overwrite" jsonschema_description:"是否覆盖已存在的目标文件"`
}

func CopyFile(args interface{}) (interface{}, error) {
	var params CopyFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	srcPath, err := resolveWorkspacePath(params.Source)
	if err != nil {
		return nil, err
	}

	dstPath, err := resolveWorkspacePath(params.Destination)
	if err != nil {
		return nil, err
	}

	// 检查源文件是否存在
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("源文件不存在: %s", displayPath(srcPath))
		}
		return nil, fmt.Errorf("获取源文件信息失败: %w", err)
	}

	// 检查目标是否已存在
	if _, err := os.Stat(dstPath); err == nil {
		if !params.Overwrite {
			return nil, fmt.Errorf("目标文件已存在: %s（如需覆盖，请设置 overwrite=true）", displayPath(dstPath))
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查目标文件失败: %w", err)
	}

	// 确保目标目录存在
	dstDir := filepath.Dir(dstPath)
	if dstDir != "." && dstDir != string(filepath.Separator) {
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建目标目录失败: %w", err)
		}
	}

	// 执行复制
	if srcInfo.IsDir() {
		if err := copyDir(srcPath, dstPath); err != nil {
			return nil, fmt.Errorf("复制目录失败: %w", err)
		}
		return fmt.Sprintf("目录已复制: %s -> %s", displayPath(srcPath), displayPath(dstPath)), nil
	}

	if err := copyFileAtomic(srcPath, dstPath); err != nil {
		return nil, fmt.Errorf("复制文件失败: %w", err)
	}

	return fmt.Sprintf("文件已复制: %s -> %s", displayPath(srcPath), displayPath(dstPath)), nil
}

func copyFileAtomic(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	data, err := io.ReadAll(srcFile)
	if err != nil {
		return err
	}

	return writeFileAtomically(dst, data, srcInfo.Mode())
}

func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFileAtomic(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// ================== Create Directory ==================

type CreateDirectoryArguments struct {
	Path      string `json:"path" validate:"required" jsonschema:"required" jsonschema_description:"要创建的目录路径"`
	Parents   bool   `json:"parents" jsonschema_description:"是否创建父目录（类似 mkdir -p）"`
}

func CreateDirectory(args interface{}) (interface{}, error) {
	var params CreateDirectoryArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	targetPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	// 检查是否已存在
	info, err := os.Stat(targetPath)
	if err == nil {
		if info.IsDir() {
			return fmt.Sprintf("目录已存在: %s", displayPath(targetPath)), nil
		}
		return nil, fmt.Errorf("路径已存在但不是目录: %s", displayPath(targetPath))
	}

	// 创建目录
	if params.Parents {
		if err := os.MkdirAll(targetPath, 0o755); err != nil {
			return nil, fmt.Errorf("创建目录失败: %w", err)
		}
	} else {
		if err := os.Mkdir(targetPath, 0o755); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("父目录不存在，请设置 parents=true 来创建父目录")
			}
			return nil, fmt.Errorf("创建目录失败: %w", err)
		}
	}

	return fmt.Sprintf("目录已创建: %s", displayPath(targetPath)), nil
}

// ================== Get File Info ==================

type GetFileInfoArguments struct {
	Path string `json:"path" validate:"required" jsonschema:"required" jsonschema_description:"要获取信息的文件或目录路径"`
}

type FileInfo struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Type         string `json:"type"`
	Size         string `json:"size"`
	SizeBytes    int64  `json:"size_bytes"`
	Mode         string `json:"mode"`
	ModifiedTime string `json:"modified_time"`
	IsDirectory  bool   `json:"is_directory"`
	IsFile       bool   `json:"is_file"`
	IsSymlink    bool   `json:"is_symlink"`
	Extension    string `json:"extension,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
}

func GetFileInfo(args interface{}) (interface{}, error) {
	var params GetFileInfoArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	targetPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	// 获取文件信息
	lstat, err := os.Lstat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件或目录不存在: %s", displayPath(targetPath))
		}
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}

	info := &FileInfo{
		Name:         lstat.Name(),
		Path:         displayPath(targetPath),
		Mode:         lstat.Mode().String(),
		ModifiedTime: lstat.ModTime().Format(time.RFC3339),
		IsSymlink:    lstat.Mode()&os.ModeSymlink != 0,
	}

	if lstat.IsDir() {
		info.Type = "目录"
		info.IsDirectory = true
		info.IsFile = false
		// 计算目录大小
		size, err := getDirSize(targetPath)
		if err == nil {
			info.SizeBytes = size
			info.Size = humanize.Bytes(uint64(size))
		} else {
			info.Size = "未知"
		}
	} else {
		info.Type = "文件"
		info.IsDirectory = false
		info.IsFile = true
		info.SizeBytes = lstat.Size()
		info.Size = humanize.Bytes(uint64(lstat.Size()))
		info.Extension = filepath.Ext(lstat.Name())

		// 计算文件的 SHA256
		if !info.IsSymlink {
			hash, err := computeFileSHA256(targetPath)
			if err == nil {
				info.SHA256 = hash
			}
		}
	}

	return info, nil
}

func getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func computeFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFileRecursive(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return copyDir(src, dst)
	}

	return copyFileAtomic(src, dst)
}
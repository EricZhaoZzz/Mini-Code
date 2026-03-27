package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

// ================== Download File ==================

type DownloadFileArguments struct {
	URL         string `json:"url" validate:"required" jsonschema:"required" jsonschema_description:"要下载的文件 URL"`
	Destination string `json:"destination" jsonschema_description:"保存路径（留空则使用 URL 中的文件名）"`
	Timeout     int    `json:"timeout" jsonschema_description:"下载超时时间（秒），默认 300 秒"`
	Overwrite   bool   `json:"overwrite" jsonschema_description:"是否覆盖已存在的文件"`
}

type DownloadResult struct {
	URL          string `json:"url"`
	Destination  string `json:"destination"`
	Size         string `json:"size"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentType  string `json:"content_type,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Duration     string `json:"duration"`
	StatusCode   int    `json:"status_code"`
}

func DownloadFile(args interface{}) (interface{}, error) {
	var params DownloadFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	// 验证 URL
	parsedURL, err := url.Parse(params.URL)
	if err != nil {
		return nil, fmt.Errorf("无效的 URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("只支持 HTTP/HTTPS 协议")
	}

	// 确定目标路径
	var destPath string
	if params.Destination != "" {
		destPath = params.Destination
	} else {
		// 从 URL 提取文件名
		filename := filepath.Base(parsedURL.Path)
		if filename == "" || filename == "." || filename == "/" {
			filename = "download_" + time.Now().Format("20060102_150405")
		}
		destPath = filename
	}

	// 解析工作区路径
	targetPath, err := resolveWorkspacePath(destPath)
	if err != nil {
		return nil, err
	}

	// 检查文件是否已存在
	if _, err := os.Stat(targetPath); err == nil {
		if !params.Overwrite {
			return nil, fmt.Errorf("文件已存在: %s（如需覆盖，请设置 overwrite=true）", displayPath(targetPath))
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查目标文件失败: %w", err)
	}

	// 确保目标目录存在
	destDir := filepath.Dir(targetPath)
	if destDir != "." && destDir != string(filepath.Separator) {
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建目标目录失败: %w", err)
		}
	}

	// 设置超时
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 300 // 默认 5 分钟
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置 User-Agent
	req.Header.Set("User-Agent", "Mini-Code/1.0")

	// 发送请求
	startTime := time.Now()
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("下载超时（超过 %d 秒）", timeout)
		}
		return nil, fmt.Errorf("下载请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败: HTTP %d %s", resp.StatusCode, resp.Status)
	}

	// 获取内容类型
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		// 只保留主要部分
		if idx := strings.Index(contentType, ";"); idx != -1 {
			contentType = strings.TrimSpace(contentType[:idx])
		}
	}

	// 下载文件到临时位置，然后原子写入
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".download-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tempPath := tempFile.Name()

	// 确保清理临时文件
	defer func() {
		tempFile.Close()
		os.Remove(tempPath)
	}()

	// 复制内容并计算 SHA256
	hash := sha256.New()
	writer := io.MultiWriter(tempFile, hash)

	written, err := io.Copy(writer, resp.Body)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("下载超时（超过 %d 秒）", timeout)
		}
		return nil, fmt.Errorf("下载文件失败: %w", err)
	}

	// 确保写入磁盘
	if err := tempFile.Sync(); err != nil {
		return nil, fmt.Errorf("同步文件失败: %w", err)
	}

	// 关闭临时文件
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("关闭临时文件失败: %w", err)
	}

	// 移动到目标位置
	if err := os.Rename(tempPath, targetPath); err != nil {
		// 如果跨设备移动失败，尝试复制
		if strings.Contains(err.Error(), "invalid argument") || strings.Contains(err.Error(), "cross-device") {
			srcFile, openErr := os.Open(tempPath)
			if openErr != nil {
				return nil, fmt.Errorf("打开临时文件失败: %w", openErr)
			}
			defer srcFile.Close()

			dstFile, createErr := os.Create(targetPath)
			if createErr != nil {
				return nil, fmt.Errorf("创建目标文件失败: %w", createErr)
			}
			defer dstFile.Close()

			if _, copyErr := io.Copy(dstFile, srcFile); copyErr != nil {
				os.Remove(targetPath)
				return nil, fmt.Errorf("复制文件失败: %w", copyErr)
			}
		} else {
			return nil, fmt.Errorf("保存文件失败: %w", err)
		}
	}

	// 计算耗时
	duration := time.Since(startTime)

	result := &DownloadResult{
		URL:         params.URL,
		Destination: displayPath(targetPath),
		SizeBytes:   written,
		Size:        humanize.Bytes(uint64(written)),
		ContentType: contentType,
		SHA256:      hex.EncodeToString(hash.Sum(nil)),
		Duration:    duration.Round(time.Millisecond).String(),
		StatusCode:  resp.StatusCode,
	}

	return result, nil
}
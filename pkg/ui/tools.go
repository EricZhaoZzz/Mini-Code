package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolCallDisplay 工具调用显示
type ToolCallDisplay struct {
	spinner *Spinner
	name    string
	args    interface{}
	start   time.Time
}

// ToolNames 工具名称映射
var ToolNames = map[string]string{
	"write_file":      "写入文件",
	"read_file":       "读取文件",
	"list_files":      "列出目录",
	"search_in_files": "搜索文件",
	"run_shell":       "执行命令",
	"replace_in_file": "替换内容",
}

// ToolIcons 工具图标映射
var ToolIcons = map[string]string{
	"write_file":      "📝",
	"read_file":       "📖",
	"list_files":      "📁",
	"search_in_files": "🔍",
	"run_shell":       "💻",
	"replace_in_file": "✏️",
}

// StartToolCall 开始显示工具调用
func StartToolCall(name string, args interface{}) *ToolCallDisplay {
	displayName, ok := ToolNames[name]
	if !ok {
		displayName = name
	}

	// 显示工具调用开始
	if colorEnabled {
		icon := ToolIcons[name]
		if icon == "" {
			icon = IconTool
		}
		fmt.Printf("\r%s %s %s\n", Info.Sprint(icon), Bold.Sprint(displayName), Dim.Sprint("执行中..."))
	} else {
		fmt.Printf("\r[%s] %s 执行中...\n", name, displayName)
	}

	// 显示参数（如果是精简模式，可以跳过）
	if currentLogLevel >= LogLevelVerbose {
		argsJSON, _ := json.MarshalIndent(args, "  ", "  ")
		if colorEnabled {
			fmt.Printf("  %s\n", Dim.Sprint(string(argsJSON)))
		} else {
			fmt.Printf("  %s\n", string(argsJSON))
		}
	}

	return &ToolCallDisplay{
		name:  name,
		args:  args,
		start: time.Now(),
	}
}

// Complete 完成工具调用
func (d *ToolCallDisplay) Complete(result string, err error) {
	elapsed := time.Since(d.start)

	if err != nil {
		if colorEnabled {
			fmt.Printf("  %s %s (%.1fs)\n", Error.Sprint(IconError), Error.Sprint(err.Error()), elapsed.Seconds())
		} else {
			fmt.Printf("  [错误] %s (%.1fs)\n", err.Error(), elapsed.Seconds())
		}
		return
	}

	// 显示结果摘要
	resultPreview := truncate(strings.ReplaceAll(result, "\n", " "), 80)
	if colorEnabled {
		fmt.Printf("  %s %s (%.1fs)\n", Success.Sprint(IconSuccess), Dim.Sprint(resultPreview), elapsed.Seconds())
	} else {
		fmt.Printf("  [完成] %s (%.1fs)\n", resultPreview, elapsed.Seconds())
	}
}

// ShowToolCallSummary 显示工具调用摘要
func ShowToolCallSummary(results []ToolCallResult) {
	success := 0
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			success++
		}
	}

	fmt.Println()
	if colorEnabled {
		fmt.Printf("%s 调用完成: %s 成功", IconComplete, Success.Sprintf("%d", success))
		if failed > 0 {
			fmt.Printf(", %s 失败", Error.Sprintf("%d", failed))
		}
		fmt.Println()
	} else {
		fmt.Printf("调用完成: %d 成功", success)
		if failed > 0 {
			fmt.Printf(", %d 失败", failed)
		}
		fmt.Println()
	}
}

// ToolCallResult 工具调用结果
type ToolCallResult struct {
	Name string
	Err  error
}

// StreamingText 流式文本输出
type StreamingText struct {
	buffer    strings.Builder
	startTime time.Time
}

// NewStreamingText 创建流式文本输出
func NewStreamingText() *StreamingText {
	return &StreamingText{
		startTime: time.Now(),
	}
}

// Write 写入内容
func (s *StreamingText) Write(content string) {
	s.buffer.WriteString(content)
	fmt.Print(content)
}

// Complete 完成输出
func (s *StreamingText) Complete() string {
	elapsed := time.Since(s.startTime)
	fmt.Printf("\n%s", Dim.Sprintf("(耗时 %.1fs)", elapsed.Seconds()))
	return s.buffer.String()
}

// GetContent 获取内容
func (s *StreamingText) GetContent() string {
	return s.buffer.String()
}

// LogLevel 日志级别
type LogLevel int

const (
	LogLevelMinimal LogLevel = iota
	LogLevelNormal
	LogLevelVerbose
)

// CurrentLogLevel 当前日志级别
var currentLogLevel = LogLevelNormal

// SetLogLevel 设置日志级别
func SetLogLevel(level LogLevel) {
	currentLogLevel = level
}

// ThinkingDisplay 思考过程显示
type ThinkingDisplay struct {
	spinner *Spinner
}

// NewThinkingDisplay 创建思考过程显示
func NewThinkingDisplay() *ThinkingDisplay {
	return &ThinkingDisplay{
		spinner: NewSpinner(SpinnerDots, "正在思考..."),
	}
}

// Start 开始显示
func (d *ThinkingDisplay) Start() {
	d.spinner.Start()
}

// Update 更新消息
func (d *ThinkingDisplay) Update(msg string) {
	d.spinner.Update(msg)
}

// Stop 停止显示
func (d *ThinkingDisplay) Stop() {
	d.spinner.Stop()
}

// StopWithMessage 停止并显示消息
func (d *ThinkingDisplay) StopWithMessage(msg string) {
	d.spinner.StopWithSuccess(msg)
}
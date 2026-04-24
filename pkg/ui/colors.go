package ui

import (
	"fmt"
	"os"
	"strings"

	"mini-code/pkg/textutil"

	"github.com/fatih/color"
)

// 颜色定义
var (
	// 主要颜色
	Primary   = color.New(color.FgCyan, color.Bold)
	Secondary = color.New(color.FgBlue)
	Accent    = color.New(color.FgMagenta)

	// 状态颜色
	Success = color.New(color.FgGreen)
	Warning = color.New(color.FgYellow)
	Error   = color.New(color.FgRed, color.Bold)
	Info    = color.New(color.FgCyan)

	// 文本样式
	Bold   = color.New(color.Bold)
	Dim    = color.New(color.Faint)
	Italic = color.New(color.Italic)

	// 特殊颜色
	Tool      = color.New(color.FgYellow)
	User      = color.New(color.FgCyan, color.Bold)
	Assistant = color.New(color.FgGreen)
)

// 支持颜色的输出
var colorEnabled = true

func init() {
	// 检测终端是否支持颜色
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		colorEnabled = false
		color.NoColor = true
	}
}

// 图标定义
const (
	IconSuccess  = "✓"
	IconError    = "✗"
	IconWarning  = "⚠"
	IconInfo     = "ℹ"
	IconTool     = "⚡"
	IconUser     = ">"
	IconBot      = "🤖"
	IconFile     = "📄"
	IconFolder   = "📁"
	IconSearch   = "🔍"
	IconEdit     = "✏️"
	IconShell    = "💻"
	IconProgress = "⏳"
	IconComplete = "✨"
	IconArrow    = "→"
	IconBullet   = "•"
)

// PrintSuccess 打印成功消息
func PrintSuccess(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if colorEnabled {
		fmt.Printf("%s %s\n", Success.Sprint(IconSuccess), msg)
	} else {
		fmt.Printf("[%s] %s\n", "SUCCESS", msg)
	}
}

// PrintError 打印错误消息
func PrintError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if colorEnabled {
		fmt.Printf("%s %s\n", Error.Sprint(IconError), msg)
	} else {
		fmt.Printf("[%s] %s\n", "ERROR", msg)
	}
}

// PrintWarning 打印警告消息
func PrintWarning(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if colorEnabled {
		fmt.Printf("%s %s\n", Warning.Sprint(IconWarning), msg)
	} else {
		fmt.Printf("[%s] %s\n", "WARNING", msg)
	}
}

// PrintInfo 打印信息消息
func PrintInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if colorEnabled {
		fmt.Printf("%s %s\n", Info.Sprint(IconInfo), msg)
	} else {
		fmt.Printf("[%s] %s\n", "INFO", msg)
	}
}

// PrintTool 打印工具调用
func PrintTool(toolName, args string) {
	if colorEnabled {
		fmt.Printf("%s %s %s\n", Tool.Sprint(IconTool), Bold.Sprint(toolName), Dim.Sprintf("(%s)", truncate(args, 50)))
	} else {
		fmt.Printf("[TOOL] %s (%s)\n", toolName, truncate(args, 50))
	}
}

// PrintToolResult 打印工具结果
func PrintToolResult(success bool, result string) {
	icon := IconSuccess
	status := "成功"
	if !success {
		icon = IconError
		status = "失败"
	}

	if colorEnabled {
		statusColor := Success
		if !success {
			statusColor = Error
		}
		fmt.Printf("  %s %s: %s\n", statusColor.Sprint(icon), statusColor.Sprint(status), Dim.Sprint(truncate(result, 100)))
	} else {
		fmt.Printf("  [%s] %s\n", status, truncate(result, 100))
	}
}

// PrintSeparator 打印分隔线
func PrintSeparator() {
	if colorEnabled {
		fmt.Println(Dim.Sprint(strings.Repeat("─", 60)))
	} else {
		fmt.Println(strings.Repeat("-", 60))
	}
}

// PrintHeader 打印标题
func PrintHeader(title string) {
	if colorEnabled {
		fmt.Printf("\n%s %s %s\n", Dim.Sprint("─"), Primary.Sprint(title), Dim.Sprint("─"))
	} else {
		fmt.Printf("\n--- %s ---\n", title)
	}
}

// PrintPrompt 打印输入提示符
func PrintPrompt() {
	if colorEnabled {
		fmt.Print(User.Sprint("➜ "), Bold.Sprint(""))
	} else {
		fmt.Print("> ")
	}
}

// PrintAssistantLabel 打印助手标签
func PrintAssistantLabel() {
	if colorEnabled {
		fmt.Print(Assistant.Sprint("🤖 Mini-Code: "))
	} else {
		fmt.Print("Mini-Code: ")
	}
}

// SprintColor 格式化彩色字符串
func SprintColor(c *color.Color, s string) string {
	if colorEnabled {
		return c.Sprint(s)
	}
	return s
}

// SprintfColor 格式化彩色字符串（带格式）
func SprintfColor(c *color.Color, format string, args ...interface{}) string {
	if colorEnabled {
		return c.Sprintf(format, args...)
	}
	return fmt.Sprintf(format, args...)
}

// PrintDim 打印暗淡文本
func PrintDim(format string, args ...interface{}) {
	if colorEnabled {
		Dim.Printf(format, args...)
	} else {
		fmt.Printf(format, args...)
	}
}

// PrintBold 打印粗体文本
func PrintBold(format string, args ...interface{}) {
	if colorEnabled {
		Bold.Printf(format, args...)
	} else {
		fmt.Printf(format, args...)
	}
}

// ClearLine 清除当前行
func ClearLine() {
	fmt.Print("\r\033[K")
}

// MoveUp 向上移动 n 行
func MoveUp(n int) {
	fmt.Printf("\033[%dA", n)
}

// MoveDown 向下移动 n 行
func MoveDown(n int) {
	fmt.Printf("\033[%dB", n)
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	return textutil.TruncateWithEllipsis(s, maxLen)
}

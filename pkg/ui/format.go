package ui

import (
	"fmt"
	"strings"
)

// BoxStyle 盒子样式
type BoxStyle struct {
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	Horizontal  string
	Vertical    string
}

// 预定义的盒子样式
var (
	BoxStyleSingle = BoxStyle{
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "└",
		BottomRight: "┘",
		Horizontal:  "─",
		Vertical:    "│",
	}
	BoxStyleDouble = BoxStyle{
		TopLeft:     "╔",
		TopRight:    "╗",
		BottomLeft:  "╚",
		BottomRight: "╝",
		Horizontal:  "═",
		Vertical:    "║",
	}
	BoxStyleRounded = BoxStyle{
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "╰",
		BottomRight: "╯",
		Horizontal:  "─",
		Vertical:    "│",
	}
	BoxStyleSimple = BoxStyle{
		TopLeft:     "+",
		TopRight:    "+",
		BottomLeft:  "+",
		BottomRight: "+",
		Horizontal:  "-",
		Vertical:    "|",
	}
)

// Box 创建一个带边框的盒子
func Box(content string, style BoxStyle) string {
	lines := strings.Split(content, "\n")
	maxWidth := 0
	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}

	var result strings.Builder

	// 顶部边框
	result.WriteString(style.TopLeft)
	result.WriteString(strings.Repeat(style.Horizontal, maxWidth+2))
	result.WriteString(style.TopRight)
	result.WriteString("\n")

	// 内容行
	for _, line := range lines {
		result.WriteString(style.Vertical)
		result.WriteString(" ")
		result.WriteString(line)
		result.WriteString(strings.Repeat(" ", maxWidth-len(line)))
		result.WriteString(" ")
		result.WriteString(style.Vertical)
		result.WriteString("\n")
	}

	// 底部边框
	result.WriteString(style.BottomLeft)
	result.WriteString(strings.Repeat(style.Horizontal, maxWidth+2))
	result.WriteString(style.BottomRight)

	return result.String()
}

// Panel 创建一个带标题的面板
func Panel(title, content string) string {
	lines := strings.Split(content, "\n")
	maxWidth := 0
	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}
	if len(title) > maxWidth {
		maxWidth = len(title)
	}

	var result strings.Builder

	// 顶部边框（带标题）
	result.WriteString("╭─")
	if colorEnabled {
		result.WriteString(Primary.Sprint(title))
		if maxWidth-len(title) > 0 {
			result.WriteString(strings.Repeat("─", maxWidth-len(title)+1))
		}
	} else {
		result.WriteString(title)
		if maxWidth-len(title) > 0 {
			result.WriteString(strings.Repeat("-", maxWidth-len(title)+1))
		}
	}
	result.WriteString("╮\n")

	// 内容行
	for _, line := range lines {
		result.WriteString("│ ")
		result.WriteString(line)
		result.WriteString(strings.Repeat(" ", maxWidth-len(line)+1))
		result.WriteString("│\n")
	}

	// 底部边框
	result.WriteString("╰")
	result.WriteString(strings.Repeat("─", maxWidth+3))
	result.WriteString("╯")

	return result.String()
}

// Table 创建一个表格
func Table(headers []string, rows [][]string) string {
	// 计算列宽
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	var result strings.Builder

	// 表头
	result.WriteString("┌")
	for i, w := range colWidths {
		result.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			result.WriteString("┬")
		}
	}
	result.WriteString("┐\n")

	// 标题行
	result.WriteString("│")
	for i, h := range headers {
		if colorEnabled {
			result.WriteString(" ")
			result.WriteString(Bold.Sprint(h))
			result.WriteString(strings.Repeat(" ", colWidths[i]-len(h)+1))
		} else {
			result.WriteString(" ")
			result.WriteString(h)
			result.WriteString(strings.Repeat(" ", colWidths[i]-len(h)+1))
		}
		result.WriteString("│")
	}
	result.WriteString("\n")

	// 分隔线
	result.WriteString("├")
	for i, w := range colWidths {
		result.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			result.WriteString("┼")
		}
	}
	result.WriteString("┤\n")

	// 数据行
	for _, row := range rows {
		result.WriteString("│")
		for i, cell := range row {
			result.WriteString(" ")
			result.WriteString(cell)
			result.WriteString(strings.Repeat(" ", colWidths[i]-len(cell)+1))
			result.WriteString("│")
		}
		result.WriteString("\n")
	}

	// 底部边框
	result.WriteString("└")
	for i, w := range colWidths {
		result.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			result.WriteString("┴")
		}
	}
	result.WriteString("┘")

	return result.String()
}

// List 创建列表
func List(items []string, bullet string) string {
	var result strings.Builder
	for _, item := range items {
		if colorEnabled {
			result.WriteString(SprintColor(Primary, bullet))
			result.WriteString(" ")
			result.WriteString(item)
		} else {
			result.WriteString(bullet)
			result.WriteString(" ")
			result.WriteString(item)
		}
		result.WriteString("\n")
	}
	return result.String()
}

// KeyValue 创建键值对显示
func KeyValue(pairs map[string]string, indent int) string {
	maxKeyLen := 0
	for k := range pairs {
		if len(k) > maxKeyLen {
			maxKeyLen = len(k)
		}
	}

	var result strings.Builder
	indentStr := strings.Repeat(" ", indent)
	for k, v := range pairs {
		if colorEnabled {
			result.WriteString(indentStr)
			result.WriteString(SprintColor(Secondary, k))
			result.WriteString(": ")
			result.WriteString(strings.Repeat(" ", maxKeyLen-len(k)))
			result.WriteString(v)
		} else {
			result.WriteString(fmt.Sprintf("%s%s: %s%s\n", indentStr, k, strings.Repeat(" ", maxKeyLen-len(k)), v))
		}
		result.WriteString("\n")
	}
	return result.String()
}

// CodeBlock 创建代码块
func CodeBlock(code, language string) string {
	lines := strings.Split(code, "\n")
	maxWidth := 0
	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}

	var result strings.Builder

	// 标题
	if language != "" {
		if colorEnabled {
			result.WriteString(Dim.Sprint("╭─ " + language + " "))
			result.WriteString(Dim.Sprint(strings.Repeat("─", maxWidth-len(language)+1)))
			result.WriteString(Dim.Sprint("╮\n"))
		} else {
			result.WriteString("--- " + language + " ---\n")
		}
	}

	// 代码内容（带行号）
	for i, line := range lines {
		if colorEnabled {
			lineNum := Dim.Sprintf("%3d │ ", i+1)
			result.WriteString(lineNum)
			result.WriteString(line)
		} else {
			result.WriteString(fmt.Sprintf("%3d | %s\n", i+1, line))
		}
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	// 底部
	if colorEnabled {
		result.WriteString("\n")
		result.WriteString(Dim.Sprint("╰"))
		result.WriteString(Dim.Sprint(strings.Repeat("─", maxWidth+6)))
		result.WriteString(Dim.Sprint("╯"))
	}

	return result.String()
}

// WelcomeBanner 欢迎横幅
func WelcomeBanner() string {
	banner := `
╔═══════════════════════════════════════════════════════════╗
║                                                           ║
║   ███╗   ███╗██╗ ██╗██╗███╗   ██╗██████╗                  ║
║   ████╗ ████║██║ ██║██║████╗  ██║██╔══██╗                 ║
║   ██╔████╔██║██║ ██║██║██╔██╗ ██║██║  ██║                 ║
║   ██║╚██╔╝██║╚██╗██╔╝██║██║╚██╗██║██║  ██║                ║
║   ██║ ╚═╝ ██║ ╚████╔╝ ██║██║ ╚████║██████╔╝               ║
║   ╚═╝     ╚═╝  ╚═══╝  ╚═╝╚═╝  ╚═══╝╚═════╝                ║
║                                                           ║
║            ██████╗ ██╗   ██╗██████╗  ██████╗              ║
║           ██╔════╝ ██║   ██║██╔══██╗██╔════╝              ║
║           ██║      ██║   ██║██║  ██║██║                   ║
║           ██║      ╚██╗ ██╔╝██║  ██║██║                   ║
║           ╚██████╗  ╚████╔╝ ██████╔╝╚██████╗              ║
║            ╚═════╝   ╚═══╝  ╚═════╝  ╚═════╝              ║
║                                                           ║
║              AI 编程助手 v1.0                           	║
║                                                           ║
╚═══════════════════════════════════════════════════════════╝`

	return banner
}

// HelpPanel 帮助面板
func HelpPanel() string {
	content := `命令列表:
  help, h, ?     显示帮助信息
  clear, cls     清除屏幕
  new, n         清空会话上下文
  reset, r       重置对话历史
  exit, q        退出程序

提示:
  • 直接输入任务描述开始对话
  • 支持多行输入，空行结束
  • 按 Ctrl+C 可中断当前操作

环境变量:
  LM_MAX_TURNS   最大轮次限制（默认 50，0=无限制）
  LM_DEBUG       开启调试日志
  LM_LOG_LEVEL   日志级别（minimal/normal/verbose）`

	return Panel("帮助", content)
}

// StatusPanel 状态面板
func StatusPanel(info map[string]string) string {
	return Panel("状态", KeyValue(info, 2))
}

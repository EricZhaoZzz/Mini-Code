package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ergochat/readline"
)

// InputHandler 处理用户输入
type InputHandler struct {
	rl          *readline.Instance
	history     []string
	historyFile string
}

// NewInputHandler 创建新的输入处理器
func NewInputHandler(historyFile string) *InputHandler {
	h := &InputHandler{
		history:     make([]string, 0),
		historyFile: historyFile,
	}

	// 尝试加载历史记录
	h.loadHistory()

	return h
}

// NewReadlineInput 创建 readline 输入
func NewReadlineInput(prompt string) (*readline.Instance, error) {
	return readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     ".mini-claw-history",
		HistoryLimit:    1000,
		AutoComplete:    getCompleter(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
}

// getCompleter 获取自动补全器
func getCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("clear"),
		readline.PcItem("reset"),
		readline.PcItem("new"),
		readline.PcItem("history"),
	)
}

// loadHistory 加载历史记录
func (h *InputHandler) loadHistory() {
	if h.historyFile == "" {
		return
	}

	file, err := os.Open(h.historyFile)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			h.history = append(h.history, line)
		}
	}
}

// saveHistory 保存历史记录
func (h *InputHandler) saveHistory() {
	if h.historyFile == "" {
		return
	}

	file, err := os.Create(h.historyFile)
	if err != nil {
		return
	}
	defer file.Close()

	for _, line := range h.history {
		fmt.Fprintln(file, line)
	}
}

// ReadLine 读取一行输入
func ReadLine(prompt string) (string, error) {
	rl, err := NewReadlineInput(prompt)
	if err != nil {
		// 回退到标准输入
		return ReadLineSimple(prompt)
	}
	defer rl.Close()

	line, err := rl.Readline()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(line), nil
}

// ReadLineSimple 简单的输入读取（无 readline）
func ReadLineSimple(prompt string) (string, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// ReadMultiLine 读取多行输入
func ReadMultiLine(prompt string) (string, error) {
	rl, err := NewReadlineInput(prompt)
	if err != nil {
		return ReadMultiLineSimple(prompt)
	}
	defer rl.Close()

	var lines []string
	for {
		line, err := rl.Readline()
		if err != nil {
			if err.Error() == "EOF" && len(lines) > 0 {
				break
			}
			return "", err
		}

		// 空行表示输入结束
		if strings.TrimSpace(line) == "" {
			break
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n"), nil
}

// ReadMultiLineSimple 简单的多行输入
func ReadMultiLineSimple(prompt string) (string, error) {
	fmt.Println(prompt)
	fmt.Println(Dim.Sprint("(输入空行结束)"))

	var lines []string
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("... ")
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n"), nil
}

// Confirm 确认提示
func Confirm(prompt string, defaultYes bool) bool {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}

	fmt.Printf("%s [%s]: ", prompt, hint)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return defaultYes
	}

	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return defaultYes
	}

	return line == "y" || line == "yes"
}

// Select 选择列表
func Select(prompt string, options []string) (int, error) {
	fmt.Printf("%s\n", prompt)
	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt)
	}
	fmt.Print("\n请选择 [1]: ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return -1, err
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return 0, nil
	}

	var choice int
	_, err = fmt.Sscanf(line, "%d", &choice)
	if err != nil || choice < 1 || choice > len(options) {
		return -1, fmt.Errorf("invalid choice")
	}

	return choice - 1, nil
}
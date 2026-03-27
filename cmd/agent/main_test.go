package main

import (
	"bytes"
	"context"
	"mini-claw/pkg/agent"
	"os"
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

// mockChatCompletionClient 用于测试的 mock client
type mockChatCompletionClient struct{}

// CreateChatCompletion 实现 chatCompletionClient 接口
func (m *mockChatCompletionClient) CreateChatCompletion(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Content: "test response",
				},
			},
		},
	}, nil
}

// CreateChatCompletionStream 实现 chatCompletionClient 接口
func (m *mockChatCompletionClient) CreateChatCompletionStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return nil, nil
}

// newTestEngine 创建测试用的 engine
func newTestEngine() *agent.ClawEngine {
	mockClient := &mockChatCompletionClient{}
	return agent.NewClawEngine(mockClient, "test-model")
}

// captureOutput 捕获 stdout 输出
func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// TestHandleBuiltinCommand_Help 测试 help 命令
func TestHandleBuiltinCommand_Help(t *testing.T) {
	engine := newTestEngine()

	tests := []struct {
		name  string
		input string
	}{
		{"help", "help"},
		{"h", "h"},
		{"?", "?"},
		{"HELP", "HELP"},
		{"Help", "Help"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				handleBuiltinCommand(tt.input, engine)
			})

			// 验证输出包含帮助信息
			if !strings.Contains(output, "可用命令") {
				t.Errorf("help 命令输出应包含 '可用命令', got: %s", output)
			}
			if !strings.Contains(output, "help") {
				t.Errorf("help 命令输出应包含 'help', got: %s", output)
			}
		})
	}
}

// TestHandleBuiltinCommand_Clear 测试 clear 命令
func TestHandleBuiltinCommand_Clear(t *testing.T) {
	engine := newTestEngine()

	tests := []struct {
		name  string
		input string
	}{
		{"clear", "clear"},
		{"cls", "cls"},
		{"CLEAR", "CLEAR"},
		{"Clear", "Clear"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				handleBuiltinCommand(tt.input, engine)
			})

			// clear 命令应该输出屏幕已清除
			if !strings.Contains(output, "屏幕已清除") {
				t.Errorf("clear 命令输出应包含 '屏幕已清除', got: %s", output)
			}
		})
	}
}

// TestHandleBuiltinCommand_Reset 测试 reset 命令
func TestHandleBuiltinCommand_Reset(t *testing.T) {
	engine := newTestEngine()

	// 先添加一些消息
	engine.AddUserMessage("test message 1")
	engine.AddUserMessage("test message 2")
	initialCount := engine.GetMessageCount()

	tests := []struct {
		name  string
		input string
	}{
		{"reset", "reset"},
		{"r", "r"},
		{"new", "new"},
		{"n", "n"},
		{"RESET", "RESET"},
		{"Reset", "Reset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 重置前的消息数量
			beforeCount := engine.GetMessageCount()

			output := captureOutput(func() {
				handleBuiltinCommand(tt.input, engine)
			})

			// 验证输出包含重置信息
			if !strings.Contains(output, "重置对话历史") {
				t.Errorf("reset 命令输出应包含 '重置对话历史', got: %s", output)
			}
			if !strings.Contains(output, "对话已重置") {
				t.Errorf("reset 命令输出应包含 '对话已重置', got: %s", output)
			}

			// 验证消息数量减少（只保留系统消息）
			afterCount := engine.GetMessageCount()
			if afterCount != 1 {
				t.Errorf("重置后应只剩系统消息，消息数量应为 1, got: %d", afterCount)
			}
			if afterCount >= beforeCount && beforeCount > 1 {
				t.Errorf("重置后消息数量应减少, before: %d, after: %d", beforeCount, afterCount)
			}
		})

		// 恢复消息以便下次测试
		engine.AddUserMessage("test message 1")
		engine.AddUserMessage("test message 2")
	}

	_ = initialCount // 避免未使用变量警告
}

// TestHandleBuiltinCommand_History 测试 history 命令
func TestHandleBuiltinCommand_History(t *testing.T) {
	engine := newTestEngine()

	tests := []struct {
		name  string
		input string
	}{
		{"history", "history"},
		{"hist", "hist"},
		{"HISTORY", "HISTORY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				handleBuiltinCommand(tt.input, engine)
			})

			// history 命令目前还在开发中
			if !strings.Contains(output, "对话历史功能开发中") {
				t.Errorf("history 命令输出应包含 '对话历史功能开发中', got: %s", output)
			}
		})
	}
}

// TestHandleBuiltinCommand_Unknown 测试未知命令
func TestHandleBuiltinCommand_Unknown(t *testing.T) {
	engine := newTestEngine()

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"random", "randomcommand"},
		{"partial", "hel"},  // 不是完整的 help
		{"partial2", "rese"}, // 不是完整的 reset
		{"with spaces", "help me"}, // 带空格的命令
		{"number", "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handleBuiltinCommand(tt.input, engine)
			// 未知命令应该返回 false
			if result != false {
				t.Errorf("未知命令 '%s' 应该返回 false, got: %v", tt.input, result)
			}
		})
	}
}

// TestLoadDotEnv 测试 loadDotEnv 函数
func TestLoadDotEnv(t *testing.T) {
	// 创建临时测试文件
	tmpFile := "test.env"
	content := `# 测试注释
TEST_KEY1=test_value1
TEST_KEY2="test_value2"
TEST_KEY3='test_value3'
# 另一个注释
TEST_KEY4=test value4

TEST_EMPTY=
`
	err := os.WriteFile(tmpFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	defer os.Remove(tmpFile)

	// 清理可能存在的环境变量
	os.Unsetenv("TEST_KEY1")
	os.Unsetenv("TEST_KEY2")
	os.Unsetenv("TEST_KEY3")
	os.Unsetenv("TEST_KEY4")
	os.Unsetenv("TEST_EMPTY")

	// 加载 .env 文件
	loadDotEnv(tmpFile)

	// 验证环境变量
	tests := []struct {
		key      string
		expected string
	}{
		{"TEST_KEY1", "test_value1"},
		{"TEST_KEY2", "test_value2"},
		{"TEST_KEY3", "test_value3"},
		{"TEST_KEY4", "test value4"},
		{"TEST_EMPTY", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			value := os.Getenv(tt.key)
			if value != tt.expected {
				t.Errorf("环境变量 %s 应为 '%s', got: '%s'", tt.key, tt.expected, value)
			}
		})
	}
}

// TestLoadDotEnv_NonExistent 测试加载不存在的 .env 文件
func TestLoadDotEnv_NonExistent(t *testing.T) {
	// 加载不存在的文件应该不会报错
	loadDotEnv("non_existent_file.env")
}

// TestLoadDotEnv_DontOverride 测试不覆盖已存在的环境变量
func TestLoadDotEnv_DontOverride(t *testing.T) {
	// 设置一个已存在的环境变量
	os.Setenv("EXISTING_KEY", "original_value")

	// 创建临时测试文件
	tmpFile := "test_override.env"
	content := `EXISTING_KEY=new_value`
	err := os.WriteFile(tmpFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	defer os.Remove(tmpFile)

	// 加载 .env 文件
	loadDotEnv(tmpFile)

	// 验证环境变量没有被覆盖
	value := os.Getenv("EXISTING_KEY")
	if value != "original_value" {
		t.Errorf("已存在的环境变量不应被覆盖, expected 'original_value', got: '%s'", value)
	}

	os.Unsetenv("EXISTING_KEY")
}

// TestPrintWelcome 测试欢迎信息输出
func TestPrintWelcome(t *testing.T) {
	output := captureOutput(func() {
		printWelcome()
	})

	// 验证欢迎信息包含关键内容
	expectedStrings := []string{
		"Mini-Claw",
		"AI 编程助手",
		"help",
		"clear",
		"new",
		"reset",
		"exit",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("欢迎信息应包含 '%s'", expected)
		}
	}
}

// TestPrintHelp 测试帮助信息输出
func TestPrintHelp(t *testing.T) {
	output := captureOutput(func() {
		printHelp()
	})

	// 验证帮助信息包含所有命令
	expectedStrings := []string{
		"可用命令",
		"help",
		"clear",
		"new",
		"reset",
		"exit",
		"显示帮助信息",
		"清除屏幕",
		"清空会话上下文",
		"重置对话历史",
		"退出程序",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("帮助信息应包含 '%s'", expected)
		}
	}
}

// TestClearScreen 测试清屏函数
func TestClearScreen(t *testing.T) {
	output := captureOutput(func() {
		clearScreen()
	})

	// 验证清屏输出
	if !strings.Contains(output, "屏幕已清除") {
		t.Errorf("清屏输出应包含 '屏幕已清除', got: %s", output)
	}
}
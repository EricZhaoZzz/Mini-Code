package telegram

import (
	"mini-code/pkg/channel"
	"strings"
	"testing"
)

func TestCommandHandler_Handle(t *testing.T) {
	// 创建一个简单的 CommandHandler 用于测试
	// 由于 Session 需要完整的上下文，我们测试各个子命令

	handler := &CommandHandler{}

	t.Run("/start response", func(t *testing.T) {
		msg := channel.IncomingMessage{Text: "/start"}
		result := handler.handleStart(msg)
		if !result.Handled {
			t.Error("start should be handled")
		}
		if !containsStr(result.Response, "欢迎使用") {
			t.Error("start response should contain welcome message")
		}
	})

	t.Run("/help response", func(t *testing.T) {
		result := handler.handleHelp()
		if !result.Handled {
			t.Error("help should be handled")
		}
		// 验证帮助文本包含所有命令
		commands := []string{"/start", "/help", "/reset", "/status", "/cancel", "/memory"}
		for _, cmd := range commands {
			if !containsStr(result.Response, cmd) {
				t.Errorf("help response should contain %s", cmd)
			}
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		msg := channel.IncomingMessage{Text: "/unknown"}
		// unknown command goes through Handle which checks if it starts with /
		result := handler.Handle(msg, nil)
		if !result.Handled {
			t.Error("unknown command should be handled")
		}
		if !containsStr(result.Response, "未知命令") {
			t.Error("should report unknown command")
		}
	})

	t.Run("non-command text", func(t *testing.T) {
		msg := channel.IncomingMessage{Text: "帮我写一个函数"}
		result := handler.Handle(msg, nil)
		if result.Handled {
			t.Error("non-command text should not be handled")
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if len(result) > tt.maxLen {
				t.Errorf("result length %d exceeds max %d", len(result), tt.maxLen)
			}
			if len(tt.input) <= tt.maxLen && result != tt.input {
				t.Errorf("expected %q, got %q", tt.input, result)
			}
		})
	}
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}
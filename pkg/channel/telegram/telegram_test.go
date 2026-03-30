package telegram

import (
	"testing"
)

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		maxLen     int
		minSegments int // 最少分段数量
		maxSegments int // 最多分段数量
	}{
		{
			name:        "short message",
			text:        "Hello, world!",
			maxLen:      100,
			minSegments: 1,
			maxSegments: 1,
		},
		{
			name:        "exact limit",
			text:        string(make([]byte, 100)),
			maxLen:      100,
			minSegments: 1,
			maxSegments: 1,
		},
		{
			name:        "over limit",
			text:        string(make([]byte, 150)),
			maxLen:      100,
			minSegments: 2,
			maxSegments: 3,
		},
		{
			name:        "multiple lines under limit",
			text:        "Line 1\nLine 2\nLine 3",
			maxLen:      100,
			minSegments: 1,
			maxSegments: 1,
		},
		{
			name:        "multiple lines over limit",
			text:        "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
			maxLen:      20,
			minSegments: 2,
			maxSegments: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segments := splitMessage(tt.text, tt.maxLen)
			if len(segments) < tt.minSegments {
				t.Errorf("expected at least %d segments, got %d", tt.minSegments, len(segments))
			}
			if len(segments) > tt.maxSegments {
				t.Errorf("expected at most %d segments, got %d", tt.maxSegments, len(segments))
			}
			// 验证每个分段都不超过最大长度（加上一些容差）
			for i, seg := range segments {
				if len(seg) > tt.maxLen+20 { // 允许一些额外的换行符
					t.Errorf("segment %d exceeds max length: %d > %d", i, len(seg), tt.maxLen)
				}
			}
		})
	}
}

func TestParseChatID(t *testing.T) {
	tc := &TelegramChannel{}

	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"telegram:123456", 123456, false},
		{"telegram:987654321", 987654321, false},
		{"123456", 123456, false},
		{"invalid", 0, true},
		{"telegram:invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := tc.parseChatID(tt.input)
			if tt.hasError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("expected %d, got %d", tt.expected, result)
				}
			}
		})
	}
}
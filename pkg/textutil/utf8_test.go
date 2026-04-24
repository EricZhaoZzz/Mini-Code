package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeUTF8_ReplacesInvalidBytes(t *testing.T) {
	input := "记忆" + string([]byte{0xff, 0xfe}) + "完成"

	got := SanitizeUTF8(input)

	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8, got %q", got)
	}
	if !strings.Contains(got, "记忆") || !strings.Contains(got, "完成") {
		t.Fatalf("expected original valid content to be preserved, got %q", got)
	}
}

func TestTruncateWithEllipsis_PreservesUTF8(t *testing.T) {
	input := "你好世界🙂编程"

	got := TruncateWithEllipsis(input, 5)

	if got != "你好..." {
		t.Fatalf("expected %q, got %q", "你好...", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8, got %q", got)
	}
}

func TestSplitByRuneLength_PreservesUTF8(t *testing.T) {
	input := "你好世界🙂编程"

	got := SplitByRuneLength(input, 4)

	want := []string{"你好世界", "🙂编程"}
	if len(got) != len(want) {
		t.Fatalf("expected %d segments, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected segment %d to be %q, got %q", i, want[i], got[i])
		}
		if !utf8.ValidString(got[i]) {
			t.Fatalf("expected segment %d to be valid UTF-8, got %q", i, got[i])
		}
	}
}

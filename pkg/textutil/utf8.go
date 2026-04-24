package textutil

import "strings"

const ellipsis = "..."

// SanitizeUTF8 将任意字符串转换为有效 UTF-8，保留可解析内容。
func SanitizeUTF8(s string) string {
	return strings.ToValidUTF8(s, "�")
}

// TruncateRunes 按 rune 数量截断字符串，避免破坏 UTF-8 编码。
func TruncateRunes(s string, maxLen int) string {
	s = SanitizeUTF8(s)
	if maxLen <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// TruncateWithEllipsis 按 rune 数量截断字符串，并在末尾附加省略号。
func TruncateWithEllipsis(s string, maxLen int) string {
	s = SanitizeUTF8(s)
	if maxLen <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= len(ellipsis) {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-len(ellipsis)]) + ellipsis
}

// SplitByRuneLength 按 rune 数量切分字符串，避免破坏 UTF-8 编码。
func SplitByRuneLength(s string, maxLen int) []string {
	s = SanitizeUTF8(s)
	if s == "" {
		return []string{""}
	}
	if maxLen <= 0 {
		return []string{s}
	}

	runes := []rune(s)
	segments := make([]string, 0, (len(runes)+maxLen-1)/maxLen)
	for start := 0; start < len(runes); start += maxLen {
		end := start + maxLen
		if end > len(runes) {
			end = len(runes)
		}
		segments = append(segments, string(runes[start:end]))
	}

	return segments
}

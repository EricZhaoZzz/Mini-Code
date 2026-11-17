package memory

import (
	"path"
	"strings"
)

// frontmatter 是极简 YAML frontmatter 的解析结果：只支持顶层
// `key: value` 与 `key:` 后跟 `- item` 的列表，足以覆盖记忆文件和规则文件。
type frontmatter struct {
	values map[string]string
	lists  map[string][]string
}

func (f frontmatter) get(key string) string { return f.values[key] }

// list 返回列表值；也接受 `key: a, b` 与 `key: [a, b]` 的单行写法。
func (f frontmatter) list(key string) []string {
	if l, ok := f.lists[key]; ok && len(l) > 0 {
		return l
	}
	v := strings.TrimSpace(f.values[key])
	if v == "" {
		return nil
	}
	v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
	var out []string
	for _, item := range splitTopLevelCommas(v) {
		if item = unquote(strings.TrimSpace(item)); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// parseFrontmatter 拆出 frontmatter 与正文；没有 frontmatter 时原样返回正文。
func parseFrontmatter(content string) (frontmatter, string) {
	fm := frontmatter{values: map[string]string{}, lists: map[string][]string{}}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return fm, content
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, content
	}
	header := rest[:end]
	body := rest[end+len("\n---"):]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}

	var currentList string
	for _, line := range strings.Split(header, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") && currentList != "" {
			fm.lists[currentList] = append(fm.lists[currentList], unquote(strings.TrimSpace(trimmed[2:])))
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		indented := line != strings.TrimLeft(line, " \t")
		if indented {
			// 嵌套键（如 metadata.type）拍平为子键名，未被顶层占用时生效
			if _, exists := fm.values[key]; !exists && value != "" {
				fm.values[key] = unquote(value)
			}
			continue
		}
		currentList = ""
		if value == "" {
			currentList = key
			continue
		}
		fm.values[key] = unquote(value)
	}
	return fm, strings.TrimLeft(body, "\n")
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// splitTopLevelCommas 按逗号切分，但不切开 `{a,b}` 中的逗号。
func splitTopLevelCommas(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// matchGlob 判断以 `/` 分隔的相对路径是否匹配 pattern，
// 支持 `**`（任意层目录，含零层）与单层 `{a,b}` 花括号展开。
func matchGlob(pattern, rel string) bool {
	for _, p := range expandBraces(pattern) {
		if matchSegments(strings.Split(p, "/"), strings.Split(rel, "/")) {
			return true
		}
	}
	return false
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}
	}
	closeIdx := strings.IndexByte(pattern[open:], '}')
	if closeIdx < 0 {
		return []string{pattern}
	}
	closeIdx += open
	var out []string
	for _, alt := range strings.Split(pattern[open+1:closeIdx], ",") {
		out = append(out, expandBraces(pattern[:open]+alt+pattern[closeIdx+1:])...)
	}
	return out
}

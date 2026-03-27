package memory

import (
	"fmt"
	"strings"
)

const maxPromptSuffixChars = 2000

// BuildPromptSuffix 构建记忆注入片段，追加在 system prompt 末尾
// 截断优先级：Session Memory > project LongTerm > user LongTerm
func (s *Store) BuildPromptSuffix(workspace string) string {
	var sb strings.Builder

	// 1. 用户偏好（user scope）
	userMems, _ := s.LoadLongTermMemory("user", "", 20)
	if len(userMems) > 0 {
		sb.WriteString("\n\n## 用户偏好\n")
		for _, m := range userMems {
			sb.WriteString("- ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}

	// 2. 项目记忆（project scope）
	projMems, _ := s.LoadLongTermMemory("project", workspace, 20)
	if len(projMems) > 0 {
		sb.WriteString("\n## 项目记忆\n")
		for _, m := range projMems {
			sb.WriteString("- ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}

	// 3. Session Memory（近 72h 摘要）
	sessions, _ := s.LoadSessionMemory(workspace, 5)
	if len(sessions) > 0 {
		sb.WriteString("\n## 近期上下文（过去 72 小时）\n")
		for _, m := range sessions {
			sb.WriteString(fmt.Sprintf("- %s: %s\n",
				m.CreatedAt.Format("2006-01-02"), m.Content))
		}
	}

	result := sb.String()

	// 截断到最大长度
	if len(result) > maxPromptSuffixChars {
		result = result[:maxPromptSuffixChars]
		// 确保不截断在汉字中间
		result = strings.TrimRight(result, "\n-: ")
		result += "\n... (记忆已截断)"
	}

	return result
}
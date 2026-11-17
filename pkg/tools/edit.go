package tools

import (
	"fmt"
	"os"
	"strings"
)

type ReplaceInFileArguments struct {
	Path       string `json:"path" validate:"required" jsonschema:"required" jsonschema_description:"要修改的工作区内相对路径"`
	Old        string `json:"old" validate:"required" jsonschema:"required" jsonschema_description:"要替换的原始文本，必须在文件中唯一；不唯一时请包含更多上下文"`
	New        string `json:"new" jsonschema_description:"替换后的文本"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema_description:"为 true 时替换全部匹配项（如批量重命名）"`
}

func ReplaceInFile(args interface{}) (interface{}, error) {
	var params ReplaceInFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if params.Old == "" {
		return nil, fmt.Errorf("old 不能为空")
	}

	targetPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	content := string(data)
	oldText, newText := matchLineEndings(content, params.Old, params.New)

	count := strings.Count(content, oldText)
	if count == 0 {
		return nil, fmt.Errorf("未找到要替换的内容（old 必须与原文逐字一致，包括缩进与空白）")
	}
	if count > 1 && !params.ReplaceAll {
		return nil, fmt.Errorf("old 在文件中匹配到 %d 处，无法确定替换哪一处：请在 old 中包含更多上下文使其唯一，或设置 replace_all=true 替换全部", count)
	}

	n := 1
	if params.ReplaceAll {
		n = -1
	}
	updated := strings.Replace(content, oldText, newText, n)
	if err := writeFileAtomically(targetPath, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("写回文件失败: %w", err)
	}

	msg := fmt.Sprintf("已替换文件内容: %s", displayPath(targetPath))
	if count > 1 {
		msg = fmt.Sprintf("已替换文件内容: %s（共 %d 处）", displayPath(targetPath), count)
	}
	return withLazyInstructions(targetPath, msg), nil
}

// matchLineEndings 让 old/new 的换行符与文件保持一致。
//
// 模型输出几乎总是 LF，而 Windows 上的文件常为 CRLF：
// 不处理时多行 old 永远匹配不上，模型只能反复重试或退而整文件重写。
// 仅当原样匹配失败且文件确实使用 CRLF 时才转换，new 同步转换，
// 避免在 CRLF 文件里混入 LF 行。
func matchLineEndings(content, oldText, newText string) (string, string) {
	if strings.Contains(content, oldText) || !strings.Contains(content, "\r\n") || !strings.Contains(oldText, "\n") {
		return oldText, newText
	}
	toCRLF := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	return toCRLF(oldText), toCRLF(newText)
}

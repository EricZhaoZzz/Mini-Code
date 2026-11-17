package tools

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

// ToolExecutor 执行工具。ctx 随任务取消（双击 Esc）而取消，
// 耗时工具（shell、网络、子 Agent）必须响应它，否则取消只能等工具自然结束。
type ToolExecutor func(ctx context.Context, args interface{}) (interface{}, error)

var (
	Definitions []openai.Tool
	Executors   map[string]ToolExecutor
)

func init() {
	Executors = make(map[string]ToolExecutor)

	// 文件操作
	register("write_file", "创建文件或整体覆盖已有文件；局部修改请用 replace_in_file", WriteFileArguments{}, WriteFile)
	register("read_file", "读取本地文件", ReadFileArguments{}, ReadFile)
	register("list_files", "列出目录下的文件", ListFilesArguments{}, ListFiles)
	register("search_in_files", "在目录下按字面文本搜索（非正则）", SearchInFilesArguments{}, SearchInFiles)
	registerCtx("run_shell", "在工作区目录运行 shell 命令（默认 120 秒超时；可访问工作区外，勿用于绕过路径限制）", RunShellArguments{}, RunShellContext)
	register("replace_in_file", "在文件中局部替换文本：old 必须与原文逐字一致且唯一；需要批量替换时设置 replace_all=true", ReplaceInFileArguments{}, ReplaceInFile)

	// 文件操作扩展
	register("rename_file", "重命名/移动文件或目录", RenameFileArguments{}, RenameFile)
	register("delete_file", "删除文件或目录（交互模式下会请求用户确认）", DeleteFileArguments{}, DeleteFile)
	register("copy_file", "复制文件或目录", CopyFileArguments{}, CopyFile)
	register("create_directory", "创建目录", CreateDirectoryArguments{}, CreateDirectory)
	register("get_file_info", "获取文件或目录的详细信息（包括大小、修改时间、SHA256等）", GetFileInfoArguments{}, GetFileInfo)

	// 网络操作
	registerCtx("download_file", "下载远程文件到本地", DownloadFileArguments{}, DownloadFileContext)
	registerCtx("web_search", "搜索互联网获取实时信息", WebSearchArguments{}, WebSearchContext)

	// Git 操作
	register("git_status", "查看 Git 仓库状态", GitStatusArguments{}, GitStatus)
	register("git_diff", "查看 Git 差异", GitDiffArguments{}, GitDiff)
	register("git_log", "查看 Git 提交历史", GitLogArguments{}, GitLog)

	// 记忆工具
	register("remember", "写入或更新一条跨会话的自动记忆（每条一个文件，并同步 MEMORY.md 索引）", RememberArguments{}, Remember)
	register("forget", "按名称删除一条自动记忆", ForgetArguments{}, Forget)
	register("recall", "检索自动记忆：按 name 读全文，按 query 关键词搜索，都不填则列出全部", RecallArguments{}, Recall)

	// Agent 调度工具
	registerCtx("dispatch_agent", "调用专化子 Agent 执行子任务（reviewer 或 researcher）", DispatchAgentArguments{}, DispatchAgentContext)
}

// register 注册不需要感知取消的快速工具（本地文件、git、记忆等）。
func register(name, description string, args interface{}, fn func(args interface{}) (interface{}, error)) {
	registerCtx(name, description, args, func(_ context.Context, a interface{}) (interface{}, error) {
		return fn(a)
	})
}

// registerCtx 注册需要响应取消的工具。
func registerCtx(name, description string, args interface{}, executor ToolExecutor) {
	Definitions = append(Definitions, openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  generateSchema(args),
		},
	})
	Executors[name] = executor
}

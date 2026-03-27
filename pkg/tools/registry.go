package tools

import "github.com/sashabaranov/go-openai"

type ToolExecutor func(args interface{}) (interface{}, error)

var (
	Definitions []openai.Tool
	Executors   map[string]ToolExecutor
)

func init() {
	Executors = make(map[string]ToolExecutor)

	// 文件操作
	register("write_file", "在本地创建或修改文件", WriteFileArguments{}, WriteFile)
	register("read_file", "读取本地文件", ReadFileArguments{}, ReadFile)
	register("list_files", "列出目录下的文件", ListFilesArguments{}, ListFiles)
	register("search_in_files", "在目录下搜索文本", SearchInFilesArguments{}, SearchInFiles)
	register("run_shell", "运行 shell 命令", RunShellArguments{}, RunShell)
	register("replace_in_file", "在文件中局部替换文本，只替换第一个匹配项", ReplaceInFileArguments{}, ReplaceInFile)

	// 文件操作扩展
	register("rename_file", "重命名/移动文件或目录", RenameFileArguments{}, RenameFile)
	register("delete_file", "删除文件或目录（需要确认）", DeleteFileArguments{}, DeleteFile)
	register("copy_file", "复制文件或目录", CopyFileArguments{}, CopyFile)
	register("create_directory", "创建目录", CreateDirectoryArguments{}, CreateDirectory)
	register("get_file_info", "获取文件或目录的详细信息（包括大小、修改时间、SHA256等）", GetFileInfoArguments{}, GetFileInfo)

	// 网络操作
	register("download_file", "下载远程文件到本地", DownloadFileArguments{}, DownloadFile)

	// Git 操作
	register("git_status", "查看 Git 仓库状态", GitStatusArguments{}, GitStatus)
	register("git_diff", "查看 Git 差异", GitDiffArguments{}, GitDiff)
	register("git_log", "查看 Git 提交历史", GitLogArguments{}, GitLog)
}

func register(name, description string, args interface{}, executor ToolExecutor) {
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

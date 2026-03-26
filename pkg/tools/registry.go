package tools

import "github.com/sashabaranov/go-openai"

type ToolExecutor func(args interface{}) (interface{}, error)

var (
	Definitions []openai.Tool
	Executors   map[string]ToolExecutor
)

func init() {
	Executors = make(map[string]ToolExecutor)

	register("write_file", "在本地创建或修改文件", WriteFileArguments{}, WriteFile)
	register("read_file", "读取本地文件", ReadFileArguments{}, ReadFile)
	register("list_files", "列出目录下的文件", ListFilesArguments{}, ListFiles)
	register("search_in_files", "在目录下搜索文本", SearchInFilesArguments{}, SearchInFiles)
	register("run_shell", "运行 shell 命令", RunShellArguments{}, RunShell)
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

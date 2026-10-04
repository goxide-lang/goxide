package main

import "github.com/spf13/cobra"

// configureCLIHelp changes presentation only. Rendering help must never inspect
// projects, resolve modules, execute programs, or open the script trust store.
func configureCLIHelp(root *cobra.Command) {
	type helpText struct{ short, long, example string }
	texts := map[string]helpText{
		"hgo": {
			"将 goxide 源码编译为普通 Go",
			"编译、运行和测试 goxide 源码，或迁移已有 Go 项目。\n长选项使用 --name，短选项使用 -x；所有选项放在输入路径之前。\n旧式 -go、-offline 等拼写不再接受，请改为 --go、--offline。",
			"  hgo run --go /absolute/path/to/go main.hgo\n  hgo help scripts\n  hgo completion bash",
		},
		"hgo build": {
			"编译文件、目录或项目并保留构建产物",
			"编译指定输入。项目构建会打印保留的二进制路径；用 --bin、--example 或 --lib 选择项目目标。",
			"  hgo build -o ./app main.hgo\n  hgo build --bin app --offline ./project",
		},
		"hgo run": {
			"编译并运行程序",
			"运行指定输入。程序参数必须放在输入路径后的 -- 之后，并原样传给程序。\n路径以短横线开头时，先用 -- 结束 hgo 选项，再用第二个 -- 分隔程序参数。",
			"  hgo run main.hgo -- --port 8080\n  hgo run -- -input.hgo -- --verbose",
		},
		"hgo test": {
			"测试目录或项目",
			"转译目录或项目并运行带 race 检测的 Go 测试。此命令不接受单个 .hgo 文件或额外 Go 测试参数。",
			"  hgo test --go /absolute/path/to/go ./project\n  hgo test --offline --locked ./project",
		},
		"hgo transpile": {
			"将源码转译为 Go 文件或模块",
			"转译指定文件、目录或项目。单文件可用 --source-map 输出源码映射；目录输入可用 -o 指定生成目录。",
			"  hgo transpile -o main.go --source-map main.map.json main.hgo\n  hgo transpile -o ./generated ./source",
		},
		"hgo expand": {
			"查看单文件的宏展开结果",
			"展开单个 .hgo 文件；此命令不接受目录或项目输入。用 -o 将结果写入文件。",
			"  hgo expand main.hgo\n  hgo expand -o expanded.go main.hgo",
		},
		"hgo import-go": {
			"查看 Go 模块迁移计划或迁移到新目录",
			"显式选择库或可执行目标。未指定 --out 时只展示计划；迁移在新目录验证成功后发布，不覆盖原工程。\n重复 --bin 可选择多个可执行目标。已有单模块 vendor 按原生 Go vendor 视图保留并校验。",
			"  hgo import-go --plan --lib . ./source\n  hgo import-go --out ./migrated --offline --bin app=cmd/app ./source",
		},
		"hgo scripts": {
			"查看和管理构建脚本的显式授权",
			"每个动作都需要显式项目路径，不会默认信任当前目录。兼容 --action ACTION 入口；省略动作时查看计划。\nhelp 不解析依赖或读取授权记录；plan 可能解析依赖，但不执行脚本。",
			"  hgo scripts plan --offline ./project\n  hgo scripts allow --subject EXACT_SHA256 ./project\n  hgo scripts --action plan ./project",
		},
		"hgo scripts plan": {
			"查看当前脚本主体与授权状态",
			"输出当前冻结执行主体的 JSON 计划，不编译或执行脚本。依赖解析可能访问网络，可用 --offline 和 --locked 限制。",
			"  hgo scripts plan --offline --locked ./project",
		},
		"hgo scripts trust-project": {
			"显式信任指定根项目的构建脚本",
			"记录根项目授权。脚本不受沙箱约束；第三方执行主体仍需逐一显式允许。",
			"  hgo scripts trust-project ./project",
		},
		"hgo scripts allow": {
			"允许计划中一个精确执行主体",
			"--subject 必须是当前冻结计划中的完整 SHA-256。此动作不会顺带信任根项目；已允许的脚本不受沙箱约束。",
			"  hgo scripts allow --subject EXACT_SHA256 ./project",
		},
		"hgo scripts revoke": {
			"撤销一个精确执行主体的授权",
			"撤销 --subject 指定的完整 SHA-256 授权。已有脚本缓存不能绕过后续授权检查。",
			"  hgo scripts revoke --subject EXACT_SHA256 ./project",
		},
		"hgo scripts forget-project": {
			"撤销指定根项目的信任",
			"删除根项目信任记录；已有缓存不能绕过后续授权检查。",
			"  hgo scripts forget-project ./project",
		},
		"hgo scripts clean": {
			"清理工具拥有的构建脚本缓存",
			"仅清理通过所有权校验的脚本缓存，保留本地授权、源码及已保留的目标产物。",
			"  hgo scripts clean ./project",
		},
		"hgo help": {
			"查看命令帮助",
			"查看根命令或指定子命令的帮助，不调用业务服务。",
			"  hgo help\n  hgo help scripts allow",
		},
		"hgo completion": {
			"生成 shell 补全脚本",
			"将指定 shell 的补全脚本输出到标准输出；不解析项目、下载依赖或读取脚本授权。",
			"  hgo completion bash\n  hgo completion zsh",
		},
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		texts["hgo completion "+shell] = helpText{
			"生成 " + shell + " 补全脚本",
			"将 " + shell + " 补全脚本输出到标准输出，按 shell 的配置方式加载；生成过程不调用业务服务。",
			"  hgo completion " + shell,
		}
	}
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		if text, ok := texts[c.CommandPath()]; ok {
			c.Short, c.Long, c.Example = text.short, text.long, text.example
		}
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(root)
	root.SetHelpTemplate(`{{with .Long}}{{.}}{{else}}{{.Short}}{{end}}

用法：
  {{.UseLine}}
{{if .HasAvailableSubCommands}}
子命令：
{{range .Commands}}{{if not .Hidden}}  {{rpad .Name .NamePadding}} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}
选项：
{{.LocalFlags.FlagUsages}}{{end}}{{if .HasAvailableInheritedFlags}}
继承选项：
{{.InheritedFlags.FlagUsages}}{{end}}{{with .Example}}
示例：
{{.}}
{{end}}{{if .HasAvailableSubCommands}}
使用 "{{.Root.Name}} help COMMAND" 或子命令的 --help 查看帮助。
{{end}}`)
}

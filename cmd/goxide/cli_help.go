package main

import "github.com/spf13/cobra"

// configureCLIHelp changes presentation only. Rendering help must never inspect
// projects, resolve modules, execute programs, or open the script trust store.
func configureCLIHelp(root *cobra.Command) {
	type helpText struct{ short, long, example string }
	texts := map[string]helpText{
		"goxide": {
			"将 Goxide 源码编译为普通 Go",
			"编译、运行和测试 Goxide 源码。\n长选项使用 --name，短选项使用 -x；所有选项放在输入路径之前。\n旧式 -go、-offline 等拼写不再接受，请改为 --go、--offline。",
			"  goxide --version\n  goxide run --go /absolute/path/to/go main.gox\n  goxide help build\n  goxide completion bash",
		},
		"goxide build": {
			"编译文件、目录或项目并保留构建产物",
			"省略输入时，从当前目录向上发现 goxide.toml。项目构建会打印稳定的二进制路径；用 --bin 选择项目二进制目标。缺省编译目标为宿主，可用 --target GOARCH-unknown-GOOS 跨目标构建。",
			"  goxide build -o ./app main.gox\n  goxide build --bin app --offline ./project",
		},
		"goxide run": {
			"编译并运行程序",
			"省略输入时，从当前目录向上发现项目。传递程序参数时，显式输入路径（当前目录可写 .）后使用 -- 分隔。\n路径以短横线开头时，先用 -- 结束 goxide 选项，再用第二个 -- 分隔程序参数。",
			"  goxide run main.gox -- --port 8080\n  goxide run -- -input.gox -- --verbose",
		},
		"goxide test": {
			"测试目录或项目",
			"转译目录或项目并运行带 race 检测的 Go 测试；省略输入时从当前目录向上发现项目。此命令不接受单个 .gox 文件或额外 Go 测试参数。",
			"  goxide test --go /absolute/path/to/go ./project\n  goxide test --offline --locked ./project",
		},
		"goxide transpile": {
			"就地生成 Go；别名 gen 推荐用于 go generate",
			"gen 是 transpile 的别名。默认将当前包或指定 .gox 写成同目录的 *_gox.go；只读加载同包手写 Go 声明，不编译或运行程序。\n推荐 //go:generate goxide gen .；用 -o - 将单个生成文件写到 stdout，用 -o PATH 保留显式文件或模块目录导出。\n原地和 stdout 输出包含生成标记与 //line 行指令；--source-map JSON 输出尚未实现。",
			"  goxide gen .\n  goxide gen -o - order.gox\n  goxide transpile -o ./generated ./source",
		},
		"goxide expand": {
			"查看单文件的宏展开结果",
			"展开单个 .gox 文件；此命令不接受目录或项目输入。用 -o 将结果写入文件。",
			"  goxide expand main.gox\n  goxide expand -o expanded.go main.gox",
		},
		"goxide env": {
			"查询宿主、Go 环境和 Goxide 编译目标",
			"显示三种目标及所选 Go 工具链；--target 仅选择 Goxide 工具链目标。名称参数查询原始进程环境，--list 列出目标值域。该命令不执行源码或解析项目。",
			"  goxide env --json --target linux/amd64\n  goxide env --list\n  goxide env --json GOOS APP_EDITION",
		},
		"goxide import-go": {
			"查看 Go 模块迁移计划或迁移到新目录",
			"显式选择库或可执行目标。未指定 --out 时只展示计划；迁移在新目录验证成功后发布，不覆盖原工程。\n重复 --bin 可选择多个可执行目标。原生 vendor 校验后物化为普通本地模块，保留供应字节。--offline 关闭远端探测与下载，仅使用本地文件/缓存；无参数 TTY 一次选择，非 TTY 默认 local。--source MODULE=remote 只接受清单证实的精确版本；差异须逐模块确认或显式 --accept-source-diff MODULE。缓存不足的离线 remote 选择失败。",
			"  goxide import-go --plan --lib . ./source\n  goxide import-go --out ./migrated --offline --bin app=cmd/app ./source",
		},
		"goxide scripts": {
			"查看和管理构建脚本的显式授权",
			"每个动作都需要显式项目路径，不会默认信任当前目录。兼容 --action ACTION 入口；省略动作时查看计划。\nhelp 不解析依赖或读取授权记录；plan 可能解析依赖，但不执行脚本。",
			"  goxide scripts plan --offline ./project\n  goxide scripts allow --subject EXACT_SHA256 ./project\n  goxide scripts --action plan ./project",
		},
		"goxide scripts plan": {
			"查看当前脚本主体与授权状态",
			"输出当前冻结执行主体的 JSON 计划，不编译或执行脚本。依赖解析可能访问网络，可用 --offline 和 --locked 限制。",
			"  goxide scripts plan --offline --locked ./project",
		},
		"goxide scripts trust-project": {
			"显式信任指定根项目的构建脚本",
			"记录根项目授权。脚本不受沙箱约束；第三方执行主体仍需逐一显式允许。",
			"  goxide scripts trust-project ./project",
		},
		"goxide scripts allow": {
			"允许计划中一个精确执行主体",
			"--subject 必须是当前冻结计划中的完整 SHA-256。此动作不会顺带信任根项目；已允许的脚本不受沙箱约束。",
			"  goxide scripts allow --subject EXACT_SHA256 ./project",
		},
		"goxide scripts revoke": {
			"撤销一个精确执行主体的授权",
			"撤销 --subject 指定的完整 SHA-256 授权。已有脚本缓存不能绕过后续授权检查。",
			"  goxide scripts revoke --subject EXACT_SHA256 ./project",
		},
		"goxide scripts forget-project": {
			"撤销指定根项目的信任",
			"删除根项目信任记录；已有缓存不能绕过后续授权检查。",
			"  goxide scripts forget-project ./project",
		},
		"goxide scripts clean": {
			"清理工具拥有的构建脚本缓存",
			"仅清理通过所有权校验的脚本缓存，保留本地授权、源码及已保留的目标产物。",
			"  goxide scripts clean ./project",
		},
		"goxide help": {
			"查看命令帮助",
			"查看根命令或指定子命令的帮助，不调用业务服务。",
			"  goxide help\n  goxide help build",
		},
		"goxide completion": {
			"生成 shell 补全脚本",
			"将指定 shell 的补全脚本输出到标准输出；不解析项目、下载依赖或读取脚本授权。",
			"  goxide completion bash\n  goxide completion zsh",
		},
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		texts["goxide completion "+shell] = helpText{
			"生成 " + shell + " 补全脚本",
			"将 " + shell + " 补全脚本输出到标准输出，按 shell 的配置方式加载；生成过程不调用业务服务。",
			"  goxide completion " + shell,
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

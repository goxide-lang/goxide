# Goxide

Goxide 为 Go 项目提供 enum、match、声明宏、Option/Result、`?` 和显式派生，生成可直接交给 Go 工具链的源码。

从当前源码安装：准备 Go 1.27，并在构建和运行 Goxide 时使用同一 Go 工具链：

```sh
git clone https://github.com/goxide-lang/goxide.git
cd goxide
GOTOOLCHAIN=local GOWORK=off go run ./scripts/build.go -o ./bin/goxide
export PATH="$PWD/bin:$PATH"
```

构建脚本会写入过程宏所需的语法 SDK 内容身份。Windows 将输出改为 `./bin/goxide.exe`，并把该目录加入 `PATH`。

在已有 Go 包中加入 `choice.gox`，`package` 与手写 Go 一致：

```gox
package main
enum Choice { Take(int), Skip }
func answer() int { return match Choice::Take(42) { Choice::Take(n) => n, Choice::Skip => 0 } }
```

在同包任意手写 `.go` 文件加入：

```go
//go:generate goxide gen .
```

运行 `go generate ./...`，再用 `go build ./...` 构建。`choice.gox` 生成同目录的 `choice_gox.go`，手写 Go 可直接调用 `answer()`。

继续阅读 [已有 Go 项目五分钟上手](docs/using-goxide-gen.md)、[当前能力状态表](docs/mvp-gap-audit.md) 或试用 [浏览器 Playground](https://goxide-lang.github.io/playground.html)。

## 命令入口

- 在已有 Go 项目里局部使用：`goxide gen .`；`gen` 是 `transpile` 的别名，默认在源码旁生成 `_gox.go`。单文件输入为 `goxide gen choice.gox`，标准输出用 `goxide gen -o - choice.gox`。
- 由 Goxide 管理构建与执行：`goxide build INPUT`、`goxide run INPUT`、`goxide test DIRECTORY`。输入可为单文件、包目录或带最小 `goxide.toml` 的项目，`test` 要求目录或项目。
- 查看选项与补全：`goxide --help`、`goxide gen --help`、`goxide completion bash`。选项放在输入前；运行参数写在 `--` 后。

需要 `std` 或 `serde` 的生成代码使用当前编译器 `go.mod` 中的版本。缺少依赖时 `gen` 给出 `go get` 命令，由使用者更新项目依赖。

## 仓库导览

| 路径 | 内容 |
| --- | --- |
| [cmd/goxide](cmd/goxide/) | 编译器 CLI |
| [internal/syntax](internal/syntax/)、[expand](internal/expand/)、[resolve](internal/resolve/)、[check](internal/check/)、[lower](internal/lower/) | 解析、宏展开、解析名字、语义检查与 Go 生成 |
| [internal/compile](internal/compile/) | 多包与最小项目编排 |
| [examples](examples/) | 可运行的语言与互操作示例 |
| [tests](tests/) | 端到端验收 |
| [docs](docs/) | [语言设计](docs/design/overview.md)、[已确认决策](docs/design/decisions.md)与 [RFC](docs/rfcs/README.md) |
| [website](website/) | 网站、示例目录与浏览器编译器源码 |

公共支持库分别在 [std](https://github.com/goxide-lang/std) 和 [serde](https://github.com/goxide-lang/serde)。未实现的功能与后续阶段统一列在[当前能力状态表](docs/mvp-gap-audit.md)。

## 开发检查

```sh
GOWORK=off GOTOOLCHAIN=local go build ./...
GOWORK=off GOTOOLCHAIN=local go test -p=1 -count=1 -timeout=90m ./internal/... ./tests/...
GOWORK=off GOTOOLCHAIN=local go vet ./internal/... ./tests/...
gofmt -l cmd internal tests
python3 -B scripts/check_design.py --docs-only
```

许可证：[Mozilla Public License 2.0](LICENSE)。

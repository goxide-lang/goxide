# Hyper Go（hgo）

hgo 是面向 Go 生态的语言设计：增加显式 trait、变体枚举、模式匹配和卫生宏，生成标准 Go 源码，并提供共享语义分析前端的 LSP。

架构以 **转译器 + 转译前 linter + 普通 Go 支持库** 为边界。最低目标语言及工具链版本为 **Go 1.27**，使用其新增的泛型方法表达带独立返回类型的锁回调等 API。

本实现分支已有可运行的有限宏与 serde 原型，见[运行方式与准确支持范围](IMPLEMENTATION.md)。完整 hgo 编译器与 LSP 尚未实现；其余设计文档中的语法不因此成为可运行承诺。

目录入口已扩展到现有 Go module 内的多文件包和有限跨包调用；[两包应用](examples/packages/main.hgo)组合 JSON、enum、Result、错误链和原生 Go 日志。[三包应用](examples/qualified_packages/main.hgo)进一步演示 qualified enum match 与导入 Result 直接 `?`。目录模式的支持子集与单文件不同，构建命令、公开入口和限制见[实现记录](IMPLEMENTATION.md#后续阶段多文件包与-go-module-入口)。

固定格式 `print!` / `println!` 和具体类型 `#[derive(Default)]` 已可运行；[启动配置示例](examples/prototype/print_default.hgo)组合显式默认值、JSON、Clone、Debug 和宏打印：

```sh
go run ./cmd/hgo run examples/prototype/print_default.hgo
```

常量 enum 的唯一值子集也已可运行：[Go webhook 互操作示例](examples/raw_enum/main.hgo)保留未知 string/数字值并用开放 match 处理。支持显式整数/string 常量；同值别名待决，raw 派生未实现，准确边界见 [常量 enum 验收](docs/t0-acceptance.md#常量-enum-唯一值子集增量)。

Default 不改变 Go 零初始化，打印只接受已登记协议。完整限制及 T0 必需/后置划分见 [验收矩阵](docs/t0-acceptance.md#print--println-与-default-增量)。

项目暂称 hgo / Hyper Go，当前远端仓库名为 `goxide`。换机器继续时先看 [Roadmap](ROADMAP.md) 和[决策登记](docs/design/decisions.md)：分别记录实现进度，以及**已确认 / 暂定 / 未定**的设计边界。

用户在自己的 C 包 `.hgo` 文件中写 `declare impl AorB::XXX for BBB::Type;`。声明只由 hgo 读取，所需 marker 仅加入本次 hgo 的隔离构建视图；不修改 AorB、BBB 的原源码或共享缓存，既有纯 Go 项目完全保持原样。

## 设计文档

- [设计总纲](docs/design/overview.md)：目标、决策状态、各部分边界与实现顺序。
- [编译器、LSP 与自举](docs/design/compiler-and-tooling.md)：Go 工具复用边界、独立前端、启动实现与自举路线。
- [RFC 0001：Go 兼容的别名风险诊断](docs/rfcs/0001-scoped-borrow-views.md)：Go 兼容默认、具体风险 warning、业务场景与机制证据；旧严格默认已否决。
- [指针拆分与引用](docs/design/pointers-and-borrows.md)：Option、读写借用、Arc、Ptr、Box、解引用及尚未闭合的权限规则。
- [显式 trait 与多接口实现](docs/design/traits.md)：无体声明补充 marker、自定义 wrapper 实现、同名方法与使用处转换。
- [Go原生类型的显式适配](docs/design/go-interop.md)：普通 `.hgo` 中的显式声明、原生方法校验、伴生源码与独立构建视图。
- [原生 Go API、安全声明与裸指针](docs/design/foreign-api.md)：原生 unsafe 视图、declare、Ptr/AsPtr 和 `.d.hgo` 声明分发。
- [关联类型](docs/design/associated-types.md)：普通关联类型的最小设计及 Go 降低方式。
- [锁与作用域回调](docs/design/synchronization.md)：指针形式的锁句柄、Go 1.27 泛型返回值、解锁和 linter 边界。
- [变体枚举与模式匹配](docs/design/enums-and-match.md)：Option / Result、穷尽匹配、值型 case、Go 零值与边界验证。
- [卫生宏与基础派生](docs/design/macros.md)：参考 Rust 的声明宏、print / println、Default / Clone / Debug、符号卫生和 Go 降低；用户过程宏延期。
- [基础包与 safe Go 标准库](docs/design/standard-library.md)：针对现有 Go std 的逐 API 声明与包装，以及泛型 slice 的权限、复制和别名。
- [quote / syn 与语法工具](docs/design/syntax-tooling.md)：额外语法包、普通代码生成工具、上下文来源和后续过程宏的边界。

总纲负责导航与记录决策，详细文档负责语义、示例、诊断和验收条件。新增专题时从总纲链接，避免把完整设计重复写在多个位置。

原生指针桥接候选见 [RFC 0002](docs/rfcs/0002-native-pointer-bridge.md)；别名/借用风险分析已独立为长期工作线，不是 MVP 条件。

当前场景评审见 [RFC 0003：Go 指针职责与 MVP 最小集合](docs/rfcs/0003-pointer-roles-and-mvp.md)。

当前用户优先级：宏、enum/match、只读/可写访问为 T0（分批交付）；trait 双路径研究见 [RFC 0004](docs/rfcs/0004-trait-lowering-paths.md)。T0 不要求恢复借用分析或先实现全部智能指针。

文档标签与独立实现会话交接见 [MVP 文档基线](docs/mvp-doc-baseline.md)。标签记录已确认方向和未决项，不代表完整规范定稿或编译器已实现。

最新优先任务是 serde，用户已决定仿照 serde-rs：核心协议/Visitor 与派生、独立 serde-json 及未来 serde-yaml；Value 属于格式层，不设强制核心树。首版 JSON 的实现契约及待实现验收见 [RFC 0006](docs/rfcs/0006-serde-core-and-formats.md)，协议设计仍可演进；本分支已实现其中具体类型 Go helper/Visitor 与 JSON 子集，准确边界见[原型说明](IMPLEMENTATION.md)。

## 验证设计示例

```sh
python3 scripts/check_design.py --docs-only
python3 scripts/check_design.py
```

完整验证需要 Python 3.10+、Go 1.27 或更新工具链及可用的 race detector。范围与限制见 [tests 说明](tests/README.md)：验证的是文档中的 Go 机制，不能证明尚不存在的 hgo 前端正确。

## 许可证

本项目采用 [Mozilla Public License 2.0](LICENSE)（SPDX：`MPL-2.0`）。


## 安装和在仓库外使用

在仓库根目录用真实 Go 1.27+ 编译器安装；无需下载第三方依赖。`GOBIN` 指向希望存放命令的绝对目录：

```sh
repo="$PWD"
work="$(mktemp -d)"
mkdir "$work/bin"
GOBIN="$work/bin" GOTOOLCHAIN=local GOWORK=off GOPROXY=off go install ./cmd/hgo
cd "$work"
"$work/bin/hgo" run -runtime "$repo" "$repo/examples/prototype/conditional_log.hgo"
"$work/bin/hgo" transpile -o "$work/generated" "$repo/examples/macro_packages"
cd "$work/generated"
go test -race ./...
go build .
```

单文件 run/build 在仓库外使用 `-runtime /absolute/goxide`。目录输入必须属于已有 `go.mod`；使用 serde 等 runtime 包的应用需要 `require github.com/hxsf/goxide v0.0.0` 和本地 `replace github.com/hxsf/goxide => /absolute/goxide`（示例已有相对 replace，转译时会重定位）。产物是可独立调用 Go 工具构建的模块，仍依赖 go.mod 声明的支持库，并非自包含 runtime 副本。

目录命令支持 `-tags feature,other`，按 Go 规则选择 `.hgo`/native `.go` 的平台后缀、`//go:build` 和旧 `// +build`；忽略 `_`/`.` 开头的源文件。`hgo test` 的选择和 importer 同时启用 `race`，生成测试也使用 `-race`。输出是这一次选择的构建视图，改变 tags、race 配置或平台必须重新转译，不可把相同产物当作全配置源码。当前目录模式拒绝非宿主 GOOS/GOARCH；`.hgo` 测试文件仍未实现，请使用 native Go 测试。

同包多文件可以使用已有有限读/写引用及函数调用，擦除前按完整包签名检查权限；跨包新引用签名暂不导出，普通 Go `*T` API 保持既有边界。`-go /absolute/go` 可绕过 PATH 中同名非编译器程序；类型 importer 使用构建 hgo 的 GOROOT 工具链，目录 run/build/test 要求 `-go` 的版本与它一致。

# Goxide 使用指南

## 选择使用方式

Goxide 为 Go 增加 enum、match、声明宏、派生和显式协议，生成普通 Go 源码。可以先在一个 Go 包中使用，也可以用 `goxide.toml` 组织一个多包项目。

| 你要做什么 | 工作方式 | 下一步 |
| --- | --- | --- |
| 在已有 Go 项目中加入少量 `.gox` | `goxide gen .` 生成同目录的 `_gox.go`，继续使用 `go build` / `go test` | [已有 Go 项目指南](using-goxide-gen.md) |
| 用 Goxide 管理源码、依赖和可执行目标 | `goxide.toml` 声明项目，`goxide build/run/test` 完成生成与 Go 工具链调用 | [第一个 Goxide 项目](#第一个-goxide-项目) |
| 先看看代码和生成结果 | 浏览器内编辑与转译 | [Go 与 Goxide 混合快速开始](https://goxide-lang.github.io/learn/?stage=mixed) |

两种工作方式共用语言和条件编译规则。`gen` 是 `transpile` 的别名；生成后的文件仍由普通 Go 工具处理。浏览器 Playground 用于查看生成代码，本地 CLI 负责构建、运行和测试。

推荐阅读顺序：[安装](#安装) → [混合使用](using-goxide-gen.md) 或 [独立项目](#第一个-goxide-项目) → [Feature](#feature) → [目标与环境](#目标与环境)。

## 安装

`v0.0.1` 是实验性预发布，尚非正式稳定发行。后续版本即使只增加 patch 或 minor，仍可能对语言、CLI、配置格式和生成代码作出不兼容变更。

### 下载 CLI v0.0.1

按运行 CLI 的机器选择平台与 CPU 架构。归档包含 Goxide 可执行文件、许可证和安装说明；Go 工具链需要单独安装。

| 平台 | CPU 架构 | 下载 |
| --- | --- | --- |
| macOS | Intel（amd64） | [下载 tar.gz](https://github.com/goxide-lang/goxide/releases/download/v0.0.1/goxide_v0.0.1_darwin_amd64.tar.gz) |
| macOS | Apple silicon（arm64） | [下载 tar.gz](https://github.com/goxide-lang/goxide/releases/download/v0.0.1/goxide_v0.0.1_darwin_arm64.tar.gz) |
| Linux | x86-64（amd64） | [下载 tar.gz](https://github.com/goxide-lang/goxide/releases/download/v0.0.1/goxide_v0.0.1_linux_amd64.tar.gz) |
| Linux | ARM64（arm64） | [下载 tar.gz](https://github.com/goxide-lang/goxide/releases/download/v0.0.1/goxide_v0.0.1_linux_arm64.tar.gz) |
| Windows | x64（amd64） | [下载 zip](https://github.com/goxide-lang/goxide/releases/download/v0.0.1/goxide_v0.0.1_windows_amd64.zip) |

[SHA-256 校验值](https://github.com/goxide-lang/goxide/releases/download/v0.0.1/checksums.txt) · [v0.0.1 版本说明](https://github.com/goxide-lang/goxide/releases/tag/v0.0.1)

下载后核对归档的 SHA-256。解压归档，将其中 `goxide`（Windows 为 `goxide.exe`）所在目录加入 `PATH`。

### 配套 Go 与安装检查

这五个平台的预编译 CLI 均使用 **Go 1.27.2** 构建，运行编译命令时必须使用 **Go 1.27.2，补丁号也须一致**。将 Go 加入 `PATH`；`--go` 可以指定其他位置的同版本 Go 可执行文件。CLI 不会自动下载或切换 Go 工具链。

```sh
go version
goxide --version
goxide env
```

`go version` 的版本应为 `go1.27.2`；发布二进制的版本输出应为：

```text
goxide v0.0.1 (go1.27.2)
```

`goxide test` 使用 Go race detector，还需要启用 cgo 并安装该平台的 C 编译器。只运行浏览器 Playground 时，无需安装本地 CLI 或 Go。

### 从标签源码安装

也可以使用 Go 1.27.2 从同一标签构建。下面的环境变量写法适用于 macOS / Linux 终端：

```sh
git clone --branch v0.0.1 --depth 1 https://github.com/goxide-lang/goxide.git
cd goxide
GOTOOLCHAIN=local GOWORK=off go install -ldflags="-X main.version=v0.0.1" ./cmd/goxide
```

将 Go 安装可执行文件的目录加入 `PATH`：设置了 `GOBIN` 时使用该目录，否则使用 `go env GOPATH` 对应目录下的 `bin`。未传版本注入参数的普通源码构建显示 `goxide dev`，上述 `-ldflags` 将版本设为 `v0.0.1`。

仓库位于 `goxide-lang/goxide`，Go module 路径仍为 `github.com/hxsf/goxide`。源码构建同样要求运行时所选 Go 与构建 CLI 的 Go 版本完全一致；更换 Go 版本后，须用新工具链重新构建 CLI。

## 第一个 Goxide 项目

新建 `hello` 目录，放入下面三个文件。项目根使用 `goxide.toml`，生成的 Go 模块放在 `target` 中。

### goxide.toml

```toml
[go_mod]
module = "example.org/hello"
go = "1.27"

[[bin]]
name = "hello"
path = "src"

[dependencies]
fmt = { go = "fmt" }
testing = { go = "testing" }
```

### src/main.gox

```gox
package main
use fmt;

enum Choice { Take(int), Skip }

func answer() int {
    return match Choice::Take(42) {
        Choice::Take(value) => value,
        Choice::Skip => 0,
    }
}

func main() { fmt::Println(answer()) }
```

### src/main_test.gox

```gox
package main
use testing;

func TestAnswer(t *testing::T) {
    if answer() != 42 { t.Fatal("unexpected answer") }
}
```

在 `hello` 目录运行：

```sh
goxide run                       # 输出 42
goxide build                     # 打印生成的可执行文件路径
goxide test                       # 运行项目测试，带 race 检测
goxide run --offline --locked     # 使用已有依赖与锁重跑
```

提交源码、`goxide.toml` 和生成的 `goxide.lock`。将 `target/` 加入项目的 `.gitignore`。项目根无需创建 `go.mod` 或运行 `go get`；Go 依赖通过清单声明。

项目可以包含多个包，用 `[namespaces]` 建立本项目的包名映射，再通过 `use` 引用。多个可执行目标分别写 `[[bin]]`，执行时用 `--bin NAME` 选择。继续阅读[清单字段](#清单字段)与[依赖和命名空间](#依赖与命名空间)，或打开[纯 Goxide 快速开始](https://goxide-lang.github.io/learn/?stage=gox)。

## 语言速览

Goxide 保留 Go 的函数、结构体、控制流和包模型，并增加下列语法。`.gox` 使用 `use` 建立包绑定，通过 `::` 引用包内名称；生成文件使用正常的 Go import。

| 能力 | 常见用途 | 进一步阅读 |
| --- | --- | --- |
| enum 与 match | 用带 payload 的变体表示不同情况，并进行穷尽分支处理 | [enum](rfcs/0008-enums.md)、[match](rfcs/0009-match.md) |
| Option、Result 与 `?` | 显式表示缺失或错误，提前返回错误结果 | [支持库与表示](design/enum-representation.md) |
| 声明宏 | 在表达式、语句、类型和声明位置复用语法 | [宏](rfcs/0011-macros.md) |
| 显式派生 | 为具体类型生成 Default、Clone、Debug、PartialEq、Eq、Error 或 serde 协议 | [派生](rfcs/0013-derive.md) |
| 引用与 payload 投影 | 通过 `&` / `&mut` 区分读写权限，在 `match &value` 中访问原槽位 | [引用](rfcs/0005-borrows.md) |
| trait、impl 与关联类型 | 为具体类型声明静态协议，适配现有 Go API | [trait](rfcs/0002-traits.md)、[关联类型](rfcs/0004-associated-types.md) |
| 条件编译 | 按 feature、目标或环境选择完整声明 | [条件编译](#条件编译) |

用户 enum 使用密封接口和每个变体对应的指针结构体。nil 不是合法 enum 值；对 nil 进行 match 会 panic，通配分支也不会接住它。match 按源码顺序尝试同一变体的 guard。

宏中的名字通常按定义点解析；需要调用点名称时显式使用 `$caller::`。`pub` 声明公开名称，Go 风格声明也保留首字母大写的公开约定。

这些链接包含完整语言设计；选择实际用法时结合[当前能力](#能力与设计)中的实现范围。可以在 [Playground](https://goxide-lang.github.io/playground.html) 中查看生成 Go，或从[混合快速开始](https://goxide-lang.github.io/learn/?stage=mixed)开始。

## 清单字段

`goxide.toml` 是单项目的作者清单。字段区分大小写，未知字段会报错。最小可运行项目见[上手示例](#第一个-goxide-项目)。

### 项目与目标

| 表 | 字段 | 含义 |
| --- | --- | --- |
| `[go_mod]` | `module`、`go` | 必填；用于生成 `go.mod`，分别指定 Go module 路径与 Go 版本要求 |
| `[go_mod]` | `toolchain` | 可选的 Go toolchain 声明；当前 CLI 使用本机匹配的 Go，不自动下载工具链 |
| `[namespaces]` | `名称 = "精确包路径"` | 将本项目的包绑定到 `use` 名称；支持带引号的 `"group::name"` 键 |
| `[[bin]]` | `name`、`path`、`required_features` | 可执行目标名、项目内目录或 `.gox` / `.go` 入口；最后一项为可选 feature 列表 |
| `[lib]` | `name`、`path`、`required_features` | 供其他项目使用的依赖库入口；最多一项 |
| `[features]` | `名称 = ["请求", ...]` | 模块的 feature 声明与展开关系；`default` 定义默认请求 |

`[go_mod]` 只保存生成 `go.mod` 所需的元数据。源码中的 `use` 名称来自依赖别名或顶层 `[namespaces]`，不由 `go_mod.module` 命名。

当前项目命令选择 `[[bin]]`；只有一个 bin 时可省略 `--bin`。`[lib]` 用于该项目作为依赖时的库入口，独立 `--lib`、example 与 workspace 目标选择尚未开放。`required_features` 检查已经启用的集合，不会自动开启 feature。

### 依赖

每项写成 `[dependencies.alias]` 或 `[dependencies]` 下的内联表。来源字段选择 `go`、`path`、`git` 中的一种。

| 字段 | 用法 |
| --- | --- |
| `go` | Go 标准库包路径，或带 `version` 的 Go module 路径 |
| `version` | 与 Go module 来源配套的精确版本或 pseudo-version；Go MVS 可以选中更高版本 |
| `path` | 本地项目或模块根，相对当前清单；从源码清单读取 module，可选 `module` 断言 |
| `git` | HTTPS Git 仓库地址；从所选源码根读取 module，可选 `module` 断言 |
| `module` | Git/path 来源的可选身份断言；Go 来源由 `go` 指定，不重复填写 |
| `rev`、`tag`、`branch` | Git 来源最多选择一项；不指定时选择默认分支并锁定 commit |
| `subdir` | Git 仓库内的项目/module 根目录；不用于选择普通子包 |
| `package` | alias 本身绑定的精确 Go package 路径；省略时 alias 只作 namespace 分组 |
| `namespaces` | 相对 alias 的名称到精确 Go package 路径的映射 |
| `optional` | 是否由 feature 激活，缺省为 false |
| `features` | 向带 Goxide 清单的源码依赖请求 feature 列表 |
| `default_features` | 是否请求依赖的默认集合，缺省为 true |

标准库来源例如 `{ go = "fmt" }`，无需 module 或 version，也不接受 feature 请求。registry 来源、workspace 继承、vendor 与 `[build]` 配置尚未开放。详细用法见[依赖与命名空间](#依赖与命名空间)。

### Go 模块政策

下面这些数组表用于声明生成 Go 模块需要的原生政策。普通 Goxide 依赖优先写 `[dependencies]`；`go.require` 本身不会创建 `use` 别名。

| 数组表 | 字段 | 含义 |
| --- | --- | --- |
| `[[go.require]]` | `module`、`version`、可选 `indirect` | 原生 Go require；也可声明生成代码使用的支持库 |
| `[[go.replace]]` | `module`、可选 `version`、`path` | 将指定 module 替换到本地路径；相对路径显式写 `./` 或 `../` |
| `[[go.replace]]` | `module`、可选 `version`、`with`、`with_version` | 替换到另一个 module 版本；与 `path` 二选一 |
| `[[go.exclude]]` | `module`、`version` | 排除某个 Go module 版本 |
| `[[go.godebug]]` | `key`、`value` | 生成 Go 模块的 godebug 设置 |

这些字段保留 Go 自身的版本选择含义。依赖内容写在 `goxide.lock` 中，作者项目根不维护另一份 `go.mod` / `go.sum`。完整字段设计见[清单规范](design/toml-manifest.md)。

## 依赖与命名空间

依赖的获取位置、module 身份与源码中的名字分别声明。下面是已有清单中的依赖示例：

```toml
[dependencies]
fmt = { go = "fmt" }
errors = { go = "github.com/pkg/errors", version = "v0.9.1", package = "github.com/pkg/errors" }
kit = { path = "../kit", package = "example.org/kit" }

[dependencies.formats]
go = "github.com/goxide-lang/serde"
version = "v0.0.0-20261009021520-f342a31e20d0"

[dependencies.formats.namespaces]
json = "github.com/goxide-lang/serde/json"
```

在 `.gox` 中，通过对应名字使用这些包：

```gox
use fmt;
use errors;
use kit;
use formats::json;
```

`formats` 没有 `package`，因此只是分组；`formats::json` 显式绑定到 JSON 包。编译器不会把 namespace 自动拼接成 Go import，也不会根据目录名猜包。手写 `.go` 仍使用普通 Go import。

### module 身份与库入口

来源身份、Go module 路径和库入口各有用途。依赖图按来源身份建立节点，同来源节点的 feature 请求取并集，不同来源保持独立。`go_mod.module` 是生成 Go 时的元数据，用于 Go `require` / `replace` 和包归属核验；生成前会检查不同来源是否占用相同 Go module 路径，冲突时给出诊断。path/Git 自动读取源码清单中的 module 路径，只有需要额外核对时才填写 `module`。Go module 来源的 `go` 已经提供该路径，标准库则不属于 module。

`use` 使用本项目声明的依赖别名，例如 `[dependencies.kit]` 对应 `use kit;`，不要求与依赖的 `go_mod.module` 同名。别名只在声明它的项目内生效；多个别名可以引用同一来源节点。

### 本项目的多个包

```toml
[namespaces]
service = "example.org/app/service"
"formats::binary" = "example.org/app/internal/binary"
```

源码使用 `use service;` 或 `use formats::binary;`。这些映射只指向本项目的包，首段名称不能与依赖 alias 重复。每个文件保留自己的 `use` 作用域；公开重导出使用 `pub use`，见[重导出规则](rfcs/0021-reexports.md)。

### Git 与仓库子目录

```toml
[dependencies.formatting]
git = "https://github.com/goxide-lang/std.git"
rev = "c02a09d8c417b35dc7e432fc484172fab6db6182"
package = "github.com/goxide-lang/std/formatting"
```

`rev`、`tag`、`branch` 最多指定一个。首次解析将完整 commit 写入锁，普通构建继续使用该 commit。`subdir = "packages/kit"` 用于仓库中的独立 module 或 Goxide 项目根；它不会搜索普通 package，也不能逃出仓库。path 来源直接指向项目根，不再使用 subdir。

使用当前编译器配套的 std/serde 版本。已有 Go 项目缺少支持库时，按 `gen` 诊断给出的固定版本命令补齐；TOML 项目将所需依赖写入清单。来源与绑定的完整规则见[依赖根](design/dependency-roots.md)。

## 锁文件与缓存

`goxide.lock` 记录解析后的依赖版本、Git commit 和校验和，随项目提交。`target/` 保存生成 Go、二进制和项目缓存，可加入 `.gitignore`。

```sh
goxide build
goxide build --locked
goxide run --offline --locked
```

| 选项 | 行为 |
| --- | --- |
| `--locked` | 要求现有锁保持不变；可以获取已经锁定但尚未缓存的依赖 |
| `--offline` | 使用本地来源和现有缓存，缺失内容时报错；它本身不禁止更新锁 |
| `--offline --locked` | 同时要求缓存可用与锁不变 |

第一次联网构建成功后，再使用 `--offline --locked` 检查重放。依赖或映射改变后若需要更新锁，先正常构建并审阅新的 `goxide.lock`，再恢复 locked 构建。

`--target-dir` 优先于 `GOXIDE_TARGET_DIR`，缺省目录为项目根的 `target`。相对路径按作者项目根解析。Git 缓存在受管 target 的 `cache/git` 下，Go 模块与构建缓存继续使用 `GOMODCACHE` / `GOCACHE`。

同样的源码和配置生成相同字节。只改变源码条件、且依赖计划不变时，锁文件保持不变；feature 激活新依赖时则可能需要更新锁。改变目标或环境后会重新选择生成结果。保留作者源码，修改 `.gox` 后重新生成，不手工维护 target 中的产物。

## Feature

feature 是名字集合，用于选择源码和请求可选依赖。两种工作方式使用不同的配置来源。

| 工作方式 | 如何声明与启用 |
| --- | --- |
| 原生 Go 的 `gen` | `--features=json,trace` 直接指定当前包的集合；缺省为空，未启用的合法名称查询为 false |
| TOML 项目 | `[features]` 声明名称与关系；根默认集合加 CLI 请求，依赖按自己的清单求解；未知名称报错 |

### 默认集合与显式请求

```toml
[features]
default = ["pretty"]
pretty = []
trace = []
```

```sh
goxide run
goxide run --no-default-features
goxide run --no-default-features --features=trace
goxide run --features=pretty,trace
```

`--no-default-features` 取消根的默认请求，显式请求仍生效。每个来源节点有自己的集合，根项目的同名 feature 不会自动传给依赖。指向同一来源节点的依赖请求取并集；一条依赖边关闭默认集合，不能撤销另一条边对默认集合的请求。

### 可选依赖与转发

下面假设本地源码库 `codec` 的清单声明了 `compact` 和 `json`：

```toml
[dependencies.codec]
path = "../codec"
package = "example.org/codec"
optional = true
default_features = false
features = ["compact"]

[features]
default = []
encoding = ["dep:codec"]
json = ["codec/json"]
```

- 本模块 feature 名展开本模块的另一个 feature。
- `dep:codec` 激活 optional 依赖。
- `codec/json` 激活该依赖，并向它请求 `json`。
- 依赖的 `features` 仅在该依赖被激活后加入请求。

optional 依赖不会自动创建同名 feature。未激活的 optional 来源不获取、不加载；若要避免某项下载，使用 optional，而不只是删掉源码中的 `use`。纯 Go 依赖和固定配置的生成 Go 依赖不接受 Goxide feature 重配请求。

### 在 Go 项目中切换

```go
//go:generate goxide gen --features=json,trace .
```

切换配置后重新生成整个包，再运行 `go build` / `go test`。Go build tags 与 feature 相互独立，`go generate -tags=...` 不会替你传入 `--features`。一个包的生成命令也不会重新配置其他包。

接着阅读[条件编译](#条件编译)，或打开[Feature 快速开始](https://goxide-lang.github.io/learn/?stage=features)。

## 条件编译

`#[cfg(...)]` 选择整个顶层声明，`#[cfg_attr(...)]` 按条件添加属性，`cfg!(...)` 返回布尔常量。

```gox
#[cfg(feature("json"))]
func Encoding() string { return "json" }

#[cfg(not(feature("json")))]
func Encoding() string { return "plain" }

#[cfg_attr(feature("trace"), derive(Debug))]
type Message struct { Text string }

const HasJSON = cfg!(feature("json"))
```

### 什么时候使用 cfg

条件关闭的顶层项不会参与后续名字、宏、协议或类型检查，也不装载该项的源码导入。当前可选择函数、类型、enum、trait/impl、`use`、宏定义与 item 宏调用等完整声明。被关闭的源码仍必须通过最初的语法解析。

同一项上的多个 cfg 条件取 AND。`cfg_attr` 为 false 时，只去掉这条属性，保留宿主声明；为 true 时应用括号内的属性。

### 为什么 cfg! 不能代替 cfg

```gox
func Mode() string {
    if cfg!(feature("json")) {
        return "json"
    }
    return "plain"
}
```

这里是普通的布尔条件，两条路径都必须通过名字和类型检查。若某函数已被 cfg 删除，不能靠 `if cfg!(...)` 保护对它的调用；应把调用者也放进相同条件的顶层声明。

### 可用谓词

| 写法 | 含义 |
| --- | --- |
| `feature("name")` | 本模块是否启用 feature |
| `all(A, B)`、`any(A, B)`、`not(A)` | 组合条件；`all()` 为 true，`any()` 为 false |
| `target_os("linux")`、`target_arch("arm64")` | 精确目标操作系统与架构 |
| `target_platform("unix")` | unix、windows、wasm、plan9 四类之一 |
| `target_endian("little")` | little 或 big |
| `target_pointer_width("64")` | 字符串 `"32"` 或 `"64"` |
| `env("APP_EDITION")`、`env("APP_EDITION", "pro")` | 原始进程环境中是否设置、是否精确等于指定值 |

参数使用字符串字面量，大小写敏感；未知谓词或非法值会报错。当前没有内建 `test`、`cgo` 或 `race` 谓词。宏模板中的条件按定义模块解释，完整捕获的条件构造保留调用点归属。

## 目标与环境

目标决定生成代码使用的操作系统和架构。先区分工作方式，再选择设置方法。

| 入口 | 默认目标 | 如何指定 |
| --- | --- | --- |
| 原生 Go 包或独立文件的 `gen` / `transpile` | 当前 Go 环境的 GOOS/GOARCH，包括 `go env -w` 保存的默认值 | 设置 Go 环境；此模式不接受 `--target` |
| TOML 项目的 `gen` / `transpile` | 运行 Goxide 的宿主 | `--target` |
| `expand` / `build` / `run` / `test`，含原生 Go 输入 | 运行 Goxide 的宿主 | `--target` |

```sh
GOOS=linux GOARCH=arm64 goxide gen .
goxide build --target arm64-unknown-linux .
goxide build --target linux/arm64 .
```

后两个目标拼写等价。工具链模式的默认宿主不受原始 GOOS/GOARCH 改写；`-o` 只改变输出位置。跨目标生成和 build 可用，非宿主 run/test 会提前报错。

### 查询当前配置

```sh
goxide env
goxide env --json
goxide env --list
goxide env --json --target linux/arm64
goxide env --json GOOS APP_EDITION
```

无名称参数时，分别显示宿主、原生生成继承的 Go 环境目标、工具链的有效目标，以及 Go 路径和版本。`--list` 列出 target 谓词值域和当前 Go 支持的组合。名称查询区分“未设置”和“设置为空串”，不与 `--target` 或 `--list` 混用。

### 环境条件

```gox
#[cfg(env("APP_EDITION", "pro"))]
func Edition() string { return "pro" }

#[cfg(not(env("APP_EDITION", "pro")))]
func Edition() string { return "community" }
```

`env` 读取启动时的原始环境，无需清单登记或额外 CLI 选项。空串也算已设置。原始 `GOOS=darwin` 配合工具链 `--target linux/amd64` 时，`env("GOOS", "darwin")` 与 `target_os("linux")` 可以同时为 true：一个查询原值，另一个查询编译目标。

原生生成把本次选择固定在 `_gox.go` 中。feature 或环境改变后，重新运行整包 `gen`；Go build 不会自动重新求值。使用目标谓词的包会生成精确的 GOOS/GOARCH build constraints，按目标分别生成即可。target 与 env 不需要新增 TOML 字段。

## CLI 参考

命令选项放在输入路径之前。`run` 的程序参数放在显式输入后的 `--` 之后，例如 `goxide run . -- --port 8080`。

| 命令 | 用途 |
| --- | --- |
| `goxide gen .` | 原位生成当前 Go 包；与 `transpile` 同义 |
| `goxide build` | 生成并构建当前 TOML 项目；也接受显式文件或目录 |
| `goxide run` | 生成、构建并运行当前项目 |
| `goxide test` | 对目录或项目运行带 race 检测的测试 |
| `goxide env` | 查询环境、工具链和目标，不加载源码 |

无 INPUT 的项目命令从当前目录向上寻找最近的 `goxide.toml`。原生 Go 包用 `goxide gen .` 明确指定当前目录；默认原位生成在最近的 `go.mod` 边界停止。`test` 接受目录或项目，`expand` 只接受单文件。

目前项目目标选择使用 `--bin`。帮助中的 `--workspace`、`--package`、`--lib`、`--example` 和 `--source-map` 尚未开放。`--expanded` 可为单文件显式导出展开结果，目录与原位生成不接受；直接查看展开结果可用 `goxide expand`。行级 `//line` 已用于原位和 stdout 生成。完整能力见[状态页](#能力与设计)。

以下帮助在网站构建时直接读取当前 `goxide` 可执行程序。也可以随时在本机运行 `goxide COMMAND --help`。

## 能力与设计

语言与工具链的实现状态来自[当前能力表](mvp-gap-audit.md)。表中“部分”表示该能力已有可用入口，具体边界以对应条目为准。

当前主线是已有 Go 项目原位生成与单项目多包构建。下一阶段按 workspace、import-go、vendor、构建脚本授权、LSP、safe std 的顺序推进。使用入口从[开始使用](#选择使用方式)选择。

深入语言规则与编译过程可阅读[设计文档](design/reading-guide.md)。设计参考：[已确认决策](design/decisions.md)、[语法与前端](design/frontend-ast.md)、[enum 表示](design/enum-representation.md)、[RFC 索引](rfcs/README.md)。RFC 描述设计范围；快速开始与能力表标明当前已实现的部分。

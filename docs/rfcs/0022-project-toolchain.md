# RFC 0022：可选 feature、依赖来源与命名构建目标

## 状态与范围

第一版已实现，采用可选 `hgo.json`，继续以 `go.mod` 的 module path 和 Go 目录包为身份。没有 manifest 的单文件、目录命令保持原行为。这不是完整 Cargo 兼容实现；不引入所有权、enum ABI、完整 mod 树或 workspace resolver。

实现集中于 `cmd/hgo/project.go`、`internal/project`。前端接入点为 `internal/frontend/module.go` 的按目标构建、配置标记和文件映射，以及 `cfg.go` 的前置配置选择。现有 use / pub use 的 canonical identity 与增强 metadata 校验不变。独立 `build.gox` 研究不属于本 RFC；没有构建脚本执行器或脚本配置字段。

## 源级规则

模块根目录显式放置 `hgo.json`，版本为 1，未知字段报错。依赖别名与 Go import 路径独立，import 仍写 module path。示例：

```json
{
  "version": 1,
  "features": {
    "default": ["pretty"],
    "pretty": [],
    "network": ["dep:client", "client/tls"]
  },
  "dependencies": {
    "client": {
      "module": "example.org/client",
      "path": "../client",
      "optional": true,
      "default_features": false
    }
  },
  "lib": {"name": "app", "path": "src/lib.hgo"},
  "bins": [
    {"name": "cli", "path": "src/main.hgo"},
    {"name": "web", "path": "src/web.hgo"}
  ],
  "examples": [
    {"name": "network", "path": "examples/network.hgo", "required_features": ["network"]}
  ]
}
```

`lib` 为零或一个对象；bins/examples 是显式列表，不自动扫描约定目录、父目录或相邻项目。每个类别内名称唯一；bin 和 example 可以同名，因为选择 flag 不同。目标 path 必须是模块内目录或 `.hgo` / `.go` 文件，不能越过嵌套 `go.mod`。未注册 helper 仍按 Go 目录包处理，不能隐式混合多个 main。

文件入口会显式映射：lib 文件到生成 module 根（包名由源文件决定）；bin/example 到 `hgo_targets/<类别>/<名称>/`。因此同一 src 目录中的多个 main 文件可以分别选择，并通过原 module path 导入 lib。目录入口保持相对目录包路径。文件入口只搬移该文件，不搬移同目录 helper 或相对 `go:embed` 资源；含这些内容的 CLI/web 应选择目录入口。所有目标入口从普通目录源中排除，只恢复选中入口和 lib；最终生成 Go 路径冲突拒绝，不覆盖文件。`hgo_targets` 与 `hgo_project_config.go` 为保留名称。

### Feature 图

feature 的数组元素支持：本模块 feature 名、`dep:alias`（只激活 optional 依赖）、`alias/feature`（激活依赖并转发 feature）。依赖还可声明 `features: ["x"]` 和 `default_features: false`。

- 默认启用已声明的 `default`；`--no-default-features` 仅关闭根模块的默认请求。
- 所有到同一 module/source 的请求取并集；任一边要求默认 feature，就启用其默认 feature。不能以一条边的 false 撤销另一条边的 true。
- 未知本地 feature、未知依赖、feature 循环、模块依赖循环诊断；所有本地 feature 边即使未启用也检查。转发到依赖的 feature 名在依赖被激活读取后检查，未激活 optional 来源不下载。
- 不隐式生成与 optional 依赖同名的 feature。原生 Go version 来源不接受 hgo feature 转发；需要增强源码配置时用 path/git。
- 原始 go.mod 的 require 不因 hgo optional 关闭而被删除；显式 Go 依赖仍由 Go 解析。

### 配置选择顺序

```hgo
//hgo:cfg all(feature("pretty"), not(feature("network")))
package app

func Pretty() bool { return cfg!(feature("pretty")) }
```

`//hgo:cfg predicate` 必须位于 package 之前，多个指令取 AND。谓词为 `feature("name")`、`all(...)`、`any(...)`、`not(...)`；任何未知 feature 都报错，包括短路另一侧。先应用 Go 文件名/build tags，再对选中目标的包依赖闭包进行 cfg。未引用包、未选目标不做宏/名称/类型检查。

文件条件为 false 时，在 import 依赖发现、宏收集/展开、use 绑定和类型检查之前移除，合法 enum、match、派生、宏均可位于关闭文件中。检查整个文件的词法合法性与括号 token tree 配对，并复用 hgo import-prefix parser 检查 package/import 前缀语法，但不装载 import、不解析名称/类型，也不检查关闭宏体内的 cfg! 或保留绑定。**关闭文件不执行完整声明/表达式语法检查**：例如配对但不完整的 `func F(){x:=}` 会在启用后报错，关闭时不报错；非法字符、未闭合字符串/注释、错配括号和错误 package/import 前缀始终拒绝。这是明确的词法/基础语法边界，不宣称已有完整 hgo 纯语法 parser。

`cfg!(...)` 是单行布尔常量查询，保留源字节位置，不能通过它让普通 if 的另一分支免于类型检查；要移除代码请使用文件条件。多行 cfg!、声明级 `#[cfg]` 暂不支持。project 中宏名/直接 use 绑定 `cfg` 保留，不允许同名声明或 alias 被预处理静默劫持。查询在用户宏展开前处理，不能用宏生成新的 cfg 指令或 cfg!。

## 转译示例

配置选中上面的文件后，`cfg!(feature("pretty"))` 替换为布尔常量，普通函数继续使用现有降低流程。核心生成 Go 如下（配置指纹还会单独写入包内私有常量文件）：

```go
package app

func Pretty() bool { return true }
```

文件条件不满足时，没有该文件的 Go 声明或 import；单纯的 `if cfg!(...)` 则仍是普通 Go 常量条件，不跳过分支的类型检查。

### 依赖、锁定与生成 Go

每个依赖必填 `module`，并在以下来源中恰选一个：

```json
{"module":"golang.org/x/text", "version":"v0.28.0"}
```

```json
{"module":"example.org/lib", "git":"https://example.org/lib.git", "rev":"release-1", "subdir":"library"}
```

```json
{"module":"example.org/lib", "path":"../lib", "features":["fast"]}
```

上面仅展示格式，不是测试访问的服务。版本解析、MVS、module 缓存、校验和全部调用真实 Go 工具。原生 go.mod 与本地 replace 可继续使用。manifest 与原 go.mod 的直接版本/来源冲突拒绝；传递本地来源也不能覆盖根已有的不一致 replace。源 go.mod/go.sum 不改写，修改限于生成副本。

每个 path/git 依赖入口（Git 含 subdir 后）必须自带普通文件 go.mod，module 字段必须等于其实际 module path；不会自动包装纯 Go 目录或合成 go.mod，也不采用父模块的 go.mod。缺少 hgo.json 的原生 Go 模块可以使用。

本地 path 相对声明它的 manifest，而非 cwd，可指向用户明确选定的模块；不递归发现相邻模块。Git 必填显式 rev，可为 commit/tag/branch；首次解析后在 `hgo.lock` 锁定精确 commit，之后移动 tag/branch 不改变锁。Git subdir 必须在 checkout 内，实际 go.mod module path 必须一致。同一 module path 不能由不同来源占用。

Git 接受无凭据的 HTTPS URL，另支持绝对本地路径/file URL 用于本地仓库；禁止 URL userinfo/query、SSH 和 ext。命令通过独立 argv 调用，不经 shell，不运行安装脚本/子模块/生成脚本，禁用 hooks、credential helper、askpass、fsmonitor 与外部协议。Git 来源的 path 后代和本地 go.mod replace 不得逃出 checkout。缓存 checkout 必须与锁定 commit 一致且无修改、未跟踪或 ignored 文件；拒绝缓存目录 symlink。

`hgo.lock` 保存 source-key→commit 和精确模块配置→Go sums；应提交。`--locked` 拒绝缺锁、缺配置、校验和或 commit 变化。`--offline` 禁止 Git fetch，Go 强制 `GOPROXY=off GOSUMDB=off GONOPROXY=none GOVCS=*:off`，并固定 `GOTOOLCHAIN=local GOWORK=off`；冷缓存明确失败。离线需预热 Git `.hgo/git` 和 Go module cache，锁文件不代替依赖字节。无自动 update 命令；更新 Git 时显式更换 rev，旧配置锁项保留。锁不冻结用户本地 path 文件内容。

Go 对各生成 main module 计算 build list。第一版要求各 hgo 节点中出现的同一原生依赖具有相同实际选中版本/replace；检测到不同则提示统一版本，**不声称已实现全图统一 MVS 规划**。这样避免以依赖 v1 检查过的增强 metadata 被根 v2 悄然复用。

生成的 bundle 包括 module 和所需 deps 副本，go.mod replace 指向本次生成目录。可在该位置用 Go 构建；移动 bundle 后绝对 replace 需要重新生成。每次使用新 bundle，没有持久转译缓存。manifest、启用 feature 集、目标种类/入口和编译器内容摘要形成每个项目模块的配置标记，进入生成源码与 metadata/Go cache 指纹。Go tags、平台及 race 仍使用已有 fingerprint；普通/测试 metadata 继续隔离。无 manifest 的已有外部产物不强加配置标记，保留其 metadata 验证环境。

## 双向互操作

### 命令与兼容性

所有 flag 在输入目录之前，真实 Go 需 1.27+：

```sh
hgo run --go /path/to/go --bin cli --features pretty ./app
hgo run --bin cli --no-default-features --offline --locked ./app
hgo build --bin web -o /tmp/app-web ./app
hgo run --example network --features network ./app
hgo build --lib ./app
hgo test --lib --offline ./app
hgo transpile --bin cli -o /tmp/app-generated ./app
```

单个 bin 可省 --bin；多个 bin 必须显式选择。没有 bin 但有 lib 时默认 lib；`run --lib` 拒绝。`--lib`、`--bin`、`--example` 互斥。`required_features` 是检查条件，不自动启用 feature。每次只构建一个目标及依赖闭包；所有 active manifest 依赖会准备其 lib。项目依赖必须声明 lib；无 manifest 的 Go/旧 hgo 模块可作传统根目录库使用。

build 二进制要求显式 -o，输出不得位于任一输入源码树内；transpile 要求全新输出目录并自动输出逐文件 source map，诊断保持原入口文件位置。run 不支持额外程序参数。test 自动 -race；原生 `_test.go` 的本模块 imports 纳入测试构建闭包，但不混入普通包类型检查；普通构建/transpile 不包含测试文件。`.hgo` 测试文件暂不支持。依赖 lib 只生成其选中闭包，跨模块访问未进入 lib 闭包的 hgo 子包暂不保证，宜由 lib API 暴露。

examples 仅显式选择构建，不成为默认 bin/lib 产物；本片没有 publish/package 命令，不承诺自动裁剪发布档案，生成副本中的普通非源码资源可能仍保留。Go 消费者继续消费标准 Go 与显式依赖关系，不需要采用 hgo feature 解析器。

## 拒绝与边界

除上述限制外，未实现互斥 feature、target-specific feature、workspace resolver 版本、weak dependency feature、全目标构建、build.gox、开发依赖分组或远端私有凭据管理。增量采用从 Go 模块开始，仅添加需要的 hgo.json，不导入 Cargo 全套语义。

同一项目写入用 `.hgo/build.lock` 目录互斥，冲突立即报错；进程崩溃后需确认没有构建，再手动移除残留目录。生成成功后写 lock，最终用户程序运行失败不回滚已解析依赖的锁。构建和运行不是沙箱；这里禁止的是依赖下载附带隐式安装脚本，并非声称隔离用户选择运行的程序。

## 验收与未定项

`cmd/hgo/project_test.go` 使用真实 Go、临时多模块、file proxy 和本地临时 Git，不访问第三方服务。覆盖默认/关闭/显式 feature、菱形依赖并集、optional 来源延迟、多个同目录 bin/example、无 lib、原路径诊断、生成 Go race/vet、A/B/A 配置切换、branch 移动与精确 rev、冷/暖离线、锁定重复构建、原 go.mod replace、传递来源冲突、输出重名、测试 helper 闭包、Git 路径边界、原生 build list 一致性。`internal/project` 覆盖未知/循环 feature、目标重复、source 互斥和路径诊断；前端 cfg 测试覆盖词法/token-tree 与 package/import 前缀、注释/字符串假指令和预处理时机。额外真实项目在同一文件组合 enum、match、derive、macro，覆盖两个 bin、example 和默认/关闭/显式开启的 A/B/A 切换；关闭文件含不存在的 import 与未解析宏也不触发加载。

可运行示例见 [examples/project](../../examples/project/README.md)。最终验证记录以提交说明与工作结果为准，不将 RFC 的验收目标冒称测试已通过。


### 已复现的修复边界

早期实现用 Go 完整 parser 验证关闭文件，误拒合法 hgo DSL；现改为上述基础检查并在任何宏/use/cfg! 查询处理前退出。配置标记的包名前缀读取同样复用 hgo parser。启用文件若 package 前有 cfg/build 注释，派生 import 插入点由格式化 Go AST 的 package 名结束位置确定，不再假设文件第一行是 package。

多行 cfg! 仍明确拒绝，不能用保留 LF 的简单布尔填充规避：Go 自动分号会使合法表达式变成语法错误。后续若支持，须接入明确 source-map edits，覆盖零缩进闭括号、CRLF、前置否定、尾随运算及后续诊断位置；本次未加入新的谓词或表达式解释器。

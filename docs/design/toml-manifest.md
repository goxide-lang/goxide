# goxide.toml schema

当前作者清单与可提交的 `goxide.lock` 位于项目根；Go 元数据和生成源码只写入受管 target。依赖字段参考 [Cargo](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html)，Go module 选版仍用 [Go MVS](https://go.dev/ref/mod#minimal-version-selection)。不读取旧 source 标签或嵌套来源表，不实现 registry。

[RFC 0028](../rfcs/0028-crate-roots.md) 的根解析已接通，全部 `namespaces` 字段已删除；当前支持范围及对应测试见[状态表](../mvp-gap-audit.md)。

## P5 新管线入口

状态：已完成。`internal/compile.PrepareProject` 接受单项目目录，使用既有清单、Go 依赖图和锁，选择 bin 后把所需模块交给 `parser → expand → resolve → check → lower`。返回稳定生成模块目录与 Go 入口包；调用者在该目录执行 Go build、run 或 test。CLI 三种输入模式在 P6 统一切换。

当前使用项目 `[go_mod]`、Go/path/Git 依赖及 lib/bin/example；Goxide 依赖默认从库自己的 `[lib]` 入口访问。多包源和所选 Goxide 依赖联合检查，保留 enum、trait、宏和引用权限，不使用旧前端导出 metadata。原生 Go 源码、测试与资源参与生成模块。crate、自动库根、同包混编与纯 Go 子包解析按 RFC 0028 接通；清单作者根不生成 go.mod/go.sum。

依赖版本和校验和由既有 Go planner 锁定，生成模块间使用 bundle 内的相对 replace；临时生成目录不进入锁或最终 Go 元数据。相同输入保持生成字节、产物路径和锁稳定，offline/locked 使用已有缓存与锁重放，清单依赖声明改变时 locked 拒绝更新。失败的源检查不发布新锁或覆盖已有 bundle。

feature 条件编译已接入：支持 cfg、cfg_attr 顶层条件属性及 cfg! 布尔查询；cfg! 不跳过 if 两支的检查。合同见 [RFC 0022](../rfcs/0022-project-toolchain.md)。workspace、vendor 迁入、import-go、构建脚本仍按后续 MVP 阶段接入；下文保留这些工具链实现的既有字段合同。

## 最小项目与真实依赖

新目录放两个文件即可运行；需 Go 1.27+，首次允许联网：

```toml
# goxide.toml
[go_mod]
module = "example.org/hello"
go = "1.27"

[[bin]]
name = "hello"
path = "src"

[dependencies]
fmt = { go = "fmt" }
errors = { go = "github.com/pkg/errors", version = "v0.9.1" }
```

```goxide
// src/main.gox
package main
use fmt;
use errors;
func main() { fmt::Println(errors::New("clean dependency download")) }
```

```sh
goxide build                    # 自动下载声明的依赖；打印 target 中的可执行文件路径
goxide build --offline --locked # 同一输出路径；不改写相同生成文件或锁
goxide run --offline --locked   # clean dependency download
```

也可在项目子目录执行。`--go /absolute/path/to/go` 选择 Go 工具，须与构建 CLI 的 importer 版本一致。作者根不补 go.mod、go.sum，不手工 go get；依赖写入 TOML。

## 字段

| 表 | 字段与含义 |
| --- | --- |
| go_mod | 生成 go.mod 的元数据：必填 module、go；可选 toolchain（建议版本，不自动下载） |
| dependencies.alias | 按下述四类来源填写；alias 选择来源根，纯 Go 子包通过真实子路径解析；Git/path 的 module 可选，仅作身份断言；不接受 package 或 namespaces |
| features | 名称到字符串数组；default 为默认集合，dep:alias 激活 optional，alias/feature 转发 feature |
| lib | 至多一项 name、path，可加 required_features；name 是自动库根，必须是合法源标识符，不能为 crate 或与依赖 alias 冲突 |
| bin / example | 数组表，各项 name、path、可选 required_features；path 为项目内目录或单个 .gox/.go 入口 |
| go.require | 数组表 module、version、可选 indirect；生成原生 Go require，供已有 Go 政策和编译器支持库依赖使用 |
| go.replace | 数组表 module、可选 version，以及 path 或 with+with_version；保留 Go replacement 语义 |
| go.exclude / go.godebug | 数组表 module+version，或 key+value；沿用当前受支持的 Go 政策 |
| build | entry、inputs、outputs、env、timeout_seconds、dependencies、tools，见 [脚本合同](../rfcs/0023-build-scripts.md)；下载不授权执行脚本 |
| workspace | members、default-members、exclude、go_mod、dependencies，以及已有原生迁入 go 政策，见 [workspace 合同](../rfcs/0026-workspaces.md) |

作者清单不声明格式版本，直接按当前字段校验。`[go_mod]` 仅描述生成 Go module 的元数据；语言中的 `use` 绑定来自当前 crate、自动库根、依赖 alias 或未占名的标准库根，不以 `go_mod.module` 作为源码导入名。顶层、依赖和 workspace 模板均不接受 `namespaces`。未知字段、错误类型、非法 TOML 和大小写错误有文件/行/列诊断。来源语义错误指出依赖名。

## Go 成员 sugar 配置

状态：**设计已确认，待实现**。`codegen.go_sugar` 控制 [trait 实例成员的 Go sugar](traits.md#go-成员-sugar)，类型为布尔值，省略时为 `true`：

```toml
[codegen]
go_sugar = true
```

CLI 使用 `--go-sugar` / `--no-go-sugar`；同次请求同时给出两者时报错。有效值按 **显式 CLI > 本模块 TOML > true** 取值，`gen/transpile/build/run/test` 一致。它是生成 Go API 的策略，不作为 feature 或宏查询条件，不改变宏输入和展开产物。

配置单位是独立分发模块，模块内所有包及 lib/bin/example 共用。CLI 只覆盖本次选为入口的模块；workspace 批量命令只覆盖选中成员，不传播到仅作为依赖加载的模块。各依赖从自己的清单读取，未配置则开启；同一来源节点只采用一个有效值，不按依赖边取并集。已发布的纯 Go/dist 直接使用其现有方法集合。若某模块同时是选中成员和另一成员的依赖，本次共用该选中模块的生成结果，不再构造第二份方法集合。

原生 Go 混写项目可在 `go.mod` 所在目录放置仅含 `[codegen]` 的 `goxide.toml`，读取该模块的生成策略，继续原位生成 `_gox.go` 并由 Go 管理 `go.mod/go.sum`。这类配置不成为 Goxide 项目或 workspace 清单；完整作者清单仍遵循 `[go_mod]` / `[workspace]` 的入口规则。配置查找止于所属模块边界，不使用更外层模块的 sugar 值。无模块的单文件使用 CLI 或默认值。

每个模块的最终有效值纳入既有生成配置摘要，以区分不同 Go 方法集合；它不改变依赖来源、选版或锁文件身份。切换开关按原有生成文件所有权规则更新产物。验收覆盖默认开启、TOML 关闭、CLI 双向覆盖、根模块开关不影响依赖、混写原位生成、命令间一致性和缓存切换。

## Feature 配置

`[features]` 同时提供模块内查询的声明目录与展开图。根模块默认启用已声明的 default，再加入 CLI 的 `--features`；`--no-default-features` 只取消根默认请求。依赖通过 `features`、`default_features` 和 feature 边接收请求，同一来源节点的请求取并集，任何一条边要求默认集合即保留。根 feature 不按同名隐式传播到依赖，各来源节点内的包共享本节点最终集合。

`dep:alias` 激活 optional 依赖，`alias/feature` 激活并转发，本地名称展开本模块 feature；不隐式创建 optional 同名 feature。请求及实际处理到的完整 cfg/cfg_attr/cfg! 谓词检查本模块声明，未知 feature 报错；外层 cfg 删除的子树、未选中的 cfg_attr 载荷不继续检查，普通 if 两支仍检查。`required_features` 在图求解后检查，不自动启用 feature。纯 Go 或固定生成 Go 依赖不能被请求重配 Goxide feature。

源码条件选择不反向修改清单图；非 optional 依赖和 go.require 仍按声明处理。产物配置摘要纳入每个 module 排序后的实际 enabled 集，不能只用依赖计划身份区分生成物；同配置请求乱序或重复不影响输出。纯 cfg 变化且依赖计划不变时保持锁文件字节稳定，可 --locked 重放。目标/测试谓词使用未来编译上下文，不添加到 `[features]`；当前实现和验收进度仍见[状态表](../mvp-gap-audit.md)。

## 编译环境与目标

[RFC 0022 的统一条件配置](../rfcs/0022-project-toolchain.md#统一条件配置)仅让 feature 读取清单配置。target 由工作流与 --target 决定，env 直接查询一次捕获的原始进程环境；不增加 `[cfg]`、env 登记或 target 清单字段，也不提供 --cfg-env。实际 env 读取记录在编译结束后进入产物摘要，不进入依赖锁身份。

## 来源与包身份

以下依赖配置可加入已有 `goxide.toml`。来源按获取方式分为 Go 标准库、Go module、本地 path、Git；`go`、`path`、`git` 互斥，`version` 当前只配合 Go module 来源。path 和 Git 都可以取得 Goxide 或纯 Go 源码。

### Go 标准库

```toml
[dependencies.fmt]
go = "fmt"
```

```goxide
use fmt;
```

显式标准库根必需 `go`，值为标准库的精确包路径；没有 `version`。alias 绑定该包，不写 `module`，不下载，不进入 module 选版图。可选 `optional`；禁止 `features`、`default_features` 和 feature 转发；真实加载时核验 Go 的 `Standard` 标志。没有同名显式根时，`use fmt;`、`use encoding::json;` 可由标准库 fallback 提供，因而无需同名依赖声明；显式改名、覆盖和 optional 继续占名，未激活时不 fallback。

### Go module

```toml
[dependencies.errors]
go = "github.com/pkg/errors"
version = "v0.9.1"
```

```goxide
use errors;
```

必需 `go`、`version`：`go` 是不带协议的 module path，不能用同一 module 的普通 package path 代替；`version` 是 Go 规范版本或 pseudo-version。MVS 可以选中更高版本，lock 记录实际结果。`go` 已提供 module 身份，禁止重复填写 `module`。

可选 `optional`、`features`、`default_features`。纯 Go module 的 alias 选择模块根路径，子包通过 `use alias::真实子路径` 解析；根目录没有 package 时仍可使用真实子包。Goxide 源码依赖必须声明自身 `[lib]` 入口。feature 请求规则见[下文](#可选依赖与-feature-请求)。

### 本地 path

```toml
[dependencies.kit]
path = "../kit"
```

```goxide
use kit;
```

必需 `path`，直接选择项目/module 根，相对声明所在清单解析。模块身份从该根的 `goxide.toml` 中 `go_mod.module` 或 `go.mod` 中 `module` 自动读取，不根据目录名或 alias 推断。

可选 `module`、`optional`、`features`、`default_features`。显式 `module = "example.org/kit"` 仅断言所读取的身份必须相等，不为依赖改名；不一致时报 module path mismatch。不接受 `version`、`subdir` 或 Git 选择器。本地字节不冒充不可变远端 checksum。

### Git

```toml
[dependencies.standard]
git = "https://github.com/goxide-lang/std.git"
rev = "c02a09d8c417b35dc7e432fc484172fab6db6182"

```

```goxide
use standard::formatting;
```

必需 `git`，远端来源使用 HTTPS 仓库 URL。module 身份从所选项目根的 `goxide.toml` 或 `go.mod` 自动读取。可选 `module` 一致性断言、`rev`/`tag`/`branch` 中的一项，以及 `subdir`、`optional`、`features`、`default_features`。

多个 Git 选择器报冲突，没有优先级。`rev` 支持 commit hash 或远端公开 ref，`tag` 与 `branch` 分别解析自己的 ref。省略选择器时解析默认分支，完整 commit 写入 lock；普通构建沿用锁定 commit，不随移动 ref 更新。Git 不接受 `version`，没有 Cargo 风格的发布版本回退。

`subdir` 只选择仓库内独立的项目/module 根，规则和完整 monorepo 示例见[下节](#monorepo-与发现)。上例的 `use standard::formatting;` 直接选择 std module 内的真实 Go 子包；`subdir` 不能用来指定该子包。

### module 身份与默认入口

path 和 Git 的来源身份与生成 Go 所需的 module 路径分开。取得源码后读取所选根的 `goxide.toml` 中 `go_mod.module` 或 `go.mod` 中 `module`；可选的显式 `module` 只作一致性断言。没有有效清单或断言不一致时报错，不从仓库 URL、目录名或 alias 猜测 Go module 路径。

依赖图按来源身份建立节点，同来源节点接收的活动 feature 请求取并集，不同来源保持独立。`go_mod.module` 只用于生成 Go module，不作为 Goxide 依赖图的节点身份；生成 Go 前检查不同来源是否占用相同 module 路径，冲突时拒绝生成。Go `require` / `replace` 和包归属核验仍使用 Go module 路径。Go module 来源的 `go` 已经提供该路径，所以不重复填写 `module`；标准库属于工具链，不使用 module 路径。作者项目本身的 `[go_mod].module` 仍是必填字段。

依赖 alias 是声明它的项目内的引用名，决定该项目源码中的 `use` 名称，不要求与 `go_mod.module` 同名。不同 alias 指向同一来源节点时共享该节点，不会复制类型身份。

path 按规范目录归一，Git 按仓库地址、锁定 commit 和 subdir 归一，Go 版本来源按逻辑 module path 接收 MVS 请求；选中版本与校验和另属内容身份。具体的来源键、有效本地 replace 和锁中路径表示见[依赖根合同](dependency-roots.md)。

依赖项不接受 `package` 或 `namespaces`。标准库 alias 的入口是 `go` 指定的包；纯 Go module 使用模块根路径并按需查询真实子包与成员；Goxide 源码依赖必须声明 `[lib]`，缺少时拒绝。`[lib]` 是库作者定义的入口，不由消费者指定另一个包代替。Goxide 的公开子路径来自 `pub mod` / `pub use`，不能通过物理 Go 目录绕开。

### Go 子包与公开路径

清单声明 module 来源与 alias，源码选择其中的真实 Go 子包，例如：

```toml
[dependencies.formats]
go = "github.com/goxide-lang/serde"
version = "v0.0.0-20261009021520-f342a31e20d0"

```

```goxide
use formats::json;
```

`formats` 选择 serde module，`formats::json` 查询真实的 `github.com/goxide-lang/serde/json` 包，不需要登记映射。每段使用合法源标识符；子包与导出成员同名时报歧义。module 根或中间目录没有包时可以继续寻址，最终目标必须是可导入的真实包；未使用的子包不被递归导入。

实际包必须属于选定的依赖 module，不能只凭路径前缀证明归属或跨入嵌套 module；Go 的 `internal` 边界保持有效。Goxide 来源始终沿库的公开模块树解析，不因底层 Go 目录存在而放行私有包。原生 `.go` 仍使用 Go import；详细绑定规则见[依赖根](dependency-roots.md)。

### 可选依赖与 feature 请求

四类来源均可设置 `optional = true`，缺省为 false；由 feature 请求激活，如 `dep:alias`，不会由 `use` 隐式开启。三类 module 来源另可设置 `features = ["名称"]` 和 `default_features`：前者向带 Goxide 源码清单的依赖请求 feature，后者缺省为 true，false 取消该依赖边的默认请求。纯 Go 或固定生成 Go 依赖没有可重配的 Goxide feature，不能向它转发 feature。完整展开规则见 [Feature 配置](#feature-配置)。

registry 来源尚未实现。只写 `version`，或 `version` 加 `registry`，均明确报错；不会据此猜测 registry 或 Git URL。

## monorepo 与发现

Git 的 subdir 只选仓库内有 goxide.toml 或 go.mod 的项目/module 根；省略选仓库根。它不选择同一 module 下的普通 package，不递归搜索或按 alias 猜目录，也不得逃出 checkout。path 已直接选根，不配 subdir。

可重放的 monorepo 示例使用本地 Git 夹具；其独立 Go module 位于 packages/lib，依赖 alias 默认绑定该 module 的根包。Git 的 subdir 选择模块根，module 身份从其中的 go.mod 读取；默认分支解析后锁定 commit：

```sh
work="$(mktemp -d)"
mkdir -p "$work/repo/packages/lib" "$work/app"
cat > "$work/repo/packages/lib/go.mod" <<'MOD'
module example.org/mono/lib
go 1.27
MOD
cat > "$work/repo/packages/lib/lib.go" <<'GO'
package lib
func Value() string { return "monorepo" }
GO
git -C "$work/repo" init -q -b main
git -C "$work/repo" add .
git -C "$work/repo" -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm fixture
cat > "$work/app/goxide.toml" <<TOML
[go_mod]
module = "example.org/mono/app"
go = "1.27"
[[bin]]
name = "app"
path = "main.gox"
[dependencies]
fmt = { go = "fmt" }
lib = { git = "$work/repo", subdir = "packages/lib" }
TOML
cat > "$work/app/main.gox" <<'GOX'
package main
use fmt;
use lib;
func main() { fmt::Println(lib::Value()) }
GOX
(cd "$work/app" && goxide run && goxide run --offline --locked)
```

真实远端依赖使用 HTTPS；绝对本地 Git 路径用于上述夹具与定向验证。没有对旧 monorepo candidate 的历史 ABI 提供兼容。

build/run/test 及指定输出目录的 transpile 无 INPUT 时，从 cwd 向上发现最近 goxide.toml；go.mod 不阻断外层项目/workspace。显式目录采用同一规则；显式文件保留单文件语义。没有清单时需显式指定 Go 目录或源码，不扫描目录猜 main。

默认原位 `gen` / `transpile` 按最近模块边界选择入口：遇到当前或上层 goxide.toml 时进入项目模式，先遇到 go.mod 时只生成当前 Go 包；嵌套原生模块不会被更外层 TOML 接管。

上述为当前入口行为；已确认的 [sugar 配置](#go-成员-sugar-配置)落地时，仅含 `[codegen]` 的原生 Go 模块配置须保持原位模式。

成员子目录选择最近成员，workspace 根用 default-members，`-p MODULE` 与 `--workspace` 显式改变选择。未列出的项目报非成员，不因目录位置补依赖。成员用 `workspace = true` 继承模板，只可追加 features 或设置 optional/default_features；模板 path 相对 workspace 根。版本与锁全 workspace 统一，未选成员 features 不自动加入本次编译。

## 锁、缓存与输出

goxide.lock 在项目/workspace 根，应提交。Git 缓存位于受管 target/cache/git；Go 缓存继续用 GOMODCACHE/GOCACHE。项目根不生成 `.goxide`；缓存位置不写成可提交的 Git 来源身份。删除缓存后可联网重取已锁 commit；offline 缺缓存明确失败。

`--target-dir` > GOXIDE_TARGET_DIR > target；相对路径以作者根为准。同一配置复用稳定 bundle 与二进制路径，更新变化字节、移除失效文件，保留相同文件的 mtime。临时生成视图结束后删除，不永久新增 run 目录。用户修改或未知文件阻止覆盖/清理，构建失败不发布新二进制。见 [target 布局](target-layout.md)。

# 依赖根、crate 边界与 Go 子包解析

## crate 与目标边界方案

源级语义、Go 对照、拒绝条件和配置去留集中维护于 [RFC 0028](../rfcs/0028-crate-roots.md)。当前实现采用该合同。来源、根声明与锁合同见[后半部](#来源身份与根声明)，清单见 [TOML schema](toml-manifest.md)，实际完成范围见[能力状态表](../mvp-gap-audit.md)。

### 修改所在的流程位置

整体顺序保持：清单与 feature 求解 → 选择 target、暂存源码并准备 Go module/importer 上下文 → 加载并确定包归属，执行 `parser.ParseFile` / 条件选择 / `expand.Expand` → `resolve.ResolveExpanded` → `check.Check` → `lower.Lower` → 写入并发布生成产物 → Go build/run/test。包加载随有效 `mod` 和 import 发现递进，宏输出也参与；条件选择始终先于名字、宏和 import 收集。

当前 `lower.CompileModules` 同时承担多包加载和前端编排。表中的“加载”“解析”“降低”是流程职责，不要求为此重排目录或新建抽象层。

| 流程位置 | 当前代码入口 | 本方案的修改职责 |
| --- | --- | --- |
| 清单读取、feature 求解与 target 选择 | [manifest.go](../../internal/project/manifest.go) 的 `Parse/validate/Resolve/Select`；[compile/project.go](../../internal/compile/project.go) 的 `PrepareProject` | 校验库名与全量依赖占名；分离已声明根和活动根；把 lib/bin/example 的选中入口及可用 lib 交给包加载器 |
| 来源图与 Go 选版 | [graph.go](../../internal/project/graph.go)、[planner.go](../../internal/project/planner.go) | 保留来源身份、feature 合并与 Go module 选版职责；自动库入口不创建 self dependency，crate 也不成为新来源节点 |
| 编译前的 Go module 上下文 | [compile/modules.go](../../internal/compile/modules.go) 的 `prepareModules` | 为 importer 准备受管 go.mod、已选版本和来源映射；此步骤在 `CompileModules` 之前 |
| 源文件选择、包图和 target 归属 | [sources.go](../../internal/compile/sources.go) 的 `stageSources/moduleRoots/libraryPackage`；[lower/module.go](../../internal/lower/module.go) 的 `CompileModules/moduleSources`；[module_crates.go](../../internal/lower/module_crates.go) | 确定 target 根、有效 `mod` 图及同 target 原生 Go import 闭包；检查嵌套 target、共享 lib、生成包和文件型 target 的归属及实际包路径 |
| Go 混编与纯 Go 包加载 | [module_native.go](../../internal/lower/module_native.go)、[module_lookup.go](../../internal/lower/module_lookup.go)、[go_namespaces.go](../../internal/paths/go_namespaces.go) | 共享同包声明、隔离文件 import；让纯 Go 子包参加 `mod` 图，按真实 Go 包树和来源身份解析外部子路径；检查手写 `.go` import 的跨 crate 边界 |
| 语法、宏上下文与静态名字绑定 | [parser](../../internal/syntax/parser/)、[expand.go](../../internal/expand/expand.go)、[static_bindings.go](../../internal/expand/static_bindings.go)、[macro_roots.go](../../internal/lower/macro_roots.go)、[resolve](../../internal/resolve/) | 识别固定 `crate::`、自动库根和定义点归属；lower/typecheck 共用展开后的卫生静态绑定，统一根优先级及标准库 fallback；纯 Go 来源按真实子路径和导出成员解析并检查歧义，Goxide 来源保持公开模块树 |
| 包可达性与成员/类型检查 | [module_visibility.go](../../internal/lower/module_visibility.go) 的 `checkSourceRoots`；[check/packages.go](../../internal/check/packages.go) | 对解析到的真实目标检查 crate 公开边界；保留成员权限、Go internal、main 和循环规则，保持类型身份 |
| Go 降低与产物发布 | [canonical_path.go](../../internal/lower/canonical_path.go)、[lower/imports.go](../../internal/lower/imports.go)、[layout_bundle.go](../../internal/project/layout_bundle.go) | 消费已解析叶子包，生成普通 import 并发布完整 bundle；共享库不复制、不加包装，核对重复生成一致性 |

全量根声明保留占名，活动请求决定可用性；包归属与公开路径按 crate 检查，不因同 module 放行。包加载与最终生成产物只纳入选中入口、lib 及明确可达子包，项目 CLI 显式选择 lib/bin/example。当前完成范围和回归入口见状态表。

### 输入入口

| 输入 | 当前流程入口 | 实施范围 |
| --- | --- | --- |
| `goxide.toml` 项目 | [cmd/goxide/compile.go](../../cmd/goxide/compile.go) → `compile.PrepareProject` | 接通 lib/bin/example 的 crate 模型与单目标操作 |
| 单文件 | 同文件 `compileSourceFile` → `typecheck.NewNamespaceImporter` | 共用根优先级、真实 Go 子包解析、fallback 和来源身份核验；没有清单 target 就不猜 lib |
| 原生 Go 包 gen/transpile | [generate.go](../../cmd/goxide/generate.go) → [lower/package.go](../../internal/lower/package.go) | 保留外部 roots 根声明、删除子包映射，接通真实 Go 子路径和 fallback；覆盖与 NamespaceImporter 不同的包导入路径 |
| 原生目录 build/run/test | [cmd/goxide/project.go](../../cmd/goxide/project.go) 的 `runDirectoryArgs` → 临时清单 → `PrepareProject` | 临时 helper bin 是执行适配，不能被当成作者 crate 而改变原生包选择 |
| 浏览器单文件/多文件 | [playground/package.go](../../internal/playground/package.go)、[platform_wasm.go](../../internal/typecheck/platform_wasm.go)、[website/build.py](../../website/build.py) | 使用配套工具链的标准库 origins/export 快照，保持 CLI/WASM 规则一致；浏览器不执行 Go 工具链查询 |
| host/build helper | [cmd/goxide/roots.go](../../cmd/goxide/roots.go)、[buildscript/prepare.go](../../internal/buildscript/prepare.go) | 保留独立根和权限上下文，删除映射字段不能把 target 根透传给 host；脚本 CLI 的重启仍属原阶段 |

标准库包身份的现有证据入口是 [module_importer.go](../../internal/lower/module_importer.go)、[typecheck/importer.go](../../internal/typecheck/importer.go) 和 WASM importer 的 `PackageOrigin`。fallback 属于名字绑定与导入上下文，不属于依赖下载/选版或最终 Go 输出阶段。

### 实现顺序

以下顺序对应共享的构建流程；各入口使用同一根解析与包身份合同。

1. **确定 target 与包归属。** 保留既有入口选择和根投影，确定 lib/bin/example 根、原生 Go import 闭包及同 module 内的 crate 公开边界。文件型 target 的 `mod` 从原始源目录定位，子包保留 module 相对原路径；同目录文件型 bin 仍有独立 crate，共享包归入 lib，不能重复分配 crate 身份。
2. **接通路径语义与标准库。** 加入 `crate::`、自动库名及宏定义点上下文；统一显式根优先、完整声明占名和根级 fallback。外部纯 Go 来源通过 `use alias::真实子路径` 按需加载，核验实际 module owner，区分子包与导出成员并报告歧义；Goxide 来源仍沿 `pub mod` / `pub use`。覆盖纯 Go `mod`、无根包的 module、原生 roots 与浏览器工具链快照，lower 只消费已确定身份。`go` 依赖继续是 module+version，path/Git 继续选择独立来源根。
3. **接通目标操作。** 项目模式开放现有 `--lib` / `--example` 选择：lib 支持 transpile/build/test，bin/example 支持 transpile/build/run/test，run lib 给明确诊断。继续显式选择目标，不增加 all-targets，也不在本任务重启 workspace、import-go、vendor 或脚本 CLI。
4. **删除配置并同步使用处。** 删除顶层、dependency、workspace 模板和原生 roots 中全部 `namespaces` 字段的读取、写出、构建输入和摘要，以及 `PackageNamespaces`；更新相关夹具、示例和网站源码。保留 dependency alias 及原生 roots 根声明，不改成新的逐包映射配置。外部 Go 子包改用真实子路径，本地包按所属 crate/lib 访问；默认同名标准库的无额外语义声明可省略。删除失效断言，不新增旧格式专用拒绝测试或兼容层。生成包布局按所属 target/lib 收敛；脚本执行功能保持关闭。
5. **验收、独立审阅和收口。** 执行 RFC 0028 的行为矩阵与下列检查，独立审阅包身份、公开边界和 fallback 错误路径；根据实际结果同步静态路径、前端 AST、文法、转译语义、TOML schema 及状态表。全部验收后再报告完成。

实施范围限 `goxide/` 的相关编译器、CLI、测试、文档和网站源码；不改相邻 `std/`、`serde/`、本地 `playground/` 或网站部署仓库，不发布、不推送、不发起 PR。

### 完成标准

- 一个 lib、两个 bin、一个 example 真实 build/run/test，共享类型身份；无 lib、增加 lib、嵌套 target 和文件型 target 分别覆盖。正反例验证兄弟包权限、跨 target 私有访问、main 反向导入。
- 同包 Go/Goxide 双向调用、纯 Go/混合子包和 Go import-only 辅助包；宏定义点根与显式调用点路径分别验证。只修改一个 bin 不改变 lib 的生成内容。
- 默认标准库、显式别名/覆盖、未激活 optional、错误来源、缺少成员；验证失败不 fallback。单文件、原生目录和浏览器覆盖相同语义；保留原生 roots 与 host 隔离。
- 外部纯 Go module 的根包、深层子包、包成员及无根包路径；Go/path/Git 来源与 replace 均保持原 module 身份，嵌套 module 和子包/成员歧义被拒绝，Goxide 私有树不能经物理 Go 路径绕过。
- 生成包归属、锁文件重放、Go 产物可读性和同输入两次生成字节一致；删除全部映射后，原有依赖子包行为改用真实子路径验证。

使用 go.mod 指定的 Go 版本，`GOTOOLCHAIN=local GOWORK=off`，运行：

```sh
go build ./...
go test -count=1 ./internal/... ./tests/... ./cmd/goxide/...
go vet ./internal/... ./tests/... ./cmd/goxide/...
gofmt -l internal cmd
python3 -B scripts/check_design.py --docs-only
```

网站源码或标准库快照变更时，按[网站验证入口](../../website/README.md#构建)运行当前工作树构建、托管审计及 CLI/WASM 一致性检查，输出仅留本仓本地产物目录。代码测试、真实 Go 运行和网站检查按实际执行结果登记，文档检查不替代这些验收。

## 来源身份与根声明

`.gox` 通过 `use` 绑定静态路径，使用 `::` 访问 namespace、类型和关联项。依赖清单负责声明可用根；声明不会自动建立源文件绑定。原生 `.go` 文件继续使用 Go 的 import 和 selector。

Go resolver 负责 module 版本、replace、校验和与工具链。依赖 alias 选择已声明的来源根：标准库为 `go` 指定的包，纯 Go module 为模块根路径，Goxide 源码模块为自身必需的 `[lib]` 入口。纯 Go 子路径按真实包与导出成员解析，Goxide 公开子路径来自 `pub mod` / `pub use`；不再通过清单逐包映射。

依赖图按来源身份建立节点。alias 是声明它的项目内的引用名，不决定节点身份；指向同一来源节点的活动边合并 feature 请求，不同来源保留独立节点。`go_mod.module` 只描述生成 Go module 时使用的路径，不参与 Goxide 来源节点的合并。生成 Go 前检查最终投影：不同来源若占用相同 Go module 路径，给出冲突诊断，不因 alias 不同而生成重复的 Go 类型身份。

来源节点键 `SourceKey` 与描述已取得内容的 `SourceID` 分开：

| 来源 | 节点键 | 内容与锁记录 |
| --- | --- | --- |
| path、根项目、workspace 成员 | 规范化来源根目录 | 本地内容可变；写锁时目录相对锁的 owner 根表示，允许 `../` |
| Git | 规范仓库地址、已锁定 commit、subdir | rev/tag/branch 只用于取得 commit；不同 ref 指向同一 commit 和 subdir 时合并，键中不含缓存目录 |
| Go module 版本来源 | 逻辑 module path | Go MVS 的选中版本、实际替换来源和校验和进入内容身份；节点键不含版本或下载目录 |

已取得的 Git 或 Go 源码内部若有 path 依赖，该子项目仍属于取得来源，以相对取得根的目录区分，不把缓存中的绝对目录变成本地 path 身份；原有 checkout 边界检查仍适用。

Go 来源经实际生效的显式本地 `replace` 指向某目录时，按该有效 path 归一，可与同目录的 path 请求合并。来源收集允许不同节点声明相同 Go module 路径；随后构造 Go 投影索引时检查唯一性，冲突发生在写临时 `go.mod` 和调用 Go 选版之前。后续选版改变取得内容时重新求解请求，不能把旧版本的 feature 或依赖留在新内容上。

### goxide.toml 项目

依赖来源由 `go`、`path` 或 `git` 声明，依赖项不接受 `package` 或 `namespaces`。外部 Go 来源的 `go` 始终是 module path，与 `version` 配合选版；不改为 package path。完整 schema 见 [清单说明](toml-manifest.md)：

```toml
[dependencies.wire]
go = "github.com/goxide-lang/serde"
version = "v0.0.0-20261009021520-f342a31e20d0"

[dependencies.standard]
go = "github.com/goxide-lang/std"
version = "v0.0.0-20261009104839-c02a09d8c417"

[dependencies.output]
go = "fmt"
```

`wire`、Go 包的声明名和 package path 可以不同；此处 `wire` 选择 serde module，`use wire::json;` 解析真实的 JSON 子包。`use standard::sum;` 同样选择 std module 内的 sum 包。module 根或中间目录没有包时可以继续寻址真实子包，但不能直接访问该目录的成员或请求其初始化。所有源路径段必须是合法的源标识符。

```goxide
package main
use output;
use wire as serde;

#[derive(serde::Serialize)]
struct Message { Text string }

func main() { output::Println("explicit dependencies") }
```

`git`、`path`、`go` 是互斥获取字段，`version` 配合 Go module；单独 version 的 registry 来源尚未实现。`go = "fmt"` 表示真实 Go 标准库，无 module/version 或下载，不接收 features/default_features。optional 仍由正常 feature 规则控制；内部加载校验 Standard，不用包名猜归属。

path/Git 从所选项目根的 `goxide.toml` 中 `go_mod.module` 或 `go.mod` 中 `module` 读取 Go 投影路径，显式 `module` 可选且只作一致性断言。依赖 checkout 可同时带有 Goxide 源清单与发布用的 `go.mod`；两份模块政策须一致，过程宏角色重载沿用此规则。主作者项目仍由 `goxide.toml` 管理 Go 元数据。Git 的 `subdir` 仍选择含自身清单的项目/module 根，不选择普通子包。库作者的 `[lib]` 决定 Goxide 公开根；消费方不重复指定入口，也不能通过真实 Go 子路径绕过 Goxide 私有包。纯 Go 子包按使用路径查询，并核验实际 module owner；同一段的子包与成员冲突时报歧义。

可选 module 依赖同样先经过 feature 解析；全量声明决定根占名，只有活动 dependency 提供可用入口。访问未激活根报错，不由 `use` 启用依赖，也不 fallback 标准库。工具链依赖保留活动请求以提供根，但不参与下载、MVS 或增强模块构建。

workspace 的 `[workspace.dependencies.alias]` 是模板。成员以 `workspace = true` 显式继承来源字段与可选 module 断言，不能在继承项上改写来源；需要另一来源时声明完整 dependency。模板不保留子包映射，不自动导入，也不自动激活成员依赖。

原生 workspace 迁入模式保留已有 Go policy。对外部 Go module 增加源根时，dependency 的 `go`/`version` 必须精确匹配该成员的 `go.require`，子包选择不能引入另一选版要求。实际 MVS 仍可选中更高版本；本地供应源继续由 `go.replace` 指定。此类根只增加活动 Requests，不进入增强成员的 Sources/Children，也不接受 feature 转发。optional 控制根可见性，不删除既有 Go require。原生 workspace 成员之间仍使用显式 member path。

### 本项目入口

同包 Go/Goxide 直接共享包级声明。本 target 内通过 `mod` / `crate::` 选择明确可达的包，bin/example 使用 `[lib].name` 消费本项目库的公开 API；共享生成包也归入 lib。不存在顶层本地包映射或隐式 self dependency，不能把整个 module 的目录树当成当前 crate。完整的目录目标、文件目标和跨 target 边界见 RFC 0028。

### 保留 go.mod 的混合源码项目

没有 `goxide.toml` 的 Go module 由原有 `go.mod` 选择依赖版本。原生 `gen` / `transpile` 从这些依赖发现 Goxide 库的默认名字；需要改名、选择精确包或指定工具链来源时，可在 module 根放置 `goxide.roots.toml`。文件只描述根，版本与 replace 仍来自原有 `go.mod`：

```toml
[roots.output]
source = "toolchain"
package = "fmt"

[roots.codec]
module = "example.org/codec"
package = "example.org/codec"
```

`goxide.roots.toml` 是独立的 alias 根输入，保留 `[roots.alias]` 的 `source`、`module`、`package` 根声明，删除其 `namespaces` 子映射。上例 `use codec::json;` 在已选根下解析真实 JSON 包并核验 `module` 归属。根的 `package` 字段与已删除的 `dependencies.alias.package` 不同；版本与 replace 仍由 `go.mod` 决定，不允许在 roots 文件写 version/path/git 等来源配置。键名区分大小写，未知字段与错误类型直接报错。

```sh
goxide run --go "$GO_BIN" ./app
goxide run --go "$GO_BIN" ./app/main.gox
goxide transpile --roots ./chosen-roots.toml ./app/main.gox
```

目录与 Go module 内单文件只读取所属 module 根的 roots 声明。`--roots FILE` 选择唯一显式根文件，不与发现到的 roots 文件合并，也不在所选显式根失败后回退。原生 `gen` / `transpile` 按下节补充尚未显式命名的 Goxide 库；原生 build/run/test、项目模式、浏览器和 host helper 保持各自已有的根输入。

独立 `.gox` 从源文件目录向上找最近的 `goxide.roots.toml`，遇到 `go.mod` 边界停止；也支持 `--roots`。这只是名字上下文，不让根文件成为版本解析器。未声明根可查询标准库，其他未知 root 给出诊断，不猜外部来源。原有独立运行的支持库配置继续由 `--runtime` 指定的工具链清单提供。

已有 `goxide.toml` 的项目和 workspace 使用 dependencies 根声明，拒绝 CLI `--roots` 覆盖或同根的 `goxide.roots.toml` 双重配置。迁入作者项目后，模块版本权威仍在 TOML 内。

### 原生 gen 的默认库名与获取

本节适用于所属 Go module 没有 `goxide.toml` 的 `gen` / `transpile`，包括包目录、单个 `.gox`、标准输出和指定输出文件。`gen` 与 `transpile` 继续是同一入口的两个名称。独立文件没有所属 `go.mod` 时沿用既有 roots / runtime 输入，不搜索其他项目或整个模块缓存。

1. 在查询前记录作者 `go.mod` 的 `require` 条目，包含带 `// indirect` 的条目。它们是默认库候选；Go 选版为这些逻辑 module 确定实际版本。仅经传递依赖到达、未列在作者 require 中的 module，以及只有 replace 而没有 require 的 module，不成为默认根。
2. 使用 Go 工具查询实际选版与 replace。缓存缺失时通过 `go mod download` 取得选中的模块内容，遵守用户的代理、私有模块、校验和与网络设置；本地 replace 直接读取其目录。不得运行 `go mod tidy`，不得自行请求 `latest`、扫描缓存中的其他版本或增加另一份锁文件。自动发现和下载使用私有 module 配置副本，保留作者 `go.mod` / `go.sum` 的原字节。
3. 从选中源码的 `goxide.toml` 读取合法的 `[lib].name` 作为默认根名，由 `[lib].path` 选择入口；没有 Goxide 库清单的普通 Go module 不产生默认根。replace 改变内容来源，逻辑根身份仍是原 require module。宏角色、依赖和重导出继续由库自己的清单决定。
4. 显式 roots 保持优先；某 module 已有显式映射时，不再为它补默认根，从而保留改名和精确包限制。其他默认根不能覆盖显式 alias。多个自动候选同名时，在使用该名字时列出候选 module 并诊断歧义，不能按 require 或缓存顺序选择。已有本地声明、`use` 绑定与真实 Go 标准库解析保持原结果；与工具链根同名的 Goxide 库通过显式 alias 使用。
5. 发现候选与准备源码依赖分开。只因 require 中存在一个未使用的库，不能构建它的过程宏、执行其宏或把它的源码加入当前目标。实际根引用按条件选择后的语法与既有卫生绑定解析，覆盖宏生成的引用；不以正则搜索源码文本代替名字解析。

缺失声明、下载失败、校验失败、库清单错误、同名歧义和宏入口不存在分别诊断。已声明版本不可用时，不改用编译器推荐版本。自动发现共用现有源码依赖准备和过程宏宿主，不建立 gen 专用宏登记表。

### gen 的 std/serde 安装提示

CLI 内置 std/serde 的模块路径与推荐公开版本，仅用于缺依赖提示；推荐版本由构建时的编译器 require pin 提供。宏实现、库默认名及协议语义仍从实际选择的库源码读取。

当当前 `.gox` 的有效引用需要 std/serde，而所属 `go.mod` 没有相应 require 时，交互式 gen 展示所需模块、明确版本与将执行的 Go 命令，询问是否添加。只有终端输入和终端诊断输出均可交互时才询问；空回答、否定或 EOF 都不添加。拒绝、非交互运行和没有可用推荐版本时，返回缺依赖诊断及可手动执行的命令，不等待 CI 或管道输入。

用户同意后才通过正常 Go 工具更新作者的 `go.mod` / `go.sum` 并下载所需内容；准备修改时使用私有副本，成功后发布两份文件，再重新读取 module 图继续本次生成。失败不留下半更新配置，不覆盖提示期间其他进程对作者配置的修改。已有 require、显式 roots 的其他来源、关闭的 cfg 分支、注释或字符串里的库名均不触发建议。一次收集当前可确定的缺失模块并共同确认，不为同一个模块重复询问。自动发现不等于自动升级依赖。

纯生成期依赖需要 Go 可识别的依赖引用，才能在使用者主动运行 `go mod tidy` 后保留 require。整包原位 gen 对实际使用、尚未被普通 Go import 保留的源码库，生成带 `goxide_tools` build tag 的 `goxide_tools_gox.go`，引用库提供的可加载入口；默认 Go 构建不加载这些生成期包。再次生成时同步移除已不用的引用。单文件、标准输出和指定输出文件保持单一产物，并提示所需的手写生成期依赖文件。不能把 compiler pin 当作 tidy 后无声明时的隐式依赖版本。

### gen 产物与验收

原位 gen 交付的 `_gox.go` 必须能由作者的 Go module 独立构建。生成阶段的私有 modfile、临时依赖替换和已清理的目录不能成为产物要求；不能以使用临时依赖的类型检查通过代替这个检查。

发布输出前，使用保留作者原始 require / replace 的私有 Go 配置核对生成包与生成期依赖入口；该检查可以复用已下载的校验和，但不得引用生成阶段的临时库替换。所选库版本只有 `.gox`、缺少可加载的 Go 包或稳定操作入口时，明确诊断该版本需要补齐 Go 发布产物，不留下声称可独立构建的输出。

上述作者依赖图检查适用于原位写入，包括只选择一个源文件的原位生成。标准输出和指定输出文件可交付给另一个消费模块，继续在准备好的依赖图上检查生成代码，由接收模块提供对应的 Go 库产物；指定输出目录则连同生成模块和依赖一起检查、导出。这些输出模式均保留默认库名发现。只有 TOML 清单的本地源码 replacement 在私有目录中复用项目规划器投影依赖图，不向源码目录补写 `go.mod`，也不忽略 Go 选版错误。

普通 Goxide 库面向原生 Go 消费时，同版本发布语义源码和稳定可导入的 Go 定义、运行时及泛型操作入口。稳定库产物使用带 Goxide 生成标记的 `*_gox.go`；编译源码库时，只在同目录或库文件入口映射中仍有对应 `.gox` 且标记吻合时忽略该派生 Go 文件并重新生成，手写 Go 保留。消费者新增的特化不能要求已发布依赖增加符号；优先使用稳定的泛型或操作参数入口，确需具体 helper 时放在消费包，并保持定义点解析、私有访问、命名类型身份和求值顺序。不得通过有限示例枚举未来全部类型，也不得为 gen 恢复 std/serde 内建实现。

验收覆盖无 roots 的三类过程宏和正式 std/serde、显式 roots 改名与精确包、同名冲突、Go 选版与本地/远程 replace、冷缓存获取及校验失败、无关 require、cfg 与卫生引用；交互接受/拒绝/EOF、非终端和已有依赖分别检查作者配置与生成输出。用真实 `go generate` 生成后清除临时依赖，再由原始 Go module 执行 build/run/test；增加用户类型、切片与跨包调用，运行 tidy 后重新生成，并核对两次输出字节一致。现有项目、原生入口、浏览器和宏隔离回归保持通过。

### 包身份、加载与锁

实际使用某个包时，frontend 检查所加载 package 的精确 path，并通过 Go 返回的 `Module.Path` 或 `Standard` 核验其 module/toolchain 来源。用相同前缀的嵌套 module 冒充父 module、把普通模块伪装成 toolchain、错误 package owner 都不能通过。Go 的 `internal` 限制和 Goxide 的公开边界均须满足；自动子包解析不绕过这些边界。没有按最长可导入前缀搜索的路径回退。

根声明本身不加载所有包。纯 Go 子包按使用路径查询，路径分组不递归导入所有子包；module 根无包不妨碍使用真实子包。根在源文件经过 `use` 才能访问；原生 Go 包的声明名不能改变 alias 的含义。

来源字段、alias 根、可选 module 断言与 feature 配置作为 manifest 的组成部分进入 project、workspace、lock 和来源摘要；读取到的 module 身份与库入口用于构建相应根上下文。`--locked` 拒绝清单变化后复用不匹配的计划。原生 Go module 的显式根上下文进入 frontend 配置指纹；不同根声明不能复用相同编译身份。删除映射字段时同步删除对应读取、写出和摘要输入，不保留旧配置兼容分支。

共享解析器是 `internal/rootconfig`。CLI、直接调用 module frontend 的库用户和 Playground 都构造同一 `paths.Roots`；Playground 的根清单是显式应用输入，不是源语言内置包名白名单。

### Host 构建脚本

`.gox` 构建脚本与 host helper 的 lowering 保持独立的 toolchain roots 和标准库上下文，沿用 `use` / `::` 语法。目标的 module dependency、crate 与自动库根不透传到 host；自动 Go 子包解析不会授予 target 依赖作为 host helper 执行的权限。显式工具链根进入脚本冻结输入、缓存及精确授权 subject 的身份。

原生 `.go` host 脚本继续通过现有 `build.dependencies` 显式 helper 快照使用 Go import。`.gox` host 源对 module 根的 `use` 需要未来独立 host namespace 输入；当前给出缺失根诊断，不从 target dependency 或 module path 猜 alias。工具链根之外的 host 映射在 runner API 也明确拒绝。

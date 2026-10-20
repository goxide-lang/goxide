# RFC 0022：feature、条件编译与项目构建

## 状态与范围

已实现：feature、`#[cfg(...)]`、`#[cfg_attr(...)]` 与 `cfg!(...)` 接入新管线。条件属性在名字和类型检查前选择有效 item；cfg! 只产生布尔常量，if 两支按普通语言规则检查。条件配置覆盖 feature、五个 target 字段和原始 env，同时覆盖原生 Go 的 `go generate` 和单项目 `goxide.toml`；当前入口和测试见[能力状态表](../mvp-gap-audit.md)。

首批支持完整顶层 item、条件属性和布尔查询，包括宏生成的这些形式。字段、变体、成员等内部节点后续接入。工作流目标、环境快照与查询入口见下文。lib/bin/example 的 CLI 目标选择已随 [RFC 0028](0028-crate-roots.md) 接入；workspace、构建脚本与跨目标执行仍未开放。

前端接入以[新前端合同](../design/frontend-ast.md#84-feature-条件编译合同)为准，使用 AST 和宏来源信息。项目模式复用 `internal/project` 的 feature 图，将每模块结果传入 `internal/compile` 与新前端；原生 Go 模式直接传入当前包的显式 feature 集。旧 `//goxide:cfg` 注释不作为配置入口，不保留文本预处理和旧 metadata 机制。

依赖 schema 与项目发现以 [goxide.toml schema](../design/toml-manifest.md)为准：Go 来源显式写 `go` 加 `version`，Git 可省略 ref 并锁定默认分支，registry 未实现。TOML 作者项目采用[统一项目模型](0025-unified-project.md)，根不保留 go.mod/go.sum；原生 Go 项目继续以自身 go.mod 为依赖权威。下文的多目标、来源与发布合同按各自阶段接入。

## 形式模型与阶段边界

记配置为 `C = (F, T, E)`：`F(m)` 是来源节点 m 的最终 feature 集，T 是有效目标，E 是本次调用捕获的原始环境。语法见[词法与语法](../design/language-grammar.md)，阶段输入输出与完整规则见[转译语义](../design/translation-semantics.md)。

```text
⟦feature("x")⟧C,m = x ∈ F(m)
⟦target_os("x")⟧C = (T.os = x)             其他 target 字段同理
⟦env("k")⟧C       = k ∈ domain(E)
⟦env("k", "v")⟧C = (k ∈ domain(E) 且 E[k] = "v")
⟦all(Pi)⟧C = ∧i ⟦Pi⟧C    ⟦any(Pi)⟧C = ∨i ⟦Pi⟧C    ⟦not(P)⟧C = ¬⟦P⟧C

SelectC(#[cfg(P)] item) = item 或 ε，取决于 P 的布尔值
ExpandC(cfg!(P)) = 布尔常量；不删除外围 if 分支
```

每个当前处理到的完整谓词先校验，再求值。cfg_attr 的选中载荷递归规范化，多个有效 cfg 取 AND；未选载荷与删除项子树不进入后续处理。源码先完整解析，配置选择在首次名字/宏/import 收集前执行；宏输出重复“解析 → 选择 → 展开保留部分”，最后进入名字和类型检查。

feature 图采用请求并集的最小不动点：根请求开始，反复加入本地 feature 展开、活动依赖边的转发与默认请求，直到不再变化。所有到同一来源节点的请求合并，关闭一条边的 defaults 不能撤销另一条边。配置构造的来源节点决定 F(m)，T/E 为整次编译共享。

生成身份包含实际读取的环境键及其存在性和值；读取记录在动态宏处理完成后收敛。配置变化影响生成缓存，依赖图不变时锁文件保持不变。工作流选择 T 的规则、原始环境与 Go 执行环境的区别见下文。

## 源级规则

模块根目录显式放置 `goxide.toml`，声明生成 Go module 所需的 `go_mod.module` / `go_mod.go`，未知字段报错。Goxide 的 `use` 使用依赖别名；手写 Go 的 import 仍写 Go package 路径。下例展示当前支持的 go_mod/dependencies/features/lib/bin/example 清单字段组合：

```toml
[go_mod]
module = "example.org/app"
go = "1.27"

[[bin]]
name = 'cli'
path = 'src/main.gox'

[[bin]]
name = 'web'
path = 'src/web.gox'

[dependencies]
[dependencies.client]
default_features = false
optional = true
path = '../client'

[[example]]
name = 'network'
path = 'examples/network.gox'
required_features = ['network']

[features]
default = ['pretty']
network = ['dep:client', 'client/tls']
pretty = []

[lib]
name = 'app'
path = 'src/lib.gox'
```

`[lib]` 为零或一个表；`[[bin]]` / `[[example]]` 是显式数组表，不自动扫描约定目录、父目录或相邻项目。每个类别内名称唯一；bin 和 example 可以同名，因为选择 flag 不同。目标 path 必须是模块内目录或 `.gox` / `.go` 文件，不能越过嵌套 `goxide.toml` 或 `go.mod`。未注册 helper 仍按 Go 目录包处理，不能隐式混合多个 main。

目标边界采用 [RFC 0028](0028-crate-roots.md) 的实施合同：每个 lib/bin/example 有独立 `crate::`；bin/example 经 `[lib].name` 使用本项目库的公开 API。原生 Go helper 按所属 target 的包图纳入，同 module 不授予跨 target 私有访问；库名须是合法源标识符并避免保留根和依赖 alias 冲突。

文件入口会显式映射：lib 文件到生成 module 根（包名由源文件决定）；bin/example 到 `goxide_targets/<类别>/<名称>/`。因此同一 src 目录中的多个 main 文件可以分别选择，并通过原 module path 导入 lib。目录入口保持相对目录包路径。文件入口只搬移该文件，不搬移同目录 helper 或相对 `go:embed` 资源；含这些内容的 CLI/web 应选择目录入口。所有目标入口从普通目录源中排除，只恢复选中入口和 lib；最终生成 Go 路径冲突拒绝，不覆盖文件。`goxide_targets` 为目标映射目录。

### 两种配置来源

| 模式 | 配置来源与作用域 | 未声明的 feature |
| --- | --- | --- |
| 原生 Go | `goxide gen --features=json,trace .` 显式指定当前目录包的集合；缺省为空。手写 `.go` 中的 `//go:generate` 保存这条命令 | 没有声明目录；未启用的名字为 false，因此不诊断合法名字的拼写错误 |
| goxide.toml | `[features]` 声明目录；根默认集合与 CLI 请求、依赖默认集合与转发请求按下节求解；结果属于各自 module | CLI 请求、依赖请求和实际处理到的谓词都检查所属模块声明，未知名报错 |

feature 名沿用清单的名称规则、区分大小写。`--features` 沿用逗号或空白分隔的字符串，重复 flag 仍取最后一个值；对其最终列表去重并排序。CLI 只提供根请求；依赖通过清单边收到请求，根模块同名 feature 不自动传播。

原生 Go 的 `--features` 不读取环境变量、Go build tags、go.mod 中的依赖名或另一个配置文件，也不扫描源码中的查询来生成声明目录。`--no-default-features` 仅用于 TOML 模式，原生 Go 没有默认集合。一个包的生成命令不重配或重新生成其他包；同包多文件统一使用本次集合，跨包生成分别执行各包自己的命令。

原生 Go 条件编译的原位生成以整个目录包为单位：使用配置语法或显式 feature 选择时，单文件原位 `gen` 应提示改用目录输入，避免把不同配置的 `_gox.go` 混在同一包。独立单文件编译或标准输出转译可使用显式集合；缺省同样为空。所有入口共用条件选择规则。

`go generate` 将本次选择固定在生成的 Go 中。切换 feature 要重新执行生成命令，再 build/test；`go build`、`go test` 不自动执行生成器，`go generate -tags=...` 也不把参数自动传成 `goxide --features`。手写 `.go` 仍按 Go 文件名/build tags 选择并正常检查，Goxide feature 不直接裁剪它。

### Feature 图

feature 的数组元素支持：本模块 feature 名、`dep:alias`（只激活 optional 依赖）、`alias/feature`（激活依赖并转发 feature）。依赖还可声明 `features: ["x"]` 和 `default_features: false`。

- 默认启用已声明的 `default`；`--no-default-features` 仅关闭根模块的默认请求，不能撤销其他显式请求。
- 所有到同一来源节点的请求取并集；任一边要求默认 feature，就启用其默认 feature。不能以一条边的 false 撤销另一条边的 true。
- 未知本地 feature、未知依赖、feature 循环、模块依赖循环诊断；所有本地 feature 边即使未启用也检查。转发到依赖的 feature 名在依赖被激活读取后检查，未激活 optional 来源不下载。
- 不隐式生成与 optional 依赖同名的 feature。version/path/git 是获取渠道；活动 manifest 边取得 goxide.toml 源码后使用相同 feature/default/optional/lib 规则。没有 goxide.toml 的纯 Go 或固定配置生成 Go 不接受显式 goxide feature 请求。仅 go.mod require/indirect 参与 Go 选版，不隐式开启 goxide defaults 或执行脚本。
- TOML go.require 不因 goxide optional 关闭而被删除；显式 Go 依赖仍由 Go 解析。

每个 module 在一次构建中只有一份最终 feature 集，其包共享该集合。`required_features` 在图求解后检查最终集合，不隐式开启 feature；选中的 lib/bin/example 及依赖 lib 均执行该检查。删除条件 `use` / import 只影响源码包绑定，不反向删除清单里的非 optional 依赖；避免取得依赖必须使用未激活的 optional 边。

### 谓词

```text
Predicate = feature("name") | all(Predicate, ...) | any(Predicate, ...) | not(Predicate)
```

`cfg`、`cfg_attr` 与 `cfg!` 共用这套谓词。`feature` 恰有一个字符串字面量参数，`not` 恰有一个谓词；`all()` 为 true，`any()` 为 false。允许多行和尾随逗号，不接受变量、函数调用或用户宏作为谓词求值。未知谓词、错误参数数量或类型报源级诊断。

当前处理到的整个谓词先校验再求布尔值；`all` / `any` 的短路不能藏住未知谓词或 TOML feature 拼写错误。已被外层 cfg 删除的子树、未选中的 cfg_attr 载荷不再求值其中的配置查询；普通 if 的两支仍处理查询。feature 名与谓词名是不同名称空间：`feature("test")` 可以是普通用户 feature，不能因此启用内建 `test` 上下文。

### cfg 与 cfg_attr

```goxide
#[cfg(feature("json"))]
use serde::json;

#[cfg_attr(feature("debug"), derive(Debug))]
#[cfg_attr(feature("json"), derive(Serialize, Deserialize))]
struct Message { Text string }
```

以上为独立片段；启用的 serde 派生仍需正常的协议导入和依赖。首批两种属性均作用于完整顶层 item：函数、类型/struct/enum、trait、impl、declare、use/mod、宏定义和 item 宏调用，以及整组 const/var/type 声明。`.gox` 使用 use 声明生成 Go import。分组中的单个条目和结构内部节点后续接入；`#![...]` 文件属性不在首批。

- `#[cfg(P)]` 为 false 时删除整个 item；同一 item 上多个 cfg 取 AND。被删除的项不占名字，不登记宏或协议，不装载其源码 import，也不展开或检查项内代码。
- `#[cfg_attr(P, A1, A2, ...)]` 至少有一个属性参数，各属性省略外层 `#[...]`。P 为 true 时在原位置依次插入这些属性，为 false 时只删除本条 cfg_attr，保留宿主 item。
- 当前节点的配置属性先验证外层参数数量、括号和属性列表结构；false 不能隐藏当前 cfg_attr 缺少属性参数等结构错误。未选载荷中的嵌套 cfg/cfg_attr 不调用处理器，其参数数量、谓词与声明检查延后到载荷被选中时；载荷内普通属性的具体参数规则由后续属性处理器检查。TokenTree 的词法与括号错误始终由解析器报告。
- 允许产生 `cfg`、嵌套 `cfg_attr` 和普通属性。对选中的嵌套属性递归处理，最后用全部有效 cfg 决定是否保留 item；只有保留项才做 derive、serde 等普通属性的绑定与检查。重复或冲突的有效属性按现有规则诊断，不自动去重。
- 同一 item 上直接生效的配置属性都完成谓词校验，不因另一个 cfg 为 false 提前跳过，交换这些属性不会改变是否报错；false cfg_attr 的载荷不进入属性语义检查，外层已删除项的子节点也不遍历。普通属性的顺序保持不变。

例如 `#[cfg_attr(feature("strict"), cfg(feature("json")), derive(Debug))]`：strict 关闭时保留原项；strict 开启时要求 json 才保留该项，并在保留后执行 Debug 派生。条件求值和派生顺序不依赖属性在源码中靠前还是靠后。

### cfg! 与条件分支

`cfg!(P)` 是编译器内建布尔常量查询，在所有表达式位置都只展开成 true 或 false。`if cfg!(P)` 生成普通的 `if true` / `if false`，两支仍按普通 if 语义参与宏展开、名字、类型、引用权限与控制流检查；常量条件不提供检查前的分支裁剪。后续 Go 编译优化可以删除死代码。

在 const、返回值、结构体字段值、复合布尔表达式及其他控制流中使用 cfg!，同样遵循普通布尔常量规则。经变量或 const 别名使用时也没有额外的配置选择语义；初始化语句、作用域与控制流沿用原有规则。

因此 cfg! 不能保护对已被 cfg 删除的函数、类型或导入的引用，也不能跳过未知宏。需要移除这些代码时，首批用 cfg 选择完整函数或其他顶层声明；函数内部的条件属性位置另行接入。

### 解析、裁剪与宏

选中的源码文件先完整 `ParseFile`，再做配置属性选择；关闭项中的普通源语法错误仍报错。宏参数/模板及属性参数继续遵循已有 TokenTree 解析边界：解析器验证词法与括号，宏输出形成具体片段时再检查该片段的语法，不为 cfg 删除的代码展开用户宏。

源码中的 cfg_attr/cfg 处理必须早于依赖根绑定、首次名字收集、宏收集和源码 import 发现。处理保留的宏调用时，先形成输出片段并解析，再对输出应用同样的 cfg_attr/cfg 规则，最后递归展开保留部分。cfg! 在宏展开阶段求布尔值；用户宏生成的 cfg! 也仅替换该表达式，不选择其所在 if 的 body。展开完成后执行最终名字解析、派生、类型检查和降低。

宏依赖分析只能记录条件属性处理后的有效部分，不能预先扫描全部原始模板路径并加载 cfg 删除项内的宏或报告它的依赖环。为形成输出而调用的外层宏仍是必要依赖；普通 if 两支内的宏都参与展开与依赖检查，保留部分的真实循环仍报错。保留现有展开预算与循环诊断，不添加文本预处理旁路。

feature 的所属模块取配置构造的语法来源：模板写出的 cfg/cfg_attr/cfg! 使用宏定义模块；完整捕获的配置构造保留调用点来源。仅将字符串或谓词参数代入模板不改变模板构造的归属。cfg_attr 生成的每个属性保留对应载荷的来源，嵌套转发也不改归属。跨模块宏结果不能统一套用输出包的 feature 集。保留 Span、Origin 与展开栈；生成的布尔常量不能被局部 `true` / `false` 绑定遮蔽。`cfg!` 的内建宏身份保留，用户宏或宏别名不能占用；普通变量、函数名 cfg 仍可使用。

### 后续属性位置

内部节点的 cfg/cfg_attr 沿用“先选择、再按剩余程序检查”的规则：字段影响布局/构造/派生，enum 变体影响表示与 match 穷尽性，trait/impl 成员影响协议完整性，match arm 和语句影响控制流。不会自动修补仍指向已删除节点的引用。参数、实参和必填表达式的删除另行设计；首批通过完整声明表达配置选择。

## 工作流与编译目标

`gen` 与 `transpile` 同义。先按动作和输入归属确定工作流，`-o` 只改变输出位置；发现项目时在最近的 go.mod 边界停止，嵌套原生 Go 模块不受外层 TOML 接管。显式单文件仍按单文件输入处理。

| 工作流 | 目标来源 | `--target` |
| --- | --- | --- |
| 原生 Go 包或独立文件的 gen/transpile | 捕获的进程环境与所选 Go 的有效 GOOS/GOARCH，包括 Go 环境文件默认值 | 拒绝 |
| TOML 项目的 gen/transpile | 显式目标；缺省为原生 Goxide 进程宿主 | 接受 |
| expand/build/run/test，包括原生 Go 输入 | 显式目标；缺省为原生 Goxide 进程宿主 | 接受 |

```sh
goxide build --target amd64-unknown-linux .
goxide build --target linux/amd64 .
```

主格式为 `<GOARCH>-unknown-<GOOS>`，同时接受 `<GOOS>/<GOARCH>`，两者归一化为同一配置身份。中间段固定 unknown，没有 vendor/ABI 含义；不接受第四段 ABI 或 Rust 架构别名。目标必须同时处于下文封闭值域和所选 Go 实际支持列表内，非法组合报错。

宿主来自原生 Goxide 进程的 OS/架构；工具链模式默认值不受进程 GOOS/GOARCH 或 `go env -w` 改写。跨目标展开、生成与 build 可用；非宿主 run/test 在生成之前拒绝，不寻找模拟器或远端 runner。清单不增加 target 字段。

## 统一条件配置

实现与验收进度见[能力状态表](../mvp-gap-audit.md)。共同配置模块承载具体数据与函数；Configure、Expand、Resolve、Check、Lower 共用该上下文，不增加注册表或另一条编译管线。

### 谓词目录与封闭值域

带值谓词只接受字符串字面量。公开谓词仅包含下表和组合算子 all/any/not；compiler/gc、cgo、race、test、debug_assertions、target_env/abi/vendor/feature 均不开放。`target_ending` 不作为 target_endian 的别名，也不提供 target_family。

| 谓词 | 合法参数 | 来源与含义 |
| --- | --- | --- |
| `feature("name")` | 项目自定义名称；TOML 限所属模块声明，原生 Go 名称开放 | 每模块最终 enabled 集；保留宏构造来源规则 |
| `target_os("value")` | aix、android、darwin、dragonfly、freebsd、illumos、ios、js、linux、netbsd、openbsd、plan9、solaris、wasip1、windows | 有效目标 GOOS，精确相等 |
| `target_arch("value")` | 386、amd64、arm、arm64、loong64、mips、mips64、mips64le、mipsle、ppc64、ppc64le、riscv64、s390x、wasm | 有效目标 GOARCH |
| `target_platform("value")` | unix、windows、wasm、plan9 | 互斥平台类别 |
| `target_endian("value")` | little、big | 目标字节序 |
| `target_pointer_width("value")` | "32"、"64" | Go 目标指针位数 |
| `env("NAME")`、`env("NAME", "VALUE")` | 非空环境键、原始值 | 原始进程环境的存在性或精确值比较；无需登记 |

已知但不匹配当前目标的参数返回 false，未知参数报错，例如 `target_os("Linux")`。数字参数 `target_pointer_width(64)` 拒绝。

### 目标组合、类别与布局

下表以本项目核验的 **Go 1.27.1** 为基线，完整列出正常支持的 47 个 OS/arch 组合。实现还必须用所选工具链的 `go tool dist list -json` 验证具体组合；不能取 OS 与 arch 的笛卡尔积，不能把 `-broken` 的目标或含历史/未来名称的 KnownOS/KnownArch 表直接作为支持集。工具链出现本规范尚未认识的目标时明确拒绝并要求更新值域/映射，不静默归类。完整值域是语言配置词汇，实际可编译范围还受工具链、前端及执行入口限制。

| target_os | 可配的 target_arch | target_platform |
| --- | --- | --- |
| aix | ppc64 | unix |
| android | 386、amd64、arm、arm64 | unix |
| darwin | amd64、arm64 | unix |
| dragonfly | amd64 | unix |
| freebsd | 386、amd64、arm、arm64 | unix |
| illumos | amd64 | unix |
| ios | amd64、arm64 | unix |
| js | wasm | wasm |
| linux | 386、amd64、arm、arm64、loong64、mips、mips64、mips64le、mipsle、ppc64、ppc64le、riscv64、s390x | unix |
| netbsd | 386、amd64、arm、arm64 | unix |
| openbsd | 386、amd64、arm、arm64、ppc64、riscv64 | unix |
| plan9 | 386、amd64、arm | plan9 |
| solaris | amd64 | unix |
| wasip1 | wasm | wasm |
| windows | 386、amd64、arm64 | windows |

平台类别是 Goxide 的单值分类，Go 本身没有 `target_platform` 枚举。当前 unix 集对应 Go 的 UnixOS，js/wasip1 归入 wasm，windows 和 plan9 各自成类；没有 `other`、`posix`、`mobile` 或多类别匹配。`target_os("linux")` 不匹配 android，darwin 不匹配 ios，solaris 不匹配 illumos；Go build tags 的别名规则不改变精确字段。依据见 [Go 目标列表实现](https://go.dev/src/cmd/dist/build.go)与 [Go UnixOS 定义](https://go.dev/src/internal/syslist/syslist.go)。

| target_arch | target_endian | target_pointer_width |
| --- | --- | --- |
| 386 | little | 32 |
| amd64 | little | 64 |
| arm | little | 32 |
| arm64 | little | 64 |
| loong64 | little | 64 |
| mips | big | 32 |
| mips64 | big | 64 |
| mips64le | little | 64 |
| mipsle | little | 32 |
| ppc64 | big | 64 |
| ppc64le | little | 64 |
| riscv64 | little | 64 |
| s390x | big | 64 |
| wasm | little | 64 |

这些字段由目标唯一派生；Go wasm 的指针宽度为 64。前端 go/types 尺寸、Go importer 导出数据、文件筛选与最终 Go 命令使用相同有效目标。CGO_ENABLED、GOARM、GOAMD64 等后端设置交由所选 Go 在该目标上下文解释，不作为 Goxide 谓词，也不一律清空或强制禁用 cgo。

### 捕获、准备与读取记录

环境按三个步骤准备：先捕获一次原始进程环境并保留不可变副本；再按工作流确定目标；最后构造执行环境。工具链模式先在副本覆盖 GOOS/GOARCH，再调用所选 Go 解析有效设置。原生生成直接解析捕获的 Go 环境。父进程与用户 Go 环境文件不变，后续命令消费解析结果。

```text
清单/feature 参数 → 每模块 feature 集 ─┐
工作流/目标参数/环境 → 环境快照 ──────┴→ Configure → Expand → Resolve → Check → Lower
```

feature 按模块及宏构造来源取值；target 与原始环境在整次编译共享。环境值来自原始快照，不从工具链覆盖后的执行环境查询：原始 `GOOS=darwin` 配合 `--target amd64-unknown-linux` 时，`env("GOOS", "darwin")` 与 `target_os("linux")` 均为 true，Go 子进程使用 linux/amd64。

| 原始快照中 NAME 的状态 | `env("NAME")` | `env("NAME", "")` | `env("NAME", "x")` |
| --- | --- | --- | --- |
| unset | false | false | false |
| 空串 | true | true | false |
| x | true | false | true |

值不 trim、不解析布尔、不做变量替换；"0" 和 "false" 的存在性仍为 true。Go 环境文件中的默认值不冒充进程变量。

不可变快照与可追加读取记录分开保存。只记录实际求值的 env 键、存在性和完整原值；all/any 的所有参数以及普通 if 两支均按现有规则处理，外层 cfg 删除的子树和未选 cfg_attr 载荷不求值。动态宏输出中的查询在实际展开时记录，不能依靠展开前的静态扫描穷举。宏调用不改变环境作用域。

当前 GOFLAGS 继续沿用既有隔离策略，后端命令使用空 GOFLAGS；原始值仍可通过 env 查询。本轮不扩展 GOFLAGS 解析或新增 race 选项，test 保留已有 Go `-race` 行为。文件选择从快照显式构造 go/build.Context，禁止独立依赖 build.Default 的目标。浏览器内存编译明确传入与内嵌 Go 导出数据一致的目标及空进程环境；缺少上下文报错，不把 js/wasm 编译器自身当成生成目标。

### 查询接口：goxide env

```sh
goxide env
goxide env --json --target amd64-unknown-linux
goxide env --list --json --go /path/to/go
goxide env --json GOOS APP_EDITION
```

无名称参数时分别显示 host、go_environment、target，以及所选 Go 路径和版本：它们分别表示 Goxide 宿主、原生生成会继承的 Go 环境目标和工具链工作流的有效目标。`--target` 使用同一准备规则。`--list` 输出五种 target 谓词值域与所选工具链支持的组合。

名称参数查询原始进程环境，区分 unset 与空串；JSON 包含 key、present、value，非 UTF-8 值用 value_base64 表示。名称查询不与 --target/--list 混用。不查询源码、不解析依赖、不运行脚本、不生成产物或锁，也不输出伪完整的构建配置摘要。

### 生成物与配置身份

原生生成固定本次配置，feature/env 改变后须重新生成整个包。实际处理 target 查询的包，为全部生成文件（含空壳）合取精确 GOOS+GOARCH 约束，并保留原 build constraints 和文件名选择规则。linux 约束排除 android，darwin 排除 ios，solaris 排除 illumos。不伪造 env/feature/cgo/race 标签，不自动生成目标矩阵。

配置身份包括规范化目标、有效后端设置、每模块 feature 声明与启用集合、实际 env 读取记录和既有入口/依赖身份。键和值均无损编码，区分 unset、空串和不同原值，不能只 hash 查询布尔结果，也不能 hash 整份进程环境。工具链模式忽略的原始 GOOS/GOARCH 不额外扰动身份；源码查询它们时才按原始读取值计入。

项目先在受管临时目录完成一次编译，取得包含动态宏查询的完整读取记录，再计算 UnitSpec、选择最终目录并发布。Host 与 Target 分开保存，路径继续使用 GOOS-GOARCH 分区；Windows 二进制扩展名沿用目标规则。两种 target 拼写共享目录与字节。源码修改仍重新编译，不新增原生生成或单文件持久转译缓存。

目标与 env 不进入依赖锁身份；feature 改变依赖图时仍按既有锁合同处理。读取原值只存在内存与显式 env 查询输出；持久标记只保存摘要及必要非敏感标识。解析、展开或检查失败不发布新 bundle；后续 Go 构建失败按现有合同保留已生成 bundle/锁但不发布新二进制。

### 验收条件

| 编号 | 行为 | 验收 |
| --- | --- | --- |
| CFG-X01 | 目标格式与映射 | 两种拼写等价；封闭值域、47 组合、布局字段；未知架构、坏组合、vendor/ABI 额外段拒绝 |
| CFG-X02 | 原始 env | unset/empty/值、无登记、宏动态输出、all/any 全参数、普通 if 两支、未选子树；非 UTF-8 键值身份无损 |
| CFG-X03 | 双模式与一致性 | 进程/Go 保存的目标配置；工具链默认宿主及显式覆盖；输出路径不改变输入归属；文件筛选、importer、类型尺寸、Go 命令同目标 |
| CFG-X04 | 查询入口 | 文本、JSON、--list、原始变量查询；不执行源码，不声称完整构建摘要 |
| CFG-X05 | 配置与锁 | env A→B→A、相同布尔不同原值、无关变量不扰动；锁稳定，重复生成字节一致 |
| CFG-X06 | 原生整包生成 | 精确目标约束与原约束合取、全部生成文件一致；保留覆盖保护与 Go 双向调用 |
| CFG-X07 | 跨目标行为 | 多个纯 Go 目标真实 build 并核对二进制；32 位溢出/类型尺寸/平台筛选；跨目标 run/test 生成前拒绝；宿主 build/run/test 通过 |

## 转译示例

### CFG-01：声明与调用使用同一配置

独立输入，包为 `example.org/codec`；TOML 模式须声明 json feature。以下 Go 展示核心输出，省略生成标记和行映射。

```goxide
package codec

#[cfg(feature("json"))]
func encodeJSON() string { return "json" }

#[cfg(feature("json"))]
func Encode() string { return encodeJSON() }

#[cfg(not(feature("json")))]
func Encode() string { return "plain" }

func Mode() string {
    if cfg!(feature("json")) {
        return "json"
    } else {
        return "plain"
    }
}

const HasJSON = cfg!(feature("json"))
```

json 开启：

```go
package codec

func encodeJSON() string { return "json" }
func Encode() string { return encodeJSON() }
func Mode() string {
    if true {
        return "json"
    } else {
        return "plain"
    }
}
const HasJSON = true
```

json 关闭：

```go
package codec

func Encode() string { return "plain" }
func Mode() string {
    if false {
        return "json"
    } else {
        return "plain"
    }
}
const HasJSON = false
```

预期：两种配置都可构建，Encode 与 Mode 分别返回 json/plain。cfg 使关闭配置中的 encodeJSON 声明及其调用者消失；cfg! 保留 Mode 的两支，只改变条件常量。若直接在 Mode 的 json 分支调用 encodeJSON，关闭 json 后仍会因名称不存在而拒绝。

### CFG-02：只选择属性，保留宿主类型

独立输入，TOML 模式须声明 defaults feature：

```goxide
package counter

#[cfg_attr(feature("defaults"), derive(Default))]
type Counter struct { Value int }
```

开启时的核心 Go：

```go
package counter

type Counter struct { Value int }
func DefaultCounter() Counter { return Counter{Value: 0} }
```

关闭时只有 Counter 类型，没有 Default 协议或 helper。开启时 DefaultCounter 返回 Value 为 0 的值；其他派生和手写协议冲突按现有合同检查。

### 依赖、锁定与生成 Go

依赖来源采用当前 schema；Go module 来源写 go/version，Git/path 从所选根清单读取 module 身份，可选 module 字段只作一致性断言。依赖项不接受 package，alias 默认绑定库入口：

```toml
[dependencies.text]
go = "golang.org/x/text"
version = "v0.28.0"

[dependencies.remote]
git = "https://example.org/lib.git"
rev = "release-1"
subdir = "library"

[dependencies.local]
path = "../local"
features = ["fast"]
```

上面仅展示格式，不是测试访问的服务。版本解析、MVS、module 缓存、校验和全部调用真实 Go 工具。TOML 内保留原生 require 与本地 replace 语义。增强依赖与 go.require 的版本请求共同交 Go MVS 选择，不因两个最低版本不同就拒绝；显式本地/Git 来源不能覆盖根 TOML 已有的不一致 go.replace。根 replace/exclude 为全局权威，不提升依赖自己的策略。源 go.mod/go.sum 不改写，修改限于生成副本。

path/git 入口（Git 含 subdir 后）可以是纯 TOML 作者模块，或者自带普通 go.mod 的原生 Go 模块；不借用父目录配置。module 身份来自该根的清单，显式断言必须相等。TOML 模块只在受管目录合成 Go 元数据。Goxide 依赖必须声明自身 lib 入口；纯 Go module 默认使用模块根路径，Go 子包按 `use alias::真实子路径` 查询并核验 module owner；不再配置逐包映射。

本地 path 相对声明它的 manifest，而非 cwd；不递归发现相邻模块。Git 的 rev/tag/branch 选择器可省略，此时选默认分支；首次解析后在 goxide.lock 锁定精确 commit，之后移动 ref 不改变锁。Git subdir 必须在 checkout 内并含自身的 goxide.toml 或 go.mod，不选择普通子包。不同来源节点占用同一 Go module 路径时，在 Go 投影前按 [schema](../design/toml-manifest.md#来源与包身份)诊断。

Git 接受无凭据的 HTTPS URL，另支持绝对本地路径/file URL 用于本地仓库；禁止 URL userinfo/query、SSH 和 ext。命令通过独立 argv 调用，不经 shell，不运行安装脚本/子模块/生成脚本，禁用 hooks、credential helper、askpass、fsmonitor 与外部协议。Git 来源的 path 后代和本地 go.mod replace 不得逃出 checkout。缓存 checkout 必须与锁定 commit 一致且无修改、未跟踪或 ignored 文件；拒绝缓存目录 symlink。

goxide.lock 保存来源 commit、Go 选版政策及校验信息；离线重放还需要对应源码缓存，锁不替代依赖字节，也不冻结本地 path 内容。`--locked` 拒绝缺失或需要变化的计划，`--offline` 使用已有缓存并禁止取得远端内容。缓存和互斥沿用[当前 target 布局](../design/target-layout.md)，配置和锁的来源规则见 [schema](../design/toml-manifest.md#锁缓存与输出)。

在共享规划 main module 中让真实 Go 选版：活动 goxide 约束作为根要求，内部 go.mod 桥保留同模块的多个版本请求，桥继承根 Go directive，保持图裁剪规则。取得实际版本后重新从根计算 goxide manifest/feature 闭包；丢弃旧版本的 goxide 边，重复非相邻状态或超过 64 轮拒绝，不自行裁剪 Go 原生图。只有稳定后才确定增强构建顺序，再做源级条件选择与转译；实际生成时核对 build list。版本、sums、来源计划进入产物配置身份。完整选版合同见 [version 来源设计](../design/version-source-plan.md)；脚本的后续接入见 [RFC 0023](0023-build-scripts.md)。

TOML 模式生成 module 和所需 deps 副本，使用现有受管 target 布局及相对 replace。产物配置摘要必须包含按 module 排序的实际启用 feature 集、现有目标/工具链输入与依赖计划；依赖计划相同不能成为复用不同 feature 产物的理由。集合的顺序和重复请求不影响身份或生成字节。不向生成 Go 注入配置常量、指纹文件或旧 metadata。

只改变条件选择、活动依赖计划完全相同时，goxide.lock 字节保持稳定且可 --locked 重放；optional 激活改变实际依赖计划时按现有锁规则处理。配置解析或源码检查/生成失败不发布新生成物或新锁。

原生 Go 模式同一包只保留当前配置的一套 `_gox.go`，先检查生成 Go 与选中手写 Go 的完整包，再替换受管生成文件；导入依赖须已可用，依赖包需要生成时先执行其自己的生成命令。某个仍存在的 `.gox` 中所有 item 被删光时，也生成保留 package 和适用文件约束的空壳，覆盖该文件此前的声明；不能跳过输出留下旧功能。手写 Go、go.mod/go.sum 与生成文件覆盖保护沿用现有规则。已删除源文件的清理不在本次条件选择范围内。

## 双向互操作

### 命令与兼容性

原生 Go 入口，在手写 Go 文件中固定包配置：

```go
//go:generate goxide gen --features=json,trace .
```

启用配置后执行 go generate、go build、go test；切换配置要修改或重新执行生成命令。Goxide 可调用所选 Go 文件中的声明；Go 直接调用本次生成的 API，例如 CFG-01 的 Encode 和 HasJSON。被 cfg 删除的公开 API 同样不对 Go 消费者存在，手写 Go 的无条件调用应正常报错。

以下是 TOML 用法，项目命令可选择 lib/bin/example。所有 flag 在输入目录之前，真实 Go 需 1.27+：

```sh
goxide run --go /path/to/go --bin cli --features pretty ./app
goxide run --bin cli --no-default-features --offline --locked ./app
goxide build --bin web ./app
goxide build --bin web --target-dir build-output ./app
goxide build --bin web -o ./app-web ./app
goxide test --bin cli --features pretty --offline ./app
goxide transpile --bin cli ./app
goxide transpile --bin cli -o ./app-generated ./app
```

单个 bin 可省 --bin；多个 bin 必须显式选择。每次选择一个根目标及依赖闭包，活动 TOML 依赖沿用其 lib；无清单的原生 Go 依赖保持既有加载方式。`--bin`、`--example` 与 `--lib` 三种选择互斥，run 只接受 bin/example；required_features 继续只检查、不隐式开启 feature。

TOML 模式保留现有 target 输出、目录所有权及覆盖保护，生成诊断保留源位置；原生 Go 原位输出规则见 [gen 指南](../using-goxide-gen.md)。feature 参与配置身份与源码选择，不隐式改变目标；目标操作按 RFC 0028，source map 与参数透传保持各自边界。

普通构建与测试沿用各入口的现有文件选择；原生 `_test.go` 正常参与测试，不提供 test 谓词。Go 消费者使用当前配置的普通 Go API，不需要 feature 求值器。

## 拒绝与边界

以下均为首批验收要求，源级拒绝发生在生成 Go 之前：

| 输入 | 结果与阶段 |
| --- | --- |
| `#[cfg(any())] func Broken() { x := }` | 普通源语法错误，ParseFile 仍拒绝 |
| `#[cfg(any())] func Hidden() { missing!(); absent() }` | 项被删除，不查宏或名称 |
| `if cfg!(any()) { absent() }` | 条件展开为 false，仍拒绝未定义名称；经 const 别名使用同样检查 |
| `if cfg!(any()) { missing!() }` 或 false 分支中类型不匹配 | 仍分别在宏展开或类型检查时拒绝 |
| `cfg!(any(all(), feature("typo")))`，TOML 未声明 typo | 完整谓词校验拒绝，不能因第一项 true 隐藏错误；原生 Go 中未启用的 feature("typo") 为 false，整个 cfg! 仍为 true |
| `#[cfg_attr(any(), derive(NoSuchTrait))] struct S {}` | 载荷未选，不查派生；改成 all() 后按普通派生规则拒绝 |
| `#[cfg_attr(any())] struct S {}` | 缺少属性载荷，配置属性结构错误 |
| `#[cfg_attr(feature("outer"), cfg(feature("unknown")))] struct S {}` | TOML 中 outer 已声明且关闭时保留 S；开启 outer 才诊断未声明的 unknown |
| `#[cfg(test)]`、`cfg!(cgo)`、`cfg!(compiler("gc"))` | 不支持的谓词，不能返回 false |
| 同时保留的同名函数、相冲突的派生与手写 impl | 按普通声明/协议规则拒绝；互斥 cfg 删除一方后可通过 |
| 实际处理到的字段、变体、参数或普通语句上的 cfg/cfg_attr | 首批位置尚未支持，指出该位置；不能静默忽略 |

feature 仍为可累加请求；互斥 feature 约束、target-specific 依赖、workspace resolver、weak dependency feature、开发依赖分组与脚本自定义 cfg 后续单独设计。首批不引入 compile_error! 或额外配置约束语言。

生成与锁发布沿用现有项目互斥和成功提交顺序；配置解析或源码检查/生成失败不发布，最终用户程序运行失败不回滚已解析依赖的锁。条件求值不执行脚本，也不读取任意环境或文件。

## 验收与未定项

下表列出行为验收合同。正例生成 Go 并真实 build/run，项目测试与原生 Go 工作流还执行真实 test；诊断反例按拒绝阶段验证。测试入口见[能力状态表](../mvp-gap-audit.md)。

| 编号 | 输入与检查 | 必须观察到的结果 |
| --- | --- | --- |
| CFG-01 | 上面的互斥声明、普通 if cfg! 与 const 查询，feature 开/关 | 两种配置均构建并返回预期值；cfg 删除关闭声明与调用；cfg! 仅生成正确布尔常量，if 两支保留 |
| CFG-02 | 条件 Default/Debug/serde 派生、嵌套 cfg_attr 产生 cfg、多个属性 | 宿主与派生按各条件出现；未选属性不绑定协议，保留项正常检查依赖和冲突；属性次序不影响选择和错误 |
| CFG-03 | feature/all/any/not、多行、尾逗号、空 all/any、错误参数和未知名 | 三种入口同一结果；TOML 全谓词校验，原生 Go 未启用合法名为 false；未知或未开放谓词明确拒绝 |
| CFG-04 | 互斥同名 item、条件 use/宏定义、关闭项的语法错误与未知名称 | 先裁剪再绑定；不装载已删除源码导入；普通语法错误始终存在，未选子树不做语义检查 |
| CFG-05 | cfg! 直接或经 const 用作 if 条件、复合表达式；false 分支含未知名称、未知宏、错误类型或引用权限 | 与普通布尔常量一致，if 两支都检查；条件恒假不能隐藏诊断，既有初始化、作用域与控制流规则不变 |
| CFG-06 | 宏生成 cfg/cfg_attr/cfg!；cfg 删除项含未知宏及跨包假循环 | 只展开和记录属性选择后的有效内部依赖；必要外层宏仍加载；普通 if 两支均处理，保留部分真实循环拒绝；宏生成语法错误正常报告 |
| CFG-07 | 宏定义模块与调用模块 feature 相反；模板、整段捕获、嵌套 cfg_attr 转发 | 配置按各自来源求值；Span/Origin/展开诊断与 `//line` 保留；局部 true/false 名字不能改变查询值 |
| CFG-08 | 原生 Go 多文件包，通过 go generate 开→关→开；某文件关后为空；同 module 另一个包单独生成 | build/run/test 均真实执行；空壳覆盖旧声明；包配置不泄漏；手写 Go 与 go.mod/go.sum 不变；失败不发布，覆盖保护仍有效 |
| CFG-09 | TOML 默认/关闭默认/显式请求、依赖转发、菱形并集、optional、required_features | 每模块一份集合；默认请求不被另一 false 撤销，根同名 feature 不泄漏；未激活 optional 来源不取得，非 optional 依赖不因源码 cfg 消失 |
| CFG-10 | 同配置重复、请求乱序/重复、配置 A→B→A、cfg-only --locked、optional 改变计划 | 同配置生成字节一致；不同集合进入不同产物身份；依赖计划不变时锁字节稳定；计划变化按锁规则处理 |

改动执行仓库要求的 build、internal/tests test、vet、gofmt 和 docs-only 检查。内部节点与其他谓词按前述接入条件另立验收。

## version 来源与发布边界

通过 Go 版本来源取得的 Goxide 源码，在 feature 接入后使用同一模块请求规则；无 goxide.toml 的固定 Go 发行物不重配 features。下载校验与传输用 go.mod 的一致性仍按[版本来源合同](../design/version-source-plan.md)执行，条件选择只改生成副本，不写下载缓存。

增强源码的传输 go.mod 声明仍须匹配消费的 Go module 路径；不自动重写改名 fork 的源码自导入身份。纯 Go fork 替换继续采用 Go 规则。Go 版本来源经实际生效的显式本地 replace 指向同目录时，与 path 归一为同一来源节点；其他不同来源保持独立，投影到相同 Go module 路径时拒绝。下载模块的 path/host helper/tools 不能越出取得的模块边界。`goxide-planner.invalid/` 为内部保留 module namespace，真实依赖引用会拒绝。

构建顺序仍是增强 module 级 DAG，不实现跨 module 的包级交错排序。main-anchor 只保证原生 module 图回边的政策解析；依赖不能通过它导入尚未生成的根包，即使某个包级图本身无环也不在本阶段支持范围内。

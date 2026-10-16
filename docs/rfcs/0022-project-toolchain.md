# RFC 0022：feature、条件编译与项目构建

## 状态与范围

已实现：feature、`#[cfg(...)]`、`#[cfg_attr(...)]` 与 `cfg!(...)` 接入新管线。条件属性在名字和类型检查前选择有效 item；cfg! 只产生布尔常量，if 两支按普通语言规则检查。首批仅实现 `feature` 谓词，同时覆盖原生 Go 的 `go generate` 和单项目 `goxide.toml`；当前入口和测试见[能力状态表](../mvp-gap-audit.md)。

首批支持完整顶层 item、条件属性和布尔查询，包括宏生成的这些形式。字段、变体、成员等内部节点，以及其他谓词按下文设计后续接入。target/env、统一配置快照和 `goxide env` 的值域及验收已写定，尚未实现；test 另需测试 view。workspace、example/lib 的 CLI 目标选择、构建脚本、跨平台构建不随本轮开放。

前端接入以[新前端合同](../design/frontend-ast.md#84-feature-条件编译合同)为准，使用 AST 和宏来源信息。项目模式复用 `internal/project` 的 feature 图，将每模块结果传入 `internal/compile` 与新前端；原生 Go 模式直接传入当前包的显式 feature 集。旧 `//goxide:cfg` 注释不作为配置入口，不保留文本预处理和旧 metadata 机制。

依赖 schema 与项目发现以 [goxide.toml schema](../design/toml-manifest.md)为准：Go 来源显式写 `go` 加 `version`，Git 可省略 ref 并锁定默认分支，registry 未实现。TOML 作者项目采用[统一项目模型](0025-unified-project.md)，根不保留 go.mod/go.sum；原生 Go 项目继续以自身 go.mod 为依赖权威。下文的多目标、来源与发布合同按各自阶段接入。

## 源级规则

模块根目录显式放置 `goxide.toml`，`package.format = 2` 及显式 `package.module` / `package.go`，未知字段报错。依赖别名与 Go import 路径独立，import 仍写 module path。下例展示清单字段组合，包含后续开放的 example 目标；首批使用 package/dependencies/features/bin 和依赖的 lib：

```toml
[package]
format = 2
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
module = 'example.org/client'
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
- 所有到同一 module/source 的请求取并集；任一边要求默认 feature，就启用其默认 feature。不能以一条边的 false 撤销另一条边的 true。
- 未知本地 feature、未知依赖、feature 循环、模块依赖循环诊断；所有本地 feature 边即使未启用也检查。转发到依赖的 feature 名在依赖被激活读取后检查，未激活 optional 来源不下载。
- 不隐式生成与 optional 依赖同名的 feature。version/path/git 是获取渠道；活动 manifest 边取得 goxide.toml 源码后使用相同 feature/default/optional/lib 规则。没有 goxide.toml 的纯 Go 或固定配置生成 Go 不接受显式 goxide feature 请求。仅 go.mod require/indirect 参与 Go 选版，不隐式开启 goxide defaults 或执行脚本。
- TOML go.require 不因 goxide optional 关闭而被删除；显式 Go 依赖仍由 Go 解析。

每个 module 在一次构建中只有一份最终 feature 集，其包共享该集合。`required_features` 在图求解后检查最终集合，不隐式开启 feature；首批接通现有 bin 与依赖 lib 的该检查，不扩大目标选择入口。删除条件 `use` / import 只影响源码包绑定，不反向删除清单里的非 optional 依赖；避免取得依赖必须使用未激活的 optional 边。

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

## 扩展谓词与统一配置（设计，尚未实现）

本节确定 feature 之外的值域、来源、查询入口和缓存合同，不表示当前 CLI 已开放这些能力。`target_ending` 的正确拼写为 `target_endian`；`target_platform` 表示平台类别，不提供 `target_family` 别名。当前未知/未支持谓词仍诊断，不返回 false。新增谓词复用现有 cfg/cfg_attr/cfg! 求值器、完整谓词校验和宏来源规则，不引入预处理器或用户可注册的谓词。

### 谓词目录与封闭值域

带值谓词只接受字符串字面量，布尔谓词使用裸名；所有谓词的求值结果都是布尔值。下面的“值”指可接受的参数或上下文字段，不是允许改写的配置项。

| 谓词 | 合法参数或字段值 | 来源与含义 |
| --- | --- | --- |
| `feature("name")` | 项目自定义名称；TOML 模式限所属模块 `[features]`，原生 Go 名称开放 | 已实现；使用每模块/包最终 enabled 集 |
| `target_os("value")` | `aix`、`android`、`darwin`、`dragonfly`、`freebsd`、`illumos`、`ios`、`js`、`linux`、`netbsd`、`openbsd`、`plan9`、`solaris`、`wasip1`、`windows` | 所选 Go 的有效 GOOS，精确相等 |
| `target_arch("value")` | `386`、`amd64`、`arm`、`arm64`、`loong64`、`mips`、`mips64`、`mips64le`、`mipsle`、`ppc64`、`ppc64le`、`riscv64`、`s390x`、`wasm` | 所选 Go 的有效 GOARCH，精确相等 |
| `target_platform("value")` | `unix`、`windows`、`wasm`、`plan9` | Goxide 定义的互斥平台类别，按下表派生 |
| `target_endian("value")` | `little`、`big` | 按目标 arch 派生的字节序 |
| `target_pointer_width("value")` | `32`、`64`（字符串） | Go 目标的指针位数，不是宿主位数或 wasm 地址指令宽度 |
| `compiler("value")` | `gc` | 所选且通过版本一致性检查的 Go 编译器；本设计不支持 gccgo |
| `cgo` | 上下文字段为 `true` / `false`；无参数 | 有效 CGO_ENABLED 为 1 / 0，表示启用状态，不表示已使用 C 或已验证 C 工具可用 |
| `race` | 上下文字段为 `true` / `false`；无参数 | 本次已解析构建配置启用 race；build/run/test 实际传入 `-race`，gen/expand 使用预期消费配置，不按能力或同名 tag 推断 |
| `env("NAME")`、`env("NAME", "VALUE")` | 名称和值开放；键必须显式登记，规则见后文 | 进程环境的存在性或精确值比较，不是 Go 工具链的有效配置查询 |
| `test` | 上下文字段为 `true` / `false`；无参数 | 测试单元角色；随测试生成流程接入，见后文，不能仅据命令名给所有包设 true |

`all`、`any`、`not` 是组合算子，不增加配置输入。`debug_assertions` 保留为后续布尔谓词，须先确定断言语义及显式启用入口；当前没有可采集的有效值，不从 run、race 或优化参数推导。`target_env`、`target_abi`、`target_vendor`、`target_feature` 暂不引入：Go 没有本设计所需的统一值域，GOARM/GOAMD64 等仍是工具链输入，不伪装成通用 ABI 或 CPU 特征谓词。

对上表的封闭参数，已知但不等于当前目标的值为 false，未知值（包括拼写错误）报错。例如 linux 上 `target_os("windows")` 为 false，而 `target_os("Linux")` 报错。不接受 `test()`、`cgo("true")` 或数字参数 `target_pointer_width(64)`。feature 与 env 的开放名称不自动扩充其他谓词的值域。

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

这些值由目标唯一决定，不允许分别用 CLI 或环境覆盖。Go wasm 的指针宽度确为 64，见 [Go 架构布局定义](https://go.dev/src/cmd/internal/sys/arch.go)。前端 `go/types` 的目标尺寸、Go importer、文件筛选和最终 Go 命令必须一致；接入目标谓词仍保留现有非宿主编译拒绝，不能仅凭有这张表宣称交叉编译已实现。跨目标 build、run/test 的 runner 后续另行验收。

cgo 能力与启用值分开保存：基线中 js/wasm、wasip1/wasm、全部 plan9 组合及 openbsd/ppc64 不支持 cgo，其余组合的 dist 能力标记为 true；最终能否编译 C 仍取决于 C 工具链。race 接入时为 gen/build/run/test/expand 统一提供布尔 `--race`（等于 `--race=true`）与 `--race=false`。有效值优先级为：显式 CLI 值 > test 入口默认 true > GOFLAGS 中的 -race 值 > false；保留当前 test 自动加 -race 的默认行为，但显式 false 可关闭。build/run/test 必须将该有效值落实为 Go 命令选项，不能在快照冻结后再追加相反值；gen/expand 没有最终构建命令，使用该值作为预期消费配置，生成物由调用者以相同 race 选项构建。仅对外层 go generate 传 -race 不会自动成为子命令的参数；须在生成指令显式写 --race，或共享 GOFLAGS。race 基线支持 linux/{amd64,arm64,loong64,ppc64le,riscv64,s390x}、darwin/{amd64,arm64}、freebsd/amd64、netbsd/amd64、windows/amd64；Go 1.27.1 的 darwin race 不要求 cgo，其余支持组合要求启用 cgo。这是工具链能力校验，不能用支持矩阵代替本次 `race` 真值。依据见 [Go race 支持判断](https://go.dev/src/internal/platform/supported.go)与 [Go 插桩初始化](https://go.dev/src/cmd/go/internal/work/init.go)；升级所选 Go 时同步核验，不照搬旧版本假设。

### env 的登记、采集与作用域

环境变量通过显式键集合进入配置，编译器不扫描源码来推测集合，也不把整个进程环境加入缓存。设计中的 TOML 字段为：

```toml
[cfg]
env = ["APP_EDITION", "WITH_TRACE"]
```

原生 Go 包或独立单文件入口使用可重复的 `--cfg-env NAME`；它只登记一个键，不设置值，不支持 `NAME=VALUE`、逗号列表、通配符、dotenv 文件或 shell 展开。TOML 模式由每个模块自己的 `[cfg].env` 登记，CLI 不替依赖或根模块补声明，避免同一源码对不同调用者暴露不同输入目录。所有模式缺省为空集合；构建脚本的 `[build].env` 是独立用途，不能隐式成为 cfg 的声明。

键限非空 ASCII `[A-Za-z_][A-Za-z0-9_]*`，名称集合去重排序。执行编译器的 Unix 宿主按大小写区分；Windows 宿主使用 ASCII 大写规范键进行登记、查询和缓存，因其环境变量名不区分大小写。同一次登记中大小写别名合并；若输入环境出现规范键相同却值不同的重复项，拒绝配置而非依赖遍历顺序。值不 trim、不解析布尔、不替换变量，按原始字节精确比较；非 UTF-8 环境值仍可做存在性查询，CLI JSON 用 base64 无损表示。

| 快照中 NAME 的状态 | `env("NAME")` | `env("NAME", "")` | `env("NAME", "x")` |
| --- | --- | --- | --- |
| 未登记 | 诊断 | 诊断 | 诊断 |
| 已登记但 unset | false | false | false |
| 已设置为空串 | true | true | false |
| 已设置为 x | true | false | true |

非空字符串 `"0"`、`"false"` 的存在性仍为 true。配置值在启动时捕获的进程环境中用 LookupEnv 语义读取；不读取程序运行时的环境，不把 `go env -w` 提供的默认值冒充进程变量。比如进程没有 GOOS 而 Go 默认 darwin 时，登记后的 `env("GOOS")` 为 false，`target_os("darwin")` 为 true。查询工具链事实应使用对应 target/cgo 谓词。

活动模块的声明集合在 feature 图确定后集中收集，未激活 optional 模块不读取、不登记；同一规范键的进程值只采集一次，各模块仍只可查询自己登记的键。模板写出的 env 查询使用宏定义模块的登记目录，完整捕获的配置构造使用调用模块目录，与 feature 的来源规则相同。即使将一个字符串参数代入模板，也不能改变所属模块。普通 if 两支中的 env 查询均验证；外层 cfg 删除的子树和未选 cfg_attr 载荷继续遵守既有不求值规则，但所有已登记键仍进入本次快照和身份。

### 一个配置入口与不可变快照

在任何源文件筛选、cfg 处理、展开、类型尺寸选择或产物身份计算之前，由同一个配置解析入口生成具体的不可变数据。以 `ResolveConfig(request, capturedEnvironment) → ConfigSnapshot` 表示职责，不要求增加接口/注册表或并行管线；将当前项目环境采集和 CLI 各入口的零散默认值归到这里。`ConfigSnapshot` 最少含：

| 部分 | 必须收集的内容 |
| --- | --- |
| 工具链 | 编译器内容摘要、所选 Go 可执行文件与版本/编译器身份、host OS/arch、受支持目标及 cgo/race 能力 |
| 目标 | 有效 GOOS/GOARCH、派生 platform/endian/pointer_width、已验证的前端类型尺寸 |
| 构建选项 | 有效 CGO_ENABLED、race、规范化 build tags、GOEXPERIMENT、架构调优项、会影响编译的 Go/C 工具链选项；编译单元的测试角色 |
| 模块配置 | 按稳定 module/source 身份排序的 feature declared/enabled 集、env 登记集合；原生模式使用明确的包或单文件作用域 |
| env 值 | 每个登记规范键的 present 位与原始值；present=false 不得与空串合并 |
| 选择与身份 | 入口种类、包/目标选择、已解析依赖计划与锁定来源、配置规范版本和由以上实际值计算的摘要 |

工具链解析是这个入口的第一阶段，依赖规划使用该阶段的已解析值；活动图稳定后再补齐模块配置并冻结完整快照，不为依赖规划和前端各读一次环境。

处理顺序是：捕获进程环境和 CLI 请求 → 解析所选 Go 的有效环境、核验工具链 → 解析 feature/活动依赖图与模块登记 → 形成并冻结快照 → 文件筛选、Parse/Configure/Expand/Check/Lower → Go 构建与发布。依赖发现不读取源级 cfg 来改变清单图。后续脚本如需影响配置，必须另行定义冻结前的受授权阶段；不能在当前设计里补写快照。

目标值取**所选 Go**在捕获环境下的 `go env -json`，服从该工具自己的环境配置优先级；宿主 runtime 只用作当前能力限制检查。`go/build.Default` 不读取全部 Go 环境文件，不能直接当作这个快照，见 [Go 环境解析](https://go.dev/src/cmd/go/internal/cfg/cfg.go)。MatchFile 使用从快照显式构造的 Context，所有 importer/go list/最终 build、run、test 子进程也显式接收同一份有效目标和选项。不能在宏展开时调用 os.Getenv、在另一个 CLI 路径重读 go env，或在快照冻结后因为 GOFLAGS 的隐藏参数改变实际配置。

GOFLAGS 中影响 cfg、文件筛选或编译的选项必须解析并归一化，再与显式 CLI 参数按 Go 的覆盖规则得到唯一实际值；无法可靠解释的选项拒绝，不透传后猜测。显式 tags 不允许伪造保留的 GOOS、GOARCH、unix、gc/gccgo、cgo、race 等内建标签，避免 MatchFile 与谓词矛盾。配置不完整或所选后端不提供目标上下文时明确报错；浏览器/WASM 转译须由调用方传入明确配置，不能把浏览器宿主当作目标或把未知值默认成 false。

### 查询接口：goxide env

设计中的公共入口为：

```sh
goxide env --json --for build --bin app --features json ./app
goxide env --json --for gen --cfg-env APP_EDITION ./native-package
goxide env --list --json --go /path/to/go
```

`goxide env [INPUT]` 省略 INPUT 时采用 `--for` 对应命令的缺省发现规则，`--for` 缺省 build，可选值完整为 `gen`、`build`、`run`、`test`、`expand`；transpile 归一化为 gen，不另设别名值。输入发现、目标选择、features/no-default-features、tags、race、cfg-env、Go 选择及 offline/locked 规则与对应执行入口相同；该入口不接受的配置 flag 在 env 中同样拒绝。gen 在最近 go.mod 边界停止并可缺省当前目录；build/run/test 省略输入时向上找 TOML，原生 Go 目录需显式传 `.`；expand 仍须选具体单文件。多个 bin 时同样要求 --bin，不能随便选一个。命令打印实际解析值，不只回显 CLI 请求，也不独立实现第二套默认规则。

输出默认是按字段路径排序的文本，`--json` 是可供工具消费的对象，包含 schema（整数 1）、action、input_mode、toolchain、host、target、build、modules、env、config_digest；test 接入后还输出每个编译单元的 role/test/digest，普通依赖与被测变体分开。features 与键集合排序；布尔字段使用 JSON boolean。env 项显式包含 key/present：present=false 时 value=null；已设置 UTF-8 值以字符串表示（含空串），否则 value=null 并给出 value_base64。只输出登记键，绝不倾倒未登记的进程变量；这是显式查询命令，可显示这些登记值，但普通日志和产物标记不得自动附带它们。主机路径等展示字段与参与语义摘要的字段明确区分，不对展示 JSON 直接做 hash。

`--list` 输出全部谓词语法、上述封闭参数值、各谓词的 implemented/planned 状态及所选工具链实际支持的目标组合/能力。该模式不接受 INPUT、--for 或项目配置 flag，不解析项目；可保留 --go 与 --json。工具链支持列表不等于 Goxide 已支持交叉编译，输出同时列明当前 host-only 限制。未知值错误应附允许值或指向这个入口。

普通 env 查询允许按 offline/locked 策略解析及获取依赖到缓存，以取得完整活动模块声明；不展开用户宏、不运行脚本、不生成代码/二进制，也不写新锁或发布 bundle。失败返回非零，不输出伪完整快照。重复查询同输入得到相同规范化值和摘要；实际构建重新解析时若环境变化，摘要随之变化，env 不是替后续命令锁住系统环境的服务。

### 生成物与测试单元

原生 Go 的生成结果固定本次配置。feature/env 改变须重新生成整个包；Go build/test 不会自动运行生成器。涉及 target/cgo/race 谓词的包，所有本次生成文件（含空壳）共同带快照对应的 Go build constraints，与原有约束合取；还需保留 Go 文件名规则。目标约束必须表达精确 OS，例如 linux 还排除 android、darwin 排除 ios、solaris 排除 illumos；cgo/race 分别使用正标签或负标签。不能只给含查询的一个文件加约束，导致包内不同配置混用。包内全部生成文件仅保留一个当前配置，不自动生成矩阵或同时留下多个版本。

env 与 feature 没有可供 Go build 自动核验的通用值标签；不伪造这类标签。切换这些值的生成与构建由同一命令/CI 配置负责。TOML 生成 view 和执行它的 Go 子进程直接共享快照。普通源码注释不嵌入 env 原值，不生成用户可见配置常量、兼容 metadata 或额外运行时求值器。

`test` 的定义是**被测包变体及它的内部/外部测试单元**为 true，作为普通依赖使用的包为 false；同一包同时被依赖和测试时须有不同单元身份。普通 build/run/gen/expand 的单元为 false。测试构建中的普通源与测试源使用该变体上下文，不能靠 `_test` 文件后缀决定普通源中的 test 值，也没有隐式 Go test 标签。

test 查询取实际处理该表达式的编译单元角色：跨包宏输出使用展开目的单元的角色，feature/env 仍按配置构造的来源模块取值；宏定义项自身的条件选择则发生在它所在的定义单元。多包测试按被测包分别构造 view 和执行 Go test，被测包之外的普通依赖保持 test=false。外部测试或辅助依赖导入被测包时统一连接当前 test=true 变体；Go 为重接依赖而重编译的辅助包仍为 false，不能直接用 Go 的 ForTest 字段判定谓词。不能把每个包的被测版本同时铺到一个目录树再 go test ./...，否则同一路径可能错误地提供给另一个测试根作普通依赖。

test 随独立测试生成 view 一并接入：普通包变体和被测变体分别选择、解析、检查并生成，测试 view 中用被测变体替换该包的普通生成声明；真正的测试声明按 Go 规则进入 `_test.go`，不能把两份完整包声明叠加。原生 Go 原位 gen 只固定普通配置，之后直接 go test 不会重新求值源级 test；需要这种重配时走 goxide test 的隔离 view，手写 Go 与源码目录保持只读。在测试 view、多包变体引用和 `_test.gox` 端到端流程验收前，test 继续明确拒绝，不能仅把一个布尔字段接进当前生成器。debug_assertions 同样在其自身语义齐备前拒绝。

### 配置身份、缓存与锁

快照是 cfg、cfg_attr、cfg!、文件筛选及构建缓存的共同输入，不得各自维护值表。配置身份采用带规范版本的确定性编码与 SHA-256：排序 map/集合，固定字段与类型，长度编码或明确转义以免连接歧义；env 原值按无损字节或 base64 编码，不能直接交给会替换无效 UTF-8 的 JSON string 编码；使用实际解析值而非命令拼写。相同 feature 请求的乱序/重复、显式值与等效默认值不改变身份；不同模块归属、env unset/empty、不同目标/模式必须不同。

所有登记 env 的键、present 与完整值都纳入身份，即使本次没有源码读取它、两个不同值恰好使当前谓词结果相同，也不能只 hash 查询结果。未登记环境变量不因 env 谓词进入身份；其中若另有工具链含义（例如 GOOS、CGO_ENABLED），仍通过对应有效字段进入身份。模块声明目录也属于输入，不能因为 enabled 集一样而复用会隐藏声明诊断的旧结果。工具链编译器身份、目标与架构调优、C 编译选项、tags/race/test、来源计划保持在同一摘要中。

项目 `UnitSpec.Config` 消费这份摘要并保留现有项目/入口身份；target 路径分区取快照目标，host 取实际执行工具链的宿主，二者不混用。当前稳定 bundle 的配置目录不是源码缓存：源码修改仍重新转译，不能因摘要相同跳过 Parse/Check。若以后增加转译结果缓存，键必须在配置摘要上增加全部源码/宏定义/原生 Go 声明、依赖产物和编译器输入的内容身份；最终 Go 对象继续由 Go 自己的缓存管理，不假定 Go 能看见隐藏 env 输入。

冻结后的 env 原值只在配置内存和用户显式 env 查询输出中存在；持久配置标记、锁、生成源码与普通日志只放摘要和必要的非敏感标识。摘要不是加密，不作为保存秘密原文的替代品。依赖身份不变的纯 target/env/cgo/race/feature 选择不改 goxide.lock 字节，也可 locked 重放；cfg 声明和值不作为依赖选版或锁校验的输入。optional feature 改变实际依赖计划时才按现有锁合同处理。配置解析或源码检查/生成失败不发布新 bundle 或锁；后续 Go 编译失败按既有布局合同保留已成功生成的 bundle 与已保存的锁，但不发布新二进制。用户程序运行失败也不回滚此前成功的生成与依赖锁。

### 扩展验收条件

以下是后续实现必须满足的检查，本次只提交设计；现有 feature 的通过记录不能替代它们。

| 编号 | 行为 | 验收 |
| --- | --- | --- |
| CFG-X01 | 封闭目录、47 组基线、派生字段 | 表驱动覆盖全部映射；未知值/坏组合/未知新架构拒绝；android/ios/illumos 不作 OS 别名；wasm 指针为 64；非宿主仍按能力限制拒绝 |
| CFG-X02 | env unset/empty/value、登记与作用域 | 三种谓词入口同值；未登记与错误参数诊断；宏定义点/完整捕获/参数代入、模块同名键隔离；Windows 大小写与冲突、非 UTF-8 输出可重放 |
| CFG-X03 | 唯一快照 | Go 环境文件与进程默认差异、CLI/GOFLAGS 优先级、cgo/race 能力限制、文件筛选/type sizes/importer/最终 Go 命令同值；展开期间改变进程环境不改变本次结果 |
| CFG-X04 | env CLI | 文本/JSON/--list 与实际构建快照一致；所有已声明键与模块均列出；未知/不支持入口拒绝；失败无部分 JSON、不发布锁或生成物 |
| CFG-X05 | 配置与缓存 | 乱序/重复等价；每种实际输入分别改变身份；env unset→empty→A→B→A、相同布尔不同值、未使用登记键及两组不同无效 UTF-8 值均检查；无关环境不扰动；源码修改仍重生成；cfg-only locked 锁不变 |
| CFG-X06 | 原生 Go 整包生成 | target/cgo/race 正反约束与原约束正确合取；配置切换清除旧声明；真实 go generate/build/run/test、Go 双向调用、两次字节一致；配置/源码失败保留旧产物，Go 构建失败不发布新二进制 |
| CFG-X07 | test（单独接入） | 普通依赖/被测变体/外部测试各自上下文，普通源 test 分支与测试源一致；两份变体不重名；隔离 view 真实 test，源码和原位普通产物不改 |


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

依赖来源采用当前 schema；Go 来源写 go，Git/path 来源写 module：

```toml
[dependencies.text]
go = "golang.org/x/text"
version = "v0.28.0"

[dependencies.remote]
module = "example.org/lib"
git = "https://example.org/lib.git"
rev = "release-1"
subdir = "library"

[dependencies.local]
module = "example.org/local"
path = "../local"
features = ["fast"]
```

上面仅展示格式，不是测试访问的服务。版本解析、MVS、module 缓存、校验和全部调用真实 Go 工具。TOML 内保留原生 require 与本地 replace 语义。增强依赖与 go.require 的版本请求共同交 Go MVS 选择，不因两个最低版本不同就拒绝；显式本地/Git 来源不能覆盖根 TOML 已有的不一致 go.replace。根 replace/exclude 为全局权威，不提升依赖自己的策略。源 go.mod/go.sum 不改写，修改限于生成副本。

path/git 入口（Git 含 subdir 后）可以是纯 TOML 作者模块，或者自带普通 go.mod 的原生 Go 模块；不借用父目录配置。TOML 模块只在受管目录合成 Go 元数据，module 必须匹配逻辑身份。

本地 path 相对声明它的 manifest，而非 cwd；不递归发现相邻模块。Git 的 rev/tag/branch 选择器可省略，此时选默认分支；首次解析后在 goxide.lock 锁定精确 commit，之后移动 ref 不改变锁。Git subdir 必须在 checkout 内，实际 module path 必须一致。同一 module path 的来源冲突按 [schema](../design/toml-manifest.md#来源与包身份)处理。

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

以下是 TOML 用法，目标选择限当前 bin 入口。所有 flag 在输入目录之前，真实 Go 需 1.27+：

```sh
goxide run --go /path/to/go --bin cli --features pretty ./app
goxide run --bin cli --no-default-features --offline --locked ./app
goxide build --bin web ./app
goxide build --bin web --target-dir build-output ./app
goxide build --bin web -o /tmp/app-web ./app
goxide test --bin cli --features pretty --offline ./app
goxide transpile --bin cli ./app
goxide transpile --bin cli -o /tmp/app-generated ./app
```

单个 bin 可省 --bin；多个 bin 必须显式选择。每次选择一个根目标及依赖闭包，活动 TOML 依赖沿用其 lib；无清单的原生 Go 依赖保持既有加载方式。后续开放 example/lib CLI 选择时，三种选择互斥、run 只接受可执行目标；required_features 继续只检查、不隐式开启 feature。

TOML 模式保留现有 target 输出、目录所有权及覆盖保护，生成诊断保留源位置；原生 Go 原位输出规则见 [gen 指南](../using-goxide-gen.md)。本次只把 feature 接入配置身份与源码选择，不扩大 source map、参数透传或目标选择接口。

普通构建与测试沿用各入口的现有文件选择；原生 `_test.go` 正常参与测试，test 谓词和 `.gox` 测试生成后续一起设计。Go 消费者使用当前配置的普通 Go API，不需要 feature 求值器。

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
| `cfg!(target_os("linux"))` 或 `#[cfg(test)]` | 首批谓词尚未支持，不能返回 false |
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
| CFG-03 | feature/all/any/not、多行、尾逗号、空 all/any、错误参数和未知名 | 三种入口同一结果；TOML 全谓词校验，原生 Go 未启用合法名为 false；target/test 明确尚不支持 |
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

增强源码的 go.mod 声明仍须匹配消费逻辑 module；不自动重写改名 fork 的源码自导入身份。纯 Go fork 替换继续采用 Go 规则。version/path 同 module 只有根不带版本限定的本地 replace 明确指向同目录时统一；否则来源冲突拒绝。下载模块的 path/host helper/tools 不能越出取得的模块边界。`goxide-planner.invalid/` 为内部保留 module namespace，真实依赖引用会拒绝。

构建顺序仍是增强 module 级 DAG，不实现跨 module 的包级交错排序。main-anchor 只保证原生 module 图回边的政策解析；依赖不能通过它导入尚未生成的根包，即使某个包级图本身无环也不在本阶段支持范围内。

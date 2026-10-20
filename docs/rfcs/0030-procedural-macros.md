# RFC 0030 草案：过程宏协议、卫生、登记与执行宿主

## 状态与范围

状态：**Draft，待评审，未实现**。用户已确认开始过程宏设计：从 `main` 建立 `feat/procedural-macros`，先确定本篇合同，再按实际消费者提取共享代码。声明式 `derive()` / `attr(...)` 的 RFC 0029 与原型继续保留在 `draft-rfc/declarative-derive-attributes`；本篇不依赖合入该分支。

本篇提出一个可独立验收的过程宏基础：公共 TokenStream、卫生与诊断来源、三个调用入口、derive helper 登记、源码依赖与重导出，以及 CLI/浏览器共用的受限 WASM 执行协议。用户已确认公共库采用独立 `github.com/goxide-lang/syntax` 仓库与 Go module，首批提供手写 Go 的 `token` 与 `proc_macro` 两包；本篇其余新增语法、SDK API、清单字段与宿主选择仍是具体提案，接受本 RFC 后才作为实施依据。

本篇不迁移 Default、serde 或其他内建宏，不补泛型 impl，不提供类型反射或已解析 TypeShape 查询，不开放编译脚本，也不实现完整 syn/quote。普通 syn/quote 库可以建立在本协议上；它们解析语法、构造语法，不取得类型检查或权限证明。库派生迁移仍需另行验收普通 trait/impl、生成代码及跨仓库发布。

关联：[声明宏合同](0011-macros.md)、[内建派生](0013-derive.md)、[crate 与依赖根](0028-crate-roots.md)、[语法工具设计](../design/syntax-tooling.md)、[能力状态表](../mvp-gap-audit.md)。Rust 的三个过程宏入口提供对照；Goxide 保持既定的定义点卫生，不采用 Rust 过程宏的默认非卫生行为。[Rust Reference](https://doc.rust-lang.org/reference/procedural-macros.html)

## 源级规则

### 1. TokenStream 与 SDK

TokenStream 是有序语法数据，不是源文本、编译器 AST 或 Go AST。已确认的公共库组织为一个独立仓库与 Go module：`github.com/goxide-lang/syntax`。首批只实现以下两个普通 Go 包，均手写 Go，不依赖 Goxide 生成步骤：

| 包 | 职责与依赖 |
| --- | --- |
| `github.com/goxide-lang/syntax/token` | 唯一公共 `token.TokenStream` 类型、token 数据、解析和构造原语；只依赖 Go 标准库 |
| `github.com/goxide-lang/syntax/proc_macro` | 宏作者 SDK，包括 Context、Output、诊断和协议编解码；只依赖同 module 的 token 包与 Go 标准库 |

普通离线工具可直接使用 token 包。SDK 使用同一个 `token.TokenStream`，不另设镜像类型；编译器内部 token/AST 与公共协议之间的转换桥、宏启动器和执行宿主留在编译器 internal 中。syntax 不反向依赖编译器，编译器的首批接入也不依赖 syn/quote。完整 syn/quote 后续作为同 module 的普通工具包分别实现，不列入首批基础。

开发项目在过程宏清单中显式依赖相邻 syntax checkout：

```toml
[dependencies.syntax]
path = "../syntax"
```

对应 `use syntax::token` 和 `use syntax::proc_macro as pm`，下文沿用这两个别名。发行项目将该项换为现有 `go = "github.com/goxide-lang/syntax"` 与该 module 实际公开的 `version`，不隐式注入依赖。syntax 与编译器独立发布，不要求仓库版本号相同。正常 MVS/replace 完成后，构建器核对实际 SDK 协议内容与编译器构建时随附的协议内容是否一致；比较范围是 SDK 协议、token 词表及编解码合同，不是无关文档或整个仓库版本号。不同内容在构建宏前诊断，不静默覆盖选版结果，也不只凭 import path 或 SDK 自报标志认可。本地修改协议后须重建编译器；缓存产物绑定这份内容身份，不运行旧协议兼容分支。

SDK 内部表示不作为序列化协议直接导出，尤其不公开现有 `syntaxToken` 的 AST、文件指针或语义旁表。公共节点合同为：

| 节点 | 数据与要求 |
| --- | --- |
| Ident | 未转义名称、独立 `raw` 标记、绑定上下文、诊断位置；`r#type` 不等于关键字 `type` |
| Keyword | Goxide 的完整关键字拼写；上下文词仍是 Ident |
| Literal | 整数、浮点、虚数、字符或字符串类别及原始合法拼写；负数是 `-` 加 literal |
| Punct | 一个完整 Goxide 词法单元，如 `::`、`=>`、`>>=`；不采用 Rust 单字符标点的拼接协议 |
| Semicolon | 显式或隐式分号；保留 `implicit`，因为解析器对二者存在实际区别 |
| Group | `()`、`[]`、`{}`，各自的边界位置及子流；根流没有外层分隔符 |
| Fragment | 宿主签发的片段引用、语法类别及可观察的语法视图；实际 AST 与可信来源留在宿主 |

输出不得含 EOF、非法 token、未知节点种类、失配分组或类别与拼写冲突的 token。`r#` 不是名称正文的一部分。文本解析走同一 Goxide lexer，包括自动分号；builder 生成的分号默认显式。宿主接收结构化输出时直接保留分号种类，不能打印成文本后再推断换行。当前 parser 尚无公共 TokenStream 入口，内部 token 解析接入是实施工作之一。

普通注释及空白不属于首版公共 TokenStream，文本显示不承诺格式或注释往返。原类型保留的 derive 不删除其原注释；attribute 重写项的注释转交按保留原项引用处理，重新构造整项不自动继承注释。编译器指令不会仅因以注释表示而成为宏输出能力；首版协议不承载新的 Go 编译指令。文档属性的独立 API 不在本篇加入。

声明宏转交的捕获不能全部摊平成文本。Fragment 可以原样复制、插入或由 syn 作为相应语法节点观察；宿主验证其句柄及类别。修改片段内部必须显式构造新语法，新语法没有原片段的“已解析类别”证明。原样转交的 `expr` / `tt` 不能冒充 `type`，`item` 不能进入表达式槽；现有 builtin `new` / `make` 的类别检查在名字解析后继续执行。

`token::ParseText(text, context)` 创建新语法和生成来源；`Display` 仅供展示，二者不是保留卫生的往返协议。普通 syn AST 不能通过一个字段把新数据声明为“已检查类型”“已授权引用”或“已验证 impl”。

### 2. 卫生、位置与可信来源

宿主分别维护以下信息：

| 信息 | 宏可做的操作 | 不产生的效果 |
| --- | --- | --- |
| 诊断 Span | 使用输入位置、调用位置或本宏定义位置；为输出指定诊断位置 | 不改变绑定、配置归属、私有访问或权限 |
| BindingContext | 默认 DefSite、显式 CallSite、保留输入已有上下文 | 不授予可见性豁免，不等于 unsafe 授权 |
| Provenance | 原样引用输入 token/fragment；新建节点标为本次生成 | 不能上传文件路径、整数或 AST 冒充可信源码 |
| Fragment 身份 | 保留、复制及观察原片段 | 不能改变原片段类别或语义检查状态 |

公共 Span 与当前内部 `Origin` 不是同一个值。当前名字恢复还使用 Origin 的文件与范围判断捕获内部作用域，配置及依赖根也使用词法来源。因此 `WithSpan` 只能修改诊断位置，实现必须把显示位置与可信来源分开，不能覆盖现有 Origin 后继续信任它。

新建自由名字默认使用当前宏定义包的 DefSite。CallSite 指本次宏调用的词法位置，嵌套时不固定为最外层用户文件。输入插值保留自身绑定和来源；每次复制产生独立语法实例。新建局部绑定每次展开取得新身份；生成包级声明归输出所在包，并保留生成代码内部引用所需的绑定关系。公开声明重名仍报普通冲突。

新建 DefSite 路径中的 `crate::` 指宏定义 crate；输入路径及显式 CallSite 路径保持各自词法 crate，重导出不改变这些身份。定义点的 import alias 可以解析它已声明依赖中的公开目标，但不能因此跨包访问私有 helper。过程宏执行依赖也不会自动成为目标程序的运行时依赖；需要的目标库按第 5 节取得。

句柄是本次请求表中的引用，不是编译器内部 ID。宿主只接受该表中允许的引用和操作；别的调用、别的构建或未经登记的上下文不能使用。进程外数据没有可信布尔字段；诊断位置和上下文在输出验证后才映射回宿主对象。

执行隔离保护编译宿主，过程宏仍是任意源码生成器，不能承诺阻止宏读取文字后恶意重建代码。宿主不接受它携带的 readonly、类型、trait 或 unsafe 证明，输出重新通过普通检查。当前新管线不支持的 unsafe 形式仍按原规则拒绝；未来开放时另行完善授权来源合同。生成 `declare` 按普通声明及真实 Go 验签处理，不额外授信；本提案接受后，将取代旧宏专题中“派生产物一律不得生成外部 declare”的预留限制。

### 3. 登记、导入与公开名字

独立库通过新增清单字段声明为过程宏库：

```toml
[go_mod]
module = "example.org/answer-macros"
go = "1.27"

[lib]
name = "answer_macros"
path = "src"
proc_macro = true
```

`proc_macro` 仅适用于 lib，默认 false。它改变此库的编译用途，不改变同项目独立 bin/example 的 crate 身份。可先构建过程宏 lib，再由独立 example 或外部验收项目调用；过程宏 crate 构建过程中不能调用自身登记的宏。普通 helper 函数调用不受此限制。

显式依赖中的实际宏调用触发受限执行，不给每次展开增加人工信任确认。被 cfg 删除的调用不执行；普通导入语法工具包不登记或执行宏。该能力不复活构建脚本入口，不继承脚本的宿主权限。

只登记根包中经 `cfg` 选择后存在的显式、非泛型 `pub func`。函数必须恰有一个登记属性，不能有 receiver，不能通过宏产物再登记入口。签名使用 token 与 SDK 的 canonical 类型，别名可经普通类型同一性验证：

```goxide
use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro]
pub func answer(ctx pm::Context, input token::TokenStream) pm::Output { ... }

#[proc_macro_attribute]
pub func trace(ctx pm::Context, args token::TokenStream, item token::TokenStream) pm::Output { ... }

#[proc_macro_derive(Describe, attributes(describe))]
pub func deriveDescribe(ctx pm::Context, item token::TokenStream) pm::Output { ... }
```

`Output` 含输出 TokenStream 与有序诊断；任一 error 使本次展开失败。登记函数以外的辅助实现是普通编译期代码。`Context` 提供 DefSite、CallSite、位置、冻结 target/features 和 RuntimeRoot；首版没有 env 查询，也没有文件、网络、进程、类型查询或动态加载入口。SDK 可提供 `Success(tokens)`、`Failure(message, span)` 等普通构造函数，不用 panic 作为常规诊断通道。

函数式与 attribute 宏使用函数名登记；derive 使用属性中显式名称，函数实现名不构成目标 API。编译宏库时函数仍是普通函数符号；对消费者的投影只导出登记宏，不同时插入同名函数符号。其他 `pub` helper 也不是目标可导入成员。入口名称、类别、定义包、helper 列表和函数签名由编译器从源码收集；WASM 程序不能自行增加登记项。

保持现有单一静态名字空间：同名 trait 与宏冲突，首版不做名字空间重构，也不提供同名三种入口重载。普通运行库可显式公开一个宏子入口：

```goxide
pub trait Describe { func describe(self Self) string; }
pub use description_macros as macros;
// 使用方：#[derive(description::macros::Describe)]
```

也可将单个宏重导出为 `DeriveDescribe`。别名和重导出保持 canonical 宏身份、定义点及 helper 所有者；不生成运行时转发包装。内建宏的保留名与现有冲突规则保持，过程宏不静默覆盖内建 Default 或 serde 派生。

### 4. 调用、helper 与展开顺序

函数式入口沿用现有表达式、类型、语句和 item 位置，不新增 pattern 宏。表达式/类型槽要求一项相应语法；语句列表允许零到多条，单语句槽必须一条。过程宏的 item 列表允许零到多项，单声明槽必须一项。原有声明式函数宏的单项行为不因提取基础代码而扩大。

attribute 接收去掉自身属性后的完整声明和其余有效属性，整体替换目标。derive 接收单个具名 TypeDecl 或 EnumDecl，输出追加到原声明之后；分组 `type (...)` 不作为一个目标。文件列表、trait/impl 成员列表和既有单声明槽各自执行类别、数量、可见性及 body 检查。字段、变体、任意语句和 inner attribute 上不开放可执行 attribute 宏。

按下面顺序处理每个完整项：

1. 依照原合同选择顶层 `cfg` / `cfg_attr`；删除项不执行宏。嵌套位置不借宏开放配置属性。
2. 选择每一个可执行 attribute 前，先从当前项已可解析的 derive 登记中取得 helper 名称，检查同名歧义；此时不执行 derive，也不因尚未解析的 derive 提前拒绝可被 attribute 删除的目标。从前到后执行 attribute，替换后重新分类、递归处理；声明体内的宏尚未展开。
3. 解析最终项的全部 derive 请求与 helper 登记，验证目标类别；尚未解析的请求此时诊断。
4. 同一项的所有过程式 derive 读取同一份去掉全部 derive 请求的语法快照，helper 和其他有效属性保留；不把前一个 derive 的产物喂给后一个。
5. 保留原项并消费自定义 derive 请求，展开其声明体；按请求顺序处理、追加各 derive 产物。输出中新出现的普通宏调用继续展开。
6. 剩余内建派生继续由既有检查/降低处理；helper 保留到所有对应消费者完成。全部结果进行普通名字、类型、权限检查。

所有宏都受同一总展开深度、次数、输出节点与 token 预算约束；宿主执行预算另见下文。首次失败返回完整展开链，不让部分产物进入降低，也不提交失败构建产物。

helper 是惰性语法，不执行宏。`attributes(a, b)` 在该 derive 所在项及其字段/变体中认可这些简单属性名；只在当前项生效，不扩展到函数体中的嵌套声明。登记从最终 derive 集合确定，所以 helper 可放在该项 derive 之前；这是 Goxide 的明确选择，不宣称复制 Rust 的顺序规则。

同一项的多个 derive 可共同登记一个 helper，例如 Serialize/Deserialize 共同读取 `serde`。宿主认可名称并向每个相关宏提供原语法；参数、位置、重复键及业务冲突由宏诊断。编译器记录 helper 的 canonical 消费者及消费状态，全部相关过程式和内建 derive 完成后才移除；例如不能因过程宏已结束就提前删除内建 Default 尚需读取的 `#[default]`。这份记录由宿主产生，不接受宏响应上传。

若名称同时解析为可执行 attribute，报告歧义，要求限定可执行路径；不能先执行再判断。没有对应 derive 的残留 helper 报未知属性。普通 attribute 不登记独立 helper；它可以读取和消费目标中的属性，保留项上的未消费未知属性仍报错。

宏不能动态新增来源依赖、过程宏入口或宏定义。输出的 `use` / `mod` 只能使用既有显式来源及正常模块发现规则，不读取任意文件；缺包与循环仍用现有诊断。运行时副作用、求值次数、短路与控制转移由生成的普通语法决定，TokenStream 和卫生不提供“一次求值”保证。

### 5. 编译期依赖、RuntimeRoot 与锁

继续使用 `[dependencies]`，按依赖 lib 的种类确定用途，不加平行包管理器：

| 依赖边 | 构建用途 |
| --- | --- |
| 普通 target → 普通 lib | 目标程序依赖 |
| 普通 target → proc lib | 编译期宏，不生成目标 Go import |
| proc lib → 普通 lib | 编译该宏程序所需的 WASI 库 |
| proc lib → proc lib | 编译宏实现时需要的上游宏，先构建上游 |
| 运行库 → proc lib 的重导出 | 公开宏身份，不改变定义点，不增加运行时包装 |

来源获取可以共用，但编译单元按来源、crate、用途、平台及 feature 区分。一个普通库可以分别参与宏的 WASI 构建和目标构建，配置与产物不能串用。目标侧沿原有统一选版规则；每个独立宏程序建立自己的 Go build list/feature 闭包，允许与目标侧选择不同版本。宏构建中的 target 是 `wasip1/wasm`；被处理程序的 target/features 则作为调用 Context 的不可变快照提供，不能拿宿主 macOS/Linux 配置替代它。

首版不向宏传递 `os.Environ()` 或当前 `config.Environment` 的环境内容，也不新增 env 白名单配置。生成语法中的既有 `cfg(env(...))` 仍由编译器按原合同处理；宏不能经 Context 枚举或查询环境。生成 `cfg` 的 feature 按语法归属读取对应构建计划中的配置，target/env 按输出阶段环境求值，保持现有规则；不能只按 module path 误取另一角色/版本的 feature。

`goxide.lock` 在现有锁中保存各角色的实际来源、版本、校验、feature 和计划关联，不另设宏锁文件。新增角色按新结构直接读写，不做旧格式兼容层。`--locked`、`--offline` 同时约束两侧；源获取和 Go 构建在宏执行之前完成，执行宏不能改变锁或下载依赖。作者目录不写入生成 Go module，编译期 staging 与目标 staging 分开。

源码发行必须包含清单、登记函数及 helper 源码。登记表由当前源码构建，不依赖已生成 Go 或 Go export data 猜回宏。宏 WASM 是本地缓存或网页构建产物，不是首版独立发行格式。宏 SDK、runtime 和编译器变更使旧构建缓存失效，直接重建，不维护多套 ABI。

运行库 A 可重导出宏库 M，但 M 不应为了生成 A 的调用而反向编译依赖 A。为此 Context 提供：

```text
RuntimeRoot(logicalModulePath) -> 已绑定的 Path，或带调用位置的错误
```

它只选择请求中已有的目标依赖根：

- 消费者是输出所属 target crate，不是宏定义 crate 或中间重导出库。
- 查询键是清单/锁中的逻辑请求 module 身份；本项目 lib 用自身 `go_mod.module`。replace 只改变内容来源，不把替换方 module 名变成备用查询键。
- 直接活动依赖、本项目可用 lib，以及显式完整根重导出可进入快照。只重导出一个 trait/宏不授予整个运行库根。
- 同来源的多个 alias 合并；冲突、未激活、未知或不可达的根返回错误。不会选版本、访问磁盘或引入依赖。
- 返回宿主签发的 canonical 路径，后续成员仍检查公开性；不返回靠字符串猜测的用户 alias，也不直接生成绕开源检查的 Go import。

例如 A 重导出 M 后，消费者直接依赖 A，M 可请求 A 的逻辑 module 身份而无需 M → A 的编译边。若上层库 B 只重导出 A 的宏，调用者仍须直接依赖 A，或 B 显式 `pub use A as runtime` 公开完整根。宏也可接受用户提供的 caller-origin 路径作为覆盖。`RuntimeRoot` 不改变普通 `crate::` 的定义点规则。

源码发现、宏构建依赖、实际宏递归和最终 Go import 环分别诊断。先锁定各计划的实际来源内容，再用包含 canonical crate、角色、选中内容与规范化构建计划身份的单元键检查拓扑；计划身份由内容/配置确定，不能在递归时分配新 ID 逃过判环。现有不含版本的 SourceKey 不能跨独立计划直接作判环键，重复发现同一逻辑 module 不等于构建环。检查通过后才运行宏实例，不靠执行宏解决自身构建环。

### 6. 宿主执行与资源边界

**建议选择 WASI command 模式，每次调用创建全新实例。** 将宏库及 SDK 降低成普通 Go，由工具链生成专用 launcher，使用 `GOOS=wasip1 GOARCH=wasm GOTOOLCHAIN=local GOWORK=off go build -trimpath -buildvcs=false` 生成 command 模块。launcher 从固定登记表选择函数；宏作者不手写 main、WASI 导出或内存指针协议。构建产物使用虚拟源码身份，宿主保留实际位置映射，不把本地绝对路径作为宏的输入能力。

CLI 建议采用 wazero；这是需要单独确认的新 Go 依赖。浏览器运行同一 WASM 字节，在独立 Worker 中提供相同的受限 WASI profile。两侧共用请求、响应及宿主验证；不另建 js/wasm 宏发行物。Go 已提供 WASI command 构建，也支持 reactor，但首版不需要长期实例、export 指针调用或跨调用状态。[Go WASI](https://go.dev/blog/wasi)、[Go WASM exports](https://go.dev/blog/wasmexport)

每个实例的 stdin 是一个 UTF-8 JSON 请求后接 EOF，stdout 必须恰好是一个 JSON 响应后接 EOF；协议之外的输出失败。stderr 是有上限的失败辅助信息，不作为成功诊断 API。JSON 解码拒绝重复键、未知字段、超深对象及越界引用，不把宽松解析差异带到浏览器。

请求包含：

- 登记入口 ID、调用类别、输出语法槽；
- 参数流、目标 item 流，以及本次可用的输入/fragment/context/span/root 引用表；
- 冻结的目标配置和 feature；不含宿主/调用者环境变量快照；
- 本次预算与仅用于诊断的虚拟源码位置。

响应包含输出树和有序诊断。输出引用采用“原输入引用”或“新建节点描述”两种形式；宿主验证后才恢复内部对象。原引用不能附带篡改的内容、类别或可信来源；允许调整的诊断位置单独表达。未知/陈旧引用、伪造已检查状态、错误结构、error 诊断、panic/trap、非零退出、未完成响应都使本次调用失败。

不向 guest 暴露磁盘、目录预打开、网络、进程启动、实际环境变量或宿主时钟。只允许下面的 `wasi_snapshot_preview1` import；其他 import 在实例化前拒绝。受支持函数操作虚拟状态，不 fallback 到宿主系统。正常 EOF、无预打开目录和已关闭描述符的错误不使整个调用失败。

| WASI 函数 | 固定 profile |
| --- | --- |
| `args_sizes_get` / `args_get` | `argv = ["goxide-proc-macro"]`，没有真实路径 |
| `environ_sizes_get` / `environ_get` | 空环境；目标的 target/features 从请求 Context 获取，没有 env 查询 |
| `fd_read` / `fd_write` / `fd_close` | 仅虚拟 0/1/2，方向不符或已关闭时返回 `EBADF`；stdin EOF 为成功且读取 0 字节；超出输出限额使执行失败 |
| `fd_fdstat_get` / `fd_fdstat_set_flags` | 虚拟字符设备及对应读写权；支持清空 flags 或 NONBLOCK，其他 flags 返回 `ENOTSUP` |
| `fd_prestat_get` / `fd_prestat_dir_name` | `EBADF`，表示没有预打开目录 |
| `clock_time_get` / `random_get` / `poll_oneoff` | 下文的虚拟时钟、固定流及就绪规则 |
| `sched_yield` / `proc_exit` | yield 成功且不读取宿主状态；exit 结束实例，非零失败 |

Go 运行时会进行这些调用，不能简单缺省 clock/random/poll，也不能把 `fd_prestat_get(3)` 或 NONBLOCK 设置当作非法 I/O 拒绝。profile 是上述有限集合，不是完整 WASI 实现；合法 Go 库若引入其他 import，也会明确报告当前宏宿主不支持。

随机流每实例从 `x = 0x6d2b79f5` 开始。每输出一个字节，依次执行 `x ^= x << 13; x ^= x >> 17; x ^= x << 5`，各步按 uint32 截断，然后输出低 8 位；多次读取连续，零长度不推进。这只是确定性兼容流，不提供随机安全性。非零初始流也避免 Go 运行时把全零熵当成失败而转入时间回退。

每实例逻辑时间 `T = 0` 纳秒，`clock_time_get` 的 realtime/monotonic 返回 T 后推进 1 ms；precision 不改变结果，其他 clock ID 返回 `EINVAL`。`poll_oneoff` 接受相对 clock 和虚拟描述符的有效读写订阅：先返回立即就绪事件，否则将 T 推进至最早相对截止，返回此时就绪的事件，按订阅顺序排列。EOF 也是读就绪；绝对 clock 订阅返回带 `ENOTSUP` 的事件；无订阅返回 `EINVAL`。关闭/方向不符的描述符按订阅返回 `EBADF`。两侧都实现该 profile，不能一侧直接使用行为不同的默认 poll。[wazero WASI 覆写入口](https://pkg.go.dev/github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1#NewFunctionExporter)

实际取消/截止使用 guest 不可见的宿主单调时钟；虚拟 sleep 不延长真实预算。常量与函数行为在实现中集中定义，并用同一组请求/响应测试验证两侧。

建议固定首版工程上限，不先增加用户配置面：

| 资源 | 上限与处理 |
| --- | --- |
| 每次调用 | 5 秒真实墙钟，从启动实例到完整退出，包含运行时和包 init；取消立即关闭实例/终止 Worker |
| 整个编译请求 | 累计宏执行时间 60 秒，包括构建上游宏时的展开；不包括 Go 工具链编译时间 |
| 线性内存 | 256 MiB，即 4096 个 64 KiB page |
| WASM 字节 | 64 MiB，实例化前检查 |
| 函数表 | 一个模块内部 funcref table，initial 不超过 65,536；maximum 固定为 initial |
| 单次请求/响应 | 各 8 MiB，解码前和写入时限长 |
| 诊断/辅助 stderr | 合计 64 KiB，超限失败 |
| 活动实例 | 每个编译请求最多一个；不在展开树内并发运行宏 |
| 结构与累计展开 | 沿用编译器现有 token/node/depth/次数预算，解码也计入 |

浏览器 Worker.terminate 只解决取消，不能独自限制内存。Go 生成模块的 memory/table section 可能没有 maximum；打包时给单一、非共享、32-bit 内存设置并验证上述 maximum，初始内存超限则拒绝，table 同样封装为固定大小。CLI 和浏览器加载同一个处理后的产物；CLI 再设置引擎内存/取消限制。拒绝 imported/multiple/shared/memory64 内存、imported/multiple table 和其他 table 元素类型。[WASM memory limits](https://webassembly.github.io/spec/core/syntax/types.html#limits)、[wazero RuntimeConfig](https://pkg.go.dev/github.com/tetratelabs/wazero#RuntimeConfig)

256 MiB 指 guest 线性内存，不是整个 CLI 进程或浏览器的总内存。编译代码、JS 对象和宿主 token 表仍需要字节/节点/实例数限额。上述容量需用完整 SDK 产物验收，不能在未实现验证器时宣称已完成隔离。

全新实例避免跨调用全局状态；实例中的 `init` 仍执行并受相同限制。只缓存编译好的宏产物，首版不缓存展开结果。产物键包含完整源码/依赖闭包、feature、构建配置、编译器/SDK/Go 工具链及 WASI profile；本地路径依赖按实际内容失效。

固定宿主输入不保证任意宏程序天然确定。宏作者不得依赖 Go map 遍历等未指定顺序；需要排序后输出。同输入、相同宏产物和配置应产生相同语法/诊断，验收覆盖冷/热缓存及两宿主结果。超时是运行条件，不承诺两台机器在边界耗时上作同样决定。[Go map iteration](https://go.dev/ref/spec#For_range)

浏览器不构建或动态下载任意宏包，只运行网站构建时固定、核验的宏产物和依赖快照；缺少宏时明确诊断。现有同步编译入口必须改为可等待宏 Worker 的异步请求，Go 回调启动编译 goroutine 后立即返回，不能阻塞 JS 事件循环。超时由运行宏的 Worker 外部执行 terminate，不能依赖被死循环阻塞的 Worker 处理取消消息；请求 ID、取消和失败后下一次编译恢复均需验收。[Go FuncOf](https://pkg.go.dev/syscall/js#FuncOf)

CLI 单文件/目录模式继续沿显式 roots 或所属 Go module 寻找宏来源；没有可用来源时给出缺依赖错误，不静默跳过宏。

### 7. 形式模型与输入输出

令 `M` 为已登记并构建的宏，`K` 为调用种类，`S` 为输入语法，`H` 为本次宿主引用表，`C` 为冻结配置与可达运行库根，`p` 为输出位置，`L` 为累计预算。`Exec` 返回候选树 `T` 和诊断 `D`；`Admit` 验证引用、结构和预算并重建宿主语法；`Fit` 检查位置类别；`Expand` 处理后续宏。

```text
Registered(M, K)   BuildReady(M)   Acyclic(buildGraph)
Encode(S, H, C, p, L) = request
Exec(FreshInstance(M), request, profile) = (T, D)
NoError(D)   Admit(T, H, L) = J   Fit(K, p, J)
────────────────────────────────────────────────────────────── [invoke]
Γ; C; H; L ⊢ M(K, S) ⇓ Expand(Insert(K, S, J))
```

`Insert` 对 function-like 替换调用，对 attribute 替换目标，对 derive 保留原项并追加产物。derive 的所有请求使用共同输入快照。预算或任一前提失败产生带调用/定义/展开链的诊断，没有可降低的部分结果。`Admit` 和 `Fit` 都不等于类型或权限检查；`resolve → check → lower` 必须在全部宏展开后继续执行。

## 转译示例

以下源级例子使用拟议 SDK，尚不能由当前编译器运行。`token` 与 `pm` 分别显式绑定 `github.com/goxide-lang/syntax/token` 和 `github.com/goxide-lang/syntax/proc_macro`，`macros` 绑定相应过程宏 lib；完整项目验收还须提供这些依赖的真实版本与清单。Go 对照是完整目标程序，不包含宏执行器。

### PM-01：函数式恒等宏与运行时求值

宏库 `example.org/identity-macros` 的 `src/lib.gox`：

```goxide
package identity_macros
use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
    return pm::Success(input)
}
```

消费项目的 `main.gox`：

```goxide
package main
use fmt;
use macros::identity;

func main() {
    calls := 0
    next := func() int { calls += 1; return 42 }
    value := identity!(next())
    fmt::Println(value, calls)
}
```

对应完整 Go：

```go
package main

import "fmt"

func main() {
	calls := 0
	next := func() int { calls++; return 42 }
	value := next()
	fmt.Println(value, calls)
}
```

输出 `42 1`。宏执行只转交语法，不执行 next；一次求值来自输入仅被插入一次。若宏复制两次或丢弃输入，运行时求值次数随生成代码变化。

### PM-02：derive 追加 API 与 attribute 转交

宏库中两个独立入口；`ParseText` 的错误由 `FromParse` 转成 Output 诊断：

```goxide
package answer_macros
use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro_attribute]
pub func keep(ctx pm::Context, args token::TokenStream, item token::TokenStream) pm::Output {
    if !args.IsEmpty() { return pm::Failure("keep takes no arguments", ctx.CallSpan()) }
    return pm::Success(item)
}

#[proc_macro_derive(Answer)]
pub func deriveAnswer(ctx pm::Context, item token::TokenStream) pm::Output {
    return pm::FromParse(token::ParseText("pub func Answer() int { return 42 }", ctx.DefSite()))
}
```

消费项目：

```goxide
package main
use fmt;
use macros;

#[macros::keep]
#[derive(macros::Answer)]
pub struct Record {}

func main() { fmt::Println(Answer()) }
```

对应完整 Go：

```go
package main

import "fmt"

type Record struct{}

func Answer() int { return 42 }

func main() { fmt.Println(Answer()) }
```

输出 `42`。keep 转交剩余 derive，derive 保留 Record 并追加 Answer；再次为另一类型请求同一宏会因 Answer 重名报错，宿主不会自动改名隐藏 API 冲突。

### PM-03：运行库重导出与别名

运行库 `example.org/description` 公开 trait，并 `pub use description_macros as macros`；消费者用任意依赖 alias 引入它。宏库通过 `ctx.RuntimeRoot("example.org/description")` 得到公开根，再构造指向其公开 helper 的路径；此处不展开尚未实现的 quote API。

预期：更换消费者 alias 后生成调用仍绑定同一公开 helper；普通 Go import 由实际叶子包决定。宏库没有编译依赖运行库，不形成反向环。消费者未获得该运行库完整根时诊断，不能靠只有宏重导出就访问任意传递依赖。

## 拒绝与边界

| 最小条件 | 拒绝位置与原因 |
| --- | --- |
| 普通 lib 上使用登记属性；签名、公开性或入口重名不符 | 登记阶段；不是可构建的宏定义 |
| 宏输出新的过程宏登记函数/宏定义 | 展开阶段；登记必须来自显式源码 |
| 同一 proc crate 构建时调用自身宏；宏构建图有环 | 运行前构建拓扑诊断 |
| 把 proc 实现函数当目标函数调用 | 普通名字/类别诊断 |
| 将原样 expr Fragment 放进 type 槽 | 输出类别检查；不通过文本扁平化逃过 |
| 替换 Span 后尝试改依赖归属、读取私有 helper | 绑定/可见性保持原规则 |
| 没有对应 derive 的 helper；helper 与可执行属性同名 | 属性归属/歧义诊断 |
| 缺少 RuntimeRoot；只拿到某个重导出成员 | 来源可达性诊断，不临时引入库 |
| 错误 JSON、无效引用、越界 token 或非法输出位置 | 宿主接收时拒绝，未进入 lowering |
| 引入文件/网络/进程等 profile 外的 import | 实例化前拒绝；环境查询只返回规定的空环境 |
| panic、trap、超时、内存/输出超限、提前退出 | 带入口及展开链的执行诊断 |
| 普通类型错误、非法引用权限、当前不支持的 unsafe | 展开后的现有检查；不是宿主成功就合法 |

Go ABI、trait 泛型能力、enum nil/typed-nil 行为和 serde codec 选择不由这个协议补齐。过程宏不能查询别名背后的类型、全盘 impl 或调用表达式的 Go 返回签名；需要此类能力的内建宏暂不迁移。

## 双向互操作

宏实现可用普通 Goxide/Go helper 库，但它们须能为 wasip1/wasm 编译，并在上述执行 profile 中运行。Go helper 不能通过 unsafe 或修改序列化字段取得宿主权限；宿主仍验证输出。宿主不会在本机执行宏包的 Go init、测试、生成指令或构建脚本，宏 init 只在受限实例内执行。

输出可以调用 `fmt::Println` 等正常可达 Go API；真实运行时 I/O 按目标程序语义执行。普通 Go 消费 PM-02 的公开生成 API：

```go
package main

import "testing"

func TestAnswerAPI(t *testing.T) {
	if got := Answer(); got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
}
```

仅沿过程宏构建边到达的 SDK/helper 不进入目标选版计划，目标程序不链接宏引擎或执行器。目标自身直接声明的普通依赖仍按既有清单规则参与选版，再按实际使用产生 Go import；同项目 bin/example 共享清单时也遵循这一点，不能按最终引用静默裁剪声明依赖。程序启动不执行过程宏。

## 验收与未定项

### 完成条件

| 编号 | 必须验证的行为 |
| --- | --- |
| A1 | 真实宏源码 → Go → WASM → 宿主展开 → 目标 Go build/run/test，PM-01/02 输出及原生 Go API 正确 |
| A2 | 三类入口、别名、重导出、错误入口、helper 共享/归属/残留、属性顺序、共同 derive 输入、单槽/成员约束 |
| A3 | raw ident、复合标点、负数、显式/隐式分号、分组与 Fragment 跨声明/过程宏往返；不得破坏现有类别反例 |
| A4 | 定义点/显式调用点/嵌套词法调用点、局部同名、重复插值、生成声明归属及私有访问正反例 |
| A5 | 修改显示 Span 不改变捕获内部变量、cfg、依赖根或权限；无效/陈旧句柄及伪造语义字段拒绝 |
| A6 | RuntimeRoot 在 alias/replace/本项目 lib/完整根重导出下正确，缺根不注入依赖，运行库重导出无构建环 |
| A7 | 宏/目标不同平台、feature 和依赖版本隔离；同逻辑 module 的不同计划不误判环；SDK 选版/replace 后协议内容不匹配拒绝，不要求 syntax 与编译器版本号相同；cold/warm、offline/locked 重放与内容失效 |
| A8 | 正常 init 探测、随机分段读取、clock sleep、poll 顺序/EOF；init 中超限、memory/table 增长、累计超时、取消、递归、过量输出、非法 import、panic、响应后死循环及下一次编译恢复 |
| A9 | CLI 与浏览器使用同一宏产物/请求，成功输出和确定错误一致；浏览器缺宏明确失败，异步界面可取消 |
| A10 | 同输入两次生成 Go 字节一致；检查源码位置、可读性、无宏运行时泄漏；普通手写 Go 消费通过 |

上述检查均待实现。手写 Go 的 WASI 机制探针只能验证工具链和宿主机制，不能替代 A1–A10。实现阶段运行仓库规定的 build/test/vet/格式检查及对应网站检查；文档阶段只验证链接、规则一致性和明确标记的机制对照。

### 实施顺序与共享代码边界

职责保持在三处：项目/编译编排准备来源、角色计划和宏产物；expand 负责名字选择、输入/输出、卫生、顺序与累计预算；宿主适配负责运行一个已准备的 WASM 请求。expand/lower 不自行启动 Go 工具链或下载依赖。CLI 与浏览器提供实际宿主实现后才按需要引入共同调用接口，前端继续使用一条展开与检查管线。

1. 接受源级及宿主合同后，从 `main` 按实际调用需要提取源 token 留存、声明列表解析、片段/来源复制及输出位置校验，配独立回归。保留现有声明宏行为，不为提取而开放零/多 item 输出。
2. 在独立 syntax module 实现手写 Go 的 token/SDK 两包，在编译器 internal 实现转换桥、启动器与宿主接收；以最小真实过程宏消费验证，区分显示 Span 和可信来源。再接源码登记、角色构建图、WASI command、CLI 三类入口及 helper；此批不实现 syn/quote。
3. 接浏览器同产物执行、取消、预算及角色隔离验收，完成 A1–A10 后再提交基础能力为已实现。实现内部可按主题提交，基础能力验收不以模拟宿主替代。
4. 声明式 Draft 分支以后再整理到共享基线上，仅保留其专用语法/匹配与差异；不把 RFC 0029 接受作为本篇前置条件。
5. 同 syntax module 中的普通 syn/quote 包、普通泛型 impl 和 Default/serde 迁移分别设计与验收。若后续 `quote!` 使用过程宏实现，其独立宏源码项目依赖 token/builder 直接构造输出，不能调用尚在构建的自身宏；普通 syntax module 不反向依赖该宏项目。源码项目可同仓库，但不因此合并 module 或构建角色。后续 Goxide 自举、生成 Go 的发行方式及跨仓库改动另按授权确定。

已存在的可提取候选是 Draft 原型中的 `ParseItems`、源 token 留存及 item/token 桥。当前 `Resolver.LookupMacro` 只返回 `MacroRulesDecl`，`invoke` 直接运行 matcher；接入真实过程宏时再拆开“调用形状”和“实现方式”，不先加没有消费者的多层后端框架。

### 接受前需确认

推荐接受上述具体方案，决策集中在：

1. `[lib].proc_macro`、带 Context 的三个登记签名、单名字空间与 helper 作用域/顺序。
2. 公共 token 与 SDK 的具体 API、Goxide token 单元及默认定义点卫生；显示 Span 与可信来源分离。独立 syntax 仓库/module 和首批手写 Go 两包的组织方式已确认，不在待确认范围内。
3. 角色分开的宏构建计划及 `RuntimeRoot` 的显式可达性，不隐式拉入运行库。
4. WASI command + 每次新实例、CLI 新增 wazero 依赖、浏览器相同产物及受限 profile。

上限数值与上述固定 WASI profile 须用真实 SDK 验收；若容量需要调整，更新本篇及共同测试，不能隐式扩大宿主能力。未经上述依赖确认，不将 wazero 写入 go.mod。当前分支只定义设计，不提取尚无过程宏消费者的代码。

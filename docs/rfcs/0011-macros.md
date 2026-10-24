# RFC 0011：声明宏与卫生展开

## 状态与范围

声明宏的模式、模板与卫生已实现；用户过程宏与正式 std/serde 宏库沿 [RFC 0030](0030-procedural-macros.md)，不属于声明宏解释器。
**2026-10-12 起本文的语法与名字解析以 [新前端规范](../design/frontend-ast.md) 为准**；下面新增的片段类别、重复模型、名字解析与分发决定取代本文其余章节中与它们冲突的旧表述，旧章节保留为历史。
依据：[原设计](../design/macros.md)。打印、派生和语法数据分别见 RFC 0012、0013、0014。

## 形式模型与阶段边界

宏接收 TokenTree、规则和定义/调用上下文，产出具体类别的 AST 及来源/卫生旁表。语法总表见[词法与语法](../design/language-grammar.md)，完整阶段模型与实际生成实例见[转译语义](../design/translation-semantics.md)。

```text
Match(rule_i, input) = 捕获表 B       i 是第一条完整匹配的规则
κ = 本次展开的新 SyntaxCtx
Expand(call, rule_i) = ParseFragment(Substitute(template_i, B, κ))

Resolve(模板自由名, κ) = 定义点查找
Resolve(模板局部名, κ) = 本次展开的局部符号
Resolve(捕获片段)      = 捕获片段原有上下文
Resolve($caller::x, κ) = 调用点查找
Owner(生成的包级声明)  = 调用包
```

捕获表保存片段类别、来源与重复层级；同层多捕获长度相等才可同步展开。模板局部身份与调用者分离，捕获表达式中的名字保留调用者绑定。执行次数由模板中的出现次数决定，卫生不隐含“实参只求值一次”。

配置选择先于源码宏收集；每次宏输出先解析为完整片段，再选择条件属性，再展开保留的子宏。最终 `ResolveExpanded → Check → Lower` 消费同一展开树。展开预算、类别错误和依赖环报告定义点/调用点；源级用户宏没有运行时宏对象，也不执行任意编译期程序。

## 语法与片段类别（2026-10-12）

宏调用是 `PathExpr` 后紧跟 `!`，参数保存为 `TokenTree`（`frontend-ast.md` §5.3、§5.6）。匹配器里的 `$name:类别`、重复与 `$caller::` 都在展开阶段解析，语法阶段只做括号配对。

片段类别：

| 类别 | 内容 | 说明 |
| --- | --- | --- |
| `expr` | 表达式 | 已实现 |
| `ident` | 单个标识符 | 已实现 |
| `tt` | 表达式 token 或括号组 | 已实现 |
| `type` | 类型 | **新增**。切片后模板的类型槽可用 `$T` |
| `item` | 声明 | **新增**。模板可展开出一个完整声明 |

- `type` 打开模板的类型位置（变量声明、参数、结果、字段、泛型实参等）。具体类型槽的类别检查在 `type` 与整块捕获之间划定：`$T` 是类型，不标注的捕获仍是表达式。
- `item` 允许模板展开完整声明（`type`、`func`、`impl` 等）。展开出的声明落在**调用方所在包**，名字按调用点解析，`pub` 表示调用包的公开。
- 类别不符、模板类型槽收到非 `type` 片段，都在展开阶段报错并给出定义点与调用点两个位置。

## 重复模型（2026-10-12）

- 一条重复 `$( ... ),*` 的体内**可以并列多个捕获**，它们按位置同步展开：

```goxide
macro_rules! table {
    ($($k:expr => $v:expr),* $(,)?) => { [ $(pair($k, $v)),* ] };
}
```

- 同一位置的多捕获长度必须一致，不一致报错。不做 zip 式配对不相等的两个列表。
- 支持 `*`（零或多次）、`+`（一次或多次）、`?`（零或一次）三种重复算子，以及模板中按同一层级展开的 `$( ... ),*`。
- 末尾逗号由 `$(,)?` 显式表达，匹配器不再隐式接受。

## 名字解析与卫生（2026-10-12）

- **默认定义点**：模板里的自由名（类型、函数、包、常量）按**定义点**的作用域解析，包括定义文件的 `use` 别名。调用点有同名项不会改变绑定。
- **crate 根**：[RFC 0028](0028-crate-roots.md) 中的 `crate::` 同样绑定定义点 crate；库宏在 bin 内展开后仍指向库根，捕获路径和 `$caller::` 保持各自的调用点规则。
- **`$caller::` 显式指调用点**：`$caller::Local`、`$caller::helpers::f` 在展开时按调用点的绑定解析。只有宏作者显式写出时才使用调用点作用域。
- **宏生成的声明**：名字按调用点解析，落在调用包里（见上节 `item`）。
- **模板局部绑定**：每次展开获得新身份（`Ident.Ctx`），调用点同名变量不会被捕获；这是定义点卫生在局部变量上的部分，与 Rust 的 `decl_macro` 方向一致。
- **捕获片段保持调用点绑定**：`$x` 是调用点的表达式、类型或声明，原样保留。
- **不静默重绑定**：任何无法按上述规则解析的自由名都是诊断，不在调用点猜。

`$caller::` 与 Rust `$crate` 是镜像关系：Rust 默认调用点、用 `$crate` 回定义包；本文默认定义点、用 `$caller::` 去调用点。选择前者是为了消除"调用方同名项被静默绑定"这类不可预测行为。若将来需要改为 Rust v1 的默认，只需交换默认与限定符的方向，源级写法不变，因此该选择是可逆的。

## 分发（2026-10-12）

- 宏**按源码分发**：引用跨模块的宏时，依赖方必须能取得该模块的源码。Goxide 的本地 path 依赖和整仓 checkout 都满足这一点。
- 预编译宏 ABI（只有 Go/metadata 产物、无源码的宏）明确不做；遇到这类输入直接报错并指出缺失的源码模块。
- 同模块内跨文件引用宏不需要额外声明。

## 转译示例

- 捕获的是带语法类别、来源和上下文的结构化片段，不是重拼后的字符串。
- 候选匹配规则：按定义顺序选择第一条完整匹配；重复层级或类别不符报错。
- 模板局部绑定每次展开取得新身份；捕获语法保留调用者绑定；自由名字按定义点解析。
- 卫生只决定绑定。实参执行次数由模板决定，不能把“一次求值”作为所有宏的隐含性质。
- 展开后仍执行普通名字、类型、借用和 unsafe 检查，再生成 Go；宏本身没有运行时对象。
- 用户宏规则不执行文件、环境、网络或任意用户程序；可以生成合法的运行时 I/O 调用。

## 转译示例

**M01：显式保存实参，保留两侧同名变量。** goxide 片段；`macro_rules!` 为候选语法。

```goxide
func twice(value int) int { return value + value }
macro_rules! twice_plus_one {
    ($x:expr) => {
        func() int {
            saved := 1
            value := $x
            return twice(value) + saved
        }()
    };
}
// 以下片段位于调用方函数体。
saved := 41
twice := func(value int) int { return -1000 }
calls := 0
next := func() int { calls += 1; return 1 }
result := twice_plus_one!(saved + next())
```

Go 对照；片段置于函数体，依赖定义点 helper 的普通函数 `macroRuntimeTwice`。
该 helper 的函数体为 `return value + value`；输出名字只是示意。

```go
saved := 41
twice := func(value int) int { return -1000 }
_ = twice
calls := 0
next := func() int { calls++; return 1 }
result := func() int {
    goxideM1Saved := 1
    goxideM1Value := saved + next()
    return macroRuntimeTwice(goxideM1Value) + goxideM1Saved
}()
```

约束：`result == 85`、`calls == 1`、`saved == 41`；调用者的同名 `twice` 不能替换定义点绑定。
不得先把捕获表达式按文本粘贴进模板，再按生成后的局部名字重新解析。

**M02：重复捕获表达式会重复执行。** goxide 片段沿用 M01 的 `next`。

```goxide
macro_rules! duplicate {
    ($x:expr) => { $x + $x };
}
result := duplicate!(next())
```

Go 对照；依赖 M01 的 `next`，从 `calls == 0` 开始观察。

```go
result := next() + next()
```

约束：`result == 2`、`calls == 2`。不得将其擅自改为保存一次再相加。
短路、闭包、defer 和 return 同样遵循实际展开语法，不由卫生额外改写控制流。

**M03：unsafe 来源不随模板移动。** 下列 goxide 候选模板不能替调用者授权。

```goxide
macro_rules! consume {
    ($x:expr) => { unsafe raw::Consume($x) };
}
consume!(raw::Other()) // Other 在调用点没有 unsafe 授权
```

预期拒绝，不生成 Go；依赖 `raw` 为未声明 safe 视图的原生 Go 包。
模板中的显式 unsafe 只按其来源授权模板引入的操作。

## 拒绝与边界

- 表达式捕获插入类型位置、重复长度或层级冲突：拒绝，不通过字符串重解析兜底。
- 模板展开到外包后调用定义包私有 helper：拒绝；应显式提供公开 helper / shim。
- 模板不得生成未知 import、宏定义或外部 API 的 declare 契约；相关限制仍属候选阶段协议。
- 展开结果缺少 impl、权限冲突或重复定义：按普通源程序拒绝。
- 递归或输出超预算：诊断包含定义、调用链和预算类别，不把半成品当成成功缓存。

## 双向互操作

goxide 宏生成的原生 Go 调用仍遵守所选 `.d.gox` 契约或 unsafe 规则。
发布模块中的 `.go` 已是展开并检查后的普通代码；Go 消费者不需要宏展开器。
Go 可调用公开生成函数，不能调用 `m!(...)`；宏规则供 goxide 工具链加载，具体承载格式未定。
增强检查只覆盖 goxide 工具链管理的代码，不给原生 Go 调用者追加源级权限保证。

## 验收与未定项

1. M01/M02 分别验证一次与两次执行；不要只比较生成临时名。
2. 同一宏重复展开、缓存命中和并行构建必须保留相同绑定关系，不复用旧调用点身份。
3. M03、私有 helper 和类别错配产生源级错误；生成 Go 错误可追溯到模板与调用点。
4. 成本：运行时成本来自展开代码；编译期成本受匹配、递归和输出节点预算限制。
5. 未定：完整捕获类别 / 重复语法、导入拼写、阶段细节、预算默认值及缓存身份协议。
6. 完整目标与末尾已实现子集分开验收；独立执行 Go 片段只能检验指定展开的行为。

## 宏 builtin 首片：len/cap（2026-10-03）

模板可直接写 `len($x)` / `cap($x)`，无需 wrapper。定义端局部/函数绑定优先，变量、类型与 import 不能误当 builtin。编译器标记在 DSL 降低后恢复原生 Go 调用，保留常量上下文、数组/数组指针的不求值规则、实参次数与 `?` 所在 frame。调用端局部变量/常量与 import 按绑定改名；struct 字段键和 map 表达式键区分处理。

[单元执行](https://github.com/goxide-lang/goxide/blob/bd90e606dcfa220b4054fe41c56bdc63a5687355/internal/macros/builtins_test.go)与[真实生成模块验收](../../tests/acceptance/macro_builtins_test.go)覆盖嵌套/跨包宏、局部及 import 遮蔽、定义端同名声明、常量/数组、None/Some 传播次数，以及生成模块 race/vet。调用包级同名声明和实际覆盖调用点的局部 named type 目前明确诊断；无关作用域的类型名称保持不变。它们是工程边界，不自动视为获批延期。首片当时未开放其他 builtin（含 append/make/new）；后续 append/copy 增量见文末，不宣称完整 Rust 宏或全面卫生已完成。零值/公开入口契约未修改。


## 显式 Go 返回值适配（2026-10-04）

内建 `go_result!` / `go_result_keep!` 已获用户授权，两种结果及传播契约见 [RFC 0005](research/0005-option-result-question.md#显式-go-result-适配宏2026-10-04)。宏可出现在声明宏模板和已支持的表达式位置；`m!(...)?` 的前处理现在保留整个 qualified 宏操作数。

实现复用宏展开器，先以内部 marker 保持捕获/定义点卫生，再展开具体适配器函数 literal；原调用位于实参，不能移进其函数体。类型参数不是通用 type fragment；首片显式 T，借用现有具体 Result 登记。原生调用必须返回 error 或 (T,error)，语义检查发生在问号类型解析稳定之后，防止暂时的 identity stub 误判原生泛型签名。局部 Result/error/nil 遮蔽、重复展开确定性和生成模块 race/vet 见[接受测试](../../tests/acceptance/go_result_test.go)。外部预编译宏规则仍未接通，独立模块测试对此明确拒绝。

### T/E 推导增量（已授权，先规范后实现）

两种 Result 适配内建改为在 typed 阶段按真实返回签名确定 arity/T/E，不增加一般声明宏的类型查询或 type fragment。规范、nil 黑白案例、显式 T 兼容、卫生/缓存/source map 及网站差分验收见 [RFC 0005 推导增量](research/0005-option-result-question.md#按静态返回签名推导-te已授权实施前规范)。此处是本轮实施契约；上节固定 error 的首片说明作为历史保留。

## 已有拒绝规则的精确诊断收口（2026-10-05，实施合同）

对应 MVP 台账 A03，本片只修诊断来源，不放宽模板语法或已有拒绝规则。
模板内部 import selector、具名 literal key、local type/struct/interface/label 等已有
诊断应定位实际触发规则的 AST token，而不是统一指向模板首 token。多问题模板按
确定的 AST 遍历顺序报告首个已识别问题，避免后续节点覆盖位置。定义文件、调用栈
及原始 token span 必须保留；捕获片段与模板来源不可混淆。没有对应 token 的合成
节点不能凭最近文字猜源位置，保留现有 fallback。

验收包括：前有合法语句的多行模板、同名 selector 重复出现、nested/package 宏、
CRLF、已有自由名和 budget 拒绝回归；诊断指向触发规则的 selector/key/声明 token，
保留既有拒绝含义。合法模板生成 Go 与执行行为不因诊断调整而改变。

宏错误保留结构化 token/span、坐标阶段和展开栈；`?` 前处理后的本文件 token
通过 prepared map 回溯，包定义的原始坐标不重复映射。定义/调用框端点来自各自
原始 token，不要求包含 `?` 改写的整个框保持等长。

单文件前端的定义点 import 解析也须复用前端 importer：原生使用既有离线导出数据，
WASM 使用已随附的真实 export archives。单文件无 package registry 不代表可以忽略
调用方传入的 importer。验收合法 `fmt.Println` 模板和其后错误 selector 在 CLI/WASM
一致；不增加浏览器包集合、文件系统、网络或外部宏包能力。


## A01 直接 append 调用实施合同（2026-10-06）

本片只接通已有宏绑定合同中的原生 `append` 直接调用，不增加捕获类别、重复语法、
类型查询或额外函数边界。`macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}`
绑定定义点 builtin；模板定义端的局部绑定和真实函数仍优先。与 `len/cap` 相同，
最终恢复原生 Go 调用，不生成 wrapper，不以赋值或逐项循环代替原生 append。

| 白例 / 已确认行为 | 黑例 / 保留边界 |
| --- | --- |
| `push!(items, event())`；原有有限重复捕获展开成多个实参；参数调用按原生次序与次数执行 | 非 slice 第一实参、错误元素类型、非法 arity/ellipsis 交由真实 Go 类型检查拒绝 |
| 模板 `append($s,$tail...)`，包括 slice 展开与原生 `[]byte` 加 `string...`；保持命名 slice 类型、容量复用、重叠追加和 nil/空行为 | 不引入新的 type fragment，不为 `make/new/copy` 或泛型 helper 自动取得模板资格 |
| 调用端局部变量、常量、参数、类型 switch 绑定和 import 同名，均按已有词法绑定机制处理；struct 字段键不被当成变量改名 | 调用包级 `append` 声明和实际覆盖调用点的局部 named type 继续明确拒绝；不改用户类型反射身份 |
| 定义端真实 `func append` 与模板局部 `append` 调用保持其定义点函数身份；重复/嵌套及同源码图跨包宏保绑定 | 定义端同名 var/const/type/import 不被冒认为 builtin；作为值的裸 `append` 继续要求显式合法绑定或诊断 |
| 条件日志未启用时不计算 append 的参数；模板含 `$v?` 时仍返回最近的用户函数，失败后不计算后续实参或执行追加 | 不把 `?` 放进新闭包，不承诺全新问号位置、外部预编译宏或 tracing span/subscriber API |

验收先保留失败基线，再执行宏绑定单元、真实 CLI 从无关 cwd 转译、生成模块
race/vet/run、重复输出与 source map，以及单文件 CLI/WASM 正反例差分。必须覆盖
定义点/调用点同名、不同文件、LF/CRLF 和原有 len/cap 回归。Mac 原生结果与历史
Linux 证据分开记录；A01 只关闭本 append 子项，RFC0011 及 A03 仍为 partial。

本片保留单文件 `transpile` / WASM 的既有降低边界：普通 append 的非法实参
可能得到相同的生成 Go，须由真实 CLI `build` / 生成 Go 编译拒绝；不把浏览器
转译成功等同于 Go 类型检查成功。宏绑定黑例则要求两端在展开阶段给出相同诊断。


## A01 直接 copy 调用实施合同（2026-10-06）

接续 A01 的已确认 builtin/卫生工程账，仅接通 `copy($dst,$src)` 直接调用。
实施前最小合法例在 `1cf4b58` 仍报 unsupported free template name；原生 copy 对照成功。
沿用现有词法绑定、builtin marker 与最终原生 Go 调用，不引入 wrapper、逐项循环、
深复制、新 type fragment、借用分析或新的捕获/重复语法。

```goxide
macro_rules! transfer {
    ($dst:expr,$src:expr) => { copy($dst,$src) };
}
// dst 与 src 是调用端已有的合法 slice。
n := transfer!(dst,src)
```

对应原生操作（捕获括号及卫生名属于实现细节）：

```go
n := copy(dst,src)
```

| 白例 / 不变合同 | 黑例 / 本片边界 |
| --- | --- |
| 目标短于/长于源、nil/空、命名 slice、重叠区域；返回 `int` 复制数 `min(len(dst),len(src))`，目标长度/容量和原存储身份不变 | 非 slice 目标、不同元素类型、错误 arity/ellipsis 交给真实 Go 编译拒绝，不默默转换或额外分配 |
| 原生 `[]byte` 目标与 string 来源；普通指针元素保持共享 pointee 身份，复制仍为原生浅复制 | 不引入 Clone/深复制，不放开 `[]rune` 与 string 等原生非法组合 |
| 两侧函数操作数按原生次序各执行一次；模板显式复制捕获则保留重复次数；作为原生副作用语句合法 | 不把所有宏捕获擅改为一次求值，也不把 builtin 当普通函数值 |
| 模板局部和定义点真实 `func copy` 保原绑定；调用点局部变量/常量/参数/type-switch 与 import alias 按已有机制处理 | 定义点 var/const/type/import 不冒认 builtin；定义点为 builtin 时调用包级同名声明和覆盖展开点的 local type 名称继续拒绝，不改类型身份 |
| `$dst?` 或 `$src?` 仍返回最近用户函数；失败后不执行复制及失败点之后的实参；禁用条件宏不求值两实参 | 不添加隐式闭包、不扩大问号允许位置；晚失败不产生部分复制 |
| 已有 defer 调用位置中，参数在注册 defer 时求值，复制在返回时执行；使用源 slice 时不提前复制其内容 | 不改变原生 defer 时序或偷偷捕获副本 |
| `copy(dst,r.Items)` 的只读标量来源可复制到独立可写目标；普通可写指针元素复制继续共享对象 | `copy(r.Items,src)` 不能写只读目标；只读指针/容器元素不能借复制抹去权限，沿用既有 refs 规则 |

验收必须包括：先红例；单元执行及 LF/CRLF 定义/调用来源；跨文件/包与重复 Go/map；
真实 CLI 生成模块独立 race/vet/run；只读正反例与原生类型黑例；CLI/WASM 分阶段差分；
独立审查及 Mac 全树 race/vet/design。Go 机制探针与 Goxide 执行证据分开。
单文件 transpile/WASM 仍可能输出普通类型错误的 Go，最终拒绝须由真实 Go 编译证明，
不宣称浏览器具备完整 Go 类型检查。既有合成括号 generated/unmapped 边界继续归 A03。

本片只关闭 A01 的直接 copy 子项；make/new、其余绑定、A03 完整来源及 RFC0011 整项
仍开放。完整 trait/borrow、blank/embedded、serde adapters 和 tracing API 不在本片决定。


## A01 固定类型 make/new 实施合同（2026-10-06）

本片接通模板中绑定到定义点 builtin 的 `make` 与类型形式 `new`，复用已有
预声明类型 alias 和 builtin 标记；不改变自由名按定义点、捕获按调用点解析的规则。
这是一片有限工程实现，不冻结 U09 的完整类型捕获语法，也不关闭 A01。
Go 1.27 原生允许 `new(expr)`；本片仅交付 `new(Type)`，前者明确保留为工程缺口，
不能将其称作 Go 类型错误或借名称白名单顺带开放。

| 白例 / 已确认行为 | 黑例 / 保留边界 |
| --- | --- |
| `make([]int,$n)`、`make([]byte,$n,$cap)`、`make(map[string][]byte)`、`make(chan int,$n)`；`new(int)`、`new(*int)`、`new([]int)`、`new([3]int)` | 类型位置不接受任何 expr/ident/tt 捕获，包括 `make($T,$n)`、`new($T)`、`new([]$T)`；`$T:type` 仍未交付 |
| 固定类型语法递归允许真正预声明类型、括号、指针、数组/slice、map 和 chan；数组长度为表达式位置，可用既有表达式捕获或模板局部常量 | 同包/导入 named type、类型参数、泛型实例、匿名 struct/interface/func 类型不在本片；非法数组常量、map key、make 类型/arity/长度等由最终 Go 检查 |
| 调用方局部 `type int string` 不得改变模板 `new(int)` 的预声明类型身份；不同文件及同源码图跨包宏保持定义点绑定 | 模板局部 `int := 7` 或定义端 `func int` 不能把 `new(int)` 偷换为值分配；预声明类型的定义包、调用包或当前文件 import 遮蔽沿用既有明确拒绝 |
| 模板局部和定义端真实 `func make/new` 仍是普通函数调用，不应用 builtin 类型门禁；显式捕获的调用目标保留调用点绑定 | 定义端 var/const/type/import 同名不能误作 builtin；裸 builtin 值、调用包同名声明及覆盖调用点的局部 named type 沿用明确诊断 |
| 原生分配的长度/容量、零值、独立存储、map/channel 可用性、size 参数顺序与次数保留；`?` 仍返回最近用户函数，失败后不求值后续参数或分配 | 不新增 wrapper、类型查询、捕获类别或问号语法；不把单文件转译/WASM 输出成功当作完整类型检查成功 |

类型检查必须在调用已确认为 builtin 后进行。第一实参的类型槽按 AST 类别检查，
类型标识符必须无模板局部绑定并通过已有 `checkPrimitiveBinding`，不能让同名函数
路径先改写为函数 alias。数组的长度不是类型槽，不能通过整棵子树禁用捕获来误伤。
数组长度表达式中，模板自身写出的显式类型位置也应用门禁，例如拒绝
`new([len([2]$T{})]int)`；完整捕获 `new([$n]int)` 的 `$n` 仍为调用点表达式，
其内部类型身份不由模板重新解析。检查针对本片 allocation 类型槽；基线在其他模板返回类型等位置已有的类别泄漏
单独保留为 A01 工程缺口，不声称本片完成全局捕获类别验证。

验收包括固定类型组合、真实原生执行与类型拒绝、定义/调用端遮蔽、模板普通函数
对照、跨文件/包、重复与嵌套展开、LF/CRLF、完整 Mac race/vet/设计检查、真实 CLI
及单文件 CLI/WASM 正反例差分。新增类型槽拒绝应保留宏定义/调用来源；下游生成
Go 的合成括号等 A03 unmapped 边界继续如实记录，不用邻近位置猜来源。


## A03 合成错误点与宏栈（2026-10-06）

本片按 [RFC0018 来源合同](0018-lowering-contract.md#a03-合成错误点与宏展开栈2026-10-06实施合同)
分别证明错误点与完整展开栈。合成点仍 generated/unmapped，已有定义/调用栈可独立
保留；包宏零长范围不能因 original 标志冒充原始位置。语言与绑定规则不变。

## A01 模板明确类型槽的捕获类别检查（2026-10-07，实施前合同）

基线 `2804d655` 的真实 CLI 已复现：`$T:expr` 可以进入 `var value $T`、
函数参数/结果、组合类型及类型断言，最后被当作调用点类型生成并执行。这违反本文
已有的表达式捕获不得进入类型位置的合同；本片修复类别检查，不引入 `$T:type`。
当前 `ident` 与有限 `tt` 也不提供类型片段资格，不得借它们绕过同一类型槽检查。

检查在填入捕获内容之前进行，使用模板 AST 的语法位置及真实定义点绑定；
捕获仍是不可拆解的调用点片段。不按捕获名字、实参拼写或预声明类型白名单猜类别，
不扫描完整捕获内部的类型名并将其重新绑定到模板定义点。

| 模板与输入 | 拒绝阶段或生成 Go / 可观察行为 |
| --- | --- |
| FC-01：`($T:expr)=>{func() int { var value $T; return 0 }()}`，调用 `!(int)` | 展开阶段拒绝占位符所在的 `var` 类型槽，不等待 Go 编译 |
| FC-02：`($T:expr)=>{func(value $T) $T { return value }(1)}`，调用 `!(int)` | 参数和结果分别作为明确类型槽检查；命名结果、变参元素遵循同一规则 |
| FC-03：`[]$T`、`[2]$T`、`map[$T]int`、`map[int]$T`、`chan $T`、`*$T` 位于类型槽 | 递归检查元素、键、值及指针目标；括号不改变类别，expr/ident/tt 均拒绝 |
| FC-04：`[]$T{}`、`value.($T)`、类型 switch 的 `case $T` | literal 的显式类型、断言类型和类型分支均在展开阶段拒绝；普通值 switch 不受影响。捕获的 nil 也不取得类型列表资格 |
| FC-05：`[len([2]$T{})]int` | 长度本身是表达式，但其中模板显式写出的 literal 类型仍检查；不能借表达式子树藏入类型捕获 |
| FC-06：`($n:expr)=>{new([$n]int)}`，调用 `!(len([2]Local{}))` | 长度捕获保持调用点身份；等价于 `new([len([2]Local{})]int)`，长度为 2，既有固定分配合同不变 |
| FC-07：`($x:expr)=>{$x}`，调用 `!(make([]Local,2))` | 等价于原生 `make([]Local,2)`；保留 Local 类型、长度 2、零值及调用点 make 绑定 |
| FC-08：完整捕获 `func(value Local) Local { return value }(Local(3))` | 整个表达式原样保留调用点绑定，值为 Local(3)，函数内部的类型不是模板类型槽 |
| FC-09：`($f:expr,$x:expr)=>{$f($x)}`、`($x:expr,$i:expr)=>{$x[$i]}`、`(*$f)(4)` | 普通调用目标、索引及函数指针解引用仍为表达式位置；没有语义证据时不能按名字把它们当作类型/泛型槽 |
| FC-10：`($f:ident,$A:ident,$B:ident)=>{$f[$A,$B](7,"x")}`，传入原生 Go `Pair[A,B any]` | 多实参实例化的 `IndexListExpr.Indices` 是明确类型槽；expr/ident/tt 捕获均在展开阶段拒绝，不把目标 `$f` 错分为类型 |

这是一项覆盖**明确类型位置**的类别修复，不声称仅凭 Go AST 就能区分所有命名
转换、单实参泛型实例和普通调用/索引。多实参实例化没有普通索引歧义，FC-10
在初版修复的真实 CLI 审查中仍可运行，故在本批补入明确类型槽。完整捕获
`identity!(Pair[int,string](7,"x"))` 或捕获整个 `Pair[int,string]` 调用目标继续
保留调用点身份；不得为阻止部分类型捕获而扫描完整表达式内部。
通用 type fragment、命名/导入类型分配及其可见性、
类别推导与诊断合同另行明确，本片不通过隐式类型捕获扩展它们。
已有模板局部 type/struct/interface 等拒绝规则继续适用。

绑定验收同时覆盖模板自由 builtin、定义点真实函数、调用点局部值/常量/import、
完整捕获及嵌套/跨包宏。调用点 `type len int` 实际遮蔽 `len` 的场景在基线已正确
给出 local named type 边界诊断，不能改名用户类型后声称卫生正确：反射 Name、
类型身份和匿名嵌入字段名都须保留。无关作用域的同名类型与定义点函数仍应成功。
本片不以 wrapper 改变常量、原生不求值规则、defer 时序或 `?` 最近帧。
局部类型别名的安全放行也须独立证明匿名字段等身份不变，不因 `IsAlias` 就直接改名。

实施顺序为保留失败基线、提交本合同、修复与单元/真实 CLI 验收、独立审查，再运行
准确提交上的 Mac 全树 race/vet/design 和 CLI/WASM 七阶段复验。正反例包括嵌套、
重复、跨定义包及 LF/CRLF，拒绝保留模板 token 与定义/调用栈。原始运行记录留仓外；
仓内仅必要脚本、夹具与短报告，执行产物检查。本片不关闭 A01、A03 或整个 MVP，
不修改公开 std/serde ABI，也不把本地验证称为 Linux/托管 CI 成功。


## A01 无冲突预声明类型拼写（2026-10-08）

模板自由类型仍绑定定义点，完整捕获仍绑定调用点。生成的 primitive alias 是保护
绑定的实现手段，不要求每次展开均保留。只有展开位置的实际词法绑定仍为同一预声明
类型时，才可恢复 `int` 等原拼写；局部 `type int string` / `type int int64` 或值遮蔽
必须保留定义点 alias。按标识符身份及作用域处理，不能全局替换同名文本。

验收覆盖无冲突 twice/重复宏、嵌套展开、局部类型与值遮蔽、无关作用域、完整捕获、
数组长度与分配、跨文件和包。定义包/调用包的包级类型与 import 冲突继续按现有
合同拒绝；不新增 type fragment 或改变用户类型身份、匿名字段名、来源映射合同。

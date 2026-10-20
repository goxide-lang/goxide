# 从 Goxide 到 Go：语义与转译模型

本文解释当前前端如何把源程序变成 Go：每个阶段接收什么、建立什么事实，以及 enum、match、引用、trait、宏、条件编译和错误传播怎样保持源级行为。[词法与语法](language-grammar.md)定义程序的形状；本文定义这些形状的含义。具体能力范围以[当前能力状态](../mvp-gap-audit.md)为准，设计决策以[决策集](decisions.md)、[前端合同](frontend-ast.md)、[enum 表示](enum-representation.md)和各已接受 RFC 为依据。

公式是描述实现合同的记号，帮助逐项检查类型、绑定、存储和求值次序；当前编译器没有附带机器验证的类型安全或语义保持证明。后文的 Go 对照取自当前编译器生成并实际运行的程序，省略生成标记与行指令，并整理空行；未展开的外围声明会在示例旁说明。

## 1. 记号与判断

| 记号 | 含义 |
| --- | --- |
| `A`、`A'` | 解析后的 AST、配置选择及宏展开后的 AST |
| `H` | 宏来源、语法上下文和展开调用链 |
| `Γ` | 源级符号环境；键包含名字和语法上下文 |
| `Δ` | 类型、enum 变体、trait、impl 与关联类型声明 |
| `C = (F, T, E)` | 各模块 feature 集、有效目标、原始环境快照 |
| `Σ`、`ℓ` | 运行时存储及其中的位置；只用于说明可观察行为 |
| `r`、`w` | 只读、可写路径资格，`r ≤ w` |
| `I` | 类型检查旁表，包括已选成员、引用资格、match 与 `?` 信息 |
| `D` | 带源位置的诊断序列 |
| `⟦x⟧` | 源级构造 `x` 的 Go 表示或降低结果 |

用以下判断区分几个不同问题：

```text
Γ; H ⊢ name ⇝ symbol               名字绑定到哪个声明
Γ; Δ ⊢ expression : type           表达式产生什么类型的值
Γ; Δ ⊢ place : type @ permission   位置可按什么资格访问
Δ ⊢ type implements trait          是否存在已登记的实现
Γ; Δ; I ⊢ expression ↦ (S, value)  降低为前置语句 S 和结果表达式
⟨expression, Σ⟩ ⇓ ⟨value, Σ', τ⟩   求值结果、存储变化和可观察事件序列
```

`type @ permission` 描述路径资格。`&T`、`&mut T` 是源级引用；普通 Go 指针 `*T` 仍是另一种源级类型构造。它们最终都可能生成 Go 指针，但在源级检查中保留不同事实。

## 2. 管线的输入与输出

核心顺序为：

```text
源码、包图、配置 C
  → ParseFile
  → Configure + Expand
  → ResolveExpanded
  → Check
  → Lower
  → Go 包类型检查 / Go build、run、test
```

| 阶段 | 输入 | 输出与建立的事实 |
| --- | --- | --- |
| `parser.ParseFile` | 文件名、UTF-8 源字节 | `*ast.File` 与诊断；节点身份、span、声明和表达式结构；宏及属性参数保留 TokenTree |
| `expand.Configure` | 原始 AST、该文件所属模块的配置 | 在首次名字/宏收集前选择完整顶层 item，规范化条件属性 |
| `expand.Expand` / `ExpandPackages` | 选中的 AST、宏源码解析器、各文件配置 | 展开后的 AST、捕获类别、定义点/调用点、SyntaxCtx、Origin 等旁表 |
| `resolve.ResolveExpanded` | 原始文件命名表、展开结果、importer | 包和作用域、声明/引用对应、可见性、Go 名、import/use 与卫生绑定 |
| `check.Check` / `CheckWithImporter` | 同一批展开 AST、解析结果、外部 Go 类型与目标尺寸 | `check.Info`；类型、构造器、match、引用权限、trait 实现、成员选择、关联投影和传播信息 |
| `lower.Lower` | 展开 AST 与对应的 `check.Info` | 文件名到格式化 Go 字节的映射；失败时返回诊断，不交付部分映射 |
| Go 检查与工具链 | 生成 Go、同包手写 Go、真实依赖与目标配置 | 普通 Go 约束检查，以及请求的构建、执行或测试结果 |

形式上，令 `success` 表示该阶段没有错误级诊断：

```text
Parse(S)            = (A, Dp)
Expand(SelectC(A))  = (A', H, De)
Resolve(A', H)     = (Γ, Dr)
Check(A', Γ, C)    = (I, Dc)
Lower(A', I)       = (G, Dl)

Emit(S, C) = G  仅当上述阶段均 success，且入口要求的 Go 检查通过
```

实现为找到宏及跨包声明，会在配置选择后建立初步包环境；最终引用绑定使用 `ResolveExpanded` 的展开树。`Lower` 消费已经选好的符号、类型与成员，不能只凭源码拼写重新猜测。原始 AST 与展开 AST 的节点不同，因此检查旁表必须和被降低的树配套。

`check.Info` 和 `resolve.Result` 是编译期旁表；生成 Go 不附带这些对象。`Lower` 自身检查生成语法并格式化；单文件和原位生成入口另调用 Go 包类型检查，build/run/test 再交给真实 Go 工具。原生 Go 声明只读参与包环境，生成步骤输出 `.gox` 对应的 Go。

入口实现可从 [单文件编译](../../cmd/goxide/compile.go)、[整包编译](../../internal/lower/package.go)、[多模块编译](../../internal/lower/module.go)继续阅读。

## 3. 名字、类型与公开边界

### 3.1 绑定先于 Go 名字

源级键是 `(Name, SyntaxCtx)`，符号身份还包含声明所在文件与节点。普通词法查找从当前作用域向外进行；宏产生的名字按第 5 节的上下文选取查找链。

```text
Γ(scope)[(name, ctx)] = s
────────────────────────── [Name]
Γ; H ⊢ name@ctx ⇝ s

Γ; H ⊢ x ⇝ s       Type(s) = T
────────────────────────────── [Variable]
Γ; Δ ⊢ x : T
```

路径中的 `::` 查询模块、类型或 trait 成员；`.` 处理值的字段/成员。`use` 引入的是目标符号或命名空间绑定，不复制一套新类型。透明 alias 复用原类型身份；新 defined 类型拥有新身份。enum 的名义身份为：

```text
Identity(E[T1, ..., Tn]) = (定义包, enum 声明, T1, ..., Tn)
```

最终 Go 名只决定目标代码的拼写。新语法声明的公开性由 `pub` 决定；Go 写法暂时接受首字母大写即公开。公开小写名生成一个首字母大写的 Go 名，私有大写新语法名加 `_` 前缀。例如 `pub enum mode` 生成 `Mode`，`enum State` 生成 `_State`。冲突在声明处诊断，不生成同义转发声明。完整规则见[可见性映射](frontend-ast.md#61-公开名到-go-名的对应)。

[RFC 0028](../rfcs/0028-crate-roots.md) 规定根绑定：`crate::` 取所属 target，bin/example 的自动库根取 `[lib].name`，显式 dependency alias 选择来源，未占名的根才查询标准库。纯 Go 来源的子路径根据真实包和导出成员解析，Goxide 来源沿 lib 的公开模块树解析。失败不切换来源，低层 Go 目录也不提高源级可见性。

解析结果保留实际叶子包身份，降低时生成普通 Go import；不同入口共享同一个库包及其类型。宏模板中的 `crate::` 使用定义点归属，同包手写 Go 与 Goxide 共享包级声明，但各文件的 import/use 仍保持文件作用域。

### 3.2 类型检查留下的事实

Go 基础类型、函数签名、可赋值性、泛型实例化和目标尺寸采用 Go 类型信息；Goxide 在此基础上检查 enum 构造、模式覆盖、引用资格、名义 trait 协议与 `?`。例如枚举构造器的判断为：

```text
Δ(E[α1, ..., αn]).V = (U1, ..., Uk)
θ = [α1 := T1, ..., αn := Tn]
Γ; Δ ⊢ ei : Si       Si 可赋值给 θ(Ui)       i = 1..k
───────────────────────────────────────────────────────── [Construct]
Γ; Δ ⊢ E[T1, ..., Tn]::V(e1, ..., ek) : E[T1, ..., Tn]
```

这里的 `θ` 是类型替换，不是运行时转换。错误发生在构造参数、泛型实参或源级类型使用处，后端不通过 `any` 存储载荷再做未检查的类型断言。

## 4. 条件编译与配置选择

配置上下文 `C = (F, T, E)` 在一次编译中显式传递。`F(m)` 属于来源节点 `m`；目标 `T` 与原始环境快照 `E` 由整次编译共享。配置构造来自哪个来源节点，由源码及宏 Origin/SyntaxCtx 决定，不能把根项目 feature 广播给依赖。

令 `ValidateC(P)` 检查一个当前实际处理到的完整谓词，然后求值：

```text
⟦feature("x")⟧C,m       = x ∈ F(m)
⟦target_os("x")⟧C,m     = (T.os = x)            其他 target 字段同理
⟦env("k")⟧C,m           = k ∈ domain(E)
⟦env("k", "v")⟧C,m    = (k ∈ domain(E) 且 E[k] = "v")
⟦all(P1, ..., Pn)⟧C,m   = ∧i ⟦Pi⟧C,m          all() = true
⟦any(P1, ..., Pn)⟧C,m   = ∨i ⟦Pi⟧C,m          any() = false
⟦not(P)⟧C,m             = ¬⟦P⟧C,m
```

`env("k")` 检查是否存在，空字符串仍表示存在；值比较使用原始字符串。谓词先整体校验，逻辑短路不能隐藏参数错误、未知谓词或 TOML 中未声明的 feature。

属性选择与表达式查询是两种不同的变换：

```text
SelectC(#[cfg(P)] item) = item       若 ValidateC(P) 成功且 ⟦P⟧C = true
SelectC(#[cfg(P)] item) = ε          若 ValidateC(P) 成功且 ⟦P⟧C = false

AttributesC(cfg_attr(P, A1, ..., An)) = [A1, ..., An]   当 P 为 true
AttributesC(cfg_attr(P, A1, ..., An)) = []              当 P 为 false

ExpandC(cfg!(P)) = Bool(⟦P⟧C)
```

多个有效 `cfg` 取 AND。选中的 `cfg_attr` 载荷在原位置递归处理；未选载荷不进入其属性语义。当前 item 所有直接生效的配置属性先完成校验，再决定保留与否。删除项不占名字、不登记宏或协议，也不展开其子树。

源文件先完整解析，所以关闭的普通源码仍须语法正确；TokenTree 内部在展开成具体片段时再检查类别语法。宏输出也先解析，随后配置选择，再递归展开保留部分。普通 `if cfg!(P)` 保留两个分支；两个分支都参与宏展开、名字、类型和权限检查。

项目 feature 的求解可写为活动依赖边上的最小不动点：

```text
F0 = 根显式请求 ∪ 根默认请求
F(n+1) = F(n) ∪ 本地 feature 展开 ∪ 活动依赖边的请求与默认请求
F* = 首个满足 F(n+1) = F(n) 的集合
```

按同一来源节点合并请求，节点身份与生成 Go 的 module 路径分开，见[依赖根合同](dependency-roots.md)。`default_features = false` 只取消该边的默认请求，不能撤销另一条边已提出的请求。optional 依赖在激活后才取得源码；循环与未知请求诊断。原生 Go 模式没有声明图，直接采用当前包 `--features` 给出的集合。

以下完整源程序在 `--features fast` 下运行输出 `fast true`：

```goxide
package main
use fmt;
#[cfg(feature("fast"))]
func label() string { return "fast" }
#[cfg(not(feature("fast")))]
func label() string { return "plain" }
func main() { fmt::Println(label(), cfg!(feature("fast"))) }
```

```go
package main

import fmt "fmt"

func label() string { return "fast" }
func main() { fmt.Println(label(), true) }
```

目标决定源文件筛选、类型尺寸、importer 与 Go 命令环境。原生 `gen` 继承有效 Go 环境；工具链工作流默认实际宿主，可显式 `--target`。环境快照固定后，只有实际处理到的 env 查询加入读取记录；项目生成身份包含这些读取的存在性与值，依赖图未变时配置变化不改写依赖锁。详细工作流、目标值域与缓存合同见 [RFC 0022](../rfcs/0022-project-toolchain.md)。

## 5. 宏展开与卫生

声明宏的输入、匹配结果和输出分别是：

```text
调用 TokenTree + 有序规则 + 调用/定义上下文
  → 第一条完整匹配规则 + 带类别和重复层级的捕获表 B
  → 模板替换 + 新 SyntaxCtx
  → 解析为调用位置要求的 AST 片段 + Origin/Hygiene 旁表
```

`expr`、`ident`、`tt`、`type`、`item` 捕获保留其语法类别与来源。模板重复中的同层多捕获按位置同步展开，长度必须相同；`*`、`+`、`?` 分别要求零次以上、一次以上、零或一次。调用位置可以是表达式、语句、类型或 item。

设定义点作用域为 `D`，调用点作用域为 `U`，本次新上下文为 `κ`：

```text
Resolve(模板自由名 x, κ)       = Lookup(D, x)
Resolve(模板局部绑定 x, κ)     = 本次展开的局部符号 x@κ
Resolve(捕获片段中的 x)         = 按捕获片段原有上下文查找
Resolve($caller::x, κ)         = Lookup(U, x)
Owner(模板生成的包级声明)       = 调用包
```

生成 item 的声明名向调用包引入；其内部模板自由引用继续遵循定义点规则。源级标识符身份建立后，降低阶段为重叠作用域的不同符号分配必要的 Go 避名。卫生保护绑定，实参的执行次数由替换次数决定：`$x + $x` 会执行捕获表达式两次，显式保存才会只执行一次。

下面完整程序输出 `85 1 41 -1000`：

```goxide
package main
use fmt;
func twice(value int) int { return value * 2 }
macro_rules! twice_plus_one {
    ($x:expr) => { func() int { saved := 1; value := $x; return twice(value) + saved }() };
}
func main() {
    saved := 41
    twice := func(value int) int { return -1000 }
    calls := 0
    next := func() int { calls++; return 1 }
    result := twice_plus_one!(saved + next())
    fmt::Println(result, calls, saved, twice(0))
}
```

```go
package main

import fmt "fmt"

func twice(value int) int { return value * 2 }
func main() {
    saved2 := 41
    twice2 := func(value int) int { return -1000 }
    calls := 0
    next := func() int { calls++; return 1 }
    result := func() int {
        saved3 := 1
        value := saved2 + next()
        return twice(value) + saved3
    }()
    fmt.Println(result, calls, saved2, twice2(0))
}
```

模板中的 `twice` 仍绑定包函数，捕获的 `saved` 仍绑定调用者的局部变量；模板局部 `saved` 得到独立符号。`saved2` 等拼写是当前输出，语言合同要求的是绑定关系而非临时名。

跨模块宏由源码提供，展开有递归深度、调用次数、节点、token 与匹配步数预算；超限或真实依赖环产生带定义点和调用点的诊断。宏展开不执行任意用户程序。具体规则见 [RFC 0011](../rfcs/0011-macros.md)与 [expand 数据结构](../../internal/expand/expand.go)。

## 6. Enum 的值与存储

用户 enum 的抽象值写为 `E::V(payload)`；其 Go 表示是密封接口和一个指向具体变体结构体的指针：

```text
⟦enum E { V1(U1), ..., Vn(Un) }⟧
  = interface E { isE() }
    + struct V1 { Value U1 } ... struct Vn { Value Un }
    + (*Vi).isE()，i = 1..n

⟦E::Vi(x)⟧ = &Vi{Value: ⟦x⟧}
```

此式省略可见性映射与泛型参数。零载荷变体是空结构体；一个元组载荷用 `Value`，多个用 `V0`、`V1` 等；命名字段使用各自 Go 名。每个泛型变体携带 enum 的全部类型参数，标记方法定义在指针接收者上。未导出的标记方法使外部包不能增加变体。

合法 enum 值须满足以下条件；运行时检查发生在实际分派处：

```text
ValidDispatchE(v)
  = v 不是 nil 接口
    且 v 的动态类型为 E 的某个 *Vi
    且其中的变体指针不是 nil
```

源码 `nil` 字面量进入 enum 类型位置时在检查阶段拒绝。Go 调用方、零值变量等仍可能带来 nil，故每次 match 分派检查接口 nil 和选中的变体指针 nil。`_` 不能捕获 nil。嵌套变体模式对内层 enum 发起新的分派；仅绑定内层 payload 不递归验证它。

复制用户 enum 复制的是接口值：

```text
Σ(a) = (*V, ℓ)       b := a
──────────────────────────────
Σ'(a) = (*V, ℓ)      Σ'(b) = (*V, ℓ)
```

因此两个 enum 值共享同一变体对象。给 `a` 整体赋新值只改变 `a`，已取得的 `&ℓ.field` 继续指向旧对象，Go GC 保持其存活。

当前 `Option` / `Result` 使用已发布 `std/sum` 的 tagged struct 与独立内联槽；构造、分派和投影通过 `NewSome` / `None` / `NewOk` / `NewErr`、`Tag*`、`Value*`、`Project*` 完成。复制其值会复制槽位；owner 替换后旧投影仍指向该 owner 的槽位，非活动槽会被清零。`Option` 的零值为 `None`，`Result` 的零值非法。`None` 与 `Some(nil)` 有不同 tag；没有指针特化。这一表示差别保留在当前输出中。

## 7. Match：一次分派与有序分支

模式判断既需要匹配值，也可能建立真实位置的引用。记 `Match(p, v, ℓ, Σ)` 为失败，或返回局部绑定 `B`。`v` 是入口值，`ℓ` 是需要借用时保存的来源位置：

```text
Match(_, v, ℓ, Σ)             = {}
Match(x, v, ℓ, Σ)             = {x ↦ copy(v)}
Match(ref x, v, ℓ, Σ)         = {x ↦ read-reference(ℓ)}
Match(ref mut x, v, ℓ, Σ)     = {x ↦ write-reference(ℓ)}
Match(c, v, ℓ, Σ)             成功当且仅当 v == c
Match(E::V(p1, ...), v, ℓ, Σ) 先检验 V，再递归匹配对应载荷
```

引用模式还要求静态可寻址性与权限成立。`match &e` / `match &mut e` 将普通 payload 绑定的默认模式改为只读/可写引用。绑定本身不增加一次 scrutinee 求值。

设 arm 为 `(pi, gi, bi)`，缺省 guard 为 `true`。运行时选择最先满足模式和 guard 的 arm：

```text
⟨scrutinee, Σ0⟩ ⇓ ⟨v, Σ1, τ0⟩
ValidDispatch(v)

依源码顺序处理适用于 v 变体的 arm：
  1. 模式失败 → 下一个 arm
  2. 模式成功 → 建立该 arm 的新绑定 Bi
  3. 求值 gi；false → 保留其副作用，继续下一个 arm
  4. true → 执行 bi，结束此次 match
```

实现将它组织为一次外层 type switch/tag switch，变体内部按源码顺序形成条件链。位于两个具体 arm 之间的通配 guard 也进入该变体的候选链，不能因分组改变顺序。普通 payload 绑定与常量模式使用首个 guard 前保存的载荷快照；引用绑定保持真实位置，同变体后续 arm 复用该位置。失败 guard 不撤销副作用，后续 arm 的局部值绑定重新建立。

穷尽性检查可以表示为无 guard 模式的值域覆盖：

```text
Cover(A) = ⋃ { Values(pi) | arm i 没有 guard }
Exhaustive(E, A) ⇔ ValidValues(E) ⊆ Cover(A)
```

guard 不计入覆盖；嵌套 enum 和 bool 等有限载荷可由多条模式共同覆盖。前序无 guard 模式已覆盖后续模式时，后续 arm 不可达。nil 不属于 `ValidValues(E)`。表达式 match 的结果必须能得到一致的结果类型；有上下文类型时允许按该类型检查可赋值性。

以下完整源程序运行输出 `3`。包路径为 `example.org/semantics/enum`：

```goxide
package main
use fmt;
enum State { Idle, Number(int) }
func score(state State) int {
    return match state {
        State::Idle => 0,
        State::Number(0) => 10,
        State::Number(n) if n > 0 => n,
        State::Number(n) => -n,
    }
}
func main() { fmt::Println(score(State::Number(-3))) }
```

```go
package main

import fmt "fmt"

type _State interface{ isState() }
type _Idle struct{}
func (*_Idle) isState() {}
type _Number struct{ Value int }
func (*_Number) isState() {}

func score(state _State) int {
    {
        var gxMatch1 int
        if state == nil {
            panic("goxide: match on nil State at example.org/semantics/enum/main.gox:5")
        }
        switch gxVar2 := state.(type) {
        case *_Idle:
            if gxVar2 == nil {
                panic("goxide: match on nil State at example.org/semantics/enum/main.gox:5")
            }
            gxMatch1 = 0
        case *_Number:
            if gxVar2 == nil {
                panic("goxide: match on nil State at example.org/semantics/enum/main.gox:5")
            }
            gxPayload3 := gxVar2.Value
            if gxPayload3 == 0 {
                gxMatch1 = 10
            } else if n := gxPayload3; n > 0 {
                gxMatch1 = n
            } else {
                n := gxPayload3
                gxMatch1 = -n
            }
        default:
            panic("goxide: invalid enum State")
        }
        return gxMatch1
    }
}
func main() { fmt.Println(score(&_Number{Value: -3})) }
```

这里 `_State`、`_Idle`、`_Number` 体现新语法未写 `pub` 的私有性。返回结果用普通局部变量保存，没有用立即调用函数包装整个 match；arm 中的外围 `return`、`break`、`continue` 等仍能保留原目标。

## 8. 引用资格与原位投影

以下判断用于源级引用上下文。普通 Go 上下文中的 `p := &x` 仍可产生原生指针；`match &x` 明确选择只读引用上下文。引用创建需要真实位置；map 元素或不可寻址临时值不能通过隐藏副本成为 payload 借用。

```text
Γ; Δ ⊢ p : T @ q       r ≤ q
───────────────────────────── [ReadBorrow]
Γ; Δ ⊢ &p : &T

Γ; Δ ⊢ p : T @ w
───────────────────────────── [WriteBorrow]
Γ; Δ ⊢ &mut p : &mut T

Γ; Δ ⊢ ref : &T        Γ; Δ ⊢ refMut : &mut T
─────────────────────  ─────────────────────────
Γ; Δ ⊢ *ref : T @ r    Γ; Δ ⊢ *refMut : T @ w
```

可写资格可以用于只读操作；只读路径不能升级为可写引用，也不能经普通指针、接口或容器擦除资格。浅复制中的独立值部分可独立修改，共享部分继续携带原路径资格。局部槽替换为新存储后按新值检查，已有副本保持其资格。

类型擦除只发生在权限检查之后：

```text
⟦&T⟧ = *⟦T⟧          ⟦&mut T⟧ = *⟦T⟧
⟦payload reference⟧ = &variant.Field   用户 enum
⟦payload reference⟧ = sum.Project*(owner)  当前 Option/Result
```

以下片段来自完整可运行例子；外围 `main` 把 `Counter::Count(1)` 复制到 `before`，调用 `bump(&mut counter)` 后读取 `before`，输出 `2`：

```goxide
enum Counter { Count(int), Empty }
func bump(counter &mut Counter) {
    match &mut *counter {
        Counter::Count(value) => { *value += 1 },
        Counter::Empty => {},
    }
}
```

对应函数中的核心 Go 分支如下；`_Counter`、`_Count` 和 `_Empty` 按第 6 节生成，外围 nil 检查与 default panic 按第 7 节生成：

```go
gxOwner1 := &*counter
var gxMatch2 _Counter = *gxOwner1
// 此处检查 gxMatch2 != nil。
switch gxVar3 := gxMatch2.(type) {
case *_Count:
    // 此处检查 gxVar3 != nil。
    gxPayloadRef4 := &gxVar3.Value
    value := gxPayloadRef4
    *value += 1
case *_Empty:
    // 此处同样检查 gxVar3 != nil。
default:
    panic("goxide: invalid enum Counter")
}
```

关键等式是 `address(value 的目标) = address(原变体.Value)`。当前检查实现类型与读写资格，不执行 Rust 式生命周期或独占借用证明。原生 Go 调用生成函数时遵循 Go 指针规则，Goxide 的只读资格不进入 Go 类型系统。完整存储合同见[原位 payload 与引用](enum-payload-borrows.md)。

## 9. Trait：名义实现、运行时视图与静态操作

trait 检查先登记实现身份，再选择成员。结构上恰好有同名 Go 方法不会自动成为 Goxide 的 trait 实现；`declare` 用于显式声明符合合同的原生方法适配。

```text
Δ 中存在 impl R for T，且全部成员与关联类型匹配
─────────────────────────────────────────────── [Impl]
Δ ⊢ T implements R

Δ ⊢ T implements R       Impl(T, R).Item = U
─────────────────────────────────────────────── [Projection]
Δ ⊢ T::R::Item = U

Γ; Δ ⊢ receiver : T @ q       Selected(T, method, q) = m
─────────────────────────────────────────────────────── [Member]
Γ; Δ ⊢ receiver.method(args) : ResultType(m)
```

`Selected` 的结果包含固有/trait 身份、具体 impl 或类型参数约束、接收权限和签名；歧义显式诊断。后端直接使用该结果。`Self` 始终指实现头的源级类型；enum 的 `Self` 是 enum 接口，不是其中某个变体。

运行时实例视图生成命名 wrapper，字段 `target` 保存目标值或真实引用。接口含 trait 身份 marker，业务方法体位于 wrapper 上；固定工厂构造视图。以下源片段来自运行输出 `Ada` 的程序，外围 main 将 `User{Name: "Ada"}` 赋给 `Named` 后调用 `name()`：

```goxide
trait Named { func name(self Self) string }
type User struct { Name string }
impl Named for User { func name(self Self) string { return self.Name } }
```

对应的运行时视图部分为：

```go
type _Named interface {
    name() string
    GoxideTraitNamed_fdfce9c4()
}
type User struct{ Name string }
type goxideUserNamed_845e7790 struct { target User }
func (gxView2 goxideUserNamed_845e7790) name() string {
    self := gxView2.target
    return self.Name
}
func (goxideUserNamed_845e7790) GoxideTraitNamed_fdfce9c4() {}
func AsUserNamed(target User) _Named {
    return goxideUserNamed_845e7790{target: target}
}
func (target User) AsNamed() _Named { return AsUserNamed(target) }
```

名称中的摘要来自类型/trait 身份。此例为值视图，所以 `target User` 保存值副本；引用视图保存指针。用户 enum 的值视图共享变体对象，owner 引用视图保存 `*E`，能观察 owner 整体替换。

静态成员和泛型 trait 约束走另一种降低：为具体类型生成空 carrier，用 Go 类型参数与方法签名表达证据，不给函数传运行时字典。下面完整程序输出 `Ada`：

```goxide
package main
use fmt;
trait Build { func make() Self }
type User struct { Name string }
impl Build for User { func make() Self { return Self{Name: "Ada"} } }
func makeValue[T Build]() T { return T::make() }
func main() { fmt::Println(makeValue[User]().Name) }
```

对应 Go 的泛型函数、carrier 和操作如下，外围保留 `User`、`main` 与 `fmt` 导入：

```go
import typeinfo "github.com/goxide-lang/std/typeinfo"

func makeValue[T typeinfo.CarrierFor[T, GoxideTOps], GoxideTOps interface {
    ~struct{}
    GoxideTraitBuild_f0aec0f7_Make() T
}]() T {
    return typeinfo.Of[T]().GoxideTraitBuild_f0aec0f7_Make()
}
type goxideUserOps struct{}
func (User) GoxideInternalTypeCarrierForSelf(User) goxideUserOps {
    return goxideUserOps{}
}
func (goxideUserOps) GoxideTraitBuild_f0aec0f7_Make() User {
    return User{Name: "Ada"}
}
func (gxOps1 goxideUserOps) Make() User {
    return gxOps1.GoxideTraitBuild_f0aec0f7_Make()
}
```

类型锚签名让 Go 推导 carrier 类型；`typeinfo.Of[T]()` 取得其零值，不调用目标类型的锚方法。关联类型 witness 同样只为 Go 类型推导提供证据，不执行。运行的只是选定操作。当前接受非泛型 trait、具体类型 impl、关联类型、静态方法和每类型参数单 trait 约束；泛型 trait/impl、多 trait 约束、GAT 与默认方法按状态表延期。成员优先级、视图能力与 Go 适配细节见 [trait 合同](traits.md)。

## 10. 错误传播与控制流

`?` 对成功载荷取值，对失败值从最近的函数（包括函数字面量）提前返回。该函数必须恰好有一个返回结果，且与操作数同属内建 Option 或 Result 家族：

```text
Γ; Δ ⊢ e : Result[T, E]       最近函数的返回类型列表 = [Result[U, F]]
E 可赋值给 F，或 CurrentPackageFrom(E, F) 恰有一个候选
──────────────────────────────────────────────────────── [TryResult]
Γ; Δ ⊢ e? : T

Γ; Δ ⊢ e : Option[T]         最近函数的返回类型列表 = [Option[U]]
──────────────────────────────────────────────────────── [TryOption]
Γ; Δ ⊢ e? : T
```

外层成功类型 `U` 可以不同于本次取出的 `T`。当 E 不能直接赋值给 F 时，`CurrentPackageFrom(E, F)` 只包含当前源包显式登记的 `From`：候选必须恰好有一个参数和一个结果，参数类型与 E、结果类型与 F 分别精确相同。零个或多个候选都报错；不会搜索其他包或串联多步转换。成功选定的唯一转换由降低阶段调用。实际判断见 [try 检查](../../internal/check/match.go)。

```text
Result::Ok(v)?   ⇓ v
Result::Err(e)?  ⇓ Return(Result[U, F]::Err(convert(e)))
Option::Some(v)? ⇓ v
Option::None?    ⇓ Return(Option[U]::None)
```

`Return` 是上述最近函数的控制流结果。已经登记的 `defer` 会按普通 Go 返回规则执行；失败之后的表达式不再求值。当前 tagged Result 的非法 tag 在传播点 panic。

以下函数取自运行输出 `bad` 的例子；外围 main 将 `Result[int, string]::Err("bad")` 传入 `next` 并匹配结果：

```goxide
func next(input Result[int, string]) Result[int, string] {
    value := input?
    return Result[int, string]::Ok(value + 1)
}
```

```go
func next(input sum.Result[int, string]) sum.Result[int, string] {
    var gxTry1 sum.Result[int, string] = input
    var gxOk2 int
    switch sum.TagResult(gxTry1) {
    case 1:
        gxOk2 = sum.ValueOk(gxTry1)
    case 2:
        return sum.NewErr[int, string](sum.ValueErr(gxTry1))
    default:
        panic("goxide: invalid Result at ?")
    }
    value := gxOk2
    return sum.NewOk[int, string](value + 1)
}
```

`sum` 的导入路径为 `github.com/goxide-lang/std/sum`。数字 tag 来自当前已发布的支持库；源级语义只依赖成功/失败变体。

复杂表达式采用“前置语句 + 剩余值”降低：

```text
Lower(e) = (Se, ve)
Lower(f(e1, e2?))
  = 保存必须先求值的函数及前序操作数
    ; 执行 e2 的传播语句
    ; 调用已保存的函数和值
```

前置语句只能进入源程序实际执行它的位置。例如：

```text
Lower(a && b?)
  = saved := a
    if saved { 执行 b 的传播语句; saved = b 的成功值 }
    结果为 saved
```

把 `b?` 的前置语句无条件提到 `a` 前面会破坏短路；把失败 `return` 放进新建的立即调用函数会改变返回目标。实现用局部变量、条件语句和控制流改写保持这些边界，普通 Go 子表达式保留 Go 原有求值规则。相关实现见 [flow.go](../../internal/lower/flow.go)、[match.go](../../internal/lower/match.go)与 [try.go](../../internal/lower/try.go)。

## 11. 可以验证的语义保持条件

转译期望满足的观察等价关系为：

```text
Check(P, C) 成功，且 P 位于当前支持范围
⟨P, Σ⟩ ⇓ ⟨v, Σ', τ⟩
──────────────────────────────────────────────────
⟨⟦P⟧C, Encode(Σ)⟩ ⇓ ⟨Encode(v), Encode(Σ'), τ⟩
```

这里 `τ` 包括用户可观察的调用、副作用、失败传播与 panic；内部临时变量和 wrapper 名不属于观察。存储编码保留第 6、8 节描述的共享关系。此式是实现和测试的验收条件，不是已完成的数学证明。

实际验证应分别检查：类型反例在正确源位置拒绝；构造和分派结果正确；带副作用的输入只执行规定次数；guard、短路、`defer` 与外围控制流次序不变；引用写入命中原位置；生成 Go 能由真实 Go 工具构建运行；相同源码、依赖和配置重复生成字节一致。

可运行回归分别位于 [核心 enum/match/?](../../internal/lower/core_e2e_test.go)、[引用](../../internal/lower/reference_e2e_test.go)、[宏](../../internal/lower/macro_e2e_test.go)、[静态 trait](../../internal/lower/p4_static_test.go)、[运行时 trait](../../internal/lower/p4_runtime_test.go)与[条件编译](../../internal/lower/cfg_e2e_test.go)。测试提供具体行为证据，当前未覆盖或延期的位置继续由[能力状态表](../mvp-gap-audit.md)列明。

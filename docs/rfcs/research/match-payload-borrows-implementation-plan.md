# Match payload 借用：实施准备与验收计划

状态：**研究提案，未实现，未运行本文新增验收案例**。代码阅读基线为
`906c10f302f651bb25a5630dda0489c5cab38a3a`，准备分支为
`feat/match-payload-borrows`。本文记录实现面和未来验收要求，不声明目前编译器能够编译下面的新语法。

语义、枚举表示和 guard 的待定决策以
[主提案](match-payload-borrows-proposed.md)为准。本文不选择 enum Go 布局，不冻结 ABI，
也不把自动绑定的成本评估当成批准。完整指针角色拆分和进一步引用检查属于后续
pre-MVP → MVP 工作；本片不能降低已有引用权限检查，也不以新增独占、生命周期或非逃逸规则代替原位借用。

## 已确认的语法分工

| 位置 | 拼写 | 含义 |
| --- | --- | --- |
| 类型 | `&T`、`&mut T` | 对目标的只读／可写引用能力 |
| 表达式 | `&value`、`&mut value` | 建立相应访问路径；原生 Go 地址表达式与受检引用的上下文边界仍须保留 |
| payload 绑定模式 | `E::V(ref p)`、`E::V(ref mut p)` | 对被匹配枚举的原始 payload 建立只读／可写引用绑定 |
| reference pattern | `&p` | 解构一个已经存在的引用；不是建立借用的 `ref p` |

明确的实施目标是 `match e` 中显式 `ref`／`ref mut` 原位借用。
`&p` 的位置分工已确认，但这不等于本片已批准所有 reference pattern、nested pattern 或自动解引用组合。
旧文档中把 `V(&p)` 当成建立 payload 引用的示例须随实施迁移，不得继续作为同义语法。

`ref mut p` 的 `mut` 修饰引用目标的写能力，不另行赋予或剥夺局部绑定的重赋值能力。
`p = otherRef` 改变局部引用槽；`*p = value` 修改目标，后者必须具有写权限。
保留普通引用的复制、保存到已支持局部槽、返回和捕获能力；可写引用不要求独占。
这里的保存不自动开放引用字段、任意容器、接口擦除或其他现有未支持边界。

不保留旧 `mut&` 引用语法兼容入口。迁移必须区分旧引用拼写与真实 Go 表达式：
`mut & x` 和 `mut&x` 均按二元按位与文法解释；名称是否已绑定由后续检查决定，
不能因字符相似或绑定状态被转成引用构造。
`&mut value` 按一元表达式位置中的 token 序列识别；空白或注释不决定权限。
若保留 `mut` 为上下文关键字，`&(mut)` 可明确表示对同名普通变量取址；
`&mut` 缺少操作数应给出语法诊断，不能按名称解析结果偷偷改变读／写意义。

静态路径与上述位置分工正交：`&mut model::Item` 是引用类型，
`model::E::V(ref p)` 是限定变体模式，`p.Field` 是值成员访问。
`use`、别名及重导出继续解析 canonical owner，不新增引用语法专用的名字白名单。

## 当前流水线与共同改动面

下面的位置描述基线代码，不是新的实现承诺；链接指向实际文件，行号用于定位基线。

| 模块 | 基线行为 | 所需工作 |
| --- | --- | --- |
| [frontend/compile.go](../../../internal/frontend/compile.go)（250–293、1025–1082） | matchexpr → enums → refs.Prepare；随后 propagate、refs.Finish、导出引用 metadata | 在 refs 擦除前保留 place、模式、nominal identity 和来源；单文件／包入口一致 |
| [enums/lower.go](../../../internal/enums/lower.go)（591–664） | arm 只有 variant/bind/guard/body 字符串；payload 只接受一个标识符，复杂／nested 模式拒绝 | 结构化绑定模式和位置；保留单一 pattern parser，不用若干字符串替换模拟模式 |
| [enums/lower.go](../../../internal/enums/lower.go)（751–814） | 保存 scrutinee 值，type assertion 得到 case 副本，再执行 `bind := tmp.Value` | 为借用保留原始 place；plain chain 和 guarded chain 共用绑定发射器；不能对 assertion 副本取址 |
| [matchexpr/compose.go](../../../internal/matchexpr/compose.go)（35–92） | 表达式 match 转为语句 match，复制完整 arm pattern | 继续传递原 pattern 与映射；不建立另一套 ref/ref mut 语义；需要解析 Go AST 的入口同步处理新引用语法视图 |
| [enums/context.go](../../../internal/enums/context.go)（15–48） | contextual body 保存原源码、segments、stages，decoder 检查版本与范围 | deferred body 保留模式来源；若新增结构字段，明确更新版本与 decoder，禁止猜测丢失信息 |
| [propagate/sum_inference.go](../../../internal/propagate/sum_inference.go)（300–351）及 [infer_match.go](../../../internal/propagate/infer_match.go)（12） | Option/Result 延后按真实类型恢复 pattern，再次调用 enum lowering；matchedEnum 目前识别 nominal enum | 携带同一 ScrutineePlan 或等价受检证据；需要引用目标时只解已批准的一层，不能擦成裸指针后靠名字猜 enum |
| [refs/lower.go](../../../internal/refs/lower.go)（408–542）及 [stages.go](../../../internal/refs/stages.go)（120–152） | 识别旧 `mut&`，write marker 和名义引用类型在 Finish 前还原为旧拼写 | 同步修改类型／表达式解析、内部 write 构造恢复和诊断；保留普通二元 `&`、权限传播及 hygiene |
| [refs/metadata.go](../../../internal/refs/metadata.go)及 [artifact.go](../../../internal/refs/artifact.go) | metadata v3 记录元素、权限、实际包身份；artifact 记录源码／依赖一致性 | 生成与加载必须 roundtrip 新拼写及真实签名；不能用错 owner、旧签名或不匹配产物绕过检查 |
| [frontend/compile.go](../../../internal/frontend/compile.go)（1525 起）、[frontend/enum_exports.go](../../../internal/frontend/enum_exports.go)、[frontend/enum_artifact.go](../../../internal/frontend/enum_artifact.go)、[enums/builtins.go](../../../internal/enums/builtins.go) | enum serde encoder/decoder 和 metadata、共享 sum 依赖当前表示 | 按主提案最终表示统一迁移构造、校验、active payload 和协议生成；不能只改 matcher |

当前主要障碍是共同的原位存储：interface 中值型 case 的断言副本不能提供原 payload 地址。
选择表示后仍须保持普通枚举值复制、共享字段的 Go 浅复制、变体身份、校验和协议行为。
不能用借用结束后复制写回、隐藏共享指针 case 或新的用户函数帧冒充等价实现。
共享 std/sum 的变动若必要，必须同步真实依赖 pin 和 producer/consumer 生成物，
而不是在编译器内临时重定义一套 Option/Result。

`&mut` 迁移不只是入口 parser 的任务：stages 的 write constructor 恢复、名义类型恢复、
metadata 导入别名／签名生成、deferred match 的 decoder 往返和相应诊断都要核对。
metadata 的 `read`／`write` 权限值与表面拼写独立；仅换 token 不要求无故升级其 schema。
若引入新的存储、投影或模式字段，则必须明确版本和缺字段的拒绝规则，不能以旧产物缺证据为由降级权限。
当前 refs 的有限具体元素检查还会拒绝 interface、某些包含 interface 的结构及泛型引用元素；
选定 inline 表示不会自动使带 error payload 的 enum 或 std 泛型 family 合法。
需要按真实元素结构和受信 nominal 登记接通这些目标，不能为了通过 match 绕开检查，
也不由此开放完整泛型引用或全部指针体系。

## 一套 ScrutineePlan／BindingMode

以下是内部 IR 建议，字段名未冻结，不是用户源码语法：

```text
ScrutineePlan {
    expression, evaluatedPlace, nominalType,
    capability, defaultBindingMode, sourceEvidence
}
Binding {
    name, projectionPath,
    requestedMode: Inherit | ExplicitRead | ExplicitWrite,
    effectiveMode: Value | ReadRef | WriteRef,
    explicitTokenEvidence, inferredModeCause
}
ReferencePattern { innerPattern, consumedReferenceLayer, sourceEvidence }
```

先解析模式请求，再结合 scrutinee 的真实类型、可取址性和访问权限计算 effectiveMode。
发射阶段只使用受检 plan，不重新从源码短名、原生 `*T` 的底层表示或调用点作用域猜权限。
`ReferencePattern` 消耗已有引用层，不能降成一个建立引用的 BindingMode。

place 表达式和索引等副作用只求值一次；借用从入口选中的活跃 payload 建立原槽投影。
后续 variant 替换可能使该槽不再活跃，旧引用行为由主提案决定。
临时量可以保存地址或必要的求值结果，但不能把隐藏副本冒充已有 place 的原始存储，
也不能让 map 元素获得原本不存在的原位地址。
函数结果或构造表达式是否可作为显式拥有的新临时值被引用，是独立的待定规则，
需明确其保存／返回、GC 存活与元素类型资格；不能在此提前禁止，也不能默默支持。
只读投影不能经这种临时量升级为可写。
语句 arm 的 `return`、`break`、`continue`、`defer`、直接 `recover` 和 `?` 保留原函数目标；
不得为复用两种语法引入可观察的新调用帧。

绑定证据分开保存：显式 `ref`／`mut` 可对应真实 token；自动默认模式没有这些 token，
只可记录来自 scrutinee 的因果范围及绑定位置。宏定义点／调用点、LF／CRLF 和 contextual body
往返后的来源仍需可区分，不能按相同文本搜索补造精确位置。

## 自动绑定的增量评估，而非批准

在同一原位借用机制上，可评估以下最小入口：

| scrutinee 形状 | 裸 payload 名字的候选默认模式 | 显式覆盖 |
| --- | --- | --- |
| `match e` | Value | `ref p`、`ref mut p` 按原 place 权限检查 |
| `match &e` | ReadRef | 不允许 `ref mut p` 升级权限 |
| `match &mut e` | WriteRef | `ref p` 可收窄为只读 |

这些入口只设置默认模式，复用相同的投影、权限检查、诊断和发射器。
显式原位借用完成后，此增量预计主要是入口解析、模式合成、deferred 路径携带和验证矩阵，
属于小到中等额外工作；没有原位表示闭环时不能把整个 #14 称作低成本。
这是静态阅读估计，没有工时或运行验证结论。

不由上述表格引入完整 Rust 默认绑定政策：

- `_` 不生成引用。当前 nested pattern 未实现，不自动承诺默认模式递归传播。
- 同一 match 的不同 arm 可用同一 IR 表达按值、显式只读和显式可写绑定；每个绑定检查实际权限。
  单个 nested pattern 内的混合规则尚未批准。
- `match r` 中 `r` 已具有 `&E`／`&mut E` 类型，需单独决定是否适用一层受检默认模式。
  不能只因源码没写 `&` 就把引用当成按值 enum，也不能静默递归解引用。
- `&&E`、`&mut &E`、多层 reference pattern 和引用 payload 不随顶层入口一起开放。
  首片若不实现，应明确报告该边界，不借此改变已经批准的 `ref` 与 `&` 语义区别。
- guard 通过其他别名写入或重赋整个 scrutinee，与普通非独占引用并存。
  variant 选择、旧借用跨替换行为、失败 guard 后继续匹配的规则依赖主提案，尚未冻结。
  不能无意间混用快照判别结果与另一份 payload，也不能临时加独占限制绕过决策。

## 真源级验收矩阵

本节所有新案例都是**未来测试输入与预期**，不代表现在已编译或通过。
“接受／拒绝”表示明确目标；“待定”表示必须先作决策或补齐有限实现范围，不能提前写成成功测试。
表内片段放在 `.gox` 函数中；除另行给出完整声明外，共用以下真实声明：

```goxide
package fixture
enum E { Empty, V(int), W(int) }
```

测试需要补齐相应函数体、变量声明和已有 roots 配置，不准以测试 helper 把旧源码自动改写成新语法。
成功案例必须通过实际 frontend，再编译和运行生成模块；失败案例检查目标诊断，不能把早先语法错误误算成权限验证。

### 明确应接受的目标

| ID | `.gox` 源码输入 | 必须观察的结果 |
| --- | --- | --- |
| P01 | `e := E::V(1); match e { E::V(ref p) => { _ = *p }, _ => {} }` | p 为只读 payload 引用，来源是 e 的原始槽 |
| P02 | `e := E::V(1); match e { E::V(ref mut p) => { *p = 2 }, _ => {} }` | 随后按值 match e 读到 2；不能只观察局部 p |
| P03 | `e := E::V(1); copied := e; match e { E::V(ref mut p) => { *p = 2 }, _ => {} }` | e 为 2，copied 仍为 1；不引入隐藏 case 共享 |
| P04 | `e := E::V(1); match e { E::V(p) => { p = 9; _ = p }, _ => {} }` | 原有按值绑定继续成立，e 仍为 1 |
| P05 | `e := E::V(1); n := 7; other := &mut n; match e { E::V(ref mut p) => { p = other; *p = 8 }, _ => {} }` | 局部重绑合法；e 仍为 1，n 为 8 |
| P06 | `e := E::V(1); match e { E::V(ref mut p) => { q := p; *q = 2; *p = 3 }, _ => {} }` | 非独占复制仍合法，e 最后为 3 |
| P07 | `e := E::V(1); match e { E::V(ref p) => { read := func() int { return *p }; _ = read() }, _ => {} }` | 捕获普通引用合法；不新增 ref-signature closure 能力 |
| P08 | `e := E::V(1); match e { E::V(ref p) => { _ = *p }, E::W(ref mut p) => { *p = 3 }, E::Empty => {} }` | 各 arm 用同一个绑定机制；权限由各模式决定 |
| P09 | `mut := 6; x := 3; a := mut & x; b := mut&x; _ = a + b` | 两种空白形式均按位与得到 2，名字 mut 不被全局禁止 |
| P10 | `n := 1; p := & /* writable */ mut n; *p = 2` | 空白／注释不改变一元 &mut 的意义；n 为 2 |

返回与保存须用完整函数验收，避免“引用必须留在 arm 内”的隐性规则：

```goxide
func take(e &mut E) &mut int {
    match *e {
        E::V(ref mut p) => { return p },
        _ => { panic("expected V") },
    }
    panic("unreachable")
}
func exercise() int {
    e := E::V(1)
    saved := take(&mut e)
    copied := saved
    *copied = 4
    match e {
        E::V(v) => { return v },
        _ => { panic("expected V") },
    }
    panic("unreachable")
}
```

目标为 `exercise()` 返回 4；它不包含 variant 替换，因此不依赖未定的旧借用跨替换政策。

### 明确应拒绝的目标

| ID | `.gox` 源码输入 | 拒绝依据 |
| --- | --- | --- |
| N01 | `e := E::V(1); match e { E::V(ref p) => { *p = 2 }, _ => {} }` | 通过只读引用直接写目标 |
| N02 | `e := E::V(1); match e { E::V(ref p) => { w := &mut *p; _ = w }, _ => {} }` | 只读投影升级为可写，不因新临时量放行 |
| N03 | `func bad(e &E) { match *e { E::V(ref mut p) => { *p = 2 }, _ => {} } }` | scrutinee 原路径只读；显式 ref mut 也不能升级 |
| N04 | `func bad(e &E) { match *e { E::V(ref p) => { var raw *int = p; _ = raw }, _ => {} } }` | 受检引用擦成原生指针 |
| N05 | `e := E::V(1); match e { E::V(ref p) => { var boxed any = p; _ = boxed }, _ => {} }` | 通过接口擦除引用权限 |
| N06 | `e := E::V(1); match e { E::V(ref p) => { q := (*int)(p); _ = q }, _ => {} }` | 显式转换不能绕过权限 |
| N07 | `m := map[int]E{0: E::V(1)}; match m[0] { E::V(ref mut p) => { *p = 2 }, _ => {} }` | map 元素不可取址；不能借副本后复制写回 |
| N09 | `func old(r mut& int) {}` | 不接受旧引用类型拼写 |
| N10 | `func old() { n := 1; p := mut& n; _ = p }` | 按二元表达式解析后 mut 未定义；不得回退为旧引用构造，区别于 P09 |
| N11 | `e := E::V(1); match e { E::V(ref mut) => {}, _ => {} }` | 缺少 payload 绑定名，报模式语法错误 |

权限复合反例必须延续现有 refs 用例：只读 struct payload 中的 slice/map/指针字段，
取址、复制聚合值、调用和赋值都不得清掉共享投影限制；纯标量／固定数组的独立值复制仍允许写入。
这类夹具须使用实际支持的具体类型，不以放开任意引用字段或泛型引用为前提。

### 待决策／待界定范围，不能记为已批准的成功案例

| ID | `.gox` 源码输入 | 需要决定或验证的内容 |
| --- | --- | --- |
| D01 | `e := E::V(1); match &e { E::V(p) => { _ = *p }, _ => {} }` | 是否批准顶层默认 ReadRef；若批准，和 P01 同一 lowering |
| D02 | `e := E::V(1); match &mut e { E::V(p) => { *p = 2 }, _ => {} }` | 是否批准顶层默认 WriteRef；若批准，和 P02 同一 lowering |
| D03 | `e := E::V(1); match &mut e { E::V(ref p) => { _ = *p }, _ => {} }` | 自动模式若获批，显式 ref 必须收窄为只读 |
| D04 | `e := E::V(1); match &e { E::V(ref mut p) => { *p = 2 }, _ => {} }` | 自动模式若获批，此组合必须以权限升级拒绝，不能只是 parser 不认识 |
| D05 | `func inspect(r &E) { match r { E::V(p) => { _ = p }, _ => {} } }` | 已有 typed reference 的普通 match r 是否应用一层默认模式；p 的类型不能含糊 |
| D06 | `func inspect(r &E) { match r { &p => { _ = p } } }` | reference pattern 消耗现有引用层，p 是 E 值；与 V(ref p) 的目标和类型不同 |
| D07 | `enum Outer { O(E) }; func nested(o Outer) { match o { Outer::O(E::V(ref p)) => { _ = *p }, _ => {} } }` | 当前 nested 模式未支持；不由自动入口默认批准递归传播 |
| D08 | `func layered(r &&E) { match r { &p => { _ = p } } }` | 多层引用本身及每层 pattern 的范围，不能静默递归解引用 |
| D09 | `enum WithRef { V(&int) }` | 引用 payload 涉及现有引用字段边界，不由 ref pattern 语法自动开放 |
| D10 | `e := E::V(1); match e { E::V(ref mut p) if replace(&mut e) => { _ = *p }, _ => {} }` | replace 在 guard 中改 variant；按主提案决定旧引用、判别及失败 guard 后的行为 |
| D11 | `match E::V(1) { E::V(ref mut p) => { *p = 2 }, _ => {} }` | 是否允许引用构造／函数结果所拥有的新临时值，及其保存／返回、GC 存活和元素类型资格；不能把它冒称为另一既有 place 的原位借用 |

D10 的完整 fixture 须定义 `func replace(e &mut E) bool { *e = E::W(2); return false }`，
并分别检查 guard 返回 true／false、同 variant 重赋与不同 variant 替换。
这些操作不能因“已有可写引用”就被临时当作独占冲突拒绝；具体可观察结果必须先由主提案决定。

### 跨流水线、模块与来源证据

以下各项是必要验收组，不以单个 parser 成功替代：

1. **求值与控制流**：`match values[nextIndex()]` 的原位投影，index 调用一次；嵌入表达式 match、
   short-circuit、未选 arm、不成功 guard、原函数 `?`、直接 recover、defer、panic、Goexit、
   外围循环 break/continue 的行为保持。借用不能提前求值未选 payload。
2. **deferred nominal 类型**：实际登记的 Option/Result，包括 generic 实例、跨文件和跨模块的 private use alias。
   对同一业务分别走 statement 和 expression match，验证类型推断恢复后仍是原 place，
   而不是 contextual marker 的值参数副本；不得按 `Option`／`Result` 短名授权。
3. **metadata 与 owner**：生成 producer，再独立编译 consumer；同拼写不同 owner、错误元素别名、
   read/write 签名错配、过期内容／依赖摘要、伪造保留 marker 均保留拒绝。
   只通过真实包身份、登记和签名检查恢复权限。artifact 的摘要是构建一致性证据，
   不是对恶意依赖的密码学认证，不扩大已有安全保证。
4. **协议与原生互操作**：按最终表示测试 Default、Clone、Debug、serde encoder/decoder、equality、
   invalid enum 及 Some(nil)/None 等既有区分；生成物脱离编译器仍能独立构建。
   原生 Go 源、普通地址／位运算及实际签名合法的调用不被全局文本重写，受检权限也不被裸 Go erasure 掩盖。
5. **source proof**：显式 ref/mut token、自动模式 scrutinee 因果范围、宏定义点与调用点、
   repeated text、LF/CRLF、contextBody encode/decode，分别断言真实 copied token 与生成片段。
   合成 token 标记 generated/unmapped；不能为了漂亮诊断伪造源 token 精确位置。

## 实施切分与交付条件

先收口主提案中的表示／guard 决策，并确定自动入口是否纳入首片；随后依次完成：
位置语法解析与旧拼写迁移、统一模式／place IR、原位发射及 refs 检查、deferred 与跨包恢复、
生成协议／共享 sum 同步、源级正反例和独立生成模块验收。
这一顺序不要求先实现完整指针体系，也不允许用降低现有权限来打通案例。

测试实现应按实际修改运行针对性验证并保留证据，最终全量交给项目现有 CI。
本文准备阶段没有运行上述测试，不能沿用旧 Go 机制探针结果宣称新编译器语法已验收。

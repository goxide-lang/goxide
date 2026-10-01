# 变体枚举、Option / Result 与模式匹配

交付边界（D20）：本文涉及的别名/借用风险 warning 是未来独立长期分析，不属于 MVP 验收；当前语言语法、类型、明确操作与真实 Go 行为照常设计。不得为未实现分析器强制复杂效果注解。

状态：**首版设计草案**。已确认变体枚举、模式匹配及与 Go 互操作的方向，并接受 Go 调用方能够构造非法值。本文提出穷尽检查、边界验证和候选 Go 表示；具体规则、语法与 ABI 尚未冻结。目标工具链与生成模块语言版本均为 Go 1.27；实现分支已有有限本地 enum / 语句 match 及 serde 派生，见[准确边界](../../IMPLEMENTATION.md)；本篇完整语义与 linter 尚未全部实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D12、D13：变体枚举 / 模式匹配、Option / Result、Go 可构造非法输入以及公开边界方向。 |
| 暂定 | T03–T05：interface 加值型 case、nil 策略、panic / 递归验证、模式子集、值绑定、穷尽与控制流规则。 |
| 未定 | U06、U08：最终 ABI、递归对象 / 共享状态校验、完整初始化算法；借用模式延期。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 hgo 功能已实现。

## 0. T0 的常量增强与带载荷范围

D21 将 enum/match 列为 T0。Go 数值/string const 的既有零值或未知值问题不是阻塞理由；T0 优先级不要求先解决所有嵌套共享图验证。

以下区分是候选范围，不冻结新语法：

| 场景 | 最小能力 / lowering | 未知或非法值的候选行为 |
| --- | --- | --- |
| 数值/string 常量枚举，如订单状态 `"pending"` / `"paid"` | 保留真实底层数值/string 与显式常量取值，补名义身份、成员提示和 match 缺项检查；Go 可为命名基础类型 + const | Go 仍可传 `""` 或未来新增状态。开放/raw 值匹配需 `_` 或显式未知值处理；不普遍禁止原生转换，也不悄悄把零值改成第一个成员 |
| 带载荷 enum，如 `Item(T)` / `Failed(error)` | 下文 interface/case 是一种候选 ABI，表达常量本身没有的数据选择 | 在执行受检查 match 时验证当前值；非法 Result nil/伪造 case 走明确边界失败（现候选 panic），不当作业务 Err，也不由 `_` 掩盖为合法 case |
| Option | None/Some 区分缺失与存在的 payload | 现候选 nil 表示 None；Some(nil) 取决于 payload 类型，与 None 不同 |

常量示意 lowering：`type State string; const Pending State = "pending"; const Paid State = "paid"`；原生未知 `State("future")` 仍可存在。闭合集合转换可显式返回校验失败；不把开放 Go 数据强制转换成闭合集合，也不将这种转换设为所有调用的门票。确切源语法、是否自动生成未知值模式尚未选定。

只枚举已知成员不能声称覆盖开放输入；对受检查闭合值可以检查已知成员穷尽。带载荷 match 的非法值分支是边界诊断，不是业务变体，不改变下文“guard 不计覆盖”等规则。原生存储读取/清零后要在哪里验证仍是 U06/U08；允许先实现有限、明确的使用点检查，不把全图/别名分析放回 T0 门槛。来源不明不自动拒绝；无借用分析不等于 Go 零值自动合法。

### 0.1 常量入口的限定实现（同值别名未定）

本轮重新核对 D21、本文第 0 节及现有 RFC 后，不重开底层 raw 值、Go 转换、未知值保留和开放 match 必须兜底的方向。仅提出沿 Go 类型声明和现有 `Enum::Member` 惯例的表层入口：

```hgo
enum Status string {
    Pending = "pending",
    Paid = "paid",
}

status := Status("future")
match status {
    Status::Pending => { handle_pending() },
    Status::Paid => { handle_paid() },
    _ => { handle_unknown(string(status)) },
}
```

建议先支持显式整数/string 常量，不增加隐式 iota、自动 Unknown 变体或新的封闭包装类型。降低仍是 Go 命名基础类型及 typed const；未知值不是非法带载荷 case，不触发后者的 match 前 panic。即使写全已知成员，也不能删掉开放输入的兜底。Go 零值原样保留，不改成首个成员。用户已授权先实现唯一 raw 值成员的这一限定入口；正式语法不因原型交付而整体冻结。同值别名继续待答复，带载荷 enum 的 `#[default]` 规则也不自动扩展到这里。

唯一待用户选择的语义是同一 enum 的重复 raw 值：

- 推荐允许作为 Go 风格同值别名，例如 `Pending = "pending", Queued = "pending"`。运行时不能区分别名，match 的覆盖/不可达必须按实际值而非成员名字计算；两个无 guard 同值分支不可达，不凭成员名虚构第二种状态。
- 另一方案要求成员 raw 值唯一，诊断更简单，但不能直接表达原生协议的同值别名。

两种方案都保留未知值和普通 Go 转换，选择不涉及重新要求所有输入先作闭合验证。问题未回答前，只实现不依赖该选择的唯一值成员子集。同值声明暂报“别名规则待用户决策，当前子集未支持”，不把它记录为永久禁止，也不假定允许；其余常量 parse/type/lowering、Go 转换和开放 match 可继续。

实际实现与 Go 互操作验收见 [常量 enum 增量](../t0-acceptance.md#常量-enum-唯一值子集增量)：唯一值入口已接通，同值别名仍待决定；raw 派生和 wildcard-only match 未实现。类型化常量不采用带载荷 case ABI。

## 1. 源级类型与构造

```hgo
pub enum Event[T] {
    Idle,
    Item(T),
    Failed(string),
}

pub enum Option[T] {
    None,
    Some(T),
}

pub enum Result[T, E] {
    Ok(T),
    Err(E),
}

var event Event[int] = Event::Item(7)
var value Option[int] = Option::None
var result Result[int, string] = Result::Ok(7)
```

enum 是具有包身份的名义类型。每个值恰好属于一个声明的变体，payload 必须满足该变体的类型。不同枚举即使变体同名也不互换；`Option[int]` 与 `Option[string]` 也不互换。类型参数由期望类型和 payload 推导，不能确定时要求显式给出。

首版模式支持无 payload 或单 payload 变体；多个数据项可以放在一个普通 struct 中。多字段变体、字段简写、or-pattern、范围模式和空枚举留待扩展，不能借此改变首版既有类型的表示。直接递归的枚举需经过可容纳递归的句柄或容器，递归类型的合法性由前端检查，不依赖 Go 接口恰好能打断布局循环。

普通 enum 与 `Result` 不隐式选择首个变体作默认值。`Option` 的默认值明确为 `None`；源代码仍使用 `None` / `Some`，不会因此引入“任何类型都能赋 nil”的规则。`Result::Err(nil)` 是否有效由 E 自身决定，例如可空的 `Ptr[T]` 可以为 nil；它与整个 Result 接口为 nil 是两件事。

赋值和模式绑定默认延续 Go 值复制语义，不引入全局 move。复制 struct 会复制字段；复制 Arc、Ptr、slice 或 map 不会深复制其引用的数据。不得把 enum 装箱说成所有权转移，含引用的值装箱/逃逸默认允许；保留来源以诊断具体风险。

## 2. match 的求值与控制流

```hgo
label := match load_result() {
    Result::Ok(value) if value > 0 => "positive",
    Result::Ok(_) => "non-positive",
    Result::Err(message) => message,
}
```

被匹配表达式只求值一次，先保存为临时值，再按源码顺序尝试分支。模式检查本身不调用用户代码；模式成功后才建立该分支的绑定、求值 guard，guard 为 true 才执行分支体。guard 为 false 则继续后续分支，不重新调用 `load_result()`，也不撤销已经发生的副作用。只执行一个分支体，不允许 fallthrough。

临时值固定外层变体和已复制的字段。若 payload 包含共享 slice、map 或句柄，guard 对其指向数据的合法修改仍可能被后续分支观察到；“只求值一次”不等于制作整个对象图的快照。

首版提供三种模式：完整限定的变体模式、绑定名、`_`。payload 可以递归使用这些模式，例如 `Option::Some(Option::None)`。常量条件先写成 guard，不增加隐式方法调用、动态相等性协议或解构副作用。

### 2.1 穷尽性与不可达分支

每个 match 都检查穷尽性，包括只用于执行副作用的 match。检查基于源级 enum 元数据，而非降低后的 Go interface 方法集：

- 无 guard 的分支才贡献覆盖集合；首版连 `if true` 也不特殊用于证明穷尽。
- `_` 或整个被匹配值的绑定模式覆盖其余所有**合法**值。
- `Some(Some(_))` 不覆盖 `Some(None)`；嵌套枚举按路径检查。
- 某个变体的 payload 只有 `_` / 全绑定模式或递归穷尽的模式集合，才算覆盖该变体。
- 已被前面的无 guard 模式完全覆盖的分支报不可达诊断。前面的 guard 分支不会使后续分支静态不可达。

例如只写 `Ok(v) if v > 0` 与 `Err(e)` 仍缺少其余 Ok；编译器不能把 guard 条件当成自动证明的业务不变量。Go 输入中的非法动态类型不属于源级覆盖集合，也不能被用户的 `_` 分支吞掉，必须先在边界或受检查的读取处拒绝。

### 2.2 绑定与作用域

首版采用值绑定：从保存的合法 case 复制 payload 到分支局部变量，作用域包含该 guard 和分支体。改写这个局部值不会回写枚举；经共享句柄修改对象仍按该句柄的权限检查。再次尝试后续分支时重新建立绑定。

不自动取地址，不把值匹配变成 `mut&`，也不从 Go type switch 得到的 case 副本中制造“指向原枚举字段”的可变借用。首版不支持 `match mut& place` 等原位借用模式；普通引用 payload 的值复制不因借用身份拒绝；真正明确不可复制的资源类型约束另行定义，不能借此恢复全局独占检查。已有 `&T` / `mut& T` 的装箱、返回、闭包、goroutine 和接口逃逸继续受引用专题约束；枚举不会绕过这些检查。

### 2.3 表达式结果与非局部退出

match 是表达式，也可在语句位置丢弃结果。有期望类型时，各个能正常结束的分支都按该类型检查，包括既有的 trait 视图转换；没有期望类型时，分支结果必须具有可统一的源级类型。不能统一时要求注解或显式转换，不自动退化为 `any`。无结果分支的类型为 Unit。

必定 `return`、`panic` 或其他不继续求值的分支不贡献正常结果类型。分支内的 `return` 仍返回原函数；`break` / `continue` 仍按源码中的合法标签或循环目标解析，match 不创造可 break 的循环。生成代码需要保证所有正常完成路径都给结果临时变量赋值。

后端把 match 展开为“求值一次的临时量 + 校验 / 分支 + 结果临时量”。不能无条件套一个 Go 立即调用闭包，因为它可能改变 return、defer 和循环跳转的作用域。对同一变体的多个 guard，应在一次 type switch 的对应 case 内保持源码顺序，不能生成重复的 Go case 或改动副作用顺序。

展开还必须保留 match 与外围表达式的求值顺序及短路行为；例如调用参数中的 match 不能被提前到先前参数的副作用之前，短路右侧的 match 不能无条件执行。若变体分支之间穿插带 guard 的通配模式，生成的各条决策路径也必须按源码顺序尝试这些 guard，不能仅按变体分组后丢掉它们。

## 3. 首版 Go 表示

首版候选采用普通 Go interface 加**值型变体 struct**，不需要 Go `unsafe`、内存重解释或 union。普通枚举的无 payload 变体也有自己的零字段 case struct；只有标准 Option 的 None 使用 nil 接口这一约定。

| hgo 值 | 生成 Go 值 |
| --- | --- |
| `Event[T]` | 带枚举专属 marker 的接口 |
| `Event::Idle` | 精确的 `EventIdle[T]{}` 值 |
| `Event::Item(x)` | 精确的 `EventItem[T]{Value: x}` 值 |
| `Result::Ok(x)` / `Result::Err(e)` | `Ok[T, E]{Value: x}` / `Err[T, E]{Error: e}` 值 |
| `Option::None` | `Option[T]` 的 nil 接口 |
| `Option::Some(x)` | `Some[T]{Value: x}` 值，包含 x 自身的合法零值 |

marker 名称区分枚举声明，签名携带**全部类型参数**，例如 `hgoResult(T, E)`。即便 Ok 的存储只需要 T，也不能漏掉 E；只有零参数 marker 会使不同实例意外具有相同方法集。marker 只提供 Go 类型约束，不在验证时调用，也不代表额外运行时 payload。

所有 case 使用值接收者 marker。Go 因而也允许 `*Some[T]`、`*Ok[T, E]` 满足接口，但这些指针不是规范 case，无论是否 nil 都必须拒绝。case 类型别名解析到同一真实类型可以接受；新定义类型或嵌入 wrapper 不能只凭方法集通过校验。

未导出 marker 可以减少意外手写实现，但不能封闭 Go 接口：外部类型仍能通过嵌入接口或 case 获得方法。**合法 case 集合由精确的 type switch 白名单决定**，不调用 marker，不接受“任何实现同一接口的值”。Go type switch 检查实际动态类型；接口中的 typed nil 与 nil 接口不同。[Go type switch](https://go.dev/ref/spec#Type_switches)、[Go FAQ：nil 接口](https://go.dev/doc/faq#nil_error)

稳定符号编码应包含包和枚举身份、变体身份及泛型参数位置；正式名称方案另行冻结。本文 `Some`、`Ok` 等短名只展示语义，不规定所有用户 enum 的 Go 导出命名。

## 4. nil、嵌套 Option 与裸指针

| 输入与目标类型 | 处理 |
| --- | --- |
| nil 接口传给普通 enum 或 `Result[T, E]` | 非法，拒绝 |
| nil 接口传给 `Option[T]` | 合法，解释为 None |
| typed nil `*Some[T]` / `*Ok[T, E]` | 非法，不解引用，也不解释为 None |
| 非 nil 的 case 指针或嵌入伪造值 | 非法，拒绝 |
| `Some[Option[int]]{Value: nil}` | 合法，表示 Some(None) |
| `Some[Result[int, string]]{Value: nil}` | payload 非法，递归验证后拒绝 |
| `Some[Ptr[int]]{Value: nil}` | 合法，表示 Some(nil) |

普通 Result 签名保持严格，不能为了接受 nil 悄悄放宽为 `Option[Result[T, E]]`。保持原签名的公开入口在非法输入时 panic，诊断注明参数与嵌套路径；如需返回错误，应另外提供显式的解析 / 校验适配函数，而非改写业务返回类型。

`Option[Option[T]]` 有三个层级不同的状态：None、Some(None)、Some(Some(value))。外层 Some 始终保留自己的 struct 包装，不能把内层 nil 展平为外层 None。

原生 `*T` 默认按[裸指针规则](foreign-api.md)导入为可空 `Ptr[T]`，不自动成为 Option。`Option[Ptr[T]]` 有 None 与 Some(nil) 两种不同的空相关状态，必须保留判别信息。

若未来针对非空 Arc 等句柄采用 `Option[Arc[T]]` 到 `*T` 的专门表示，必须先证明 payload 不能为 nil，并定义嵌套、泛型实例和 Go API 的桥接。该优化不是本篇默认 ABI；不能推广到 Ptr、所有指针状类型或嵌套 Option，也不能仅凭当前一次非空观察选择布局。

## 5. Go 双向互操作与边界验证

### 5.1 Go 调用生成的 hgo API

Go 调用方可构造导出的规范 case 值并传给生成接口，例如 `Ok[int, string]{Value: 7}`。它也可对返回值做 type switch；Go 自身不提供 hgo 的穷尽性诊断。公开 case 字段遵循类型可见性设计，构造函数可以提供便利，但不能假设 Go 只经构造函数创建值。

每个从 Go 进入 hgo 的函数、方法、回调及其他数据入口都按目标签名验证。外部回调返回的 enum、从外部对象字段读取的 enum、从 Go 可写共享容器读取的 enum，也属于输入。hgo 调用 Go 前输出规范值；同一次 Go 调用返回后，任何可能被它改写的别名不能沿用调用前的验证结论。

验证既检查精确 case，也检查对应 payload 的源级约束。标量 int / string 无需额外检查；嵌套 enum 递归检查，非空句柄检查空值，容器检查当前有效元素。泛型验证需要保留或传入类型参数对应的验证能力，不能以 `T any` 为由把所有 T 都当成天然有效，也不使用开放接口上的用户方法来判断可信度。

一旦值离开受信任的 hgo 路径，精确 case 检查仅说明当前外层合法。共享 slice、map、指针对象可能在之后被 Go 修改；一次深层扫描并不建立永久不变量。首版 safe 声明必须约束同步与修改行为，或通过实际包装复制所需数据；无法建立稳定契约不再自动要求 unsafe；有具体风险链则 warning，已选定的合法值边界检查仍执行。竞争访问仍须同步，验证不能修复数据竞争。循环对象图的遍历、快照与递归验证策略须单独实现，不能承诺所有任意 Go 对象都能无成本安全导入。

### 5.2 hgo 导入 Go 值与原生 API

对带 hgo 元数据的生成 Go 包，编译器恢复源级 enum 身份、变体及校验规则。没有这些元数据的普通 Go interface / const 集合不会自动被猜成封闭 enum；需要明确声明或实际包装。

原生 Go `(T, error)` 不自动等价于 `Result[T, E]`。返回约定、成功时的 T、错误时是否保留 T、typed nil error 等行为必须由声明或包装明确处理。同样，`(*T, bool)`、nil 指针与 Option 的转换都必须保留原始协议的信息，不按类型形状一概推断。

普通 `declare` 可以提供已经 ABI 兼容的类型契约。若映射需要检查、重新装箱或把多返回值改成一个 Result，就必须有实际桥接逻辑，不能只靠擦除声明完成。普通 callable 缺摘要不自动 unsafe；明确 unsafe 操作及声明可信边界继续遵守[原生 API 与裸指针](foreign-api.md)。

## 6. 初始化、容器与 Go 零值

Go 会为变量、数组元素和新分配存储提供零值，map 缺失查询和已关闭 channel 的接收也能得到元素零值。接口枚举的这个零值是 nil；只有 Option 将其定义成合法 None。[Go 零值](https://go.dev/ref/spec#The_zero_value)、[Go 索引表达式](https://go.dev/ref/spec#Index_expressions)、[Go 接收操作](https://go.dev/ref/spec#Receive_operator)

hgo 前端必须在产生或读取这些值的位置处理，不能等普通 Go 编译器发现：

| 操作 | 首版规则草案 |
| --- | --- |
| 未初始化的普通 enum / Result 局部变量或字段 | 使用前必须有合法赋值；不允许隐式观察 nil |
| `[N]Result[T, E]`、struct 中的 Result 字段 | 要求完整初始化，或使用只在完成后暴露对象的受检查构建方式 |
| `make([]Result[T, E], n)` 且 n 非零 | 不作为已初始化的 safe 容器；使用逐项生成器或先创建长度为 0 的 slice 再 append 合法值 |
| slice 的扩长、reslice 暴露新元素 | 不能让未初始化容量进入可读范围；需初始化检查，不能只检查第一次 make |
| map 缺失项，V 没有合法默认值 | 不允许直接把 `m[k]` 当 V；使用返回 `Option[V]` 的 checked lookup，或先分支检查原生 comma-ok 的 ok |
| channel 接收且元素没有合法默认值 | 必须区分关闭状态，关闭时不暴露伪造的元素值；可用 Option 或受检查的 comma-ok 形式 |
| 泛型零值 / 部分初始化构造 | 只有已知类型具有合法默认值时才允许；无约束 T 不满足该条件 |

checked lookup 的具体源码拼写待标准库设计；降低到 Go 时先读取 `(value, ok)`，只在 ok 为 true 时包装 Some 或允许读取 value。value 的临时 Go 零值可以存在于生成代码中，但不得成为可观察的 hgo V。普通 enum 即使有零字段变体也不因此自动得到默认值。

`[6]Option[T]` 的零值可以是六个 None；`[6]Result[T, E]` 的六个 nil 则不是合法 Results。纯 Go 可以自由制造后者并传入，公开边界必须逐元素拒绝，不能声称 hgo 的内部初始化检查限制了纯 Go。即使 Go 只单向导入 hgo，泛型 `Zero[T]`、回调和可变容器也足以制造这些状态。

## 7. 完整 Go 降低示例

下面是一个可独立编译的 Go 包，演示规范值型 case、严格 Result 输入、嵌套 Option 与可空 Ptr。标量示例使用精确 type switch 同时完成验证与匹配；`NestedState` 显式验证内层，不把通用外层检查冒充递归检查。错误路径在执行业务分支前 panic。

```go
package enumdemo

import "strconv"

type Option[T any] interface {
    hgoOption(T)
}

type Some[T any] struct {
    Value T
}

func (Some[T]) hgoOption(T) {}

type Result[T, E any] interface {
    hgoResult(T, E)
}

type Ok[T, E any] struct {
    Value T
}

type Err[T, E any] struct {
    Error E
}

func (Ok[T, E]) hgoResult(T, E)  {}
func (Err[T, E]) hgoResult(T, E) {}

type Ptr[T any] *T

// Describe 对应先匹配 Ok，再尝试其 guard，最后匹配 Err。
// 精确值型 case 之外的动态类型（包括 nil）都被拒绝。
func Describe(input Result[int, string]) string {
    saved := input
    switch value := saved.(type) {
    case Ok[int, string]:
        if value.Value > 0 {
            return "positive"
        }
        return "non-positive"
    case Err[int, string]:
        return value.Error
    default:
        panic("hgo: Describe.input: invalid Result[int, string]")
    }
}

// 外部回调的返回值重新进入验证；fetch 仅调用一次。
func FetchAndDescribe(fetch func() Result[int, string]) string {
    if fetch == nil {
        panic("hgo: FetchAndDescribe.fetch: nil callable")
    }
    saved := fetch()
    return Describe(saved)
}

func NestedState(input Option[Option[int]]) string {
    switch outer := input.(type) {
    case nil:
        return "None"
    case Some[Option[int]]:
        switch inner := outer.Value.(type) {
        case nil:
            return "Some(None)"
        case Some[int]:
            return "Some(Some(" + strconv.Itoa(inner.Value) + "))"
        default:
            panic("hgo: NestedState.input.Some: invalid Option[int]")
        }
    default:
        panic("hgo: NestedState.input: invalid Option[Option[int]]")
    }
}

func PointerState(input Option[Ptr[int]]) string {
    switch value := input.(type) {
    case nil:
        return "None"
    case Some[Ptr[int]]:
        if value.Value == nil {
            return "Some(nil)"
        }
        return "Some(pointer)"
    default:
        panic("hgo: PointerState.input: invalid Option[Ptr[int]]")
    }
}
```

Go 下游可直接调用 `Describe(Ok[int, string]{Value: 7})`；`NestedState(nil)` 与 `NestedState(Some[Option[int]]{Value: nil})` 分别得到 `None` 与 `Some(None)`。`PointerState(Some[Ptr[int]]{Value: nil})` 则得到 `Some(nil)`，无需解引用裸指针。

把 `(*Ok[int, string])(nil)` 传给 Describe 可以通过 Go 接口赋值，但会被 default 拒绝。`type Forged struct { Result[int, string] }` 也能通过嵌入满足接口，而 `Describe(Forged{})` 同样被拒绝。这里没有调用 marker，因此不会把嵌入的 nil 当作合法 case，也不会在验证中触发其方法。

## 8. 成本、兼容性与首版限制

接口装箱可能产生逃逸与堆分配；大 payload 的构造和匹配可能发生复制。是否分配需测量生成代码，不能把“使用值型 struct”写成“零分配”。匹配顶层 case 的成本与验证整个对象图不同，递归验证、容器复制、泛型验证参数与桥接都有额外成本。

生成接口不能直接承担所有源级操作。例如含 slice 的 case 不可比较，Go 接口相等比较可能 panic；首版不自动为所有 enum 开放 `==` 或 map key 资格，需另行定义 payload 的相等性与可比较约束。序列化、反射、跨进程格式也不是这个 Go ABI 自动提供的稳定协议。

为公开 enum **新增变体是破坏性变更**：旧的穷尽匹配需要更新，旧边界白名单也会拒绝新 case。删改变体、改 payload 类型或 Go 表示同样需要兼容性评估。通配分支可以减少某些源码修改，但不能使所有下游或旧 ABI 自动兼容。编译器和 LSP 必须绑定同一版本的 enum 元数据，不把两个版本的变体集合混用。

首版不声称拥有 Rust 式全局所有权证明，不声称私有 marker 封闭了 Go 世界，也不声称入口校验能永久冻结 Go 可修改的对象。未知边界应明确记录，具体别名风险按 D19 warning；不因缺证明普遍要求 unsafe。类型与合法变体检查仍保留。

## 9. 实现验收条件

以下是未来实现条件，Go 示例编译成功只验证降低机制，不代表这些 hgo 规则已经实现：

本轮从本文提取 Go 示例，在 Go 1.27.1 的独立临时模块中通过六项行为测试：合法 Result 分支、外部非法 case、嵌套 Option、可空 Ptr、回调单次求值与返回值校验、共享字段再次读取的校验。另验证不同 Option 参数和 Result 的 E 参数不能因 marker 同形而混用，两个负例均得到预期的 Go 编译错误。

1. 普通 enum、Option、Result 的泛型身份准确；构造与 payload 类型不匹配在源码处诊断。
2. match 目标只求值一次，guard 和分支保持顺序；guard 不贡献穷尽覆盖，嵌套缺项与不可达分支有准确诊断。
3. 分支复制、引用限制、结果统一、return / break / continue 与 defer 作用域不因降低改变。
4. nil Result、typed nil、非 nil case 指针及接口嵌入伪造值均在业务副作用前拒绝；合法标量 case 通过。
5. None、Some(None)、Some(Some(value)) 与 Option[Ptr] 的 Some(nil) 可区分；嵌套非法 payload 被拒绝。
6. 数组、slice 创建与扩长、map 缺项、关闭 channel、泛型零值和部分 struct 初始化不能在 safe hgo 中暴露非法值。
7. Go 入口、外部回调返回、共享容器读取与变更后的再次读取遵循同一边界规则；验证不能被仅一次构造检查代替。
8. Go 消费者可构造和读取规范 case；原生 `(T, error)` 等协议只经明确桥接转换，生成示例可用 Go 1.27 独立编译。
9. 新增变体触发必要的源码 / ABI 兼容性检查；LSP 的补全、缺失分支与跳转使用同一 enum 元数据。


Option/Result 的专用 `?` 见 [RFC 0005](../rfcs/0005-option-result-question.md)：D22 确认限域与目标已知的显式 From 单步转换；精确语法与实现仍待完成，不公开通用 Try/Residual，不自动转换 Go 多返回。

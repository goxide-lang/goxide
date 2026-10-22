# RFC 0032：From / Into 与显式一步转换

## 状态与范围

**设计提案，尚未批准实施。** 本篇给出完整推荐方案：普通参数化 trait 定义转换，std 用普通 impl 提供 identity 与 From → Into，`?` 选择标准 From 的一个已确定实现。已有单步传播规则继续作为约束；新增的泛型 trait、impl 归属和 blanket 规则在本篇明确列出，不能把它们记为现有支持。

范围是转译器与 std。Error 的 `#[from]`、错误消息、source、backtrace 和上下文选择留给后续人体工程库；这些库只输出普通 impl。`TryFrom/TryInto`、隐式数值转换、转换链、通用 Try/Residual、特化、负 impl、GAT 和默认方法不在本篇实施范围。这里的 From 不采用 Rust move 语义，按 Goxide 既有传值、共享存储与引用权限工作。

### 与现有 RFC 的关系

| 现有合同 | 本方案 |
| --- | --- |
| D22 / [传播 RFC](research/0005-option-result-question.md)：可赋值优先，精确静态来源与目标，一步 From；不猜动态 error | 保留；`?` 的本包 helper 清单改为普通协议证据 |
| D22 的“不接受任意泛型 blanket 转换” | 需修订为只接受下述通用归属、无重叠和终止规则认可的显式泛型 impl；不开放隐式链或任意第三方兜底 |
| [RFC 0031](0031-generic-protocol-impls.md)：trait 无输入类型参数、impl 参数只出现在 Self、禁止裸参数目标 | 需扩展为参数化 trait 和完整实现头；允许满足归属规则的 blanket；关联类型仍是输出 |
| RFC 0031：trait 包或最外层具名 Self 包拥有有体 impl | 扩展到有序完整实现头，包含本地来源及指针链；避免本地 S → 外部 T 无法定义 From |
| RFC 0031：忽略 where 的保守重叠检查、定义点绑定、有限需求闭包 | 完整实现头先合一，再按 RFC 0031 的共享约束求解排除交集；泛型参数、未来跨包有体实现与原生 Go 前提均参与。未知仍拒绝，定义点绑定与有限闭包保留 |
| [RFC 0003](0003-declare-impl.md)：原生采用移除合同 | 已确认删除全部 declare impl；普通有体 impl、Go interface 与双向 API 调用保留 |
| [RFC 0020](0020-associated-functions.md)：无 self 固有成员在 carrier，enum 有 self 在接口和变体 | 保留；From/Into 是 trait 实现，不自动变成固有成员或修改原始 enum 的方法集 |
| [RFC 0019](0019-uniform-trait-wrappers.md)：运行时 trait 统一 wrapper | 保留；From 只有静态操作，Into 在目标已确定时可形成普通运行时视图 |
| [RFC 0030](0030-procedural-macros.md)：纯语法宏，普通输出再检查 | 保留；宏无需登记转换、查询类型或获得新权限 |
| std::prelude 后续提供常用名称 | 可重导出 From/Into；不影响实现身份、归属或 `?` 的选择 |

已确认完整删除 declare impl、从已知类型信息推导 trait 输入，并按包级开放世界检查；普通 Go 双向互调用保留。其完整规则及通用过程宏验收统一见 [RFC 0031 第 2 节](0031-generic-protocol-impls.md#2-通用互斥与实现选择设计提案)。其余新增语言合同仍为本篇设计提案，尚未实施。

推荐扩展通用 trait 能力，而非增加 `#[try_from]` 或 From⇒Into 编译器专用规则。后两者仍需处理多来源身份、跨包和冲突，却会留下独立转换登记路径。

## 源级规则

### 1. 标准协议与运行合同

以下为拟提供于 `std::convert` 的普通声明；完整 trait 路径以及静态限定调用是本篇候选语法：

```goxide
pub trait From[S any] {
    func from(value S) Self
}
pub trait Into[T any] {
    func into(self Self) T
}

impl[T any] From[T] for T {
    func from(value T) Self { return value }
}
impl[S any, T any] Into[T] for S where T: From[S] {
    func into(self Self) T {
        return T::from::<From[S]>(self)
    }
}
```

`From[S] for T` 表示从 S 得到 T；转换不要求 S/T 实现 Go error，也不自动建立反向 `From[T] for S`。std blanket 提供的 `Into[T] for S` 调用相同的已选 From；独立 Into 须通过下述互斥检查，它不反向提供 From。identity 按普通赋值返回值，不 clone、不解包接口，也不归一化 typed nil。

From 的签名没有失败分支，适用于无预期失败的转换；可失败解析或有多个业务含义的转换用具名函数返回 Result。编译器不证明函数不会 panic，也不把其 panic 捕获成 Err；panic、defer、Goexit 沿普通函数规则传播。From 本身不自动产生 Error/Unwrap，不判断一个业务转换是否“无损”。引用资格按普通参数和返回检查，不能因 Go 表示相同而擦掉只读权限。

上述 std blanket 的 Into 转发体是真实 std 源码调用层。`defer x.into::<Into[T]>()` 延期执行这个 Into，Into 再调用 From；From 中的直接 recover 因而不是直接延期调用。独立 Into 按自身业务体执行。编译器不得把 Into 无条件改成 From 别名，也不得为 From/Into 额外插入业务转发帧。

### 2. trait 输入、调用与多来源

trait 声明的位置类型参数是**输入**，必须参与实现选择；关联类型是已选 impl 的**输出**。例如 `impl From[IOError] for AppError` 与 `impl From[ParseError] for AppError` 可并存，不能用一个关联 `Source` 表达二者。

应用写作 `Q[A1,...,An, Item=U]`：位置输入在前，现有关联类型绑定在后；输入数量与声明一致，不增加默认输入或部分 trait application。每个输入先满足声明的普通类型/协议约束，关联输出则在选定 impl 后核对。`T::Q[A]::Item` 投影同时指明输入，`T:Q[A] + Q[B]` 保存两份独立义务；同一 application 的矛盾输出绑定仍诊断。模板与方法体中的 Self、输入参数按符号绑定替换，不按拼写识别 From。

候选限定调用扩展现有 `::<Trait>`：

```goxide
a := AppError::from::<From[*io::Error]>(io_error)
b := AppError::from::<From[*parse::Error]>(parse_error)
c := io_error.into::<Into[AppError]>()
make_io_error := AppError::from::<From[*io::Error]>
```

它补齐静态成员的限定表达式，并允许 trait 路径携带位置输入。未限定调用沿既有固有优先规则：静态 `AppError::from` 筛选可达的公开 trait，实例 `value.into()` 另要求 trait 在词法作用域。多个不同 trait 同名仍须限定；不通过逐个尝试 impl 来消歧。

已确认允许从实参及已有返回上下文推导输入。先确定唯一 trait 声明；显式限定固定输入，否则若定义点 ParamEnv 对该 Self/trait 已给出唯一完整 application，先使用该 application，再按普通参数/返回赋值规则检查调用。其余情况对声明签名引入输入变量，与接收者、实参静态类型及已知返回类型合一；取得完整 application 后才查询 impl。多个同一 trait 的已知 application 通过这些类型等式确定所需输入，不逐个尝试业务实现或可赋值转换。不能从已登记 impl 清单构造“唯一已知 application”：该优先规则只读取声明中的约束。

```goxide
a := AppError::from(io_error)              // From[*io::Error]
var b AppError = io_error.into()           // Into[AppError]
c := io_error.into::<Into[AppError]>()      // 显式限定始终可用
// 没有已知目标上下文或唯一完整约束时，io_error.into() 报输入未定。
```

因此 `func f[S Into[int]](s S)` 内的 `s.into()` 返回 int。若定义点只有 `T:From[error]`，`T::from(ptr)` 使用已知 From[error]，ptr 按普通参数规则可装箱；具体 AppError 未带该 ParamEnv 时，`AppError::from(ptr)` 按 ptr 的静态类型推导，不能为利用某个现有 From[error] 隐式改选。完整限定可明确这种来源选择。

不能从“目前只有一个实现”、where 能否证明、关联输出反向搜索或某条转换链猜出未知输入。返回上下文必须由显式标注、已知形参/返回签名等先确定；无类型常量沿普通定型规则处理，不为通过某个 impl 改定类型。若推导结果未定或多个不同 trait 同名，诊断要求补全限定/目标类型；调用者也可使用普通 helper。保存方法值同样可用完整限定或已知函数类型上下文推导。`?` 的来源 E 来自操作数 Result[A,E]，目标 F 来自最近函数/闭包的 Result[B,F]，继续按第 5 节精确查询 F:From[E]，不套用普通调用的 ParamEnv 输入固定规则。

std 可提供普通函数形式，避免调用方反复写完整限定：

```goxide
pub func from[T any, S any](value S) T where T: From[S] {
    return T::from::<From[S]>(value)
}
pub func into[T any, S any](value S) T where S: Into[T] {
    return value.into::<Into[T]>()
}
// 完整写法：std::convert::from[AppError, *io::Error](io_error)
// 按普通泛型参数推导可省略可由 value 确定的尾部 S；T 仍显式给出。
```

显式选择 `From[error]` 后，实参按普通函数参数规则可装箱为 error；这与 `?` 的精确来源匹配不同。无类型常量同样遵守普通参数定型规则。没有 From 的普通赋值、参数传递或 return 不会自动调用转换。

trait 的方法不增加独立类型参数；本篇仅扩展 trait 输入。类型和 `const N int` impl 参数必须能从完整实现头的结构位置绑定，不能仅藏在 where、关联输出或方法体里。现有固定数组长度规则继续适用，不增加 const 算术求解。

### 3. 实现归属与跨包可见性

对有体 `impl[P...] Q[A1,...,An] for T0`，先规范化透明 alias，令有序实现头为 `H = (T0, A1, ..., An)`：

1. 若 Q 在当前 **Go/Goxide package** 定义，允许该包声明满足其余规则的 impl，包括裸参数 blanket。
2. 否则，在 H 中找第一个本地具名类型 `Hi`。在 `H0..H(i-1)` 中不得出现未被类型构造覆盖的 impl 参数。没有本地类型则拒绝。
3. owner 判断只允许穿过普通 `*` 指针链：`*Local`、`**Local` 视作本地；`*P` 不覆盖参数 P。`Local[P]` 的名义构造覆盖 P。这个规则只用于归属，不把 `*E` 与 E 视为相同类型。
4. slice、map、array、chan、func、匿名 struct/interface 不因内部包含 Local 而成为本地类型；它们作为类型构造可覆盖其中的参数。透明 alias 不创建 owner；新 defined type 创建独立身份。实现归属不等于 Go 原始类型能够承载 receiver，实际业务可放 impl 包的 ops/wrapper。

因此同一 module 的相邻 package 也不自动拥有对方类型。以下在 Local 的定义包：

| 实现头 | 归属结果 |
| --- | --- |
| `From[foreign::S] for Local` | 允许，Self 本地 |
| `From[Local] for foreign::T` | 允许，来源本地，前面的目标无未覆盖参数 |
| `From[*Local] for error` / `From[error] for *Local` | 允许；指针 owner 规则不改变精确匹配 |
| `impl[P] From[Local[P]] for []P` | 允许，前面的 P 被 slice 构造覆盖 |
| `impl[P] From[Local[P]] for P` | 拒绝，裸 P 出现在首个本地类型之前 |
| `From[[]Local] for foreign::T` | 拒绝，嵌套元素不授予 owner |
| `From[foreign::S] for foreign::T` | 第三方业务包拒绝；用本地 newtype 或显式函数 |

这些只是 owner 判定，随后还要检查完整实现头重叠、签名和方法体。不能因为拥有 S 或 T 就覆盖别人已有的同一转换。

转换实现来自本次目标的已选、可达 Goxide 源码包图；cfg 选择先完成，再收集声明。所有这些包的实现统一做冲突检查，不根据当前文件的 use 顺序、距离或“本包优先”选择。没有可达边的依赖不因仅列在清单里就自动加载。`pub use` 和依赖别名复用 canonical 声明身份，不新增候选。

实现存在性与名称可见性分开：没有 private/pub 两套可覆盖的 impl 表；公开 trait 对可用类型的实现可跨包成为证据。直接书写类型或 trait 路径仍须有可见名称；通过推导得到的私有类型可沿现有规则使用其公开协议，生成器不得泄漏私有字段或把业务体搬到无权限的包。普通未限定 `.into()` 仍需 Into 名称在作用域，限定路径可直接使用；`?` 不要求显式 use From。

只有 Go 产物的依赖提供现有 Go API，不凭方法名扫描出新 Goxide impl。Goxide 源依赖按既有 P5/RFC 0031 恢复声明和条件模板；不新增 enum 元数据或运行时转换注册表。

### 4. 通用重叠与转换协议

所有协议共用 [RFC 0031 第 2 节](0031-generic-protocol-impls.md#2-通用互斥与实现选择设计提案) 的符号约束、开放世界、原生采用移除、证书及选择规则，本篇不再维护只覆盖具体类型的负证明子集。

完整头为 (canonical Q, norm(Self), norm(inputs...))；where 与关联输出不变成协议输入，但不同条件声明使用不同 DeclID。头合一后，对两边参数与全部前提求同一个交集。Impossible 才允许不相容声明并存，Possible 给出冲突见证，Unknown 报告未证明互斥；不根据具体程度、源码/包顺序或“本包优先”选实现。重复有体 impl 不合并，公开 wrapper 与工厂身份仍固定。

转换协议按这些普通规则得到：

| 声明组合 | 结论及依据 |
| --- | --- |
| 独立 Into[Target] for Source，与 std blanket | 交点要求 Target:From[Source]；在开放环境下可证明其不存在时允许，不要求 Self/输入 ground |
| 泛型 Into[[]T] for Wrap[T]，位于 Wrap 包 | 必须能在整个 T 域排除 []T:From[Wrap[T]]；固定名义根关闭下游 owner，上游 blanket 属需重检的扩展 |
| 独立 Into 后又加入对应适用 From | 两边有真实交集，报冲突；不能自动换用 blanket |
| From[T] for T 与自定义同类型转换 | identity 提供真实见证，不能覆盖 |
| From[[]S] for []T，或 From[Box[S]] for Box[T]，条件为 T:From[S] | S=T 时由 identity 满足，通用同容器逐元素转换冲突；使用普通 map 函数 |
| 不同外层容器，或具体不同元素的同容器转换 | 按实际头及联合前提检查，不统一禁止容器转换 |
| From[T] for Box[T] 与 identity | 合一要求 T=Box[T]，occurs check 排除 |
| From[S] for AppError，条件为 S:SomeTrait | 检查交点 AppError:SomeTrait；可能存在合法未来有体 impl 时不能仅凭当前缺实现否定 |

From 与 Into 都通过普通有体 impl 实现；同名原生方法和 Go marker 不创建转换关系。完整移除 declare 后，普通实例协议和静态协议采用相同的 owner/开放世界证明，不再有第三方原生采用例外。

仍推荐优先实现 From，使显式 Into 和 `?` 可使用同一转换；仅 Into 是普通独立实现，不自动产生 From。Rust 的 [implicit negative impl check](https://rustc-dev-guide.rust-lang.org/coherence.html#the-implicit-negative-impl-check) 与 [Into 文档](https://doc.rust-lang.org/std/convert/trait.Into.html) 可供比较；具体判定仍按 Goxide 的 owner、Go 约束和开放环境合同。

### 5. 已选证据与 `?`

转换调用使用经过全图 coherence 认证的正证据。某一候选已有正证据且与其他候选的互斥已认证，即可排除后者；后者单独查询仍为 Unknown 不会推翻该证书。泛型 ParamEnv 保留约束证据及定义点选择，不将普通证明失败解释为“转试下一个实现”。逻辑递归、关联输出依赖与业务调用图分别按 RFC 0031 处理。

所有查询、投影、成员、缓存、需求队列及绑定器包含完整 trait application 与约束环境；证据另保存 ImplDeclID、代换与前提证据。Count:From[[]int]、Count:From[[]string] 以及同头互斥条件声明不能误合并。负缓存绑定声明/原生方法环境、cfg 和开放扩展依据；依赖升级或增加实现必须重新认证。

对 `Result[A,E]` 上的 `?`，最近用户函数或闭包必须返回 `Result[B,F]`：

```text
E 按普通 Go/Goxide 赋值及权限规则可赋给 F
──────────────────────────────────────────────── [TryAssign]
Err(e) → return Err(e)             // 不查询 From，保留接口装箱行为

E 不可直接赋给 F，Γ ⊢ F:std::convert::From[E] ⇓ w
──────────────────────────────────────────────── [TryFrom]
Err(e) → return Err(call(w.from,e)) // 精确 E/F，一次普通调用
```

具体步骤：

1. operand 按原顺序求值一次；仍执行既有 Result 有效性检查。
2. Ok 只取成功值，继续原函数；不调用转换。
3. Err 先用 `TryAssign`；不满足时只查询精确静态 `F: From[E]`，只在失败分支调用一次。
4. 不查询 Into、不搜索 E→X→F、不先把 E 装箱成 error/any 来凑来源、不按动态类型选择变体，也不自动解引用/借用。无证据或证明失败时源级诊断，列出 E、F 与失败的义务。
5. 转换后立即返回外层 Err，保持已登记 defer、最近闭包、短路和 typed nil。普通转换 panic 不被改写为 Result。Option 上的 `?` 仍只传播 None，不经 From 跨到 Result。

泛型函数可声明 `where F: std::convert::From[E]`；若只知道该前提，就按它生成转换，而不是在未来具体化时悄悄换成可赋值路径。若声明环境已证明可赋值，则优先直接赋值。实例化只代入已检查的选择，保留泛型函数体的语义；不因后续 E/F 恰好相容而跳过已选业务转换。

`?` 识别所选 std 依赖中 From 声明的 canonical 身份。别名、重导出和未来 prelude 不改变它；用户同名 From 不获得传播资格。缺少该标准声明且需要转换时明确诊断，不回到 helper 名称扫描。

## 转译示例

以下是推荐 Go 形态，不是当前编译器产物。示例名省略现有导出规划中必要的 canonical 身份后缀；正式命名须包含完整 trait 输入，保持可读前缀、确定性及冲突检查，不依赖 import 别名或遍历顺序。

### CV-01：一个目标、两个来源

源包 domain 定义 AppError；来源来自 native 包。普通 From impl 不要求 Error 派生：

```goxide
pub enum AppError { IO(*native::IOError), Parse(*native::ParseError) }
impl From[*native::IOError] for AppError {
    func from(value *native::IOError) Self { return AppError::IO(value) }
}
impl From[*native::ParseError] for AppError {
    func from(value *native::ParseError) Self { return AppError::Parse(value) }
}
```

对应 domain 包 Go 声明片段；enum 类型和构造按现有表示生成：

```go
type AppErrorFromIOErrorOps struct{}
func (AppErrorFromIOErrorOps) From(value *native.IOError) AppError {
    return &IO{Cause: value}
}
type AppErrorFromParseErrorOps struct{}
func (AppErrorFromParseErrorOps) From(value *native.ParseError) AppError {
    return &Parse{Cause: value}
}
```

每个完整参数化 impl 使用独立零字段 ops，避免在同一个 carrier 上声明两个同名 From。原始业务体直接位于方法，没有“友好名 → 内部方法”的额外转发。Go 的方法名须在同一 receiver 上唯一，且不能给外包类型或 interface 声明 receiver，见 [Go 方法规范](https://go.dev/ref/spec#Method_declarations)。此布局只使用普通 Go 类型和方法，不依赖为未来 S 动态寻找转换。

Into 属于 trait 实例业务，继续走 wrapper/静态已选操作；不会为每个目标向原始 S 追加一个同名 `Into()`。新增第二个来源或目标不重命名已有转换的 canonical 入口。

### CV-02：`?` 在原函数中调用选定 From

```goxide
func load() Result[int, AppError] {
    value := read_native()?
    return Result[int, AppError]::Ok(value + 1)
}
```

假设 `read_native` 返回 `Result[int,*native::IOError]`。对应 Go 函数使用当前发布的 std/sum 表示，不修改 std/sum ABI：

```go
func Load() sum.Result[int, AppError] {
    result := ReadNative()
    switch sum.TagResult(result) {
    case 1:
        value := sum.ValueOk(result)
        return sum.NewOk[int, AppError](value + 1)
    case 2:
        converted := (AppErrorFromIOErrorOps{}).From(sum.ValueErr(result))
        return sum.NewErr[int, AppError](converted)
    default:
        panic("goxide: invalid Result at ?")
    }
}
```

没有包裹原调用的 IIFE、反射、运行时转换表或 error 动态分类。若目标是 error 且来源为实现 error 的指针，失败分支直接装箱原指针；typed nil 仍是非 nil 的 error 接口。显式的 `From[*native::IOError] for error` 不覆盖 `?` 的普通可赋值优先级。

### CV-03：条件泛型、私有类型与来源包生成

```goxide
impl[T any] From[T] for Box[T] {
    func from(value T) Self { return Box[T]{Value: value} }
}
func convert[T any, S any](value S) T where T: From[S] {
    return T::from::<From[S]>(value)
}
```

Go 能表达的泛型载体继续保留：

```go
type BoxFromValueOps[T any] struct{}
func (BoxFromValueOps[T]) From(value T) Box[T] {
    return Box[T]{Value: value}
}

// 需要定义点业务体和精确回调时，沿 RFC 0031 的模板绑定路径。
func ConvertFunc[S, T any](from func(S) T) func(S) T {
    return func(value S) T { return from(value) }
}
```

`ConvertFunc` 中闭包就是源 convert 的业务函数体，外层工厂只绑定证据；不是再包一层去转发某个已经存在的 convert。已选 From 的原生方法值直接作为 `func(S) T`。来源或目标是跨包私有类型时，可由值与回调签名推导：

```go
makeSecret := conversion.ConvertFunc(model.SecretFromIntOps{}.From)
readSecret := conversion.ConvertFunc(model.IntFromSecretOps{}.From)
hidden := makeSecret(42)
result := readSecret(hidden) // 42；调用包不需要拼写 model.secret
```

生成定义归 impl/函数的定义包，不把私有字段业务复制到消费者；std 的通用 Into 模板接收已选操作，不能反向 import 业务包。按需实例只写受管生成产物，不修改依赖 checkout 或模块缓存。条件性方法不进入所有 `Box[T]` 的 Go 方法集。

### CV-04：明确请求的 Into 运行时视图

`From[S]` 仅有静态成员，不能存成运行时值。固定目标 T 的 `Into[T]` 可按普通 trait 资格形成值视图，捕获来源 S 的值副本；直接源码调用默认静态分派，不自动装箱。

对于 std blanket 提供的 Into，跨包模板需要回调时，可用以下完整 Go 形状承载已选证据；独立 Into 按其自身 impl 业务体生成视图，不要求 From 回调：

```go
package conversion

type Into[T any] interface {
    Into() T
    GoxideTraitIntoWitness(T)
}
type intoView[S, T any] struct {
    value S
    from func(S) T
}
func (v intoView[S, T]) Into() T { return v.from(v.value) }
func (intoView[S, T]) GoxideTraitIntoWitness(T) {}
func AsInto[S, T any](value S, from func(S) T) Into[T] {
    return intoView[S, T]{value: value, from: from}
}
```

回调在构造时已经选定，调用时没有协议搜索；这沿用 RFC 0031 的精确签名证据，不是任意 `any` 字典。运行时视图及保存方法值按需捕获来源和已选证据；普通直接调用不必因此构造接口值。marker 采用完整协议身份，示例仅展示类型关系。方法值绑定一次，之后每次调用读取自己的值副本；值内的指针仍遵守原共享规则，不自动 clone。

## 拒绝与边界

| 编号 | 输入 | 判定 |
| --- | --- | --- |
| CV-N01 | 同一 canonical S/T 定义两个无条件 From，或通过 alias 重复 | Goxide 重叠诊断，指出两处定义 |
| CV-N02 | 同一头条件的交集有见证或无法证明互斥，或靠文件顺序决定优先级 | 分别报告实际冲突或 Unknown；不特化 |
| CV-N03 | 已有适用 From 再独立实现对应 Into，或覆盖 identity | 真实交集，与 std impl 冲突 |
| CV-N04 | 第三方包为两个外部类型写 From | owner 诊断；使用 newtype 或具名函数 |
| CV-N05 | `impl[P] From[Local[P]] for P` | owner 之前有未覆盖 P；不能靠来源中的 Local 抢占所有目标 |
| CV-N06 | impl 参数仅出现在 where/关联输出/方法体 | 未受实现头约束；拒绝搜索未知类型实参 |
| CV-N07 | 多个不同 trait 同名，或根据实参/已知返回上下文仍无法确定输入 | 成员歧义或输入未定；要求限定，不尝试 impl 重载 |
| CV-N08 | 只有 `From[A] for B`、`From[B] for C`，却传播 A 到 C | 缺 `C:From[A]`，不搜索两跳 |
| CV-N09 | 静态 `error` 试图选择 `From[*IOError]` | 来源不匹配，不检查动态类型 |
| CV-N10 | 静态 `*IOError` 只有 `From[error] for AppError` | 不先装箱再查 From；调用者可显式选择/装箱 |
| CV-N11 | 泛型体声明 `S:Into[T]`，却需要按 `?` 传播到 T | Into 不能反向证明 From；须声明 `T:From[S]` |
| CV-N12 | From 自身返回 Result 或 `(T,error)`，却声明为 `From[S] for T` | 普通签名错误；可失败转换使用具名函数 |
| CV-N13 | 前提形成 `A:From[B] → ... → A:From[B]` 或无限类型增长 | 普通协议证明/需求闭包诊断 |
| CV-N14 | 为 Into 视图向外部来源类型添加 receiver，或给 E 接口加 From receiver | 非法 Go 生成；应改用 impl 包的 ops/wrapper |
| CV-N15 | 请求 From 时消除 readonly 权限，或因 pointer-owner 规则自动解引用 | 普通权限/类型诊断；owner 不授予转换或写权限 |

From 不是编译器保留方法名，普通 `impl T { func from... }`、普通 Go 函数或同名自定义 trait 均不创建标准转换关系。仅实现 Into 的类型可满足 Into 约束，但不能用于要求 From 的 `?`。

## 双向互操作

Goxide → Go：原生 `(T,error)` 保持原签名，仍由已定 `go_result!` 适配；本篇不把任意双返回自动变为 Result。原生 struct、pointer、error 可成为精确 S/T，但原生方法名不会自动登记 From。外部类型的方法集保持不变。

Go → Goxide：具体已生成 From 操作可直接调用，或使用作者显式导出的普通业务函数，例如 `WrapIO(*native.IOError) AppError`。后一入口是作者写出的函数，不是编译器重新生成固有成员包函数。条件泛型能力沿 D38/RFC 0031 由这些明确入口确定需求；纯 Go 的新组合不会自动触发 Goxide 选择和实例化，也不要求调用者手写内部哈希或 Ops 证据。

内部跨包模板可以暴露精确 `func(S) T` 绑定 helper；它解决私有类型和包依赖方向，不作为对所有 Go 使用者的强制 API。Go 可手写相同方法集，但不会由此凭空创建 Goxide impl；语言增强依据源声明恢复。

兼容目标是 `goxide/go.mod` 的 Go 1.27。新布局保留源类型身份、具体签名、typed nil、调用帧和包边界；不保持旧生成 helper 的 ABI，pre-1.0 随编译器与 std 一起重生成，不增加旧登记表回退或兼容包装。

Rust 的 [From](https://doc.rust-lang.org/std/convert/trait.From.html) / [Into](https://doc.rust-lang.org/std/convert/trait.Into.html) 同样采用标准库 blanket 联系两个方向；本篇参考其输入/目标关系及 [orphan 规则](https://doc.rust-lang.org/reference/items/implementations.html#trait-implementation-coherence)。Goxide 保留普通赋值优先、没有 move，并对普通有体 impl 使用统一符号证明；不以 Rust 某个具体接受用例替代本语言的完整合同。

## 验收与未定项

### 实施顺序与完成条件

1. 参数化 trait：解析、解析后身份、约束和关联投影支持完整输入；一个目标同时要求多个 From，私有类型和 alias 不混淆。
2. 通用 impl 归属与重叠：完成 RFC 0031 的 GC-01–14，包括非 ground 互斥、共享约束、包级开放世界、原生采用移除边界与独立 Into；补 From 的冲突、同容器真实交集及身份/顺序检查不能只用具体类型替代。
3. 证明与降低：所有缓存/实例/成员/绑定键包含完整 application；pair ops、泛型模板与精确回调保持定义包、函数帧和确定性。
4. std 声明接入：From、Into、identity 与普通转发 impl；具体与条件实例分别验证，Error 派生库不作为前提。
5. `?` 改为选定普通证据，删除 Error 专用转换登记。用手写普通 impl 证明旧传播行为后，再调整现有内建 Error 的过渡输出；不保留两套选择规则。

实施验收必须真正编译 `.gox`、生成 Go 并跨模块 build/run/test，覆盖 CV-01–04 与所有拒绝例、重复生成字节一致。另含泛型声明环境已选择 From 后的实例化语义、Into 与 From 的延期 recover 差别、转换 panic/Goexit/defer、nil/typed-nil、私有来源与目标、跨包实现和重导出、无 Goxide 调用的具体 Go 消费、source order 与依赖顺序重排。

既有手写探针已验证推荐 Go 机制：Go 1.27.1 下，两独立 module、多包 ops/运行时视图/泛型绑定、已发布 std/sum pin 的手写传播形状，实际 build/run/test/vet 通过；测试覆盖多来源、原 cause 身份、typed nil、跨包私有 S/T、identity、泛型 carrier、失败分支单次转换、defer 及 From/Into 的 recover 帧差别。探针中的 Error/Unwrap 是手写方法，只核对 cause 身份，不代表 enum 新固有映射已验收。这是候选表示的可行性证据，尚未验证 Goxide 前端实现上述语法或 coherence。

完整移除 declare impl、保留普通 Go 双向互调用、类型上下文输入推导及包级开放世界已确认；其余待整体确认的语义改动是参数化 trait、完整头与有序 owner/指针规则、普通 identity/Into blanket，以及泛型声明环境中的固定转换选择。通用互斥规则按 RFC 0031 设计及验收，不保留具体类型首批门槛。实现前同步 RFC 0031、D22/传播规范、trait/原生互调用合同及前端 AST。生成名的可读前缀可沿现有规划收敛，但不能遗漏完整身份；源码行号、private callback 的具体实现沿现有机制选择。

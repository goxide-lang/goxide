# RFC 0031：普通库协议的泛型 impl 与静态证据

## 状态与范围

**Draft，待用户确认，前端尚未实现。** 本篇把过程宏输出的普通 impl 接入同一套语言能力，使 Default、Clone、Debug、PartialEq、Eq、Serialize、Deserialize 的业务逻辑可以由库和宏实现。

现有 [RFC 0030](0030-procedural-macros.md) 只提供语法变换与受限宏执行；它没有授予宏类型查询、impl 登记或绕过检查的能力。当前 [trait 合同](../design/traits.md) 仍以具体具名目标、单 trait 约束和类型 carrier 为基础；业务派生另有专用检查与降低路径。仅把派生源码移到宏包，不能消除这些路径。

本篇提议一起确认以下边界：

| 待确认项 | 候选规则及影响 |
| --- | --- |
| 泛型 impl 与多约束 | 支持带类型参数的 impl、同一类型的多个 trait 约束、具体类型的 where 条件；库可表达容器协议及条件实现 |
| impl 归属 | 有体 impl 可由 trait 定义包或最外层具名目标的定义包提供；原始类型及未命名容器只能由 trait 定义包提供 |
| 唯一性 | 可能匹配同一目标的有体 impl 一律冲突，不以约束、优先级或特化区分 |
| 普通协议前提 | 非泛型 trait 可声明 `where Self: Other` 前提，使 Eq 对 PartialEq 的要求成为普通规则；不开放泛型 trait、GAT 或默认方法 |
| 派生入口 | 普通 trait 与宏沿现有单一名字空间，使用库的显式宏子路径，不新增同名双登记或固定内建名称映射 |
| 静态证据与 Go API | 每个选定 impl 使用独立零大小 Ops；原生 Go 调用协议泛型函数可能须补充 Ops 类型实参，修订 D37 的同数量承诺 |
| 数组长度 | 开放最小 `const N int` 参数及跨包按长度实例化；Go 输出仍只使用普通类型参数和具体数组长度 |
| 递归 | 无条件具体 impl 的普通方法体允许自身及相互递归；显式 impl 前提的证明循环仍拒绝，不把两类递归混为一谈 |
| enum 可失败检查 | 增加显式 `enum_valid(value)` 检查候选，非法外层 enum 返回 false；普通 match 的 nil panic 与 `_` 规则不变 |

[已确认决策](../design/decisions.md) D05 的外部原生采用、D19/D20 的权限边界、运行时 wrapper 和 enum 表示继续适用。此稿不增加 move、独占分析、动态字典、反射兜底或每种 derive 的编译器 hook。用户在泛型类型上使用 derive 可继续由宏明确拒绝；但库的泛型 impl 必须能为具体业务类型的泛型字段提供协议。

Error 派生、`go_result!` / `go_result_keep!`、`cfg` 属于不同边界：Error 涉及原生 `error` 适配，go_result 涉及调用结果形状和错误转换，cfg 决定进入展开的源码。本篇不据此宣称它们均可迁出编译器。print/println 的库化也须另验格式检查与调用行为；本篇仅提供其可使用的普通协议能力。

## 源级规则

### 1. 声明、归属与约束

候选语法沿用已有 `impl[T ...]`、`where` 形状，增加 `+` 组合和常量参数：

```goxide
impl[T Clone] Clone for []T { /* 必需成员 */ }
impl[T any] Clone for Box[T] where T: Clone { /* 必需成员 */ }
impl[T any] Decode for Box[T] where T: Deserialize + Default { /* 必需成员 */ }
impl[T Clone, const N int] Clone for [N]T { /* 必需成员 */ }
```

第一个和第四个 impl 位于 Clone 的定义包；第二个也可位于 Box 的定义包。`Box[T]` 自身可保持 `T any`，协议条件不回写成类型声明的约束。where 左侧允许实际字段类型，如 `[]Label`，从而让过程宏只依据语法输出条件，再交给普通类型检查证明。泛型 impl 的每个参数必须出现在目标模式中；不开放 `impl[T P] Q for T` 这种裸类型参数 blanket impl。

归属以 canonical 包与类型身份判断，透明 alias 不创建新 owner，新 defined 类型是独立目标。`[]Local`、`map[string]Local` 和 `[3]Local` 的元素属于本包，不使这些未命名容器成为本包所有。traits 不带位置类型参数；方法不引入独立泛型参数，关联类型仍是 impl 的输出。

这不引入 underlying-type 自动采用：`type Score = int` 可使用 int 的实现，`type Score int` 不因此取得任何协议。当前 Default/Clone/Debug 的字段检查也先要求 defined 类型有显式实现，serde 只展开 alias、不展开 defined 类型的 underlying；内部生成函数对已登记根类型的解包不是普通字段兜底。迁移须保留这一差别，不开放裸 T blanket 或按派生名称补证据。为 Score 新写普通显式 impl 属于本篇允许的库/用户定义能力，不是假定旧代码已有该实现。

同一目标可同时要求多个 trait；关联类型绑定仍按所属 trait 区分。成员查询保持现有权限候选和固有成员优先规则，同名 trait 成员须限定，不按约束书写次序选择。每个约束都须有显式 impl，不能凭 Go 方法集补足。

Eq 的关系用普通 trait 前提表达，候选语法如下：

```goxide
pub trait PartialEq { func equals(self Self, other Self) bool }
pub trait Eq where Self: PartialEq {}
```

证明 `T: Eq` 时先证明其前提；在 `T: Eq` 的函数内可使用 PartialEq。只开放无环的非泛型 trait 前提，不增加默认方法、运行时自动视图转换或任意继承机制。PartialEq 继续使用两个值参数，不借迁移暗中改成只读借用。

若保留“派生 Eq 只接受结构派生 PartialEq”的区别，可由普通库 marker `StructuralPartialEq` 表达：PartialEq 宏同时生成该 marker 的 impl，Eq 宏要求 `Self: StructuralPartialEq`。手写 PartialEq 本身不产生 marker。marker 与手写 Eq 一样是作者承诺，用户也可显式实现；它不是编译器签发的不可伪造证明。接受此规则包含接受这种显式承诺入口；不能为隐藏它重新增加 derive 名称判断。

Default 成员统一写作 `r#default`，使用普通 raw identifier；不再以 trait 名称触发关键字解析例外。静态函数返回真实 Self；Clone、Debug、Serialize 的 `self &Self` 保持既有只读权限；Deserialize 返回 `(Self, error)`，不抹成 `any`。

trait 与派生宏沿 RFC 0030 的单一名字空间：运行库可导出 `Clone` trait，并将宏包重导出为 `derive`，调用写作 `#[derive(protocols::derive::Clone)]`；也可显式导入宏并改名。现有裸内建派生入口在迁移时删除，不用编译器固定映射模拟普通库。宏输出的运行库路径通过既定定义点绑定与 RuntimeRoot 解析，消费者按 RFC 0030 声明运行库依赖；宏执行依赖不会自动成为目标依赖。这里的模块及导出名是示例，最终库布局须在迁移前确定。

### 2. 唯一性与证明模型

输入为展开后声明集合 `D`、canonical 类型/包环境 `Γ` 与有效配置 `C`；输出为带已选实现和证据的类型化程序，或源位置诊断。宏产生的声明与手写声明属于同一个 `D`。

定义 `I = (owner, trait, target-pattern, parameters, prerequisites, body)`。`norm` 解析 alias 和路径身份；`θ` 是目标模式匹配得到的类型及数组长度代换；`P ⊢ τ:Q ⇓ w` 表示在当前证明栈 `P` 下，为目标 τ 的协议 Q 选中静态证据 w。

```text
Owner(I) = owner(trait(I)) 或 owner(outermost-named(target(I)))
全部参数出现在 target(I) 中，成员及关联类型在 prerequisites(I) 下可检查
────────────────────────────────────────────────────────────── [Impl合法]
                       Γ ⊢ I valid

trait(I) = trait(J)，存在 θ、φ 使 norm(θ target(I)) = norm(φ target(J))
I、J 是两个不同有体实现（不考察各自 prerequisites）
────────────────────────────────────────────────────────────── [重叠拒绝]
                       Γ ⊢ I, J conflict

match(norm(target(I)), norm(τ)) = θ，I 是唯一匹配实现
(τ,Q) 不在 P，每个 θ prerequisite(I) 均在 P ∪ {(τ,Q)} 下可证明
────────────────────────────────────────────────────────────── [实现选择]
            P ⊢ τ:Q ⇓ Ops(I, θ, prerequisite-witnesses)
```

这些规则是设计约束，不是已完成的形式证明。删除 owner 条件会让任意依赖定义外部类型的业务；删除重叠检查会让依赖集合或顺序改变分派；用“当前约束不相交”规避重叠，会在以后增加 impl 时改变唯一性。本稿采用保守的目标模式合一检查，不求解约束互斥性。

`declare impl` 继续采用真实原生方法，保留 [RFC 0003](0003-declare-impl.md) 允许第三方包采用外部 trait/类型和相容声明合并的规则。该入口不能因新的有体 impl owner 规则被删除。相同采用关系可合并；有体与 declare 的匹配目标重叠仍诊断，不用“有体优先”覆盖已有行为。首批不要求泛型 declare；现有具体 declare 必须仍能参与泛型函数的证据传递。

证明缓存按 canonical 目标、trait、关联绑定和代换区分。命中正在证明的同一义务报告完整链；不默认假设递归协议成立。无匹配、重叠、关联类型冲突或证明循环均在生成成功产物前诊断。

普通函数体调用不进入上述 impl 证明栈。手写 `Default::r#default` 可经普通递归调用返回，也可能无限递归，语言不承诺终止。移除当前仅针对 Default 的函数调用图拒绝，是本稿明确提出的语义变化。相反，宏若显式生成递归字段前提，形成 `Node: Clone → []Node: Clone → Node: Clone`，仍是证明循环；本稿不以共同递归证明放行。

具体、无前提的 impl 则先登记其完整签名，再检查普通方法体；所有实现体通过后才可生成成功产物。检查方法体内的调用时，可以选择该无条件实现自身或同批登记的其他具体实现。以 `Node { Next *Node }` 为例，serde 宏可生成无 where 前提的具体 Serialize/Deserialize impl：方法体选择 `*Node` 容器证据，后者选择已登记的无条件 Node 证据。其 Go 形式是具名 `NodeOps` 的方法调用 `PointerOps[Node, NodeOps]`，没有无限展开的证据类型；互相递归的具名业务类型按同一规则处理。方法体仍逐项检查字段协议，未知 codec 不能因提前登记而被放行。

这与带有 `where *Node: Serialize` 的 Node impl 不同：后者的资格本身依赖尚待证明的同一资格，仍被拒绝。现有 serde 生成器可通过直接 helper 调用处理递归字段，迁移不能机械地给每个具体字段增加 where 而失去此能力。当前 Clone 的递归拒绝可由其宏生成显式字段前提保留；不要求所有业务宏选择同一前提策略。两种递归都不承诺运行时终止，也不替循环对象图增加检测。

### 3. 统一静态证据与运行时视图

每个 impl 的业务静态入口由一个独立、字段为空的 Ops 类型承载。带约束的 impl，其 Ops 类型以目标类型参数及所需子协议 Ops 作为 Go 类型参数。具体调用直接使用已选 Ops；泛型函数增加必要的证据类型参数，函数体取其零值并调用方法，不传运行时字典值。

不再要求给目标 T 增加 `GoxideInternalTypeCarrierForSelf` 一类锚方法来取得静态资格；否则 `int`、`[]T` 和条件性的 `Box[T]` 仍不能统一表达。所有目标采用同一条证据路径，不保留一套仅供旧具体类型使用的兼容后端。既有 `typeinfo.Of`/carrier 的生成契约须在实施时一并修订；本 Draft 不授权改写 `std` 或发布契约。

生成方法名按 canonical trait/成员身份区分，两个同名 trait 不共用操作名。下文为可读性使用 `Clone` 等短名；正式命名沿编译器公开符号规则确定，不按依赖遍历顺序编号。Ops 约束可包含 `~struct{}` 和精确方法签名；这些 interface 只充当 Go 类型约束，不作为运行时值传递。关联类型继续通过 Ops 的类型见证参与 Go 类型推导，见证不执行。

显式转换为可运行时表示的 trait 仍使用已有 wrapper、marker 和固定包级工厂，保持 receiver 的值复制或借用身份。静态 Self 返回值不强迫 trait 获得运行时接口。生成 Go 中的 `*T` 仍只是引用表示，源级只读检查由普通 checker 执行；Ops 不赋予额外权限。

### 4. 最小数组长度参数与跨包实例化

`const N int` 首批只服务固定长度数组和原样转传的长度参数：N 从实际 `[n]T` 的长度匹配，也可由显式非负整数常量提供。N 在实例化后是普通编译期整数。首批不接受负长度、非常量实参、`[N+1]` 模式求解、改变 N 的递归实例化或任意类型的 const 参数；不开放 const 泛型具名类型。

Go 不能直接承载此处的长度参数，因此编译器对 N 生成普通 Go 声明，例如 `ArrayCloneOps3[T, TOps]`。类型 T 及子协议证据仍保持 Go 泛型，不按每个具体元素类型复制函数。零长度数组同样合法。

跨包构建先收集可达的具体长度实例需求，再以确定性工作队列生成定义包的派生声明。实例键至少包含 canonical impl、按参数顺序排列的常量值及其定义源码身份；同键去重，依赖顺序不影响命名或内容。超过统一编译资源预算时诊断，不输出部分成功包。

实例文件位于本次构建 staging 中的 impl 定义包，定义点绑定和私有 helper 访问保持原样，不写入原依赖 checkout 或模块缓存。T 保持 Go 泛型，使定义包无需导入消费者的私有元素类型，避免反向依赖。宏展开已结束的普通 impl 模板进入该过程；不能在实例化时重新用消费者环境解析宏自由名字。

Goxide 构建必须取得该依赖的源声明与实现模板，只有已生成 `.go` 的依赖不足以追加任意新长度。原生 Go 调用方可以使用已生成的具体长度 API；需要新的长度时须由 Goxide 源构建生成相应实例。本稿不承诺纯 Go 发布包能导出 Go 本身不能表达的任意长度泛型入口。

### 5. serde 迁移所需的普通表达性

仅有 Serialize/Deserialize 方法签名还不足以保持现有行为。本节包含待确认的通用 enum 检查，以及迁移前必须落实的普通库协议；不把字段分派搬成编译器隐式 hook。

**显式 enum 检查候选。** 暂用内建调用 `enum_valid(value)` 表示，名字沿普通内建的解析和遮蔽规则。输入的静态类型须为具体 enum 或已知 enum 的泛型实例，透明 alias 同样适用；不接受任意 `T any`、接口或自动解引用的指针。参数只求值一次，结果为 bool：外层值属于本 enum 声明的一个变体且变体存储有效时为 true；nil 接口、typed-nil 变体、未知外层变体以及空 enum 的任何值为 false。该检查不遍历 payload，不执行用户方法，不使用反射或 recover；原生 Go 可传入的边界值也按同一规则处理。

Option/Result 按各自现有表示检查外层变体；None 是有效值，Some(nil) 的外层也有效，载荷为 nil 不在此处递归检查。普通 `match` 保留现有分派前的 nil panic，`_` 不接收非法值；显式检查不构成永久有效性证明，后续 match 仍执行自己的检查。serde 宏可先检查普通指针参数是否为 nil，再对解引用后的 enum 调用此操作，返回现有 Type error 后才进入 match。该提案新增了源级显式检查能力，须经确认后修订 enum 合同；不会恢复每类型 `Validate*` 包装或暴露私有 Go 变体名。

**Deserialize 的缺失字段协议。** 对字段 `type Maybe = Option[int]`，宏不能仅凭拼写决定缺失是否合法。候选普通库 trait 可把该行为作为一个明确静态成员，例如：

```goxide
pub trait Deserialize {
    func deserialize(d Deserializer) (Self, error)
    func missing() (Self, bool)
}
pub func read[T Deserialize](d Deserializer) (T, error) {
    return T::deserialize(d)
}
```

`missing` 返回缺失时的值和是否接受缺失；它不是默认方法，每个库实现显式提供。Option 实现返回 None 和 true，普通 scalar、pointer、容器及派生业务类型返回零值和 false；pointer 可接受显式 null，不因此接受字段缺失。该方法不承担 `#[serde(default)]` 的业务默认工作。宏通过普通协议选择处理 alias，先完成读取和所有必填校验，之后才按字段顺序应用 default/skip；已有字段的格式错误不触发 default。库的最终方法名和 API 可在迁移设计中收敛，但不能省略这个可表达机制、改用宏类型查询或按 Option 拼写判断。

Serialize 与 Deserialize 必须为独立协议。Serialize 的非 skip 字段只要求 Serialize；Deserialize 的非 skip 字段要求 Deserialize，default/skip 字段另要求 Default，skip 字段不要求任何读写 codec。仅 Serialize 上出现 default 不应额外要求 Default。返回 Self 的 Deserialize 通过上例显式类型实参的普通 read 函数和 Ops 证据选择，不依赖从 Deserializer 输入猜出目标类型。用户泛型 derive 暂拒绝，不影响库容器实现独立支持两个方向。

serde 的 map 实现首批精确针对 `map[string]T`；string 的透明 alias 同样匹配，defined string key 不自动匹配，保持当前边界。一般 std map 协议若引入 K 参数，还必须保留普通 Go `comparable` 类型约束，K 的 Go 声明不能降低成 any。一个仅含 marker 的库 trait 或手写 Eq 都不能让原本不可比较的类型成为 Go map key。comparable 只保证 map 类型可形成，不扩大各协议原有的键支持范围；需要更窄的稳定相等约束时，再使用普通显式库协议，不让 comparable 自动授予 Eq。

## 转译示例

### GP-01：原始类型、slice 和独立 Ops

以下为候选 Goxide 包 `example.org/protocols` 的声明片段。Clone 定义包拥有原始类型和 slice 实现；`cloneValue` 只依赖普通协议。

```goxide
pub trait Clone { func clone(self &Self) Self }
impl Clone for int {
    func clone(self &Self) Self { return *self }
}
impl[T Clone] Clone for []T {
    func clone(self &Self) Self {
        if *self == nil { return nil }
        result := make([]T, len(*self))
        for i := range *self { result[i] = Clone::clone(&(*self)[i]) }
        return result
    }
}
pub func cloneValue[T Clone](value &T) T { return Clone::clone(value) }
```

对应 Go 机制包如下，短操作名仅用于本例；独立编译运行的覆盖边界见末节。

```go
package protocols

type CloneEvidence[T any] interface {
    ~struct{}
    Clone(*T) T
}
type IntCloneOps struct{}
func (IntCloneOps) Clone(value *int) int { return *value }

type SliceCloneOps[T any, O CloneEvidence[T]] struct{}
func (SliceCloneOps[T, O]) Clone(value *[]T) []T {
    if *value == nil { return nil }
    result := make([]T, len(*value))
    var element O
    for i := range *value { result[i] = element.Clone(&(*value)[i]) }
    return result
}
func CloneValue[T any, O CloneEvidence[T]](value *T) T {
    var operations O
    return operations.Clone(value)
}
```

源级 `cloneValue[[]int](&value)` 选择 `SliceCloneOps[int, IntCloneOps]`。nil 保持 nil；非 nil 空 slice 保持非 nil；非空结果取得新存储，按索引顺序逐元素调用一次 Clone。普通赋值仍只复制 slice 描述符。原生 Go 的对应调用见互操作一节。

### GP-02：条件 impl 不改变泛型类型声明

候选 Goxide 片段位于 Box 定义包 `example.org/model`，依赖 GP-01 的 protocols。

```goxide
use protocols;
pub struct Box[T any] { value T }
impl[T any] protocols::Clone for Box[T] where T: protocols::Clone {
    func clone(self &Self) Self {
        return Self{value: protocols::Clone::clone(&self.value)}
    }
}
```

该包对应的 Go 核心片段依赖 GP-01；目标 Box 不增加协议约束或 carrier 方法。

```go
package model

import "example.org/protocols"

type Box[T any] struct { value T }
type BoxCloneOps[T any, O protocols.CloneEvidence[T]] struct{}
func (BoxCloneOps[T, O]) Clone(value *Box[T]) Box[T] {
    var element O
    return Box[T]{value: element.Clone(&value.value)}
}
```

`Box[int]` 具有 Clone；缺少 Clone 的元素仍可构造 `Box[Element]`，只有请求其 Clone 时失败。业务包的具体 Record 可使用普通库派生；以下为候选源片段，protocols 的 `derive` 子路径公开过程宏：

```goxide
use protocols;
use model;
#[derive(protocols::derive::Clone)]
struct Record { payload model::Box[int] }
```

宏只按字段语法输出以下普通 impl，目标绑定到输入的 Record，协议路径绑定到运行库；不查询 Box 的实现或私有布局：

```goxide
impl protocols::Clone for Record where model::Box[int]: protocols::Clone {
    func clone(self &Self) Self {
        return Self{payload: protocols::Clone::clone(&self.payload)}
    }
}
```

该业务包对应 Go 核心片段依赖 GP-01/02 的 protocols 和 model：

```go
type Record struct { payload model.Box[int] }
type RecordCloneOps struct{}
func (RecordCloneOps) Clone(value *Record) Record {
    operations := model.BoxCloneOps[int, protocols.IntCloneOps]{}
    return Record{payload: operations.Clone(&value.payload)}
}
```

普通 solver 证明字段条件，普通 checker 检查生成的方法体。重复的手写/派生 impl 通过同一重叠检查拒绝；无须为 Clone 增加产物登记 hook。

### GP-03：数组长度在定义包实例化，消费者类型保持私有

候选 Goxide 片段位于 GP-01 的 protocols 包：

```goxide
impl[T Clone, const N int] Clone for [N]T {
    func clone(self &Self) Self {
        var result [N]T
        for i := range *self { result[i] = Clone::clone(&(*self)[i]) }
        return result
    }
}
```

消费者 `example.org/client` 使用长度 3，并在自己的包中为私有 `secret` 显式实现 Clone。构建器在 protocols 的 staging 包中追加以下 Go 声明；该文件不导入 client：

```go
package protocols

type ArrayCloneOps3[T any, O CloneEvidence[T]] struct{}
func (ArrayCloneOps3[T, O]) Clone(value *[3]T) [3]T {
    var result [3]T
    var element O
    for i := range *value { result[i] = element.Clone(&(*value)[i]) }
    return result
}
```

client 对应的 Go 机制包依赖 GP-01 与上述长度实例：

```go
package client

import "example.org/protocols"

type secret struct { n int }
type secretCloneOps struct{}
func (secretCloneOps) Clone(value *secret) secret { return *value }
func Copy(value *[3]secret) [3]secret {
    return protocols.CloneValue[
        [3]secret,
        protocols.ArrayCloneOps3[secret, secretCloneOps],
    ](value)
}
```

预期复制三个元素，保持值与调用顺序；定义包没有消费者依赖或消费者私有名字。不同包请求同一长度只生成一个定义包实例；长度 4 产生另一个普通声明，不改变长度 3 的身份。只使用 Go 编译器的调用方不能自行请求未生成的 `ArrayCloneOps4`。

### GP-04：Default 的普通调用递归与约束递归分别处理

候选 Goxide 片段在业务包中，普通协议 Default 的声明为 `pub trait Default { func r#default() Self }`：

```goxide
use protocols;
struct Counter { n int }
func makeCounter(n int) Counter {
    if n == 0 { return Counter{n: 7} }
    return makeCounter(n - 1)
}
impl protocols::Default for Counter {
    func r#default() Self { return makeCounter(2) }
}
```

业务包对应 Go 核心片段如下：

```go
type Counter struct { n int }
func makeCounter(n int) Counter {
    if n == 0 { return Counter{n: 7} }
    return makeCounter(n - 1)
}
type CounterDefaultOps struct{}
func (CounterDefaultOps) Default() Counter { return makeCounter(2) }
```

预期得到 n 为 7 的值，普通函数递归不被 Default 特判拒绝。若删除退出分支导致无限递归，适用普通函数的运行时行为，不是假称编译器证明安全。另一个 impl 若把 `Counter: Default` 作为自身的证明前提，则在调用前按证明循环拒绝；它没有合法 Go 输出。

## 拒绝与边界

以下均由 Goxide 前端拒绝，不能留给 Go 错误或静默退化：

| 编号 | 输入或条件 | 原因 |
| --- | --- | --- |
| GP-05 | 同时有 `impl[T Clone] Clone for []T` 与 `impl Clone for []int` | 目标模式重叠，具体实现无优先权 |
| GP-06 | `impl[T A] P for Box[T]` 与 `impl[T B] P for Box[T]` | 不以当前 A/B 约束区分重叠 |
| GP-07 | 第三方包为外部 trait 和外部类型写有体 impl，或为 `[]Local` 写外部 trait impl | 不满足 owner 规则；不影响合法的具体 declare 采用 |
| GP-08 | 只具有同名 Go 方法，或只有伪造 helper，没有显式协议关系 | Go 结构相容不建立 Goxide impl |
| GP-09 | Node 的显式 where 前提要求 `[]Node: Clone`，容器又要求 `Node: Clone` | 输出有限诊断链，拒绝资格证明循环；不拒绝无条件具体 impl 的普通方法体递归 |
| GP-10 | `const N` 为负数、非常量、未能匹配，或需要求解 `[N+1]` 模式 | 超出首批长度参数规则 |
| GP-11 | Clone 方法通过只读 self 修改字段，或返回不相容 Self | 普通权限/类型检查；宏来源不豁免 |
| GP-12 | Eq 的 PartialEq 前提缺失、trait 前提形成环或关联类型绑定矛盾 | 普通协议证明失败 |
| GP-13 | `enum_valid` 的输入为普通接口、指针或无 enum 约束的类型参数 | 无法按静态 enum 身份检查，不插入反射或隐式解引用 |
| GP-14 | 只有 Serialize 的字段用于 Deserialize，或反向使用 | 两种协议独立，不能以一个共同 Codec 约束暗中补足或额外要求另一方向 |
| GP-15 | `type Score int` 没有显式协议，却因 int 的 impl 请求其协议 | defined 身份独立；透明 alias 对照用例应成功 |

库实现继续承担既有行为：Default 按字段顺序调用业务默认，enum 只要求默认变体 payload；Option 默认 None 不要求元素 Default，Result 不自动取得 Default；容器 Clone 说明存储和元素复制，不能改用赋值兜底；Debug 保持字段/变体名、数据及字符串引号，map 输出的排序等行为由库实现；serde 保持方向独立、缺失/default/skip、错误与范围检查、nil/空和 Option 规则。enum 的检查和变体身份属于通用 enum 合同，显式可失败检查按第 5 节提议，宏不能绕过普通 match 的检查。

这些要求需逐项迁移验收，不因泛型 impl 可以表达某个签名就认为业务等价。原生 map 键资格等额外能力若需证明，应由普通库协议及约束表达；手写 Eq 不自动取得该资格，浮点等类型也不能因 Go comparable 就被授予 Eq。不能在 checker 中按 Default、Clone 或 serde 名字偷偷补实现。

## 双向互操作

Goxide 调用原生 Go 函数保留原签名；`declare impl` 继续精确采用原生方法并生成所在声明包的静态 Ops 及必要 wrapper，不给原生类型追加方法。普通非泛型函数和无协议约束的泛型函数不增加 Ops 参数。

原生 Go 调 GP-01 时，显式写出无法从普通值参数推导的证据类型：

```go
value := []int{1, 2}
copy := protocols.CloneValue[
    []int,
    protocols.SliceCloneOps[int, protocols.IntCloneOps],
](&value)
_ = copy
```

Goxide 源调用仍只写自己的类型实参，由 checker 选定 Ops。省略上例 Ops 写成 `protocols.CloneValue[[]int](&value)`，Go 的独立机制检查得到 `cannot infer O`。关联类型仍可在显式 Ops 后推导：对形如 `Read[T any, O SourceEvidence[T, Item], Item any]` 的函数，O 的 `ItemWitness() Item` 签名使 `Read[Slot, SlotOps]` 推导 Item 为 int；见证方法不执行。这保留现有见证推导机制，但不再保证所有函数的 Go 类型实参数量都等于 Goxide 源数量。为每个具体调用生成转发包装不作为隐藏这一变化的通用办法。

Go 可以自行提供满足 Ops 约束的类型，这与普通 Go 可实现导出 marker 一样，不构成经 Goxide 验证的协议登记；重新导入时仍从可信源声明恢复显式关系。Go 调用方也不自动获得 Goxide 的只读检查。运行时 trait 的既有固定工厂、wrapper 动态类型和引用身份保持不变。

## 验收与未定项

确认本篇后，实施应按一条普通路径推进：泛型 impl/多约束与归属唯一性 → 独立 Ops 和关联类型证据 → 数组长度实例化 → 库 impl → 宏输出普通 impl → 删除对应固定派生登记、专用 helper 生成和类型分支。宏输出必须回到 `expand → resolve → check → lower`，不能用生成 Go 再 ParseFile 的方式绕过源检查。

验收至少包括：

1. GP-01 至 GP-04 由真实 Goxide 生成 Go 并 build/run，检查值、nil/空、字段和元素调用顺序、容器存储身份；另有原生 Go 消费者调用 Ops 泛型 API。
2. GP-05 至 GP-15 在源位置拒绝；alias、重导出、同名 trait、依赖顺序变化不改变实现选择和诊断归属。
3. 覆盖具名泛型容器、数组/slice/map 嵌套、具体业务字段、跨包私有元素类型和关联类型；两个长度的定义包实例可共存，输入重复两次生成字节一致。
4. 手写与派生业务实现使用同一唯一性和权限检查；无宏类型查询、编译器内 derive 名称分支或失败时的赋值/反射兜底。
5. 原有 Default/Clone/Debug/equality/serde 的对应端到端行为继续通过；手写 Default 函数递归接受是明确变更，证明循环仍拒绝。用户泛型 derive 若暂不开放，由宏提供明确诊断。
6. 现有运行时 wrapper、具体外部 declare、Go → Goxide 恢复及普通原生调用不因静态证据替换改变行为；原生 Go 类型实参变化按本稿更新合同和调用示例。
7. serde 的具体无条件 impl 覆盖自身及相互递归字段，有限值真实 encode/decode 成功；相同目标改成显式循环前提时拒绝。Option alias 缺失为 None、普通 pointer 缺失仍报错、present 格式错误不调用 default、所有必填校验先于 default/skip 的副作用顺序均有端到端对照。
8. 显式 enum 检查覆盖合法变体、nil 接口、typed-nil、空 enum、非法外层变体和含 nil payload 的合法外层值；检查参数只求值一次，普通 match 仍 panic，serde 返回 Type error。原生 Go 消费者注入的边界值必须参与验收。

上述能力仍须连同普通库方法与宏接线验收。既有组内 TypeSpec 的 derive、helper 属性生命周期和公开 Go 调用入口不能因切换到普通宏而丢失；这些是迁移集成要求，不由泛型 impl 的 Go 机制例子证明完成。

GP-01 至 GP-04 的 Go 片段已在临时独立模块以 Go 1.27.1、`GOTOOLCHAIN=local GOWORK=off` 编译运行：slice 存储分离、nil/空、元素次数与顺序、条件 Box、私有元素的跨包数组、Default 普通递归均通过；定义包依赖图没有消费者反向依赖。另验证既有 carrier/见证和独立 Ops 的关联类型推导，anchor/见证方法均未执行，缺少 Ops 的原生 Go 调用按预期拒绝。这些是手写 Go 机制验证，以上 Goxide 前端及迁移验收仍待实施。

待确认项见开头，包括 Eq marker 的作者承诺入口。公开 Ops 的最终稳定名称、关联类型辅助参数顺序和分阶段库迁移范围须在实施前按现有命名与发布规则收敛；跨仓库代码与发布仍遵循对应授权。

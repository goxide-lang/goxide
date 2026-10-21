# RFC 0031：普通库协议的条件 impl 与按需实例化

## 状态与范围

**泛型/条件 impl、按需实例化与 Go 普通入口已实现。** 宏输出普通 Goxide 声明，经同一条解析、名字、类型及权限检查管线处理。当前状态与实际验收见[状态表](../mvp-gap-audit.md)。

[RFC 0030](0030-procedural-macros.md) 提供语法变换与受限执行，宏不查询类型、不登记 impl。Default、Clone、Debug、PartialEq、Eq、Serialize、Deserialize 的迁移使用普通库协议；每个业务协议仍须保持原有行为并分别验收。

| 已确认边界 | 规则 |
| --- | --- |
| 泛型与条件 impl | 类型参数、同一类型的多个 trait 约束、具体或复合类型的 where 条件由普通 checker 处理；条件不收紧目标类型本身 |
| impl 归属与唯一性 | 有体 impl 归 trait 定义包或最外层具名目标的定义包；可能匹配同一目标的实现冲突，不以约束或特化排序 |
| 普通协议前提 | 非泛型 trait 可声明无环的 `where Self: Other` 前提；关联类型按所属 trait 区分 |
| 具体 impl | 没有待定参数且前提可证明的 impl 直接生成，Go 使用者可调用其正常公开入口 |
| 按需实例化 | 含待定源级协议条件的 impl/函数保留为模板；从本次转译范围内的实际请求递归生成已确定的实例 |
| 普通 Go 泛型 | Go 能直接表达、不依赖待定 Goxide 实现选择的类型和函数保留泛型；`Box[T any]` 不因条件 Default/Clone 收紧 T |
| Go 使用边界 | 条件能力通过 Goxide 明确导出的普通函数使用；Go 调用者不传 Ops，也不引用实例哈希名 |
| sugar | 按[分发模块配置](../design/toml-manifest.md#go-成员-sugar-配置)，所有 impl 来源一致；只对 Go 允许追加方法的目标生成短入口 |
| 数组长度参数 | `const N int` 仅匹配固定数组长度、接受显式非负整数常量和原样转传；不开放长度表达式求解或 const 泛型具名类型 |
| enum 外层检查 | `enum_valid(value)` 单次求值并返回外层变体是否合法；payload 与普通 match 的检查规则不变 |

最小数组长度参数与显式 enum 可失败检查已实现。Default 的函数调用图递归限制和 Eq 派生的现有资格规则保持不变；下文关于放宽这两项的候选暂不实施。`no_mangle` 不属于本篇或本次实现。

[已确认决策](../design/decisions.md) D05 的外部原生采用、D19/D20 的权限边界、运行时 wrapper 和 enum 表示继续适用。此稿不增加 move、独占分析、动态字典、反射兜底或每种 derive 的编译器 hook。用户在泛型类型上使用 derive 可继续由宏明确拒绝；但库的泛型 impl 必须能为具体业务类型的泛型字段提供协议。

Error 派生、`go_result!` / `go_result_keep!`、`cfg` 属于不同边界：Error 的 struct 方法已有普通 inherent impl 迁移证明，enum 原生 `error` 桥接与 `#[from]` 注册仍未覆盖；go_result 涉及调用结果形状和错误转换，cfg 决定进入展开的源码。本篇不据此宣称它们均可迁出编译器。print/println 的库化也须另验格式检查与调用行为；本篇仅提供其可使用的普通协议能力。

## 源级规则

### 1. 声明、归属与约束

已确认语法沿用 `impl[T ...]`、`where` 形状和 `+` 组合；数组常量参数按第 4 节限制使用：

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

普通 trait 前提可表达 Eq 对 PartialEq 的关系；将现有 Eq 派生迁为下列库协议时仍须核对其独立行为合同：

```goxide
pub trait PartialEq { func equals(self Self, other Self) bool }
pub trait Eq where Self: PartialEq {}
```

证明 `T: Eq` 时先证明其前提；在 `T: Eq` 的函数内可使用 PartialEq。只开放无环的非泛型 trait 前提，不增加默认方法、运行时自动视图转换或任意继承机制。PartialEq 继续使用两个值参数，不借迁移暗中改成只读借用。

**暂缓的 Eq 迁移候选。** 若保留“派生 Eq 只接受结构派生 PartialEq”的区别，可由普通库 marker `StructuralPartialEq` 表达：PartialEq 宏同时生成该 marker 的 impl，Eq 宏要求 `Self: StructuralPartialEq`。手写 PartialEq 本身不产生 marker。marker 与手写 Eq 一样是作者承诺，用户也可显式实现；它不是编译器签发的不可伪造证明。接受此规则包含接受这种显式承诺入口；不能为隐藏它重新增加 derive 名称判断。

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
            P ⊢ τ:Q ⇓ Selected(I, θ, prerequisite-witnesses)
```

这些规则是设计约束，不是已完成的形式证明。删除 owner 条件会让任意依赖定义外部类型的业务；删除重叠检查会让依赖集合或顺序改变分派；用“当前约束不相交”规避重叠，会在以后增加 impl 时改变唯一性。本稿采用保守的目标模式合一检查，不求解约束互斥性。

`declare impl` 继续采用真实原生方法，保留 [RFC 0003](0003-declare-impl.md) 允许第三方包采用外部 trait/类型和相容声明合并的规则。该入口不能因新的有体 impl owner 规则被删除。相同采用关系可合并；有体与 declare 的匹配目标重叠仍诊断，不用“有体优先”覆盖已有行为。首批不要求泛型 declare；现有具体 declare 必须仍能参与泛型函数的证据传递。

证明缓存按 canonical 目标、trait、关联绑定和代换区分。命中正在证明的同一义务报告完整链；不默认假设递归协议成立。无匹配、重叠、关联类型冲突或证明循环均在生成成功产物前诊断。

普通函数体调用不进入上述 impl 证明栈。**既有 Default 递归限制的调整暂缓。** 候选规则允许手写 `Default::r#default` 可经普通递归调用返回，也可能无限递归，语言不承诺终止。移除当前仅针对 Default 的函数调用图拒绝，是本稿明确提出的语义变化。相反，宏若显式生成递归字段前提，形成 `Node: Clone → []Node: Clone → Node: Clone`，仍是证明循环；本稿不以共同递归证明放行。

具体、无前提的 impl 则先登记其完整签名，再检查普通方法体；所有实现体通过后才可生成成功产物。检查方法体内的调用时，可以选择该无条件实现自身或同批登记的其他具体实现。以 `Node { Next *Node }` 为例，serde 宏可生成无 where 前提的具体 Serialize/Deserialize impl：方法体选择 `*Node` 容器证据，后者选择已登记的无条件 Node 证据。其 Go 形式是已选定的 Node 和指针处理函数相互调用；实例工作队列按身份去重，互相递归的具名业务类型按同一规则处理。方法体仍逐项检查字段协议，未知 codec 不能因提前登记而被放行。

这与带有 `where *Node: Serialize` 的 Node impl 不同：后者的资格本身依赖尚待证明的同一资格，仍被拒绝。现有 serde 生成器可通过直接 helper 调用处理递归字段，迁移不能机械地给每个具体字段增加 where 而失去此能力。当前 Clone 的递归拒绝可由其宏生成显式字段前提保留；不要求所有业务宏选择同一前提策略。两种递归都不承诺运行时终止，也不替循环对象图增加检测。

### 3. 转译范围、需求闭包与 Go 入口

项目转译以本次选中的模块为入口，原生 Go 的原位 `gen` 以当前包为入口。已选源码依赖提供普通声明及模板；请求沿实际调用、函数值和 impl 前提追踪，不枚举可见类型与全部协议的笛卡尔积。关闭 sugar 不删除协议需求。

具体 impl 先完成前提检查，再直接生成全部成员。泛型/条件模板先在声明的约束下检查方法体；只有进入已确定请求的模板才产生实例。普通函数体和包级初始化、具体 impl 的成员体，以及作者明确提供的具体公开函数均可引出请求。仅给仍含协议参数的模板加 `pub` 不产生未来所有 Go 类型的实例。

工作队列按 canonical 声明身份、类型实参、关联类型绑定及已选条件实现去重。每个实例把代换应用到其调用、函数值、关联投影及内层 impl 条件，再加入新的请求。相同实例递归只生成一份声明；前提证明循环仍由 checker 拒绝。请求顺序和依赖遍历顺序不改变产物。无法形成有限实例闭包的类型增长递归在源位置诊断，不生成部分成功产物。

普通 Go 泛型继续保留：例如 `Box[T any]`、`identity[T any]` 和能直接按 Go 方法集检查的函数，不因同包存在条件 impl 而全部单态化。Goxide 协议条件只约束使用该能力的 impl/函数；`Box[NoDefault]` 可以存在，只有请求其 Default 时才检查条件。

原生 Go 通过明确的普通源函数使用条件能力。例如业务库提供 `encodeRoot` / `decodeRoot`，分别确定 Root 的 Serialize / Deserialize 及其字段依赖。对应 Go 函数保持源码公开名称和业务签名；Go 调用者不构造 Ops、不传证据类型参数、不引用实例化产生的哈希符号。Encode 与 Decode 分别引出自己的条件闭包。

实例化保持定义点绑定、Go 包身份与接收权限。不能为了展开库实现，把访问私有字段或私有 helper 的业务体搬到无权访问的包，也不能让定义包反向依赖消费者私有类型。必要时在定义包保留普通 Go 泛型 helper，传入已确定签名的操作函数；例如 `CloneVecFunc[T](value Vec[T], clone func(T) T)`。跨包 helper 使用稳定、可读的名称；具体公开入口负责提供操作，Go 业务调用者不处理编译器证据。多方法和关联类型按实际签名传递，不把任意协议抹成 `any` 或反射。

保存 trait 方法值时，接收者在绑定时只求值一次；value 接收者每次调用取得独立副本，引用接收者继续指向原槽位。绑定器返回含原业务体的 Go 方法值；声明适配的直接调用和保存调用绑定实际原生 Go 方法，避免额外转发帧破坏 `defer`/`recover`。已有公开 Go 泛型入口需要保存方法时，其可用具体 carrier 同步提供对应绑定器，即使库内没有 Goxide 调用。`trait_method_value_frame_test.go`、`trait_go_binding_boundary_test.go` 和 `trait_concrete_frames_test.go` 覆盖这些行为及具体 impl 的业务体注释与 panic 源行；运行时 TraitView 的声明适配不在此次修改范围内。

实例只写本次生成目录或受管原位产物，不修改依赖 checkout、模块缓存。只有生成 Go 的依赖提供其现有 API；需要新的源级条件实例时由 Goxide 取得模板并重新生成。

已有具体 impl 的 wrapper、marker、固定工厂、As/For、carrier 和关联类型 witness 保留完整协议入口。静态条件实例化不向 `Box[T any]` 强加条件性 anchor，也不把源目标 T 换成运行时 wrapper。运行时视图仍按已选 impl 保存值或真实借用 owner。[Go 成员 sugar](../design/traits.md#go-成员-sugar) 在全部 impl 成员确定后按模块配置处理；实例化 `Box[User]` 不会使 Go 允许给这个特定实例单独追加方法。

### 4. 最小数组长度参数

本节已实现。`const N int` 首批只服务固定长度数组和原样转传的长度参数：N 从实际 `[n]T` 的长度匹配，也可由显式非负整数常量提供。首批不接受负长度、非常量、`[N+1]` 模式求解、改变 N 的递归实例化或任意类型的 const 参数；不开放 const 泛型具名类型，也不将 `[N]T` 扩展为 Go 类型集合约束。显式值可使用整数常量表达式与导入的 Go 整数常量；当前不将 `len` 等内建调用作为常量参数求值。

沿用第 3 节的需求闭包，为需要的具体长度生成普通 Go 声明。元素类型能继续保留 Go 泛型时不额外展开；不能以每种数组长度一个编译器协议 hook 代替普通模板。原生 Go 通过已有具体入口使用结果，新的长度由 Goxide 源请求引出。

### 5. serde 迁移所需的普通表达性

仅有 Serialize/Deserialize 方法签名还不足以保持现有行为。本节包含已确认的通用 enum 检查，以及迁移前必须落实的普通库协议；不把字段分派搬成编译器隐式 hook。

**显式 enum 检查已实现。** 内建调用 `enum_valid(value)` 沿普通内建的解析和遮蔽规则。输入的静态类型须为具体 enum 或已知 enum 的泛型实例，透明 alias 同样适用；不接受任意 `T any`、接口或自动解引用的指针。参数只求值一次，结果为 bool：外层值属于本 enum 声明的一个变体且变体存储有效时为 true；nil 接口、typed-nil 变体、未知外层变体以及空 enum 的任何值为 false。该检查不遍历 payload，不执行用户方法，不使用反射或 recover；原生 Go 可传入的边界值也按同一规则处理。

Option/Result 按各自现有表示检查外层变体；None 是有效值，Some(nil) 的外层也有效，载荷为 nil 不在此处递归检查。普通 `match` 保留现有分派前的 nil panic，`_` 不接收非法值；显式检查不构成永久有效性证明，后续 match 仍执行自己的检查。serde 宏可先检查普通指针参数是否为 nil，再对解引用后的 enum 调用此操作，返回现有 Type error 后才进入 match。表示与检查规则统一见 [enum 合同](../design/enum-representation.md#21-显式外层有效性检查)；普通可命名输入在调用处生成检查。跨包私有 enum、私有变体或私有类型实参无法在调用处命名时，允许按需在 enum 定义包生成编译器内部检查函数，并由 Go 推导类型实参；该函数不增加 Goxide 公开 API，不暴露私有变体名，也不改变单次求值和无用户方法调用的规则。

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

Serialize 与 Deserialize 必须为独立协议。Serialize 的非 skip 字段只要求 Serialize；Deserialize 的非 skip 字段要求 Deserialize，default/skip 字段另要求 Default，skip 字段不要求任何读写 codec。仅 Serialize 上出现 default 不应额外要求 Default。返回 Self 的 Deserialize 通过上例显式类型实参的普通 read 函数选择具体实例，不依赖从 Deserializer 输入猜出目标类型。用户泛型 derive 暂拒绝，不影响库容器实现独立支持两个方向。

serde 的 map 实现首批精确针对 `map[string]T`；string 的透明 alias 同样匹配，defined string key 不自动匹配，保持当前边界。一般 std map 协议若引入 K 参数，还必须保留普通 Go `comparable` 类型约束，K 的 Go 声明不能降低成 any。一个仅含 marker 的库 trait 或手写 Eq 都不能让原本不可比较的类型成为 Go map key。comparable 只保证 map 类型可形成，不扩大各协议原有的键支持范围；需要更窄的稳定相等约束时，再使用普通显式库协议，不让 comparable 自动授予 Eq。

## 转译示例

以下展示已确认的输出边界；库协议名称在派生迁移时接入，示例不将当前内建名称误认为已迁移。

### GP-01：slice 条件实现由具体入口请求

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
pub func cloneInts(value &[]int) []int { return Clone::clone(value) }
```

Go 的公开入口为 `CloneInts(value *[]int) []int`。实例化后的元素调用选择 int 的具体实现，循环只复制一次每个元素；nil 保持 nil，非 nil 空 slice 保持非 nil。未请求的 `[]Other` 不额外生成，宏不根据元素类型自行分派。

### GP-02：条件 impl 不改变泛型类型声明

```goxide
pub struct Box[T any] { value T }
impl[T Clone] Clone for Box[T] {
    func clone(self &Self) Self {
        return Self{value: Clone::clone(&self.value)}
    }
}
pub func cloneIntBox(value &Box[int]) Box[int] {
    return Clone::clone(value)
}
```

`Box[T any]` 保留普通 Go 泛型。Go 消费者调用 `CloneIntBox`，可以另行构造 `Box[NoClone]`；只有源级请求克隆后者才要求其元素满足 Clone。具体业务类型的 derive 只输出普通 impl，字段协议由同一个 checker 证明。

### GP-03：跨包私有类型与普通 helper

容器包保留能访问其私有字段的普通泛型 helper，元素复制作为精确函数参数。业务包的具体公开函数传入本包的操作，不让容器包导入业务包或命名其私有元素类型：

```go
// 容器定义包
func CloneBoxFunc[T any](value *Box[T], clone func(*T) T) Box[T] {
    return Box[T]{value: clone(&value.value)}
}

// 业务包
func CloneRecord(value *Record) Record {
    return Record{payload: model.CloneBoxFunc(&value.payload, cloneSecret)}
}
```

上例仅展示 Clone 的操作签名。其他协议按其真实参数、结果和关联绑定生成；不增加通用运行时字典或反射兜底。

### GP-04：Default 函数约束与类型约束分开

`Box[T any]` 与 `defaultBox[T Default]() Box[T]` 使用不同约束。现有 carrier 能让 Go 从具体类型推导 Default 操作时，函数可以保留对应普通 Go 泛型；这不自动授予任意 `Box[T]` 一个普通 anchor。对没有 anchor 的容器组合，作者提供例如 `defaultUserBox() Box[User]` 的具体入口，由 Goxide 完成条件选择。

已有 Default 的业务默认、字段顺序和递归拒绝继续按当前合同处理；调整既有递归规则仍是本篇单列候选。

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

库实现继续承担既有行为：Default 按字段顺序调用业务默认，enum 只要求默认变体 payload；Option 默认 None 不要求元素 Default，Result 不自动取得 Default；容器 Clone 说明存储和元素复制，不能改用赋值兜底；Debug 保持字段/变体名、数据及字符串引号，map 输出的排序等行为由库实现；serde 保持方向独立、缺失/default/skip、错误与范围检查、nil/空和 Option 规则。enum 的检查和变体身份属于通用 enum 合同，显式可失败检查按第 5 节实现，宏不能绕过普通 match 的检查。

这些要求需逐项迁移验收，不因泛型 impl 可以表达某个签名就认为业务等价。原生 map 键资格等额外能力若需证明，应由普通库协议及约束表达；手写 Eq 不自动取得该资格，浮点等类型也不能因 Go comparable 就被授予 Eq。不能在 checker 中按 Default、Clone 或 serde 名字偷偷补实现。

**PartialEq 的 enum 深层校验仍是迁移缺口。** 现有 enum 相等入口在比较变体或执行手写相等方法前，先校验左操作数、再校验右操作数的有效载荷；校验会穿过结构体字段、数组、slice 和 map 的值，即使载荷类型有手写 PartialEq。普通结构体和容器的相等比较则仍按访问顺序短路。`enum_valid` 只检查外层，不能代替这个既有合同。例如外层 enum 的有效变体携带一个结构体，结构体中未初始化的内层 enum 仍须导致 panic，即使另一操作数属于不同外层变体，且载荷的手写相等方法不会被调用。`tests/acceptance/equality_test.go` 与 `manual_equality_test.go` 保留这些行为验收；当前普通库证明尚未提供等价的通用表达方式，不得据此移除该校验或偷偷增加字段协议要求。所需通用能力或行为调整须另行确认。

普通库迁移基础已有真实 WASI 验证：`internal/compile/protocol_migration_test.go` 从公共 syn 读取实际字段和变体，生成五种 `Library*` 普通协议实现。具体 struct/enum 的 Default/Clone/Debug 证明覆盖默认变体字段的约束与调用顺序、Option None 不要求元素 Default、Result/容器不自动取得 Default；Clone 覆盖 slice/array/map、enum 与 Option/Result 的元素协议、独立存储和 nil/empty 状态；Debug 覆盖字段/变体名、字符串转义及 map 稳定排序。map Clone 的键限于非 complex 的内建 scalar，Debug 限于 string，透明 alias 对照通过，defined key 不自动匹配。14 个拒绝用例检查缺失协议、helper 选择及键资格；原有私有字段和手写字段相等协议断言保留。该宏仍只接受具体类型，未补齐上述 enum 深层 PartialEq、Eq 派生资格或 Default 调用图规则。

`internal/compile/error_protocol_migration_test.go` 以普通 `LibraryError` 宏读取 struct 字段和自定义 helper，生成 inherent `Error() string` 与有 source 时的 `Unwrap() error`，不使用内建 Error hook。证明覆盖消息插值与转义、scalar/error 透明 alias、私有字段、nil/typed-nil source 的保留，以及原生 `errors.Is` / `errors.As` 到达原 cause；14 个拒绝用例覆盖消息/helper 错误、source 与 scalar 类型资格、手写方法冲突。此证明限于具体 struct，不包含 enum 原生 error 桥接或 `#[from]` 注册。

两项证明均实际执行 Go 消费者 build/run/test，并核对锁定重放的生成内容与锁文件字节一致。`internal/compile/serde_protocol_migration_test.go` 验证普通 Serialize/Deserialize 协议与现有公开 serde API 的接线、条件容器、读写方向独立、Option alias 与指针的缺失区别、显式 null、default/skip 顺序及 enum 错误路径。`attribute_protocol_migration_test.go` 覆盖 attribute 产出的条件 impl，`print_protocol_migration_test.go` 覆盖格式解析、普通 Display/Debug 调用、求值顺序和调用帧。它们使用普通宏与库入口；std/serde 正式迁移发布及完整业务等价验收仍待完成。Default 专用调用图检查与 Eq 派生资格仍按上面的暂缓决定处理；旧入口的移除须随对应库的正式迁移验收进行。

### 待确认的迁移边界

以下是剩余缺口的候选合同，尚未批准或实现；它们不改变上面的已确认边界。Default 递归与 Eq marker 继续暂缓。

**保留配置选择和 Go 返回适配为语言机制。** 普通宏可输出现有 `cfg` / `cfg_attr` / `cfg!` 语法，由编译器继续按来源和构建角色求值。原始源码的配置选择先于入口登记与宏执行，宏生成项按现有展开流程继续筛选；完整规则包含宏 Context 不可查询的环境，不能以普通 attribute 执行代替。`go_result!` / `go_result_keep!` 继续依据真实单值或双值返回签名选择适配，按原静态错误类型判断 nil，并保留调用帧。让它们完整迁出需要另行设计配置阶段接口及静态返回形状能力，不能把仅支持 target/feature 或精确 `error` 的库函数算作完整替代。该候选保留 RFC 0030 的纯语法边界及 D36 的行为。

**enum 的显式 Go 成员桥接。** 暂定 `#[go_method]`，供普通宏输出，也可手写；按属性而非 `Error` 名称决定桥接。

```goxide
impl Failure {
    #[go_method]
    pub func Error(self Self) string { /* 普通业务体 */ }
    #[go_method]
    pub func Unwrap(self Self) error { /* 普通业务体 */ }
}
```

首批目标是本包具体 enum 的无条件固有值成员，接收者为 `self Self`。enum 接口获得精确签名，每个指针变体的方法直接承载业务体，体内 self 仍为完整 enum；借用接收者保留现有槽位入口。源码调用及方法值使用真实 Go 分派，nil 接口在调用或绑定方法值时失败；typed-nil 变体可进入方法，Error 宏在业务体内生成既有外层校验。桥接不更换动态变体身份，`Unwrap` 原样返回 cause，`defer` / `recover` 不经过额外转发帧。成员权限、源码位置及名称冲突沿普通规则检查。

验收须同时包含普通非 Error 方法、原生 Go interface 调用、直接与保存方法值的 recover、nil 绑定时机、typed-nil、私有变体、字段或方法冲突，以及借用和外部目标拒绝。按变体复制业务体会增加生成代码量；此代价需随接口一并确认。

**显式单步转换登记。** 暂定 `#[try_from]`，只登记展开后的普通函数声明，不接受宏响应中的特殊登记数据。

```goxide
#[try_from]
func FromFailure(cause error) Failure {
    return Failure::Input(cause)
}
```

候选函数须非泛型、无 receiver、非 variadic，恰有一个入参和一个结果。`?` 先尝试直接可赋值，再选择当前包唯一且类型精确匹配的 `S → T` 登记；不搜索转换链或依赖包，不按函数名字匹配。S/T 沿用既有 Result 错误载荷范围，不新增 Go `error` 限制。库 Error 宏可消费 `#[from]` 后生成该函数。验收包括 alias 与 defined 身份、未登记或重复候选、两跳和跨包不自动采用、签名拒绝，以及输入单次求值和仅 Err 分支转换一次。

**enum 比较前的有限结构校验。** 候选是将既有校验开放为显式 `enum_assert_valid(value)`，首批用于 payload 静态类型完全确定的 enum 实例：实参求值一次，无返回值；普通 PartialEq 宏在两个操作数都已求值后，先对左值、再对右值调用，随后才比较变体或执行业务相等方法。校验读取活动 payload，穿过结构体字段、array、slice 和 map 值，不调用字段的业务协议，也不把额外协议条件加到字段类型上。非法 enum 按既有规则 panic。现有 `enum_valid` 的外层 bool 检查保持不变。

候选原样保留当前有限遍历范围：不遍历 pointer、普通 interface 或 map 键；遇到当前包含路径上已有的规范类型，终止该条边且不再次校验该节点。例如 `E` 的变体携带 `[]E`，slice 元素会在静态回边处截断，不能据此证明整个运行时对象图有效。若将不可访问字段的检查移到其定义包，生成 helper 仍须保留调用点的路径截断，不能重新开始校验。只在实际需要读取的字段无法合法访问且没有可生成检查的定义源码时诊断；无关私有 scalar 字段不构成拒绝理由。这部分跨包实现与等价验收尚未完成，不能把 helper 设想记作支持。批准前继续使用现有 enum 相等入口；批准后以普通库宏验收有限遍历、左右顺序、手写 payload 协议及私有字段路径，再移除对应 hook。

## 双向互操作

Goxide 调用原生 Go 函数保留原签名；具体 `declare impl` 精确采用原生方法，不给外部类型追加方法。普通 Go interface 使用 Go 方法集，不搜索 Goxide 条件 impl。

Go 调用已生成的具体业务入口、具体 impl 的完整协议入口以及启用的 sugar。只有 Goxide 源构建解析新的条件需求；纯 Go 下游新增类型组合不会触发编译器隐式生成。原生 Go 的 `Box[T]` 继续保持源类型参数和身份，条件 impl 不回写容器声明约束。

[sugar](../design/traits.md#go-成员-sugar) 的签名参与 Go 方法集、接口和原生泛型约束检查，不登记成新的 Goxide 固有成员。显式固有/手写 Go 方法先占名，剩余候选重名时报错。模块开关不改变 impl 选择、宏输入、接收权限或完整协议入口。

## 验收与未定项

实施顺序：普通泛型/条件 impl 和多约束 → 转译范围内需求闭包与普通 Go 入口 → 模块 sugar 与属性生命周期 → 普通库 impl 和过程宏接线 → 删除对应固定派生检查及降低。转译改动按主题独立原子提交，与业务宏迁移分开验收。宏输出始终回到 `expand → resolve → check → lower`。

验收至少包括：

1. 具体 impl 在没有 Goxide 调用时仍可被 Go 使用；未请求的条件实例不生成，普通 Go 泛型继续保留。
2. slice、map、具名容器嵌套及具体字段请求形成完整闭包；Go 消费者只调用明确普通入口，验证字段/元素调用顺序、nil/空和存储身份。
3. 泛型函数显式/推导调用、函数值、关联类型及多约束传播正确；具体和相互递归实现去重，证明循环与类型增长递归有源位置诊断。
4. 跨包私有元素、私有容器字段和定义点 helper 保持可见性；无反向依赖，同输入两次生成字节一致。
5. 普通函数的业务体保持调用帧与源码位置，覆盖 `defer`/`recover`、体内注释和行号；不以额外转发层改变语义。
6. sugar 的默认/TOML/CLI 优先级、依赖配置隔离、原位生成和缓存切换；Go 混写与独立 dist 消费真实 build/run/test，关闭 sugar 后完整协议入口仍可用。
7. 手写、声明宏、attribute 和 derive 产出的 impl 使用同一路径；显式成员先占名、剩余冲突诊断、借用保留槽位身份，sugar 不成为源级新固有方法。
8. Serialize/Deserialize 需求独立，保留 Option alias 缺失、pointer 缺失、present 错误、default/skip 顺序及 enum 错误行为；不按 derive 名称补隐式能力。
9. 惰性属性遵循消费者生命周期，attribute 替换负责转交、derive 保留原项；未知属性仍拒绝，不实现 `no_mangle`。

最小数组长度与 enum 显式检查已按上述最小范围实现；Default 既有递归规则调整和 Eq marker 候选暂缓。跨仓库写入与发布遵循对应授权。阶段状态记录在状态表，不把机制原型或未执行的迁移验收记为完成。

# RFC 0008：变体枚举、Option 与 Result

## 当前规范与形式模型

当前用户 enum 使用密封接口与每变体一个指针结构体，具体合同见[enum 表示](../design/enum-representation.md)。语法见[词法与语法](../design/language-grammar.md)，阶段输入输出、存储模型与真实 Go 对照见[转译语义](../design/translation-semantics.md)。以下模型优先于本文后面的历史候选布局。

设 `Δ(E[α]).V = (U1, ..., Un)` 是变体字段声明，`θ = [α := T]` 为类型替换：

```text
Γ ⊢ ei : Si       Si 可赋值给 θ(Ui)       i = 1..n
────────────────────────────────────────────────── [Enum-Construct]
Γ ⊢ E[T]::V(e1, ..., en) : E[T]

Identity(E[T]) = (定义包, enum 声明, T)
⟦E[T]::V(values)⟧ = &V[T]{对应字段: values}
```

Go 输出中 `E[T]` 是含未导出标记方法的接口；`*V[T]` 实现该方法并携带 enum 的全部类型参数。普通赋值复制接口并共享变体对象；payload 槽位不可变，整体替换 owner 后，旧只读投影与固有方法值继续指向旧对象。容器 payload 按自身权限使用，详见[引用合同](../design/enum-payload-borrows.md)。

```text
ValidDispatchE(v) = 非 nil 接口 ∧ 动态类型为合法 *V ∧ 变体指针非 nil
nil 字面量进入 enum 类型位置 → check 诊断
运行时 nil 接口或 typed-nil 变体到达分派点 → panic，不能由 _ arm 捕获
```

合法性检查局限于正在分派的 enum；嵌套变体模式触发内层分派，单纯绑定 payload 不递归验证。当前 `Option` / `Result` 仍使用已发布 `std/sum` 的 tagged struct 与原位槽 API；`None` 和 `Some(nil)` 不同，Option 不做指针特化。两种实际表示及未来统一阶段见[能力状态表](../mvp-gap-audit.md)。

比较使用普通 `std::PartialEq` / `std::Eq` trait 和 impl；`Eq` 是依赖 `PartialEq` 的显式语义承诺，不产生方法或编译器专用资格记录。`std::NativeKey` 另行承诺原生 Go map 查找与该类型的相等语义一致，不能从 `Eq` 自动推出。导入具名类型按可达的普通 impl 求解；当前编译器不产生或读取 enum/equality metadata。派生、容器比较及键约束见 [RFC 0031](0031-generic-protocol-impls.md)。

## 历史方案与讨论

以下保留早期设计的比较依据；其中值型 case、旧 metadata 和 ABI 候选不作为当前生成合同。

2026-10-09 更新：本轮直接切换至内联独立 payload 槽、`ref` / `ref mut` 模式和 `&mut` 引用拼写，详见[当前原位 payload 合同](../design/enum-payload-borrows.md)。下文旧 interface/case、旧 Go case API 和布局待定的章节记录历史阶段，不是当前接口或兼容承诺；完整 Rust 模式规则不由本轮实现。

### 状态与范围

变体枚举、Option / Result、名义类型身份是已确认目标；本分支已实现有限值型case子集；完整RFC尚未实现。
源码拼写、泛型验证能力和 Go ABI 仍是草案。下文 interface 加值型 case 只是一种候选，不能据此排除借用模式。
本篇约束枚举值的表示、初始化和转换；分支执行与借用绑定见 [RFC 0009](0009-match.md)。
背景与已有 Go 机制实验见[枚举专题](../design/enums-and-match.md)。

### 源级规则

最新D27覆盖旧开放raw/C enum：只保留封闭变体。数值/string属于serde外部映射，未知输入返回UnknownVariant而非合法未知值；业务validator延期，不取消结构边界检查。具体属性与反例见[当前enum规则](../design/enums-and-match.md#01-封闭-enum-与外部映射)。

- 一个合法 enum 值恰好属于一个声明的变体，payload 满足对应类型约束。
- 类型身份包括定义包、枚举声明及类型实参；同名变体不使不同 enum 互换。
- `Option::None`、`Option::Some(nil)`、`Option::Some(Option::None)` 表达不同状态，不得展平。
- 普通 enum / Result 不自动取得首个变体作为默认值；Option 默认值拟定为 None。
- 普通赋值延续 Go 值复制方向，不自动执行 move 或递归深复制；含借用值仍受权限检查。
- 首版候选以无 payload 或单 payload 变体为基础；复合数据可放入 struct，多字段语法另定。

### 转译示例

**E01：构造保留变体、payload 和全部类型参数。**

```goxide
pub enum Result[T, E] { Ok(T), Err(E) }
var r Result[int, string] = Result::Ok(7)
```

```go
// 候选 A：interface + 值型 case；符号名仅为示意。
type Result[T, E any] interface { goxideResult(T, E) }
type Ok[T, E any] struct { Value T }
type Err[T, E any] struct { Error E }
func (Ok[T, E]) goxideResult(T, E) {}
func (Err[T, E]) goxideResult(T, E) {}
var r Result[int, string] = Ok[int, string]{Value: 7}
```

对应约束：marker 包含 E，即使 Ok 不存储 E。不能使 `Result[int, string]` 与 `Result[int, bool]` 意外兼容。
成本：接口装箱可能产生分配；不能承诺所有实例零分配，最终以选定 ABI 和 Go 编译结果为准。

**E02：嵌套 Option 保留层级。**

```goxide
var a Option[Option[int]] = Option::None
var b Option[Option[int]] = Option::Some(Option::None)
var c Option[Option[int]] = Option::Some(Option::Some(3))
```

```go
// 仍采用候选 A；只用 nil 表示外层 None。
type Option[T any] interface { goxideOption(T) }
type Some[T any] struct { Value T }
func (Some[T]) goxideOption(T) {}
var a Option[Option[int]] = nil
var b Option[Option[int]] = Some[Option[int]]{Value: nil}
var c Option[Option[int]] = Some[Option[int]]{Value: Some[int]{Value: 3}}
```

对应约束：a、b、c 必须能被模式匹配区分；不能以“底层都是 nil”消除 b 的 Some 包装。 此为内存中的名义值语义；普通 JSON Option 表示中 a 与 b 均可编码为 null，解码为外层 None，不承诺 JSON 无损往返或 PATCH 三态，见 [serde RFC](research/0006-serde-core-and-formats.md#72-内建具体-option-派生)。

**E03：没有合法初始化的 Result 不能成为源级可读值。**

```goxide
var r Result[int, string]
copy := r // 拒绝：r 尚未取得合法值。
```

```go
// Go 允许；此代码展示不能直接照搬的降低。
var r Result[int, string]
copy := r // nil interface，不是任何 Result 变体。
_ = copy
```

拒绝来自 goxide 初始化检查，不依赖 Go 编译器。生成代码内部可有临时零值，但不能让业务代码观察它。

**E04：外部值必须按所声明的枚举契约取得合法视图。**

```goxide
// 签名示意；输入位置的验证策略以公开 API RFC 为准。
pub func value(r Result[int, string]) int {
    return match r { Result::Ok(v) => v, Result::Err(_) => 0 }
}
```

```go
func Value(r Result[int, string]) int {
    switch c := r.(type) {
    case Ok[int, string]:
        return c.Value
    case Err[int, string]:
        return 0
    default:
        panic("invalid Result[int,string]")
    }
}
```

这是候选 A 的标量验证示例，不能直接推广到任意泛型 T。校验不调用 marker 或其他用户方法。

### 拒绝与边界

- 候选 A 中，Go 允许 `*Ok[int, string]` 满足接口；它仍不是规定的值型 case，应拒绝，包括 typed nil。
- 嵌入合法 case 的其他 Go struct 不因继承 marker 而成为新变体；白名单核对实际动态类型。
- `Result::Err(nil)` 是否合法由 E 决定；它与整个 Result 为 nil 分开判断。
- 容器扩长、缺失查询、关闭 channel 及泛型零值不能暴露非法 Result；具体容器入口见 [RFC 0010](0010-collections.md)。
- 验证共享容器只说明当次读取的值；共享写入、同步和借用仍需独立契约，不能靠一次扫描永久证明。

### 双向互操作

goxide 导入带元数据的生成包，应恢复 enum 身份、变体与约束；普通 Go interface 不自动推断为封闭 enum。
原生 `(T, error)` 改成 Result 需要实际桥接，并保留原 API 允许的“值和错误同时存在”信息；declare 擦除不能改变结果形状。
Go 调用生成包时仍使用普通 Go 类型与工具链；goxide 不向第三方 Go 程序追加静态穷尽性或借用规则。
需要公开的运行时校验随 dist 交付，其位置、失败方式及成本必须记录；不能承诺原始 Go 任意输入都满足 goxide 契约。
最终表示须兼容 [RFC 0009](0009-match.md) 的三种绑定，导出 ABI 和内部布局是否分离尚未选择。

### 验收与未定项

当前真实验收：[enum_wire业务](../../examples/enum_wire/project/main.gox)与[独立测试](../../tests/acceptance/enum_wire_test.go)，包含安装CLI、Go双向调用、known往返、unknown错误先于业务。旧raw语法是前端拒绝例；数值映射不是任意Go强转。下列未实现的完整范围仍需独立验证。

1. 正例 E01、E02 保留类型参数、变体及嵌套层级；E03 在 goxide 阶段拒绝。
2. E04 对规范标量 case 通过，对 nil、指针 case 和嵌入伪造值拒绝。
3. 增补带共享字段、递归数据、含借用 payload 及跨包元数据往返样例。
4. Go 机制测试与 goxide 端到端验证分开记录；当前没有后者的实现。
5. 未定：最终 ABI、递归验证、泛型验证证据、完整初始化算法及稳定导出符号。
6. 不得先冻结值型 case，再把无法实现的原位借用默默降低成副本修改。

#### 独立 enum 稳定阶段与声明分隔符（2026-10-03）

最新用户顺序为宏 → enum → 指针拆分。当前独立 enum 阶段保持 interface＋值型 case，不引入 Copy/move 或新借用模型；未来原位 payload 可写借用仍是演进目标，不再作为本阶段前置。既有引用功能保留。此调整不承诺 case 布局永久兼容，也不自动决定比较/map key 资格、非法零值或所有 Go 入口的校验政策。

已修正 `enum E { A, }; func f...` 中显式声明分号残留、导致生成 Go 顶层空声明报错的问题。仅移除 enum 后紧随的显式分号，保留间隔注释和自动换行分隔；不改变 match 语义。原来把合法表达式 match 当负例的测试迁为实际执行正例，多文件/pub enum、混合普通声明、同线多 enum、注释、EOF 与真正语义错误见[回归验收](../../tests/acceptance/enum_separator_test.go)。

#### 跨 module 公开具体 enum 恢复（2026-10-03）

目录构建输出内部 `.goxide.enums.json`（当前 metadata schema 8 / artifact v1；旧命名产物需重新生成）和 `GoxideEnumMetadata` 常量。消费者在 enum 语法 lowering 之前恢复闭合变体集合，并检查真实 Go enum 类型、私有 marker、值型 case、构造器参数/返回和 validator 签名。元数据不保存 AST 或生产者私有载荷名；载荷类型由真实 case.Value 与构造器签名提供，所以私有类型的公开字段读取、转发和重构可继续推断。

产物绑定实际包路径/名称、逻辑元数据摘要、选中 Go 文件、源闭包与目标构建配置，递归绑定增强依赖 artifact；复用引用阶段的离线 importer 指纹机制。enum/ref 两种 marker 均先写入最终 Go，再计算各自产物，不使已有引用文件摘要失效。缺失/陈旧/错包/版本/构建变化在消费 import 处诊断；这不是生产者认证，外部 cgo 输入限制沿用现有边界。

[真实双模块验收](../../tests/acceptance/enum_external_test.go)覆盖文件级 import alias、公开透明 type alias 再导出、函数转发、闭合构造/穷尽 match、guard 前非法动态值拒绝、私有载荷推断、同名不同包隔离及定义新类型不冒充 alias。生产者已生成的 serde/Clone/Default 辅助函数可以直接 Go 调用；原生 Go 消费者不依赖 JSON。库与应用两次生成一致，独立模块分别 race/vet，引用权限恢复继续成立。

首片限定公开具体 enum 与源可用 local replace。消费者跨包 derive 协议、外部通用 Option/Result 家族元数据、单文件外部 enum 语法恢复未由本片完成；已有同 module builtin 能力保留。透明 alias 若指向消费者不可导入的 `internal` 定义包，普通公开 Go API 仍可使用，实际 enum 模式/构造语法明确诊断未支持；是否增加公开 relay helper 另需收敛，不冻结为永久延期。

本片不迁移 interface＋值型 case，不引入 Copy/move，不改变比较/map key、非法零值或全公开入口的验证策略。元数据与具体 case 导出仍为内部/版本耦合协议，不承诺未来 payload 原位引用或 ABI 迁移无破坏。

#### 跨 module Option/Result 家族（2026-10-03）

内部 enum 协议升级为 **v2**（产物 envelope 仍 v1，旧协议需重新生成）。`Families` 来自实际 builtin registry，独立记录 canonical family、固定顺序的 Some/None 或 Ok/Err、公开 generic case/constructor 与生产者已生成的具体 instance validator。只出现 builtin 的包同样产生 marker/artifact。消费者检查真实 Go 泛型 origin、全部 phantom 参数、透明 alias、case/constructor 精确类型和 validator 参数身份；名称或接口形状不授权协议。

目录/local replace 恢复支持 `dep.Option[int]::Some(...)`、None、Result 构造、穷尽 match、既有 `?`；具体 alias、泛型参数换序、部分应用与复合实参从实际 Go alias 求得 canonical 参数。替换只作用于类型引用，不改 struct 字段名或函数参数名；生成代码保留源 alias 的实例化，Go 类型检查继续验证其约束。构造本身沿现有 helper 行为，不新增全入口验证；match/`?` 必须找到真实匹配的生产者 instance validator，不能退回只检查外层 case 的泛型 validator。

私有载荷有实际公开透明类型 alias 时可保持类型身份；没有可见类型拼写的载荷、带私有字段且不可命名的匿名形状仍是增强操作的工程缺口，普通 API 调用/转发保留。native generic aliases 可消费，不代表 `.gox` 已支持用户泛型定义。internal origin facade、无源产物、单文件外部 enum、消费者跨包 derive 仍未由本片补齐，不视为获批延期。

[双模块执行验收](../../tests/acceptance/builtin_external_test.go)区分真实 goxide 构造/match/传播与第三个原生 Go 消费模块，检查 None、Some(None)、typed-nil、非法动态 case、嵌套非法载荷、别名约束与 metadata 缺失/篡改。serde 生产者 helper 的 Option 缺失/null 仍为 None，不依赖 Default。比较/零值/入口策略未在本片修改。

#### 显式 PartialEq/Eq 首片（2026-10-03）

已接通具体 scalar、struct、array 和 enum 的 `#[derive(PartialEq)]` / `#[derive(PartialEq, Eq)]`，实例 `a.equals(b)` 静态调用已登记的 `EqualsT(a,b T) bool`。Eq 是依赖 PartialEq 的资格标记，不产生第二套方法；单独 derive Eq 在尚无 PartialEq 实现时拒绝。本片未实现手写协议或一般 trait 求解。

PartialEq 按字段/元素递归比较；enum 先验证左参数、再验证右参数，然后比较变体和活跃载荷。两个实参先完成既有表达式求值，helper 不在两实参之间插入验证。Eq 要求全部载荷有 Eq；float/complex 的普通相等只支持 PartialEq（NaN 不自反），不能派生 Eq。具名字段类型需要明确协议，普通方法名或 Go comparable 不构成注册。无写副作用的值参数继续采用现有 Go 风格传入，不引入指针拆分。

生成 helper 以保留名绑定防局部同名劫持；布尔常量不引用可遮蔽的 true/false 标识符。scalar equals 同样返回 **typed bool**，不能将比较表达式的 untyped bool 语义泄漏到调用方。需要生成 bool 返回类型时，包级遮蔽 builtin bool 仍给明确当前限制诊断；scalar helper 所需 comparable 被包级遮蔽也单列限制；不改无此功能的普通 Go 代码。

goxide 中两个已知 enum 的直接 `==`/`!=` 给出使用显式 equals 的诊断；现有 enum 与 nil 比较保留。已知包含不可比 enum 载荷的 struct/array、混合 interface 比较、switch tag/case 和原生 map 键使用也在编译期拒绝，含 map[any] 的已知 enum key 及 builtin delete。只按实际类型处理已知 enum，不做全程序 interface/alias 风险分析；普通原生 Go 文件与无 enum 的 Go 比较不受此检查影响。安全载荷的既有 Go map key、expression switch 规则未扩大新许可，Eq 不自动赋予 Go map key 资格；对 nil 与其他隐式比较的最终协议边界仍单列。

历史 enum metadata **v3** 曾记录 Equality：真实目标身份、helper 与 Eq 标记；当时的 equality-only 包也生成产物，透明 alias 核对定义包记录与 `(T,T) bool` 实际签名，导出范围限于公开 canonical 类型。该记录属于已删除的旧管线；当前跨包比较和派生按本文开头的普通 trait/impl 合同处理，不恢复此 metadata 协议或其公开 alias 限制。

[真实模块验收](../../tests/acceptance/equality_test.go)验证 NaN、变体/递归字段、array、上下文大整数、`?`、全局初始化、调用遮蔽、参数求值及左右非法值顺序、别名与普通 Go helper 消费。blank/embedded 字段的 struct 派生、builtin Option/Result 的条件泛型比较协议、slice/map 内容相等及 nil/空规则、pointer/chan/interface 自动协议、手写权限入口、Hash/HashMap 均未实现，不由本片自动延期。

后续容器规则已由 [D33](../design/decisions.md) 确认：同类型 nil slice/map 与非 nil 空容器不相等，两个 nil 相等，两个非 nil 空容器按内容相等；Option.None/Some(nil)/Some(empty) 区分，serde 不变。固定长度 array 无 nil。对应 [纯 Go 契约探针](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/tests/design/enum/equality_contract_test.go)不冒充尚未实现的 goxide 容器派生；nil/空不再待决，map 键规则仍单列。

#### 容器 PartialEq/Eq 派生（2026-10-03）

按 D33/D34 扩展显式派生到 slice/map 及其有限嵌套：先区分 nil 与非 nil，再比较长度、元素或键对应的值。两个 nil 相等；非 nil 空值相等；nil 与非 nil 空值不相等。数组仍没有 nil。递归调用已登记元素/值协议，float/complex 仅允许 PartialEq，不使用 reflect.DeepEqual，也不按相同容器地址跳过内容（否则 NaN 语义会被改变）。map 查找必须同时检查键是否存在，缺失键不能与零值混淆。

旧管线首片 map 键证明限于基础 bool/integer/string、透明 alias、有限结构/数组和本地派生具名键；当时 equality metadata 不携带原生键一致性证据，因而拒绝导入具名键。当前这一限制已由普通 `NativeKey` trait/impl 合同替代：基础及组合类型按库 impl 求解，导入具名键可提供显式实现；`Eq` 本身仍不赋予原生 map key 资格。此处的旧白名单不再作为当前编译器规则，也不代表已实现 HashMap。

struct 依字段顺序、slice/array 依索引短路；map 依 Go 未指定的迭代顺序逐键查找并短路，不承诺不同键之间的校验顺序。两个用户实参先求值，再进入 helper。访问到 enum 值时沿现有 validator 左后右；不为普通 struct/container 增加全值预扫描。外层 enum 则仍先校验两侧，再比较活跃载荷；为容器载荷补全有限 array/slice/map 的递归 enum validator，非法 nil/typed-nil/伪造值不会参与原生 interface 内容比较。

在派生字段/元素中，已登记的具体 Option/Result 实例按载荷资格条件比较，None、Some(nil)、Some(empty) 保持区分，Result 的非法 nil 仍拒绝；不改 serde 协议；validator 的有限容器投影也让此前拒绝的嵌套 Option 容器进入既有 serde 路径，旧负例迁为真实执行验收。直接匿名容器或 Option/Result 的 `.equals` 尚未新增独立入口。自递归容器可能构成运行时环，本片保留明确诊断，不擅定环等价关系或退回任意动态深比较。手写协议、指针拆分与原位 borrow 留后续独立阶段。

#### 手写具体 PartialEq/Eq（2026-10-03）

本片沿已批准的具体值参数 helper 路径支持手写比较，不等待 `&T`/`&mut T` 或一般 trait 求解。源级入口使用明确具体类型拼写；首参数必须名为 `self`，两个参数均为目标值类型，单个返回值为 builtin bool。没有 static，不增加隐式只读、独占或全局不可变保证；函数体按普通 Go 值参数和共享容器语义执行。`Self` 简写、泛型 impl、别名/外部目标和借用签名不在此具体入口内。

```goxide
type User struct { ID int; Display string }
impl PartialEq for User {
    func equals(self User, other User) bool { return self.ID == other.ID }
}
impl Eq for User {}
#[derive(PartialEq, Eq)]
enum Event { User(User), Closed, }
```

降低为原函数体所在的唯一包级 `EqualsUser(self User, other User) bool`，`.equals` 及外层派生均调用已登记 helper；不会用 Go `==` 跳过手写业务规则。普通 Go 调用方也使用这个 helper。手写 enum 的同一 helper 函数体开头插入左、右 validator，先于用户代码，两个调用实参仍先求值；不另留未校验 body helper，也不加函数帧破坏直接 `recover()`。此入口检查不扩成所有公共函数的自动检查。

同一目标、同一协议只能有一个来源：重复 impl、manual 与该协议 derive 冲突以及导出 helper 碰撞均诊断，跨文件一并检查。手写 Eq 要求已有 PartialEq，但只是作者的等价关系承诺，编译器不能证明任意函数的自反、对称、传递性质。derive PartialEq + 手写 Eq 可组合；手写 PartialEq + derive Eq 明确拒绝，需作者另写空 Eq impl，不能从载荷性质推出任意业务函数的性质。纯 derive Eq 的递归资格检查保持不变。

手写 PartialEq 不自动成为原生 map 键协议：即使底层全是整数/string，业务相等也可能与 Go 查找不同；含该字段的结构键同样拒绝自动内容派生。手写 Eq 本身不增加 map 键资格；若 PartialEq 仍由派生生成且原先的结构一致性证明成立，则不因 Eq 的手写标记误拒。float/complex 的默认 PartialEq 不变；用户可以显式编写把 NaN 视为相等的具体 wrapper，并自行承诺 Eq。

metadata v4 在已验证身份、helper 签名和 Eq 资格之外记录 PartialEq/Eq 各自的 derive/manual 来源，透明 alias 必须与定义包来源一致，不能通过 relay 改写来源。内部协议升级需重生成产物，不修改普通 Go ABI。目标仍限具体 scalar/struct/array/slice/map 与已登记 enum；不引入用户泛型、自动循环图比较或额外 trait dispatch。宏模板生成比较 impl 仍受上游语法限制，不由注册阶段的位置推导为已支持。手写体中的源级 enum 直接比较限制仍保持；业务规则可以显式匹配变体。

当前内部协议已升级 **v5**，新增显式 Default 注册及 canonical 来源核验，Default-only 包也发行 artifact；具体消费边界见[派生 RFC](0013-derive.md#跨包已登记-default-字段2026-10-03)。不改变 enum ABI、外部载荷类型范围、零值或公开入口验证契约。

内部协议 **v6** 新增显式 Clone/Debug 字段协议记录，复用 v5 的 typed identity、canonical alias 和内容/依赖验证；见[派生 RFC](0013-derive.md#跨包已登记-clonedebug-字段2026-10-03)。不改变闭合 enum 表示或公开边界契约。

### 消费者透明 alias 与协议身份（2026-10-03）

本地包级多级 Go `=` alias 可恢复外部 canonical 的已登记 Default/Clone/Debug/Serialize/Deserialize 与 PartialEq/Eq，包含 alias 在 native `.go` 中声明的场景。身份由 Go 类型系统 Unalias/Identical 核验，new defined 及同名局部类型不冒继承；metadata v6 的来源/签名/产物校验不变。实现与真实三模块证据见 RFC 0013 和 `tests/acceptance/local_protocol_alias_test.go`。此项不改变 enum 表示、合法值或公开函数入口验证契约。

#### A06 单文件外部恢复实施合同（2026-10-06）

本片按先提交的 [RFC0001 A06 合同](0001-package-loading.md#a06-已知-go-module-的单文件外部恢复2026-10-06实施前合同)
接通配置已知的单文件 consumer；复用当前 schema 8、真实 canonical enum/family 身份
及完整 artifact 校验链。不新定 enum 表示、公开入口验证、internal-origin relay 或
private payload 的新发布协议。实际交付、正反例和准确 SHA 见
[单文件外部恢复报告](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/single-file-external-recovery-20261006.md)，不以合同本身作为测试证明。

### 具体 builtin sum 字段协议（2026-10-03）

已登记具体 Option/Result 的 Clone/Debug 字段组合已接通，依据现有 validator 参数的 Go 类型身份，不按 family 拼写推断。None 与 Some(nil/empty) 保持不同变体/载荷状态；Result 只运行活跃分支协议，非法动态 case 仍拒。Option Default 仅为 None，载荷 Default/可命名性不是前提；Result 不获得隐式默认值。跨文件及跨 module 实际验收见 RFC 0013 和 `tests/acceptance/sum_clone_debug_test.go`。不改变公开入口或指针阶段契约。

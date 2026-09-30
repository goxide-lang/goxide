# 普通关联类型

状态：采用普通关联类型的方向已纳入设计；以下为最小语义草案。非泛型 trait 适配可先实现，不必等待完整关联类型系统。所有运行时 impl 视图沿用统一 wrapper、导出 marker 与固定包级工厂；尚无 hgo 编译器实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D04、D24：纳入普通关联类型方向；运行时视图采用统一 wrapper、marker 和固定工厂。 |
| 暂定 | T08、T12：关联类型唯一性、显式绑定、投影拼写、固定 Go 接口与静态选择。 |
| 未定 | U04：完整泛型约束和后端策略；GAT、特化延期；Option ABI 尚未冻结。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 hgo 功能已实现。

本文旧示例中的 `*Counter` 等是接收者 Go 身份的示意，不增加通用 safe 指针类型；引用权限与 trait 视图的有效范围不能在关联类型替换或泛型降低时丢失，见[源级权限约束](overview.md#35-源级权限与-go-表示必须分开)。

## 1. 解决的问题

trait 类型参数参与选择实现；关联类型由选定的实现决定。二者不是两种完全等价的拼写。

例如 `Iterator[Input]` 可以按不同 `Input` 选择不同 impl；而 `Iterator.Item` 表达某个迭代器实现产出的类型。Rust 官方文档对泛型 trait 参数与关联类型作了相同的职责区分，此处借用概念，不照搬全部 Rust 能力。[关联类型说明](https://doc.rust-lang.org/book/ch20-02-advanced-traits.html)

## 2. 最小语法

```hgo
pub trait Iterator {
    type Item
    next() Option[Self.Item]
}

impl Iterator for *Counter {
    type Item = int
    // next 的方法体定义在对应 impl 内，随后降低到 wrapper。
}
```

上例省略方法体，是类型关系示意，不是完整实现。有体 impl 必须完整定义方法，其关联类型在 body 内用 `type Item = ...` 给出。无方法体的 `declare impl` 在头部给出关联类型绑定，在 wrapper 中逐项转发现有业务方法并保留 marker，见第 5 节。两种形式都必须显式给出全部关联类型，不从方法返回类型反向猜测，也不提供默认值。

`Self.Item` 在 trait 内引用其关联类型。外部泛型代码使用限定投影 `T::Iterator.Item`；跨包可写 `T::iter::Iterator.Item`。这组拼写是草案，解析时 trait 引用以最后的 `.Item` 为界。

只有已证明 `T` 满足对应 trait 实例，才能使用投影。多个 trait 都有 `Item` 时必须限定；首版不增加依赖候选搜索的 `T.Item` 简写。

## 3. 实现唯一性与接口值

关联类型由 `(接收者类型, trait 身份, trait 参数)` 所选中的 impl 唯一决定，不增加 impl 的选择维度。不能针对同一个非泛型 `Iterator` 与同一个接收者，分别声明 `Item = int` 和 `Item = string` 两份实现。

运行时 trait 值必须绑定全部关联类型：

```hgo
var it Iterator[Item = int]
```

这里 `Item = int` 是关联类型相等约束，不是普通位置类型参数。若 trait 还有位置参数，先写位置参数，再写具名关联类型绑定；首版不支持省略部分绑定。

裸 `Iterator` 可以在 trait 声明和泛型约束位置出现；不能作为带未知 `Item` 的普通变量、参数或返回值类型。完整的泛型函数约束语法另行定义。

首版可用作运行时视图的 trait 还要求方法签名在替换关联类型后能够形成固定的 Go 接口。除接收者语义和关联类型投影外，在参数/结果中暴露裸 `Self` 的方法、方法级类型参数等动态分派能力先不支持，并给出诊断。

## 4. Go 降低

固定的关联类型可以成为后端接口的类型参数：

```go
// 片段；Option 的最终 Go 表示由枚举专题定义。
type Iterator[Item any] interface {
    Next() Option[Item]
    HyperGoTraitIterator()
}
```

hgo 的 `Iterator[Item = int]` 对应后端 `Iterator[int]`，不是允许 hgo 用户为同一个接收者任意挑选 `Item`。

Go 接口参数只承载解析后的类型；marker 表达对应 trait 的显式采用。marker 定义在 impl 包的 wrapper 上并采用导出名称，使跨包 wrapper 可以实现接口；上例短名称不是正式 ABI。关联类型选择、投影正规化、等式约束、精确显式实现与 impl 唯一性仍由 hgo 检查，marker 不是安全证明。不能把某份 Go 代码能够编译作为 hgo 关联类型语义成立的充分条件。

泛型函数中未具体化的投影，可能降低为附加类型参数及实现证据，也可能使用专门化；具体策略与泛型后端一起定稿。无论使用何种策略，不能在语义层把泛型实参 `T` 直接替换成一个接口 wrapper。

关联类型描述实现选择的类型结果；类型自有的无 self 关联函数属于 [RFC 0020](../rfcs/0020-associated-functions.md)。构造函数按类型解析，不因此成为静态 trait 实现，本文不冻结静态 trait 成员的声明语法。

## 5. 直接采用原生 Go 方法

实现关联类型支持后，用户在自己的 C 包普通 `.hgo` 文件中使用具名绑定；本文统一采用 `declare impl` 的草案拼写：

```hgo
import iter "example.org/hgo/iter"
import native "example.org/native/counter"

declare impl iter::Iterator[Item = int] for *native::Counter;
```

这份声明属于 C，由 hgo 根据 C 的源码及显式导入的 impl 元数据使用；不会登记成 `native` 包对所有项目公开的实现。`Item = int` 声明这个实现的关联类型，不增加 impl 键的选择维度；可见实现集合中不能再为同一个 `*native::Counter` 和非泛型 `Iterator` 声明 `Item = string` 的第二个 impl。

编译器检查 `*native::Counter` 的真实业务方法集。对应的 Go `Next` 必须与代入 `Item = int` 后、按最终选定且版本一致的 `Option[int]` ABI 降低出的签名一致；本文只采用枚举专题的当前候选表示，不表示 ABI 已冻结。检查通过后，在 C 包生成独立 wrapper、显式转发方法、导出 marker 和固定包级工厂。原生包无需 marker 或 As，不向其追加任何方法。

```go
// C 包的机制片段；Iterator / Option 代表已解析的生成接口类型。
type counterIterator struct { target *native.Counter }
func (v counterIterator) HyperGoTraitIterator() {}
func (v counterIterator) Next() Option[int] { return v.target.Next() }
func AsCounterIterator(c *native.Counter) Iterator[int] {
    return counterIterator{target: c}
}
```

该片段的接收者检查按选定的源级权限契约补齐，不因最终都是 Go 指针就免检。target 保持原始指针身份，接口动态类型成为 counterIterator；wrapper 使用命名字段，不提升原生 Counter 的其他方法。

工厂随 C 的普通 Go 源码发布。Go 用户可以使用 `C.ForCounter(c).AsIterator()` 一类仅转发工厂的 helper；只有类型与 impl 同包，且属于 Go 允许定义方法的本地 named type，才能在原类型生成同样只转发工厂的 As 方法。`type Ptr[T any] *T` 等具名指针类型不满足 Go 接收者规则，仍须使用包级工厂或 facade。具体导出名称和类型参数编码未定，不能让 helper 根据上下文改变 Item 或所选 impl。

如果原生 `Next` 返回 `(int, bool)`，无体声明不能自动把它转为 `Option[int]`，必须报告签名不匹配。用户可以改写为有方法体的完整 impl，在 body 中声明 `type Item = int` 并显式完成转换；它与 declare 使用同一 wrapper 机制，但执行用户方法体，不会被原生同名方法覆盖。签名适配与调用规则见 [Go 原生类型的显式适配](go-interop.md)。

原生泛型类型的实例化与约束仍需验证，`T` 和 `*T` 不可混同。完整的泛型 impl 声明头与投影降低策略由泛型专题补齐；在关联类型能力实现前，工具必须给出明确的不支持诊断。

## 6. 首版范围与验收条件

支持普通关联类型、显式定义、限定投影和接口值的完整绑定。暂缓 GAT（关联类型自身带参数）、默认关联类型、特化、重叠实现、存在类型接口值，以及复杂父 trait 推导。

未来验收要求：

1. 缺失、重复和未知关联类型绑定均有诊断。
2. 同一 impl 键不能通过不同关联类型重复声明。
3. `Iterator[Item = int]` 与 `Iterator[Item = string]` 不能相互赋值。
4. 泛型投影只在具有相应实现证据时有效；冲突名称必须限定。
5. 无体声明对原生 Go 业务方法在代入关联类型后逐项验签，通过后在 wrapper 显式转发并保留 marker；不匹配时拒绝。
6. 未绑定的 trait 值、GAT 及暂不支持的动态分派形式被明确拒绝。
7. 固定工厂与便利 helper 保持相同关联类型和 impl 身份，原生类型无新增方法；Go 接口中的 marker 不能代替 hgo 元数据。

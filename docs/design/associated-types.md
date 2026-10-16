# 普通关联类型

状态：P4 已在新管线完成。关联类型是所选 impl 的类型结果；Go 侧使用普通类型参数和固定签名。运行时视图沿用 wrapper、导出 marker 和固定工厂；泛型函数通过空 carrier 的类型 witness 推导关联类型。

| 状态 | 范围 |
| --- | --- |
| P4 | 显式完整绑定、`Self::Item` / `T::Trait::Item`、固定 Go 接口、单 trait 泛型约束与 carrier witness。 |
| 后续 | 泛型 trait/impl、多 trait 约束、GAT、默认关联类型、特化、重叠实现及未知关联类型的存在视图。 |

状态编号见[决策登记](decisions.md)。完整 P4 验收须走新管线生成并运行 Go。

有体实现以基础类型为目标，self 参数声明读写权限。关联类型替换和泛型降低不能擦除该权限，见[源级权限约束](overview.md#35-源级权限与-go-表示必须分开)。

## 1. 解决的问题

trait 类型参数参与选择实现；关联类型由选定的实现决定。二者不是两种完全等价的拼写。

例如 `Iterator[Input]` 可以按不同 `Input` 选择不同 impl；而 `Iterator.Item` 表达某个迭代器实现产出的类型。Rust 官方文档对泛型 trait 参数与关联类型作了相同的职责区分，此处借用概念，不照搬全部 Rust 能力。[关联类型说明](https://doc.rust-lang.org/book/ch20-02-advanced-traits.html)

## 2. 最小语法

```goxide
pub trait Iterator {
    type Item
    func next(self &mut Self) Option[Self::Item]
}

impl Iterator for Counter {
    type Item = int
    // next 的方法体定义在对应 impl 内，随后降低到 wrapper。
}
```

上例省略方法体，是类型关系示意，不是完整实现。有体 impl 必须完整定义方法，其关联类型在 body 内用 `type Item = ...` 给出。无方法体的 `declare impl` 在头部给出关联类型绑定，在 wrapper 中逐项转发现有业务方法并保留 marker，见第 5 节。两种形式都必须显式给出全部关联类型，不从方法返回类型反向猜测，也不提供默认值。

`Self::Item` 在 trait/impl 内引用其关联类型。外部泛型代码使用限定投影 `T::Iterator::Item`；路径中的 trait 可以带包限定，如 `T::iter::Iterator::Item`。所有静态段统一使用 `::`，不接受 `Self.Item` 或未限定的 `T::Item`。

只有已证明 `T` 满足对应 trait 实例，才能使用投影。多个 trait 都有 `Item` 时必须限定；首版不增加依赖候选搜索的 `T.Item` 简写。

## 3. 实现唯一性与接口值

关联类型由 `(接收者类型, trait 身份, trait 参数)` 所选中的 impl 唯一决定，不增加 impl 的选择维度。不能针对同一个非泛型 `Iterator` 与同一个接收者，分别声明 `Item = int` 和 `Item = string` 两份实现。

运行时 trait 值必须绑定全部关联类型：

```goxide
var it Iterator[Item = int]
```

这里 `Item = int` 是关联类型相等约束，不是普通位置类型参数。若 trait 还有位置参数，先写位置参数，再写具名关联类型绑定；首版不支持省略部分绑定。

裸 `Iterator` 可以在 trait 声明和泛型约束位置出现；不能作为带未知 `Item` 的普通变量、参数或返回值类型。P4 的泛型函数使用单 trait 约束，如 `func Read[T Source](value T) T::Source::Item`。

可用作运行时视图的 trait 要求方法签名在替换关联类型后形成固定 Go 接口。除接收者和关联投影外，参数/结果暴露裸 Self 的 trait 不能形成运行时值；其具体 impl、静态操作和受约束泛型调用仍可使用，实例业务保留单一实现。方法级类型参数继续延期。

## 4. Go 降低

固定的关联类型可以成为后端接口的类型参数：

```go
// 片段；Option 的最终 Go 表示由枚举专题定义。
type Iterator[Item any] interface {
    Next() Option[Item]
    GoxideTraitIterator()
}
```

goxide 的 `Iterator[Item = int]` 对应后端 `Iterator[int]`，不是允许 goxide 用户为同一个接收者任意挑选 `Item`。

Go 接口参数只承载解析后的类型；marker 表达对应 trait 的显式采用。marker 定义在 impl 包的 wrapper 上并采用导出名称，使跨包 wrapper 可以实现接口；上例短名称不是正式 ABI。关联类型选择、投影正规化、等式约束、精确显式实现与 impl 唯一性仍由 goxide 检查，marker 不是安全证明。不能把某份 Go 代码能够编译作为 goxide 关联类型语义成立的充分条件。

泛型函数中未具体化的投影降低为附加 Go 类型参数，类型关系由 `std/typeinfo` 空 carrier 中的 witness 方法签名提供。生成的具体目标类型有精确 Self 类型锚；`typeinfo.Of[T]()` 返回 carrier 零值。类型锚与 witness 只参与推导，不调用，不创建目标对象，不增加运行时字典参数。T 始终是源目标类型，不能替换成运行时接口 wrapper。

以成员为 `func get(self Self) Self::Item` 的 Source 为例，源级 `Read[T Source](value T) T::Source::Item` 的 Go 形状如下；Ops 的操作转调选定 wrapper，实例业务体仍只有一份：

```go
package example

import "github.com/goxide-lang/std/typeinfo"

func Read[
    T typeinfo.CarrierFor[T, Ops],
    Ops interface {
        ~struct{}
        GoxideAssocSourceItem() Item
        GoxideSourceGet(T) Item
    },
    Item any,
](value T) Item {
    return typeinfo.Of[T]().GoxideSourceGet(value)
}
```

普通 Go 调用方写 `Read[Box](value)`，Ops 由 Box 的精确锚推导，Item 由 witness 的结果类型推导；不必命名私有 carrier 或补写 Item。witness 的生成名按 trait 与关联成员的规范身份固定，同名 trait 的 Item 不得共用一个推导槽。具体实现只承诺登记的关联绑定，不能根据调用者期望类型改变 Item。

enum E 的接口声明精确 `E → EOps` 类型锚，每个指针变体返回同一空 EOps。`T = E` 时仍可推导；读写枚举 owner 的 Ops 方法参数为 `*T` / `*E`，不需要为 `*E` 定义类型锚。详见 [enum 表示](enum-representation.md#11-p4-的类型锚与实例适配)。

关联类型描述实现选择的类型结果；类型自有的无 self 关联函数属于 [RFC 0020](../rfcs/0020-associated-functions.md)。构造函数按类型解析，不因此成为静态 trait 实现，本文不冻结静态 trait 成员的声明语法。

## 5. 直接采用原生 Go 方法

用户在自己的 C 包普通 `.gox` 文件中使用具名绑定：

```goxide
use iter; // explicit dependency root
use native; // explicit dependency root

declare impl iter::Iterator[Item = int] for native::Counter;
```

这份声明属于 C，由 goxide 根据参与编译的声明使用；不会登记成 `native` 包对所有项目公开的实现。`Item = int` 声明这个实现的关联类型，不增加 impl 键的选择维度；可见实现集合中不能再为同一个 native::Counter 和非泛型 Iterator 声明 Item = string 的第二个 impl。

Self = native::Counter，`self &mut Self` 要求编译器检查精确 `*native.Counter` 方法集。Go `Next` 必须与代入 Item = int 后的 Option[int] Go 签名一致；P6 前继续使用当前 `std/sum` 表示。检查通过后，在 C 包生成独立 wrapper、显式转发方法、导出 marker 和固定包级工厂。原生包无需 marker 或 As，不向其追加任何方法。

```go
// C 包的机制片段；Iterator / Option 代表已解析的生成接口类型。
type counterIterator struct { target *native.Counter }
func (v counterIterator) GoxideTraitIterator() {}
func (v counterIterator) Next() Option[int] { return v.target.Next() }
func AsCounterIterator(c *native.Counter) Iterator[int] {
    return counterIterator{target: c}
}
```

该片段的接收者检查按选定的源级权限契约补齐，不因最终都是 Go 指针就免检。target 保持原始指针身份，接口动态类型成为 counterIterator；wrapper 使用命名字段，不提升原生 Counter 的其他方法。

工厂随 C 的普通 Go 源码发布。Go 用户可以使用 `C.ForCounter(c).AsIterator()` 一类仅转发工厂的 helper；Go 允许定义方法的本地类型可提供 As。enum 值视图通过接口签名和指针变体的一行转发提供 As；借用 enum、外部类型及具名指针类型使用 facade。helper 不能根据上下文改变 Item 或所选 impl。

如果原生 `Next` 返回 `(int, bool)`，无体声明不能自动把它转为 `Option[int]`，必须报告签名不匹配。用户可以为本包定义的适配类型编写有体 impl，在 body 中声明 `type Item = int` 并显式完成转换；它与 declare 使用同一 wrapper 机制，但执行用户方法体，不会被原生同名方法覆盖。签名适配与调用规则见 [Go 原生类型的显式适配](go-interop.md)。

`T` 和 `*T` 的原生方法集不可混同。外部原生类型不注入类型锚，P4 的 declare 提供运行时适配；它不自动让该外部类型满足 carrier 泛型入口。泛型 impl 继续延期。

## 6. 首版范围与验收条件

支持普通关联类型、显式定义、限定投影和接口值的完整绑定。暂缓 GAT（关联类型自身带参数）、默认关联类型、特化、重叠实现、存在类型接口值，以及复杂父 trait 推导。

P4 验收要求：

1. 缺失、重复和未知关联类型绑定均有诊断。
2. 同一 impl 键不能通过不同关联类型重复声明。
3. `Iterator[Item = int]` 与 `Iterator[Item = string]` 不能相互赋值。
4. 泛型投影只在具有相应实现证据时有效；冲突名称必须限定。
5. 无体声明对原生 Go 业务方法在代入关联类型后逐项验签，通过后在 wrapper 显式转发并保留 marker；不匹配时拒绝。
6. 未绑定的 trait 值、GAT 及暂不支持的动态分派形式被明确拒绝。
7. 固定工厂与便利 helper 保持相同关联类型和 impl 身份，原生类型无新增方法；Go 接口中的 marker 不能代替 goxide 元数据。
8. struct/enum 的普通 Go 下游都可省略隐藏的 Ops/Item 实参；anchor/witness 没有生成运行时调用，`*E` owner 的替换保持原槽位身份。

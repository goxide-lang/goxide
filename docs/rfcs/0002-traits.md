# RFC 0002：显式 trait 与实现视图

## 状态与范围

- 已确认：显式实现、期望类型适配；运行时 trait 统一 wrapper 与固定包级工厂，Go 接口保留业务方法和导出 marker。
- 已确认：类型与 impl 同包且满足 Go 方法接收者规则时生成 As 便利方法；其他类型使用含命名字段的 facade，两者都转发同一工厂。
- 暂定：实例视图的方法解析与具体降低细节；下文实例 wrapper 示例仍是设计机制，不是完整 trait 编译器验收。
- 未定：接收权限语法、泛型实现证据、正式符号编码及公开 ABI。

本篇主要约束 `impl Trait for T` 的实例方法。当前新增的用户关联协议子集见[用户说明](../static-protocols.md)：非泛型 `trait`、本包具体 struct/enum 的显式 `impl`、单协议泛型函数约束和 `U::Abc()` / `T::Abc()`，通过空 carrier 与普通 `std/typeinfo.Of` 降低。这不实现下文的运行时视图、实例方法自动适配或泛型 impl。既有业务派生继续按 [RFC 0013](0013-derive.md) 使用。
类型自有的 `impl T { ... }` 见 [RFC 0020](0020-associated-functions.md)，不会自动建立 trait 实现关系。
详细背景见[显式 trait](../design/traits.md)与[决策登记](../design/decisions.md)。
下文 `*Value` 仅标识 Go 接收者身份，不引入 safe 指针语法。
源级引用权限须与 [RFC 0005](0005-borrows.md) 接合；示例不替权限规则定稿。

## 源级规则

1. 方法名和签名相同不自动构成 goxide impl；必须具有显式实现元数据。
2. 按规范化接收者、trait 身份和 trait 参数选择实现；有体实现须唯一。
3. 有体 impl 完整提供必需方法；与原生方法同名也使用 impl 内的方法体。
4. 参数、赋值、返回的期望 trait 类型可触发适配；没有期望类型则保留具体类型。
5. 一次适配只求值一次接收者表达式；不是整个程序只创建一次视图。
6. 已经具有同一 trait 视图的值不重复包装；泛型参数不自动改成接口值。
7. impl 选择、权限、借用来源与有效范围须保留到 IR，不由 Go 方法集反推。
8. 自定义 wrapper 保存原对象；其方法体内调用当前 trait 时不能绕回原生实现。
9. wrapper 与工厂属于 impl 声明包 C，不必属于 trait 包；declare 转发原生方法，有体 impl 执行其方法体，marker 均由 wrapper 实现。
10. goxide 适配使用固定工厂；Go 便利方法不另选 impl。只有类型与 impl 同包且满足 Go 方法接收者规则才生成 `t.AsTrait()`，其他类型使用 `adapters.ForT(t).AsTrait()`。
11. impl 中 `Self` 绑定源级目标类型及其泛型实参，不绑定生成的 wrapper；`self` 绑定实例接收者。self 的字段访问保持原对象语义，当前 trait 方法调用保持选定的 impl。impl 的 func 首参数为 self 时是实例成员，否则为关联函数；没有接收者的成员不获得 self，不需要 static。具体接收类型与权限拼写仍未定。
12. 直接声明的 Go 包函数与接收者方法保留 Go 语义，不自动变为 impl。Self / self 的转译按已解析绑定进行；普通 Go 声明中的同名标识符不被全局替换。

统一表示与便利入口详见 [RFC 0019](0019-uniform-trait-wrappers.md)。静态 trait 操作和泛型实现证据不强制生成实例 wrapper。

## 转译示例

### T01：同名方法使用各自实现

goxide 机制片段；省略包及构造函数。两个 `*Value` 使用上述接收者身份记法。

```goxide
pub trait IntValue { value() int }
pub trait TextValue { value() string }
pub struct Value { n int }

impl IntValue for *Value {
    value() int { return self.n }
}
impl TextValue for *Value {
    value() string { return "custom" }
}
```

对应 Go 为可独立编译的示意包；短 marker 和 As 名称不是正式 ABI。
为了暴露错误分派，额外放入原类型的同名 `Value()` 方法。

```go
package example

type IntValue interface {
    Value() int
    GoxideTraitIntValue()
}
type TextValue interface {
    Value() string
    GoxideTraitTextValue()
}
type Value struct { N int }
func (v *Value) Value() int { return -1 }

type intView struct { target *Value }
type textView struct { target *Value }
func (v intView) GoxideTraitIntValue() {}
func (v textView) GoxideTraitTextValue() {}
func (v intView) Value() int { return v.target.N }
func (v textView) Value() string { return "custom" }
func AsIntValueForValue(v *Value) IntValue { return intView{target: v} }
func AsTextValueForValue(v *Value) TextValue { return textView{target: v} }
func (v *Value) AsIntValue() IntValue { return AsIntValueForValue(v) }
func (v *Value) AsTextValue() TextValue { return AsTextValueForValue(v) }
```

`(&Value{N: 7}).AsIntValue().Value()` 得到 7；TextValue 得到 `"custom"`。
把 IntValue 调用改成原对象的 `Value()` 会得到 -1，属于错误转译。

### T02：适配不重复求值

goxide 机制片段；依赖 T01 以及返回合法接收者视图的 `make_value()`。

```goxide
var i IntValue = make_value()
var j IntValue = i
```

对应 Go 片段；依赖 T01，`makeValue` 是同一工厂的 Go 输出。

```go
tmp := makeValue()
var i IntValue = AsIntValueForValue(tmp)
var j IntValue = i
```

工厂只能执行一次，`j` 复用已有视图。也可直接输出
`var i IntValue = AsIntValueForValue(makeValue())`；临时变量不是强制 ABI。
下一条独立源级表达式再次调用工厂时，仍须再次执行。

## 拒绝与边界

- T03：`type X` 只有 `Value() int` 而无显式 impl，传入 IntValue 时拒绝。
- T04：IntValue 视图隐式变成 TextValue，或隐式恢复为 `*Value`，拒绝。
- T05：同一实现键有两个业务方法体，拒绝；不得按导入顺序取其一。
- T06：期望类型为 `any` 时，不能擅自生成 trait wrapper。
- T07：只有 raw Ptr 的值不得通过 trait 适配获得 safe 引用。
- T08：适配复制工厂表达式、丢失借用范围、重复包装或绕过自定义实现，均为后端错误。
- T09：给外部类型生成 As 方法，或由 Go 便利入口再次搜索 impl，属于错误降低。
- T10：把 impl 中 Self 绑定到 wrapper，或把所有 self 调用一律改为原对象方法，属于错误降低。

wrapper 转发原生方法及声明合并见 [RFC 0003](0003-declare-impl.md)。
模式匹配的值绑定与借用绑定由 [RFC 0009](0009-match.md) 单独决定，
与这里“接收者表达式求值一次”是不同规则。

## 双向互操作

- goxide → Go：先选定 trait 视图，再传给方法集相容的普通 Go interface；
  不在任意 interface 目标下搜索 wrapper 来凑方法集。
- Go → goxide：可调用固定包级工厂、本包类型的 As 方法或外部类型的 facade；后两者转发工厂，不改变实现选择。
- 接口动态类型为 wrapper，其 target 保留输入指针；原类型无需新增 marker。marker 必须导出以供其他包的 wrapper 实现，不是不可伪造证明。
- goxide 重新导入生成包时，依据版本一致的元数据恢复 impl；只有 marker 不足以恢复关系。
- 普通 Go 调用方遵守导出 API 的 Go 契约；goxide 的源级借用检查不自动约束其代码。
- 发布视图和公开边界分别受 [RFC 0001](0001-package-loading.md)
  与 [RFC 0015](0015-public-api.md) 约束。

## 验收与未定项

未来端到端验收须逐项覆盖 T01–T09，并同时观察 goxide 诊断与生成 Go 行为。
工厂计数验证 T02；原生同名方法返回 -1 验证 T01 没有错误分派。
两个视图读取同一对象，验证 wrapper 没有复制本应共享的对象。
包级工厂与便利方法返回相同 wrapper 类型；外部 facade 的对应验收见 RFC 0003。
上述 Go 包只验证后端机制，不能替代 goxide → Go 的端到端测试。

尚须确定接收权限拼写、方法解析完整优先级、正式名称编码、泛型实现证据，
以及哪些 trait 可以形成公开运行时视图；确定前不得宣称相应 ABI 已冻结。

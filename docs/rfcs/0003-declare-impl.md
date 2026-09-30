# RFC 0003：原生实现采用与声明合并

## 状态与范围

- 已确认：用户包可采用外部 Go 类型的已有方法；统一 wrapper 转发，固定包级工厂位于 impl 声明包 C。
- 已确认：trait Go 接口包含业务方法与导出 marker，wrapper 实现 marker；Go 便利入口转发同一工厂。
- 已确认：相容 declare 可合并；有业务方法体的 impl 须唯一。具体相容判定暂定，尚无 hgo 实现。
- 未定：采用原生的 declare 与有体 impl 同时出现时的语义、正式符号编码及权限细节。

本篇细化[声明合并规则](../design/go-interop.md#6-校验来源与唯一性)：
不再仅因两个包独立写了相容 declare 就报重复定义。
`*native::Buffer` 仅表示 Go 接收者身份，权限语法见 [RFC 0005](0005-borrows.md)。

## 源级规则

1. declare、wrapper 与固定工厂属于声明包 C。C 正常依赖 trait 包 A、原类型包 B，不向外部包添加方法。
2. 采用原生先验签，再在 wrapper 上生成业务转发和导出 marker；缺少业务方法仍是错误。
3. 按规范接收者、trait 身份及参数聚合声明；别名不产生新实现键。
4. 相同原生业务绑定、关联类型和调用契约的声明可合并；保留每个来源。
5. 不同但相容的声明怎样合并须有显式规则；不能靠导入先后来放宽权限或改变绑定。
6. 同一生成包内按实现键去重；不同声明包的相容关系合并不使其 Go wrapper 变成同一动态类型。已发布工厂与载体归属固定，不随下游导入集合重新选择。
7. 同一实现键的有体 impl 至多一个；多个有体实现即使文本相同也拒绝。
8. 现有 declare impl 表示采用原生实现，不等同于任意有体实现的前置签名。
   在混合语义定稿前，declare 与有体 impl 共现应诊断，不静默覆盖业务行为。
9. 工厂返回 trait 接口；外部类型的 `ForBuffer(b).AsReadValue()` facade 用命名字段保存 b，并转发同一工厂。
   类型与 impl 同包且满足 Go 方法接收者规则时才生成原类型的 As 便利方法；其他类型使用包级工厂或门面。统一方案见 [RFC 0019](0019-uniform-trait-wrappers.md)。

## 转译示例

### D01：两个使用方合并相同采用声明

以下 hgo 是三个包的机制片段；省略包布局，权限接收者语法尚待接合。

```hgo
// 包 protocols
pub trait ReadValue { read() int }

// 包 client_a
import protocols "example.org/protocols"
import native "example.org/native"
declare impl protocols::ReadValue for *native::Buffer;

// 包 client_b，独立声明相同关系
import protocols "example.org/protocols"
import native "example.org/native"
declare impl protocols::ReadValue for *native::Buffer;
```

原生 Go 包已有业务实现；这是可独立编译的原生包。

```go
package native

type Buffer struct { N int }
func (b *Buffer) Read() int { return b.N }
```

trait 包的 Go 输出（完整包）：

```go
package protocols
type ReadValue interface {
    Read() int
    HyperGoTraitReadValue()
}
```

client_a 生成以下完整 Go 包，依赖上述 native / protocols；名称仅为机制示意。

```go
package client_a

import (
    "example.org/native"
    "example.org/protocols"
)

type bufferReadValue struct { target *native.Buffer }
func (v bufferReadValue) Read() int { return v.target.Read() }
func (v bufferReadValue) HyperGoTraitReadValue() {}
func AsReadValueForBuffer(b *native.Buffer) protocols.ReadValue {
    return bufferReadValue{target: b}
}
type bufferFacade struct { target *native.Buffer }
func ForBuffer(b *native.Buffer) bufferFacade { return bufferFacade{target: b} }
func (v bufferFacade) AsReadValue() protocols.ReadValue {
    return AsReadValueForBuffer(v.target)
}
```

client_b 可发布其固定工厂与 wrapper；相容声明只合并逻辑 impl 关系，不全局重定位已发布 Go 类型。
若要求跨包使用同一动态类型，应引用共同适配包的固定工厂。C → A、B 不新增 A/B → C 的反向依赖。

### D02：调用方适配原对象

hgo 片段依赖 D01、具有合法接收权限的 `b`，及接受 ReadValue 的 `consume`。

```hgo
consume(b)
```

对应 Go 机制片段依赖前述 client_a 和生成的 `consume`；该适配点已确定使用 client_a 的工厂。

```go
consume(client_a.AsReadValueForBuffer(b))
// 普通 Go 也可写 client_a.ForBuffer(b).AsReadValue()，其内部转发同一工厂。
```

接口动态类型为 client_a 的 wrapper，其中 target 与 b 是同一个指针。
不向 native.Buffer 增加 marker 或 As；facade 不匿名嵌入原对象，不另外选择 impl。
若接收者是工厂表达式，仍只求值一次；普通 Go 的反射和接口相等性可观察 wrapper 类型。

## 拒绝与边界

- D03：缺少 `Read()` 或返回类型不是 int，拒绝；declare 不隐式转换返回值。
- D04：仅 `*Buffer` 有 Read，却对 `Buffer` 声明采用，拒绝；不偷偷取地址。
- D05：同一实现键的关联类型或调用契约冲突，拒绝，并列出声明来源。
- D06：已有同名生成入口签名不兼容，拒绝；不能覆盖原有方法或随机改名。
- D07：同一实现键有两个有体 impl，拒绝；混合 declare / 有体模式见第 8 条。
- D08：给外部 native.Buffer 生成原类型 As 方法，拒绝该后端输出；应使用声明包的 facade。其他源级接收者形式的支持范围仍须单独规定。
- D09：Go 代码手写 marker 不自动创建 hgo impl；必须有显式元数据。

marker 必须导出，使 C 的 wrapper 可以实现 A 的接口；它不改变 B 中原类型的方法集。
生成名称与 C 的现有声明冲突仍须检查；新增依赖不能改变既有工厂选择的业务实现。

## 双向互操作

- hgo → Go：相容声明通过后，采用原生方法；声明作者承担其外部行为契约。
  marker 不证明原生代码不会留存参数，详见 [RFC 0006](0006-foreign-api.md)。
- Go → hgo：直接调用声明包的工厂或 facade；普通 Go 调用方按公开 Go API 使用。
  Go 原始指针不能直接赋给含 marker 的 trait 接口，须先适配。
- 整个 dist 交付普通 Go wrapper、工厂、便利入口和声明元数据，正常依赖 A/B；
  [RFC 0001](0001-package-loading.md) 的发布不需要 trait 专用 overlay 或增强标准库。
- 多包声明按来源聚合，保留公开工厂归属；不能把逻辑合并解释为任意 Go wrapper 动态类型相等。

## 验收与未定项

未来验收覆盖 D01–D09：相容声明不报重复实现，同包生成去重，
交换导入顺序不改变既有工厂行为与类型，冲突诊断列出所有相关来源。
工厂与 facade 返回相同 wrapper 类型，其 target 保留原指针；原生业务方法按实际调用次数执行。
新建纯 Go 主模块正常导入 dist；原依赖和标准库不需要添加方法或额外构建视图。

以上为编译器验收约束，Go 机制示例本身不构成端到端实现。
尚须定稿非完全相同契约的相容判定、declare 与有体共现的声明含义、
泛型接收者、生成名称编码，以及声明元数据的版本和失效规则。

# RFC 0004：普通关联类型与固定视图

## 状态与范围

- 已确认：支持普通关联类型的设计方向；关联类型由选定实现决定。
- 已确认：运行时视图采用统一 wrapper 和声明包工厂；Go 接口保留业务方法及导出 marker。
- P4 已完成：完整显式绑定、`Self::Item` / `T::Trait::Item`、固定 Go 接口，以及空 carrier witness 推导的单 trait 泛型函数已接入新管线。
- 后续：泛型 trait/impl、多 trait 约束、GAT、默认关联类型、特化和重叠实现。

依据[普通关联类型](../design/associated-types.md)。本篇不用 Option 表示，
避免关联类型示例预先决定 [RFC 0008](0008-enums.md) 的 enum ABI。
有体 impl 以基础 Cell 为目标，引用资格由 `self &Self` / `self &mut Self` 表达；declare 另按精确原生 T / *T 方法集核对。

## 源级规则

1. trait 的位置类型参数参与实现选择；关联类型是选定实现的输出。
2. 关联类型不增加实现键维度；同一接收者不能通过不同 Item 定义两份相同 trait 实现。
3. 有体 impl 在 body 内显式定义全部关联类型；declare 在头部显式绑定。
4. 不从返回类型倒推关联类型，不提供默认绑定；相容重复 declare 按 RFC 0003 合并。
5. 运行时 trait 值须绑定全部关联类型；绑定后方法须能形成固定 Go 接口。
6. 投影须有对应实现证据；多个 trait 都有 Item 时必须限定 trait。
7. 替换关联类型或降低泛型时须保留权限、借用来源、unsafe 效果与实现身份。

## 转译示例

### A01：一个实现确定一种 Item

goxide 机制片段；字段与 receiver 使用现有专题的示意语法，省略包及构造函数。

```goxide
pub trait Source {
    type Item
    func get(self &Self) Self::Item
}
pub struct Cell { n int }
impl Source for Cell {
    type Item = int
    func get(self &Self) int { return self.n }
}
```

下列 Go 展示运行时视图形状。接口类型参数承载已解析的 Item；示例缩写 marker 名，实际输出按完整 trait 身份固定生成名称。

```go
package example

type Source[Item any] interface {
    Get() Item
    GoxideTraitSource()
}
type Cell struct { N int }
type cellSource struct { target *Cell }
func (v cellSource) Get() int { return v.target.N }
func (v cellSource) GoxideTraitSource() {}
func AsSourceForCell(v *Cell) Source[int] { return cellSource{target: v} }
func (v *Cell) AsSource() Source[int] { return AsSourceForCell(v) }
```

### A02：完整绑定的运行时视图

goxide 片段依赖 A01，以及合法接收者 c。

```goxide
var s Source[Item = int] = c
var n int = s.get()
```

对应 Go 片段依赖上述包内的 c。

```go
var s Source[int] = AsSourceForCell(c)
var n int = s.Get()
```

后端 `Source[int]` 不表示源级调用者可以为 Cell 任意选择 Item。
源级关系已经由 Cell 的唯一实现确定；Go 类型参数只承载该选择。

### A03：采用原生业务方法

goxide 片段位于 adapters 包，依赖 A01 的 Source（假定位于 `example.org/protocols`），以及 `example.org/native` 中的 `*Cell`。

```goxide
use protocols;
use native;
declare impl protocols::Source[Item = int] for native::Cell;
```

Self = native::Cell，`self &Self` 要求精确 `*native.Cell` 方法集。原生包具有 `func (*Cell) Get() int` 时，验签后在声明包生成下列完整 Go 文件。
它依赖上述 native / protocols，不向原生 Cell 添加方法；名称仅为示意。

```go
package adapters

import (
    "example.org/native"
    "example.org/protocols"
)

type cellSource struct { target *native.Cell }
func (v cellSource) Get() int { return v.target.Get() }
func (v cellSource) GoxideTraitSource() {}
func AsSourceForCell(c *native.Cell) protocols.Source[int] {
    return cellSource{target: c}
}
type cellFacade struct { target *native.Cell }
func ForCell(c *native.Cell) cellFacade { return cellFacade{target: c} }
func (v cellFacade) AsSource() protocols.Source[int] { return AsSourceForCell(v.target) }
```

与 A01 相同，这里也返回 wrapper；差别是 Get 转发原生业务方法。原生 `Get() (int, bool)` 不匹配；
若要适配，须用有体 impl 明确选择结果并处理 bool，不能假装签名相同。

## 拒绝与边界

- A04：`var s Source` 未绑定 Item，不能成为普通运行时变量、参数或返回值。
- A05：对同一 `*Cell` 再声明 `Source[Item = string]`，与 Item = int 冲突。
- A06：Source[Item = int] 赋给 Source[Item = string]，拒绝。
- A07：缺失、重复或未知关联类型绑定，拒绝；相同 declare 的合并不是重复绑定。
- A08：没有 Source 实现证据就使用 `T::Source::Item`，拒绝；`Self.Item` 和未限定 `T::Item` 也拒绝。
- A09：裸 Self 出现在不支持的动态方法签名、方法级类型参数或 GAT，首版诊断不支持。

投影只存在于 Goxide 类型系统。`func Read[T Source](value T) T::Source::Item` 降低为额外 Go 类型参数及空 carrier 约束；carrier witness 的结果类型提供 Item 等式，普通 Go 调用保持 `Read[Cell](value)`，不传运行时字典。具体 Go 形状见[关联类型降低](../design/associated-types.md#4-go-降低)。

类型锚与 witness 只用于推导，不调用。enum E 的所有指针变体返回同一个 EOps，精确 Self 是 E；需要读写 owner 时，Ops 操作用 `*E` 参数，不要求 `*E` 具有类型锚。

## 双向互操作

- goxide → Go：先由 goxide 解析 Item 与 impl，再生成固定业务签名；原生方法按此验签。
- Go → goxide：Go 可使用固定工厂、同包 Cell 的 As 方法或外部 Cell 的 facade，取得 Source[int]；Go 自行满足接口方法集
  不足以建立 goxide 显式实现，也不能让 goxide 任意改变已有 Item 绑定。
- goxide 重导入生成包时，读取实现键与关联类型元数据；信息缺失时不得仅凭 marker 恢复可信绑定。
- 普通 Go 调用方不承担 goxide 泛型投影检查；公开接口的表达能力由
  [RFC 0015](0015-public-api.md) 规定，未决定的泛型入口不能声称已可公开。
- 原生转发与自定义实现的区别见 [RFC 0003](0003-declare-impl.md)，两者都由 wrapper 实现 marker；
  借用规则见 [RFC 0005](0005-borrows.md)。

## 验收与未定项

未来验收覆盖 A01–A09，检查源级诊断、Go 签名及实际 Get 结果。
相容 declare 合并后 Item 仍唯一；改变声明顺序不改变绑定。
原生 Get 返回 int 的采用成功，返回二元组的采用失败，并给出替换后的目标签名。
工厂与便利入口使用同一 impl，接口载体为 wrapper，target 保留原指针；外部类型方法集不变。
goxide 包导出再导入后仍恢复同一 Item、实现模式与权限，不能只验证 Go 能编译。

P4 验收包括独立普通 Go 包推导私有 carrier 与关联类型、类型锚/witness 零调用，以及 enum owner 的原位替换。外部原生目标仅由 declare 提供运行时适配，不通过注入类型锚获得 carrier 泛型资格。GAT、默认关联类型、泛型 impl、特化、重叠实现和未知 Item 的存在类型视图继续延期。

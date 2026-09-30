# 显式 trait 与多接口实现

状态：所有可形成运行时视图的显式 impl 统一生成独立 wrapper，业务方法与 marker 均位于 wrapper。declare 逐方法转发原生业务实现，有体 impl 执行用户方法体。固定包级工厂负责适配，便利方法只转发工厂；不修改原生类型。尚无 hgo 编译器实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D04–D07、D24：显式 trait、普通关联类型方向、统一 wrapper、保留 marker、固定工厂与期望类型适配。 |
| 暂定 | T08、T09、T12、T17：声明合并、方法解析、便利入口编码和静态 Self 的降低。 |
| 未定 | U03、U04：源级接收权限语法、泛型实现证据、完整静态成员与公开 ABI。 |

状态编号见[决策登记](decisions.md)。本文 `*T` 示例只描述 Go 接收者身份，不额外引入通用 safe 指针；`&T`、`mut& T`、Arc 的接收权限与借用来源仍需在前端保持。见[源级权限约束](overview.md#35-源级权限与-go-表示必须分开)。

## 1. 语义与术语

| 概念 | hgo 含义 | Go 承载 |
| --- | --- | --- |
| 原始类型 | 数据、固有方法与类型身份 | 原始 named type |
| trait | 必须显式采用的行为协议 | 元数据；满足运行时条件时有对应接口 |
| `impl Trait for T` | 选定的 trait 实现关系与业务来源 | 对应 wrapper、marker 和固定工厂 |
| `impl T` | 类型自有的关联函数与实例成员 | 关联函数降低为包级函数；实例成员按接收者规则降低 |
| trait 视图 | 选择了特定 impl 的值 | 保存接收者的 wrapper，按需装入接口 |
| Go interface | 结构式方法集合 | 原生 Go 接口，保留 Go 规则 |

方法签名相同不自动创建 hgo impl。降低后的 trait 接口仍要求业务方法和 marker；marker 必须保留，但显式关系、权限与精确接收者身份由 hgo 元数据决定。

此模型描述可形成运行时视图的实例方法。`Default.default() -> Self` 等静态操作仍按选定 impl 降低为普通函数，不能强行塞入运行时接口。见[静态 trait 操作](macros.md#55-静态-trait-操作与-go-运行时接口)。`impl A for T` 与 trait 继承是不同功能，supertrait 另行设计。

类型自有成员写在 `impl T { ... }` 中，`T::new(...)` 等无接收者关联函数见 [RFC 0020](../rfcs/0020-associated-functions.md)。它按类型查找函数，trait 静态操作则按显式 impl 选择实现；两者都可降低为包级函数，但前者不会自动创建 Default impl。静态 trait 成员的源级声明语法仍未冻结。

直接写出的 `func NewT(...) T` 和 `func (recv T) M(...)` 保留普通 Go 函数与方法语义，不自动成为 hgo impl，也不凭名字建立 `T::new` 绑定。impl 的 `Self` / `self` 支持按绑定解析；普通 Go 声明中的同名标识符仍按 Go 规则处理。

## 2. 源级语法

### 2.1 declare：采用已有业务方法

```hgo
import native "example.org/native/buffer"
import byteio "example.org/hgo/byteio"
type Buffer = *native::Buffer

declare impl byteio::Reader for Buffer;
```

Buffer 是原生指针的别名，不创建新类型。编译器按精确的 `T` / `*T` 方法集验签，再在 impl 所在包生成 wrapper、逐项转发方法、marker 与包级工厂。缺业务方法是错误；原生类型不需要预先提供 marker、wrapper 或 As 方法。

declare 不是等待实现的前置声明。它采用原生方法的业务与可信调用契约，不凭签名证明不逃逸或只读；见[原生 API](foreign-api.md)。

### 2.2 有体 impl：执行用户方法体

```hgo
pub trait ByteReader { read(p []byte) (int, error) }
pub trait TextReader { read() string }
pub struct Buffer { data []byte }

impl ByteReader for *Buffer {
    read(p []byte) (int, error) { return copy(p, self.data), nil }
}
impl TextReader for *Buffer {
    read() string { return string(self.data) }
}
```

有体 impl 完整提供业务方法，即使原生类型已有同名同签名方法，也执行所写方法体。需要复用时显式委托。`[]byte` 和 `*Buffer` 仅展示后端机制，不代表源级容器与权限语法已冻结。

规范实现键包含接收者、trait 身份和 trait 参数；关联类型是实现的输出。相容 declare 可以合并，有体实现保持唯一，矛盾契约不得覆盖。declare 与有体实现混合的具体声明语义仍待定；见 [RFC 0003](../rfcs/0003-declare-impl.md)。

## 3. 统一 wrapper 的 Go 降低

两种业务来源使用同一结构，区别是 wrapper 方法执行原生转发还是用户方法体。declare 的机制片段如下，代码属于 impl 包：

```go
// native.Buffer 已有 Read；ByteReader 包含 Read 与对应 marker。
type nativeByteReader struct { target *native.Buffer }
func (v nativeByteReader) HyperGoTraitByteReader() {}
func (v nativeByteReader) Read(p []byte) (int, error) {
    return v.target.Read(p)
}
func AsNativeBufferByteReader(b *native.Buffer) ByteReader {
    return nativeByteReader{target: b}
}
```

Go 不允许给外部类型追加方法，但允许 impl 包给自己的 wrapper 定义方法。原生包不增加 marker 或 As；非空与权限检查按选定接收者契约实施，上述短片段不自称建立了安全证明。

下面是可独立编译的有体 impl 示例。包级工厂是固定入口；本地 Buffer 与 impl 同包，所以可附加只转发工厂的便利方法。现有 Go 夹具继续使用这些便利方法。

```go
package views

type ByteReader interface {
    Read([]byte) (int, error)
    HyperGoTraitByteReader()
}
type TextReader interface {
    Read() string
    HyperGoTraitTextReader()
}
type Buffer struct { data []byte }
func NewBuffer(data []byte) *Buffer { return &Buffer{data: data} }

type bufferByteReader struct { target *Buffer }
type bufferTextReader struct { target *Buffer }

func AsBufferByteReader(b *Buffer) ByteReader {
    if b == nil { panic("hgo: nil Buffer") }
    return bufferByteReader{target: b}
}
func AsBufferTextReader(b *Buffer) TextReader {
    if b == nil { panic("hgo: nil Buffer") }
    return bufferTextReader{target: b}
}
func (b *Buffer) AsByteReader() ByteReader { return AsBufferByteReader(b) }
func (b *Buffer) AsTextReader() TextReader { return AsBufferTextReader(b) }

func (v bufferByteReader) HyperGoTraitByteReader() {}
func (v bufferTextReader) HyperGoTraitTextReader() {}
func (v bufferByteReader) Read(p []byte) (int, error) {
    if v.target == nil { panic("hgo: invalid ByteReader view") }
    return copy(p, v.target.data), nil
}
func (v bufferTextReader) Read() string {
    if v.target == nil { panic("hgo: invalid TextReader view") }
    return string(v.target.data)
}
```

Buffer 没有这两个 Read；业务方法和 marker 都在对应 wrapper 上。Go 用户可调用固定工厂或 `b.AsByteReader()`，后者不重新选择 impl。外部类型使用 `adapters.ForBuffer(b).AsByteReader()` 一类入口，见[互操作专题](go-interop.md)。

wrapper 必须采用命名 target 字段，仅生成选定 trait 的业务方法与 marker。匿名嵌入 `*bytes.Buffer` 会提升 WriteTo 等额外方法，使 Go `io.Copy` 可能绕过 wrapper 的 Read；不能为节省转发代码引入这种变化。

marker 使用导出的稳定符号，使其他包拥有的 wrapper 能实现 trait 接口。正式名称需编码完整 trait 身份并检查冲突；公开 marker 仍可被 Go 手写，不是不可伪造的安全证明。

## 4. 接收者、共享状态与 self

`Self` 与 `self` 区分大小写：`Self` 表示 impl 的源级目标类型，包括其泛型实参，不是生成的 wrapper 类型；`self` 表示实例成员所绑定的接收者。无接收者关联函数可以使用 `Self`，不能因此获得 `self`。接收者与权限的具体声明语法另行规定，不能根据函数体是否使用 self 来决定成员有没有接收者。

`impl Trait for *T` 的 wrapper 保存同一个 `*T`。不同视图可观察同一对象变化；对象指针身份保持不变，Go interface 动态类型则是 wrapper。反射与类型断言不能再假定看到原始 `*T`。

`T` 与 `*T` 是不同实现键。不能为了通过方法集检查，给临时 T、map 元素偷偷复制取址；值接收者按其声明的值复制规则处理。先验原方法集，再生成转发，不能利用生成代码里局部字段可寻址的事实补齐缺失方法。

例如仅有 `func (*Counter) Next()` 时，`struct{ target Counter }` 的值接收者转发可能在 Go 中编译，却每次修改 target 副本。这不能作为 Counter 实现该 trait 的依据。

| impl 方法体中的操作 | 降低目标 |
| --- | --- |
| `self.field` | 原始接收者的字段，保持声明的接收权限 |
| 调用固有方法 | 原始对象的方法 |
| 调用当前 trait 方法 | 当前 wrapper 的选定实现 |
| 限定调用另一 trait | 为原接收者选择该 impl 并适配 |

不能把所有 `self.m()` 改成 `target.m()`。前端必须保存已选 impl，静态优化只能消除无可观察影响的装箱或转发，不能改业务来源。Self / self 的降低依据已解析的类型与接收者绑定，不对源文件、字符串、字段名或其他同名标识符做文本替换。

## 5. 同名方法与调用解析

```hgo
b.read::<ByteReader>(p)
b.read::<TextReader>()
b.read::<byteio::Reader>(p)
```

限定调用选择一个 trait。普通调用暂定固有方法优先；否则查询可见显式实现，候选唯一才能简写，多个候选要求限定。已经是 trait 视图的值调用该 trait 方法，不再按原对象的方法集选择。

新增 impl 可能使未限定的 hgo 调用歧义，但不得改变既有 wrapper 或工厂符号。Go API 稳定与源码调用永不歧义不是同一承诺。

## 6. 期望类型驱动的转换

前端先选择 impl，生成 `TraitView(value, implID)`，记录接收者、业务来源和固定工厂。declare 和有体 impl 都生成对应 wrapper；接收者表达式只求值一次。

转换保留权限、借用来源和有效范围。短借用不能因封装视图而逃逸，raw Ptr 不能仅凭 marker 升级为 safe 引用。

| 使用位置 | 转换规则 |
| --- | --- |
| 参数、赋值、返回的期望类型为已实现 trait | 调用选定包级工厂 |
| `x := value`、目标为 any 或相同具体类型 | 保留原类型 |
| 已具有相同 trait 与关联类型绑定的视图 | 复用已有视图，不递归包装 |
| 泛型形参 T 受 trait 约束 | 保留 T，实现证据另行传递或选择 |
| `[]T` 整体转 `[]Trait` | 不隐式逐元素转换 |
| 字面量元素具有 trait 期望类型 | 可以按元素适配 |
| TraitA 转 TraitB 或恢复原生 `*T` | 不隐式转换或 unwrap |

普通 Go interface 继续按方法集检查，不搜索全部 trait wrapper 来凑接口。需传给具体原生 `*T` 的 API 时使用原始变量，或使用单独定义并受权限检查的显式桥接。

## 7. 原生类型的采用与公开入口

用户在 C 包声明外部 trait 与外部类型的关系，wrapper、marker 和工厂归 C；不写回原始类型定义包。生成 Go 代码随 dist 交付，纯 Go 可以使用普通工厂，hgo 额外加载实现元数据。

包级工厂是唯一适配语义入口，便利 API 只转发它：

- 类型与 impl 同包，且属于 Go 允许定义方法的本地 named type，才可生成 `t.AsSized()` 一类方法。
- 外部类型不能追加方法，使用 `adapters.ForBuffer(t).AsSized()`；ForBuffer 返回 impl 包拥有的命名适配器。
- 两者都不根据调用位置重新选择实现，不在原类型添加 marker，也不匿名嵌入目标以提升其他方法。

同包只是必要条件：`type Ptr[T any] *T` 这样的具名指针类型不能在 Go 中作为方法接收者基类型，仍须使用包级工厂或自有 facade；不能为便利入口违反 Go 接收者规则。

已有带游标或缓存的原生 wrapper 可以作为接收者采用，应保存该对象，不在每次业务调用中重新构造。外部私有字段的访问仍受 Go 包边界约束。

## 8. Go API 与符号稳定性

每个公开 impl 固定 wrapper 身份和包级工厂。declare 改为有体 impl 会改变业务契约，须评估兼容性；不应仅因业务来源变化自动更换包装类型。此前“declare 为原类型补 marker 并返回原值”的方案已被统一 wrapper 替代。

符号身份至少包含定义包、接收者形式、trait 身份及参数；不依赖 import 别名、时间戳或每次构建变化的值。相容重复声明如何映射到固定生成归属，与声明合并和发布规则一并确定，不能按导入顺序选宿主。

AsBufferByteReader、AsByteReader、ForBuffer 是短示例，正式编码尚待冻结。必须区分同名 trait、检测固有方法及生成名冲突，新增实现不能重命名旧入口。元数据恢复完整身份，不依赖拆分名称猜测语义。

## 9. 非法状态与边界

装有 nil target 的值 wrapper 进入接口后仍不等于 nil。是否拒绝由源级接收者和公开契约决定；本篇完整示例明确拒绝，不把 wrapper 构造等同于非空证明。

包含不可比较 T 的值 wrapper 可能使 Go 接口比较 panic；指针 wrapper 又可能让两次适配获得不同包装对象身份。公开的比较、nil、反射和分配行为须记录，保存原 target 不等于完全透明或零分配。

marker 即使被 Go 手写或嵌入提升，也不会创建新的 hgo impl。源级采用关系、权限、关联类型和业务来源仍以一致的实现元数据为准。

## 10. 实现验收条件

1. 同名异签名、同签名不同业务分别执行选定 impl；declare 转发，有体执行用户方法体。
2. 两种来源的 wrapper 均保留 marker，原生类型不新增方法；缺业务方法在源级拒绝。
3. 指针接收者不同视图共享原对象，Go 动态类型明确为对应 wrapper。
4. 固定工厂、本地 As 与外部 ForBuffer/As helper 选择一致，helper 只转发工厂。
5. 适配只求值一次，保留借用范围，不隐式 unwrap，不产生递归 wrapper 链。
6. 精确 T / *T 方法集、声明冲突、权限与函数值转换均有正反例。
7. 通过 io.Copy 等原生调用检查没有提升额外方法，新增 impl 不改变既有工厂和视图身份。
8. LSP 与编译器读取同一元数据，可导航到采用的 Go 方法或 hgo 方法体。

现有 views Go 夹具验证完整示例的共享对象、自定义分派和 nil 工厂；不代表 hgo 前端或新 declare 工厂已经实现。

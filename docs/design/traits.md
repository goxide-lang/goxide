# 显式 trait 与多接口实现

状态：P4 已在新管线完成。运行时 impl 统一生成独立 wrapper，实例业务方法与 marker 均位于 wrapper；静态操作和泛型证据使用空 carrier。declare 逐方法转发原生业务，有体 impl 执行用户方法体。固定包级工厂负责适配，便利入口只转发工厂，不修改外部原生类型。

| 状态 | 范围 |
| --- | --- |
| P4 | 非泛型 trait、本包具体 struct/enum 的 impl、三种 self 接收权限、普通关联类型、单 trait 泛型约束、空 carrier、运行时 wrapper 和 Go 适配。 |
| 后续 | 泛型 trait/impl、多 trait 约束、supertrait、GAT、默认关联类型、特化与重叠实现。跨包 Goxide 加载及发布信息恢复由 P5 接续。 |

状态编号见[决策登记](decisions.md)。有体实现写在基础类型 T 上，通过成员的 `self Self` / `self &Self` / `self &mut Self` 区分传值、只读和可写接收者。`declare impl` 的原生 T / *T 方法集按精确 Go 类型核对，不把原生指针视为获得引用权限。见[源级权限约束](overview.md#35-源级权限与-go-表示必须分开)。

[RFC 0001](../rfcs/research/0001-scoped-borrow-views.md)已按 D19 重写：不将逃逸或未知摘要作为适配准入限制。

诊断边界仍按 D19/D20：基础类型权限照常检查，别名/逃逸风险分析不属 MVP，不以未知效果或无法证明不逃逸普遍拒绝。

## 1. 语义与术语

| 概念 | goxide 含义 | Go 承载 |
| --- | --- | --- |
| 原始类型 | 数据、固有方法与类型身份 | 原始 named type |
| trait | 必须显式采用的行为协议 | 编译期实现关系；满足运行时条件时有对应接口 |
| `impl Trait for T` | 选定的 trait 实现关系与业务来源 | 对应 wrapper、marker 和固定工厂 |
| `impl T` | 类型自有的关联函数与实例成员 | 关联函数降低为包级函数；实例成员按接收者规则降低 |
| trait 视图 | 选择了特定 impl 的值 | 保存接收者的 wrapper，按需装入接口 |
| Go interface | 结构式方法集合 | 原生 Go 接口，保留 Go 规则 |

方法签名相同不自动创建 goxide impl。降低后的 trait 接口仍要求业务方法和 marker；marker 必须保留，显式关系、权限与精确目标身份由源级解析与检查结果决定。

此模型描述可形成运行时视图的实例方法。无 self 的 trait 操作按选定 impl 通过空 carrier 调用，不加入运行时接口；P2 的具体派生 helper 保持既有合同。`impl A for T` 与 trait 继承是不同功能，supertrait 另行设计。

例如同时具有 `func make() Self` 与 `func transform(self &Self) Self` 的 trait 可在具体类型和泛型约束下使用；裸 Self 结果使其不能成为 `var value Trait` 的运行时视图，不能因此拒绝整个 impl。

类型自有成员写在 `impl T { ... }` 中，`T::new(...)` 等无接收者关联函数见 [RFC 0020](../rfcs/0020-associated-functions.md)。它按类型查找函数，trait 静态操作按显式 impl 选择实现；固有构造函数不会自动创建 Default impl。impl 成员用 `func` 声明，首参数 self 标识实例成员，否则为关联函数，无需 static。

直接写出的 `func NewT(...) T` 和 `func (recv T) M(...)` 保留普通 Go 函数与方法语义，不自动成为 goxide impl，也不凭名字建立 `T::new` 绑定。impl 的 `Self` / `self` 支持按绑定解析；普通 Go 声明中的同名标识符仍按 Go 规则处理。

## 2. 源级语法

### 2.1 declare：采用已有业务方法

```goxide
use native;
use byteio;
type Buffer = native::Buffer

declare impl byteio::Reader for Buffer;
```

Buffer 是原生类型的别名，不创建新类型。假定 Reader 的接收者为 `self &Self`，代入 Self = native::Buffer 后核对精确 `*native.Buffer` 方法集，再在 impl 包生成 wrapper、转发、marker 和工厂。缺业务方法是错误；原生类型无需预备 marker、wrapper 或 As。

declare 同样按结构替换 Self：`for *native::Buffer` 配 `self Self` 核对 `*native.Buffer`；若配 `self &Self` 则核对 `**native.Buffer`，不能自动消去引用层来凑方法集。只有代入后精确的接收者方法集通过，才能生成转发。

declare 不是等待实现的前置声明。它采用原生方法的业务与可信调用契约，不凭签名证明不逃逸或只读；见[原生 API](foreign-api.md)。

### 2.2 有体 impl：执行用户方法体

```goxide
pub trait ByteReader { func read(self &mut Self, p []byte) (int, error) }
pub trait TextReader { func read(self &Self) string }
pub struct Buffer { data []byte }

impl ByteReader for Buffer {
    func read(self &mut Self, p []byte) (int, error) { return copy(p, self.data), nil }
}
impl TextReader for Buffer {
    func read(self &Self) string { return string(self.data) }
}
```

有体 impl 完整提供业务方法，即使原生类型已有同名同签名方法，也执行所写方法体。需要复用时显式委托。两个 impl 的基础目标均是 Buffer，接收者权限分别由 self 参数声明。

规范实现键包含基础目标与 trait 身份；P4 的 trait 不带位置参数，关联类型是实现的输出。相同目标不能以不同关联类型再实现同一 trait。相容 declare 可以合并，有体实现保持唯一，矛盾契约不得覆盖；同一键混合 declare 与有体实现时诊断，见 [RFC 0003](../rfcs/0003-declare-impl.md)。

## 3. 统一 wrapper 的 Go 降低

两种业务来源使用同一结构，区别是 wrapper 方法执行原生转发还是用户方法体。declare 的机制片段如下，代码属于 impl 包：

```go
// native.Buffer 已有 Read；ByteReader 包含 Read 与对应 marker。
type nativeByteReader struct { target *native.Buffer }
func (v nativeByteReader) GoxideTraitByteReader() {}
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
    GoxideTraitByteReader()
}
type TextReader interface {
    Read() string
    GoxideTraitTextReader()
}
type Buffer struct { data []byte }
func NewBuffer(data []byte) *Buffer { return &Buffer{data: data} }

type bufferByteReader struct { target *Buffer }
type bufferTextReader struct { target *Buffer }

func AsBufferByteReader(b *Buffer) ByteReader {
    return bufferByteReader{target: b}
}
func AsBufferTextReader(b *Buffer) TextReader {
    return bufferTextReader{target: b}
}
func (b *Buffer) AsByteReader() ByteReader { return AsBufferByteReader(b) }
func (b *Buffer) AsTextReader() TextReader { return AsBufferTextReader(b) }

func (v bufferByteReader) GoxideTraitByteReader() {}
func (v bufferTextReader) GoxideTraitTextReader() {}
func (v bufferByteReader) Read(p []byte) (int, error) {
    return copy(p, v.target.data), nil
}
func (v bufferTextReader) Read() string {
    return string(v.target.data)
}
```

Buffer 没有这两个 Read；业务方法和 marker 都在对应 wrapper 上。Go 用户可调用固定工厂或 `b.AsByteReader()`，后者不重新选择 impl。外部类型使用 `adapters.ForBuffer(b).AsByteReader()` 一类入口，见[互操作专题](go-interop.md)。

wrapper 必须采用命名 target 字段，仅生成选定 trait 的业务方法与 marker。匿名嵌入 `*bytes.Buffer` 会提升 WriteTo 等额外方法，使 Go `io.Copy` 可能绕过 wrapper 的 Read；不能为节省转发代码引入这种变化。

marker 使用导出的稳定符号，使其他包拥有的 wrapper 能实现 trait 接口。正式名称需编码完整 trait 身份并检查冲突；公开 marker 仍可被 Go 手写，不是不可伪造的安全证明。

## 4. 接收者、共享状态与 self

`Self` 与 `self` 区分大小写：`impl Trait for T` 中 Self 是基础目标 T，enum 的 Self 是完整 enum 接口；不是 wrapper、carrier 或变体。首参数 `self Self` 为传值，`self &Self` 为只读引用，`self &mut Self` 为可写引用。impl 与 trait 的接收模式必须一致。无接收者关联函数可以使用 Self，但没有隐式 self；不能按函数体是否使用 self 推断成员种类。

完整运行时视图按该 trait 的实例方法选择捕获方式：

| 实例接收者集合 | wrapper 的源级 target |
| --- | --- |
| 全部为 `self Self` | T 值副本 |
| 全部为 `self &Self` | `&T` |
| 包含 `self &mut Self`，或传值与借用成员混合 | `&mut T` |

每个方法中的 self 仍按自身声明检查；可写 target 可收窄给只读方法。传值方法读取目标的值副本，沿用 P3 对共享字段的规则。传值与借用混合时要求可写 target，避免用只读浅副本执行可修改共享字段的值方法。

借用 wrapper 保存同一对象或 enum owner 槽位；Go 表示为 `*T`，Go interface 动态类型为 wrapper。值类型按既有复制规则处理：enum 的副本共享变体对象，整体替换不会改写旧值视图。借用 enum 的 `*E` 则观察真实 owner 的整体替换。

原生 declare 在代入 Self 与成员接收模式后，分别验证精确 T / *T 方法集。不能为了通过方法集检查给临时 T、map 元素偷偷复制取址，也不能利用生成代码里局部字段可寻址来补齐原生方法。

例如仅有 `func (*Counter) Next()` 时，`struct{ target Counter }` 的值接收者转发可能在 Go 中编译，却每次修改 target 副本。这不能作为 Counter 实现该 trait 的依据。

| impl 方法体中的操作 | 降低目标 |
| --- | --- |
| `self.field` | 原始接收者的字段，保持声明的接收权限 |
| 调用固有方法 | 原始对象的方法 |
| 调用当前 trait 方法 | 当前 wrapper 的选定实现 |
| 限定调用另一 trait | 为原接收者选择该 impl 并适配 |

不能把所有 `self.m()` 改成 `target.m()`。前端必须保存已选 impl，静态优化只能消除无可观察影响的装箱或转发，不能改业务来源。Self / self 的降低依据已解析的类型与接收者绑定，不对源文件、字符串、字段名或其他同名标识符做文本替换。

## 5. 同名方法与调用解析

```goxide
b.read::<ByteReader>(p)
b.read::<TextReader>()
b.read::<byteio::Reader>(p)
```

限定调用选择一个 trait；等价函数式写法为 `Trait::read(b, p)`。普通调用按已有引用与寻址规则构造接收者候选，在同一候选中先查固有方法，再查可见显式 trait 实现；多个 trait 候选须限定。不得把某个后续候选上的固有方法提前覆盖先前候选。struct 与 enum 使用同一源级成员表，不引入 Rust Deref trait 或所有权系统。已经是 trait 视图的值调用该 trait 方法，不再按原对象的方法集选择。

新增 impl 可能使未限定的 goxide 调用歧义，但不得改变既有 wrapper 或工厂符号。Go API 稳定与源码调用永不歧义不是同一承诺。

## 6. 期望类型驱动的转换

前端先选择 impl，生成 `TraitView(value, implID)`，记录接收者、业务来源和固定工厂。declare 和有体 impl 都生成对应 wrapper；接收者表达式只求值一次。

该转换保留源级类型和引用权限。真实接收类型/impl 不匹配仍 error；引用视图可以复制、返回和存储，不追加逃逸或独占检查。原生指针不因 marker 自动获得不同源类型。

| 使用位置 | 转换规则 |
| --- | --- |
| 参数、赋值、返回的期望类型为已实现 trait | 调用选定包级工厂 |
| `x := value`、目标为 any 或相同具体类型 | 保留原类型 |
| 已具有相同 trait 与关联类型绑定的视图 | 复用已有视图，不递归包装 |
| 泛型形参 T 受 trait 约束 | 保留 T，使用空 carrier 与关联类型 witness 选择实现 |
| `[]T` 整体转 `[]Trait` | 不隐式逐元素转换 |
| 字面量元素具有 trait 期望类型 | 可以按元素适配 |
| TraitA 转 TraitB 或恢复原生 `*T` | 不隐式转换或 unwrap |

普通 Go interface 继续按方法集检查，不搜索全部 trait wrapper 来凑接口。需传给具体原生 `*T` 的 API 时使用原始变量，或使用单独定义并受权限检查的显式桥接。

## 7. 原生类型的采用与公开入口

用户在 C 包声明外部 trait 与外部类型的关系，wrapper、marker 和工厂归 C；不写回原始类型定义包。生成 Go 代码随 dist 交付，纯 Go 可以使用普通工厂，goxide 额外加载实现元数据。

包级工厂是唯一适配语义入口，便利 API 只转发它：

- 类型与 impl 同包，且属于 Go 允许定义方法的本地 named type，才可生成 `t.AsSized()` 一类方法。
- 本包 enum 的值视图在 E 接口声明 As，由各指针变体一行转发固定工厂，保留 `e.AsSized()`；实例业务仍只在 wrapper。借用 enum 视图使用 `ForE(&e).AsSized()` 保存真实 owner。
- 外部类型不能追加方法，使用 `adapters.ForBuffer(t).AsSized()`；ForBuffer 返回 impl 包拥有的命名适配器。
- 两者都不根据调用位置重新选择实现，不在原类型添加 marker，也不匿名嵌入目标以提升其他方法。

`For<Base>(*Base)` 统一保存 owner。值视图的 As 从 owner 读取值并调用固定工厂，借用视图把同一 owner 传给工厂。外部纯值类型可直接调用 `As<Type><Trait>(value)`，或用 `ForType(&value).AsTrait()`；不为 facade 临时复制目标再取址。

同包只是必要条件：`type Ptr[T any] *T` 这样的具名指针类型不能在 Go 中作为方法接收者基类型，仍须使用包级工厂或自有 facade；不能为便利入口违反 Go 接收者规则。

已有带游标或缓存的原生 wrapper 可以作为接收者采用，应保存该对象，不在每次业务调用中重新构造。外部私有字段的访问仍受 Go 包边界约束。

## 8. Go API 与符号稳定性

每个公开 impl 固定 wrapper 身份和包级工厂。declare 改为有体 impl 会改变业务契约，须评估兼容性；不应仅因业务来源变化自动更换包装类型。此前“declare 为原类型补 marker 并返回原值”的方案已被统一 wrapper 替代。

符号身份至少包含定义包、接收者形式、trait 身份及参数；不依赖 import 别名、时间戳或每次构建变化的值。相容重复声明如何映射到固定生成归属，与声明合并和发布规则一并确定，不能按导入顺序选宿主。

P4 的工厂采用 `As<Type><Trait>`，helper 采用 `As<Trait>` / `For<Type>`；marker 编码完整 trait 身份。carrier operation 始终有 trait 与成员身份确定的 canonical 名称；唯一静态成员可增加简短转发，保留 `typeinfo.Of[T]().Abc()`。多个 trait 具有同名静态成员时，只使用 canonical 入口，无限定源码调用报歧义，简短入口不猜选实现。生成名与用户声明冲突时诊断，不静默覆盖或重命名；新增实现不能改变旧工厂身份。

## 9. 非法状态与边界

装有 nil target 的值 wrapper 进入接口后仍不等于 nil。P4 保持已有 Go / enum 的 nil 规则，不额外生成一刀切非空工厂；原生方法自身的 nil 行为继续由原生实现决定，enum match 继续按 enum 规范检查。

包含不可比较 T 的值 wrapper 可能使 Go 接口比较 panic；指针 wrapper 又可能让两次适配获得不同包装对象身份。公开的比较、nil、反射和分配行为须记录，保存原 target 不等于完全透明或零分配。

marker 即使被 Go 手写或嵌入提升，也不会创建新的 goxide impl。源级采用关系、权限、关联类型和业务来源仍以一致的实现元数据为准。

## 10. 实现验收条件

1. 同名异签名、同签名不同业务分别执行选定 impl；declare 转发，有体执行用户方法体。
2. 两种来源的 wrapper 均保留 marker，原生类型不新增方法；缺业务方法在源级拒绝。
3. 指针接收者不同视图共享原对象，Go 动态类型明确为对应 wrapper。
4. 固定工厂、本地 As 与外部 ForBuffer/As helper 选择一致，helper 只转发工厂。
5. 适配只求值一次，保留借用范围，不隐式 unwrap，不产生递归 wrapper 链。
6. 精确 T / *T 方法集、声明冲突、权限与函数值转换均有正反例。
7. 通过 io.Copy 等原生调用检查没有提升额外方法，新增 impl 不改变既有工厂和视图身份。
8. struct 与 enum 共用成员选择；静态 carrier、关联类型 witness 和原生 Go 泛型调用保持精确 Self，不执行类型锚或 witness。

跨包 Goxide 加载与后续 LSP 复用已选实现和来源信息，分别随 P5 和 MVP 工具链推进。P4 的阶段行为必须由新管线生成 Go 并真实运行验收。

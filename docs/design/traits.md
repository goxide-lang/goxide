# 显式 trait 与多接口实现

状态：采用显式声明、marker 与期望类型转换。declare 复用已有业务方法并补 marker；有方法体的 impl 使用稳定 wrapper，业务方法体与 marker 位于 wrapper。声明写在用户包的普通 `.hgo` 文件中，仅影响 hgo 构建，不修改原生包或其他纯 Go 项目。具体名称编码与构建接口仍为设计草案。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D04–D07：显式 trait、普通关联类型方向、无体补 marker / 有体 wrapper、稳定 As 与期望类型适配。 |
| 暂定 | T08、T09、T12：唯一性与导入细节、方法解析、名称编码、静态 Self 的降低。 |
| 未定 | U03、U04：源级接收权限、完整泛型与静态成员；supertrait 延期。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 hgo 功能已实现。

与新增引用体系的接合状态：本文早期 hgo 示例中的 `*T` 只用于展示 Go 接收者身份、方法集与 wrapper 分派，不是新语言额外提供的通用 safe 指针。`&T` / `mut& T` / `Arc[T]` 的接收权限、借用有效范围和源级 impl 接收者写法仍需接合；`Ptr[T]` 不得通过期望 trait 类型转换自动提升为 safe 引用。本文 `[]byte` 写入示例也仅验证 Go 降低，不代表容器写权限规则已完成。见[源级权限约束](overview.md#35-源级权限与-go-表示必须分开)。

## 1. 语义与术语

本文的接口、marker 与 wrapper 降低主要描述可形成运行时视图的实例方法。新增基础协议还需要 `Default.default() -> Self` 等静态构造，以及返回具体 Self 的 `Clone`；这些操作按选定 impl 降低为普通 Go 函数，不能仅靠本篇的接口模型表达。完整静态成员语法与泛型实现证据仍待定，见[静态 trait 操作](macros.md#55-静态-trait-操作与-go-运行时接口)。

| 概念 | hgo 含义 | Go 中的承载 |
| --- | --- | --- |
| 原始类型 | 数据、固有方法与类型身份 | 原始 named type，例如 `Buffer` |
| trait | 需要显式声明的行为协议 | hgo 元数据；满足运行时视图条件时有对应 Go 接口 |
| impl | 某个类型对某个 trait 实例的唯一实现 | 运行时视图采用已有方法或 wrapper 加 marker；静态操作采用选定函数 |
| trait 视图 | 选择了特定 impl 的运行时值 | 原始接收者或 wrapper，按需装入 Go 接口 |
| Go interface | 按 Go 方法集满足的结构式接口 | 原生 Go 接口 |

方法签名碰巧相同，不会自动创建 hgo `impl`。显式关系由 hgo 元数据记录；降低后的 Go trait 接口要求业务方法和对应 marker。declare 的作用是确认已有业务实现，并为接收者补上 marker，而不是要求原生包提前提供它。

`impl A for T` 与 `trait B: A` 分别表示类型实现和 trait 继承。本文件只规范前者；父 trait、上转型与菱形关系另行设计。

## 2. 源级语法

### 2.1 declare：采用已有方法并补 marker

```hgo
import native "example.org/native/buffer"
import byteio "example.org/hgo/byteio"

type Buffer = *native::Buffer

declare impl byteio::Reader for Buffer;
```

本例 `Buffer` 是原生 `*native::Buffer` 的类型别名，不创建新结构体。也可以直接写 `declare impl byteio::Reader for *native::Buffer;`。接收者方法集检查始终区分原生 `T` 与 `*T`，不会为通过检查而自动取地址。

`declare impl ...;` 表示完整采用现有业务方法，不是等待补齐业务实现的前置声明。此前讨论的 `impl ...;` 表达的是同一意图，本文统一使用用户明确的 declare 写法。

编译器先按 trait 规定的 Go 方法名和签名校验业务方法，再生成 marker 与适配入口；转换复用原值。缺业务方法是错误，缺 marker 则是本声明要完成的生成工作。原生包不必预先提供 wrapper、marker 或 `AsXXX()`。

### 2.2 有方法体：提供自定义实现

```hgo
pub trait ByteReader {
    read(p []byte) (int, error)
}

pub trait TextReader {
    read() string
}

pub struct Buffer {
    data []byte
}

impl ByteReader for *Buffer {
    read(p []byte) (int, error) {
        return copy(p, self.data), nil
    }
}

impl TextReader for *Buffer {
    read() string {
        return string(self.data)
    }
}
```

`self` 是当前 impl 的源级接收者，本例类型为 `*Buffer`。`Self` 是相应类型占位符。trait 方法的 hgo 可见性随 trait；完整的 `pub` 和 Go 名称导出规则另见未来专题。

本例使用两个不同签名的 `read`。签名完全相同但行为不同，也必须是两个独立实现，不能合并。

每个规范化后的 `(接收者类型, trait 身份, trait 类型参数)` 最多有一个有效 impl。关联类型是该实现的输出，不是用于区分冲突 impl 的额外键。两种实现模式也不是额外的键：同一实现不能同时声明 declare 和有体 impl 两份。首版不支持重叠实现或特化。

有方法体的 impl 必须完整提供 trait 要求的业务方法。即使原始类型已经有同名同签名的 Go 方法，选中这个 impl 时也使用所写的方法体，不悄悄回退到原始方法。需要复用时在方法体中显式委托。

普通 `.hgo` 文件允许为外部 Go 类型声明显式 impl，包括 trait 也来自另一个包的情况，不要求借助专用声明文件。实现元数据随普通 hgo 包导入加载；在实际导入图中，同一个规范 impl 键出现两个有效定义时报冲突，不依导入顺序覆盖。具体规则见 [Go 原生类型的显式适配](go-interop.md)。

## 3. 两种实现模式的 Go 降低

declare 在接收者定义包的构建视图中生成 marker 和便利入口，直接返回同一个接收者：

```go
// 片段：*Buffer 已有 Read，ByteReader 要求 Read 和下列 marker。
func (b *Buffer) HyperGoTraitByteReader() {}

func (b *Buffer) AsByteReader() ByteReader {
    return b
}
```

Go 不允许在调用包里给外部类型追加方法。declare 的源码归用户包所有，生成的 marker 则必须进入原始定义包的隔离构建视图；原依赖文件和共享模块缓存保持原样。该模式没有新的 wrapper 动态类型，也没有额外业务方法体。具体机制见 [Go 原生类型的显式适配](go-interop.md)。

有方法体的 impl 则使用下面的 wrapper 表示。

下面是完整、可独立编译的 Go 降低示意。为便于阅读使用短名称；正式名称必须满足第 8 节的稳定身份规则。

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

type Buffer struct {
    data []byte
}

func NewBuffer(data []byte) *Buffer {
    return &Buffer{data: data}
}

type bufferByteReader struct { target *Buffer }
type bufferTextReader struct { target *Buffer }

func (b *Buffer) AsByteReader() ByteReader {
    if b == nil { panic("hgo: nil Buffer") }
    return bufferByteReader{target: b}
}

func (b *Buffer) AsTextReader() TextReader {
    if b == nil { panic("hgo: nil Buffer") }
    return bufferTextReader{target: b}
}

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

这里 `Buffer` 没有两个 `Read`，也没有 `Hyper__Trait__Read` 一类承载实现体的方法。实际实现体和对应 marker 都位于各 wrapper。自定义 impl 不会把它的 marker 加到原类型上，避免原值绕过自定义行为。

Go 调用方使用 `b.AsByteReader().Read(p)` 或 `b.AsTextReader().Read()`。`ByteReader` 视图也满足普通的 Go `io.Reader` 方法要求；原始 `*Buffer` 不因此自动满足 `io.Reader`。同一接收者基类型的方法名必须唯一、方法不能随意加到别的包的类型上，这是需要 wrapper / 包级工厂的 Go 约束。[Go 方法声明](https://go.dev/ref/spec#Method_declarations)

编译器可以在静态已知实现的调用中省掉接口装箱或内联 wrapper，但不能把有体 impl 改成调用原始类型的同名方法。对外工厂、动态类型和可观察行为仍受 ABI 规则约束。

## 4. 接收者、共享状态与方法体内的 self

共享状态示例使用 `impl Trait for *T`。直接采用模式保留这个指针；自定义模式的 wrapper 保存同一个 `*T`。A/B 两个视图读取或修改同一对象，生成的 wrapper 不增加独立业务状态。

`T` 与 `*T` 是不同的 impl 键：

- 已有 `*T` 表达式可以按所选 impl 适配。
- 不会为满足 `impl Trait for *T` 而把临时 `T`、map 元素等悄悄取地址或复制到堆上。
- 用户先建立明确的地址/指针，再进行共享适配；直接采用 `T` 的值方法集时遵循 Go 值复制语义，不宣称保留指针身份。
- 泛型不把 `T` 悄悄改成 `*T`；指针形式应由约束或参数类型表达。

自定义方法体降低必须区分两种接收者：

| hgo 方法体里的操作 | 降低目标 |
| --- | --- |
| `self.data` | 原始对象，例如 `view.target.data` |
| 调用原始类型的固有方法 | 原始对象的方法 |
| 对当前 `self` 调用当前 trait 的方法 | 当前 wrapper，避免重复构造同一视图 |
| 对 `self` 限定调用另一个 trait | 从原始接收者选择另一个 impl 并适配 |

不能把所有 `self.m()` 都机械改写为 `view.target.m()`。前端需要保存调用所属的 trait/impl，而不是在代码生成阶段重新猜测。

## 5. 同名方法与调用解析

限定调用沿用已讨论的形式：

```hgo
b.read::<ByteReader>(p)
b.read::<TextReader>()

// 包限定名示意
b.read::<byteio::Reader>(p)
```

一个限定位置只选择一个 trait，不使用 `A/B` 表示合并候选。实际语法中的包路径分隔符属于待冻结的词法设计；本文一致使用 `pkg::Name`。

普通调用的草案规则：先查固有方法；没有固有方法时，查询当前作用域可见 trait 的适用实现。候选唯一才允许简写；多个候选要求限定调用，即使它们的签名不同，也不依靠重载猜测。

已经是 `ByteReader` 视图的变量调用 `read`，直接使用该 trait 的方法。由接口擦除后的 A 视图不能自动转成 B 视图，也不能自动恢复成原始 `T`。

新增实现不会改变旧的 Go `AsXXX()` ABI，但可能使未限定的 hgo 方法调用变得歧义。对稳定性敏感的调用可以使用限定形式；不能把 Go API 稳定性扩大成所有 hgo 源码永不受新增 impl 影响的承诺。

## 6. 期望类型驱动的转换

类型检查先选择 impl，再产生 `TraitView(value, implID)` 节点；节点记录 declare 或自定义实现模式。前者在补齐 marker 的构建视图中直接转换原值，后者构造所选 wrapper 后装箱。原表达式只求值一次。

该转换还必须携带源级权限、借用来源和有效范围；权限检查是适配前提，后端都是 `*T` 不代表可相互替代。短借用形成的 trait 视图不能返回、外部存储或异步传递而超出该借用范围；仅有 raw Ptr 或 marker 不授予安全视图。下表描述的是已经满足这些前提的适配位置。

| 使用处 | 是否自动适配 |
| --- | --- |
| 实参是 `*T`，形参是已声明实现的 trait | 是 |
| 赋值/返回的期望类型是已声明实现的 trait | 是 |
| `x := value`，没有 trait 期望类型 | 否，保留具体类型 |
| 目标是 `any` 或相同具体类型 | 否 |
| 已经是相同 trait 与相同关联类型绑定的值 | 否，不重复包装 |
| 泛型形参 `T`，约束要求 trait | 不改写实参类型；传递/选择实现证据另行降低 |
| 整个 `[]T` 赋值为 `[]Trait` | 不支持隐式逐元素转换 |
| 切片字面量的单个元素具有 trait 期望类型 | 可以逐元素适配 |
| `TraitA` 值传给 `TraitB` | 不支持隐式转换 |

`any` 与普通 Go interface 的方法集检查仍遵守 Go 互操作规则。不得在任意结构式接口目标下搜索所有 trait wrapper 来“凑”方法集；先显式选定 trait 视图，再传给相容的 Go interface。

编译器不能仅为修复 Go 类型错误而随意增加 `AsXXX()`，也不能因为原始类型新增了匹配方法就跳过既有自定义实现。已有 Go 适配对象的复用见 [Go 原生类型的显式适配](go-interop.md)。

## 7. 原生 Go 类型的直接采用与自定义适配

原生包的业务方法已经满足要求时，用户在自己的 C 包中写 `declare impl AorB::XXX for BBB::Type;` 即可。编译器补 marker 和适配入口，不要求专用声明文件、原生 wrapper 或原生 `AsXXX()`。

声明的定义来源是 C，hgo 按实际导入的元数据使用它，不把它写回 BBB 或注册为其他项目都能看到的实现。纯 Go 构建不读取这条 `.hgo` 声明，也不会获得或依赖本次 hgo 的伴生文件。

原生包已有其他包装类型时，可以对包装类型本身写 declare；要让原始类型拥有不同语义，则写有体 impl，使用正常 hgo 代码调用已有 API。对外部 Go 私有字段的访问仍遵守原包边界，生成 marker 不授予额外的私有访问权。

LSP 在原始类型下展示其显式 impl；直接采用的方法导航到 Go 方法，自定义方法导航到 hgo 方法体。两个原生类型不会仅因为存在适配关系就被当成同一类型。

## 8. Go API 与符号稳定性

每个公开 impl 从第一版起固定实现模式：declare 使用补齐 marker 的原始接收者，有体 impl 使用对应 wrapper。后续增加另一个 impl，不能重命名原有入口，也不能自动切换旧接口值的表示。

这是对早期“所有实现都需要 wrapper”规则的修正。稳定的是已选择的实现模式与适配入口；declare 本身也可以稳定。主动把 declare 改成有体 impl 会改变方法来源、接口动态类型及反射/类型断言结果，应视为需要兼容性评估的 API 变更。

规范身份至少包含包的规范导入路径、声明身份、接收者形式、trait 身份和参数。身份不依赖调用处的 import 别名，也不混入每次构建变化的时间戳或模块补丁版本。

短名称 `AsByteReader` 只是本文示例。正式的默认导出编码尚待单独冻结，但必须满足：

- 同名 trait 来自不同包时可区分；追加新 impl 不使旧名称变化。
- 与用户固有方法冲突时诊断或使用已固定的编码，不能临时改名。
- 编译器使用元数据恢复身份，不通过拆分 `__` 猜语义。
- 编码要可证明无碰撞，或者完整身份校验发现碰撞后明确报错。

简单地以 `__` 分隔，再把原名的连续下划线加一，不能保证可逆；例如边界两侧的单下划线可能产生同一结果。正式 ABI 编码与元数据版本是发布前的必要设计项。

修改已有 trait 的必需方法、签名、关联类型约束或适配语义仍可能破坏兼容性；固定实现模式只解决表示随新增实现变化的问题。

## 9. 非法状态与边界

直接采用保留原生 Go 接收者与方法的行为；`return s` 本身不证明 `s` 非 nil，也不擅自修改原方法对 nil 接收者的处理。自定义 wrapper 的状态要求由其实现和公开边界规则规定，示例中显式拒绝空目标。完整的非空类型与 `pub` 检查策略另行设计。

hgo 编译期要求显式 impl；普通 Go 调用方须同时满足业务方法和 marker。公开 marker 仍可手写，嵌入也可能提升它，因此不能替代 hgo 的精确 impl 元数据，更不能作为不可伪造的运行时安全证明。

## 10. 实现验收条件

以下是未来编译器的验收要求，不是当前已通过的 hgo 测试：

1. 同一类型的两个同名异签名方法均可调用；同名同签名但不同实现也不合并。
2. declare 复用真实业务方法并补齐 marker；缺业务方法时报错。有体 impl 的方法体与 marker 位于 wrapper，不被原始同名方法覆盖。
3. A/B 视图共享原始指针，修改后另一视图观察到变化。
4. 参数、赋值和返回值按期望类型转换；`any`、类型推断和泛型接收者不被意外包装。
5. 接收者表达式只求值一次；重复适配不产生递归 wrapper 链。
6. 缺少 impl、重复 impl、指针不匹配和不明确方法调用给出源级诊断。
7. 增加第三个 impl 后，既有 Go 工厂符号、直接采用/自定义模式及接口动态表示保持稳定。
8. LSP 与编译器选择相同 impl，且可以跳到对应的源实现或 Go wrapper 方法。

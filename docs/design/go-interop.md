# Go 原生类型的显式适配

状态：所有可形成运行时视图的显式 impl 统一使用 impl 包拥有的 wrapper，marker 保留在 wrapper 上。declare 逐方法转发原生业务，有体 impl 执行用户代码；固定包级工厂负责适配，便利 helper 只转发工厂。原生类型不追加方法，具体符号编码与接收权限语法尚未冻结，当前没有 hgo 编译器实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D05、D06、D24：使用方声明、impl 包生成统一 wrapper、保留 marker、不要求原生包预备 As 或 wrapper。 |
| 暂定 | T08、T17：声明合并、共享载体协议、工厂与便利入口编码、发布元数据。 |
| 未定 | U03、U07：接收权限身份、跨包重复声明的稳定生成身份与缓存细节。 |

状态编号见[决策登记](decisions.md)。`*native::T` 仅描述精确 Go 接收者身份，不能绕过 Ptr / 借用权限体系；见[源级权限约束](overview.md#35-源级权限与-go-表示必须分开)。其他 declare 形式见[原生 API](foreign-api.md)。

## 1. 声明属于使用方 C 包

```hgo
package C
import AorB "example.org/protocols"
import BBB "example.org/native"

declare impl AorB::XXX for BBB::Type;
```

C 采用 BBB::Type 的已有业务方法来实现 AorB::XXX；声明、生成 wrapper 和固定工厂均由 C 拥有。不要求 AorB 或 BBB 作者增加方法，不写入其源码、模块缓存或标准库。

hgo 按实际参与编译的源码及元数据加载显式关系。纯 Go 不读取 `.hgo`，但可以调用 dist 内普通 Go 工厂和 wrapper；其他项目继续使用原生 BBB。声明无需专用 `.d.hgo`，声明文件可用于分发契约，不能替代真实 Go 类型信息的验签。

## 2. 原生包只需要业务方法

```go
package buffer

type Buffer struct { Data []byte }
func (b *Buffer) Read(p []byte) (int, error) {
    return copy(p, b.Data), nil
}
```

使用方可以写：

```hgo
package adapters
import native "example.org/native/buffer"
import byteio "example.org/hgo/byteio"

type Buffer = *native::Buffer
declare impl byteio::Reader for Buffer;
```

别名不产生新类型。`for native::Buffer` 与 `for *native::Buffer` 的方法集分别检查，不通过自动取址补齐。尤其不能利用生成代码中的局部变量可寻址，把只存在于 `*T` 的方法视为 T 方法。[Go 方法集](https://go.dev/ref/spec#Method_sets)

declare 表示完整采用已有业务与 trait 调用契约。缺 Read 是错误，原生类型缺 marker 或 As 不是错误；这些能力位于新生成的 wrapper 和工厂。签名兼容不证明不逃逸、只读或并发行为，声明作者仍负责可信的外部契约。

## 3. wrapper、marker 与固定包级工厂

trait 输出包含业务方法及 marker：

```go
// byteio 包的片段；正式符号编码未定。
type Reader interface {
    Read([]byte) (int, error)
    HyperGoTraitByteReader()
}
```

以外部标准库类型 `*bytes.Buffer` 为例，采用声明会在 adapters 包产生以下机制代码：

```go
package adapters

import "bytes"
import byteio "example.org/hgo/byteio"

type bufferReader struct { target *bytes.Buffer }
func (v bufferReader) HyperGoTraitByteReader() {}
func (v bufferReader) Read(p []byte) (int, error) {
    return v.target.Read(p)
}
func AsBufferReader(b *bytes.Buffer) byteio.Reader {
    if b == nil { panic("hgo: nil Buffer") }
    return bufferReader{target: b}
}
```

此例工厂采用非空接收者契约；其他接收者的检查由各自源级契约决定。marker 必须使用导出符号，让跨包 wrapper 可以实现接口；身份编码还需包含完整 trait 身份与必要实例信息。

保存的 target 仍是同一个 `*bytes.Buffer`，但接口动态类型是 adapters.bufferReader。修改共享对象可被其他合法视图观察到；反射、断言和比较不再与裸原生接口值相同。

wrapper 使用命名字段，逐项生成 trait 方法。禁止匿名嵌入 `*bytes.Buffer` 来省略转发，否则 WriterTo 等方法会提升，`io.Copy` 可以绕过 trait 所选的 Read，甚至绕过自定义行为。

### 3.1 便利入口只转发固定工厂

外部类型不能增加 `b.AsReader()`。Go 用户可调用工厂，也可使用 adapters 自有的辅助类型：

```go
// 接上面的 adapters 包；不匿名嵌入 bytes.Buffer。
type BufferAdapters struct { target *bytes.Buffer }
func ForBuffer(b *bytes.Buffer) BufferAdapters {
    return BufferAdapters{target: b}
}
func (a BufferAdapters) AsReader() byteio.Reader {
    return AsBufferReader(a.target)
}
```

例如 `adapters.ForBuffer(b).AsReader()`；对于名为 Sized 的 trait，同一规则可形成 `adapters.ForBuffer(b).AsSized()`。helper 不建立第二套实现选择或业务逻辑，所有校验与 wrapper 构造经固定工厂完成。

只有类型与 impl 同包，且属于 Go 允许定义方法的本地 named type 时，才可生成 `t.AsSized()` 这类本地便利方法；它也只转发工厂。`type Ptr[T any] *T` 这类具名指针类型即使本地定义，也不能作为 Go 方法接收者基类型，仍使用包级工厂或自有 facade。泛型推导、公开命名、名字冲突及方法集影响需在正式 ABI 中规定，不以便利方法代替固定工厂身份。

### 3.2 依赖方向与生成归属

wrapper 所在的 impl 包导入 trait 包和原生包，原生包不因适配而反向导入 trait 包。这样消除了旧注入方案引起的反向依赖；源程序本来存在的包循环仍须诊断。

同一个规范 impl 的 wrapper、marker、工厂和 metadata 应关联同一稳定生成身份。相容声明去重不能按临时导入顺序选择宿主；跨独立 dist 的重复声明如何汇合须由发布规则固定。

## 4. 构建与整个 dist 发布

建议流程：

1. 读取 Go 模块图、原生包类型信息及实际导入的 hgo 元数据。
2. 合并相容声明，校验精确接收者方法集、业务签名、权限契约与 impl 身份。
3. 在 impl 包生成 wrapper、marker、逐方法转发或用户方法体以及固定工厂。
4. 生成可选的本地 As 或外部 ForBuffer/As helper，确保仅转发固定工厂。
5. 输出普通 Go 代码及版本一致的声明 / 元数据，由标准 Go 工具链检查与构建。

发布整个 dist，包含 `.go`、`.d.hgo` 与可选 `.hgo`；Go 编译 `.go`，hgo 额外读取声明或源码。所需适配代码已经位于 impl 的普通 Go 包中，`*bytes.Buffer` 示例无需增强标准库或要求下游启用 overlay。加载与发布约束见 [RFC 0001](../rfcs/0001-package-loading.md)。

旧稿曾试验向原类型补 marker 的隔离视图。`tests/design/marker` 及[测试说明](../../tests/README.md)中的 overlay / replace 记录仅是历史机制实验，不是当前 trait 生成或发布方案；它们通过不能证明统一 wrapper 前端已实现。

## 5. 自定义 impl 与原生采用

```hgo
import textio "example.org/hgo/textio"
impl textio::Reader for Buffer {
    read() string { return string(self.Data) }
}
```

wrapper 保存原始接收者，自定义方法执行所写 body，即使原生方法同名同签名也不能替代。声明采用和自定义实现的差异如下：

| 源级形式 | wrapper 业务方法 | marker 位置 | Go 接口动态类型 |
| --- | --- | --- | --- |
| `declare impl Trait for T;` | 显式转发所采用的 T 方法 | impl 包的 wrapper | 对应 wrapper |
| `declare impl Trait for *T;` | 显式转发同一指针的原生方法 | impl 包的 wrapper | 对应 wrapper |
| `impl Trait for T { ... }` | 执行用户方法体 | impl 包的 wrapper | 对应 wrapper |

有体 impl 仍须完整提供必需方法，漏写不自动委托。已有原生适配对象可以作为接收者采用；带游标、缓存等状态的对象应保存并复用，不在每次方法调用中重新创建。

trait 视图不能为满足原生 `*T` 参数而隐式 unwrap；使用保留的原变量或明确的受检查桥接。否则会绕过所选实现，或将视图中的借用变成可自由留存的裸指针。

## 6. 校验、来源与唯一性

业务方法按 trait 规定的规范 Go 名称、签名和接收者方法集校验，不能隐式改参数、结果或关联类型。wrapper 和工厂接受普通 Go 类型检查，hgo 另外保存精确 impl 元数据；可借助 [go/types](https://pkg.go.dev/go/types) 和 [go/packages](https://pkg.go.dev/golang.org/x/tools/go/packages) 读取原生信息。

外部类型与外部 trait 可在 C 组合声明，实现来源仍是 C，不把这条关系注册为 BBB 对所有项目公开的实现。元数据保留声明来源、接收权限、借用来源、关联类型、业务来源和稳定工厂身份。

参与构建的有效集合按规范 `(接收者类型, trait, trait 参数)` 合并相容 declare；别名归一、同一绑定与契约幂等，并保留全部来源。单个生成身份只输出一份 marker / wrapper。两个有体实现或矛盾契约报冲突；declare 与有体实现混合的具体语义仍待定，不能静默替换。[RFC 0003](../rfcs/0003-declare-impl.md) 细化合并约束。

版本、replace、平台、tags 或声明集合改变时应重新校验。缓存需包含原生类型指纹、完整声明集合、生成规则和工具链配置，不能把某次构建的 wrapper 当成原生类型本来具有的方法。

公开 marker 可被 Go 手写或通过嵌入提升，所以不能从它反推新 hgo impl。权限与借用也不能从最终 `*T` 布局推导；marker 保留其接口约束作用，但不承担不可伪造的安全证明。

## 7. LSP 与稳定性

```text
BBB::Type
  原生方法                    [导航到原 Go 源码]
  declare impl AorB::XXX      [来源 C；生成转发 wrapper]
    trait 方法               [导航到采用的 Go 方法]
  有体 impl                  [来源相应 hgo 包]
    trait 方法               [导航到 hgo 方法体]
```

LSP 与编译器使用同一元数据。生成 wrapper、marker、工厂和 helper 均可追溯到声明；不能伪装成上游源码已有方法。

新增 impl 不改变既有 wrapper 和固定工厂。改变原生采用契约或用户方法体仍需兼容性评估，便利入口不能根据当前可见声明集合临时选择不同 impl。

## 8. 实现验收条件

1. 外部 `*bytes.Buffer` 无需 As / marker，生成 adapters 包可用普通 Go 工具链构建。
2. declare 的 wrapper 逐项转发；有体 wrapper 执行用户代码；两者 marker 均保留且可跨包实现接口。
3. 固定工厂、本地 As、外部 ForBuffer/As 一致；后两者只转发工厂，不重新选择实现。
4. 精确 T / *T 方法集不被自动取址掩盖，原生源码与方法集没有变化。
5. 指针 target 保持共享身份，Go 接口动态类型为 wrapper；额外 WriterTo 等方法不被提升。
6. 没有隐式 unwrap；nil、比较、函数值、借用期限及包装分配的行为有正反例和成本说明。
7. 声明合并、稳定生成归属、缓存与 LSP 导航一致；整份 dist 在独立纯 Go 下游可构建。

这些是未来前端与构建驱动的验收要求；独立 Go 片段只验证对应机制。

## 9. 原生 Go API 的两种视图

未知原生 `*T` 默认导入为 raw Ptr，未描述调用走 unsafe 边界。不能自动把未知指针解释成非空借用或 Option[Arc]。

普通 declare func 可以采用已审核的读借用、写借用或共享句柄契约；需要转换形状时提供真实桥接。declare impl 在同一可信边界内采用原生业务，但其 marker 与转发只存在于 wrapper，不会顺便把其他原生调用变成 safe。

Ptr、AsPtr、unsafe 效果和回调桥接见[原生 API 与裸指针](foreign-api.md)。

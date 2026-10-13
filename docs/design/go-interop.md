# Go 原生类型的显式适配

状态：P4 已在新管线完成。运行时 impl 统一使用声明包拥有的 wrapper，marker 保留在 wrapper 上。declare 转发原生业务，有体 impl 执行用户代码；固定工厂负责适配，As/For 只转发工厂。外部类型不追加方法或 carrier 类型锚。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D05、D06、R24：使用方声明、impl 包生成统一 wrapper、保留 marker、不要求原生包预备 As 或 wrapper。 |
| P4 | 结构化代入 Self、精确原生方法集、相同 declare 合并、名称冲突检查、固定工厂与保存 owner 的 For。 |
| 后续 | P5 跨包 Goxide 加载与发布信息恢复；共享适配包、缓存和 LSP 另随相应阶段推进。 |

状态编号见[决策登记](decisions.md)。Self 始终绑定完整声明目标，成员的 Self / &Self / &mut Self 按结构代入后核对 Go 方法集，不消去指针层或擦除权限。其他 declare 形式见[原生 API](foreign-api.md)。

诊断边界仍按 D19/D20：基础类型权限照常检查，别名/逃逸风险分析不属 MVP，不以未知效果或无法证明不逃逸普遍拒绝。

## 1. 声明属于使用方 C 包

```goxide
package C
use AorB;
use BBB;

declare impl AorB::XXX for BBB::Type;
```

C 采用 BBB::Type 的已有业务方法来实现 AorB::XXX；声明、生成 wrapper 和固定工厂均由 C 拥有。不要求 AorB 或 BBB 作者增加方法，不写入其源码、模块缓存或标准库。

goxide 按实际参与编译的源码及元数据加载显式关系。纯 Go 不读取 `.gox`，但可以调用 dist 内普通 Go 工厂和 wrapper；其他项目继续使用原生 BBB。声明无需专用 `.d.gox`，声明文件可用于分发契约，不能替代真实 Go 类型信息的验签。

## 2. 原生包只需要业务方法

```go
package buffer

type Buffer struct { Data []byte }
func (b *Buffer) Read(p []byte) (int, error) {
    return copy(p, b.Data), nil
}
```

使用方可以写：

```goxide
package adapters
use native;
use byteio;

type Buffer = native::Buffer
declare impl byteio::Reader for Buffer;
```

假定 Reader 声明 `func read(self &mut Self, p []byte) (int, error)`。别名不产生新类型，Self = native::Buffer，接收类型为 &mut native::Buffer，故验证精确 *native.Buffer 方法集。若声明目标是 *native::Buffer，self Self 才对应该方法集；self &Self 则得到 **native.Buffer，不得消层或自动取址补齐。[Go 方法集](https://go.dev/ref/spec#Method_sets)

declare 表示完整采用已有业务与 trait 调用契约。缺 Read 是错误，原生类型缺 marker 或 As 不是错误；这些能力位于新生成的 wrapper 和工厂。签名兼容不证明不逃逸、只读或并发行为，声明作者仍负责可信的外部契约。

## 3. wrapper、marker 与固定包级工厂

trait 输出包含业务方法及 marker：

```go
// byteio 包的片段；正式符号编码未定。
type Reader interface {
    Read([]byte) (int, error)
    GoxideTraitByteReader()
}
```

以外部标准库类型 `*bytes.Buffer` 为例，采用声明会在 adapters 包产生以下机制代码：

```go
package adapters

import "bytes"
import byteio "example.org/goxide/byteio"

type bufferReader struct { target *bytes.Buffer }
func (v bufferReader) GoxideTraitByteReader() {}
func (v bufferReader) Read(p []byte) (int, error) {
    return v.target.Read(p)
}
func AsBufferReader(b *bytes.Buffer) byteio.Reader {
    return bufferReader{target: b}
}
```

工厂不增加统一非空检查；原生方法的 nil 行为仍由该方法决定。marker 使用导出符号并编码完整 trait 身份，让跨包 wrapper 可以实现接口。

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

本地、可作为 Go receiver 的类型可以生成 As。enum 值视图在 E 接口声明 As，由指针变体一行转发同一工厂；借用 enum 保存 *E owner，使用 ForE。外部类型与具名指针类型使用包级工厂或 facade，不生成非法 receiver。`For<Base>(*Base)` 统一保存 owner，值视图从 *target 取值，借用视图保持 target；外部纯值适配可用工厂(value)或 ForT(&value)。

### 3.2 依赖方向与生成归属

wrapper 所在的 impl 包导入 trait 包和原生包，原生包不因适配而反向导入 trait 包。这样消除了旧注入方案引起的反向依赖；源程序本来存在的包循环仍须诊断。

同一个规范 impl 的 wrapper、marker、工厂和 metadata 应关联同一稳定生成身份。相容声明去重不能按临时导入顺序选择宿主；跨独立 dist 的重复声明如何汇合须由发布规则固定。

## 4. 构建与整个 dist 发布

建议流程：

1. 读取 Go 模块图、原生包类型信息及实际导入的 goxide 元数据。
2. 合并相容声明，校验精确接收者方法集、业务签名、权限契约与 impl 身份。
3. 在 impl 包生成 wrapper、marker、逐方法转发或用户方法体以及固定工厂。
4. 生成可选的本地 As 或外部 ForBuffer/As helper，确保仅转发固定工厂。
5. 输出普通 Go 代码及版本一致的声明 / 元数据，由标准 Go 工具链检查与构建。

发布整个 dist，包含 `.go`、`.d.gox` 与可选 `.gox`；Go 编译 `.go`，goxide 额外读取声明或源码。所需适配代码已经位于 impl 的普通 Go 包中，`*bytes.Buffer` 示例无需增强标准库或要求下游启用 overlay。加载与发布约束见 [RFC 0001](../rfcs/0001-package-loading.md)。

旧稿曾试验向原类型补 marker 的隔离视图。`tests/design/marker` 及[测试说明](../../tests/README.md)中的 overlay / replace 记录仅是历史机制实验，不是当前 trait 生成或发布方案；它们通过不能证明统一 wrapper 前端已实现。

## 5. 自定义 impl 与原生采用

```goxide
use textio;
impl textio::Reader for Buffer {
    func read(self &Self) string { return string(self.Data) }
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

业务方法按 trait 的规范 Go 名称、签名和代入 Self 后的精确接收者方法集校验，不能隐式改参数、结果或关联类型。wrapper 和工厂接受普通 Go 类型检查；新编译器用标准库 [go/types](https://pkg.go.dev/go/types) 及 Go 工具链提供的真实包信息验签，不添加外部依赖。

外部类型与外部 trait 可在 C 组合声明，实现来源仍是 C，不把这条关系注册为 BBB 对所有项目公开的实现。元数据保留声明来源、接收权限、借用来源、关联类型、业务来源和稳定工厂身份。

参与构建的有效集合按规范目标与 trait 身份合并相同 declare；别名归一、同一绑定与契约幂等，并保留来源。单个生成身份只输出一份 marker/wrapper。两个有体实现、矛盾契约或混合 declare/有体实现均诊断。[RFC 0003](../rfcs/0003-declare-impl.md) 细化合并约束。

外部原生类型不注入 `GoxideInternalTypeCarrierForSelf`。P4 的 declare 提供运行时视图和普通 Go 适配，不要求未改造的外部目标自动满足 carrier 泛型入口；本包生成类型的泛型证据按[关联类型合同](associated-types.md)处理。

版本、replace、平台、tags 或声明集合改变时应重新校验。缓存需包含原生类型指纹、完整声明集合、生成规则和工具链配置，不能把某次构建的 wrapper 当成原生类型本来具有的方法。

公开 marker 可被 Go 手写或通过嵌入提升，所以不能从它反推新 goxide impl。权限与借用也不能从最终 `*T` 布局推导；marker 保留其接口约束作用，但不承担不可伪造的安全证明。

## 7. LSP 与稳定性

```text
BBB::Type
  原生方法                    [导航到原 Go 源码]
  declare impl AorB::XXX      [来源 C；生成转发 wrapper]
    trait 方法               [导航到采用的 Go 方法]
  有体 impl                  [来源相应 goxide 包]
    trait 方法               [导航到 goxide 方法体]
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

原生 Go `*T` 的旧 raw `Ptr[T]` 导入方案与普通参数桥接现列 U15 待协调；D19 明确普通调用不因未描述而要求 unsafe。此前将所有未知 `*T` 自动投影为 `Option[Arc[T]]` 的方案不再作为原生默认视图；`Option[Arc[T]]` 仍可用于明确选择的声明或包装接口。

普通 declare func 可以采用已审核的读借用、写借用或共享句柄契约；需要转换形状时提供真实桥接。declare impl 在同一可信边界内采用原生业务，但其 marker 与转发只存在于 wrapper，不会顺便把其他原生调用变成 safe。

Ptr、AsPtr、unsafe 效果和回调桥接见[原生 API 与裸指针](foreign-api.md)。

# RFC 0019：统一 trait wrapper 与包级适配工厂

## 状态与范围

已确认当前方向（2026-10-01）：运行时 trait 视图统一使用独立 wrapper；包级适配函数作为后端入口，同时为 Go 用户生成本包方法 helper 或外部类型的适配入口。trait 接口中的 marker 保留，由 wrapper 实现。具体 ABI 名称、权限和比较细则尚待定稿，未实现。该方案替代此前向外部原始类型注入 marker、保留原始动态类型的设计。

适用范围是能形成运行时实例视图的 trait。Default 等静态操作、泛型实现证据仍按 [RFC 0013](0013-derive.md) 单独降低，不要求所有调用都实际构造 wrapper。旧发布反例见 [RFC 0001](0001-package-loading.md)，本篇 W01 给出其具体解法。

## 源级规则

1. `declare impl` 验证并采用原生业务方法，由 wrapper 显式转发；有体 impl 的方法体直接生成在对应 wrapper 上。两者都不向原生类型添加方法。
2. trait 在 A 包、类型在 B 包、impl 在 C 包时，wrapper 和工厂生成在 C，依赖方向为 C → A、B。不默认放在 trait 定义包，也不修改 B 的源码、模块缓存或 GOROOT。
3. 用包级函数适配，例如 `adapters.AsBytesBufferSized(b)`。hgo 的期望类型转换和限定调用直接记录工厂/impl 身份，不要求 `b` 在 Go 中真的具有 As 方法。
4. 每个规范运行时 impl 使用自己的 wrapper；同名不同 trait 方法不会碰撞。正式编码要包括稳定包/类型/trait/参数身份，示例短名不构成最终 ABI。
5. 保存命名字段并只提供选定 trait 的方法。`*T` 实现保存同一个 `*T`；`T` 实现按明确的值复制/接收权限规则处理，不借生成代码的自动取址改变实现键。
6. 运行时 trait 的 Go 接口包含业务方法和 marker；wrapper 必须实现二者。跨包实现需要可导出的稳定 marker 名；trait 包私有方法不能由另一个包简单声明同名方法来实现。marker 与 helper 分工不同，helper 不能代替 marker。hgo 精确显式关系仍由元数据决定。
7. 相同视图赋值不重复包装；具体值进入 trait 视图时不重复求值。`any` 或原生 `*T` 参数位置不隐式创建或解开 wrapper。
8. 类型与 impl 同属生成包，且类型允许作为 Go 方法接收者时，可生成 `t.AsSized()` 便利方法；外部类型或不满足接收者要求的本地类型使用 `adapters.ForBuffer(t).AsSized()` 一类门面。两类 helper 只转发既有包级适配函数，不另建实现选择逻辑，不将外部类型的方法写回其定义包。

## 转译示例

### W01：bytes.Buffer 实现 Sized，普通 Go 直接消费

以下 hgo 是两包的机制片段；`*bytes::Buffer` 仅标识原生接收者身份，具体源级权限仍需 [RFC 0005](0005-borrows.md)。本例工厂选择非空目标契约，不把此 nil 策略推广为所有原生接收者规则。

```hgo
// example.org/demo/contracts
pub trait Sized { len() int }

// example.org/demo/adapters
import contracts "example.org/demo/contracts"
import bytes "bytes"
declare impl contracts::Sized for *bytes::Buffer;
pub func view(b *bytes::Buffer) contracts::Sized { return b }
```

生成 `contracts/sized.go`（完整文件，module 为 `example.org/demo`）：

```go
package contracts

type Sized interface {
    Len() int
    HyperGoTraitSized()
}
```

生成 `adapters/buffer_sized.go`（与上一文件在同一模块，完整文件）：

```go
package adapters

import (
    "bytes"
    "example.org/demo/contracts"
)

type bufferSized struct { inner *bytes.Buffer }

func (v bufferSized) Len() int { return v.inner.Len() }
func (bufferSized) HyperGoTraitSized() {}

func AsBytesBufferSized(b *bytes.Buffer) contracts.Sized {
    if b == nil { panic("hgo: nil Sized target") }
    return bufferSized{inner: b}
}

func View(b *bytes.Buffer) contracts.Sized {
    return AsBytesBufferSized(b)
}
```

Go 消费方函数体片段，文件导入 `bytes` 与 `example.org/demo/adapters`：

```go
b := bytes.NewBufferString("ab")
s := adapters.View(b)
before := s.Len() // 2
b.WriteString("c")
after := s.Len() // 3：wrapper 保存同一个 b，没有复制 Buffer。
_, _ = before, after
```

整个 dist 包含这些 `.go` 及声明信息，标准库 `bytes` 无需任何新增文件。Go 侧可以使用包级工厂或 W05 的便利入口；原始 `*bytes.Buffer` 不新增 `AsSized()`。hgo 若以后提供方法式适配语法，也只是降低为上述工厂的语法糖。

### W02：有体实现不能被原生方法覆盖

源级实现替换为以下方法体时（假设单独构建此版本，不与 W01 的 declare 同时生效）：

```hgo
impl contracts::Sized for *bytes::Buffer {
    len() int { return 42 }
}
```

对应 wrapper 方法替换为：

```go
func (v bufferSized) Len() int { return 42 }
```

此时 `s.Len()` 必须返回 42。静态优化可以省去接口装箱，但不能将该调用替换为 `b.Len()`。统一 wrapper 不自动决定 declare 与有体 impl 共现时谁提供行为，不能静默覆盖。

### W03：不能用匿名嵌入代替受控转发

以下完整 Go 包是错误生成策略的反例，使用标准库即可构建；它不属于期望输出：

```go
package badexample

import (
    "bytes"
    "io"
)

type badReader struct { *bytes.Buffer }
func (v badReader) Read(p []byte) (int, error) {
    n, err := v.Buffer.Read(p)
    for i := 0; i < n; i++ {
        if p[i] == 'a' { p[i] = 'A' }
    }
    return n, err
}
func Observe() string {
    src := badReader{Buffer: bytes.NewBufferString("a")}
    var dst bytes.Buffer
    _, _ = io.Copy(&dst, src)
    return dst.String() // "a"：提升的 WriteTo 绕过了自定义 Read。
}
```

用命名字段 `inner *bytes.Buffer` 并只实现 Reader 的方法，可避免意外实现 `io.WriterTo`。即使 hgo 源码不做开放接口断言，原生 Go 的可选接口分派仍实际存在。

### W04：本包类型的 Go 方法 helper

以下片段假定 `LocalBuffer` 是当前包定义的结构体类型，与该 impl 同包，且生成了 `AsLocalBufferSized`。原类型不带 trait 的业务 marker；As 方法只是便利入口。

```go
func (b *LocalBuffer) AsSized() contracts.Sized {
    return AsLocalBufferSized(b)
}
```

Go 调用 `b.AsSized()`。若类型来自已发布的另一个包，即使它也由 hgo 编写，第三方 impl 包也不能给它追加此方法，应使用 W05。本地定义也不保证能声明方法：例如 `type Ptr[T any] *T` 不满足 Go 接收者规则，同样使用包级工厂或独立门面。

### W05：外部类型的 Go 适配入口

将下列代码追加到 W01 的 adapters 包；这是依赖 W01 导入和工厂的 Go 片段。

```go
type BufferViews struct { inner *bytes.Buffer }

func ForBuffer(b *bytes.Buffer) BufferViews {
    return BufferViews{inner: b}
}
func (v BufferViews) AsSized() contracts.Sized {
    return AsBytesBufferSized(v.inner)
}
```

Go 调用 `adapters.ForBuffer(b).AsSized()`；多个转换可先保存 `views := adapters.ForBuffer(b)`。该入口只列本适配包提供的转换，不自动收集程序所有包中的 impl；追加其他 trait 可增加对应 As helper。它持有同一个指针，不复制 Buffer。所有非空检查仍由选定工厂执行，`ForBuffer(nil).AsSized()` 在 W01 的工厂处拒绝。

### W06：hgo 固定生成函数调用，helper 服务 Go 用户

源级片段假定 b 拥有合法接收权限，`consume` 接受 Sized：

```hgo
var s contracts::Sized = b
consume(b)
n := b.len::<contracts::Sized>()
m := s.len()
```

生成的 Go 函数体片段：

```go
var s contracts.Sized = adapters.AsBytesBufferSized(b)
consume(adapters.AsBytesBufferSized(b))
n := adapters.AsBytesBufferSized(b).Len()
m := s.Len()
_, _ = n, m
```

前端选择 impl，元数据记录工厂包路径与符号，后端据此插入调用。原表达式在每个源使用处只求值一次，已有 s 不重复包装；工厂不依赖 Go convenience helper 是否存在。静态优化不能绕过有体实现或改变对外可观察的视图表示。

## 拒绝与边界

| 编号 | 情形 | 约束 |
| --- | --- | --- |
| W-N1 | 给非本地 `*bytes.Buffer` 生成 As 或 marker 方法 | 不生成；使用 C 包工厂及 C 包 wrapper |
| W-N2 | 原生只有 `func (*T) M()`，却 declare `Trait for T`，靠 wrapper 内可寻址副本调用 | 验签拒绝；生成 Go 能自动取址不代表源接收者匹配 |
| W-N3 | 实现 `Trait for *T` 却让 wrapper 存 `T` 副本 | 错误降低，会改变状态与复制成本 |
| W-N4 | 把 `Sized` 视图传给要求原生 `*bytes.Buffer` 的函数 | 不隐式解包；传原始 b 或使用另行明确的转换契约 |
| W-N5 | 为方便生成接口而强制 Default、静态 Self 或任意泛型方法成为实例 wrapper | 不支持这种抹除；按相应静态规则处理 |
| W-N6 | 普通 Go 类型只有 Len，却没有 trait marker | 不能赋给 Sized；helper 的存在不会改变此条件 |
| W-N7 | As/For 便利名称与现有业务方法或同名 trait helper 碰撞 | 按正式名称/导出规则诊断或显式命名；不重命名既有公开入口 |
| W-N8 | 给本地具名指针类型 Ptr 直接生成 As 方法 | 不生成非法 Go 接收者；使用包级工厂或独立门面 |

保存指针身份不等于保存接口动态类型。W01 中动态类型为 `adapters.bufferSized`，不是 `*bytes.Buffer`；Go 的断言、反射及接口比较可观察这个区别。W01 拒绝 nil；其他允许 nil 原生接收者的适配需要单独说明 nil 如何装箱、调用，不能以 `interface != nil` 证明 inner 非空。

wrapper 不抹去借用期限，也不自动实现线程安全。值 wrapper 的方法调用还可能复制 target；有不可复制状态或不同接收权限时须先定义合法载体，不把同一示例套到所有 T。

## 双向互操作

hgo → Go：`var s Sized = b` 或限定调用使用已选 factory/impl；普通 Go 业务接口只看到 wrapper 明确实现的方法。原始 b 仍可在合法权限下传给要求 `*bytes.Buffer` 的 Go API。

Go → hgo：导入 adapters，调用 `AsBytesBufferSized` 或 `ForBuffer(b).AsSized()`，再使用 contracts.Sized；本地类型也可通过自身 As helper 进入同一工厂。生成模块中已包含所有 wrapper 与 marker，无需给外部包添加源码。下游 hgo 读取 `.d.hgo` 恢复显式关系与工厂绑定，不从 Go 方法集猜测 impl。

调用者须能正常导入 impl 包。不能把 C 中声明的实现反向注入 C 的底层依赖，使依赖图出现 C → B → C；真正的 Go 包循环仍须诊断或通过调整源级依赖拆分。

## 验收与未定项

未来验收：W01 在普通 Go 主模块直接导入后返回 2/3，W02 返回 42，W03 展示匿名嵌入错误策略返回小写 a、受控转发版本返回大写 A；W04/W05 与直接工厂产生相同视图/行为，W06 不借 helper 重新选择 impl。检查 marker、接收者求值次数、原指针共享、多 trait 同名方法和权限拒绝；不能只检查是否可编译。当前没有执行本篇 Go 编译或 hgo 端到端验证。

相容 declare 的合并与 Go 载体身份分开：C1/C2 可以声明同一逻辑采用关系，但各自的本地 wrapper 是不同 Go 类型。工厂及载体应由固定的声明包和实现身份确定，不随下游新增依赖重新选择归属。若需要跨包相同动态类型，应指定公共适配包 C0，其他包复用其工厂；不以构建顺序选择一个临时 owner。

待定：正式工厂/marker/helper 编码、便利名称冲突策略、具体接收权限、相容声明的工厂选择及公共适配包协议、nil 与比较契约。wrapper 的直接转发可以内联，接口装箱是否逃逸需实测，不承诺额外分配恒为零。旧 marker 注入夹具仅作为历史机制保留，不把旧实验通过当作本方案通过。类型关联函数和构造函数的候选语法另见 [RFC 0020](0020-associated-functions.md)。

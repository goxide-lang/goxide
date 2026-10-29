# RFC 0019：统一 trait wrapper 与包级适配工厂

## 状态与范围

已确认：运行时 trait 视图统一使用独立 wrapper；固定包级工厂作为后端入口，为 Go 用户提供 As/For helper。trait 接口保留导出 marker，由 wrapper 实现。P4 已在新管线完成；Self 接收权限、enum 便利入口和 nil 规则按本篇及[trait 合同](../design/traits.md)。原始类型 marker 注入方案已被替代。

`declare impl` 及无体采用的相容合并已移除。本篇描述普通有体 impl 的 wrapper、marker、固定工厂和 Go 双向调用；既有 `declare func/type` 支持见 [RFC 0003](0003-declare-impl.md)。

适用范围是能形成运行时实例视图的 trait。无 self 的关联操作及泛型证据使用空 carrier，关联类型 witness 只参与 Go 类型推导；P2 派生保持 [RFC 0013](0013-derive.md) 的既有 helper。不要求静态调用构造运行时 wrapper。旧发布反例见 [RFC 0001](0001-package-loading.md)，本篇 W01 给出其具体解法。

## 源级规则

1. 有体 impl 的方法体直接生成在对应 wrapper 上，不向外部原生类型添加方法。复用原生业务时，在方法体中显式调用。
2. 满足有体 impl 归属规则的实现，其 wrapper 和工厂生成在 impl 包。完整头不满足归属时使用自有包装类型；生成包按真实依赖导入 trait 包和原生包。不默认放在 trait 定义包，也不修改原生类型定义包、模块缓存或 GOROOT。
3. 用包级函数适配，例如 `adapters.AsBufferSized(b)`。goxide 的期望类型转换和限定调用直接记录工厂/impl 身份，不要求 `b` 在 Go 中真的具有 As 方法。
4. 每个规范运行时 impl 使用自己的 wrapper；同名不同 trait 方法不会碰撞。正式编码要包括稳定包/类型/trait/参数身份，示例短名不构成最终 ABI。
5. 保存命名字段并只提供选定 trait 的方法。有体 impl 的 Self 为基础 T；全值实例成员捕获 T，全只读借用成员捕获 `&T`，含可写或值/借用混合捕获 `&mut T`，每个方法仍按自身 self 声明检查。普通原生调用照常检查 Go 方法集与源级权限，不自动消去指针层。
6. 运行时 trait 的 Go 接口包含业务方法和 marker；wrapper 必须实现二者。跨包实现需要可导出的稳定 marker 名；trait 包私有方法不能由另一个包简单声明同名方法来实现。marker 与 helper 分工不同，helper 不能代替 marker。Goxide 精确显式关系由已选可达源码声明与类型检查决定。
7. 相同视图赋值不重复包装；具体值进入 trait 视图时不重复求值。`any` 或原生 `*T` 参数位置不隐式创建或解开 wrapper。
8. 类型与 impl 同属生成包，且类型允许作为 Go 方法接收者时，可生成 `t.AsSized()` 便利方法；外部类型或不满足接收者要求的本地类型使用 `adapters.ForBuffer(t).AsSized()` 一类门面。两类 helper 只转发既有包级适配函数，不另建实现选择逻辑，不将外部类型的方法写回其定义包。
9. 本包 enum 的值视图在 E 接口声明 As，由各指针变体一行转发固定工厂，保留 `e.AsNamed().Name()`；业务方法仍只在 wrapper。借用 enum 视图使用 `ForE(&e).AsNamed()` 保存真实 owner，不能临时复制 E 再取址。
10. 工厂采用 `As<Type><Trait>`，便利入口采用 `As<Trait>` / `For<Type>`；marker 编码完整 trait 身份。名称冲突诊断，不覆盖用户声明或随机更改已生成身份。
11. `For<Base>(*Base)` 保存真实 owner；值视图从 `*target` 取值后调用工厂，借用视图保持 target。外部纯值适配可直接用工厂，或用 `ForT(&value)`。

## 转译示例

### W01：包装 bytes.Buffer，普通 Go 直接消费

以下 Goxide 是两包的机制片段。adapters 定义自己的 Buffer 保存原生指针，再显式实现 contracts::Sized。业务体调用原生指针方法，使用可写 self；生成视图保留同一个原生对象。

```goxide
// example.org/demo/contracts
pub trait Sized { func len(self &mut Self) int }

// example.org/demo/adapters
use contracts;
use bytes;
type Buffer struct { raw *bytes::Buffer }
pub func wrap(raw *bytes::Buffer) Buffer { return Buffer{raw: raw} }
impl contracts::Sized for Buffer {
    func len(self &mut Self) int { return self.raw.Len() }
}
pub func view(b &mut Buffer) contracts::Sized { return b }
```

生成 `contracts/sized.go`（完整文件，module 为 `example.org/demo`）：

```go
package contracts

type Sized interface {
    Len() int
    GoxideTraitSized()
}
```

生成 `adapters/buffer_sized.go`（与上一文件在同一模块，完整文件）：

```go
package adapters

import (
    "bytes"
    "example.org/demo/contracts"
)

type Buffer struct { raw *bytes.Buffer }
func Wrap(raw *bytes.Buffer) Buffer { return Buffer{raw: raw} }

type bufferSized struct { inner *Buffer }

func (v bufferSized) Len() int { return v.inner.raw.Len() }
func (bufferSized) GoxideTraitSized() {}

func AsBufferSized(b *Buffer) contracts.Sized {
    return bufferSized{inner: b}
}

func View(b *Buffer) contracts.Sized {
    return AsBufferSized(b)
}
```

Go 消费方函数体片段，文件导入 `bytes` 与 `example.org/demo/adapters`：

```go
b := bytes.NewBufferString("ab")
wrapped := adapters.Wrap(b)
s := adapters.View(&wrapped)
before := s.Len() // 2
b.WriteString("c")
after := s.Len() // 3：wrapper 保存同一个 b，没有复制 Buffer。
_, _ = before, after
```

整个 dist 包含这些 `.go` 及声明信息，标准库 `bytes` 无需任何新增文件。Go 侧可以使用包级工厂或本地 Buffer 的便利入口；原始 `*bytes.Buffer` 不新增 `AsSized()`。goxide 若以后提供方法式适配语法，也只是降低为上述工厂的语法糖。

### W02：有体实现不能被原生方法覆盖

以本包 LocalBuffer 展示有体实现；其原生 Len 返回 -1，trait 实现明确返回 42：

```goxide
type LocalBuffer struct{}
func (b *LocalBuffer) Len() int { return -1 }
impl contracts::Sized for LocalBuffer {
    func len(self &mut Self) int { return 42 }
}
```

对应 wrapper 方法替换为：

```go
func (v localBufferSized) Len() int { return 42 }
```

此时经固定工厂得到的视图调用 Len 必须返回 42。静态优化可以省去接口装箱，但不能换成原生 b.Len() 的 -1；重复有体实现会诊断。

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

用命名字段 `inner *bytes.Buffer` 并只实现 Reader 的方法，可避免意外实现 `io.WriterTo`。即使 goxide 源码不做开放接口断言，原生 Go 的可选接口分派仍实际存在。

### W04：本包类型的 Go 方法 helper

以下片段假定 `LocalBuffer` 是当前包定义的结构体类型，与该 impl 同包，且生成了 `AsLocalBufferSized`。原类型不带 trait 的业务 marker；As 方法只是便利入口。

```go
func (b *LocalBuffer) AsSized() contracts.Sized {
    return AsLocalBufferSized(b)
}
```

Go 调用 `b.AsSized()`。若类型来自已发布的另一个包，即使它也由 goxide 编写，第三方 impl 包也不能给它追加此方法，应使用 W05。本地定义也不保证能声明方法：例如 `type Ptr[T any] *T` 不满足 Go 接收者规则，同样使用包级工厂或独立门面。

### W05：trait owner 为外部类型提供入口

外部类型可以由 trait 定义包提供有体实现，例如同包的 `Stamp` 与 `impl Stamp for time::Time`，业务体显式调用原生 `Unix()`。该包生成固定 `AsTimeStamp` 工厂，外部 `time.Time` 不增加 As 方法。保存 owner 的 facade 机制如下：

```go
type TimeViews struct { target *time.Time }
func ForTime(value *time.Time) TimeViews { return TimeViews{target: value} }
func (v TimeViews) AsStamp() Stamp { return AsTimeStamp(*v.target) }
```

这里假定 Stamp 仅有传值实例成员，因此 As 在调用时读取 owner 的值。借用视图则向固定工厂传入同一个 owner。便利入口只列本包的合法实现，不收集其他包中的 impl，不重新选择业务体。

### W06：goxide 固定生成函数调用，helper 服务 Go 用户

源级片段假定 b 拥有合法接收权限，`consume` 接受 Sized：

```goxide
var s contracts::Sized = b
consume(b)
n := b.len::<contracts::Sized>()
m := s.len()
```

生成的 Go 函数体片段：

```go
var s contracts.Sized = adapters.AsBufferSized(b)
consume(adapters.AsBufferSized(b))
n := adapters.AsBufferSized(b).Len()
m := s.Len()
_, _ = n, m
```

前端选择 impl，元数据记录工厂包路径与符号，后端据此插入调用。原表达式在每个源使用处只求值一次，已有 s 不重复包装；工厂不依赖 Go convenience helper 是否存在。静态优化不能绕过有体实现或改变对外可观察的视图表示。

## 拒绝与边界

| 编号 | 情形 | 约束 |
| --- | --- | --- |
| W-N1 | 给非本地 `*bytes.Buffer` 生成 As 或 marker 方法 | 不生成；使用 C 包工厂及 C 包 wrapper |
| W-N2 | 第三方 trait 与第三方目标的实现头不满足归属，仅通过透明别名包装名称 | 拒绝；定义自有包装类型并显式实现 |
| W-N3 | 应捕获原生 *T 或引用，却让 wrapper 存 T 副本 | 错误降低，会改变状态与复制成本 |
| W-N4 | 把 `Sized` 视图传给要求原生 `*bytes.Buffer` 的函数 | 不隐式解包；传原始 b 或使用另行明确的转换契约 |
| W-N5 | 为方便生成接口而强制 Default、静态 Self 或任意泛型方法成为实例 wrapper | 不支持这种抹除；按相应静态规则处理 |
| W-N6 | 普通 Go 类型只有 Len，却没有 trait marker | 不能赋给 Sized；helper 的存在不会改变此条件 |
| W-N7 | As/For 便利名称与现有业务方法或同名 trait helper 碰撞 | 按正式名称/导出规则诊断或显式命名；不重命名既有公开入口 |
| W-N8 | 给本地具名指针类型 Ptr 直接生成 As 方法 | 不生成非法 Go 接收者；使用包级工厂或独立门面 |

保存指针身份不等于保存接口动态类型。W01 中动态类型为 `adapters.bufferSized`，不是 `*bytes.Buffer`；Go 的断言、反射及接口比较可观察这个区别。P4 不添加统一非空工厂，不能以 `interface != nil` 证明 inner 非空；enum 的 nil 使用行为继续按其表示规范处理。

wrapper 不抹去借用期限，也不自动实现线程安全。值 wrapper 的方法调用还可能复制 target；有不可复制状态或不同接收权限时须先定义合法载体，不把同一示例套到所有 T。

## 双向互操作

goxide → Go：`var s Sized = b` 或限定调用使用已选 factory/impl；普通 Go 业务接口只看到 wrapper 明确实现的方法。包装前保留的原生指针仍可在合法权限下传给要求 `*bytes.Buffer` 的 Go API。

Go → goxide：导入 adapters，调用 `AsBufferSized` 或本地 Buffer 的 As helper，再使用 contracts.Sized；外部目标通过其合法实现包提供的工厂或 W05 facade 适配。生成模块中已包含所有 wrapper 与 marker，无需给外部包添加源码。下游 goxide 读取 `.d.gox` 恢复显式关系与工厂绑定，不从 Go 方法集猜测 impl。

调用者须能正常导入 impl 包。不能把 C 中声明的实现反向注入 C 的底层依赖，使依赖图出现 C → B → C；真正的 Go 包循环仍须诊断或通过调整源级依赖拆分。

## 验收与未定项

行为验收：W01 在普通 Go 主模块直接导入后返回 2/3，W02 返回 42，W03 展示匿名嵌入错误策略返回小写 a、受控转发版本返回大写 A；W04/W05 分别与对应固定工厂产生相同视图/行为，W06 不借 helper 重新选择 impl。检查 marker、接收者求值次数、原指针共享、多 trait 同名方法和权限拒绝；不能只检查是否可编译。具体运行覆盖由编译器对应测试维护。

工厂及载体由固定的 impl 定义包和实现身份确定，不随下游新增依赖重新选择归属。需要复用同一 Go 动态类型时，调用同一个公共实现包的工厂；重复书写实现不能合并或按构建顺序选择 owner。

P4 的生成身份、权限和便利入口按上述规则；原生 Go 的接口比较和 nil 语义保留，trait marker 不授予 PartialEq/Eq。接口装箱是否逃逸需实测，不承诺额外分配恒为零。跨包加载与发布信息见 [RFC 0001](0001-package-loading.md)。类型关联函数见 [RFC 0020](0020-associated-functions.md)。

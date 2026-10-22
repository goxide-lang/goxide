# RFC 0019：统一 trait wrapper 与包级适配工厂

## 状态与范围

已确认：运行时 trait 视图统一使用独立 wrapper；固定包级工厂作为后端入口，为 Go 用户提供 As/For helper。trait 接口保留导出 marker，由 wrapper 实现。P4 已在新管线完成；Self 接收权限、enum 便利入口和 nil 规则按本篇及[trait 合同](../design/traits.md)。原始类型 marker 注入方案已被替代。

[RFC 0003](0003-declare-impl.md) 已确认移除 `declare impl` 及无体采用的相容合并，当前编译器实现待删。本篇原生采用、验签、合并及 W01 等相关示例仅描述现存实现；普通有体 impl 的 wrapper、marker、固定工厂和 Go 双向调用合同继续有效。`declare func/type` 不在此次删除范围。

适用范围是能形成运行时实例视图的 trait。无 self 的关联操作及泛型证据使用空 carrier，关联类型 witness 只参与 Go 类型推导；P2 派生保持 [RFC 0013](0013-derive.md) 的既有 helper。不要求静态调用构造运行时 wrapper。旧发布反例见 [RFC 0001](0001-package-loading.md)，本篇 W01 给出其具体解法。

## 源级规则

1. 有体 impl 的方法体直接生成在对应 wrapper 上，不向原生类型添加方法。当前仍实现的 `declare impl` 验证原生业务并由 wrapper 转发，但该采用入口已决定移除，见 [RFC 0003](0003-declare-impl.md)。
2. trait 在 A 包、类型在 B 包、impl 在 C 包时，wrapper 和工厂生成在 C，依赖方向为 C → A、B。不默认放在 trait 定义包，也不修改 B 的源码、模块缓存或 GOROOT。
3. 用包级函数适配，例如 `adapters.AsBytesBufferSized(b)`。goxide 的期望类型转换和限定调用直接记录工厂/impl 身份，不要求 `b` 在 Go 中真的具有 As 方法。
4. 每个规范运行时 impl 使用自己的 wrapper；同名不同 trait 方法不会碰撞。正式编码要包括稳定包/类型/trait/参数身份，示例短名不构成最终 ABI。
5. 保存命名字段并只提供选定 trait 的方法。有体 impl 的 Self 为基础 T；全值实例成员捕获 T，全只读借用成员捕获 `&T`，含可写或值/借用混合捕获 `&mut T`，每个方法仍按自身 self 声明检查。declare 先结构化代入 Self，再按精确原生方法集检查，不借自动取址或消去指针层补齐方法。
6. 运行时 trait 的 Go 接口包含业务方法和 marker；wrapper 必须实现二者。跨包实现需要可导出的稳定 marker 名；trait 包私有方法不能由另一个包简单声明同名方法来实现。marker 与 helper 分工不同，helper 不能代替 marker。goxide 精确显式关系仍由元数据决定。
7. 相同视图赋值不重复包装；具体值进入 trait 视图时不重复求值。`any` 或原生 `*T` 参数位置不隐式创建或解开 wrapper。
8. 类型与 impl 同属生成包，且类型允许作为 Go 方法接收者时，可生成 `t.AsSized()` 便利方法；外部类型或不满足接收者要求的本地类型使用 `adapters.ForBuffer(t).AsSized()` 一类门面。两类 helper 只转发既有包级适配函数，不另建实现选择逻辑，不将外部类型的方法写回其定义包。
9. 本包 enum 的值视图在 E 接口声明 As，由各指针变体一行转发固定工厂，保留 `e.AsNamed().Name()`；业务方法仍只在 wrapper。借用 enum 视图使用 `ForE(&e).AsNamed()` 保存真实 owner，不能临时复制 E 再取址。
10. 工厂采用 `As<Type><Trait>`，便利入口采用 `As<Trait>` / `For<Type>`；marker 编码完整 trait 身份。名称冲突诊断，不覆盖用户声明或随机更改已生成身份。
11. `For<Base>(*Base)` 保存真实 owner；值视图从 `*target` 取值后调用工厂，借用视图保持 target。外部纯值适配可直接用工厂，或用 `ForT(&value)`。

## 转译示例

### W01：bytes.Buffer 实现 Sized，普通 Go 直接消费

以下 goxide 是两包的机制片段。Sized 使用 self &Self，目标为原生 bytes::Buffer，代入后按精确 *bytes.Buffer 方法集检查；wrapper 保存同一个指针，不添加统一非空检查。

```goxide
// example.org/demo/contracts
pub trait Sized { func len(self &Self) int }

// example.org/demo/adapters
use contracts;
use bytes;
declare impl contracts::Sized for bytes::Buffer;
pub func view(b &bytes::Buffer) contracts::Sized { return b }
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

type bufferSized struct { inner *bytes.Buffer }

func (v bufferSized) Len() int { return v.inner.Len() }
func (bufferSized) GoxideTraitSized() {}

func AsBytesBufferSized(b *bytes.Buffer) contracts.Sized {
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

整个 dist 包含这些 `.go` 及声明信息，标准库 `bytes` 无需任何新增文件。Go 侧可以使用包级工厂或 W05 的便利入口；原始 `*bytes.Buffer` 不新增 `AsSized()`。goxide 若以后提供方法式适配语法，也只是降低为上述工厂的语法糖。

### W02：有体实现不能被原生方法覆盖

以本包 LocalBuffer 展示有体实现；其原生 Len 返回 -1，trait 实现明确返回 42：

```goxide
type LocalBuffer struct{}
func (b *LocalBuffer) Len() int { return -1 }
impl contracts::Sized for LocalBuffer {
    func len(self &Self) int { return 42 }
}
```

对应 wrapper 方法替换为：

```go
func (v localBufferSized) Len() int { return 42 }
```

此时经固定工厂得到的视图调用 Len 必须返回 42。静态优化可以省去接口装箱，但不能换成原生 b.Len() 的 -1；同键 declare 与有体 impl 共现时诊断。

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

Go 调用 `adapters.ForBuffer(b).AsSized()`；多个转换可先保存 `views := adapters.ForBuffer(b)`。该入口只列本适配包提供的转换，不自动收集程序所有包中的 impl；追加其他 trait 可增加对应 As helper。它持有同一个指针，不复制 Buffer。装入 nil 指针时接口动态值仍为 wrapper，业务方法的 nil 行为来自原生实现。

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
| W-N2 | trait 是 `self Self`，原生只有 `func (*T) M()`，却 declare `Trait for T` 并靠可寻址副本调用 | 验签拒绝；`self &Self` 则按代入后的精确 *T 方法集检查 |
| W-N3 | 应捕获原生 *T 或引用，却让 wrapper 存 T 副本 | 错误降低，会改变状态与复制成本 |
| W-N4 | 把 `Sized` 视图传给要求原生 `*bytes.Buffer` 的函数 | 不隐式解包；传原始 b 或使用另行明确的转换契约 |
| W-N5 | 为方便生成接口而强制 Default、静态 Self 或任意泛型方法成为实例 wrapper | 不支持这种抹除；按相应静态规则处理 |
| W-N6 | 普通 Go 类型只有 Len，却没有 trait marker | 不能赋给 Sized；helper 的存在不会改变此条件 |
| W-N7 | As/For 便利名称与现有业务方法或同名 trait helper 碰撞 | 按正式名称/导出规则诊断或显式命名；不重命名既有公开入口 |
| W-N8 | 给本地具名指针类型 Ptr 直接生成 As 方法 | 不生成非法 Go 接收者；使用包级工厂或独立门面 |

保存指针身份不等于保存接口动态类型。W01 中动态类型为 `adapters.bufferSized`，不是 `*bytes.Buffer`；Go 的断言、反射及接口比较可观察这个区别。P4 不添加统一非空工厂，不能以 `interface != nil` 证明 inner 非空；enum 的 nil 使用行为继续按其表示规范处理。

wrapper 不抹去借用期限，也不自动实现线程安全。值 wrapper 的方法调用还可能复制 target；有不可复制状态或不同接收权限时须先定义合法载体，不把同一示例套到所有 T。

## 双向互操作

goxide → Go：`var s Sized = b` 或限定调用使用已选 factory/impl；普通 Go 业务接口只看到 wrapper 明确实现的方法。原始 b 仍可在合法权限下传给要求 `*bytes.Buffer` 的 Go API。

Go → goxide：导入 adapters，调用 `AsBytesBufferSized` 或 `ForBuffer(b).AsSized()`，再使用 contracts.Sized；本地类型也可通过自身 As helper 进入同一工厂。生成模块中已包含所有 wrapper 与 marker，无需给外部包添加源码。下游 goxide 读取 `.d.gox` 恢复显式关系与工厂绑定，不从 Go 方法集猜测 impl。

调用者须能正常导入 impl 包。不能把 C 中声明的实现反向注入 C 的底层依赖，使依赖图出现 C → B → C；真正的 Go 包循环仍须诊断或通过调整源级依赖拆分。

## 验收与未定项

未来验收：W01 在普通 Go 主模块直接导入后返回 2/3，W02 返回 42，W03 展示匿名嵌入错误策略返回小写 a、受控转发版本返回大写 A；W04/W05 与直接工厂产生相同视图/行为，W06 不借 helper 重新选择 impl。检查 marker、接收者求值次数、原指针共享、多 trait 同名方法和权限拒绝；不能只检查是否可编译。当前没有执行本篇 Go 编译或 goxide 端到端验证。

相容 declare 的合并与 Go 载体身份分开：C1/C2 可以声明同一逻辑采用关系，但各自的本地 wrapper 是不同 Go 类型。工厂及载体应由固定的声明包和实现身份确定，不随下游新增依赖重新选择归属。若需要跨包相同动态类型，应指定公共适配包 C0，其他包复用其工厂；不以构建顺序选择一个临时 owner。

P4 的生成身份、权限和便利入口按上述规则；原生 Go 的接口比较和 nil 语义保留，trait marker 不授予 PartialEq/Eq。接口装箱是否逃逸需实测，不承诺额外分配恒为零。跨包 Goxide 加载、相容声明的公共适配包和发布信息恢复由 P5 接续。类型关联函数见 [RFC 0020](0020-associated-functions.md)。

# Go 原生类型的显式适配

普通 Go 函数、方法、函数值和 interface 按真实 Go 类型使用。Goxide trait 关系由普通有体 `impl` 建立，适配生成固定 wrapper、marker 和工厂；外部类型不追加方法或 carrier 类型锚。`declare impl` 及其相容合并已移除，见 [RFC 0003](../rfcs/0003-declare-impl.md)。`declare func/type` 保留既有解析与语义支持，具体边界见[原生 API](foreign-api.md)。

状态编号见[决策登记](decisions.md)。诊断边界按 D19/D20：基础类型与读写权限照常检查，别名/逃逸风险分析不属 MVP，不以缺少外部效果摘要普遍拒绝普通 Go 调用。

## 1. 有体 impl 的归属与业务来源

trait 定义包可以为外部 Go 类型提供有体实现：

```goxide
use time;

pub trait Stamp { func stamp(self Self) int64 }
impl Stamp for time::Time {
    func stamp(self Self) int64 { return self.Unix() }
}
```

当 trait 和目标类型都来自其他包、完整实现头不满足有体 impl 归属规则时，定义自己的包装类型：

```goxide
package adapters
use bytes;
use byteio;

type Buffer struct { raw *bytes::Buffer }
func Wrap(raw *bytes::Buffer) Buffer { return Buffer{raw: raw} }
impl byteio::Reader for Buffer {
    func read(self &mut Self, p []byte) (int, error) {
        return self.raw.Read(p)
    }
}
```

这里假定 `byteio::Reader` 声明相同的 `read(self &mut Self, p []byte)` 方法。`Buffer` 是 adapters 的独立类型；透明别名不改变原类型身份和归属。wrapper、marker 和工厂由 impl 所在包生成，原生包、模块缓存与标准库均无需修改。

有体 impl 完整提供必需方法。即使原生类型已有同名同签名方法，也执行所写方法体；复用原生行为时显式调用它。带游标、缓存或可变状态的原生对象保存在字段中并复用，不在每次业务调用中重新创建。

## 2. wrapper、marker 与固定工厂

能形成运行时视图的 trait 生成业务方法和 marker。以上适配的 Go 机制如下，短名用于说明布局：

```go
// byteio 包
type Reader interface {
    Read([]byte) (int, error)
    GoxideTraitByteReader()
}

// adapters 包
type Buffer struct { raw *bytes.Buffer }
func Wrap(raw *bytes.Buffer) Buffer { return Buffer{raw: raw} }

type bufferReader struct { target *Buffer }
func (v bufferReader) GoxideTraitByteReader() {}
func (v bufferReader) Read(p []byte) (int, error) {
    return v.target.raw.Read(p)
}
func AsBufferReader(b *Buffer) byteio.Reader {
    return bufferReader{target: b}
}
```

wrapper 使用命名 target 字段，只生成所选 trait 的业务方法和 marker。匿名嵌入 `*bytes.Buffer` 会提升 `WriteTo` 等额外方法，使 `io.Copy` 可能绕过所选 `Read`，因此不能代替显式业务体。

保存的 raw 仍是同一个 `*bytes.Buffer`，修改可被其他合法视图观察到；装入接口后的动态类型则是 adapters 的 wrapper。反射、断言和比较按这个 Go 类型执行。工厂不添加统一非空检查，业务体及其原生调用保留相应的 nil 行为，enum 使用边界仍按其表示规范检查。

marker 使用编码完整 trait 身份的导出名称，使跨包 wrapper 可以实现接口。Go 手写或嵌入获得 marker 不会建立新的 Goxide impl，源级关系、关联类型和权限以检查结果为准。

## 3. 便利入口只转发固定工厂

本地、可作为 Go receiver 的类型可以生成 `b.AsReader()`，它只调用 `AsBufferReader(b)`。外部类型的有体 impl 只能由满足归属规则的包声明，生成包级工厂及自有 facade；不能给外部类型追加 As 方法。

`For<Base>(*Base)` 保存真实 owner。值视图从 `*target` 取值后调用工厂，借用视图将同一 target 传给工厂。外部纯值类型可以直接使用工厂(value)，或使用 `ForT(&value).AsTrait()`；不能复制临时值再取址来伪造原存储。

enum 值视图在 E 接口声明 As，由各指针变体转发固定工厂；借用 enum 使用 `ForE(&e)` 保存 `*E` owner。具名指针类型等不能作为 Go receiver 的目标也使用包级工厂或 facade。便利入口不建立第二套实现选择或业务逻辑。

## 4. 接收者、权限和方法值

`Self` 绑定完整目标类型；`self Self`、`self &Self`、`self &mut Self` 分别表示传值、只读引用和可写引用。指针目标与引用的层次照常保留，不能因 Go 表示相近而消去源级权限。

普通 Go 方法、方法表达式和保存的方法值继续按真实方法集解析。原生接口及泛型方法约束使用 Go 结构式规则；方法签名恰好相符不会自动实现 Goxide trait。

有体 trait 的直接调用、限定调用和保存的方法值保持所选业务体。绑定接收者只求值一次；值接收者保存副本，借用接收者保存真实对象或 owner 槽位。生成的绑定与泛型证据不得在实际业务体前插入额外转发帧，影响 `defer` / `recover`。

trait 视图不能为满足原生 `*T` 参数而隐式 unwrap。需要调用原生 API 时，使用保留的原始变量或显式、受检查的桥接。普通 Go 调用仍接受基础权限检查，`declare func` 的可信契约也不会扩大其他调用的权限。

## 5. 构建、发布与稳定身份

编译器加载参与构建的 Go 类型信息与 Goxide 源码，检查有体 impl 的归属、签名、关联类型和唯一性，再在 impl 包生成普通 Go。依赖方向由真实 import 决定，不向原生类型定义包反向注入实现。透明别名按规范类型归一；同一实现重复书写不会相容合并。

每个实现的 wrapper 与工厂由定义包及规范实现身份固定。新增 impl 不改变已有工厂的归属或业务体，不能按临时导入顺序选择实现。名称冲突在源级诊断。外部原生类型不注入 `GoxideInternalTypeCarrierForSelf`；泛型证据和普通 Go 入口见[关联类型](associated-types.md)与 [RFC 0031](../rfcs/0031-generic-protocol-impls.md)。

生成代码随普通 Go dist 交付，Go 下游直接导入生成包。Goxide 的源码/声明加载和发布信息见 [RFC 0001](../rfcs/0001-package-loading.md)；更换依赖、replace、平台或 tags 后按实际构建重新检查。Go 包循环、私有访问和类型不相容仍按各自规则诊断。

后续 LSP 复用编译器的成员选择与来源：原生调用导航到 Go 方法，有体 trait 调用导航到该 impl 的业务体，工厂和 helper 关联同一个实现。

## 6. 行为验收

- trait owner 对外部 Go 类型的有体 impl，以及使用自有包装类型的跨包 impl，均生成可直接 build/run 的 Go。
- 普通函数、方法、方法表达式、方法值、interface 和泛型 Go 约束继续工作；真实权限和签名错误仍被拒绝。
- 固定工厂与 As/For helper 选择一致，wrapper 不提升额外方法，多个合法视图观察同一个原生状态。
- 重复实现与非法归属不合并；私有访问、关联类型、接收者身份和 `defer` / `recover` 保持原行为。

裸指针、AsPtr、unsafe 效果和回调桥接见[原生 API 与裸指针](foreign-api.md)。

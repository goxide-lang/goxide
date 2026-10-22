# RFC 0003：移除 declare impl，保留普通 Go 互操作

## 状态与范围

**已完整移除 `declare impl`。** 原生采用、相容无体声明合并及其生成路径已删除；普通 Go 双向调用和有体适配继续保留。实施与验收状态见[状态表](../mvp-gap-audit.md)。

删除的是“把现有 Go 方法显式登记成 Goxide trait 实现”的语言入口，包含 trait/type 所有者及第三方声明。普通 Go 双向调用、Go interface、`declare func`、`declare type` 与普通有体 impl 保留，不删除 declare 关键字或其他外部声明能力。

## 源级规则

1. Goxide trait 关系由普通有体 impl 建立，必须提供必需业务成员；Go 方法名、结构相容、marker 或同名 helper 不自动登记关系。
2. 有体 impl 沿既有及 [RFC 0032](0032-from-into-conversions.md#3-实现归属与跨包可见性) 拟扩展的 owner 规则检查。trait 定义包仍可为外部类型写完整 impl；删除采用入口不顺带收紧普通归属规则。
3. 完整实现头不满足普通有体 impl 的 owner 规则时，定义本地包装类型再实现 trait，或使用普通函数。拥有 trait 输入的包仍可按完整头规则实现协议，例如本地来源转换到外部目标。透明 alias 不创建 owner；包装字段可保存原 Go 指针，显式方法体负责委托。
4. 运行时 trait 的 wrapper、marker、固定工厂、As/For 与 Go 成员 sugar 保留，只服务已选有体实现。不给外部类型添加方法，不合并不同业务实现，不通过包路径或更具体优先选择。
5. Goxide 可直接调用原生 Go 函数、方法，并使用 io.Reader 等原生接口；Go 可直接调用生成的公开函数/方法及已有协议入口。删除采用不改变普通参数、返回、nil、指针身份或 D19/D20 权限规则。
6. 泛型/跨包互斥统一见 [RFC 0031 第 2 节](0031-generic-protocol-impls.md#2-通用互斥与实现选择设计提案)。不再有 MayDeclare、采用声明合并或多采用工厂来源选择；原生 Go 约束及其未来方法变化仍参与普通条件 impl 证明。

## 显式适配示例

protocols::Reader 为外包 Goxide trait，要求 `read(self &mut Self, p []byte) (int,error)`。业务包拥有自己的 BufferReader，保存同一个原生对象：

```goxide
use bytes;
use protocols;

pub struct BufferReader { raw *bytes::Buffer }
impl protocols::Reader for BufferReader {
    func read(self &mut Self, p []byte) (int, error) {
        return self.raw.Read(p)
    }
}
pub func wrap(b *bytes::Buffer) BufferReader {
    return BufferReader{raw: b}
}
```

这是真实用户类型和普通业务体；宏也可生成相同声明，不需要采用权限或新的 hook。若只需要原生 Go 接口，直接写普通函数即可：

```goxide
use io;

pub func read(r io::Reader, p []byte) (int, error) {
    return r.Read(p)
}
```

Go 消费者调用生成的 Read，Goxide 消费者调用原生 Read；两者都不依赖 `declare impl`。需要 Goxide trait 的调用使用已明确实现的 BufferReader；仅有原生同名方法不再提供该资格。包装的额外调用层是源码明确写出的业务层，编译器按普通方法体和方法值规则处理，不能冒充直接原生方法绑定。

## 实施与验收

- 删除 parser/AST 中 declare 包装 ImplDecl 的专用分支、resolve/check 的原生采用与合并、lower 的采用转发和来源选择；保留其他 DeclareDecl 形式。不留旧入口的兼容回退或替代采用属性。
- 删除采用专属夹具；源文件及声明宏、WASI function-like/attribute/derive 输出中的 `declare impl` 均按普通语法拒绝。`declare func/type` 按现有解析/语义支持范围做对照，不借删除工作扩展其实现范围。
- 以普通有体 impl 验收跨包 trait 视图、marker、工厂、权限与名称冲突；重复有体 impl 不因文本或目标原生方法相同而合并。
- 本地包装类型、trait owner 对外部类型的普通实现及非法第三方实现均有正反例；原始 Go 类型的方法集不被修改，alias 不绕过 owner。
- 真实 `.gox → .go` 后，以独立 Go 消费模块 build/run/test：直接函数/方法/interface 调用、原指针状态、nil/typed-nil、可见性、方法值与调用帧；同输入两次生成字节一致。
- 通用互斥仍覆盖非 ground 条件、未来有体实现和 Go 原生约束。删除采用不能变成只接受具体类型或忽略外包新增方法的理由。

删除边界由 `declaration_boundary_test.go` 覆盖源码、声明宏及真实 WASI 三类宏；`go_interop_test.go` 覆盖四个独立模块的普通互调用、有体适配和 Go 消费者。通用互斥与 From/Into 继续按 RFC 0031/0032 推进。

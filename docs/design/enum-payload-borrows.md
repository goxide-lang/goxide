# 原位 enum payload 与引用合同

本合同记录 2026-10-09 的 pre-MVP 实施范围，对应 [#14](https://github.com/goxide-lang/goxide/issues/14)。用户要求先让 match enum 可用，本轮不增加生命周期、逃逸、借用冲突或跨变体失效检查；后续工作单独跟踪于 [#17](https://github.com/goxide-lang/goxide/issues/17)。已有的类型、读写权限和擦除检查保留。

## 源码入口

| 形式 | 行为 |
| --- | --- |
| `&T` / `&value` | 只读引用类型 / 在引用上下文中从位置取只读引用 |
| `&mut T` / `&mut value` | 可写引用类型 / 从可写位置取引用，不承诺独占 |
| `match e { E::V(p) => ... }` | 复制匹配入口快照中的 payload |
| `E::V(ref p)` | 绑定原 payload 的只读引用 |
| `E::V(ref mut p)` | 绑定原 payload 的可写引用 |
| `match &e { E::V(p) => ... }` | 普通 payload 绑定默认只读引用 |
| `match &mut e { E::V(p) => ... }` | 普通 payload 绑定默认可写引用 |

`&p` 是解构已有引用的模式方向，不是 `ref p` 的另一种拼写。本轮只实现表中的基本模式；复杂嵌套引用模式须明确诊断，不套用完整 Rust match ergonomics。已有引用变量作为 scrutinee 时需要显式 `*owner`，例如 `match &mut *owner`。普通 Go 上下文中的 `p := &x` 仍可创建原生指针；`match &e` 明确选择只读引用上下文。

```goxide
enum Counter { Count(int), Empty, }

func edit(counter &mut Counter) &mut int {
    match *counter {
        Counter::Count(ref mut value) => { return value },
        Counter::Empty => { panic("empty counter") },
    }
    panic("exhaustive match")
}

func increment() int {
    counter := Counter::Count(1)
    match &mut counter {
        Counter::Count(value) => { *value += 1 },
        Counter::Empty => {},
    }
    return *edit(&mut counter)
}
```

旧的 `mut& T` 引用拼写已移除；合法标识符 `mut` 参与的二元 `mut & x` 和 `mut&x` 仍是相同的按位运算，不由空白或名称解析偶然决定。

## 存储、复制和 guard

用户 enum 和共享 Option/Result 均使用 tag 加各变体独立的、真实类型的内联槽。投影取得该槽的 Go 指针；不对类型断言副本取址，不在分支退出时写回，也不加入 generation wrapper。

普通赋值浅复制整个 enum；复制前后已有引用仍指向原 enum。payload 内的 slice、map 和指针仍遵循 Go 浅复制语义，不承诺深复制。

scrutinee 与索引、接收者等位置组成部分只求值一次。分派及值绑定使用入口快照；原位投影在执行 guard 前取得，后续同变体分支复用投影。guard 失败不撤销副作用。降低保留外围 return、break、continue、goto 与 defer 的控制流，不复制分支体。

通过其他别名整体替换 enum 不被拒绝。旧引用指向固定槽，因此有下列直接的 Go 行为：

| 对持有 A payload 引用的原 enum 操作 | 旧引用观察 |
| --- | --- |
| 用新的 A 整体赋值 | 读取新的 A payload |
| 从 A 整体替换成 B | A 槽被清零；旧引用仍可读写非活动 A 槽 |
| 从 B 再整体替换回 A | 再次读取新的 A payload |

这些是当前存储行为，不是失效保护。活动变体的协议验证忽略非活动槽。原生 Go 对可比较 struct 的 `==` 会包含非活动槽；goxide 的显式 equality 协议只比较经验证的活动变体及 payload。

## 生成 Go 与模块边界

std/sum 的 `GoxideSumABI` 为 3；enum metadata 为 9。共享库只提供普通 Go 存储与操作，不导入 compiler、serde 或用户包。serde core 继续拥有 Serialize/Deserialize，JSON 只负责格式。

- `sum.NewSome`、`sum.None`、`sum.NewOk`、`sum.NewErr` 构造值。
- `sum.TagOption` / `sum.TagResult` 读取 tag；`ValidateOption` / `ValidateResult` 独立验证合法 tag。
- `sum.ValueSome` / `ValueOk` / `ValueErr` 返回活动 payload 的副本。
- `sum.ProjectSome` / `ProjectOk` / `ProjectErr` 接收 enum 指针并返回真实槽指针，检查当前 tag 和 nil owner。
- `pub enum` 的生成包导出对应的构造、Validate、Tag、Value 和 Project helper 及必要的 payload 类型别名；私有 enum 只生成内部 helper。metadata 绑定真实 owner、类型身份和签名。

Tag 读取不承担验证，serde 因而可以按现有协议返回错误；match、传播和其他派生在读取 payload 前保持各自的验证次序。Option 零值是 None；Result 和用户 enum 的零值仍非法，使用点执行既有验证。None 与 Some(nil)、Some(empty) 保持不同。

投影的源级引用权限通过编译器已验证的函数对象和 enum 身份登记；同名普通函数、变量或伪造 owner 不取得这种资格。只读引用不能提升为可写或擦除权限。原生 Go 直接消费真实指针时遵循 Go 类型规则，不执行 goxide 的只读检查。

旧 `sum.Some/Ok/Err` case 类型、生成 `GoxideCase*` 及 `GoxideOptionSome/GoxideResultOk/GoxideResultErr` case API 已移除。旧 interface、marker 和 schema 产物必须全部重新生成，不提供兼容转发层。正常的类型别名、公开引用类型命名和保护类型绑定的卫生性别名继续保留。

## 验证入口与范围

[可运行示例](../../examples/enum_borrows/main.gox)演示返回引用、普通复制和默认绑定。在仓库根构建 CLI 后运行 `goxide run --go /absolute/go examples/enum_borrows/main.gox`，输出为 `5 1` 和 `5`。该示例已用 Go 1.27.1 执行；它不替代下列跨模块验收。

[实际 CLI 验收](../../tests/acceptance/enum_payload_borrow_test.go)覆盖原位修改、复制、保存/返回/捕获、求值次数、guard 与整体替换、共享 sum、跨模块协议恢复和普通 Go 消费，并对生成模块执行 race/vet。测试文件列出验收目标；某个提交是否通过以该提交的实际测试记录和 CI 为准。

生命周期、独占、跨变体引用失效保护、完整 Rust 模式规则、泛型用户 enum 和完整 trait 不由本轮完成。未新增安全分析不取消已有权限错误，也不改变普通 Go 互操作。

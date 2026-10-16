# 原位 enum payload 与引用合同

本合同规定新前端 P3 的引用、读写权限与原位 payload 投影。用户 enum 的存储遵循[密封接口表示](enum-representation.md)；复制共享变体对象、整体替换后旧投影仍指向旧对象的行为已于 2026-10-13 确认。生命周期、逃逸、独占和共享别名风险分析按 [D19/D20](decisions.md) 独立推进；当前检查源级类型与路径读写权限。

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

用户 enum 是密封接口，动态值为变体结构体指针。普通赋值复制接口值，两个值共享同一个变体对象。投影直接取得该对象字段的地址，例如 `&variant.Value`；字段写入对持有该对象的其他 enum 值可见。

整体赋值只替换接收赋值的 enum 变量。已有 payload 引用继续指向旧变体对象，Go GC 保持该对象存活；新值使用同一变体或另一变体均遵循此规则。

Option/Result 在 P6 前沿用 `std/sum` 的 tag 与独立内联槽。普通赋值复制槽位，投影取得原 owner 内槽位的地址。两类存储的区别如下：

| 操作 | 用户 enum | Option/Result |
| --- | --- | --- |
| 复制 enum 后经投影写入 | 共享变体对象，复制值可见 | 原 owner 槽位更新，复制值的槽位独立 |
| 原变量整体换成同一变体的新值 | 旧投影仍访问旧对象 | 旧投影读取同一槽位中的新 payload |
| 原变量整体换成另一变体 | 旧投影仍访问旧对象 | 旧槽被清零，旧投影仍访问该非活动槽 |
| 再整体换回原变体 | 旧投影仍访问旧对象 | 旧投影读取原槽位中的新 payload |

scrutinee 与索引、接收者等位置组成部分只求值一次。分派使用入口 enum 值；普通 payload 绑定及常量模式使用首个 guard 前保存的 payload 值。引用绑定指向真实字段或槽位，在 guard 前取得，同变体后续 arm 复用该位置。guard 失败不撤销副作用。降低保留外围 return、break、continue、goto、defer 与 `?` 的控制流，不复制分支体。

payload 值的复制遵循 Go 浅复制规则：标量和 struct/array 的值部分独立，slice、map、指针及用户 enum 可共享对象。只读路径上的共享部分继续保留读权限；独立值部分可以修改。引用可复制、保存、返回和捕获，不能从只读路径取得可写引用，也不能通过普通指针、接口或容器擦除只读资格。局部变量或独立值字段替换成新存储后按新值检查，旧副本保留原资格。

取引用要求真实可寻址位置；map 元素与不可寻址临时值不能通过隐式副本实现借用。引用重绑定只改变本地绑定，已有引用仍指向原存储。源级 `&T` 与 `&mut T` 的权限检查完成后均生成普通 Go `*T`。

## 生成 Go 与模块边界

用户 enum 的投影由变体字段直接表达，不生成 Tag、Value、Project helper 或 enum metadata。同一次源码构建中的引用签名按解析后的函数、类型与包身份传递权限。普通同名声明不能冒充已检查的引用入口。

Option/Result 使用当前 `std/sum` 的既有入口：

- `sum.NewSome`、`sum.None`、`sum.NewOk`、`sum.NewErr` 构造值。
- `sum.TagOption` / `sum.TagResult` 读取 tag；`ValidateOption` / `ValidateResult` 独立验证合法 tag。
- `sum.ValueSome` / `ValueOk` / `ValueErr` 返回活动 payload 的副本。
- `sum.ProjectSome` / `ProjectOk` / `ProjectErr` 接收 enum 指针并返回真实槽指针，检查当前 tag 和 nil owner。

Tag 读取不承担验证，serde 因而可以按现有协议返回错误；match、传播和其他派生在读取 payload 前保持各自的验证次序。Option 零值是 None；Result 和用户 enum 的零值仍非法，使用点执行既有验证。None 与 Some(nil)、Some(empty) 保持不同。

按 [D31](decisions.md)，可写引用可自然传给元素类型匹配的 Go 指针参数；只读引用不能借此升级写权限。原生 Go 直接消费生成函数时遵循 Go 类型规则，不执行 goxide 的只读检查。跨构建的增强信息恢复由项目工具链阶段衔接。

## 验证入口与范围

[可运行示例](../../examples/enum_borrows/main.gox)演示返回引用、共享变体对象和默认引用绑定。新管线生成 Go 后运行，输出为 `5 5` 和 `5`。

[新管线验收](../../internal/lower/reference_e2e_test.go)验证原位修改、引用复制与保存、返回与捕获、存储身份、单次求值、guard、整体替换和生成结果确定性，并真实构建运行生成的 Go。读写权限反例在生成 Go 前诊断。

泛型用户 enum 的投影使用实例化后的真实字段类型。完整 Rust 模式规则与 trait 按各自阶段推进。

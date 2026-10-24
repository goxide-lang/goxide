# enum payload 与引用合同

用户 enum 使用[密封接口与指针变体](enum-representation.md)。payload 槽位不可变；复制 enum 共享变体对象，整体重绑定只替换当前变量。生命周期、逃逸、独占和共享别名风险分析按 [D19/D20](decisions.md) 独立推进；当前检查源级类型与路径读写权限。

## 源码入口

| 形式 | 行为 |
| --- | --- |
| `&T` / `&value` | 只读引用类型 / 从位置取只读引用 |
| `&mut T` / `&mut value` | 可写引用类型 / 从可写位置取引用，不承诺独占；可以重绑定 enum 变量 |
| `match e { E::V(p) => ... }` | 复制匹配入口快照中的 payload；局部副本可重新赋值 |
| `E::V(ref p)` | 绑定原 payload 的只读引用 |
| `match &e { E::V(p) => ... }` | 普通 payload 绑定默认只读引用 |
| `E::V(ref mut p)` / `match &mut e` | 拒绝：不能取得 payload 槽位的可写投影 |

不可变槽位规则也适用于 Option/Result、嵌套模式和泛型 enum。改变 payload 时构造新 enum 并赋给变量；局部变量 `e` 本身仍可重绑定。

```goxide
enum Counter { Count(int), Empty, }
enum Items { Values([]int), }

func increment(counter Counter) Counter {
    return match counter {
        Counter::Count(value) => Counter::Count(value + 1),
        Counter::Empty => Counter::Count(1),
    }
}

func update() {
    counter := Counter::Count(1)
    previous := counter
    counter = increment(counter)
    match &previous { Counter::Count(value) => { _ = *value }, Counter::Empty => {} }

    items := Items::Values([]int{3})
    match items { Items::Values(values) => { values[0] = 9 } }
}
```

上例修改 slice 的元素，未替换 enum 保存的 slice 槽。payload 中的 slice、map、指针和内部可变容器按其自身权限使用；从只读路径取得的共享数据继续受只读资格约束。struct/array 的值副本可修改，普通指针、接口或容器不能擦除只读资格。

`&p` 是解构已有引用的模式方向，不是 `ref p` 的另一种拼写。复杂嵌套引用模式仍须明确诊断。普通 Go 上下文中的 `p := &x` 可创建原生指针；`match &e` 明确选择只读引用上下文。旧 `mut& T` 拼写已移除，标识符 `mut` 参与的 `mut & x` 仍是普通按位运算。

## 存储、复制和 guard

用户 enum 的动态值为变体结构体指针。普通赋值复制接口值，共享同一个变体对象；只读投影直接取得该对象字段的地址。原变量整体换成同一或另一变体时，已有投影继续指向旧对象，Go GC 保持该对象存活。

Option/Result 目前保留 `std/sum` 的 tag 与独立内联槽，表示统一安排在 [MVP-6](../pre-mvp-plan.md#后续阶段mvp)。赋值复制槽位；只读投影指向原 owner 内的槽。owner 换值时，旧投影仍指向该槽，可能读到新 payload 或被清零的非活动槽。生成代码使用现有 `Value` / `Project` API；这些 Go API 的指针形状不授予 Goxide 可写 payload 权限。

scrutinee、索引和接收者等位置组成部分只求值一次。分派使用入口 enum 值；普通绑定及常量模式使用首个 guard 前保存的 payload 值。只读引用在 guard 前取得，同变体后续 arm 复用该位置。guard 失败不撤销副作用；降低保留外围 return、break、continue、goto、defer 与 `?` 的控制流。

取引用要求真实可寻址位置；map 元素与不可寻址临时值不能借隐式副本实现引用。引用可复制、保存、返回和捕获；重新绑定引用变量不改变已有引用。源级 `&T` / `&mut T` 检查后均使用普通 Go `*T`。

## 固有成员与 owner

enum 固有实例成员只接受 `self Self` 或 `self &Self`，在接口和各指针变体上直接生成方法。两种成员的方法值均捕获绑定时的变体对象；以后重绑定 enum 变量不会改换已保存方法值的对象。`self &mut Self` 固有成员拒绝，更新操作返回新 enum，由调用方赋值。

普通函数或 trait wrapper 仍可接收真实 `&mut E` 并重绑定 owner；这不允许写 payload 槽。trait 借用视图保存真实 owner，其行为与固有方法值分别见 [trait 合同](traits.md#4-接收者共享状态与-self)和 [RFC 0020](../rfcs/0020-associated-functions.md)。

## Go 与模块边界

同一次源码构建按真实函数、类型及包身份传递权限；不产生 enum metadata。按 [D31](decisions.md)，可写引用可传给元素类型匹配的 Go 指针参数，只读引用不能借此升级写权限。原生 Go 直接消费生成代码时遵循 Go 类型规则。

match、传播与库协议在读取 enum 时保留各自的有效性检查次序。Option 零值为 None，Result 和用户 enum 零值非法；None、Some(nil) 与 Some(empty) 保持区别。

## 验证入口

[借用示例](../../examples/enum_borrows/main.gox)输出 `2 1`、`2`、`9`，展示变量重绑定、只读投影与容器共享。[不可变槽位检查](../../internal/check/enum_immutable_test.go)覆盖显式/default/nested/Option/Result 反例及容器和 owner 正例；[引用端到端](../../internal/lower/reference_e2e_test.go)覆盖存储身份、单次求值、guard 与确定性；[固有方法端到端](../../internal/lower/inherent_enum_test.go)覆盖直接 Go 调用、方法值、nil 和调用帧。

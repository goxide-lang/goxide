# Enum 显式比较：有界实施提案

状态（2026-10-03）：用户已选**资格编译期判断，不能把含不可比载荷的 enum 比较留给 Go interface 运行时 panic**；进一步确认显式派生宏自动实现 PartialEq/Eq，并承接 enum 使用 `equals` 的方向。下面具体签名、容器/非法值边界仍是提案；尚未实现，不以本文授权全 trait 或修改普通 Go 类型的 `==`。

## 最小协议

- PartialEq 提供一次布尔比较 `equals`。Eq 依赖已有 PartialEq，只声明自反、对称、传递的等价关系，不生成第二套方法。
- 建议 `#[derive(PartialEq, Eq)]`。单独 Eq 要求已存在 PartialEq；重复手写/派生诊断。派生 PartialEq 比较变体及活跃载荷，派生 Eq 检查所有载荷的 Eq 资格。
- 手写拟沿用具体 `impl PartialEq for E` 和 `impl Eq for E {}`，不需一般 trait 求解器。实例首参 self、无 static 的既定约定继续适用；self/other 的精确权限签名待确认。手写 Eq 是作者契约，编译器不能证明任意函数满足等价关系。
- hgo 的 `a.equals(b)` 可静态降低到已登记的具体 helper，建议 Go 导出 `EqualsE(a,b E) bool`。当前 E 是 interface，不能直接给它声明 Go receiver 方法；不按任意同名 Go 方法猜协议，也不因此重写 enum ABI。

## Go 互操作资格

Go `comparable` 是 `==`/`!=` 的类型参数约束，不是业务 Eq 协议：float/complex 的 NaN 破坏自反性；interface 值还可能携带不可比动态值。不能提供任意 comparable blanket Eq。

建议为 bool、整数、string 内建明确 Eq 映射；struct/array 逐字段/元素递归判断。float 与 complex 类型按普通 Go 数值相等只支持 PartialEq，不能派生 Eq。指针/chan 的身份相等与内容相等不同，不在首片自动解释为深比较。原生具名类型若希望不同于 Go `==` 的语义，必须显式适配。

Go 1.27.1 的本地 `go doc cmp.Compare` 确認它是 Ordered 泛型函数，并非用户通用 Compare 接口；其浮点规则把 NaN 与 NaN 视为相等。可以显式包装为另一种浮点比较，但不能悄悄替换默认 PartialEq。`go doc time.Time.Equal` 则说明它按时间瞬间比较，时区不同仍可能 Equal；不能根据方法名或 Go 可比较性推断这些业务语义。

## 容器、原生 map 与 Hash

slice/map 可由显式派生定义内容相等，资格递归依赖载荷协议，不等同于 Go `==` 资格。nil 与空容器是否相等尚需确认。若 map 内容比较以原生键查找实现，键还须满足 Go 比较与选定语义一致且自反；NaN 键不能被隐藏忽略。循环容器、递归指针和动态 interface 的通用深比较不加入首片。

Eq 不使 enum 自动成为 Go map key。原生 map 使用自身 `==`/hash，不调用 equals；必须单独核验实际 Go 可比较性、无动态 panic 风险与语义一致性。未来若迁 tag struct，非活跃字段也参与原生比较，因此不能仅凭 tag struct 可比较就声称与活跃载荷 equals 一致。

本阶段无需 Hash，也无需自定义 HashMap。未来自定义哈希容器才需要明确“equals 相等则 hash 相同”的契约；PartialEq/Eq 不依赖 Hash。

## 非法值与跨模块

建议两个实参先按既有表达式求值规则完成，再在 equals helper 内从左至右验证两侧并比较；普通 enum/Result 的 nil、typed-nil case 与伪造动态值沿现 validator 规则 panic，Option.None 是合法值。是否由公共 wrapper 在手写实现之前保证合法输入、两侧求值/验证顺序及 enum 与 nil 的源级比较例外需写明，不能从“不隐式相等”自行推断。

跨模块沿现有摘要绑定记录具体目标身份、实现 helper、derive/manual 来源与 Eq 标记；检查真实 Go 签名，不从 Go comparable 或函数名猜测协议。普通 Go 可显式调用 helper，但 hgo 的静态比较禁令不会改写任意原生 Go 源码。

## 建议实施顺序与待确认点

第一片先确定上述具体入口，支持有限 scalar/struct/array 载荷的显式 PartialEq/Eq 派生、真实 NaN 反例和跨模块 helper；不引入全 trait、隐式适配或 HashMap。以下分片顺序不是用户批准延期。第二片再做已确认容器语义的 slice/map 递归派生与 manual 入口。每片都需真实 hgo→Go 执行及独立审查。

仍需确认：self/other 的权限签名及 Go helper 命名；enum 对 nil 比较是否例外；nil/空容器语义与 map 键契约；手写实现之前的验证保证。当前讨论方向为 enum 统一走显式 equals；落地静态禁令前应在最终契约中写清 `==`/`!=`、隐含比较 switch 和 map key 的边界，不能扩大成普通 Go 类型全局禁令。

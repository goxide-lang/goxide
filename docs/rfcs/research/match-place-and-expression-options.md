# Match 原位借用与表达式展开的实现选择

状态：D29–D31 的用户需求已确认；本文的具体 Go 布局/接口为待定实现方案，不宣布 ABI 冻结。基线 `6380b6788333485a2fdd24e6558da0aba264df68`。两项普通权限/私有 bool bug 已先独立修复；不借本研究缩减跨 module、复杂表达式或自然可写指针互操作要求。

## 已确认目标与实际代码

用户要求首版支持 `match mut& enum_value { Var1(mut& inner) => *inner += 1 }` 的原位写入。按现有具名变体和语句 arm 形式举例，目标为：

```hgo
enum Counter { Empty, Value(int) }
e := Counter::Value(1)
copy := e
match mut& e {
    Counter::Value(mut& inner) => { *inner += 1 },
    Counter::Empty => {},
}
// e.Value 应为 2；copy.Value 仍为 1。
```

该语法尚未实现。用户期望的简写变体名与无块赋值 arm 不由示例自行冻结；首片可以沿已有类型限定名/语句块解析，不扩展其他 Rust 模式。

当前 user enum 为带私有 marker 的 interface，各变体是值型 struct。match 保存 scrutinee 一次，校验后依次 type assert、复制 Value 并执行 arm；表达式 match 先声明结果临时量，最后用于原局部声明/单标识符赋值。带问号的 arm 在原函数生成失败 return。复杂目标和嵌入表达式在 matchexpr 的上下文识别层直接拒绝，还没有通用 expression prelude/结果组合计划；这是工程欠账，不是语言禁令。

## 原位 payload 的最小可行表示

interface 中的值型 case 不可直接取原 payload 地址。取断言副本地址不符合原位；统一改成指针 case 会使普通 enum 赋值隐藏共享，也不符合既定值复制。对当前有限非递归 payload，直接可行的 Go 表示是私有 tag 加每个变体独立的具体值字段：

```go
type Counter struct {
    tag uint8
    value int
}
func NewCounterValue(v int) Counter { return Counter{tag: 2, value: v} }
func counterValueRef(e *Counter) *int {
    if e.tag != 2 { panic("wrong Counter variant") }
    return &e.value
}
```

这不是开放数字 enum：tag/字段不公开，构造、匹配与输入校验仍只认封闭变体。跨包需要受检的生成 accessor，编译期间保留引用资格；最终指针表示不要求独占、不引入借用风险分析。普通 struct 复制自然复制 payload 值；slice/map 的内部存储仍按 Go 浅复制，不能偷偷深复制。

serde、Debug、Clone、Error 与 Default 必须迁移到 active-tag 分派；仅处理活跃变体，Default 仍只要求明确选中变体的载荷。用户 enum/Result 零 tag 可以保持非法值，Option 零 tag 可以表示 None，Some(nil) 仍区别于 None。不扩递归/任意对象图能力。

但这不是透明 ABI 替换：

- 原 Go case 类型断言 API、Option 的 nil interface 表示会变化；生成构造名称可保留，但需要迁移说明和正反例。
- Go `==` 的能力会变化：struct 中任一不可比较载荷会使整个 enum 不可比较，而旧 interface 在活跃 unit case 上仍能比较。全部载荷可比较时，原生 struct 比较又会看非活跃字段；不能假装自动得到 active-only enum 相等。当前 hgo 已实际接受 Unit==Unit、enum 与 nil 比较；含 slice 的活跃 case 自比较会按既有 Go interface 行为 panic，独立生成 race 已验证。迁移不能静默破坏这些源级程序，须明确保留/降低；Go 消费 API 的变化也应显式说明。
- 存储尺寸是多个 payload 槽位之和，不是最大 payload 的大小；不要声称零体积成本或未经测量的零分配。
- 旧借用跨 variant 替换建议采用独立槽的普通 Go 语义：`p=&e.A; e=NewB(...)` 后 p 指向清零的 A 槽，写 p 不修改活跃 B；再赋 A 时 p 观察该槽新值。因此 validator 不应要求非活跃槽永远为零。同型变体共用槽会产生另一种可观察别名行为，不应默认这样优化。

[独立机制探针](../../../tests/research/match_lowering/place.go.fixture)已经 race 运行验证值复制/稳定槽/浅复制，但尚未证明编译器迁移完成。关键待选项是是否接受尚未冻结的生成 Go enum API/布局迁移；若坚持旧 interface API 和普通 Go copy 行为全部不变，目前没有已证明同时满足真实 payload 指针的简单安全方案。不能把复制写回或隐藏共享包装伪称为等价方案，也不能因此把原位借用悄悄后置。

## typed IIFE 与原函数语句展开

用户提出的 `func() T { match ...; return x }()` 对纯求值是有效候选。现有 explicit user IIFE 的 int/跨包具名值真实 hgo→Go race 已通过；新引用返回的 explicit IIFE 当前仍在 refs.Prepare 拒绝。自动包装尚未实现。

[可复现机制实验](../../../tests/research/match_lowering/README.md)比较 int、具名 uint64、原生指针、参数、短路、索引赋值。Go 1.27.1 上所测小 IIFE 均内联、全部样本零分配；这否定“一有闭包必然分配”的预设，不证明任意 match 都同样优化。纯 Go tag switch 基准没有模拟全部 hgo enum 校验或引用权限。

IIFE 可以让 Go 自己管理原表达式的位置和许多上下文求值规则；不足是返回类型必须可表示，生成闭包必须通过权限检查，且不能改变语义调用帧。`?` 必须退出最近用户函数，编译器包装不算新用户函数；直接 recover 即使位于普通表达式 arm，也会因包装而失效。不能提前执行未选 arm 的 recover 来规避。当前 expression arm 只支持表达式，不能把任意 return/defer/break 语句块当成已经支持；statement match 保留外围控制流则是另一现有能力。

建议实现路线：以带结果的语句前缀作为统一语义表示；对不含外层退出、直接 recover 或其他已知帧敏感操作，且类型/权限能表示的纯分支，可选择 typed IIFE 发射。其余在原函数按求值顺序展开，必要时使用明确的退出状态传回外层，但不能截获用户函数的传播。新增上下文须以真实 hgo 正反例和生成模块验收闭环，不能把本机制报告算成前端交付。

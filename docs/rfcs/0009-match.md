# RFC 0009：模式匹配与按模式绑定

## 状态与范围

变体分派与穷尽检查是已确认目标；用户已明确需要按 `Ok(value)`、`Ok(&value)`、`Ok(mut& value)` 区分复制和借用。
这些模式的具体语法、guard 借用范围及 Go ABI 尚未冻结，编译器尚未实现。
本篇将旧草案“首版仅值绑定”的范围扩展为三种绑定的设计目标；不能把旧候选布局当成拒绝借用需求的理由。
背景见[枚举专题](../design/enums-and-match.md)；枚举身份与构造见 [RFC 0008](0008-enums.md)。

## 源级规则

| 模式 | 绑定语义 | 对应约束 |
| --- | --- | --- |
| `Ok(value)` | 复制 payload 到局部值 | 要求该复制合法，不是隐式 move |
| `Ok(&value)` | 对原 payload 建立读借用 | 保留来源和有效范围，不借用隐藏副本 |
| `Ok(mut& value)` | 对原 payload 建立写借用 | 需要可写位置；借用期间禁止冲突访问 |

被匹配表达式求值一次；对位置表达式，一次确定其来源位置，不要求先复制整个枚举。
“求值一次”不决定复制还是借用，也不表示整个可达对象图只读。trait 适配中的同一要求只约束接收者表达式次数。
分支按源码顺序尝试，模式成功才建立绑定和执行 guard；guard 失败不撤销已经产生的副作用。
无 guard 的分支才参与穷尽覆盖；不可达分支报错。`return`、`break`、`continue` 保留外围控制流目标。

## 转译示例

**M01：值绑定复制 payload，调用表达式只执行一次。**

```hgo
n := match load_result() {
    Result::Ok(value) => value + 1,
    Result::Err(_) => 0,
}
```

```go
// 使用 RFC 0008 的候选 A 及其中的 Result / Ok / Err 定义。
saved := loadResult()
var n int
switch c := saved.(type) {
case Ok[int, string]:
    value := c.Value
    n = value + 1
case Err[int, string]:
    n = 0
default:
    panic("invalid Result")
}
```

复制包含 slice 或共享句柄时仍保留其内部别名；M01 不承诺深复制。

**M02：读借用必须指向匹配对象的 payload。**

```hgo
match r {
    Result::Ok(&value) => inspect(value),
    Result::Err(_) => (),
}
```

```go
// 仅为候选 B 演示：tag struct，不据此选定最终 ABI。
type IntResult struct { Tag uint8; OK int; Err string }
place := &r // 此段 r 的类型为 IntResult；没有先复制 r。
switch place.Tag {
case 1:
    inspect(&place.OK) // inspect 的只读契约由 hgo 检查。
case 2:
default:
    panic("invalid Result")
}
```

候选 B 可直接寻址，但存储可能包含所有变体字段；大小、初始化与公开桥接需要另外评估。

**M03：写借用修改原 payload；值型 interface case 的直接取址不成立。**

```hgo
match r {
    Result::Ok(mut& value) => { *value = 2 },
    Result::Err(_) => (),
}
```

```go
// 候选 B 的预期行为：r.OK 变为 2。
place := &r
switch place.Tag {
case 1:
    value := &place.OK
    *value = 2
case 2:
default:
    panic("invalid Result")
}
```

```go
// 反例：候选 A 不能照这样实现上面的 hgo 程序。
var boxed Result[int, string] = Ok[int, string]{Value: 1}
c := boxed.(Ok[int, string])
value := &c.Value
*value = 2
println(boxed.(Ok[int, string]).Value) // 仍为 1，原 payload 未变。
```

改成指针 case 可寻址，却会使普通接口赋值共享 case；临时副本再写回也需要独立证明 guard、panic、重入与退出时序。

## 拒绝与边界

**M04：读借用写入由 hgo 拒绝，即使生成指针允许写。**

```hgo
match r { Result::Ok(&value) => { *value = 2 }, Result::Err(_) => () }
```

```go
value := &r.OK
*value = 2 // Go 类型检查允许；不能作为 M04 的合法输出。
```

可变模式要求真实可写位置及权限；原生 Go map 的值槽位不可寻址，不能取临时副本冒充原位借用。
活跃 payload 借用期间，禁止通过冲突别名替换整个变体；借用也不能逃出其允许范围。
临时表达式能否建立分支内借用、混合绑定与 guard 失败时的借用结束点，须另写正反例后冻结。
展开不得提前执行短路右侧的 match，不得无条件套闭包而改变 return 或 defer 的作用域。

## 双向互操作

hgo 可以将 match 降低为 Go type switch 或 tag switch；禁止源级开放断言不意味着后端不能使用这些机制。
纯 Go 调用方不会获得 hgo 的穷尽检查或读写权限诊断；输出指针的普通 Go 行为必须在公开契约中说明。
原生返回值经合法 enum 视图进入 match，未经验证的非法动态类型不能被用户通配分支吞掉。
枚举返回 Go 或跨生成包传递时，必须保持已选布局、复制语义和变体身份；借用不因装箱或桥接延长寿命。

## 验收与未定项

1. M01 的 load_result 调用计数为一；M02 指向原 payload；M03 的修改在借用结束后可见。
2. M04 在 hgo 阶段拒绝；Go 允许写指针不构成 hgo 正例。
3. 覆盖嵌套模式、guard 副作用、多个 guard、短路、return、break、continue 和 defer。
4. 未定：最终表示、借用返回、临时值期限、guard 权限以及各布局的复制与分配成本。

# RFC 0018：转译前后的共同对应约束

## 状态与范围

已确认：每项语法需有独立 RFC 与转译对照；goxide 语义检查先于生成 Go。暂定：具体 IR 与检查算法；当前没有生成器。本文定义跨特性的评审方式，工程架构见[工具链专题](../design/compiler-and-tooling.md)。

## 源级规则

每个 lowering 记录：源级类型与权限、生成 Go 类型、已选 impl、表达式求值顺序、存储位置、复制/别名、控制流、边界检查，以及原始/宏/生成源码位置。相同 Go 类型不意味着相同 goxide 类型或权限。

成员定义按声明形式区分：`impl T { ... }` 的固有成员按 [RFC 0020](0020-associated-functions.md) 转译，`impl Trait for T { ... }` 按 [RFC 0002](0002-traits.md) 保留显式实现身份。直接写出的 Go 包函数与接收者方法保持 Go 声明语义，不凭命名自动建立关联函数或 impl。这一边界不取消 enum、match 等其他增强语法各自的转译规则。

impl 中的 `Self` 绑定源级目标类型及其泛型实参，`self` 绑定实例接收者；trait wrapper 是生成表示，不能替换源级 Self 身份。无接收者的关联函数没有 self。后端根据已解析绑定降低这些名字，普通 Go 声明中的同名标识符、字段名与字符串不参与文本替换。

保证以合法 goxide 程序及声明的外部契约为前提。输出可以多出临时变量或检查，但不能改变规定的业务副作用；不以输出文本相同代替行为相同。

## 转译示例

### LOW-01：一次适配中的求值次数

源级片段，consume 期望 trait，`make_value()` 已选定相应 impl：

```goxide
consume(make_value())
```

Go 函数体片段，假设 `AsReader` 是选定 impl 的生成工厂：

```go
tmp := makeValue()
consume(AsReader(tmp))
```

makeValue 执行一次，工厂只适配该值。执行两次原语句仍各调用一次；这不规定一个对象终身只构造一次视图。若优化为直接调用，仍保留相同实现和次数。

### LOW-02：短路中的 match

源级片段，hit/miss 都是 BoolResult 的无 payload 变体；具体 enum 表示未定：

```goxide
ready && match next() {
    BoolResult::Hit => true,
    BoolResult::Miss => false,
}
```

以下 Go 函数体片段采用候选 tag 表示，`next`、`hit`、`miss` 由该候选表示定义：

```go
answer := false
if ready {
    saved := next()
    switch saved.tag {
    case hit:
        answer = true
    case miss:
        answer = false
    default:
        panic("goxide: invalid BoolResult")
    }
}
_ = answer
```

ready 为 false 时 next 不执行；为 true 时仅执行一次。不能把 `saved := next()` 无条件提升到 if 前。

### LOW-03：声明与产物往返

```text
goxide 源码 + 所选 Go/声明依赖
    → 检查后的 IR
    → Go 实现 + goxide 声明元数据
    → 另一 goxide 包读取
```

回读须恢复相同的公开类型、impl、关联类型、unsafe、权限和 enum 身份。普通 Go 只读取 Go 实现；丢失元数据时不凭生成方法名猜回可信语义。

## 拒绝与边界

| 编号 | 错误降低 | 不得改变的语义 |
| --- | --- | --- |
| LOW-N1 | 为适配重复执行 makeValue | 次数与副作用 |
| LOW-N2 | match 全部套立即调用闭包 | return/defer/break/continue 的所属作用域 |
| LOW-N3 | 将原位借用改为对 case 副本取址 | 原存储与修改可见性，见 [match](0009-match.md) |
| LOW-N4 | 因 Go 函数签名相同而丢弃 unsafe/只读/不逃逸信息 | callable 效果与权限 |
| LOW-N5 | 新增原生同名方法后绕过已选自定义 impl | 行为来源与 ABI |
| LOW-N6 | 将普通 Go NewT 自动解释为 T::new，或全局替换 Self/self 同名文本 | 声明身份与名字绑定 |

## 双向互操作

goxide → Go：检查真实函数签名；直接参数可转换不代表函数值可直接赋值，必要时生成桥接函数。桥接须保留 nil、求值时机、效果与别名契约。

Go → goxide：验证选定公开 ABI 能构建和调用，执行已规定的输入检查。普通 Go 的静态权限不属于 goxide 保证；不能用这个范围限制掩盖生成代码本身不合法的问题。

## 验收与未定项

验收由源级接受/拒绝、生成 Go 编译、行为观察和声明回读四部分组成。LOW-01 统计调用次数，LOW-02 统计短路路径，LOW-03 比较恢复的语义身份；不只做生成文本快照。

宏展开可一对多，生成辅助代码可多对一对应源表达式。位置映射应关联原始调用、定义和生成位置，而非仅保存固定行号偏移。IR 字段、元数据格式、增量失效及测试运行器尚未实现。

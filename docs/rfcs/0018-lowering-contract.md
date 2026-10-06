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


## A03 合成错误点与宏展开栈（2026-10-06，实施合同）

本片增强已有来源诊断，不改变语言语义、拒绝规则、生成 Go 行为或宏绑定规则。
错误点的位置与宏展开栈是独立证据，不能因一者未知而捏造另一者，也不能无条件
丢弃另一者。`?` 预处理插入的 marker/括号没有等长原始 token，仍标记
`generated/unmapped`；不将最近的 `?`、`new` 或捕获 token 当作其精确来源。

| 记录 | 本片规则 |
| --- | --- |
| 错误点 `Origin` | 仅接受非空半开原始范围，文件、偏移与坐标必须属于正确来源。零长、越界、无法证明或错误阶段的范围不能声称真实源码位置 |
| 原始 macro metadata | 明确记录为 original 的定义文件坐标不再套用调用文件的 prepared map；original 标志不能使无效/零长范围变成有效 |
| prepared token | 仅通过所属文件的显式 token 映射恢复；其他文件相同数值偏移、重复文字、最近位置均不是来源证据 |
| Definition/Callsite frame | 由已知宏语法范围及其可证明的原始边界恢复。范围内可包含 `?` 合成内部，不要求整段等长；frame 不是其内部每一字节的映射承诺 |
| 完整展开栈 | 只有全部 frame 的定义与调用范围均可证明时，保留原 outer→inner 顺序；本片对任一不可证明 frame 仍保守不展示整条链，不拼接看似完整的部分链 |
| 合成点与已知栈 | `Origin=nil`、`Kind=generated/unmapped` 可以同时有已证明的完整 `Stack`。结构化错误、最终 Go map 与文本诊断应一致保留两者的区别 |
| 原生 Go 诊断 | 保留原始 stderr，只补充匹配的具体生成文件；合成点可补充准确宏定义/调用栈，但不能显示虚构的 source span |

基线 `c32b94f` 有两个明确问题：单文件错误在 point origin 未知时清空已知栈；
包宏收集用零长范围记录无来源的预处理 token，但错误映射可能因 original 标志
把它当成真实 source span。本片须同时修正准确性和信息保留，不能仅放开栈显示。

验收包含：单文件/同包多文件/跨包、nested macro、调用前及模板中的多个 `?`、
make/new 类型槽、普通模板语法及真实 Go 类型错误、enum/derive 前后位置变化、
相同文件名或相同数字偏移的反例、LF/CRLF、重复运行与原生输出不变。未知点仍未知，
原始 token 仍精确；不将 helper、合成传播 AST 或 refs 中间阶段的其余缺口一并宣称完成。

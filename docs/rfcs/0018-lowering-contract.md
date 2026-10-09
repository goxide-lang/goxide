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


## A03 AST 变换的证据快照与生成节点（2026-10-06，实施合同）

本片在 `f5b6ce658e976d4d5e951e1bb7d3ce4aaea14be1` 之后继续已有来源合同，
不增加语言语法或用户可见的 stack schema。上一片的已验证生产源码是
`fd8380c39876c96441857a737be8e32c322a0467`。`len($x?)` 暴露了两个通用缺口：
builtin alias 在 AST 中改名后，首次打印仍与改名前的 token 串比较，清掉整文件映射；
传播降低创建的新节点只有输出位置，没有显式原因记录。不能用诊断名字或附近 token
逐例修补；本片建立可复用的变换边界和单原因登记。

| 不变量 | 实施规则 |
| --- | --- |
| 变换前冻结 | 输入 AST、FileSet 和 map 属于同一阶段时，冻结节点身份、具体自有 token 槽、token kind/text/range、点来源与完整栈。输出时不得再用已改变的 Pos 查询旧 buffer |
| token 自有权 | Ident 的名字、运算符的 OpPos 等是明确槽；BinaryExpr.Pos 属于左子节点，CallExpr.Pos 属于 callee，不能默认作为复合操作原因。本片的 question 原因是明确的 marker Fun Ident |
| 存活原节点 | 未变 token 可保留冻结点；同一指针改名不能继续声称精确旧点，只能保留独立已证明的完整栈。位置改动、相同文字或数值偏移不是新证明 |
| 显式生成原因 | 新 token 只有通过明确单原因 handle 登记才能携带栈，始终 Origin=nil、generated/unmapped。无原因、未知原因、相冲突原因保留未知；相同 handle 的重复登记可幂等 |
| 不覆盖子节点 | 输入节点的未知记录同样不可被外层填补；保留子节点各自证据。只可先登记完整新建的编译器模板，再把旧 operand 放进明确空槽；组装后的混合树不能递归盖外层栈 |
| owner 身份 | 原因 handle 带本次阶段/registry 身份，不能按显示文件名、offset、栈文本猜同源；另一包同名文件及另一 FileSet 同值 Pos 不可冒充输入 |
| 输出对应证明 | 对实际变换后的 AST 与重新解析的打印输出验证节点类型、子字段角色/顺序及语义标量（如 operator、literal kind、ellipsis）。不以相等 leaf 文本代替完整结构；不匹配则保守丢弃未证明映射 |
| 分段边界 | 只映射已证明自有 token 范围，不用整个 parent 范围覆盖子节点。打印新增/删除的括号不能按最近文字、最长栈或共同前缀猜所有者 |
| reparse | 每次重新解析先产生本阶段 map，再捕获新 registry；合成 context 不因下一阶段出现位置而升级为原始点 |
| 行为保持 | 求值次数/顺序、最近用户传播帧、短路、panic/recover/guard、权限与原生错误文本保持；共享 helper 声明不归给第一次调用 |

`CaptureAST` 的调用前置条件是传入当前同一次 parse 的 root、FileSet 和输入 map。
Go AST 本身没有所属 FileSet 标记；人为把另一个 parse 的 root 与数值完全相同的 Pos、
文件名和字节配对，无法仅靠 AST 判别。实现校验可观察的文件范围/token 证据，实际入口
负责保存配套对象；捕获后，未登记的 foreign node 及别 registry 的 cause 不会获得来源。
这不是从裸 Pos 推断 owner 的许可。

现有 nil Stack 同时可能表示无宏与未知链，本片不靠 segment 存在来推断“已知空链”。
只传递已经证明的非空完整链；未知/未登记/冲突状态在内部区分，不引入部分链拼接。

分阶段验收：

1. 共享证据快照/打印传递先覆盖 frontend 的 parse→ResolveBuiltins→首次打印，
   再替换现有三个 propagation AST 传递入口。单元反例包含改名/改 Pos、NoPos、
   跨 FileSet、重复文字不同 owner、结构/运算符不匹配、未知墓碑与冲突原因。
2. 在共享 questionFlow 构造处登记新建 scaffold/value，并保留已经降低的 operand AST；
   普通表达式、effect plan、guard frame 三类调用者共同验收。新的 AssignStmt 不经
   print/reparse 丢弃原操作数；不自动给所有临时变量工厂或共享 alias 补栈。
3. 单层/嵌套/重复 sibling、同包跨文件/跨包同名文件、enum、LF/CRLF 验证精确完整栈；
   合成 payload 引用继续无原始点。旧点精度、未知内层和非宏控制不能退化或被外层覆盖。
4. 对合法程序做基线/当前生成 Go 与运行行为对照；再做独立审查、完整 Mac race/vet/
   design、真实 CLI/WASM 精确诊断及错误后恢复，记录准确源码 SHA。Linux/托管 CI 分别记账。

本片不关闭 A03 整项。冗余括号消除的多 owner 歧义、refs 编辑坐标、共享 helper 因果备注，
以及其他尚无单原因证明的临时节点继续独立保留；需要猜测或新增契约时停在建议。

## A03 宏与 equality 派生组合（issue #6，2026-10-09）

`zero!() → int(0)` 与 `PartialEq` / `Eq` 派生同文件时，equality 追加 helper
改变整文件 token 串，旧 `Compose` 因而清空未变宏调用的来源。本片将上述 AST
合同接入 `equality.Prepare` / `equality.Lower`：每次 parse 后、任何改写前按文件
冻结证据，打印后用实际 AST 结构对应传递；不放宽 `Compose` 的拒绝猜测规则。
逐文件记录实质改写，完全未改写的文件继续使用整串 token 证明，保留 AST 自有槽
尚未覆盖的标点精度；改写文件只保留已经支持且有证据的 AST 自有 token。

原调用和参数的存活 token 保留各自点与栈。把 `receiver.equals(arg)` 改写为
helper 调用时，新 callee 只从原 `equals` 标识的自有 token 取得完整栈；延迟
intrinsic 的后续改写同样只从该标识取得栈。新 token 保持 `Origin=nil` 和
`generated/unmapped`，不能把 receiver 的位置充作调用标识来源。共享派生 helper、
别名绑定和手写 enum 的验证前缀无唯一宏调用原因，保持未知，不继承首个调用者。

验收包括独立最小失败回归、PartialEq/Eq、嵌套和重复调用、同包多文件、LF/CRLF、
非宏与共享 helper 对照，并比较结构化来源、最终 Go map、适用 CLI 诊断及
CLI/Go WASM 输出。原生成 Go 行为和既有编译拒绝合同不变；本片只关闭 issue #6
的已复现组合，不关闭 A03 整项或其他 lowering 的来源缺口。

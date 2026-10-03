# RFC 0009：模式匹配与按模式绑定

## 状态与范围

变体分派与穷尽检查是已确认目标；用户已明确需要按 `Ok(value)`、`Ok(&value)`、`Ok(mut& value)` 区分复制和借用。
具体借用模式、guard 规则及支持原位访问的 Go ABI 尚未冻结；已有有限值匹配前端不代表这些借用模式已实现。

D19/D20 边界：读写权限和已确认类型不变量照常检查；别名、留存、独占与重入风险分析独立长期推进，不属 MVP。未来仅有具体风险证据才 warning；未知不默认拒绝或告警。

本篇将旧草案“首版仅值绑定”的范围扩展为三种绑定的设计目标；不能把旧候选布局当成拒绝借用需求的理由。
背景见[枚举专题](../design/enums-and-match.md)；枚举身份与构造见 [RFC 0008](0008-enums.md)。

## 源级规则

| 模式 | 绑定语义 | 对应约束 |
| --- | --- | --- |
| `Ok(value)` | 复制 payload 到局部值 | 要求该复制合法，不是隐式 move |
| `Ok(&value)` | 对原 payload 建立读借用 | 保留原存储身份和只读资格，不借用隐藏副本 |
| `Ok(mut& value)` | 对原 payload 建立写借用 | 需要可写位置和写资格；不承诺独占 |

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
经其他可写别名替换变体不因活跃引用而默认拒绝。其对已取得 payload 引用的可观察行为须由最终 ABI 说明；有证据的失效/别名风险按 D19/D20 独立研究，不能以不逃逸证明作为准入门槛。
临时表达式能否建立分支内借用、混合绑定与 guard 失败时的借用结束点，须另写正反例后冻结。
展开不得提前执行短路右侧的 match，不得无条件套闭包而改变 return 或 defer 的作用域。

## 双向互操作

hgo 可以将 match 降低为 Go type switch 或 tag switch；禁止源级开放断言不意味着后端不能使用这些机制。
纯 Go 调用方不会获得 hgo 的穷尽检查或读写权限诊断；输出指针的普通 Go 行为必须在公开契约中说明。
原生返回值经合法 enum 视图进入 match，未经验证的非法动态类型不能被用户通配分支吞掉。
枚举返回 Go 或跨生成包传递时，必须保持已选布局、复制语义和变体身份；装箱或桥接不得擦除读写资格。GC 保持可达对象存活，不据此宣称临时访问或不留存契约已获证明。

## 验收与未定项

1. M01 的 load_result 调用计数为一；M02 指向原 payload；M03 的修改在借用结束后可见。
2. M04 在 hgo 阶段拒绝；Go 允许写指针不构成 hgo 正例。
3. 覆盖嵌套模式、guard 副作用、多个 guard、短路、return、break、continue 和 defer。
4. 未定：最终表示、借用模式的返回签名覆盖、临时值寻址、guard 权限以及各布局的复制与分配成本；普通引用可保存/返回的方向不变。
5. 为普通复制、保存和其他可写别名保留接受例；未来冲突风险 warning 单独验收，M04 的权限错误不降级。

## 常用表达式组合实施切片（2026-10-02）

沿用[表达式上下文规则](../design/enums-and-match.md)的期望类型、顺序和非局部退出要求，本阶段没有增加模式语法。
旧范围为显式类型局部变量的整个 initializer、一个非具名结果的整个 return。
新增完整 arm 内递归 match，以及同文件普通函数的完整调用实参：调用自身必须是整个 return、独立调用语句或已获期望类型的完整 arm/实参。嵌套完整调用可以递归组合。

普通函数须非方法、非泛型、非 variadic，且名称在当前文件没有绑定/函数值用途；任何 `//go:` 指令修饰的声明不采用本 lowering。此项是有限签名解析边界，不是别名安全限制；未含此种 match 的普通 Go 用法不受影响。包级类型别名绑定参数声明处的类型，防止调用处的局部类型/导入名遮蔽。所有实参依次以参数类型求值，match 仅执行选中 arm；嵌套 match 继承相同的期望类型。调用展开放在普通块内，避免新增局部变量改变外围 goto 的合法性。

没有生成 IIFE、额外函数帧或正常路径 panic。`?` 仍退出最近函数，先前实参副作用保留，后续实参和调用不执行；enum 的非法 Go 表示沿用既有验证器。独立审查修复了 goto、内联闭包 owner 和括号函数类型绑定三个边界，回归见[接受测试](../../tests/acceptance/match_composition_test.go)。

尚未实现且不视为获批延期：任意运算符/短路/字面量内部、普通赋值、局部 `:= match` 推断、方法/动态/跨文件签名/泛型/variadic 参数、表达式块，以及此有限提取器不支持的复杂签名。tuple/record 模式没有扩大。常用组合阶段后先转向公开引用元数据；这些 match 长尾仍留在 T0 清单。

## 普通赋值与局部结果推断切片（2026-10-02）

新增单个既有局部标识符 `x = match ...`、`x := match ...` 和 `var x = match ...`；沿用已支持封闭 enum、guard 和模式，不增加 tuple/record/借用模式。复杂 LHS、多目标和控制头不在本片，必须明确诊断而不是错误地提前 scrutinee 或改变 LHS 求值顺序。

有上下文的赋值使用真实目标变量类型，逐 arm 按原有 Go 可赋值规则检查，包含常量范围与 nil。无上下文推断先要求所有已定型 arm 具有同一真实类型，再检查未类型化常量/nil 是否可赋给该类型；只有未类型化 arm 时要求同一默认类型，统一默认一次。全 nil、不同 defined type、不同默认类型或需要公共 interface/隐式数值转换时明确要求类型补充。别名 spelling 不建立新名义身份，底层布局相同不合并不同类型。

语法阶段保留待解析临时类型；类型阶段与 `?` 签名共同迭代，支持 arm 中的问号和推断出 Result 后再问号。arm 用无赋值上下文的 Go CheckExpr 重新查询，不把临时 pending interface 的默认类型作为结果；每次解析后重新检查整个包，全部类型确定后才保存传播 lowering 的上下文事实。最终 assignment 检查包含具体常量值，不能只用 AssignableTo 代替溢出检查。没有共同类型时不退化成 any。

现有目标类型见证只在编译期读取类型，生成 Go 前删除，不额外读取/取址目标。`=` 展开放在普通块中，最终目标赋值位于 pattern 绑定之外；`:=` 最后才声明用户变量，arm 内同名标识符仍绑定原来的外层变量，无新变量和 goto 的原合法性保留。包级卫生类型 alias 保护 import/builtin 名称，局部类型须在当前位置解析为同一身份；不可命名的私有/遮蔽类型明确诊断，不猜拼写。

推断引用结果复用已有具体引用 alias，保留读写资格。穷尽 expression match 的结果临时量由本次编译逐文件显式登记，成功路径必赋值、失败问号退出；只有登记项允许无 initializer，不按名称前缀放开用户引用零值。此内部例外不改变源级引用初始化语义。同次 module 构建先绑定公开引用 metadata 再进行 match/问号类型推断，导入函数结果保留真实名义引用资格；完成权限检查后才恢复原生调用。不能借 raw Go 指针自动丢失资格。

[真实模块验收](../../tests/acceptance/match_assignment_test.go)覆盖目标和 arm 一次求值、选中副作用、上下文 1<<63、别名/跨包具名类型、nested match/最近闭包问号、defer/direct recover、外层同名、goto、引用结果及 source 类型诊断。生成模块单独 race/vet，两次产物逐字节一致；15 个拒绝反例包括 mixed/nil/overflow/no-new-variable、复杂LHS/control-header、readonly写/any和未初始化/伪造临时量。类型诊断使用保存的源位置，缺可证明来源仍标 unmapped；不把 CheckExpr 合成表达式坐标冒充源码。生成代码没有新增 IIFE、正常失败 panic 或额外目标读取。

剩余：任意运算符/短路/字面量、复杂目标、动态/方法/其他参数调用、私有不可命名结果和更完整来源映射。这些是工程/表示范围账，不自动改成批准延期。下一先执行多包联合验收，用真实阻断而不是继续扩大模式系统决定收口。

## 原位借用与表达式实现选择（2026-10-02）

用户 D29 已明确 `match mut& enum_value` 的可写 payload 原位借用是首版必需，不再作为可后置候选。D30/D31 同时要求跨 module hgo 增强恢复和自然可写指针 Go 互操作。现有值型 case 不能借副本地址冒充原位；[有界实现评审](research/match-place-and-expression-options.md)给出 tag struct 候选、实际 Go API/比较/旧借用取舍，以及用户提出的 typed IIFE 机制实验。布局尚未冻结，当前前端尚不支持新借用语法；纯 Go 探针不算实现。普通复杂目标/嵌入表达式仍是工程欠账，不能因 IIFE 的复杂分支限制而拒绝评估纯分支，也不能让生成包装改变问号或 recover 的用户函数边界。

## 二元表达式与简单索引目标（2026-10-02）

生产前端现支持 `return 10 + match ...`，以及 `a[index()] = match ...`。二元算术、位运算/shift、比较和 &&/|| 可在单结果 return、局部初始化/赋值及已有静态调用/arm 子集中组合；索引目标首片为一个标识符加一个 subscript，接受 slice/map/array/*array。未增加模式、enum ABI、任意调用解析或原位借用语法。

语法阶段用专用保留名称的编译期 function-literal 节点承载结果槽和 statement match，类型阶段解析后全部变成原函数内的 prelude/value，最终 Go 没有该包装调用。普通用户闭包保留真实退出边界；短路右侧的整个 match 仅在条件成立时展开执行。问号失败沿最近用户函数返回，不用 panic；直接 recover 不新增调用帧。return/普通赋值的生成变量处于独立普通块，不破坏 forward goto。

类型候选沿真实二元 AST 读取同一操作数域的 typed peer，每轮先推进可确定的问号载荷签名，再解析已有具体 arm 的槽，最后默认常量；问号依赖未定 match 时仍等待下一轮，支持两侧 match 与嵌套运算而不提前钉死 int。未类型化 peer 用独立 CheckExpr 查询，不从 provisional interface 接受已默认的 int。期望数字/bool 类型来自真实用户签名或目标；局部返回类型不搬到包级 alias。比较返回 bool 不污染操作数，shift count 不继承左侧类型，返回 any 时算术 match 仍按实际 arm/peer 类型推断。仍用现有同型/精确可赋值规则，不添加数值 widening 或共同 interface；私有不可命名结果等范围继续留账。

原始索引 assignment 交给 typed place：先依序保存 slice/map/pointer 边界和 index，array 保留原存储；RHS match/问号完成后才写入并执行 bounds/nil-map/nil-array 检查。类型见证只在编译期，不额外读取 `a[index()]`；RHS 重绑 slice/map 仍写旧容器，重绑 array 后仍写原变量存储。引用临时量复用既有逐文件可信登记，readonly 的写入/any 擦除继续拒绝。

[真实多包验收](../../tests/acceptance/match_binary_index_test.go)含四项生成模块 race 测试、vet、两次确定性、七个负例；覆盖 typed peer/两侧 match/大整数、scope/goto、短路、一次求值、存储及 panic 时点、失败/defer/direct recover 与用户闭包边界。原先 operator、short-circuit、simple-index 三类拒绝用例迁入正例，不通过删除合法功能来收口。[优化构建实测](../../tests/research/match_lowering/README.md#production-binaryindex-preludes)比较真实 hgo 输出与显式 statement match，不能与早期纯 Go IIFE 探针混为实现证据。

本片采用 prelude 统一保留控制语义，不否定已有 IIFE 机制实验，也不宣称任意组合零成本。更复杂目标/控制头/字面量/动态调用/不可命名结果仍是工程账，未获自动延期；enum 表示迁移继续等用户选择。

## 命名返回与静态类型已知的函数变量 match（2026-10-03）

两个旧最小失败例现已接通：`func F(x E)(out int){return match x{E::A(n)=>n,E::B=>0}}` 与 `func F(x E,g func(int)int)int{return g(match x{E::A(n)=>n,E::B=>0})}`。命名单结果从真实用户函数签名获得类型，避免局部同名类型捕获。arm 先求值到私有 block 内的临时槽，普通 return 再写命名返回槽；defer 观察/修改最终槽，arm panic 不提前改槽，direct recover 保持用户帧，goto 不跨生成声明。

函数变量调用仅接受标识符、静态已知非 variadic 签名及精确 arity。类型检查沿实际 enclosing call 获取参数类型并确认变量对象，不复制同名包函数的参数拼写，不执行类型 witness。既有 propagation 求值机制先保存函数值，再按顺序求实参；实参中重绑 g 不改变已取出的函数值，nil 函数在参数求值完成后才触发调用 panic。scrutinee/guard 保留一次求值，`?` 失败仍返回最近用户函数，跳过后续实参与函数调用。编译器暂存 atom 在 Go 输出前消除，不新增 IIFE/反射/类型擦除。

`tests/acceptance/match_named_dynamic_test.go` 为真实 CLI→Go 模块 race/vet 验收，含确定性、返回槽/defer/panic/goto、direct recover、named 类型遮蔽、函数值捕获、参数 uint64 上下文/shift、局部/statement/嵌套组合、nil 时机、Result 失败/defer/用户闭包，以及负例原 match 行列。旧 named return/dynamic argument 及四条遮蔽/括号/分组函数变量拒绝测试已迁入正向执行验收，未删除其场景覆盖。独立审查另验命名槽旧值、首实参重绑函数变量与包函数同名遮蔽。纯 Go 设计探针单独报告，不替代这些执行证据。

该片之后的调用形状缺口已由下节统一签名组继续接通；多结果 `return match` 仍未实现，参数数量、溢出和不兼容类型仍正常拒绝。这些是当前实现边界，不自动批准延期。blank/embedded 字段、公开 enum 入口、Proposed adapters 不改，整体 MVP 未宣布完成。

## 统一静态调用签名与 match 参数（2026-10-03）

`tests/acceptance/match_call_signatures_test.go` 接通函数字段/直接方法 `s.F(M)`、返回的函数 `factory()(M)`、variadic `g(M)`/固定参数加 `g(prefix,M,rest...)`、展开参数本身为 `g(M...)`，以及原生 Go 声明的显式 `call[int](M)`（M 为完整 match 表达式）。同文件静态函数、跨文件/跨包函数、函数变量、方法表达式共用实际 Go 签名和参数身份；不再扫描同名函数或复制其参数类型拼写。variadic 普通尾项取元素类型，`...` 取 slice 类型，Go 自身仍执行展开/调用；禁止将额外 variadic 元素与 `rest...` 混合的非法 arity。`.hgo` 用户泛型定义仍在 enum 前端明确拒绝，不能与原生 Go generic 调用支持混淆。

函数值沿既定规则在参数前保存一次；nil 函数直到调用时才 panic。直接方法不能变成提前创建 method value：先执行 receiver 中调用、receive、短路运算，普通读取、索引、类型断言和隐式解引用留在最终直接调用。为提前执行某个 receiver 调用所必需的操作仍随之执行，例如 `a.(*S).Get().Method(M)` 必须先断言才能调用 Get。隐式取址继续由 Go 对原 receiver 判定，原本不可寻址的函数返回 struct/array 元素不会因编译器临时变量而获得资格。`?` 失败直接返回最近用户函数，不调用最终 callee；direct recover 不增加用户调用帧。只读到原生可写指针/接口/方法的擦除仍拒绝。

Go 规范 Order of evaluation 只规定调用、接收和逻辑运算等的相对次序；普通读取/断言/索引相对实参调用并非全部规定。独立矩阵发现同一 native receiver 断言在默认与 race 构建中可出现不同先后，因此验收分开验证：确定的调用顺序、hgo 一致的 late receiver 规则、原生未规定次序的允许结果。不把某次优化器时点伪装成语言保证，也不按指针/值断言逐条模拟 Go 优化器。

真实 CLI 输出覆盖独立生成模块 race/vet、输出确定性、callee/receiver 一次求值、值/指针 receiver、nil receiver/interface/callee、嵌入 receiver、索引及断言、私有名义类型的公开 alias、scope shadow、变参 nil/empty 和 backing slice 身份、原生显式 generic 的上下文/溢出、`?`/defer/recover、goto 和权限负例。旧 selector/factory/variadic/generic-call 与静态函数被取值的拒绝用例已迁为正例，不重新添加为缺口。独立审查修复 method-value 提前检查后通过真实 race/vet。

优化构建探针（非 race；Go 1.27.1，单 CPU，100ms×3）中，方法、slice 展开、显式 generic 的真实 hgo 输出均为 0 B/op、0 allocs/op；中位数约 1.963/1.706/1.655 ns/op，内联原生 Go 对照约 0.5635/0.2835/0.2828 ns/op。微基准可被内联/常量折叠，不代表业务性能等价，记录见 `tests/research/match_lowering/call-signatures-2026-10-03.txt`。原生 receiver 顺序研究属于机制探针，未替代真实 hgo 执行证据。

仍明确未接通：多结果 `return match`；完全无可见类型拼写的私有调用参数（如 `m.Hidden()(match ...)`）；既有 unary/composite/block-arm 等非本组表达式上下文。非法 arity、常量溢出、不可寻址 pointer receiver 和权限擦除属于应拒绝输入，不算功能欠账。带编译器指令的本地函数保持既有明确诊断，不擅改调用的逃逸/活性约束。blank/embedded 字段、公开 enum 入口验证、serde adapters Proposed 均未在此决定。

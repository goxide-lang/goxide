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

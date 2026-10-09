# 卫生宏：声明宏与内建派生

优先级（D21）：宏为 T0；这是用户的产品优先判断，不是已经验证所有宏都无成本。有限声明宏/具体类型派生可分批闭环；卫生、求值次数、诊断与工具链验收保留，不等待完整泛型 trait 或风险分析。

交付边界（D20）：本文涉及的别名/借用风险 warning 是未来独立长期分析，不属于 MVP 验收；当前语言语法、类型、明确操作与真实 Go 行为照常设计。不得为未实现分析器强制复杂效果注解。

状态：首版参考 Rust 的模式匹配声明宏，提供 `print!`、`println!` 等基础入口，以及 `#[derive(Default, Clone, Debug)]`。声明宏使用模式、模板与重复展开；派生和格式字符串检查由编译器内建实现，不执行用户编写的 AST 变换程序。用户过程宏后续再做。具体签名、包路径和展开协议仍为草案；实现分支已有有限声明宏及具体类型派生，见[准确边界](../../IMPLEMENTATION.md)；本篇完整协议及 linter 尚未全部实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D14：参考 Rust 的声明宏、print / println、内建 Default / Clone / Debug；用户过程宏延期。 |
| 暂定 | T10–T12：定义点卫生（区别于 Rust mixed-site）、匹配与展开规则、格式子集 / I/O panic、各 derive 的精确协议和静态降低。 |
| 未定 | U04、U09：静态 Self 与泛型后端、完整宏语法、formatter、上下文 / 缓存协议。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 goxide 功能已实现。

## 1. 分类与首版范围

后续独立会话的实现授权：在文档基线之后交付可运行的模式宏和内建派生原型，场景为 log/tracing 风格结构化日志、常见模式宏以及 Debug/Clone/serde/thiserror。JSON 是推荐首版序列化格式，不等于 Rust serde 全协议；thiserror 生成显式 From，问号的目标已知单步转换见 [RFC 0005](../rfcs/research/0005-option-result-question.md)。原型支持语法、已实现范围与失败诊断须单独登记；未完成整套 trait、引用和 enum 前端时，不把 Go helper 生成称为这些语言功能已完成。禁止以宏授权恢复 MVP 借用分析。本标签仅包含文档和机制证据，未包含宏实现。

标签后用户进一步确定 **serde 优先并仿照 serde-rs**：核心 Serialize/Deserialize、Serializer/Deserializer、Visitor/Access 连接类型与格式，Value 属于 serde-json 等格式层，不设公共通用核心树。[RFC 0006](../rfcs/research/0006-serde-core-and-formats.md)已按官方实现修订，撤回此前 core Value 与强制树中转候选。derive 不直接调用 encoding/json，不把 Debug 输出当序列化格式，不自动授予字段写权限；具体 Go/goxide ABI、rename/default/skip 和泛型 codec 子集仍需实施细化。新增内建派生不开放用户过程宏。

“如何调用”“如何实现”“是否有副作用”是三个不同维度：

| 维度 | 形式 | 含义 |
| --- | --- | --- |
| 调用形状 | function-like，例如 `m!(...)` | 在表达式、语句或声明位置调用宏 |
| 调用形状 | derive，例如 `#[derive(...)]` | 为目标类型请求额外实现 |
| 调用形状 | attribute | 对被标注项提供其他变换入口；开放的自定义 attribute 延期 |
| 实现方式 | 声明式 | 使用模式捕获、模板替换和规定的重复规则 |
| 实现方式 | 过程式 | 执行用户编写的程序来检查、构造或改写语法树 |
| 效果 | 纯或有副作用 | 是否读取外部状态或执行 I/O，不决定它是不是过程宏 |

一个用户编写的 `Syntax -> Syntax` 函数，若由编译器作为宏展开器执行，即使确定性、无 I/O，也仍是受限过程宏；不能仅以“纯函数”为名将它算作声明宏。普通离线工具运行同样的 AST 变换不因此成为过程宏。function-like 调用也不天然意味着声明式；本文首版明确选择声明式实现。Rust 的过程宏同样可以采用 function-like、derive 或 attribute 调用形式，说明调用形状与实现方式不能混为一谈。[Rust 过程宏分类](https://doc.rust-lang.org/reference/procedural-macros.html)

首版提供：

- 带语法类别的模式捕获、模板插入、规定的重复展开。
- 基于符号与 scope 身份的卫生，以及展开来源映射。
- `print!`、`println!` 声明宏及其内建格式检查支持。
- 编译器登记的 `Default`、`Clone`、`Debug` derive 规则，产生普通显式 impl。

在编译期宏扩展中执行用户 AST 程序、用户可编程 derive、任意宿主 Go 代码或插件、开放 I/O 与自由改写声明的自定义 attribute 均延期。内建 derive 本身当然由编译器程序实现；首版不把这一实现能力开放为用户过程宏接口。此限制不禁止普通程序或离线工具使用语法分析和生成库。

宏是 goxide 转译前端的一个阶段。展开结果接受正常名字解析、类型检查和转译前 linter，再降低为 Go 1.27 源码。不引入另一个运行时，也不改变[总纲](overview.md)中的 Go 支持库与自举方向。

这里参考 Rust 的是宏入口和派生机制，不是复制 Rust 标准库或引入全局 move / `Copy` 体系。goxide 基础包针对现有 **Go std API** 提供 safe 声明或包装，并补充泛型 slice，见[基础包与 safe Go 标准库](standard-library.md)。`quote` / `syn` 风格工具属于额外语法工具包，是否需要过程宏取决于调用方式，见[语法工具与过程宏边界](syntax-tooling.md)。

## 2. 模式与模板操作结构化语法

模式中的 `expr`、`type`、`ident` 等捕获类别按 goxide 语法解析。模板捕获值是保留来源与上下文的结构化语法片段，不是拼接后重新猜测绑定关系的字符串。

首版语法类别的精确名单待解析器设计；不同类别不能任意互换。将一个声明列表插入算术表达式位置应报错，不能借重新解析文本隐藏错误。

每个语法片段至少携带：

- 节点或 token tree 的结构及语法类别。
- 标识符的绑定身份或待解析名字，以及 scope / syntax context。
- 源码来源、当前展开实例和展开调用栈。
- 检查上下文，包括 unsafe 授权来源。

声明宏只使用语言规定的匹配和模板操作。宏作者不能运行自定义分支、字段遍历函数或任意 AST 构造程序；需要这类能力的用户扩展属于后续过程宏。内建 derive 读取类型形状是单独的编译器能力，不为普通声明宏提供反射后门。

编译器按显式依赖读取源文件与包元数据，但用户宏规则不能自行读取文件、环境变量、时钟或网络，也不能启动进程。这个限制不禁止宏生成运行时 I/O 代码：模板可以产生普通的日志、文件或网络调用，只要展开结果满足相应类型、依赖和 unsafe 规则。

## 3. 函数式声明宏示例

当前源码包采用 `pub macro_rules! name` 导出、`use root as alias;` 绑定、`alias::name!(...)` 调用。根及精确 Go 包映射由[依赖配置](dependency-roots.md)声明；宏与运行时项共用静态路径解析。运行时 helper 按 Go 导出约定写 `Emit` 等公开函数；私有 helper 跨包不可见，须由作者提供公开 helper。旧包点号调用与 `import macro` 不再支持；下文 `pub func` 等完整协议仍是草案。限定实现与来源/预算边界见[宏实现说明](../../internal/macros/README.md)。

以下使用类似 `macro_rules` 的草案形式，不是当前可运行的 goxide 程序。假设 `macrokit` 包提供一个普通、公开的运行时 helper：

```goxide
pub func twice(value int) int {
    return value + value
}

pub macro_rules! twice_plus_one {
    ($x:expr) => {
        func() int {
            saved := 1
            value := $x
            return twice(value) + saved
        }()
    };
}
```

`$x:expr` 捕获一个表达式，模板中的 `$x` 插入该表达式的结构化语法。展开器不会在编译期执行 `$x` 或运行时函数 `twice`。模板中的立即调用函数是显式生成的普通代码，不依赖另一套尚未定义的块表达式规则。这个宏专门用于 `int`，实参类型在展开后的普通检查中确定。

调用形状同样为草案：

```goxide
use macrokit as m; // dependency root maps to example.org/goxide/macrokit

saved := 41
twice := func(value int) int { return -1000 }
calls := 0
next := func() int {
    calls += 1
    return 1
}

result := m::twice_plus_one!(saved + next())
```

预期 `result == 85`、`calls == 1`，调用方的 `saved` 仍为 41。实参中的 `saved` 不被模板生成的同名局部变量捕获；模板中的自由名字 `twice` 指向定义包的 helper，不被调用者的局部 `twice` 替换。

这个示例只求值一次，是因为模板明确生成了 `value := $x` 这个局部绑定，并复用 `value`。**卫生本身不保证求值次数。** 模板如果写 `$x + $x`，就会执行实参两次；丢弃实参则可能不执行它。求值顺序、短路、闭包、`defer` 和 `return` 的行为都由实际生成的普通语法决定。

`use` 显式选择宏项或 namespace。模板使用的运行时 helper 也必须来自宏定义文件已声明的依赖；展开不能临时产生未知包路径。编译器根据绑定后的引用记录所需的普通 Go 依赖。

### 3.1 规则选择与重复

首版建议规则按定义顺序匹配，必须消费完整输入；第一条完整匹配的规则生效，不根据实参类型或当前 impl 集合选择分支。局部匹配歧义应诊断，不依赖无限回溯尝试解决。

重复语法可采用 `$($item:expr),*` 一类草案：模式保存每次捕获的结构，模板按对应层级重复插入。重复体不得在不消费输入的情况下无限匹配；捕获层级、重复长度或分隔符不一致时报错。

这是一组规定的声明式操作，不等于允许宏作者运行任意循环。最终支持的重复形式和歧义规则需要在实现解析器前冻结。

### 3.2 基础声明宏与内建格式检查

首版核心确定为 `print!` 和 `println!`。以下扩展分批提供，不以完整复制 Rust 宏清单为首版验收条件：

| 入口 | 范围与职责 |
| --- | --- |
| `print!`、`println!` | 首版核心；模式匹配参数形状，调用内建格式检查并在运行时写 stdout |
| 内部 `format_args!` 类入口 | 首版编译器支持；解析格式字面量、生成格式化计划和类型约束，不开放用户过程代码 |
| `format!`、`eprint!`、`eprintln!` | 后续优先补充；复用同一格式协议，分别返回字符串或写 stderr |
| `assert!`、`matches!` | 后续基础宏；分别需要冻结失败行为、条件与消息求值规则，以及已有 match 的模式语义 |

Rust 的 `print!` / `println!` 源码是声明宏外层，格式处理转交 `format_args!` 一类入口；`format_args!` 的实现被标记为编译器内建。这说明“声明宏入口”与“有编译器支持”可以同时成立，不能把完整格式检查伪装成普通模式替换。[Rust 打印宏源码](https://doc.rust-lang.org/src/std/macros.rs.html)、[Rust 内建格式入口](https://doc.rust-lang.org/src/core/macros/mod.rs.html)

goxide 也采用这层分工：声明宏处理空参数和普通参数列表；内建入口只产生格式化专用 IR 和待验证的 trait 约束。普通类型检查阶段验证这些约束，然后降低为已选择 formatter 的普通调用。格式化计划不授予任意 AST 反射，也不改变第 3.1 节的规则选择方式。

首版建议固定以下格式子集，扩展语法另行增加：

- 第一个参数必须是格式字符串字面量；`{}` 按顺序请求 `Display`，`{:?}` 请求 `Debug`，`{{` / `}}` 表示字面花括号。
- 编译期检查花括号、占位符数量、实参数量及对应协议；不匹配、未使用参数或未实现 trait 均报错。内建标量也由基础包提供明确的格式化协议，不隐式把所有 Go 值变成可格式化值。
- 首版不支持运行时格式串、隐式捕获同名变量、位置重排、宽度精度参数或任意 Go `%v` 透传；如需动态模板，使用另有明确信任和错误契约的普通函数。
- 每个显式实参按从左到右顺序恰好求值一次，全部求值完后才按占位符顺序调用 formatter。临时量保留源级类型与来源；复制引用默认允许，先后参数存在具体冲突链时 warning，真实只读写入等类型错误仍拒绝。

`{}` / `{:?}` 的协议分工参考 Rust 的格式入口；具体 goxide 格式子集与求值顺序由本设计规定，不代表兼容全部 Rust 格式语法。[Rust format_args](https://doc.rust-lang.org/std/macro.format_args.html)

`Display` 和 `Debug` 是显式 trait 协议；这里的 safe 不承诺静态别名或并发安全。编译器不能用 Go `fmt.Stringer`、`error.Error()`、反射或 `any` 兜底调用未知方法；原生类型须有经过检查的显式适配。标量可降低为 `strconv` 等具体函数，复合值调用选定的实现。格式化实参可以在合法范围内被临时只读借用；格式化计划、formatter 和借用的留存不默认拒绝；有明确协议离开或冲突链时 warning。

打印发生在**生成程序运行时**。宏展开时不执行参数、formatter 或 stdout I/O。首版采用以下运行时契约草案：

- `print!("text")` 不追加换行；`print!("")` 不产生输出。`print!()` 缺少格式字面量，报错。
- `println!()` 与 `println!("")` 都输出一个 LF（`\n`）；非空形式在内容后再追加一个 LF，即内容已有换行时仍会追加。
- 正常完成返回 Unit；写入错误或短写转为带原始原因的 panic，不静默丢弃。错误前可能已有部分输出，不承诺回滚、自动重试或跨进程输出原子性。
- 调用者需要恢复和处理部分写入时，使用 [safe Go std 的普通 I/O 入口](standard-library.md)，保留写入计数和错误。缓冲与 flush 按所选输出适配契约处理，不因为使用 `println!` 就承诺持久化。

以上 I/O 错误策略参考 Rust 打印宏的失败行为；goxide 的具体支持库仍以 Go std 写入接口和明确包装实现，不引入 Rust 标准库运行时。

### 3.3 实现结果回流：限定打印协议

实现基准 `47af69f` 已交付 `print!` / `println!`：第 3.2 节的固定占位符、参数先按序各求值一次后格式化、空输出/LF 与写错/短写保留 cause 的 panic 契约均有真实运行证据。后端使用具体 typed formatter，不以 Stringer/error/反射/any 兜底；同包可信 Debug 派生可用于 `{:?}`，不自动成为 Display。

当前入口只接受直接语句位置，支持宏模板、显式参数闭包内的嵌套打印和参数中的 `?`；不支持 go/defer、值位置、initializer/post、导入 Debug 类型或用户格式协议。此范围不修改第 3.2 节的长期协议，也不声称完整 trait/格式生态。编译期仅使用可信来源，生成 I/O 代码仍可 unmapped，未实现运行时 panic 的 goxide 源码回映射。实现和独立生成模块 race 证据见 [实现记录](../../IMPLEMENTATION.md#print--default-最终验收)。

## 4. 卫生是符号身份，不是随机改名

Rust 的 `macro_rules!` 使用 mixed-site hygiene：局部变量和标签按定义点查找，其他符号通常按调用点查找；`$crate` 可明确指向定义包。goxide 本文选择模板自由名字按定义点绑定，是有意采用的更统一规则，不声称照搬 Rust 的名字解析结果。[Rust 宏卫生规则](https://doc.rust-lang.org/reference/macros-by-example.html#hygiene)

标识符解析依赖 SymbolID、ScopeID 和展开上下文。正式内部结构待前端设计，但必须保留以下区别：

| 名字来源 | 解析规则 |
| --- | --- |
| 模板新建的局部绑定 | 每次展开得到新的绑定身份；模板内引用连接到该身份 |
| 模式捕获后插入的调用者语法 | 保留调用者 scope 和已绑定符号，不被模板局部变量重新捕获 |
| 模板中的自由标识符 | 在宏定义点解析，保留对应包和符号身份 |
| 内建 derive 的目标类型引用 | 使用目标 SymbolID，不按显示字符串重新查找 |
| 公开 item、trait 成员、字段选择器 | 按声明协议使用有意公开的名称或成员身份，不能全部 gensym |

第 3 节的两个 `saved` 是不同符号。Go 后端需要文本名字时，在绑定完成后为不同身份分配无碰撞的合法名称。临时变量后缀只是输出形式；随机字符串、时间戳或全进程计数不能充当卫生机制。

公开 API 名称是受控的有意接口。例如派生的 `Clone.clone` 必须对应该 trait 成员，而不是随机新方法。模板需要向调用方提供一个公开普通函数时，应由明确捕获的声明标识符或固定协议给出名称，并按目标包的正常规则检查冲突。首版不提供任意取消卫生、捕获调用者局部变量的通用操作。

宏展开身份、局部绑定身份和 Go 导出 ABI 名称分别管理。宏规则不自行拼接 marker 或 AsXXX 名称；这些仍由 [trait 降低](traits.md)与稳定符号规则决定。

### 4.1 定义点绑定不授予私有访问权

模板在定义点能找到私有 helper，不表示展开到另一个包后就能访问它。首版禁止跨包展开结果直接调用宏定义包的私有运行时成员；错误同时指向模板引用和宏调用。

宏作者可以提供普通公开 helper 或显式公开 shim。shim 在其定义包内访问私有实现，调用方只使用正常公开入口。展开器不自动突破 Go 的导出规则，也不赋予调用方额外权限。

宏包内部使用私有声明宏时，仍按显式登记的编译期依赖展开；它不能借此在最终代码中留下非法的私有运行时引用。

## 5. 内建 Default、Clone 与 Debug 派生

首版提供 `Default`、`Clone`、`Debug`，由编译器登记固定派生算法。它们也是可手写 impl 的普通显式 trait；derive 只是生成这些实现的入口。`Display` 是格式化协议，但没有统一可推导的业务展示形式，首版不提供 `derive(Display)`。后续加入其他可派生 trait 时逐个确定规则，不以“等”字隐含所有 trait 都可自动派生。

```goxide
#[derive(Default, Clone, Debug)]
pub struct Point {
    x int
    y int
}
```

上例要求 `int` 有明确的三个基础实现。`Point` 的默认构造逐字段调用 `Default`，复制逐字段调用 `Clone`，调试输出按字段顺序调用 `Debug`。生成代码属于目标类型所在包，保留派生来源；没有借宏产生一个 `*Point` safe 接收者类型。方法的借用权限使用源语言的 `&Self` 等规则，不能从后端 Go 指针形状推断。

### 5.1 类型形状、约束与冲突

derive 入口按规范 trait 身份选择内建规则，不能仅凭同名字符串选择，也不能将未知名字当作用户过程宏执行。规则读取有限 TypeShape：目标类型身份、类型参数与声明约束、源码顺序的字段或变体、字段类型引用，以及固定的 derive 辅助属性。

规则不先查询最终方法集、借用结果、Go 内存布局或逃逸分析；它为实际需要的字段协议生成约束，交由普通 impl 求解检查。例如泛型字段要求 `FieldType: Clone`，不因为某个类型参数出现过就凭空要求它满足无关协议。字段类型只作为不透明句柄时，使用该句柄自己的显式实现，不递归查看其私有字段。

当前包声明中的私有字段可以参与派生，因为实现生成在该包内；不能借 derive 为外部类型窥探私有结构。缺失字段协议、同键重复 impl 或冲突属性正常报错；内建规则不因发现已有实现就静默跳过或覆盖。希望自定义某一项时，从 derive 列表删除该项并手写 impl。

派生首版只输出 impl 和明确的普通辅助函数，不删除或改写字段，不产生新的宏定义、import 或后续 derive 请求。辅助属性只在内建规则规定的位置有效，不是开放的用户 attribute 宏。关联类型若参与具体协议，仍按[关联类型规则](associated-types.md)检查。

### 5.2 Default：显式构造合法值

`Default` 的语义是静态构造操作 `default() -> Self`，没有实例接收者。struct 派生依声明顺序调用每个字段类型的 `Default`；字段没有实现时拒绝。空 struct 可以直接构造。手写实现可以选择业务默认值，不要求它等于底层 Go 零值。这一逐字段派生思路参考 [Rust Default](https://doc.rust-lang.org/std/default/trait.Default.html)。

普通 enum 的自动派生要求恰好一个变体标注 `#[default]`，该变体可以有 payload，例如：

```goxide
#[derive(Default)]
pub enum Mode {
    #[default]
    Named(string),
    Idle,
}
```

上例默认构造 `Named`，其字符串字段使用 string 的 Default。没有标注、多处标注或非法属性位置都拒绝。默认变体的 payload 按声明顺序逐字段调用 Default，缺少协议时拒绝；非默认变体的 payload 不要求 Default。`#[default]` 是内建 derive 的固定辅助属性，不执行用户程序。

`Option[T]` 可以由基础包提供返回 `None` 的显式实现，不必要求 `T: Default`。`Result[T, E]` **不自动获得 Default**，不猜测 `Ok(default)` 或 `Err(default)`。即使用户手写返回合法变体的 Default，也只有显式调用它才执行构造；原生 Go 输入的 nil 仍不是合法 Result。

`Arc[T]`、借用和其他非空句柄也不因后端使用 Go 指针就自动得到 Default。构造 Arc 必须由明确实现创建或取得合法对象；`&T` / `&mut T` 不能凭默认构造制造不存在的有效借用。特别是 nil 不可冒充这些类型的合法默认值。

具有 `Default` 不等于允许任意零初始化，也不自动改写 Go 的 `var`、`make`、map 缺失读取或已关闭 channel 接收。需要默认元素的容器 API 必须明确逐项调用选定的构造；如构造返回共享句柄，每次结果是否共享由该实现决定。泛型构造不得用 `var value T` 冒充调用 `T` 的 Default。严格枚举初始化和 Go 边界继续遵守[枚举规则](enums-and-match.md)。

#### 5.2.1 实现结果回流：具体 Default 与 Go 零值

历史实现基准 `47af69f` 采用具体 helper `DefaultType() Type`，支持基础标量字段、同包已派生字段、空 struct、唯一指定无 payload 变体和具体 Option None。该选择沿第 5.5 节普通 Go 函数降低方向，不冻结通用静态 trait ABI。宏按显式 derive 登记 helper 身份，调用方局部同名函数不截获。

该历史版本拒绝带 payload 的默认标记；当前规则已允许它，并只检查选中字段。缺失/重复默认标记、未登记协议、Result/引用/容器/外部类型自动默认仍明确拒绝。Option 的 None 在当前 ABI 为 nil，不代表其他 enum 的 nil 合法。Go var/make/map 缺失读取保持原零值。历史原型的 `#[serde(default)]` 曾保留缺失字段 Go 零值；当前字段 serde(default/skip) 已显式改为调用登记的 Default，属于兼容变化，见 [serde 字段规则](../rfcs/research/0006-serde-core-and-formats.md#71-字段-default-接入与兼容变化)。真实正反例见 [独立验收](../../tests/acceptance/print_default_test.go)。

当前手动协议入口为 `impl Default for T { func default() T { ... } }`，限同包本地非泛型 struct；函数零参数并返回目标具体类型，嵌套 derive 调用登记实现。按包与类型身份检查唯一性，手动与 derive 冲突、错误签名及不受支持的递归协议依赖均拒绝。同名普通 Default helper 不取得协议身份，跨包消费与完整 trait 后端仍后置。enum 非默认变体的 payload 不要求 Default。

字段级 serde(default/skip) 的解码沿用该登记：default 仅缺失时调用，skip 无论输入是否存在均默认构造一次；本层读取及 required 检查成功后按字段声明顺序调用。缺协议拒绝，不悄悄用 Go 零值替代；Serialize-only 无此解码要求。原生指针可解码 null，但没有 Default，故不能作为解码 default/skip 字段。嵌套默认构造的副作用不回滚。

内建具体 Option 的 serde 缺失/null 为 None；`serde(default/skip)` 使用 Option 自身 None 默认，不要求或调用载荷的 Default。未 skip 字段仍要求载荷具有对应方向的 serde codec，显式错误值不默认；skip 仅验证输入格式和预算，不按目标类型解码。

### 5.3 Clone：字段协议决定复制内容

`Clone` 的语义是从合法只读借用构造同类型结果 `clone(&Self) -> Self`。struct 派生依字段声明顺序调用各字段的显式 `Clone`；enum 保持当前合法变体，对其 payload 调用相应协议。缺失任一字段协议时拒绝，不回退成普通 Go 赋值。这个逐字段调用思路参考 [Rust Clone](https://doc.rust-lang.org/std/clone/trait.Clone.html)。

goxide 的普通赋值仍遵循既有 Go 值复制方向；不会因为新增 `Clone` 就引入全局 move 或要求所有赋值实现 `Copy`。显式 `Clone` 也不承诺深复制整个可达对象图，协议可以有成本或运行时副作用：

| 字段 | Clone 边界 |
| --- | --- |
| 有显式实现的标量、字符串 | 产生同值结果；可降低为普通复制 |
| `Arc[T]` | 复制共享句柄，指向同一对象；不隐含 `T: Clone`，不增加可变权限 |
| `Lock[T]` / `RWLock[T]` | 复制指向同一锁与受保护状态的句柄；禁止复制内部 mutex 对象 |
| `&T` | 若提供显式实现，可复制只读引用；不产生所有权证明，有具体协议离开链则 warning |
| `&mut T` | 旧“因独占一律无 Clone”已撤回；是否提供基础 impl 待定，缺 impl 按普通 trait 错误处理，不以别名本身拒绝复制 |
| slice、map 或自定义容器 | 必须有明确的实现，说明存储与元素如何复制、哪些别名仍共享；不是默认逐字节复制 |

原生 Go `sync.Mutex` / `sync.RWMutex` 没有自动 Clone；派生不能因字段具有 Go 值类型就按值复制锁。表中的 Lock 是 goxide 支持库的指针句柄，与包含原生 mutex 的值结构分开处理。

Go `slices.Clone` 用赋值复制元素，属于浅复制；这不能自动等价于 goxide 的逐元素 Clone。基础包中的浅复制入口、显式元素回调和未来容器 Clone impl 分别定义，见[泛型 slice](standard-library.md)。[Go slices.Clone](https://pkg.go.dev/slices#Clone)

Clone 结果仍要保留字段的引用来源。复制 slice header、map、Arc 或锁句柄不洗掉外部别名，锁回调引用形成长寿命结果默认允许，未来有具体违约使用才 W01。手写 Clone 同样接受基于证据的诊断；若想从借用制作独立快照，必须实际复制所需内容并证明结果不依赖该借用，不能只改返回类型。

开放 raw/C enum 已按 D27 撤回。serde 数值/string 映射不改变封闭变体的 Clone/Debug 身份，见[枚举专题](enums-and-match.md#01-封闭-enum-与外部映射)。

### 5.4 Debug：受检查的运行时格式化

Debug 按声明顺序格式化 struct 字段；enum 输出当前变体及其 payload。所有被格式化字段必须有显式 Debug 实现。生成的名称和字段布局便于调试，但不承诺跨版本稳定，不用作序列化、协议编码或持久化格式。这一用途边界参考 [Rust Debug](https://doc.rust-lang.org/std/fmt/trait.Debug.html#stability)。

格式化使用临时只读借用和受限制的输出器。实现留存参数借用、格式化计划或输出器时按具体协议风险 warning，不作普遍禁止；直接违反源类型权限仍 error。派生不能为了打印锁内内容自动制造解引用权限或未经定义地加锁；锁句柄的 Debug 行为由基础包自己的显式实现规定，缺失时拒绝派生。

Debug 不自动隐藏私有字段中的业务内容；需要控制输出的类型应手写该实现。它不通过 Go 反射或任意 `String()` 方法兜底，也不把 `{}` 当成 `{:?}` 的别名。具体 formatter 签名和错误载体与基础包一起冻结；打印写入失败的处理见第 3.2 节。

### 5.5 静态 trait 操作与 Go 运行时接口

`Default.default() -> Self` 与 `Clone.clone(&Self) -> Self` 不能强行装入只规定固定业务签名和 marker 的旧 Go 接口模型。前者没有接收者，后者结果依赖具体 Self；不能改成返回 `any` 或 wrapper 后宣称保持类型语义。impl 成员统一用 `func`，首参数 self 标识实例成员，其余是关联函数，不需要 static；接收类型及权限拼写仍待收敛。以上签名描述操作语义，当前手动 Default 要求具体返回类型。固有类型关联函数的 `T::method` 调用及包级函数降低已确认，已确认声明规则见 [RFC 0020](../rfcs/0020-associated-functions.md)；不因此自动建立 Default impl。

首版对这些操作按规范 impl 身份在编译期选择实现，对具体类型生成普通 Go 函数，例如 `defaultPoint() Point`、`clonePoint(value *Point) Point`；后者的 `*Point` 仅为只读借用的后端表示。impl 唯一性、包可见性和借用检查仍由同一套 trait 元数据负责，不另建结构式自动采用关系。

未具体化的泛型调用可以考虑传入类型化函数字典，或由后端专门化；这是[关联类型与泛型降低](associated-types.md)中尚待定稿的通用实现选择，不在宏文档中冻结新 ABI。无论选择哪种，证据必须携带选定的实现及源级约束，不能在运行时扫描 Go 方法集或抹成 `any`。尚未支持的泛型形式应有明确诊断。

只有满足既有运行时 trait 视图条件的方法，才继续采用[trait 文档](traits.md)的 wrapper、业务方法和 marker；不因此承诺可声明未知 Self 的运行时 `Default` 或 `Clone` 值。Debug 也须在 formatter 签名和借用有效范围确定后再决定运行时视图形状。derive 是这些普通实现的生成来源，不是绕开限制的另一种分派。

## 6. 暂定的展开阶段

首版采用以下阶段草案，避免宏等待完整类型、完整类型又等待宏输出的循环：

1. 解析源文件和显式依赖，登记源码中的宏规则、宏导入及调用位置。编译期依赖循环报错，不靠运行插件解决循环。
2. 展开普通声明宏。单个语法树按源码顺序、由外到内处理；外层规则接收未展开的捕获语法，再递归处理输出中保留或生成的宏调用。输出不能新增宏定义或 import，因此宏名字空间不会在展开时任意变化。内建格式入口此时解析字面量并产生格式计划及待检查约束，不执行实参或 formatter。
3. 收集普通展开完成后的类型声明头与 derive 请求，解析必要的包和类型身份，形成固定 TypeShape；不要求方法体检查或完整 trait 求解。按目标源码顺序及 derive 列表顺序执行内建规则。
4. 对全部展开结果执行正常名字解析、类型检查和转译前 linter，包括派生 impl 的字段约束、格式占位符所需的 Display / Debug 和静态 Self 调用。完成后产生已解析语义 IR，再降低为 Go。

声明宏模板可以生成带固定 derive 入口的类型声明，这些请求在第 3 步统一收集。内建 derive 输出不得回头改变其他请求的 TypeShape 或再次触发 derive。

规则选择只依赖显式输入。不同平台或构建配置影响声明选择时，其差异先反映到输入和 TypeShape 中；展开器不提供读取环境或遍历磁盘来临时改变结果的宏能力。

展开成功只表示得到合法类别的语法，不保证程序类型正确。缺少 impl、重复定义、私有访问与真实类型权限错误仍可在后续阶段报错；别名风险默认 warning。支持库和前端内部的派生算法也不能跳过这些检查。

## 7. unsafe 与转译前检查

展开节点保留定义来源和调用来源，随后使用[原生 API 与裸指针](foreign-api.md)中的普通检查规则。宏不能免除生成代码的 unsafe、引用、锁回调或 trait 约束，也不能在展开时执行模板中的原生调用。

尤其不能由模板生成的 unsafe 替捕获参数里的代码授权。例如下列模板片段：

```goxide
unsafe raw::Consume($x)
```

模板作者引入的 `Consume` 可以按模板中的显式 unsafe 检查，但 `$x` 内调用者写下的操作保留调用者原有授权上下文。若实参为明确声明 unsafe 且未获授权的 `raw::Other()`，展开后仍应报错。调用点显式写出的 unsafe 可按普通求值规则授权其直接表达式，但不自动授权新建函数或闭包体。

实现不能只在展开文本中寻找最近的 unsafe 祖先节点；必须使用节点来源和授权范围。重复插入、再经另一宏转发或内建 derive 包装，都不能把调用者节点重新标成定义者代码，洗掉检查要求。

宏作者明确生成的 unsafe 操作承担与普通库作者相同的正确性责任；这不授权宏自动为用户代码新增可信外部契约。首版宏和派生结果不得生成外部 API 的 declare 契约声明，这些声明仍由显式源码或声明包提供。

借用派生的 Ptr 经宏包装后仍保留已知来源；不能因展开或 unsafe 表达式结束就认为外部别名消失。linter 的边界沿用总纲，不借卫生宏宣称完整跨包所有权证明。

## 8. Go 降低与双向互操作

### 8.1 卫生展开

Go 接收展开、检查后的普通代码，没有运行时宏对象或宏解释器。规则与捕获语法只存在于编译期。第 3 节可以降低为以下完整、可独立执行的 Go 示意：

```go
package main

import "fmt"

// 为独立验证而放在同一文件；实际跨包输出引用宏包的公开 helper。
func macroRuntimeTwice(value int) int {
    return value + value
}

func main() {
    saved := 41
    twice := func(value int) int { return -1000 }
    _ = twice
    calls := 0
    next := func() int {
        calls++
        return 1
    }

    result := func() int {
        goxideM1Saved := 1
        goxideM1Value := saved + next()
        return macroRuntimeTwice(goxideM1Value) + goxideM1Saved
    }()

    fmt.Println(result, calls, saved)
}
```

本轮已从本文提取示例，在 Go 1.27.1 的独立临时模块中执行，实际输出为 `85 1 41`。`goxideM1Saved` 等名字仅展示绑定后如何避开冲突，卫生检查发生在分配这些文本名字之前。运行这个 Go 示例只能验证特定展开代码的行为，不能证明尚不存在的 goxide 展开器已经正确。

### 8.2 Default 与 Clone 的静态调用

下面是另一段完整 Go 示意。`Record` 的派生结果调用三个字段各自选定的普通函数：Label 的默认值是非空字符串，示例分数列表的复制创建独立存储，共享计数器的复制只返回同一锁句柄。列表 helper 仅代表本例明确选定的字段实现，不承诺基础包已经确定泛型 Slice 的 Clone 策略。

```go
package main

import (
    "fmt"
    "sync"
)

type Label string

func defaultLabel() Label { return "untitled" }
func cloneLabel(value Label) Label { return value }

type Counter struct {
    mu sync.Mutex
    value int
}

func defaultCounter() *Counter { return &Counter{} }
func cloneCounter(value *Counter) *Counter { return value }

func defaultScores() []int { return []int{} }
func cloneScores(value []int) []int {
    result := make([]int, len(value))
    for i, item := range value {
        result[i] = item // 本例 int 的显式 Clone 可降低为同值复制。
    }
    return result
}

type Record struct {
    Title Label
    Scores []int
    Shared *Counter
}

func defaultRecord() Record {
    return Record{defaultLabel(), defaultScores(), defaultCounter()}
}

func cloneRecord(value *Record) Record {
    return Record{
        cloneLabel(value.Title),
        cloneScores(value.Scores),
        cloneCounter(value.Shared),
    }
}

func main() {
    original := defaultRecord()
    original.Scores = []int{1, 2}
    cloned := cloneRecord(&original)
    cloned.Scores[0] = 9
    cloned.Shared.mu.Lock()
    cloned.Shared.value = 7
    cloned.Shared.mu.Unlock()
    original.Shared.mu.Lock()
    sharedValue := original.Shared.value
    original.Shared.mu.Unlock()
    // fmt 仅用于观察这个独立示例，不代表 print! 的通用 lowering。
    fmt.Println(original.Title, original.Scores[0], cloned.Scores[0],
        original.Shared == cloned.Shared, sharedValue)
}
```

本轮从本文提取示例，在 Go 1.27.1 独立临时模块中执行，实际输出为 `untitled 1 9 true 7`：默认值由显式构造得到；分数列表的写入不影响原列表，锁句柄仍共享状态。此例只验证这些已选择的 Go 函数，不验证通用派生、借用诊断或泛型函数字典。生成函数的正式名字仍需按稳定 impl 身份编码。

### 8.3 Go 接口边界

内建 derive 复用正常 trait 检查；静态操作按第 5.5 节生成函数，满足运行时 trait 视图要求的方法才生成对应 wrapper、业务方法和 marker。

goxide 调用原生 Go 时，宏可生成已经显式依赖的原生调用，但仍使用选中的 declare 视图或 unsafe 规则，不能因宏包受信就把调用方所有 Go API 提升为 safe。

Go 源码不能直接调用 goxide 宏，但可以使用宏生成并导出的普通类型、函数、方法和 trait 视图，遵守最终 Go ABI。若生成 API 涉及原生类型的 marker，发布与独立构建视图仍遵循 [Go 适配](go-interop.md)规则；宏不免除相关构建要求。

宏可决定使用方的公开 API，改变生成名称、签名或表示时需要正常兼容性审查，不能仅当作宏包内部实现变化。

## 9. 资源预算、确定性与缓存

没有用户过程代码也不代表展开必然终止。递归调用宏、重复模板或派生数量仍可能造成资源爆炸。每次展开和整个构建需要可配置的匹配工作量、递归深度、输出节点数、展开总量与内存预算。

超限诊断应包含宏调用链、定义位置和超限类别，不能留下部分树继续当成成功。具体默认值在实现测量后决定；墙钟超时与用户取消可以中断工具，但不应缓存为源码必然非法。

缓存至少包含：

- 编译器、宏语法、卫生协议及内建 derive 算法版本。
- 格式语法、内建标量格式协议和 formatter 接口版本。
- 宏规则内容、显式宏依赖和运行时 helper 的符号接口身份。
- 捕获语法结构、卫生上下文与授权来源，或目标 TypeShape 与 derive 参数。
- 影响输入选择的构建配置及相关声明视图版本。

缓存不能把第一次调用的局部绑定身份、调用点 span 或 unsafe 授权原样贴到第二次调用。可缓存带待实例化身份的展开模板，命中时重新建立当前展开的 fresh bindings 与来源映射。

输出不依赖磁盘遍历顺序、map 随机迭代、进程地址、时间戳或并行调度；不要求内部对象地址相同。取消和资源耗尽产生的半成品不得进入成功缓存。

## 10. 诊断、LSP 与自举

每个展开节点同时保留 origin span 与 invocation span：前者指出模板或捕获参数的来源，后者指出用户为什么得到该节点。嵌套展开还要保留调用栈，不能只留下最内层生成文件的位置。

LSP 与构建共享宏依赖、展开器、缓存协议及语义数据库，应支持：

- 从调用跳到规则定义，查看当前展开结果与引入的普通依赖。
- 捕获参数错误定位调用者输入；模板错误同时显示定义点和调用点。
- 将 derive 产生的方法展示在目标类型下，标注内建规则和生成来源。
- 按 SymbolID 重命名，不因为显示名字相同就改到另一卫生作用域。
- 将生成 Go 的错误映射回 goxide 节点，同时保留生成位置用于调试。

展开预览是只读派生结果。修改规则或输入后重算；首版不支持编辑展开文本后反向猜测应修改哪一段模板。

启动实现使用 Go 编写匹配器、卫生设施与内建 derive，再逐步使用 goxide 自举。用户规则不因编译器用 Go 编写就获得宿主执行能力，也不要求先实现通用的编译期函数解释器。自举前后比较规范展开结构、绑定关系和生成程序行为，不只比较临时变量的文本名字。

## 11. 未来实现的验收条件

以下是未来要求，不代表当前已通过 goxide 端到端测试：

1. 第 3 节模板与调用者均使用 saved 时分别绑定；调用者的 twice 不能遮蔽模板定义点的函数。
2. 同一宏展开两次，其局部绑定不互相引用；缓存命中和未命中的绑定关系一致。
3. 特定示例输出 85，只执行一次 next；明确复制实参的模板可以执行两次，不把求值一次归因于卫生。
4. 模式完整匹配、语法类别、重复层级和歧义均有确定诊断，不依赖最终类型选择规则。
5. 模板生成的 unsafe 不能授权捕获参数中未获授权的调用，新函数体也不继承外围授权。
6. 跨包模板引用私有运行时 helper 被拒绝；公开 helper 或显式公开 shim 可用。
7. 打印格式字面量、实参数量、Display / Debug 实现均在编译期检查；实参从左到右各求值一次，临时引用按具体来源风险 warning；不调用未经适配的 Go Stringer 或反射。
8. `print!("")` 无输出，`println!()` 和 `println!("")` 各输出一个 LF；写入错误及短写遵守 panic 策略，不吞掉部分写入事实。
9. Default 派生逐字段构造；enum 接受单个 default 变体，仅其 payload 字段递归要求 Default；Result nil、无约束泛型零值不能借 Default 获得合法性。
10. Clone 派生调用字段协议，缺少显式 impl 仍错误；不能仅因字段含 `&mut` 就套用旧独占规则拒绝；共享 Arc 和锁仍共享状态，独立数据复制不被夸大为整个对象图深复制。Debug 不延长借用，也不绕过锁权限读取内部数据。
11. 内建 derive 只读取有限形状，输出显式 impl；缺失字段协议、重复 impl、关联类型错误正常诊断。静态 Self 操作正确选择函数，不伪装成返回 any 的运行时接口。未知 derive 不能执行宿主程序。
12. 递归或输出爆炸在预算内终止并诊断；声明规则不能读取环境、文件或在宏扩展中执行用户 AST 程序。
13. 输入、规则、依赖、shape、格式协议、算法版本或授权来源变化时正确失效缓存；不同调用不复用旧 scope 和 span。
14. Go 可使用导出的生成 API；纯 Go 构建无需宏展开器，但须具备产物声明的支持库与按需增强构建视图。LSP 与构建给出相同展开和诊断。

本轮仅交付设计文档和独立 Go 降低示意，未实现宏语法、展开器、缓存、内建派生或相关 LSP 能力。


问号传播候选见 [RFC 0005](../rfcs/research/0005-option-result-question.md)：`{:?}` 格式协议与传播运算符无关；展开中的 `?` 服从真实函数/闭包边界，卫生不改变返回目标。

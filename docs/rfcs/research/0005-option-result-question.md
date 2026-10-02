# RFC 0005：Option / Result 专用问号传播

状态：Option/Result 专用传播与下述显式 From 单步错误转换方向已获用户确认；完整语法及前端仍未完成；本实现分支已有下述有限可运行子集。不公开 Residual/Try。thiserror 核心派生生成显式 From，目标 Result 已知时 `?` 可选择一步转换；不搜索转换链或任意泛型 blanket impl。当前具体实现边界见 [IMPLEMENTATION.md](../../../IMPLEMENTATION.md)，不改变本 RFC 已确认方向。

## 1. 当前规范核对与最小建议

此前没有定义 `?`、Try、FromResidual 或 Residual；宏里的 `{:?}` 是 Debug 格式占位，不是传播运算符。已有 match 明确早退仍针对当前函数，并保留求值顺序；已有 Go I/O 规则保留 `(n,error)` 的部分成功。专用 `?` 不与这些规则冲突，不能通过隐藏闭包降低而改变它们。

推荐 MVP：仅识别基础包的名义 Option/Result（不能因为用户类型名字一样就获得权限），直接分支和 return；不公开通用 Try/Residual trait，不增加自动 Option↔Result 转换、try block、Poll/ControlFlow 或用户自定义传播实现。普通泛型可以在已知 `Result[T,E]`/`Option[T]` 上使用，不需要 `R: Try`。

| 操作数 | 当前函数/闭包返回类型 | 成功 | 短路 |
| --- | --- | --- | --- |
| Option[T] | Option[U] | Some(x) 提取 T，继续计算 U | None 返回当前函数的 None |
| Result[T,E] | Result[U,E] | Ok(x) 提取 T | Err(e) 构造目标 Result[U,E] 的 Err，保留 e |
| Result[T,E] | Result[U,F] | 同上 | 相同/普通赋值兼容错误保留；否则要求唯一的显式 From[E] for F，一步转换，无转换链 |
| Option[T] | Result[U,E]（或反向） | 不隐式转换 | 类型错误，要求显式 match 指定 None/Err 的业务含义 |
| 原生 `(T,error)` 多返回 | 任意 | 不直接接受 `native()?` | 先显式调用/包装成 Result，不猜测数据应否丢弃 |

先检查相同类型与普通赋值兼容，再查询明确源/目标类型对应的唯一显式 From；禁止多步搜索和任意泛型 blanket 转换。重复来源映射属于歧义，要求用户显式分类；派生不能猜测。接口装箱后的 typed nil 原样保留，不能归一化为 nil。

### 1.1 thiserror 与静态错误类型

候选拼写 `#[from] Io(IOError)` 表示派生一个明确的 `From[IOError] for AppError`，例如普通 Go helper `AppErrorFromIOError(e IOError) AppError`；目标已知的 `?` 使用该 helper 一次。只生成 helper 并通过 Go 测试不代表 hgo 的 `?` 已实现。错误链通过明确 source/Unwrap 协议保留，不自动把任意 Error 字段当来源。

若输入的静态类型是 Go `error`，编译器不能根据运行时动态类型自动选择 Io/Parse 等变体。可显式声明唯一兜底 `From[error] for AppError`，或由业务分类函数使用 errors.As/Is 再构造具体变体；保留部分成功与 typed nil 语义。两个变体都声明来自同一个 error 类型应诊断冲突。From 不同时把 None 转成 Err，也不要求公共 Try/Residual。

## 2. 完整正向用法与专用 lowering

以下为候选 hgo 语义写法，名字和泛型显式参数不是冻结的 parser 规范：

```hgo
func port(text string) Result[int, error] {
    n, err := strconv::Atoi(text)
    if err != nil { return Result::Err(err) }
    return Result::Ok(n)
}
func next_port(text string) Result[int, error] {
    n := port(text)?
    return Result::Ok(n + 1)
}
func first_name(names []string) Option[string] {
    if len(names) == 0 { return Option::None }
    return Option::Some(names[0])
}
func name_length(names []string) Option[int] {
    name := first_name(names)?
    return Option::Some(len(name))
}
```

Go lowering 在原函数内产生临时量和分支，不能把 `?` 包成提前返回的普通 helper：

```go
func NextPort(text string) Result[int, error] {
    tmp := Port(text) // 恰好一次
    var n int
    switch v := tmp.(type) {
    case Ok[int, error]: n = v.Value
    case Err[int, error]: return Err[int, error]{Error: v.Error}
    default: panic("hgo: invalid Result at ?")
    }
    return Ok[int, error]{Value: n + 1}
}
```

Option 在现有候选 ABI 下 nil 表示 None，Some 提取载荷，其他非法动态值走与 match 相同的边界失败。Result nil 不是 Err(nil)，不能由 `?` 自动变成成功/失败业务变体。成功类型 T 和返回成功类型 U 可不同，错误 case 必须按目标类型重新构造，不能直接返回不同泛型身份的整个操作数。

## 3. 消费 Go API：显式桥接，不普遍套 Result

`strconv.Atoi` 的包装明确选择 err!=nil 时不使用 n，代码短且可审核。可以后续提供普通 `result_from_go(value, err)` 帮助函数或逐 API 包装，但名字/隐式多返回展开未批准；MVP 不需要专门的 Go 多返回捕获宏才能工作。

- **typed-nil error**：`var p *MyError=nil; var err error=p` 时 `err!=nil`；应进入 Err 并保留原 error 接口动态类型，不用 reflect 把它变成成功，也不为判断调用 Error()。Go typed-nil 事实见 [Go FAQ](https://go.dev/doc/faq#nil_error)。
- **多个值**：`host, port, err := net::SplitHostPort(addr)` 后显式构造 `Endpoint{host,port}` 再 Ok；不要求 MVP 自动合成 tuple/payload。若 err 时仍有需保留的数据，选择相应 outcome struct。
- **部分成功**：`n, err := reader.Read(buf)` 可能 n>0 且 err!=nil。普通 Result 二选一会丢 n；继续原生双返回或现有 ReadOutcome，不能自动 `Read(buf)?`。同理关闭/事务错误与业务值如何合并由调用者决定，不由 `?` 猜测。
- **错误映射**：使用现有 match 显式生成 DomainError，再对得到的 `Result[T,DomainError]` 使用 `?`；无需先实现 map_err 方法。若将来提供 map_err，它是普通 API，不等于隐式 From 搜索。

```hgo
func require_name(names []string) Result[string, error] {
    // first_name(names)? 在此是类型错误：None 没有自动对应的 error。
    return match first_name(names) {
        Option::Some(name) => Result::Ok(name),
        Option::None => Result::Err(errors::New("missing name")),
    }
}
```

这里正常调用 Go API 不要求效果摘要或 unsafe。若包装涉及指针权限，则沿既有 D11/U15 类型边界处理；`?` 不提供新的桥接特权。

## 4. 控制流与宏边界：首版必须明确的少量规则

1. 操作数求值一次；失败后不计算同表达式后续操作。`f(first(), load()?, last())` 顺序是 first、load，只有成功才 last/f。短路布尔分支未进入时不求值其内的 `?`。不能把临时量统一提升到整个表达式前面。
2. 目标是词法上最近的函数/闭包，不跨调用者返回。`func() Result[int,error] { return Ok(port(text)?) }` 的 `?` 只退出该闭包；外层是否传播由外层再次 `?` 决定。返回普通 int/void 的闭包里使用 `?` 为返回类型错误。
3. 已注册的 defer 随该目标函数正常 return 执行，未到达的 defer 不注册。`defer f(load()?)` 的实参在注册时计算；失败时还未注册 f。`defer func(){...}()` 内的问号属于这个 defer 闭包，不能退出外层。资源清理错误不自动合并到 Result。
4. 宏展开后沿真实函数边界解析 `?`，保留调用点/展开位置诊断。宏若显式插入闭包，会改变问号目标；卫生不会自动恢复外层返回。`{:?}` 不受影响。
5. 成功继续使用 payload 时维持原有类型/读写资格；`?` 不是拷贝、独占、借用释放或同步点。D20 不要求任何额外风险分析。

宏展开器、函数返回类型、表达式 CFG/源码映射是依赖；专用语法也不是只做字符串替换。try block、跨闭包传播、泛型 Try-bound、多个 residual 家族转换均可延期，不阻塞以上两种常见情况。

## 5. Rust 术语核准与成本对比

Rust 的 `Try` 拆分成功 Output 与短路 Residual，通过 branch 决定继续或返回；Result 的 residual 不只是裸 E，而是保留家族身份的 `Result<Infallible,E>`。`FromResidual` 定义目标返回类型如何接受短路部分及哪些跨类型转换可用。[Try](https://doc.rust-lang.org/std/ops/trait.Try.html)、[FromResidual](https://doc.rust-lang.org/std/ops/trait.FromResidual.html)

单独的 `Residual<O>` trait 则根据 residual 与新的成功类型 O 找回相应 TryType，服务通用算法；它不是用户要写 Option/Result `?` 就必须显式引入的第三种容器。当前所查官方文档将这些自定义 trait API 标为 nightly 实验；不能由此说 Rust 常见 Option/Result 问号本身不稳定。[Residual](https://doc.rust-lang.org/std/ops/trait.Residual.html)、[问号表达式](https://doc.rust-lang.org/reference/expressions/operator-expr.html#the-try-propagation-expression)

| 方案 | 编译器依赖 | 使用收益 / 代价 |
| --- | --- | --- |
| 专用 Option/Result | 两个名义类型识别、分支、当前返回类型、错误赋值/显式映射、CFG lowering | 覆盖常见传播，不需要 trait 字典或用户协议；仍需可靠控制流/诊断，成本低于通用协议但非零 |
| 通用 Try + FromResidual | trait 求解、关联 Output/Residual、构造/branch、一致性及转换选择、泛型 evidence ABI | 统一多种短路家族与 try_fold 等通用算法；与当前 U04 后端未决耦合，复杂度明显更高 |
| 再公开 Residual<O> | 输出重绑定、规范类型恢复及约束推导 | 泛型算法更统一；对首版两种内建容器收益小，可长期再评估 |

前端 IR 可用内部的“提取成功值/早退失败值”节点统一降低，不公开 Rust 同名协议、不占用未来公共 API 承诺。不虚构工期，也不承诺新增专用语法立即可用。

## 6. 验收与证据

Go 机制夹具 [propagation_test.go](../../../tests/design/enum/propagation_test.go)核对 typed-nil 原样保留、恰好一次、失败后跳过后续操作、defer、闭包目标、Option 及部分成功原生 I/O。它没有编译 hgo `?` 或实现错误转换规则。

完整前端的最小验收目标（有限实现证据见下节）：上述两个正例；Option/Result 混用、错误类型不兼容、普通闭包内 `?` 的明确类型诊断；宏插入闭包、短路和参数顺序；非法 Result nil 不被当成 Err。From 正例需验证目标已知、源/目标唯一、转换恰好一次且保留 source/typed nil；负例包括重复来源、仅存在两跳转换、任意 blanket 及从静态 error 猜动态变体。不要求用户自定义 residual、完整映射库或所有原生多返回形式自动适配才能交付首版。

待闭合：显式 From 的最终声明/派生拼写、可见性与冲突诊断；普通赋值兼容优先于 From 的具体选择细则；From 本身失败或 panic 的协议。实现不得为通过用例隐式搜索转换链。宏原型可先生成明确 Go helper 验证转换和错误链，必须另行标出尚未端到端实现的 `?`。


### 6.1 实现分支状态（2026-10-01）

`internal/propagate` 已在真实前端接入内建容器元数据及 Go 类型检查，以原函数内分支实现专用 `?`。有限内建泛型要求显式 `Option[int]::Some(...)` / `Result[int,error]::Ok(...)`；用户泛型函数、通用 Try/Residual、跨家族转换仍未实现。Error 派生只为唯一静态 `error` 字段的 `#[from]` 注册显式一步 helper；普通赋值兼容优先，不做转换链或动态类型搜索。这不是最终通用 From 声明/可见性协议。

[传播示例](../../../examples/prototype/propagation.hgo) 使用真实 `strconv.Atoi` 显式桥接，含 Result/Option、成功类型变化与保留 source 的错误转换；[表达式 match 示例](../../../examples/prototype/match_expression.hgo) 单独验证有限表达式位置。两者均通过 CLI run 和转译产物 `go run -race`，命令与输出记录在实现说明。

[前端接受测试](../../../tests/acceptance/propagate_test.go) 与上面的 Go 机制夹具分开：前者确实编译 `.hgo` `?`，检验顺序、短路、defer、最近闭包、nil Result、typed nil、原生部分成功和错误转换。模块 API 层还覆盖重复/缺失/两跳转换拒绝；这不表示源语言已支持用户声明所有转换。

支持上下文限单 RHS、单项 var、return、调用参数、普通运算/selector、短路和无 initializer 的 if 条件。循环/switch/索引/切片/复合字面量/赋值目标/多 RHS/带 initializer 的 if 内传播拒绝。表达式 match 限显式类型局部初始化或单个非具名返回的完整表达式；无普遍类型推断。宏模板仅支持固定内建类型参数及有限 Go 复合类型，不能捕获任意调用者类型；模板显式闭包照常改变传播目标。后续组合阶段已支持普通 sum 载荷与标量引用参数/局部及 derive 同文件使用；引用作为 sum 载荷仍未支持，权限在传播后完整检查才擦除。完整支持表及源码映射限制以[实现说明](../../../IMPLEMENTATION.md)和[传播阶段说明](../../../internal/propagate/README.md)为准。


### 6.2 T0 收口：带 initializer 的 if

沿 §4 一次求值及最近函数早退契约，现支持 `if n := load()?; condition(n)? { ... } else { ... }`，以及普通 initializer 加传播条件。lowering 使用原函数内普通词法块：initializer 先执行一次，条件临时量随后执行，声明覆盖 then/else/else-if，但不泄漏到后续语句。短路仍跳过未选择操作；已注册 defer 仍在函数返回时执行。没有隐藏闭包或新 From 规则。

[真实 hgo 验收](../../../tests/acceptance/propagate_if_test.go)覆盖成功/失败、条件短路、遮蔽、赋值 initializer、Option、else-if、最近闭包，以及作用域泄漏/跨家族/赋值目标的拒绝（多 RHS 已由 §6.3 接通）；生成模块显式 race。[业务例子](../../../examples/prototype/if_propagation.hgo)调用真实 strconv.Atoi，显式包装后传播。循环/switch/索引/切片/复合字面量等剩余上下文仍未实现，这些是实现缺口，不自动视作用户批准的延期。


### 6.3 T0 收口：多 RHS、取值与发送

已接通标识符目标的多 RHS 赋值/短声明及单个 var spec 的多值初始化，例如 `port, rank := parsePort(text)?, *priority()?`。先按源码顺序计算操作数并捕获前面的运行时值，全部成功才执行原同步赋值；失败不部分写入目标。常量/nil 保留上下文类型；`var a,b uint64 = 1<<n, load()?` 的非恒定位移也保留 uint64，不能用临时 `:=` 默认成 int。具名标量的局部声明/遮蔽/闭包捕获和多文件生成名字一起验收。

`*loadPointer()?` 和 `loadAny()?.(T)` 保留 Go 解引用/断言行为；单 RHS 的 `value, ok := loadAny()?.(T)` 保留 comma-ok 元组。`channel() <- load()?` 先捕获 channel，再计算传播值，成功才发送；失败不在 nil channel 上阻塞，也不在已关闭 channel 上 panic，成功路径仍保留 Go 发送行为。最近函数/闭包、短路、defer 和显式一步 From 规则不变。

[真实前端与生成模块 race](../../../tests/acceptance/propagate_values_test.go)区分运行结果和错误拒绝；[两文件任务投递业务](../../../examples/propagate_values/main.hgo)用真实 strconv.Atoi 并保留 NumError 错误链。测试还覆盖早退不部分赋值、后续副作用跳过、嵌套闭包、63 位位移、channel 重绑定及已关闭 channel。

剩余最小场景：索引 `items[index()?]`、切片 `items[low()?:high()?]`、字面量 `Config{Port: parse()?}`、间接赋值 `items[index()] = load()?`；for 的每轮条件/post 与 continue、switch 的条件/逐 case 求值及 fallthrough、select 的候选求值/选择后赋值需要各自 CFG lowering。前四项主要是已定求值/类型规则的工程缺口，不能归为“等用户设计”。for/switch 中普通传播仍沿最近函数早退；select 候选中失败是否在选择前退出尚未明确规定，先记录待明确的组合规则，不猜成只执行选中分支。完整 generic Try/Residual、From 链和隐式原生多返回适配不加入范围。


### 6.4 T0 收口：索引、切片与复合字面量

新增 `items[index()?]`、`items[low()?:high()?:max()?]` 和 struct/array/slice/map 字面量中的传播，仍仅限既有名义 Option/Result。依据 Go 1.27 工具链随附规范的 [Order of evaluation](https://go.dev/ref/spec#Order_of_evaluation)，调用/方法调用、接收和逻辑运算保持词法顺序；变量读取、索引和 map 插入没有被重新定义成全序。验收不要求某个未规定的动态重复键覆盖结果，只断言结果属于 Go 允许集合及规定的调用顺序。

索引保留 map comma-ok。数组切片保持原存储；不可取址数组不能因临时量获得切片或指针接收者调用能力。省略上下界、长度/容量、常量错误及运行时边界检查保留。字面量保留最终形状，不把 map 改写成逐项赋值，不抹掉重复常量键或非常量数组下标错误；省略元素类型和隐式指针沿原上下文保留。外部包不可命名的 scalar 类型通过原容器的纯零值类型见证推断，支持位移、比较及短路；不读取业务存储或调用用户方法。`comparable` 被源码遮蔽时使用编译器支持包限定约束，不拒绝普通 Go 命名；该路径需要声明已有 goxide 模块 依赖，不引入 source trait 或稳定 ABI。

Go 的数组 `len/cap` 是否求值也保留：移除 `?` 的调用标记后，不能让原本会求值的操作数变成被跳过的常量表达式。`&T{...?...}`（含括号）支持，任意 `&(load()?)` 仍不支持。`unsafe.Sizeof/Alignof/Offsetof` 不求值参数中的传播明确报限制：现有 prelude 直接搬出参数会错误执行它，需后续有意识的非求值类型降低，不能静默执行或宣称已完成。

[真实前端验收](../../../tests/acceptance/propagate_aggregate_test.go)覆盖三份实际生成模块 race、失败/副作用/defer/最近闭包、slice storage、map/index/pointer、键值顺序、只读数组投影、上下文 private 类型及静态拒绝；[跨包批次业务](../../../examples/propagate_aggregate/main.hgo)真实调用 strconv.Atoi 后解析窗口并构造原生 Go 包容器。间接赋值目标尚未加入：它需要保存 LHS 的地址或 map/key，并在 RHS 成功后才写入，与本节的读取表达式不同。循环/switch/select 未借此放宽，select 候选传播规则仍未确定。

### 6.5 T0 收口：间接赋值的两阶段降低

依据本机 Go 1.27.1 随附规范的 [Assignments](https://go.dev/ref/spec#Assignment_statements) 与 [Order of evaluation](https://go.dev/ref/spec#Order_of_evaluation)：第一阶段计算左侧索引/显隐式指针间接访问的操作数和 RHS；第二阶段从左至右写入。传播失败退出最近函数，不进入第二阶段，但不回滚第一阶段用户调用已产生的副作用。成功进入写阶段后，后一个目标 panic 不回滚前一个目标的写入。变量读取相对调用的未规定顺序仍不被收紧成新的语言承诺。

新增 `*pointer()? = value()?`、字段、slice/array/pointer-array/map 索引及其多目标组合。降低保存 pointer、slice header、map 和 index，不复制作为目标的 array/struct；例如 `s[i].Array[j]` 的最终越界及 `p.Array[j]` 的最终 nil 检查仍在写阶段。`p.P.X` 则先读取并保存中间 `p.P`：外层 p 为 nil 是求值阶段 panic，已保存 P 为 nil 是写阶段 panic。不能统一用 `&完整目标` 捕获位置，因为这会过早触发最终检查。

保留原最终赋值语句，包括 compound `op=`、tuple 和 map comma-ok；不提前读取 compound 旧值。RHS 或后面的 LHS 改变原 pointer/container 时，写入已保存的目标。动态不可比较 map key、nil map 和最终 bounds 均保持写阶段 Go panic。已有类型与权限检查继续执行，不增加借用/别名分析，`&T` 不承诺全局不变，`mut&` 不承诺独占。私有 scalar 的上下文零值见证只用于静态同型 RHS/key；interface 目标、不同类型的位移计数不会被强制换成目标类型。

[真实 hgo 验收](../../../tests/acceptance/propagate_place_test.go)覆盖上述顺序/错误/写入/别名/权限和静态拒绝；[跨包业务](../../../examples/propagate_places/main.hgo)使用真实 strconv.Atoi，验证失败保持已有目标值和 NumError cause，并覆盖外部私有 scalar、纯值嵌入 promotion、63 位 shift、不同类型 compound 及 any 键。安装 CLI 后从仓库外转译，所有新增生成模块分别 race。审查使用的纯 Go panic 阶段探针仅为机制证据，不计作 hgo 验收。

**本小节交付时的工程边界（§6.6 仅接通功能，§6.7 性能复核未通过）**：需要保存外包私有嵌入指针的 promoted field 路径，如 `dep.Outer{*privateInner}` 的 `o.X = load()?`，当时给出 `private imported embedded pointer path is not yet lowered`。纯值嵌入 promotion 已支持。私有指针不能直接生成为不可访问 selector，也不能提前对最终字段取地址；本组未引入 unsafe、反射或生产包桥接。这是未完成实现，不是安全拒绝、待用户新语义或获批延期，不能把整个间接赋值项标为完成。

本轮检查 unsafe 拒绝边界：按实际 `types.Builtin` 身份，仅限制 Sizeof/Alignof/Offsetof 中的传播；需要求值的 unsafe.Add/Slice 与普通同名函数均真实执行通过。未扩展 unsafe 非求值降低或源泛型能力。循环/switch 保持下一组工程缺口，select 候选规则继续待决。

### 6.6 私有嵌入指针目标：保留原赋值与原函数执行帧

§6.5 的私有嵌入指针工程缺口现由 [assignment-frame 降低](../../../internal/propagate/frame.go)接通。例如原生 Go 包 `type leaf struct{ Count int }; type branch struct{ *leaf }; type Tree struct{ *branch }` 允许调用方写 `tree.Count = parse(text)?`，但生成 `tree.branch.leaf` 会越过 Go 可见性规则。直接对最终字段取地址会提前触发最终 nil 检查；把整个赋值放进 IIFE 又会改变 deferred 源函数里直接 `recover()` 的归属。私有标量字段的上下文类型也不能通过 `zero Tree.Count` 推断：zero 的嵌入指针为 nil。

**研究校正**：Go 规定的是 §6.4 的偏序与赋值两阶段，不规定所有隐式 pointer 读取都在 RHS 调用之前。先前“浅拷贝必因 RHS 重绑而写错旧/新目标”的推断过强，不能据此宣称根本不可降低。普通 place 路径选择先捕获操作数是允许实现；私有路径选择保留原完整赋值同样允许。测试对 RHS 重绑只接受 Go 允许结果集合；对第二阶段第一项重绑深指针、第二项写 promoted field，则必须写入第一阶段确定的旧位置，不能重走新链。

当前方案保留完整 assignment，让 Go 负责隐藏操作数、上下文类型、可取址性及写阶段；仅把 `?` 替换为接收原 sum 值的有类型 helper。所有源操作数作为 helper 实参在原函数执行帧中求值，直接 recover 仍是直接调用。失败先完成验证与错误转换，再经 [compiler-support guard](../../../runtime/assignment.go)的私有 token 退出该赋值；源函数直接 defer 的 Catch 只在自身 pending 时恢复该 token、填写原函数返回槽，随后正常执行先前用户 defer。普通 panic、invalid sum、panic(nil)、Goexit 不进入此恢复路径。没有反射、unsafe、导出私有字段或生产包专用捕获接口。

返回槽在原函数入口以保留名字的指针绑定，避免内层遮蔽；原无名结果增加 hygienic 名字前先检查完整函数的非法 bare return，blank 名结果也正确处理。guard/defer/赋值放普通词法块，避免声明挡住原合法 forward goto；不增加整条赋值的闭包执行帧。嵌套源闭包保持自己的传播目标，重复遍历不会重复插入入口绑定。

代价是此特殊路径新增 helper、每次执行的 defer、失败时内部 panic，以及对已有 goxide 模块 的明确依赖；成功 guard 也保留至函数退出，不宣称零开销或稳定 ABI。另一可行架构是由可控制的生产包生成有类型的目标捕获/写入 helper，在其可见范围内读取私有链；它需要生产包重编译、调用方/生产方元数据协议，并不能自动覆盖任意现存原生 Go 依赖，因此本阶段不采用。整条赋值 IIFE 方案因直接 recover 反例被排除；Go 编译器内部 typed-IR 集成则超出当前可独立转译的后端范围。

[真实跨包业务](../../../examples/propagate_places/private.hgo)和[接受测试](../../../tests/acceptance/propagate_private_place_test.go)覆盖业务解析/cause、私有整数与布尔上下文、两层指针、RHS/写阶段重绑、nil/失败不写、named/blank/shadow 返回、直接 recover、source defer、旧 panic、Goexit、legacy panic(nil)、nested closure、goto 和只读权限。纯 Go 研究探针只作方案依据；这些仅为功能证据；§6.7 的性能门槛未通过，本项未完成。非求值 unsafe 参数和其他既有传播限制未扩大。

### 6.7 性能门槛与无 panic 控制流研究

§6.6 在 `a3f064fbcd0e75bb06bdba406f007e5abd68bd71` 的实现未获性能验收：guard 的 token 地址传入 panic 接口导致 guard 逃逸，成功也每条分配 32 B。失败额外有 Result 重建，某些多 defer 恢复路径还有 runtime 分配。详见[可复现夹具、数据及反例](../../../tests/research/assignment_guard/README.md)。内部 panic 也可被调试器/采样器观察；不能仅说“有少量 helper/defer 成本”。当前不扩到循环。

路由必须只检查赋值存储路径，不能扫描 LHS 内所有 selector：`a[t.Count] = r?` 的 `Count` 只是普通读取；index/key/调用实参和 pointer/slice/map 容器读取可以使用既有普通 lowering。本次真实 hgo 回归检查生成代码不含 guard。

无 panic 正常控制流候选结论：提前 `&t.Field`（含泛型推断 `capture(&t.Field)`）会早触发最终 nil panic；RHS 成功后统一取址仍会阻止多赋值前项本应发生的写入；延迟闭包重新访问根对象会丢失第一阶段的隐藏指针目标。泛型可以推断不可命名类型，但不会延迟实参求值，Go 函数字面量也不提供省略返回类型来构造通用私有指针 getter。这些具体反例不构成“不可能实现”的证明。

保留整条最终 Go assignment、只分离调用/传播/短路副作用的 IR 值得继续：手写子集已保留私有 uint64 的 `1 << n` 上下文、未类型化 bool 及写阶段 nil。然而不能统一把布尔临时量改回比较式：原命名 bool 在泛型/接口中的类型会变为内建 bool。须记录源表达式原始 typed/untyped 属性（不能仅依赖被上下文具体化的 TypeOf），同时覆盖 tuple/comma-ok、callee/实参顺序、源直接 recover、移除调用后 len/cap 是否仍求值等。该实验不是编译器实现或私有目标验收；这是已定语义的工程工作，不需要新语言设计，不默认为长期延期。

### 6.8 显式传播控制流基础重构

本阶段基于 `ec437f2a12dc40671cb474f7cd2a0efc40cf5db1`，仅完成正常控制流 IR 所需的第一个基础切片，不声称替换私有目标 guard。

候选最终形状是：在原函数体内执行传播的校验和普通失败 return，成功取得 payload 后，保留完整 Go assignment。因失败路径不会到达 assignment，所有目标均不写；成功后 Go 仍负责隐藏指针目标的第一阶段求值和第二阶段从左到右写入，后一目标的最终 nil 不回滚前项写入。私有 scalar 的 `1<<n` 等表达式继续留在原赋值上下文，不提前默认成 int，也不生成不可访问的类型名或 selector。这个说明适用于已验证的手写子集，**不是把任意表达式先求值再赋值都判为正确**。尤其调用/接收/短路条件有规定顺序，中间指针读取可能 panic；不能仅按某次 gc 的隐式读取顺序追加语言保证，也不能忽略确定的控制依赖。

[question.go](../../../internal/propagate/question.go)现在把一个已降低 operand 的 sum 校验、成功 payload、失败转换与结果表示为 `questionFlow`。它不调度源 operand、不处理目标位置。`emit` 显式接收失败出口：普通 expr 使用 `returnFailure`；现有 frame 使用 `guard.Fail`。From 与目标失败值作为出口参数先求值，validator/From panic 不会先设置 pending。frame 不再扫描生成 AST、寻找 return 后改写，源操作数仍在原函数帧作为 helper 实参执行。分支容器是新建的，但表达式节点按只读约定共享，不能把重复 emit 当作深拷贝 API。

最小后续阶段及风险：

| 阶段 / 模块 | 具体职责 | 必须验证的风险 |
| --- | --- | --- |
| 本次：question.go + expr/frame 两调用方 | 显式传播节点与可选择失败出口 | validator/From/payload/失败值顺序、最近函数、guard pending；已实现 |
| 待实现：独立 expression facts 模块，在修改 AST 前保存 | 原始 typed/untyped、地址性、builtin 身份与原 len/cap 求值属性 | 私有 bool 进入泛型/接口不能变为 builtin bool；不能只读上下文具体化后的 TypeOf |
| 待实现：assignment effects planner | 只覆盖现有 private fixtures 所需调用/接收/短路/传播，保留控制与先后依赖 | callee/receiver、LHS 调用与 RHS 调用、短路条件读取、嵌套 ?、直接 recover；不重写全语言 |
| 待实现：完整赋值 emitter | 普通失败 return + residual assignment，不展开隐藏字段 | 私有 scalar 上下文、tuple/comma-ok、compound 旧值、多目标前项换链、nil/bounds panic；覆盖后显式删除对应 guard 路由 |
| 待实现：替换验收 | 真实 hgo 代码形状、跨包权限/race、与显式 match 基准 | 正常 ? 无 panic/recover、成功无新增堆分配；未覆盖路由必须逐项公开 |

**本次仍有 guard 的精确位置**：`statement` 对 `needsAssignmentFrame` 为真的赋值仍调用 `assignmentFrame`；后者声明 Assignment 并 defer Catch，`frameExpr` 的问号失败仍调用 Fail。runtime/assignment.go 未变。所有真正私有嵌入指针目标仍在此路径，包括 [private.hgo](../../../examples/propagate_places/private.hgo) 的 stock/stockFields、重绑、writeThenNil、compoundPrivate、direct recover、命名/blank/shadow 返回、Option、privateLHS、重复赋值与嵌套闭包、权限和 goto 用例；研究 Guard1/Guard8/PackedGuard 也仍使用 guard。只有此前已修复的普通读操作数（index/key/call arg）及普通 place 路径不使用 guard。本次没有新覆盖、静默替换或性能改善声明，也没有扩到循环、select 或 adapters。

新增 [question_test.go](../../../internal/propagate/question_test.go)是生成 Go 的机制测试：同一控制流分别产生普通 return/可观察出口，验证 Option、Result 成败、validator/转换 panic 不进入出口及只读重复 emit。它不算新的 hgo 功能；既有 private acceptance 继续真实安装 CLI 转译并在独立模块 race。


### 6.9 首批无 panic 完整赋值 lowering

基于 `249567c6055f3f7d3ab6d506d9ea39100843c671` 实现 §6.8 的局部 planner，而非全语言 IR 重写。[facts.go](../../../internal/propagate/facts.go)在改写前保存原地址性、checked 类型、常量及语法/对象定义决定的原始 typed/untyped 属性；比较结果是未类型化 bool，逻辑表达式由两侧原属性决定，shift 继承左侧原属性。不能把已被赋值上下文具体化的 TypeOf 当作原属性。

[effects.go](../../../internal/propagate/effects.go)先做不修改 AST 的支持判定，再生成原函数体内的求值、questionFlow 普通失败 return 和一条完整最终赋值。LHS 的显式 pointer 操作数、调用和索引先保存；array/struct 存储路径不复制，不提前对最终字段取址，也不展开隐藏 selector。RHS 的调用/接收、传播和短路按控制依赖求值；失败不会到达最终赋值，成功则由 Go 完成隐藏指针的第一阶段读取和从左到右写入。写阶段前项更换隐藏链时，后项仍写第一阶段确定的位置；最终 nil 不回滚已完成写入。隐式读取相对 RHS 调用的未规定顺序仍不收紧为语言承诺。

原始未类型化数值表达式只在可证明不会产生运行时 panic 的局部算术子集留作 residual：先保存其有类型操作数，保留 `1<<n` 等表达式在私有 uint64 的原赋值上下文；动态 shift count 要求 unsigned，除法/余数等不在此子集。原始未类型化 bool 存为临时 bool 后以与 `(0==0)` 的比较恢复未类型化属性，不引用可被用户遮蔽的 true；原本有类型的私有 bool 则保留其名义类型。短路分支不执行的 operand、校验与转换都不能提前求值。源 recover 仍在原函数执行，不增加 IIFE/defer；用户 panic、defer 和 Goexit 继续走 Go 原控制流。生成临时量封装在普通 block，保留 forward goto；依据原地址性判定，不能通过复制 struct 返回值把非法目标变合法。

| 路径 | 本阶段状态 / 证据 |
| --- | --- |
| 原 private.hgo 全部用例 | 改走普通 return；真实生成文件无 Assignment/Catch/Fail，原 11 项 private 测试（含读操作数）和两项业务继续独立 race |
| 新 effects.hgo | 接收先于失败、真正私有 bool vs builtin bool 的原生泛型类型身份、true 遮蔽、未含问号的 len 副作用、私有 uint64 复合 shift；生成文件无 guard |
| 研究 Guard1/Guard8/PackedGuard | 保留历史函数名便于基准对照，当前生成代码不再使用 guard；实际 hgo 与显式 match 对照，不用手写 IR 探针冒充实现 |
| effects_fallback.hgo 五个赋值 | mixed private bool 右臂推型、有符号 contextual shift、含问号 len、tuple、unsafe.Pointer 转换链仍显式走 guard；形状断言要求恰好五个 Catch，另有运行验收 |

**仍未替换的范围**：private assignment 的 tuple/comma-ok、map/slice 目标组合、需要猜测右侧私有名义 bool 类型的短路、可能 panic 的未类型化 residual 算术、含问号的非 recover builtin（包括 len/cap/unsafe）、动态 callee/方法调用或 index/slice/literal/断言等尚未规划的含问号表达式。无问号且有独立类型的完整表达式可以整体求值，不按上表把所有相关 Go 语法一律拒绝。支持判定失败仍调用 assignmentFrame，源码合法性仍由既有检查保证；这是公开列出的剩余工程范围，不是已经完成或获批延期。runtime guard 不能删除；其命名结果/返回槽绑定等仅为实际 fallback 生成，不再覆盖已替换文件。

独立审查发现的具体反例：`t.Count, out = 7, unsafe.Pointer(uintptr(unsafe.Pointer(p)) + offset()?)`，p 指向两字节数组、offset=1。把 uintptr 存入临时变量后再转回 pointer 会破坏 Go unsafe 的同表达式转换/存活规则，真实 hgo 生成模块 race 触发 checkptr；旧 frame 则通过。支持判定现按真实类型身份保留 unsafe.Pointer 及从 unsafe.Pointer 到 uintptr 的转换链（也保护 syscall 特殊实参），永久回归验证有效偏移与失败不写。该扫描保守覆盖本赋值包含的嵌套闭包体；这可能保留额外 guard，但不会拒绝合法源码。后续可以细化范围，不能为了零分配破坏指针存活。

这种反例限制当前 planner 的拆分策略，并不证明所有无 panic 方案不可能。下一工作是逐类保留特殊转换/求值单元及补齐上下文类型见证；本组不扩循环、select 或 adapters，也不声称整体 T0/MVP 已完成。


### 6.10 私有 bool、上下文 shift 与 len/cap 第二组

基于 `693282c64f0868cb38beaacd98c113e2aba029b1`，新增 [effect_context.go](../../../internal/propagate/effect_context.go)，把上组五条明确 fallback 中三条迁到 [effects_context.hgo](../../../examples/propagate_places/effects_context.hgo) 的无 guard 路径；tuple 和 unsafe 转换链仍保留。没有取消其余未规划形状的限制或宣布全部私有目标完成。

**私有 bool 短路推型**：对于 `(n==0) && model.Flag(r?==7)`，若右臂提供纯静态、非泛型、非 variadic、非方法的函数签名，编译器通过泛型 zero-result helper 从该签名推导 private 返回类型。helper 只返回零值，既不调用 Flag，也不读取目标字段或执行右臂。先用该零值声明保留名义类型的临时量，再写入左条件；只有选中右臂才执行其调用/传播。真实 hgo 验证原生泛型观察到 private bool 而非 builtin bool、二参数函数 witness、未选中右臂调用计数为零，以及 nil 目标上右臂失败不提前读目标。

**上下文有符号 shift**：先保存 count，再在其原求值位置执行 `_ = 0 << count` 的原生 Go 检查；不合成 panic/recover，也不把最终 `1<<count` 默认成 int。负 count 在后续传播前产生原生 shift panic；非负路径仍把真实移位留在私有 uint64 上下文，63 位不丢失。constant 和 unsigned count 不加这项动态检查。残余表达式只有已经排除 panic 的部分延后；contextual 除法/余数等仍未纳入。

**含问号 len/cap**：通过 effects planner 处理 operand，保持私有函数参数的 bool/shift 上下文。显式 array/slice literal 的元素与常量 key 保留，整份 literal 明确求值后再调用 len/cap；不因去掉问号调用而使原本需求值的 array 操作数被常量折叠跳过。pointer-array 值为 nil 时 cap 可取长度，但 `len(*pointerResult?)` 的实际解引用仍 panic；失败传播先于解引用。当前支持 planner 已能处理的普通 operand，加显式 array/slice literal（不含需要推断类型的嵌套省略 literal）；map literal、其他未规划 operand 保持 guard。

独立研究拒绝了“直接复用旧 expr 就足够”的假设：`len(Array((n==0)&&Flag(r?==7)))` 和 `len(ArrayMask(1<<n,r?))` 会在旧普通 expr 路径把 private bool/mask 默认成 bool/int。新路径使用 effects 自己的上下文处理，两条真实跨包 hgo 反例均通过，生成文件无 guard。旧普通目标 expr 的这些组合不是本组已修复范围，仍是具体工程待办；不能借私有目标通过就宣称所有 len 组合已完成。

当前剩余 guard 分组：

- 私有 bool 的类型只来自字段/动态 callee/泛型或 variadic 函数等尚无安全 witness 的来源；不提前执行右臂取类型。
- contextual 除法/余数等未规划残余；len/cap 的未规划 operand 或隐式嵌套 literal；此前 map/slice 目标、动态调用/其他含问号表达式组合。
- tuple/comma-ok 的多值处理，以及 unsafe.Pointer/uintptr 转换链的求值和存活约束。现有永久 fallback 文件恰好两条 Catch，继续 race/checkptr。

下一组先处理 tuple/comma-ok 和可证明保留完整转换链的 unsafe 子集；目前没有证明统一无 guard 不可能。for/range 与 switch 仍是已确认、未完成的 T0 工程项，不因这些分组自动延期；若 unsafe 剩余确有边界，可独立推进循环切片。select 候选语义与 Proposed adapters 不作新决定。

### 6.11 单 RHS tuple 与 comma-ok 的第三组替换

基于 `2fe4c2e9c191ad4d60087153eb1c06f0e784749b`，新增 [effect_multivalue.go](../../../internal/propagate/effect_multivalue.go)。仅对已通过原 Go 检查的单 RHS、多 LHS 普通赋值规划多值；静态函数调用必须具有与目标数一致的 tuple 类型，参数必须在 effects 支持范围内。参数依次规划后，整次调用只执行一次，用多变量 `:=` 推导各自原返回类型，再保留完整最终赋值。不把 tuple 展开成非法的多个普通实参，也不默认私有返回类型为内建类型。

map 查询、类型断言、channel 接收仅在原 `HasOk` 的二值赋值语境进入该路径。保存操作数后原操作执行一次；第二结果以比较恢复未类型化 bool，允许原本合法的 private bool 赋值。函数返回的 bool 是有类型值，不做这种恢复。原目标地址性、只读权限、类型/数量及语境检查不变；不提前取最终字段地址、不复制 array/struct 存储、不拆开最终写入，因此前项重绑隐藏指针后后项仍使用 Go 第一阶段选定的位置，最终 nil 的部分写入也保留。

[真实 hgo 用例](../../../examples/propagate_places/effects_multivalue.hgo)涵盖调用计数、失败不调用/不写、私有 mask/flag 返回、空白目标、前项重绑、最终 nil、map 命中/缺失、断言成败、channel 单次接收/关闭及失败。生成文件必须无 guard；非法混合 RHS、错误数量和多值调用外带实参继续拒绝。专门 fallback 文件由两条降为一条 unsafe 转换链；此数字只描述该文件，绝不代表全语言只剩一处 guard。

剩余 guard：动态/方法 callee、多值调用作为另一调用唯一实参、map/slice 目标组合、未规划的私有 bool witness、contextual 除法/余数、len/cap 未规划 operand、其他含问号的 index/slice/literal/断言组合，以及 unsafe.Pointer/uintptr 转换链。没有新增可证明安全的 unsafe 拆分子集，本次保持原转换链的 race/checkptr 回归；GC/特殊 syscall 存活约束不能用保存 uintptr 或运行时 KeepAlive 猜测替代。该长尾不阻塞本多值切片独立验收，也没有被视为已批准延期。

for/range 与 switch 仍是 T0 待实现项；普通 expr 的私有上下文问题仍须修复。select 与 Proposed adapters 的设计决策继续独立列出，不扩完整 trait/native bridge/Value/PATCH/YAML。整体 T0/MVP 尚未完成。

### 6.12 for 与 slice/map range 的首批传播

基于 `863051b9a57b198c3237f2ef06ac7b0e25baa0b1`，本阶段从 guard 长尾转向普通循环控制流。[loops.go](../../../internal/propagate/loops.go)使用原函数中的普通 return，不引入 panic、recover、IIFE 或 Assignment guard。[真实 hgo 示例](../../../examples/prototype/loop_propagation.hgo)与生成模块验收同步。

**for init/条件/post**：含问号 initializer 的求值前缀放在包围 block；若源 initializer 是短声明，最后声明仍留在原生 For.Init 中，因此 Go 1.22 起的每轮变量身份仍由 Go 管理，不能把它简单提升成循环外变量。条件的传播前缀与真假检查在每轮 body 入口执行。含问号 post 放到下一轮入口，用循环外 first 标志只跳过首轮；此时原条件即使不含问号也移到 post 之后，保持 post→条件→body。Go 原生下一轮变量复制先于 post，普通 continue 和带标签 continue 均继续经过该复制及 post。没有 post 传播时保留原生 post。无限循环不额外插入退出条件；源 break/return 跳过 post。

原 body 保留独立 block，防止其局部变量遮蔽移入的 header 表达式。生成代码仍属于原函数，失败执行该函数 defer；循环体的 defer 不提前执行，函数字面量中的传播仍返回最近闭包。initializer 目前沿用已有 statement 支持，post 覆盖已有普通赋值/调用及丢弃传播等形状；含问号 inc/dec 目标等未支持 statement 不会被顺带放行。

**标签**：源 goto 指向外层 block，重入时重新执行 initializer/range 源前缀；break/continue 改指向内部原生 loop 的新标签。只有 break/continue 使用时不保留已无 goto 用户的外层标签。改写前按原词法函数检查重复标签与空白标签目标，嵌套函数有独立标签作用域；否则删除/改名会掩盖本应拒绝的源码。独立审查发现这两条真实反例，均已修复并加入负例。合法重复 `_:` 定义、仅 goto、仅 continue、两类混用及原本 unused 标签均有验收；不会用伪 goto 消除 unused 诊断。

**range**：新增传播源仅支持 slice/map（含其命名类型），完整求值并保存一次源，再保留原生 RangeStmt；`:=` 逐轮独立变量与 `=` 共享既有变量不改写。nil slice/map 仍为空迭代，失败不进入循环。原 range 源不含问号时不求值、不保存或改写它，因此 `range p` / `range *p`（nil 数组指针、单变量、常量长度）、整数 range 原有规则保留，均有循环体传播验收。当前前端接受的原生函数迭代器也保留原语法，已验证单值 iterator 的 body 失败早退和 defer；由 Go 处理 yield/return 协议，不在 hgo 新造 iterator ABI。

| 范围 | 本阶段状态 |
| --- | --- |
| 常见 for init/条件/post、已有可降低 body | 已接通；每轮求值、短路/普通 continue、标签、嵌套循环、闭包和地址身份、部分执行后失败与 defer |
| slice/map range 源含问号 | 已接通；一次求值、成败、nil、捕获及标签重入 |
| 无问号的其他 Go range 源 | 保留原表达式与原生迭代；验收 nil 数组指针/解引用、整数、单值函数 iterator body |
| 数组/数组指针、整数、string/channel、函数 iterator 源含问号 | 尚未接通，明确诊断；尤其不能去掉 marker 后让常量长度规则跳过原本需求值的 array 解引用，也不能一律强制求值 |
| range key/value 目标含问号 | 尚未接通；目标按轮求值不能提升到循环前 |
| 循环内需要旧 assignmentFrame 的组合 | 明确未实现 guard-free plan，拒绝新增 guard；这是工程缺口，不是别名安全规则或批准延期 |
| switch/select、泛型函数原型限制及其他未支持 statement | 不借循环改动扩大；switch 仍待实现，select 仍待决 |

其余 unsafe/私有赋值 guard 长尾仍按 §6.11 留表，不阻塞本切片也不标完成。for/range 只对上表已验收范围完成，整体 T0/MVP 未完成。性能研究使用真实 hgo 循环与相同 Option 协议的显式 match，单独运行非 race 基准；零分配不代表与显式 match 耗时相同。

### 6.13 表达式 switch 的传播与循环开销复核

基于 `ab8889b26fdf32272ccec77204042c7dd57cf87a`，新增 [switches.go](../../../internal/propagate/switches.go)，覆盖 expression switch 的 initializer、tag、case 表达式和已有可降低的 case body。初始化与 tag 在原函数中求值，局部声明保留在包围整个 switch 的 block；tag 仅求值一次，未类型化 tag 仍使用 Go 默认类型。没有 tag 时保留原生 switch 的隐式 true 规则，不能把私有名义 bool case 偷偷当作内建 bool。

含 case 传播时，按原 clause 与逗号列表的顺序逐项求值；每项放进单 case 的原生 switch，保留原类型转换、接口 nil 与动态不可比较值 panic 的规则。只在尚未匹配时求下一项；传播失败普通 return，尚不进入任何 case body。选中编号交给最后的原生 switch，原 clause 顺序和 default 位置保持，因此 break、fallthrough（包括落入/离开 default）、case 独立作用域和带 default 的终止性仍由 Go 保证。default 仅在所有非 default case 不匹配之后执行，不因其写在前面就提前选中。

标签复用控制语句的入口分离：goto 重入 initializer/tag/case 求值，带标签 break 指向最终原生 switch，continue 到 switch 标签仍非法。循环中的 continue 继续作用于外层循环。nearest-function return、defer 和用户 panic 不增加隐藏函数或 guard。Type switch 与 expression switch 分开：本阶段不支持 type switch 自身 initializer/guard/body 中的传播，仍明确诊断，未改其无传播语法。

改写前保存最终原 AST 的 Go 类型检查错误，对 switch header/tag/case 区间先报错，避免拆成单 case 后掩盖重复常量、类型不匹配、不可比较 tag 等原静态错误。原 case body 留给最终 Go 检查，非法末尾 fallthrough、continue 标签和泄漏变量不能放行。独立审查还发现跨包 private mask 的 `(1<<n)|(1<<r?)` 会在旧 expr 丢失上下文；现优先复用已验证的 effects planner，真实跨包用例覆盖 63/62 位、原生负 shift 检查先于后续失败，以及 private bool 短路。effects 尚未覆盖的表达式仍受既有普通 expr 范围限制，不能宣称全部 Rust/Go 表达式已支持。

含问号的 unsafe 转换链不在本 switch plan 中，明确拒绝而不拆 pointer/uintptr 链；仍需旧 assignmentFrame 的控制流赋值同样留工程缺口，不增加 guard。其他 range 源/目标、type switch、旧私有上下文长尾和其余 guard 未完成，select/adapters 不作新设计。

for 性能复核：上一阶段实际生成循环为成功 **39.31 ns**、失败 **21.00 ns**，显式 match 为 **26.64/12.69 ns**，都零分配。独立同轮对照为原生成 **38.90/18.41 ns**、仅去 first/移 post 尾部的手写 Go 机制探针 **36.59/17.00 ns**、显式 match **26.85/13.21 ns**；保持同 Option 协议、输入与结果。三者都超过 inline budget，first 分支仅能解释部分差异，且伴随布局/寄存器变化；不能把明显耗时差距用 0alloc 掩盖。探针对捕获了短声明迭代变量的生产循环不安全，本阶段不作这种替换。完整 [原始数据、汇编与复现](../../../tests/research/loop_propagation/README.md#first-flag-follow-up-isolate-part-of-the-overhead)明确区分真实 hgo 与手写机制探针，后续性能优化仍是独立工程项。

### 6.14 type switch 与其他非函数 range 源

基于 `e1face9af24ef7cf89ce87cb77da5ab182567c1b`，接通 type switch 的 initializer、被断言接口表达式和已有可降低 case body。保留原 `x := value.(type)` 或无绑定 guard，以及原 case 类型列表，不把 case 变量改成统一 any。Go 因而继续在单类型 case 中赋予具体类型，在多类型/default/nil case 中使用原接口类型。只前置初始化与接口表达式的普通求值/失败 return，case 作用域、typed-nil 与 nil、重复类型检查、非法 fallthrough/continue、标签重入和最近闭包规则不变。

range 源新增 string、整数、channel、数组和数组指针，沿用原生 RangeStmt 管理每轮变量、接收阻塞与停止。string 验证 UTF-8 byte index/rune 值；channel 仅保存 channel 值，不预接收，验证未缓冲发送配对、关闭、提前退出不多收、失败源为 nil 时早退。

**整数上下文**：effects 先保存操作数并执行必要的原生负 shift 检查，最后纯 residual 仍留在原 Range.X。`for t.Wide = range 1<<r?` 的 private uint64 类型由原迭代目标提供，不能 `tmp :=` 默认成 int。无变量/短声明的 Go 默认类型及已类型化源的类型仍由原生 range 检查；负数/零没有迭代。真实跨包测试以 63 位计数只进入首轮，避免大循环并能检测错误的 int 默认化；失败不写目标，负 shift 仍先于后续传播失败。

**数组求值边界**：对本组可降低、实际求值的传播源保存完整数组值，不能只保存指针，再让 `range *savedPointer` 因常量 len 而省略原本必须发生的解引用。数组值保留进入循环前的副本；数组指针保留指针，后续元素变化按 Go 可见。验证了 nil 指针单索引可迭代、第二变量为 `_` 不增加元素解引用、两变量读元素会 panic、实际求值的完整数组解引用（包括零长数组）仍 panic。

不自行实现一套 Go 常量 len 判定器：公共 go/types 的 TypeAndValue 不直接给出所有数组 range 免求值结论，不能简单依据 X.Value 或剪掉常量子树。源不含传播时完全保留原表达式，尤其 `range *nilArrayPointer` 的原生免求值行为。传播位于 unsafe.Sizeof/Alignof/Offsetof 等现有未支持非求值子树时继续明确拒绝，不为接通 range 而强制执行。函数迭代器源中的传播与 range 赋值目标中的传播仍未实现；无传播的原生 iterator 源及此前已验证的 body 保持既有行为。

[真实示例](../../../examples/prototype/type_range_propagation.hgo)覆盖两种 type guard、单/多类型/接口 nil、init/subject 失败顺序、defer、case/init 闭包捕获和标签，以及上述 range 边界；[私有整数用例](../../../examples/propagate_places/effects_range.hgo)独立跨包验证。没有新增正常传播 panic 或 Assignment guard；原 Go panic 与 channel 阻塞仍是原行为。其余控制流内需要旧 guard 的赋值、unsafe 链与普通 expr 上下文长尾不因此完成。

后续 T0 优先级不再由问号长尾占满：先推进常用 match 表达式组合，再接通源码依赖/同模块的公开引用权限元数据，并修正普通用法的权限跟踪误拒绝；再按应用补齐剩余传播与性能/诊断。完整 trait、长期别名分析、未定 native bridge/adapters 不能作为这些既定工程的隐性前置，公开元数据也不等于一次冻结最终外部 ABI。

### 6.15 普通短路表达式的私有 bool 身份

普通 expr 原先把 `(n==0)&&model.Flag(r?==7)` 的左臂用 `:=` 默认成 bool，尽管完整表达式的真实类型是外包私有 flag。在 `len(model.FlagArray(...))`、普通参数或局部值中，原本合法的表达式因此在生成 Go 阶段被误拒。

现对已定型、不可直接命名且左臂未类型化的 &&/||，在改写任何 arm 前执行已有 effects preflight；静态函数签名能提供原类型见证时复用 contextual planner。见证只取零值，不调用右臂函数，不读取外部存储；选中分支才执行传播/业务调用。保留私有名义 bool 身份，不新增转换、IIFE 或 guard，失败仍退出最近用户函数/闭包。

[真实跨包 CLI 验收](../../../tests/acceptance/propagate_private_bool_test.go)覆盖 interface 中真实类型、短路次数、len/cap 求值、未选 nil Result、失败与 defer、显式闭包及含传播的直接 recover。生成模块单独三项 race 测试与 vet，重复输出一致且无 Assignment/Catch/Fail；typed bool 与 private bool 不兼容、private bool 传 builtin bool 仍拒绝。本组不扩大未知动态/方法/泛型 witness，不删除剩余 unsafe/guard，也不宣布所有私有表达式长尾完成。

# RFC 0005：Option / Result 专用问号传播

状态：Option/Result 专用传播与下述显式 From 单步错误转换方向已获用户确认；精确语法和前端实现仍待完成。不公开 Residual/Try。thiserror 核心派生生成显式 From，目标 Result 已知时 `?` 可选择一步转换；不搜索转换链或任意泛型 blanket impl。当前没有问号前端实现。

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

Go 机制夹具 [propagation_test.go](../../tests/design/enum/propagation_test.go)核对 typed-nil 原样保留、恰好一次、失败后跳过后续操作、defer、闭包目标、Option 及部分成功原生 I/O。它没有编译 hgo `?` 或实现错误转换规则。

未来前端最小验收：上述两个正例；Option/Result 混用、错误类型不兼容、普通闭包内 `?` 的明确类型诊断；宏插入闭包、短路和参数顺序；非法 Result nil 不被当成 Err。From 正例需验证目标已知、源/目标唯一、转换恰好一次且保留 source/typed nil；负例包括重复来源、仅存在两跳转换、任意 blanket 及从静态 error 猜动态变体。不要求用户自定义 residual、完整映射库或所有原生多返回形式自动适配才能交付首版。

待闭合：显式 From 的最终声明/派生拼写、可见性与冲突诊断；普通赋值兼容优先于 From 的具体选择细则；From 本身失败或 panic 的协议。实现不得为通过用例隐式搜索转换链。宏原型可先生成明确 Go helper 验证转换和错误链，必须另行标出尚未端到端实现的 `?`。

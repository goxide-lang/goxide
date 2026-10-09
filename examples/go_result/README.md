# 显式适配 Go 返回值

`go_result!` 和 `go_result_keep!` 是 **Goxide 编译器保留的 builtin 宏**，可以直接调用，无须 `use` 宏定义。它们不是 `std` 或 `std/sum` 导出的源码包宏，也没有 `std::go_result!` 这样的限定入口。普通版在失败时只保留原 E；保值版同时保留返回值和原 E。

编译器负责识别调用、检查原生 Go 签名、生成适配 helper，并登记具体 Result 的载荷检查。`github.com/goxide-lang/std/sum` 提供共享的 `Result[T,E]` 类型、Ok/Err cases 和构造器；源码中的 `Result` 由编译器接到这个共享类型，不是在每个用户包定义一个新家族。无须导入 builtin 不等于生成程序没有 std 模块依赖：依赖版本由工程的 manifest 固定。

普通 Go 包（例如 `strconv`、`io`）仍按 Go 规则 import；它们的原生调用仍返回 E 或 `(T,E)`，只有显式写适配宏时才得到 Result。普通 `.go` 文件不能使用 `go_result!(...)` / `go_result_keep!(...)`、`?` 或 Goxide 的 match 语法；Go 消费者可导入 `std/sum` 使用其普通 Go API，或调用已生成的 Go 包。这里不增加 namespace、prelude、隐式适配或新语法。

## 选择普通版或保值版

```goxide
// strconv.Atoi 返回 (int, error)
ordinary := go_result!(strconv.Atoi(text))
// reader.Read 返回 (int, error)，可能同时返回 n > 0 和 error
retained := go_result_keep!(reader.Read(buffer))
```

| 入口 | 结果类型 | 失败载荷 |
| --- | --- | --- |
| `go_result!(call())`，返回 `(T,E)` | `Result[T,E]` | 原始 E，明确舍弃此次返回的 T |
| `go_result_keep!(call())`，返回 `(T,E)` | `Result[T,struct{Value T; Err E}]` | 普通 Go 值保存的 `Value` 和原始 `Err` |
| `go_result!(call())`，仅返回 E | `Result[struct{},E]` | 原始 E |
| `go_result_keep!(call())`，仅返回 E | `Result[struct{},struct{Value struct{}; Err E}]` | unit 值及原始 E |

E 须实现预声明 error 且可为 nil（接口、指针，以及具名 slice/map/func/channel）。成功条件是**原调用静态 E 类型**上的 `err == nil`，成功载荷为 T（error-only 为 `struct{}{}`）。nil 的 `*MyError` 为 Ok；接口内的 typed-nil error 保持非 nil，不先装箱成 error 再比较、不调用 `Error()` 来判断，也不归一化。外层期望的 Result 错误类型不会改写这次判断。保值不是 Clone；slice/map/pointer 等依旧遵循普通 Go 赋值语义。

可以给保值载荷起**透明 alias**，方便签名和 match：

```goxide
type ReadFailure = struct { Value int; Err error }
func read(reader io.Reader, buffer []byte) Result[int,ReadFailure] {
    return go_result_keep!(reader.Read(buffer))
}
func readSize(reader io.Reader, buffer []byte) Result[string,ReadFailure] {
    n := go_result_keep!(reader.Read(buffer))?
    return Result[string,ReadFailure]::Ok(strconv.Itoa(n))
}
```

`ReadFailure` 没有 `Error`/`Unwrap` 方法，也没有自动 From：对 `partial.Err` 使用 `errors.Is/As`。将保值结果直接 `?` 到 `Result[U,error]` 会拒绝；相同载荷类型传播保留全部字段，赋给 any 也保留整个结构。若要舍弃 Value，显式 match 后选择 `partial.Err`，或一开始选择普通版。普通版 `?` 沿用既有可赋值与精确单步 From 规则；`*MyError` 可赋给 error，但不会先装箱再隐式链接 `From[error,F]`。

## 运行

在仓库根目录，使用真实 Go 1.27.x，并让 CLI 构建与运行使用同一工具链。示例依赖 [go.mod](go.mod) 固定的 `github.com/goxide-lang/std v0.0.0-20261009104839-c02a09d8c417`（sum ABI 3）；根模块的 std 版本相同。首次运行前需缓存 manifest 指定的依赖。

```sh
export GO_BIN=/absolute/path/to/go1.27.1/bin/go
export GOROOT="$("$GO_BIN" env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off
work="$(mktemp -d)"
git rev-parse HEAD # 记录实际 compiler 源码版本
"$GO_BIN" version
"$GO_BIN" list -m github.com/goxide-lang/std
"$GO_BIN" build -o "$work/goxide" ./cmd/goxide
"$work/goxide" run --go "$GO_BIN" examples/go_result
"$work/goxide" transpile --go "$GO_BIN" -o "$work/generated" examples/go_result
"$GO_BIN" -C "$work/generated" run -race .
"$GO_BIN" -C "$work/generated" vet ./...
"$GO_BIN" test ./tests/acceptance -run '^TestGoResult' -count=1
```

示例两次运行都输出：

```text
next: 42
parse error: true
partial: 3 abc true
```

[main.gox](main.gox) 使用 `io.Reader`，最后一个非空片段同时返回 EOF；[ops/macros.gox](ops/macros.gox) 展示源码包宏组合。`ops` 导入仅用于自定义的 `parse_result!` 包装宏，直接调用两个 builtin 不需要它。

[官网单文件例子](../../website/examples/go_result.gox) 无自定义宏包依赖，同时展示 Atoi 普通适配和 Reader 保值适配；官网“Go 返回值适配”卡片也说明 builtin 来源。将该文件原样复制到 Go module 外的临时目录后，可用 `goxide run --go "$GO_BIN" /path/to/playground.gox` 执行，输出同上；浏览器 Playground 对同一源文件执行转译，不执行用户 Go 程序。CLI/WASM 一致性以同一 compiler、std/export data 版本的生成 Go 逐字节比较为准，不能用历史网站产物代替。

完整断言在[真实 CLI 验收](../../tests/acceptance/go_result_test.go)：生成模块单独 race/vet、原始错误身份、Reader 次数/缓冲区、解析 `errors.As`、typed nil、错误类型转换、卫生、panic/defer/recover、单次求值与参数顺序；另有两个独立 go.mod 的原生 alias、保值 Result metadata、match/问号和预编译宏导入拒绝。

## 求值与范围

- 单参数形式只接收一个调用表达式，按其原生静态签名推导 T/E；只允许 E 或 `(T,E)`，不接收任意值、零返回或三个以上返回值。兼容 `go_result!(T, call)` 和 keep 对应形式，显式 T 形式要求恰好两个返回值，且 T 与实际第一返回类型完全相同。非 nilable 的 struct 错误不推定零值成功；`(A,B,E)` 需先组织为业务 struct。
- 具体 Result 登记要求 T/E 在包作用域可命名；外部私有类型若有公开透明 alias 可以使用，无公开写法或局部自定义类型给出明确诊断，不降级成 any。尚未解析的泛型/嵌套问号/适配器在 typed 固定点等待；支持跨文件包级初始化。
- 展开为具体类型包级辅助函数调用，原调用位于其**实参**，不会迁入函数体；只执行一次，内层 `?`、direct recover 仍属于用户函数。适配器运行在原调用完成之后。真实实例进入 validator/equality/metadata 登记，沿用载荷投影检查。
- `defer go_result!(call())` 和 `go go_result!(call())` 遵循展开后的普通 Go 调用语义：**call 在注册 defer/启动 goroutine 前求值**，延期/并发的是接收返回值的适配器。若要延期/并发原调用，请显式写用户函数体。
- 两个入口是保留的内建宏，可组合进现有声明宏。它们不扩展通用宏 type fragment，也不承诺所有局部类型上下文。局部 `error`/`nil`/`Result` 遮蔽已有测试；包级 error 遮蔽沿现有内建类型限制拒绝。
- 源码同模块跨包宏支持；独立模块可调用原生 API、消费生成 Result 并恢复增强信息。**从预编译模块导入宏规则仍不支持**，已有明确负例；不将其称作获批延期。
- 未增加隐式 `nativeCall()?`、泛化 From、Residual/Try 或指针桥接。部分诊断仍标明 generated/unmapped 中间位置，未宣称完整源码映射。

推导验收见 [独立模块及静态类型测试](../../tests/acceptance/go_result_inference_test.go)，包含生成模块 race/vet、可命名私有 alias、非法嵌套 enum 和负例。

语义与实现原因见 [RFC 0005](../../docs/rfcs/research/0005-option-result-question.md#显式-go-result-适配宏2026-10-04)。

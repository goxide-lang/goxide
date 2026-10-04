# 显式适配 Go 返回值

用户授权两种选择：普通版在失败时只保留 error；保值版同时保留返回值和 error。不会自动改写原生 Go 调用，也不引入 Try/Residual 或新指针语法。

```hgo
// strconv.Atoi 返回 (int, error)
ordinary := go_result!(int, strconv.Atoi(text))
// reader.Read 返回 (int, error)，可能同时返回 n > 0 和 error
retained := go_result_keep!(int, reader.Read(buffer))
```

| 入口 | 结果类型 | 失败载荷 |
| --- | --- | --- |
| `go_result!(T, call())` | 本包 `Result[T,error]` | 原始 error，明确舍弃此次返回的 T |
| `go_result_keep!(T, call())` | 本包 `Result[T,struct{Value T; Err error}]` | 普通 Go 值保存的 `Value` 和原始 `Err` |
| `go_result!(call())`，调用仅返回 error | `Result[struct{},error]` | 原始 error |
| `go_result_keep!(call())`，调用仅返回 error | `Result[struct{},struct{Value struct{}; Err error}]` | unit 值及原始 error |

成功条件是原生 `err == nil`，成功载荷为 T（error-only 为 `struct{}{}`）。typed-nil error 保持非 nil，不调用 `Error()` 来判断，也不归一化。保值不是 Clone；slice/map/pointer 等依旧遵循普通 Go 赋值语义。

可以给保值载荷起**透明 alias**，方便签名和 match：

```hgo
type ReadFailure = struct { Value int; Err error }
func read(reader io.Reader, buffer []byte) Result[int,ReadFailure] {
    return go_result_keep!(int, reader.Read(buffer))
}
func readSize(reader io.Reader, buffer []byte) Result[string,ReadFailure] {
    n := go_result_keep!(int, reader.Read(buffer))?
    return Result[string,ReadFailure]::Ok(strconv.Itoa(n))
}
```

`ReadFailure` 没有 `Error`/`Unwrap` 方法，也没有自动 From：对 `partial.Err` 使用 `errors.Is/As`。将保值结果直接 `?` 到 `Result[U,error]` 会拒绝；相同载荷类型传播保留全部字段，赋给 any 也保留整个结构。若要舍弃 Value，显式 match 后选择 `partial.Err`，或一开始选择普通版。普通版 `?` 仍可使用既有 `#[from] error` 的唯一一步转换。

## 运行

在仓库根目录，使用真实 Go 1.27.x。以下命令已在 Go 1.27.1 执行；没有把纯 Go 机制探针当成 hgo 验收。

```sh
export PATH=/workspace/goxide-environment/go/bin:$PATH
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off
work="$(mktemp -d)"
go build -o "$work/hgo" ./cmd/hgo
"$work/hgo" run examples/go_result
"$work/hgo" transpile -o "$work/generated" examples/go_result
go -C "$work/generated" run -race .
go -C "$work/generated" vet ./...
go test ./tests/acceptance -run '^TestGoResult' -count=1 -v
```

示例两次运行都输出：

```text
next: 42
parse error: true
partial: 3 abc true
```

[main.hgo](main.hgo) 使用 `io.Reader`，最后一个非空片段同时返回 EOF；[ops/macros.hgo](ops/macros.hgo) 展示源码包宏组合。完整断言在[真实 CLI 验收](../../tests/acceptance/go_result_test.go)：生成模块单独 race/vet、原始错误身份、Reader 次数/缓冲区、解析 `errors.As`、typed nil、错误类型转换、卫生、panic/defer/recover、单次求值与参数顺序；另有两个独立 go.mod 的原生 alias、保值 Result metadata、match/问号和预编译宏导入拒绝。

## 求值与范围

- `T` 需显式提供，并与调用的第一个返回类型相同（透明 alias 可以）。原生泛型调用自身仍可由 Go 推断；宏不自动推断 T。当前具体 Result 登记要求 T 在包作用域可命名，局部自定义类型仍受既有限制。
- 最后返回类型必须是预声明的 `error` 或其透明 alias。`(T,*MyError)` 明确拒绝，避免 nil 指针被装箱后悄悄变成 Err。仅有 error 的调用可以直接适配；`(A,B,error)` 需先返回业务 struct，不自动合成 tuple。
- 展开为具体类型函数 literal 的调用，原调用位于其**实参**，不会迁入函数体；它只执行一次，内层 `?`、direct recover 仍属于用户函数。适配器运行在原调用完成之后。
- `defer go_result!(call())` 和 `go go_result!(call())` 遵循展开后的普通 Go 调用语义：**call 在注册 defer/启动 goroutine 前求值**，延期/并发的是接收返回值的适配器。若要延期/并发原调用，请显式写用户函数体。
- 两个入口是保留的内建宏，可组合进现有声明宏。它们不扩展通用宏 type fragment，也不承诺所有局部类型上下文。局部 `error`/`nil`/`Result` 遮蔽已有测试；包级 error 遮蔽沿现有内建类型限制拒绝。
- 源码同模块跨包宏支持；独立模块可调用原生 API、消费生成 Result 并恢复增强信息。**从预编译模块导入宏规则仍不支持**，已有明确负例；不将其称作获批延期。
- 未增加隐式 `nativeCall()?`、泛化 From、Residual/Try 或指针桥接。部分诊断仍标明 generated/unmapped 中间位置，未宣称完整源码映射。

语义与实现原因见 [RFC 0005](../../docs/rfcs/research/0005-option-result-question.md#显式-go-result-适配宏2026-10-04)。

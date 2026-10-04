# RFC 0012：print / println 与格式检查

## 状态与范围

已确认目标：首版提供 `print!` / `println!`（D14）。
本文规定候选对应关系；格式子集、formatter 接口与 I/O 错误策略未冻结（T11、U09）。
依据：[原设计](../design/macros.md)。格式检查由内建入口支持，不开放用户过程宏。

## 源级规则

- 候选格式子集：字面量中的 `{}` 请求 Display，`{:?}` 请求 Debug，`{{` / `}}` 输出花括号。
- 编译期检查格式语法、参数数量和显式 trait 实现；内建标量也必须有选定的实现。
- 每个显式实参从左到右恰好求值一次，全部求值后再按占位符顺序运行 formatter。
- 参数临时量保留借用来源；不能复制独占借用、延长借用或把格式计划带出调用。
- 不通过 Go 反射、`fmt.Stringer`、`error.Error()` 或 `any` 回退执行未知方法。
- 打印在生成程序运行时执行；宏展开不执行参数、formatter 或 stdout 写入。

## 转译示例

**F01：先执行全部参数，再格式化。** goxide 函数体片段；`left` / `right` 返回 int。

```goxide
println!("{} {}", left(), right());
```

Go 对照片段；依赖 `strconv`、选定的 stdout 写入 helper 及相同的参数函数。
此例已选择 int 的 Display，`strconv.Itoa` 仅表示该具体实现，不是通用格式化 ABI。

```go
goxideArg0 := left()
goxideArg1 := right()
goxideText0 := strconv.Itoa(goxideArg0)
goxideText1 := strconv.Itoa(goxideArg1)
writeChecked(out, goxideText0+" "+goxideText1+"\n")
```

约束：顺序为 `left → right → Display(0) → Display(1) → write`，各参数只执行一次。
若替换为有副作用的显式 formatter，仍保持这个顺序；借用冲突在生成 Go 前拒绝。

**F02：空参数和换行。** goxide 函数体片段。

```goxide
print!("");
println!();
println!("x\n");
```

Go 对照；依赖下面的候选 `writeChecked` 与绑定到 stdout 的 `out`。

```go
// print!("") 不产生输出。
writeChecked(out, "\n")
writeChecked(out, "x\n\n")
```

输出依次为零字节、一个 LF、`x` 后两个 LF；不合并或删除用户已有换行。
候选 helper 的普通 Go 实现如下，依赖 `io`；正式包路径与名称不在本 RFC 冻结。

```go
func writeChecked(out io.Writer, text string) {
    n, err := io.WriteString(out, text)
    if err != nil {
        panic(err)
    }
    if n != len(text) {
        panic(io.ErrShortWrite)
    }
}
```

正常返回对应 Unit。写入错误或短写 panic；错误前允许已写出部分内容，不承诺回滚。
需要处理写入计数与错误的业务代码应使用普通 I/O API。

**F03：格式错误在前端拒绝。** goxide 反例；无对应 Go 输出。

```goxide
print!();                  // 缺少格式字面量
println!(runtime_format);  // 不支持运行时格式串
println!("{}", 1, 2);      // 多余参数
println!("{:?}", secret);  // 假设 secret 类型没有显式 Debug
```

依赖：`runtime_format` 为 string；`secret` 的类型未提供 Debug。
不能生成 `%v` 或反射代码使这些输入通过。

## 拒绝与边界

- 未闭合花括号、缺少参数或缺少相应 trait：定位格式位置与实参，拒绝生成。
- 隐式变量捕获、位置重排、宽度 / 精度和动态模板：不在当前候选子集中。
- formatter 留存参数借用、输出器或计划，以及转交 goroutine：按借用规则拒绝。
- 对锁内数据的 Debug 必须有合法访问协议；打印不额外授予访问权限。
- 不承诺一次系统调用、跨调用输出原子性、自动重试或持久化。

## 双向互操作

goxide 格式化 Go 类型须有显式适配；Go 已有 String 方法不自动构成 goxide Display。
发布 `.go` 包含普通格式化和输出调用，Go 构建不读取宏或格式计划元数据。
Go 可以调用导出的打印函数；不能直接使用 goxide 宏语法，也不获得 goxide 借用检查。

## 验收与未定项

1. F01 用记录事件的参数和 formatter 检验顺序；参数只求值一次。
2. F02 比较精确字节；用失败和短写 writer 分别验证 panic 与部分写入。
3. F03 及借用逃逸必须前端拒绝，不能依赖 Go 构建诊断。
4. 成本：格式化与 I/O 在运行时发生；示例拼接可分配字符串，不承诺零分配。
5. 未定：formatter 签名、错误载体、正式输出适配和格式子集 / panic 策略的最终确认。
6. `format!`、stderr 入口、assert 和 matches 不因本 RFC 自动进入首版范围。

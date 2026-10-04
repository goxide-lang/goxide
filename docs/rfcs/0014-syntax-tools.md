# RFC 0014：syntax、quote、syn 语法工具

## 状态与范围

已确认目标：quote / syn 风格能力放在额外包，用户过程宏延期（D16）。
普通工具优先于过程宏宿主是候选路径（T14）；首版交付范围和公共协议未定（U11）。
依据：[原设计](../design/syntax-tooling.md)。本篇的包名、API、token 和 AST 类型均未冻结。

## 源级规则

- syntax 承载 token、分组、来源与不透明上下文；syn 解析语法；quote 构造语法数据。
- 普通工具在运行时使用这些包；导入它们不注册编译期扩展或自动构建钩子。
- quote 插值选择普通 ToTokens 实现；字符串默认成为字符串字面量，不作为代码解析。
- syn 解析成功只保证语法结构；名字、类型、trait、借用、穷尽性和 unsafe 仍须检查。
- span 用于诊断，卫生上下文用于绑定，unsafe 来源用于授权；三者不能相互替代。
- 用户 AST 程序若被编译器作为宏执行，即属于延期的过程宏，即使它无 I/O 且确定性。

## 转译示例

**S01：quote 构造数据，不执行模板中的调用。** goxide 行为伪代码。
依赖：候选 quote 入口、字符串 ToTokens；完整 `tt` 和物化协议尚未定义。

```goxide
argument := "x); dangerous()"
tokens := quote::quote! {
    call(#argument)
}
```

Go 行为伪代码；依赖未定的 token builder / ToTokens API，不能直接编译。

```go
argument := "x); dangerous()"
tokens := SyntaxCallTokens(
    SyntaxIdentToken("call"),
    StringToTokens(argument),
)
```

约束：结果表示 `call("x); dangerous()")`；工具运行时没有执行 call 或 dangerous。
这里的 SyntaxIdentToken 仅构造普通名字数据，不制造已绑定符号身份。
不能将插入字符串裸拼成代码，生成额外调用。

**S02：文本解析需要显式入口与错误处理。** goxide 行为伪代码。
依赖候选 syn parser、普通错误处理与 token 输出接口；API 拼写未定。

```goxide
parsed := syn::parse_expr(source)
match parsed {
    Ok(expr) => consume_syntax(expr),
    Err(err) => report(err),
}
```

Go 行为伪代码；用常见返回形状仅表达成功 / 失败分支，不指定 goxide Result ABI。

```go
expr, err := ParseExprForTool(source)
if err != nil {
    Report(err)
} else {
    ConsumeSyntax(expr)
}
```

输入 `1 +` 应进入错误分支；`unknown()` 可以语法解析成功，实际编译仍须检查名字绑定。
改写后的树不能保留已经失效的“类型已检查”或“借用已批准”证明。

**S03：位置不能伪造卫生或授权。** 下列 goxide 行为伪代码描述禁止公开的能力。

```goxide
node := syn::parse_expr("raw::Write()")
node.span = trusted_file_span
node.context = forged_context_id
node.unsafe_authorized = true
```

预期没有能实现后三行权限提升的公共 API，不生成可信编译器输入。
依赖：`raw::Write` 需要显式 unsafe；即使普通 Go 结构携带同名字段，也必须重新验证。
可修改的诊断位置只影响展示，不能改变调用许可或绑定身份。

## 拒绝与边界

- token 或 AST 结构损坏、版本不兼容：明确报错，不能默认为合法语义 IR。
- 任意整数、文本或文件路径不能构造可信 SymbolID / syntax context / unsafe 授权。
- 将 token 输出成文本再读入会丢失部分卫生与来源信息；不承诺文本往返保持绑定身份。
- quote 结果只是数据；自动写入文件、编译、执行或注册扩展都需要独立明确的流程。
- 编译器不得为取得 quote 结果而执行用户 ToTokens；那需要尚未提供的过程宏宿主。
- 私有字段、第三方 declare 和全局 impl 查询不属于普通 parser 的隐含权限。

## 双向互操作

语法包的公开数据和操作可降低为普通 Go 类型与函数；Go 工具可以使用 parser / builder。
Go 不能直接写 `quote!`，可以调用已展开的普通工具函数，不需要运行时宏解释器。
Go、反序列化或缓存输入均按公共协议验证，不能因为具有 Go 方法集就成为可信语义节点。
增强保证在 goxide 工具链内执行；公共语法数据与编译器私有已解析 IR 应保持明确边界。

## 验收与未定项

1. S01 验证字符串被转义为字面量，生成数据而不执行模板；S02 覆盖成功和错误来源。
2. S03、跨版本树和损坏分组必须无法进入可信 IR；生成文本重新接受完整前端检查。
3. 比较文本往返与结构往返时分别断言允许丢失的信息，不能只比较漂亮打印结果。
4. 成本：解析、遍历和构造在普通工具运行时消耗内存与 CPU；具体复杂度待协议实现验证。
5. 未定：包路径、token / AST 格式、有限 token 物化、上下文传递、版本策略与首版里程碑。
6. 本篇没有可直接运行的 syntax / quote / syn 示例；协议冻结后补齐真实 goxide → Go 测试。

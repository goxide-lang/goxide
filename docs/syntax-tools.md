# syntax 语法工具

## 语法工具概览

`github.com/goxide-lang/syntax` 是独立的 Go module，当前提供三个包。它们使用 Go 1.27，仅依赖 Go 标准库；普通 Go 工具可以直接使用，过程宏也使用同一组类型。

| 包 | 当前用途 | 阅读入口 |
| --- | --- | --- |
| `token` | 不可变 token 树、词法与分组、构造器、位置和绑定上下文 | [读取与构造 token](#读取与构造-token)、[API](https://goxide-lang.github.io/docs/?page=sdk-token) |
| `syn` | 单个 type/struct/enum 声明的只读视图 | [读取类型声明](#读取类型声明)、[API](https://goxide-lang.github.io/docs/?page=sdk-syn) |
| `proc_macro` | Context、Output、诊断和调用协议 | [过程宏大章](procedural-macros.md#过程宏概览)、[API](https://goxide-lang.github.io/docs/?page=sdk-proc-macro) |

公共树的节点类型是 `token.Token`，有序容器是 `token.TokenStream`。“token 树”是结构描述，SDK 没有叫 `TokenTree` 的公开类型。

### 普通 Go 与 Goxide 中使用

普通 Go module 可直接添加与本网站相同的 SDK：

```sh
go get github.com/goxide-lang/syntax@v0.0.0-20261021034157-cb998fa4b2d4
```

然后使用普通 Go import：

```go
import (
    "github.com/goxide-lang/syntax/token"
    "github.com/goxide-lang/syntax/syn"
)
```

Goxide 项目在清单中写 `[dependencies.syntax]`，通过 `use syntax::token`、`use syntax::syn` 导入；完整清单见[第一个过程宏](procedural-macros.md#第一个过程宏)。syntax 与编译器各自发布，过程宏接入核对实际 SDK 协议内容，而不是要求两边版本号相同。

### 当前范围

`token.ParseText` 做词法分析与括号分组；`syn.Parse` 读取一个类型声明。名字绑定、类型推导、trait/impl 选择、引用权限及完整程序检查由编译器完成。

当前没有 `quote` 包或 `quote!`。构造语法使用 `token` 构造器、`ParseText` 和保留原 token 的 `ToTokens()`；完整 syn 文件/表达式 AST、遍历和修改接口仍未提供。导入语法工具不会登记或执行宏，也不会扫描目录或改写文件。

## 读取与构造 token

`TokenStream` 是有序的 `Token` 序列。Group 包含子流，因此它能表示嵌套结构；根流没有外层分隔符。节点不可变，`Tokens()` 返回切片副本，`Append` 和位置修改方法返回新值。

### 一个可运行的 Go 工具

在已添加 SDK 的普通 Go module 中，将下面保存为 `main.go`，执行 `go run .`：

```go
package main

import (
    "fmt"

    "github.com/goxide-lang/syntax/token"
)

func main() {
    stream, err := token.ParseText("r#match::value + 1;", token.BindingContext{})
    if err != nil { panic(err) }
    for _, node := range stream.Tokens() {
        fmt.Printf("%s %q raw=%v\n", node.Kind(), node.Text(), node.Raw())
    }
}
```

`r#match` 得到一个 `Text() == "match"`、`Raw() == true` 的 Ident；`::` 是一个完整 Punct。普通离线工具可以使用零值 `BindingContext`，不需要编译器宿主。

### 节点种类

| Kind | 保留的信息 |
| --- | --- |
| Ident | 名字正文、raw 标记、绑定上下文和位置 |
| Keyword | Goxide 关键字拼写 |
| Literal | 整数、浮点、虚数、字符或字符串类别，以及原始合法拼写 |
| Punct | 一个完整词法标点，例如 `::`、`=>`、`>>=` |
| Semicolon | 分号及 `Implicit()` 标记 |
| Group | `Parenthesis` / `Bracket` / `Brace`、子流及两端位置 |
| Fragment | 宿主签发的已解析片段及可观察视图 |

读取 Group 时用 `Children()` 遍历子流。其他节点可通过 `Kind()` 选择相应 getter；检查类型后再读取 `LiteralType()` 或 `Delimiter()`，不要用显示文本猜节点种类。

### 选择构造方式

短的固定语法可用 `ParseText(text, context)`，拼接调用方的内容时保留它的原 token。需要精确控制结构时，用 `NewIdent`、`NewKeyword`、`NewLiteral`、`NewPunct`、`NewSemicolon` 和 `NewGroup`。

下面在普通 Go 中构造 `(21) * 2`，所有构造器错误都明确处理：

```go
package main

import (
    "fmt"

    "github.com/goxide-lang/syntax/token"
)

func main() {
    context := token.BindingContext{}
    number, err := token.NewLiteral(token.Integer, "21", context)
    if err != nil { panic(err) }
    group, err := token.NewGroup(token.Parenthesis, token.New(number), context)
    if err != nil { panic(err) }
    multiply, err := token.NewPunct("*", context)
    if err != nil { panic(err) }
    two, err := token.NewLiteral(token.Integer, "2", context)
    if err != nil { panic(err) }
    output := token.New(group, multiply, two)
    if err := output.Validate(); err != nil { panic(err) }
    fmt.Println(output.Display())
}
```

在宏里将零值 context 换成 `ctx.DefSite()`，输入来自入口参数，错误转换成 `pm.Failure`，就是[函数式宏](procedural-macros.md#函数式过程宏)的构造方式。

### 词法细节

- raw identifier 的 `r#` 不属于名字正文；`NewIdent("match", true, context)` 是标识符，`NewKeyword("match", context)` 是关键字。
- 负数由 `-` 与正数字面量组成。`NewLiteral(token.Integer, "-1", context)` 不是合法的单个整数字面量。
- `ParseText` 使用 Goxide 的自动分号规则；末尾的名字、字面量或右括号可能产生隐式分号。作为更大语法的一段来拼接时，应检查边界分号，不能一律删除输入中的分号。
- `NewSemicolon` 创建显式分号。原输入中的显式/隐式差异会保留，影响后续语法解析。
- 普通注释和空白不进入公共树，但注释中的换行仍可能影响自动分号。`Display()` 输出可读文本，不保留原格式或注释。

`ParseText` 成功只说明词法和分组成功；合法类型或表达式需要在使用位置继续解析、检查。错误可通过 `*token.ParseError` 取得 `Span()`，位置使用 UTF-8 字节偏移。

完整的构造器、getter、预算常量和错误类型见 [token API](https://goxide-lang.github.io/docs/?page=sdk-token)。

## 读取类型声明

`syn.Parse` 接收恰好一个 type、struct 或 enum 声明，返回只读 `syn.Item`。输入前后的分号会被接受并保留；分组 `type (...)`、完整文件和其他种类声明不在当前入口范围内。

### 一个可运行的字段检查器

下面的普通 Go 程序读取多名字段，输出每个字段名及其写出的类型：

```go
package main

import (
    "fmt"

    "github.com/goxide-lang/syntax/syn"
    "github.com/goxide-lang/syntax/token"
)

func main() {
    input, err := token.ParseText("pub struct Pair { left, right int }", token.BindingContext{})
    if err != nil { panic(err) }
    item, err := syn.Parse(input)
    if err != nil { panic(err) }
    fmt.Println(item.Kind(), item.Name().Text())
    fields, ok := item.Type().StructFields()
    if !ok { panic("expected a written struct") }
    for _, field := range fields {
        for _, name := range field.Names() {
            fmt.Println(name.Text(), field.Type().ToTokens().Display())
        }
    }
}
```

输出为：

```text
struct Pair
left int
right int
```

`Names()` 返回空列表时，字段可能是嵌入字段或 enum 的位置参数；空白名字 `_` 仍是一个名字。`Tag()` 返回原字面量 token 和是否存在的布尔值。

### 各视图提供什么

| 视图 | 主要信息 |
| --- | --- |
| `Item` | Kind、Name、Attributes、Visibility、Generics、Where、Alias、Type、Variants |
| `Ident` | 原始名字、raw 标志和上下文 |
| `Attribute` | Path 与未解释的 Arguments；参数保留括号或等号 |
| `Type` | 写出的类型 token；直接写出的 struct 可取 StructFields |
| `Field` | 属性、显式可见性、名字列表、类型和 tag |
| `Variant` | 属性、名字、unit/tuple/named 形状和 payload 字段 |

每个视图都能 `ToTokens()` 回放原 token，并以 `Span()` 取得首 token 的诊断锚。这个锚不会跨多个源文件或宏输出拼出一个虚构的整段范围。

### 类型、泛型与可见性

`Generics()` 包含原来的方括号组，`Where()` 包含 `where` 关键字。它们是原语法视图；生成新的 impl 时需要保留参数并正确生成对应约束，不能把泛型声明的文本直接当作某个具体类型使用。

`Visibility()` 只报告显式写出的 `pub`。Go 风格 `type Order ...` 的首字母公开约定由编译器解释，syn 不把它改写成 `pub`。

`Type.StructFields()` 只读取直接写出的 struct，不追踪 alias、路径、括号或不透明类型片段。enum 通过 `Variants()` 读取，`Item.Type()` 为空；`struct Name { ... }` 糖的 Type 是花括号内容，struct 关键字属于声明头。

数组长度、属性参数、函数签名组和 interface body 保留为 token；syn 不解析这些内容的全部表达式语义。它也不会解析名字、展开 alias、推断类型或查询 impl。

### 在 derive 中使用

derive 可以逐个字段/变体读取类型与属性，保留调用方名字，构造普通 `impl`。完整实例见[派生宏与 helper](procedural-macros.md#派生宏与-helper)。遇到不支持的输入，使用相关节点的 Span 返回诊断；`*syn.ParseError` 也提供错误位置。

syn 当前没有可变 AST、Visitor 或重新打印任意文件的 API。需要修改时，用 `token` 显式构造新语法，再由编译器检查。完整类型与方法见 [syn API](https://goxide-lang.github.io/docs/?page=sdk-syn)。

## 保留语法与来源

宏处理的不只是 token 拼写。名字绑定、诊断位置、原输入引用分别承担不同职责；选择正确的操作，才能在展开后继续正确解析调用方的名字。

| 操作 | 结果 |
| --- | --- |
| 直接转交输入 token | 保留其绑定、位置与原输入身份 |
| `syn` 节点的 `ToTokens()` | 回放原 token，包括隐式分号和片段引用 |
| `WithSpan` / `WithDelimiterSpans` | 新值使用指定诊断位置，绑定与来源保持原规则 |
| 构造器、`ParseText` | 创建新语法，使用提供的上下文 |
| `Display()` 后再 `ParseText` | 通过文本创建新语法；原引用和绑定身份不会从拼写恢复 |

### Fragment 的观察与转交

声明宏可能把已经解析的表达式、类型、路径等交给过程宏。SDK 用 Fragment 表示这类输入；真实 AST 与可信来源仍留在编译器宿主中。

原样转交 Fragment 会保留它的语法类别。`Children()` 允许读取其公开 token 视图，但视图中的后代没有原 Fragment 的引用身份；重新输出这些后代属于新语法。把原样 expr Fragment 放入 type 槽会报类别错误。

普通宏作者通常只需保留原 token，或用 syn 读取后显式构造输出。`Scope.CaptureFragment` 是宿主转换桥的接口，不是用来给宏生成内容签发“已经检查过”的证明。

### 位置与权限

Span 只控制显示错误的位置。修改 Span 不会改变 `crate::` 的归属、cfg 的来源、私有成员访问或引用权限。输出语法会重新经过普通名字、类型和权限检查。

想从调用方选择名字时使用 `ctx.CallSite()`；想生成宏定义包里的公开名字时使用 `ctx.DefSite()`。用运行库根生成调用时使用 `RuntimeRoot`。具体例子见 [Context 与名称解析](procedural-macros.md#context-与名称解析)。

## 高级宿主协议

这一页面向开发 SDK 调用工具或执行宿主的人。编写普通 Goxide 宏时，编译器已经生成 launcher，直接实现 Context → Output 的入口即可。

### 一次调用的生命周期

1. 宿主为调用建立唯一 `token.Scope`，准备定义点、调用点、位置与输入。
2. `pm.NewRequest(scope, config)` 捕获输入并冻结独立的引用表快照。
3. `pm.WriteRequest` 写入一个完整 UTF-8 JSON 请求，后接 EOF。
4. guest 用 `pm.ReadRequest` 读取；`pm.Dispatch` 从固定 `[]pm.Entry` 表选择一个 handler。
5. `pm.WriteResponse` 验证并写入输出与诊断；宿主用原 Request 调用 `pm.ReadResponse`。

可运行的请求/响应例子已经随 SDK 发布，见 [proc_macro API 参考](https://goxide-lang.github.io/docs/?page=sdk-proc-macro)的示例。它直接展示 Scope、RequestConfig、静态 Entry、字节缓冲和响应验证；普通 Go 中也能运行。

### Scope 与传输数据

`Scope` 由宿主在执行前构建，一次调用一个唯一 ID。`ScopeData` 是宿主发给 guest 的快照；响应不能通过提交另一份快照替换宿主引用表。`WireToken` 等结构是传输描述，需要按原 Scope 验证后才能恢复引用。

`NewRequest` 冻结后，继续修改原 Scope 或配置不会给这个请求追加能力。其他调用的引用、陈旧上下文或越界位置会被拒绝。新建零上下文 token 在响应编码时绑定到定义点。

### 静态入口与失败

每个 `Entry` 恰好设置 Function、Attribute、Derive 中的一个 handler。重复名字、错误种类或未知入口会报错；SDK 不会动态发现或登记宏，也不吞掉 handler panic。

请求和响应读取要求恰好一个 JSON 文档。SDK 检查未知/重复字段、大小写、Unicode、节点结构、引用和资源预算。任一 error 诊断会使 `ReadResponse` 返回 `DiagnosticError`，保留有序诊断但不提供可用输出 token。

SDK 自身没有隐式进程 I/O，也不执行 WASM。文件、网络、时间、内存、取消与整体执行预算由宿主实现；Goxide 的实际宿主规则见[执行环境与浏览器](procedural-macros.md#执行环境与浏览器)。

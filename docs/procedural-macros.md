# 过程宏与 proc_macro

## 过程宏概览

过程宏在编译时读取语法，返回新的语法或诊断。宏库是独立的 Goxide lib；编译器把它构建为 WASI 程序，执行后继续检查和转译输出。最终 Go 程序只包含展开结果。

`proc_macro` 是 `github.com/goxide-lang/syntax` 模块中的 SDK 包。宏入口接收 `pm::Context` 和 `token::TokenStream`，返回 `pm::Output`。语法数据来自同一模块的 `token`，需要读取类型声明时再使用 `syn`。

| 入口 | 写在哪里 | 输入与结果 |
| --- | --- | --- |
| `name!(...)` | 表达式、类型、语句或声明位置 | 参数 token → 对应位置的语法 |
| `#[name(...)]` | 支持属性的完整声明上 | 属性参数、原声明 → 替换声明 |
| `#[derive(Name)]` | 一个具名类型或 enum 上 | 原类型 → 保留原类型并追加实现 |

先完成[第一个过程宏](#第一个过程宏)，再分别阅读[函数式宏](#函数式过程宏)、[属性宏](#属性过程宏)和[派生宏](#派生宏与-helper)。[完整三类宏项目](https://goxide-lang.github.io/learn/?stage=procedural)还提供可下载的消费者、宏实现、清单和测试。

阅读 API 时可分别查阅 [proc_macro 参考](https://goxide-lang.github.io/docs/?page=sdk-proc-macro)、[token 参考](https://goxide-lang.github.io/docs/?page=sdk-token)和 [syn 参考](https://goxide-lang.github.io/docs/?page=sdk-syn)。这些参考页来自网站所用编译器实际选择的公开 SDK 版本。

浏览器用于修改调用方、观察固定宏的展开结果。编写自己的宏使用下面的本地项目流程；当前在线入口见[执行环境与浏览器](#执行环境与浏览器)。

## 第一个过程宏

先按[安装指南](website-guide.md#安装)准备对应 CLI 与 Go 工具链。新建 `hello-macro` 目录，放入以下五个文件。消费者和宏库分别有自己的 `goxide.toml`；项目根无需手写 `go.mod`。

```text
hello-macro/
  goxide.toml
  main.gox
  main_test.gox
  macros/
    goxide.toml
    lib.gox
```

### goxide.toml

```toml
[go_mod]
module = "example.org/hello-macro"
go = "1.27"

[[bin]]
name = "hello-macro"
path = "."

[dependencies.macros]
path = "macros"
```

### macros/goxide.toml

```toml
[go_mod]
module = "example.org/hello-macros"
go = "1.27"

[lib]
name = "macros"
path = "."
proc_macro = true

[dependencies.syntax]
go = "github.com/goxide-lang/syntax"
version = "v0.0.0-20261021034157-cb998fa4b2d4"
```

这里固定的 syntax 版本与本网站编译器的 `require` 一致。更新 SDK 时，使用对应的编译器构建；协议内容不匹配会在构建宏之前报告错误。[工程章节](#宏项目测试与发布)介绍本地开发、依赖角色与缓存。

### macros/lib.gox

```gox
package macros

use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
    return pm::Success(input)
}
```

`#[proc_macro]` 登记一个函数式入口，名字就是 `identity`。完整实现只有一条语句：原样返回输入 token，保留它们的名字绑定和源码位置。`ctx` 暂时用不到，仍是入口签名的一部分。

### main.gox

```gox
package main

use fmt;
use macros;

func main() {
    calls := 0
    next := func() int { calls++; return 42 }
    fmt::Println(macros::identity!(next()), calls)
}
```

`next()` 不在宏进程里执行。宏返回这段调用语法，程序运行时再调用 `next`。因此输出是 `42 1`。

### main_test.gox

```gox
package main

use macros;
use testing;

func TestIdentity(t *testing::T) {
    calls := 0
    next := func() int { calls++; return 42 }
    if macros::identity!(next()) != 42 || calls != 1 {
        t.Fatal("identity changed the result or evaluation count")
    }
}
```

### 构建、运行与测试

在 `hello-macro` 目录执行：

```sh
goxide build .
goxide run --offline --locked .
goxide test --offline --locked .
goxide gen --offline --locked -o ../hello-macro-go .
```

第一次 build 可能下载固定的 SDK，并创建 `goxide.lock`。后面的命令使用已有来源与锁；`test` 真正执行项目测试，工具链要求见安装页。生成目录中的 Go 可以直接阅读，表达式仍是普通 `next()` 调用。

提交源码、两个清单和锁文件，把 `target/` 排除在版本控制之外。移动这个本地 path 项目后，先重新执行不带 `--locked` 的 build，再使用锁定命令。

继续练习时保持这两个清单与 `macros/lib.gox`，按下一页增加宏文件并替换消费者和测试。每个登记函数都必须位于宏 lib 根包，显式 `pub`、非泛型、没有 receiver，且恰有一个登记属性。

## 函数式过程宏

函数式宏接收调用括号内的 token；最外层调用括号不在输入流中。下面的 `twice!` 生成 `(输入) * 2`。输入只出现一次，因此表达式只求值一次。

沿用[首宏项目](#第一个过程宏)，增加 `macros/twice.gox`，替换 `main.gox` 和 `main_test.gox`。

### macros/twice.gox

```gox
package macros

use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro]
pub func twice(ctx pm::Context, input token::TokenStream) pm::Output {
    if input.IsEmpty() {
        return pm::Failure("twice requires an expression", ctx.CallSpan())
    }
    group, err := token::NewGroup(token::Parenthesis, input, ctx.DefSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    multiply, err := token::NewPunct("*", ctx.DefSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    two, err := token::NewLiteral(token::Integer, "2", ctx.DefSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    return pm::Success(token::New(group, multiply, two))
}
```

`NewGroup` 保留输入 token 的上下文；新增的分组、标点和字面量使用定义点上下文。构造器会检查合法的分隔符、标点和字面量拼写，错误由宏转换为可定位诊断。

### main.gox

```gox
package main

use fmt;
use macros;

func main() {
    calls := 0
    next := func() int { calls++; return 21 }
    fmt::Println(macros::twice!(next()), calls)
}
```

### main_test.gox

```gox
package main

use macros;
use testing;

func TestTwice(t *testing::T) {
    calls := 0
    next := func() int { calls++; return 21 }
    if macros::twice!(next()) != 42 || calls != 1 {
        t.Fatal("twice must evaluate its input once")
    }
}
```

运行首宏项目的四条命令，结果仍是 `42 1`。把调用改成 `macros::twice!()` 会得到 `twice requires an expression`；传入不能与整数相乘的表达式，则由展开后的普通类型检查报告错误。

### 输入输出位置

| 调用位置 | 输出要求 |
| --- | --- |
| 表达式 | 一个表达式 |
| 类型 | 一个类型 |
| 语句列表 | 零条或多条语句 |
| 单语句槽 | 一条语句 |
| 声明列表 | 零项或多项声明 |
| 单声明槽、trait/impl 成员 | 符合该位置的数量与成员规则 |

当前没有 pattern 过程宏。输出中的其他普通宏调用会继续展开。宏返回语法后，编译器照常检查名字、类型、可见性和引用权限。

求值次数由生成的代码决定。把输入复制成 `输入 + 输入` 会执行两次；卫生机制不会替宏作者修正副作用、短路或控制流。

## 属性过程宏

属性入口接收三项参数：`Context`、属性括号内的 `args`、去掉本次属性后的 `item`。返回值整体替换目标声明。需要保留的属性和原声明内容应由宏自己转交。

以下实现同时提供 `keep` 和 `constant`：前者检查没有参数并原样转交，后者为无额外属性的普通 `func` 声明替换函数体。这是一个范围明确的小例子，复杂函数语法需要更完整的解析。

沿用首宏项目，增加 `macros/attributes.gox`，替换消费者和测试。

### macros/attributes.gox

```gox
package macros

use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro_attribute]
pub func keep(ctx pm::Context, args token::TokenStream, item token::TokenStream) pm::Output {
    if !args.IsEmpty() {
        return pm::Failure("keep takes no arguments", ctx.CallSpan())
    }
    return pm::Success(item)
}

#[proc_macro_attribute]
pub func constant(ctx pm::Context, args token::TokenStream, item token::TokenStream) pm::Output {
    if args.IsEmpty() {
        return pm::Failure("constant requires a return expression", ctx.CallSpan())
    }
    tokens := item.Tokens()
    if len(tokens) == 0 || tokens[0].Text() != "func" {
        return pm::Failure("constant requires a function", ctx.CallSpan())
    }
    keyword, err := token::NewKeyword("return", ctx.DefSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    body := token::New(keyword).Append(args.Tokens()...)
    group, err := token::NewGroup(token::Brace, body, ctx.DefSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    for index := len(tokens) - 1; index >= 0; index-- {
        if tokens[index].Kind() == token::GroupKind && tokens[index].Delimiter() == token::Brace {
            tokens[index] = group
            return pm::Success(token::New(tokens...))
        }
    }
    return pm::Failure("constant requires a function body", ctx.CallSpan())
}
```

### main.gox

```gox
package main

use fmt;
use macros;

#[macros::keep]
#[macros::constant(6 * 7)]
func answer() int { return 0 }

func main() { fmt::Println(answer()) }
```

### main_test.gox

```gox
package main

use testing;

func TestConstant(t *testing::T) {
    if answer() != 42 { t.Fatal("constant did not replace the body") }
}
```

运行结果为 `42`。`keep` 先转交仍带 `constant` 的声明，随后 `constant` 替换函数体。将 `keep` 改成 `keep(1)` 会得到 `keep takes no arguments`；把 `constant` 的参数删空会得到相应诊断。

### 属性顺序与适用位置

编译器先选择 `cfg` / `cfg_attr`，删除的项不会执行宏；再从前向后执行可执行属性。每次替换后重新分类、处理输出，最后处理 derive 与声明体。

可执行属性用于支持它们的完整声明；字段、enum 变体、任意语句和 inner attribute 位置不开放可执行属性宏。字段上的 `#[serde(...)]` 等是由 derive 读取的 helper，规则见下一页。

属性宏没有独立的 helper 登记机制。它可以读取或消费目标上的属性；保留下来的未知属性仍会进入普通诊断。宿主也不会把原属性自动复制到宏输出的每一个新声明。

## 派生宏与 helper

derive 宏接收一个具名 type/struct/enum 声明。编译器保留原声明，把宏返回的声明追加在它后面；宏不需要再次输出原类型。登记中的名字是用户在 `derive(...)` 中使用的名字，入口函数名可以不同。

下面给具体类型生成 `typeName()` 方法。沿用首宏项目，增加 `macros/type_name.gox`，替换消费者和测试。实现全部写在此文件中。

### macros/type_name.gox

```gox
package macros

use strconv;
use syntax::token;
use syntax::syn;
use syntax::proc_macro as pm;

#[proc_macro_derive(TypeName)]
pub func deriveTypeName(ctx pm::Context, input token::TokenStream) pm::Output {
    item, err := syn::Parse(input)
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    if item.Alias() || !item.Generics().IsEmpty() || !item.Where().IsEmpty() {
        return pm::Failure("TypeName requires a concrete, non-alias type", item.Span())
    }
    prefix, err := token::ParseText("impl", ctx.DefSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    body, err := token::ParseText(
        "{ pub func typeName(self Self) string { return " + strconv::Quote(item.Name().Text()) + " } }",
        ctx.DefSite(),
    )
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    output := prefix.Append(item.Name().ToTokens().Tokens()...)
    return pm::Success(output.Append(body.Tokens()...))
}
```

### main.gox

```gox
package main

use fmt;
use macros;

#[derive(macros::TypeName)]
type Order struct { ID int }

func main() { fmt::Println(Order{ID: 1001}.typeName()) }
```

### main_test.gox

```gox
package main

use testing;

func TestTypeName(t *testing::T) {
    if (Order{}).typeName() != "Order" { t.Fatal("wrong type name") }
}
```

运行结果为 `Order`。输出的 Goxide 相当于下面的实现，再由正常前端生成 Go 方法：

```gox
impl Order {
    pub func typeName(self Self) string { return "Order" }
}
```

`item.Name().ToTokens()` 保留调用方类型名字的绑定；`ParseText` 只用于新的 `impl` 关键字和方法体。类型名进入返回字符串时使用 `strconv::Quote` 生成合法字符串字面量，而不是把任意字符串拼成代码。

这个 `TypeName` 示例主动限制为无泛型、非 alias 类型。`syn` 已提供 `Generics()` 和 `Where()` 原文视图；支持泛型的派生需要保留参数，并生成适当的普通 `impl` 与约束。宏登记函数本身仍是非泛型函数。字段、变体与语法读取方法见 [syn 教程](syntax-tools.md#读取类型声明)。

### helper 的登记与消费

用 `#[proc_macro_derive(Name, attributes(option))]` 为一个 derive 声明可读取的惰性属性。helper 可出现在该类型及其字段、变体上，也可写在 derive 前面；它只在这个目标范围内生效。

例如 `#[proc_macro_derive(TypeName, attributes(type_name))]` 可以为 TypeName 登记 `type_name`。实际实现还需解析 `syn::Item.Attributes()` 或字段/变体上的属性，并检查参数、位置和重复键；仅登记名称不会改变上面 TypeName 的生成结果。`Attribute.Path()` 给出属性路径，`Arguments()` 保留括号组或等号后的原 token，SDK 不替业务宏解释参数。

同一项的所有 derive 读取同一份去掉 derive 请求的输入快照；前一个 derive 的生成结果不会传给下一个。多个 derive 可以共享同名 helper，全部相关消费者完成后才消费它。没有对应 derive 的 helper 会报未知属性；若同名又能解析为可执行属性，则需要限定可执行属性的路径以消除歧义。

## Context 与名称解析

`Context` 提供本次调用的定义点、调用点、位置、目标配置和可用运行库根。它的值由宿主准备；宏作者从入口接收它，无需自己创建上下文。

| API | 用途 |
| --- | --- |
| `DefSite()` | 新语法按宏定义包解析名字 |
| `CallSite()` | 显式按本次宏调用的词法位置解析新名字 |
| `DefSpan()` / `CallSpan()` | 宏定义或调用的诊断位置 |
| `Target()` | 被展开目标程序的 OS 与 Arch |
| `Features()` / `HasFeature(name)` | 本次调用冻结的目标 feature 集合 |
| `RuntimeRoot(module)` | 获取宿主已经提供的目标运行库路径 |

### 保留输入，显式选择新名字的上下文

输入 token 已经携带调用方的绑定。直接转交 `input` 或 `item.Name().ToTokens()` 会保留这些信息。构造新名字时通常使用 `ctx.DefSite()`；需要调用方提供的名字时，再显式使用 `ctx.CallSite()`。

例如下面的完整宏读取调用位置的 `answer`。把它加入宏根包，并在消费者调用所在作用域定义 `answer := 42`，`macros::callerAnswer!()` 就生成对这个局部变量的引用：

```gox
package macros

use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro]
pub func callerAnswer(ctx pm::Context, input token::TokenStream) pm::Output {
    if !input.IsEmpty() { return pm::Failure("callerAnswer takes no arguments", ctx.CallSpan()) }
    name, err := token::NewIdent("answer", false, ctx.CallSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    return pm::Success(token::New(name))
}
```

相反，定义点名字从宏定义包寻找；重导出和调用方改 alias 不会改变它。嵌套宏的 CallSite 是本次调用的词法位置，不固定为最外层用户文件。定义点路径中的 `crate::` 指宏 crate；输入路径保留自己的 crate 归属。

新 token 的零值上下文在宏响应中采用定义点，离线 Go 工具也能使用零值上下文。显式传 `ctx.DefSite()` 能让生成意图更清楚。复制输入、新建局部绑定和生成包级公开声明仍遵循正常的卫生与重名检查。

### RuntimeRoot 与运行库

运行库可以重导出宏，例如业务库公开一个 `macros` 子入口。宏为该库生成调用时，通过 `ctx.RuntimeRoot("example.org/runtime")` 取得消费者可见的完整根，而不是猜消费者使用了什么 alias。

查询键是逻辑 module 路径。返回的是带绑定的 `TokenStream`，可继续追加公开成员路径。不存在、未激活或不可达的根会返回带调用位置的错误；这个方法不会新增依赖、选择版本或读取磁盘。

消费者必须直接依赖运行库，或通过显式完整根重导出取得它。只重导出某个 trait 或宏不会同时授予整个库根。宏库也无需为了输出运行库调用而反向编译依赖运行库，这样可以避免“运行库重导出宏、宏又依赖运行库”的构建环。

完整签名见 [proc_macro API](https://goxide-lang.github.io/docs/?page=sdk-proc-macro)。

## 源码位置与诊断

token 的 `Span` 用来显示错误位置，`BindingContext` 用来解析名字。调整 Span 不会改变名字绑定、依赖来源或可见性。

### 选择合适的位置

| 出错内容 | 常用位置 |
| --- | --- |
| 缺少整个宏参数、无法读取调用输入 | `ctx.CallSpan()` |
| 某个字段、类型、属性参数有问题 | 该输入节点的 `Span()` |
| 宏定义中的配置有问题 | `ctx.DefSpan()` |
| 解析新生成文本失败 | 解析错误自身的生成文本位置 |

`Span.Range()` 返回字节偏移；Unicode 字符可能占多个字节。`LocalSpan` 描述生成文本中的偏移，不是任意宿主文件路径。分组还保留开、闭分隔符位置；`WithSpan`、`WithDelimiterSpans` 返回新值，原 token 不变。

### 返回错误、警告与说明

`pm::Failure(message, span)` 返回一条 error。需要同时返回多条诊断时，直接构造 `Output`；顺序会被保留。下面的宏发出警告和说明，并继续原样转交输入：

```gox
package macros

use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro]
pub func inspect(ctx pm::Context, input token::TokenStream) pm::Output {
    return pm::Output{
        Tokens: input,
        Diagnostics: []pm::Diagnostic{
            {Severity: pm::Warning, Message: "inspect is a tutorial macro", Span: ctx.CallSpan()},
            {Severity: pm::Note, Message: "the input is returned unchanged", Span: ctx.CallSpan()},
        },
    }
}
```

任一 error 使整次展开失败，输出 token 不会继续进入后续编译。`pm::FromParse(token::ParseText(...))` 可把 token 解析的 `(stream, error)` 转成 Output，并保留解析错误的位置。常规输入错误应返回诊断；panic、trap、超时等作为宏执行失败报告。

成功诊断通过 `Output.Diagnostics` 返回。stdout 属于宏请求/响应协议；向 stdout 写调试文本会破坏响应。stderr 仅是有容量限制的执行失败辅助信息。

## 目标与条件编译

宏程序为 `wasip1/wasm` 构建，处理的目标程序可能是 `linux/arm64`、`darwin/arm64` 等其他平台。宏实现需要根据目标选择输出时读取 `ctx.Target()`，不要使用运行宏机器的 OS，也不要用宏程序自己的 WASI 构建目标代替它。

下面的完整宏生成目标平台字符串。将文件加入首宏项目，在消费者中调用 `macros::targetName!()`：

```gox
package macros

use strconv;
use syntax::token;
use syntax::proc_macro as pm;

#[proc_macro]
pub func targetName(ctx pm::Context, input token::TokenStream) pm::Output {
    if !input.IsEmpty() { return pm::Failure("targetName takes no arguments", ctx.CallSpan()) }
    target := ctx.Target()
    value, err := token::NewLiteral(token::String, strconv::Quote(target.OS + "/" + target.Arch), ctx.DefSite())
    if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
    return pm::Success(token::New(value))
}
```

### 三处配置的区别

| 配置位置 | 影响 |
| --- | --- |
| 宏库源码中的 `cfg` 与宏依赖 feature | 选择构建这个 WASI 宏程序的源码与依赖 |
| 调用方的 `cfg` / `cfg_attr` | 在宏执行前选择是否保留该项、是否调用该宏 |
| `ctx.Features()`、`ctx.HasFeature(name)` | 查询本次被展开目标的冻结 feature |

生成语法中的 `cfg` 继续走普通配置规则：feature 按语法归属查对应构建计划，target/env 按输出阶段环境求值。复制输入会保留其来源；换一个诊断 Span 不会把 feature 归属改成另一个模块。

宏 SDK 没有 env 查询方法，WASI 环境也不传入宿主环境变量。宏需要的选项应写在输入语法、已声明的 feature 或显式项目配置中。普通 `cfg(env(...))` 的源级用法见[目标与环境](website-guide.md#目标与环境)。浏览器快照只接受构建时可冻结的来源。

## 宏项目测试与发布

### 登记与依赖

`[lib].proc_macro = true` 把该 lib 用作编译期宏。入口必须是根包中经 `cfg` 选择后存在的显式 `pub func`，非泛型、没有 receiver，且只带一个登记属性。入口签名使用 SDK 的真实类型；类型别名可按普通类型同一性检查。

函数式和属性宏以函数名导出；derive 以登记参数中的名字导出。普通 `pub` helper 不会成为消费者的运行时函数；消费者也不能把宏入口当普通目标函数调用。

| 依赖关系 | 构建用途 |
| --- | --- |
| 消费者 → 普通 lib | 目标程序的运行时依赖 |
| 消费者 → 过程宏 lib | 编译期展开，不生成目标 Go import |
| 过程宏 lib → 普通 lib | 构建 WASI 宏程序的依赖 |
| 过程宏 lib → 上游过程宏 | 构建宏自身时先展开上游宏 |

同一个普通库可同时参加宏角色与目标角色，分别使用自己的平台、feature 与依赖计划。宏 crate 构建期间不能调用自己登记的宏；用独立消费者或独立 example 验证它。普通 Go helper 可以与 `.gox` 放在同一宏包，像[可下载教程](https://goxide-lang.github.io/learn/?stage=procedural)那样由登记函数调用。

别名与 `pub use` 保留宏身份、定义点和 helper 所有者。同名 trait 与宏仍处于既有名字空间；运行库可重导出一个 `macros` 子入口，或为单个宏取不同公开名字。

### 测试可观察行为

每个宏至少覆盖正常结果与关键错误输入。改变表达式的宏还要检查求值次数、顺序和短路；派生宏要检查字段、变体、泛型约束及手写实现的冲突。测试消费者必须真正生成并执行 Go。

首宏各页提供可直接执行的 `main_test.gox`。在同一个项目上依次运行：

```sh
goxide build .
goxide run --offline --locked .
goxide test --offline --locked .
goxide gen --offline --locked -o ../generated-a .
goxide gen --offline --locked -o ../generated-b .
diff -ru ../generated-a ../generated-b
```

保持两次生成目录在作者项目之外，避免它们参与下一次源码发现。相同源码、依赖和配置应生成相同字节；遍历 Go map 时应排序后再输出。

调试时先看诊断中的宏入口、调用位置和展开链，再检查生成 Go。`goxide expand` 与 `gen --expanded` 当前用于单文件输入；完整项目使用 `gen -o` 检查生成结果，具体输入规则见 [CLI 参考](website-guide.md#cli-参考)。

### SDK、锁与缓存

发行宏库应包含清单、登记函数和 helper 源码，syntax 使用公开版本。开发时可显式依赖本地 syntax path；编译器仍会核对实际 SDK 协议内容，修改协议后需重新构建匹配的编译器。相邻 checkout 的修改不会自动替换公开 require。

`goxide.lock` 同时记录目标与宏角色，不另设宏锁。`--offline` 控制来源获取，`--locked` 控制锁定计划；缺少缓存时先进行正常构建。宏执行期间不能下载依赖或改变锁。

缓存保存宏构建产物，键包含源码与依赖、feature、工具链、编译器和 SDK 等实际输入；本地 path 内容改变会重新构建。每次调用使用新实例，宏全局变量不用于跨调用保存状态。源码发行由依赖系统重建宏，WASM 不是当前独立发布格式。

### 常见失败

| 诊断方向 | 先检查 |
| --- | --- |
| 宏未登记、签名不匹配 | `[lib].proc_macro`、根包、`pub`、登记属性与 canonical SDK 类型 |
| 找不到宏或运行库根 | 显式依赖、`use`、公开路径与 RuntimeRoot 的逻辑 module 键 |
| SDK 内容不匹配 | CLI 的构建入口、实际 require/replace 与宏计划选中的 SDK |
| 输出类别错误 | 表达式/类型/单项/列表位置，原 Fragment 是否放入正确槽 |
| helper 未知或歧义 | 对应 derive、helper 登记范围、可执行属性的限定路径 |
| 普通类型或可见性错误 | 展开结果仍需通过目标包的正常编译检查 |

## 执行环境与浏览器

宏使用受限 WASI command，每次调用创建新实例。编译器生成 launcher 和静态入口表；宏作者只写登记函数，不需要手写 `main`、WASI 导出或 JSON I/O。

### 宏的执行环境

宏可使用 SDK 提供的输入、上下文和 Go 运行时支持的普通计算。执行环境没有磁盘预打开目录、网络、进程启动或真实宿主环境；时钟、随机流与描述符使用确定的虚拟 profile。引入 profile 外的 WASI import 会在运行前报错。

| 资源 | 当前限制 |
| --- | --- |
| 单次调用 | 5 秒真实墙钟，包含实例初始化与响应验证 |
| 一次编译的宏执行 | 累计 60 秒，不含 Go 工具链构建时间 |
| guest 线性内存 | 256 MiB |
| WASM 产物 | 64 MiB |
| 单次请求、响应 | 各 8 MiB |
| 诊断及辅助 stderr | 合计 64 KiB |

token 树还有深度、节点和文本预算，见 [token 参考](https://goxide-lang.github.io/docs/?page=sdk-token)。panic、trap、超限或响应不完整都会形成宏执行诊断，失败产物不进入后续降低。

### 当前在线体验

Playground 运行随网站发布、核验过 SHA-256 的固定宏快照。可以编辑调用方并查看真实生成 Go，浏览相关宏源码、清单和 API；自定义宏请下载项目后在本地开发。

当前浏览器不构建或上传用户自己的宏，不下载任意依赖，也不执行生成的 Go。页面展示的程序输出是网站构建时的真实 CLI 运行结果。编译总超时为 70 秒；停止和超时会取消宏请求并请求终止 Worker。浏览器决定 Worker 实际停止 CPU 的时刻，因此结果截止与物理停止时刻分开处理。实现细节见 [RFC 0030](rfcs/0030-procedural-macros.md#6-宿主执行与资源边界)。

## 标准库与 serde 派生

标准协议由普通库声明，派生和打印使用库导出的过程宏。声明对应的 `std` 与 `serde` 依赖后，源码显式导入根。下面展示消费者中的协议用法；依赖版本使用本网站所选的公开库版本。

```gox
use std;
use serde;
use serde::json as serdejson;

#[derive(std::derive::Default, std::derive::Clone, std::derive::Debug,
         serde::derive::Serialize, serde::derive::Deserialize)]
type Config struct { Name string }

func show() {
    value := std::default_value[Config]()
    copy := std::Clone::clone(&value)
    std::println!("{:?}", copy)
    data, err := serdejson::Marshal(&copy, serde::Write[Config])
    if err != nil { panic(err) }
    restored, err := serdejson::Unmarshal(data, serde::Read[Config])
    if err != nil { panic(err) }
    std::println!("{:?}", restored)
}
```

宏生成普通 `impl`，也可以手写相同协议。Default 使用 `std::default_value[T]()`，Clone / Debug 使用 `std::Clone::clone(&value)` / `std::Debug::debug(&value)`，内容比较使用 `std::PartialEq::equals(left, right)`。serde 的 `Write[T]` 与 `Read[T]` 可直接作为 JSON 回调；它们不依赖按类型名拼出的辅助函数。

这些库既是可用功能，也是更完整的宏实例。自己的派生应先从本章小项目开始，再结合 `syn` 的字段/变体视图、RuntimeRoot 与普通 trait/impl 扩展到业务协议。

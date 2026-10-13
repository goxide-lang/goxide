# 新前端：词法、保留字与 AST 设计

状态：设计稿，待评审。本文替换现有 `internal/frontend` 的文本重写管线，是「重写前端」这项工作的第一步；只定义词法、保留字和 AST，不定义类型检查算法与 Go 降低细节。

## 1. 为什么重写

现有前端没有自己的语法树。每个特性是一个 `[]byte → []byte` 的 pass：用 `go/scanner` 找到新语法，改写成 Go 能解析的文本，再交给 `go/parser`，位置靠 `sourcemap` 逐级合成。结果是：

- 新语法能出现在哪些位置，取决于每个 pass 各自处理了哪些文本形状。`match` 在一元、复合字面量、后缀、多返回值位置要逐个接通。
- 类型信息只在文本已经变成 Go 之后才有，`Ok(v)` 这类需要期望类型的推断做不了。
- trait、泛型、LSP 都需要「解析一次、多处查询」的语义层，文本管线提供不了。

新前端的管线：

```text
源文件 ─ 词法 ─▶ token ─ 语法 ─▶ AST ─ 宏展开 ─▶ AST ─ 名字解析 / 类型检查 ─▶ 带语义表的 AST ─ 降低 ─▶ go/ast ─▶ Go 源码
```

`go/ast` 只作为输出格式。`go/types` 继续用于加载和检查原生 Go 包。

## 2. 已定的前提

| 项 | 决定 |
| --- | --- |
| 语法基线 | Go 超集。Go 的声明、语句、表达式、类型写法全部保留，合法的 Go 函数体可以粘贴进 `.gox` |
| Rust 糖 | 只收两项：`struct S { ... }` 声明、`\|x\| expr` 短闭包。它们与 Go 写法共用同一个 AST 节点，用 `Form` 字段记录书写形式 |
| 保留字 | 分层：硬保留、上下文关键字、未来预留（当前只 warning） |
| 未来预留方向 | `async` / `await`；Rust 风格语句与声明（`let`、`fn`、`loop`、`while`）；用户过程宏（`macro`） |
| 可见性 | `pub` 是唯一的可见性标记。Go 写法的声明暂时兼容「首字母大写即公开」；新语法只认 `pub`。见第 6 节 |
| 推断 | 能推导的就不要求用户写。短闭包的参数与返回类型都可省略 |
| 不预留 | 生命周期记号 `'a`、`move`、`dyn`、`static`、`crate`、生成器 / `try` 块、`yield` |
| 实现语言 | Go。AST 是自有类型，不嵌入 `go/ast` 节点 |

不嵌入 `go/ast` 的原因：`match`、`?`、路径、宏调用可以出现在任意表达式内部，`go/ast.Expr` 是封闭的节点集合，装不下这些子节点；另外自有节点才能统一携带节点 ID 和宏卫生信息。

不预留借用标注关键字的原因：Go 没有 move 语义，D08 已确认不引入全局 move；D20 的风险分析是 warning-first 的推断式分析，不需要签名级标注才能运行。将来若要给分析器提示，用属性表达（例如 `#[escapes(p)]`），属性是开放的扩展点，不占关键字，也不需要 `'a` 这种会与 Go rune 字面量 `'a'` 抢词法的记号。

## 3. 词法

以 Go 词法为基础，改动如下。

### 3.1 新增 token

| token | 用途 |
| --- | --- |
| `::` | 静态路径分隔：包、类型、变体、关联项、宏 |
| `=>` | match 分支；宏规则 |
| `?` | 后缀传播 |
| `#` | 属性起始 `#[...]`；`#![...]` 预留给文件级属性 |
| `$` | 仅在宏定义的匹配器和模板内有效 |
| `r#name` | 原始标识符，见 3.4 |

`!` 沿用 Go 的 token。宏调用由语法层识别：路径后紧跟 `!`，再紧跟 `(`、`[` 或 `{`。Go 中这个序列不是合法表达式，没有歧义。

`|` 沿用 Go 的 token。短闭包只在**表达式起始位置**识别（那里 Go 不允许二元 `|`）。

### 3.2 自动分号

保留 Go 规则，并补三条：

1. 行尾的 `?` 触发分号插入，使 `x := f()?` 可以独占一行。
2. 属性的右括号 `]` 之后不插入分号，使 `#[derive(...)]` 可以独占一行。词法器在看到 `#[` 时记录括号深度来判断。
3. match 分支之间用 `,` 分隔；最后一个分支后的 `,` 可省略，语法层在 `}` 前接受一个被自动插入的分号。

### 3.3 保留字

Go 的 25 个关键字不变。唯一的成员名例外：`impl Default for T` 内允许直接写 `func default() T`；`default` 在包级函数、其他 impl、trait 或普通绑定位置仍须转义。新增三层：

| 层 | 词 | 规则 |
| --- | --- | --- |
| 硬保留 | `enum` `match` `trait` `impl` `use` `pub` `mod` `unsafe` `declare` `Self` | 任何位置都不能作标识符 |
| 上下文关键字 | `mut` `ref` `as` `where` `self` `super` `in` `macro_rules` | 只在指定语法位置是关键字，其余位置是普通标识符 |
| 未来预留 | `async` `await` `let` `fn` `loop` `while` `macro` | 现在仍可作标识符，但给 warning 并建议改名或写 `r#name`；对应语法落地时升为硬保留 |

上下文关键字的生效位置：

| 词 | 生效位置 |
| --- | --- |
| `mut` | `&` 之后（`&mut T`、`&mut x`）；模式里 `ref` 之后；将来 `let` 之后 |
| `ref` | 模式的绑定位置 |
| `as` | `use` 声明内 |
| `where` | 泛型声明的签名之后、`{` 之前 |
| `self` | `trait` / `impl` 的成员体内。其他位置是普通标识符，因此 `func (self *T) M()` 这种 Go 写法不受影响 |
| `super` | 路径的第一段且后面紧跟 `::`，表示上一级模块。随 `mod` 体系启用，在此之前该位置报「尚未支持」 |
| `in` | 宏匹配器内；预留给将来的 `for x in` |
| `macro_rules` | 后面紧跟 `!` 时 |

以下词**不是**关键字，按普通名字解析：

- 属性名：`derive`、`cfg`、`default`、`serde` 等。
- 预导入名：`Option`、`Result`、`Some`、`None`、`Ok`、`Err`，以及 `i32` 这类基础类型别名。它们可以被遮蔽。
- `yield`：Go 的 range-over-func 惯用它作参数名，不预留。

### 3.4 原始标识符

`r#name` 表示名为 `name` 的标识符，绕过所有保留层，降低到 Go 时输出 `name`。主要用途是引用恰好叫 `match`、`use`、`mod` 的 Go 包或局部名：

```goxide
use r#match;

func find(r#mod int) bool {
    return r#match::Any(r#mod)
}
```

小写关键字不会与 Go 的导出 API 冲突（导出名首字母大写），冲突只来自包名和局部名，所以转义的实际使用频率很低。`Self` 是唯一的大写硬保留字。

## 4. AST 的共同约定

### 4.1 节点身份与位置

```go
type NodeID uint32          // 文件内稠密编号，解析时分配

type Span struct {
    Start, End uint32       // 源文件字节偏移，半开区间
}

type Node interface {
    ID() NodeID
    Span() Span
}
```

- 每个节点有 `NodeID` 和 `Span`。行列号由文件的行表按需计算，不存进节点。
- **语义信息不放在节点字段里**，放在以 `NodeID` 为键的旁表中：解析结果、类型、选中的 impl、常量值。AST 解析后不可变，编译器和 LSP 共用同一棵树、各查各的表。
- 宏展开产生的节点额外带 `ExpnID`，指向产生它的那次宏调用；诊断据此给出展开栈。这取代现有的 `sourcemap` 合成。

### 4.2 标识符与卫生

```go
type Ident struct {
    NodeID
    Span
    Name string
    Raw  bool        // 源码写作 r#name
    Ctx  SyntaxCtx   // 宏卫生标记；非宏产生的标识符为 0
}
```

名字解析按 `(Name, Ctx)` 比较，宏内部引入的局部名不会捕获调用点的同名变量。新前端保留源码名字与符号身份；`lower` 在生成 Go 时只为发生捕获冲突的局部符号分配不同名字。

### 4.3 错误恢复

每个类别有一个错误节点：`BadItem`、`BadStmt`、`BadExpr`、`BadType`、`BadPat`。语法错误不中断解析，后续阶段跳过错误节点。这是 LSP 的前提，从第一版就做。

### 4.4 注释与书写形式

- 注释按 Go 的做法收集为 `CommentGroup`，挂在 `File` 上，并关联到相邻声明作文档注释。
- `//go:build`、`//goxide:` 这类指令注释解析为 `Directive`，挂在所属文件或声明上。
- 糖和等价的 Go 写法共用节点，节点上的 `Form` 字段记录源码用的是哪种，供格式化器原样输出。降低阶段不看 `Form`。

### 4.5 预留的扩展槽

为了以后加能力时不改节点形状，下列字段从第一版就存在，即使当前只有一个合法取值：

| 字段 | 所在节点 | 当前取值 | 预留给 |
| --- | --- | --- | --- |
| `Attrs []*Attr` | 所有声明、字段、变体、参数、match 分支、语句 | 声明 / 字段 / 变体上已使用 | 语句级 `cfg`、分析器提示 |
| `Vis Visibility` | 所有声明、`use`、字段、impl 成员 | 未标注、`pub` | `pub(scope)` |
| `Mods FuncMods` | 函数、闭包、trait / impl 成员 | `Unsafe` | `Async`、`Const` |
| `Generics *GenericParams` 与 `Where *WhereClause` | 函数、类型、enum、trait、impl | 单约束泛型函数 | 泛型 trait、泛型 impl、多约束 |
| `Mut bool` | 引用类型、引用表达式、绑定模式 | 已使用 | `let mut` |
| `Root PathRoot` | 路径 | 无根 | `super::` |

## 5. 节点目录

五个封闭的类别：`Item`、`Stmt`、`Expr`、`Type`、`Pat`。每个类别是一个带未导出标记方法的接口，新增节点必须改动 `ast` 包。标 †的节点是预留位置，第一版解析器不产生。

### 5.1 文件与声明（Item）

```go
type File struct {
    Package    *Ident
    Attrs      []*Attr        // #![...]，预留
    Items      []Item
    Comments   []*CommentGroup
    Directives []*Directive
}
```

| 节点 | 语法 | 要点 |
| --- | --- | --- |
| `UseDecl` | `use a::b as c;` `use a::*;` `pub use ...` | `Tree` 字段是 use 树：路径、可选别名、glob；预留 `use a::{b, c}` 分组 |
| `ModDecl` | `mod child;` | 预留内联体 `mod m { ... }` |
| `FuncDecl` | `func (r T) Name[T C](...) R { ... }` | Go 函数与方法；`Recv` 仅用于 Go 写法的方法；`Body` 为空表示无体声明 |
| `TypeDecl` | `type A = B`、`type S struct{...}`、`struct S {...}` | `Form` 区分 Go 写法和 `struct` 糖；别名用 `Alias bool` |
| `EnumDecl` | `enum E[T] { A, B(int), }` | 变体见下 |
| `TraitDecl` | `trait T { func m() Self; type Item }` | 成员是 `TraitItem`：方法签名（可带默认体）或关联类型 |
| `ImplDecl` | `impl T for U { ... }`、`impl U { ... }` | `Trait` 为空表示固有 impl；成员是 `FuncDecl` 或关联类型绑定 |
| `DeclareDecl` | `declare func p::F(...)`、`declare impl T for U;` | 包一个无体的 `FuncDecl` / `ImplDecl` / `TypeDecl` |
| `MacroRulesDecl` | `macro_rules! name { (...) => {...}; }` | 见 5.6 |
| `MacroItem` | 声明位置的 `name!(...)` | 展开后由展开结果替换，原节点保留在展开表里 |
| `ConstDecl` / `VarDecl` | Go 的 `const` / `var`，含分组和 `iota` | 与 Go 相同 |
| `BadItem` | — | 包含 `.gox` 中不接受的 `import` 声明，带专门诊断 |

变体：

```go
type Variant struct {
    Attrs  []*Attr
    Name   *Ident
    Kind   VariantKind   // Unit、Tuple、Struct†
    Fields []*Field      // Tuple：仅类型；Struct†：名字加类型
    Disc   Expr          // †显式判别值，预留
}
```

`struct` 糖：`struct Point { x int; pub y int }` 与 `type Point struct { ... }` 产生同一个 `TypeDecl`，`Form` 不同。字段语法、标签、嵌入与 Go 的 struct 相同，另外每个字段可带 `pub`。两种写法的可见性规则不同，见第 6 节。

### 5.2 类型（Type）

| 节点 | 语法 |
| --- | --- |
| `PathType` | `int`、`pkg::T`、`Result[T, E]`、`Iterator[Item = int]`；泛型实参列表里允许关联类型绑定 |
| `RefType` | `&T`、`&mut T` |
| `PointerType` | `*T` |
| `SliceType` `ArrayType` `MapType` `ChanType` `FuncType` `StructType` `InterfaceType` | 与 Go 相同 |
| `SelfType` | `Self` |
| `ProjectionType` | `Self.Item`、`T::Item` |
| `ParenType` | `(T)` |
| `MacroType` | 类型位置的 `name!(...)` |
| `BadType` | — |

`&T` 与 `*T` 是不同节点。前者是 Goxide 引用（带读写权限），后者是 Go 指针；二者是否同表示由降低决定，语法层不合并。

### 5.3 表达式（Expr）

Go 的表达式节点全部保留：`Ident`、`BasicLit`、`CompositeLit`、`KeyValue`、`ParenExpr`、`SelectorExpr`、`IndexExpr`（含多下标的泛型实例化）、`SliceExpr`、`TypeAssertExpr`、`CallExpr`、`StarExpr`、`UnaryExpr`、`BinaryExpr`、`FuncLit`。

新增：

| 节点 | 语法 | 要点 |
| --- | --- | --- |
| `PathExpr` | `pkg::F`、`E::V`、`Result[int, error]::Ok`、`T::Abc` | 段可带泛型实参；与 `SelectorExpr`（`.`，值上的字段和方法）严格区分 |
| `MatchExpr` | `match x { P if g => e, ... }` | 可出现在任何表达式位置；分支体是表达式或块 |
| `TryExpr` | `x?` | 后缀，优先级同调用 |
| `RefExpr` | `&x`、`&mut x` | Go 的取地址也用这个节点，`Mut` 为假 |
| `UnsafeExpr` | `unsafe f(p)` | 前缀，作用于紧随的一元表达式 |
| `MacroExpr` | `path!(...)`、`path![...]`、`path!{...}` | 参数是未解析的 `TokenTree` |
| `QualifiedSelectorExpr` | `x.m::<Trait>` | 限定分派，见下 |
| `AwaitExpr`† | — | 预留，形式未定 |
| `BadExpr` | — | |

限定分派有三种写法，只有第二种需要专门节点：

| 写法 | AST | 何时使用 |
| --- | --- | --- |
| `x.m(args)` | `CallExpr` 包 `SelectorExpr` | 默认写法。`x` 的类型上只有一个可见的 `m` 时直接可用 |
| `x.m::<Trait>(args)` | `CallExpr` 包 `QualifiedSelectorExpr` | 多个 trait 提供同名方法时指明用哪一个 |
| `Trait::m(x, args)` | `CallExpr` 包 `PathExpr` | 与上一种等价的函数式写法 |

```go
type QualifiedSelectorExpr struct {
    X     Expr
    Sel   *Ident
    Trait Type      // ::< > 内恰好一个 trait 路径，可带泛型实参
}
```

不带调用的 `x.m::<Trait>` 是方法值。同名方法有多个候选而用户写了 `x.m(args)` 时，类型检查报歧义并列出候选，建议改写成第二种。`::<` 只在选择器之后出现，泛型实参在其他位置一律用 `[T]`。

闭包：`FuncLit` 同时表示 `func(a int) int { ... }` 和 `|a| a + 1`。

```go
type FuncLit struct {
    Form   FuncForm    // GoFunc 或 Bar
    Mods   FuncMods
    Params []*Param    // Bar 形式下 Param.Type 可为空
    Result Type        // Bar 形式下可为空
    Body   *Block      // 块体
    Expr   Expr        // Bar 形式的表达式体；与 Body 二选一
}
```

match 分支：

```go
type MatchArm struct {
    Attrs []*Attr
    Pat   Pat
    Guard Expr      // 可为空
    Body  Expr      // 表达式体
    Block *Block    // 块体；与 Body 二选一
}
```

短闭包省略的类型按以下顺序推断，都得不到时才报错并要求标注：

1. 期望类型：闭包作为实参、赋值右侧、返回值时，取对应位置的函数类型。
2. 闭包体：参数在体内的用法能唯一确定类型时采用；返回类型取体表达式或各 `return` 的类型。

推断只在闭包所在的语句内进行，不做跨语句的全局推断。

构造 `Ok(v)`、`Some(x)` 在 AST 中就是普通的 `CallExpr`，callee 是 `Ident`。把它解析成 `Result[T, E]::Ok` 是类型检查阶段按期望类型做的事，语法不需要特殊节点。

### 5.4 语句（Stmt）

Go 的语句节点全部保留：赋值、短声明、自增自减、表达式语句、发送、`go`、`defer`、`return`、`break` / `continue` / `goto` / `fallthrough`、块、`if`、`switch`、类型 `switch`、`select`、`for`、`range`、带标签语句、声明语句、空语句。

- 没有 `MatchStmt`。语句位置的 `match` 是表达式语句，其表达式为 `MatchExpr`。「所有分支都终止则 match 终止」由终止性分析处理，写完 match 后不必再补一个 `return`。
- 语句位置的宏调用同理，是表达式为 `MacroExpr` 的表达式语句；宏展开后可以替换为多条语句。
- 预留：`LetStmt`†、`LoopStmt`†、`WhileStmt`†。

### 5.5 模式（Pat）

| 节点 | 语法 |
| --- | --- |
| `WildPat` | `_` |
| `BindPat` | `x`、`ref x`、`ref mut x`；预留 `x @ P` 的子模式字段 |
| `LitPat` | `1`、`"a"`、`true`、`-1` |
| `PathPat` | `E::V`、`None`、命名常量 |
| `TuplePat` | `E::V(p, q)`、`Some(x)` |
| `StructPat`† | `E::V { a, b: p, .. }`，随 struct 变体启用 |
| `OrPat` | `A \| B` |
| `RangePat`† | `1..=9` |
| `BadPat` | — |

单个标识符是绑定还是引用常量 / 单元变体，语法层无法区分，一律产生 `BindPat`，名字解析阶段再改判。

### 5.6 属性与宏

```go
type Attr struct {
    Inner bool         // #![...]
    Path  *Path        // derive、serde、cfg、default ...
    Args  *TokenTree   // 可为空；内容由属性自己的处理器解析
}

type TokenTree struct {
    Delim  Delim       // Paren、Bracket、Brace、None
    Tokens []TokenOrTree
}

type MacroRulesDecl struct {
    Vis   Visibility
    Name  *Ident
    Rules []*MacroRule // 每条：匹配器 TokenTree、模板 TokenTree
}
```

- 宏的参数和规则体在语法阶段只做括号配对，保存为 `TokenTree`。匹配器里的 `$x:expr`、重复 `$(...),*` 在宏展开阶段解析。
- 展开时按片段类别调用解析器的对应入口（表达式、语句、类型、声明、模式），得到的 AST 节点带 `ExpnID` 和新的 `SyntaxCtx`。
- 属性参数同样保存为 `TokenTree`。`derive` 和 `cfg` 由编译器内建处理器解析，其他属性名先解析路径再决定归属。

### 5.7 路径

```go
type Path struct {
    Root     PathRoot        // None；Super†
    Segments []*PathSegment
}

type PathSegment struct {
    Name *Ident
    Args *GenericArgs        // [T, U] 或 [Item = int]，可为空
}
```

`::` 用于静态项，`.` 用于值，沿用[静态路径合同](static-paths.md)。因此 `strings::Repeat(strings.Text, 2)` 中前一个 `strings` 是命名空间、后一个是局部值，两者在 AST 里就是不同节点，不需要靠解析结果消歧。

## 6. 可见性

`pub` 是可见性的唯一标记，AST 只记录源码里有没有写：

```go
type Visibility struct {
    Kind VisKind    // VisNone、VisPub；VisScoped† 预留给 pub(scope)
    Span Span
}
```

声明是否真的公开（有效可见性）由名字解析阶段算出，存在旁表里。规则按书写形式分两类：

| 书写形式 | 包含 | 规则 |
| --- | --- | --- |
| 新语法 | `struct` 糖及其字段、`enum`、`trait`、固有 `impl` 的成员、`use`、`mod`、`macro_rules!` | 只认 `pub`。首字母大小写与可见性无关：`struct Point {}` 是私有的 |
| Go 写法 | `func`、`type`、`var`、`const`、Go 写法 struct 的字段、Go 写法的方法 | 写了 `pub` 即公开；没写时，**兼容模式**下首字母大写视为公开 |

随所属声明走、不单独标注的成员：enum 的变体、trait 的方法和关联类型、trait impl 的成员。固有 `impl` 的成员逐个写 `pub`。

兼容模式是当前默认。将来提供严格模式关闭「大写即公开」，届时 Go 写法也只认 `pub`；开关的形式（文件属性、清单字段或版本切换）到时再定。现在就在大写的 Go 写法声明上写 `pub` 是允许的，没有 warning。

### 6.1 公开名到 Go 名的对应

Go 只按首字母判断导出，所以源码名和生成的 Go 名可能不同。Goxide 代码始终用源码名引用（`pkg::parse`）；Go 调用方使用生成的 Go 名。

| 情况 | 生成的 Go |
| --- | --- |
| 公开，名字已大写 | 原名 |
| `pub` + 小写名（函数、方法、类型、变量、常量） | 只生成大写名，包内引用一并改写，不保留小写副本。每个声明在 Go 里只有一个名字：函数身份唯一，没有包装帧；变量也不会出现两份存储 |
| 私有，名字大写（仅新语法会出现） | 加下划线前缀，如 `enum Mode` 生成 `_Mode`。Go 视其为不导出，且不会与用户自己的 `mode` 冲突 |

大写名由源码名首字母大写得到。得到的名字与包内已有声明冲突、或首字母无法大写（以 `_` 或无大小写的文字开头）时，在声明处报错。

## 7. 需要语法层处理的歧义

| 位置 | 处理 |
| --- | --- |
| `match x {` 与复合字面量 `x{` | 与 Go 的 `switch` / `if` 头部相同：头部表达式内禁止无括号的复合字面量 |
| `Result[int, error]::Ok` 与下标表达式 | 先按 `IndexExpr` 解析；其后出现 `::` 时把已解析部分重解释为带泛型实参的路径段 |
| 表达式起始的 `\|` | 只在一元表达式起始位置进入闭包解析 |
| `&mut x` | `&` 之后的 `mut` 若后面还跟着操作数起始 token，则是关键字 |
| `name!(` | 路径后紧邻 `!` 且其后紧邻定界符才是宏调用；`a != b` 由词法的最长匹配先成为 `!=` |
| 模式里的 `\|` | 模式语法独立于表达式语法，`\|` 在模式里是或模式 |
| `struct S {` | Go 中 `struct` 不能起始一个声明，无歧义 |
| `x.m::<Trait>(` | `::` 后紧跟 `<` 进入限定选择器，读一个类型后要求 `>`。类型语法内部不含裸 `>`，无需回溯 |
| trait / impl 成员 | 统一要求 `func` 起始。设计文档里省略 `func` 的写法（`read(p []byte) int`）不接受 |

## 8. 包布局与迁移

```text
internal/syntax/token     token 种类、保留字表、原始标识符
internal/syntax/lexer     词法器，含自动分号
internal/syntax/ast       本文定义的节点
internal/syntax/parser    递归下降解析器，带错误恢复
internal/syntax/printer   AST → .gox 源码（格式化器与往返测试的基础）
internal/expand           宏展开与卫生
internal/resolve          名字解析
internal/check            类型检查
internal/lower            AST → go/ast
```

迁移顺序：

1. `token`、`lexer`、`ast`、`parser`、`printer`。验收：仓库里现有的全部 `.gox` 文件（约 250 个）解析无错误节点，且 解析 → 打印 → 再解析 得到相同的树。这一步不依赖旧管线，可以立刻开始。现有文件里省略 `func` 的 trait 成员、依赖「大写即公开」的 `enum` / `trait` 需要先改写（补 `func`、补 `pub`）。
2. `lower` 的纯 Go 子集：不含任何扩展语法的 `.gox` 生成等价 Go，且生成的 Go 能编译运行。这一阶段**不做独立的 `check`**：纯 Go 子集的类型由 Go 编译器检查，独立类型检查随扩展特性（enum / match / `?` / 引用 / trait）一起落地，那时才有 Go 类型系统表达不了的东西要查。
3. 按特性接入：`use` 与路径 → enum / match / `?` → 宏 → 派生与 serde → 引用 → trait / impl。每个特性用 `.gox → .go` 的 golden 文件加运行结果验收；`check` 随这些特性逐步建立。
4. 新管线通过现有 `tests/acceptance` 和各示例后，CLI 一次性切换，删除 `internal/frontend` 及 `enums`、`matchexpr`、`propagate`、`refs`、`equality`、`protocols`、`printing`、`sourcemap` 等旧 pass。

迁移期间旧管线冻结，只修阻塞性 bug，不加新特性。两套管线不在同一次编译里混用。

### 8.1 管线接口

各阶段的输入输出固定为下列形状；包之间只通过这些类型连接，不互相引用内部状态。

```text
parser.ParseFiles(paths)          → map[string]*ast.File, []parser.Diagnostic
expand.Expand(files, resolver)    → expand.Result
resolve.Resolve(files, expand)    → resolve.Result, []resolve.Diagnostic
check.Check(files, resolve)       → check.Info, []check.Diagnostic
lower.Lower(files, check)         → map[string][]byte   // 每个 .gox 对应一份 Go 源码
```

三条跨阶段的约定：

1. **标识符身份**。`ast.SyntaxCtx` 是解析结果和查询结果两侧共用的键。展开器为模板绑定分配新的 `Ctx`，捕获片段保留调用点的 `Ctx`。`resolve` 的作用域表按 `(Name, Ctx)` 建键，不按拼写。
2. **宏生成的声明**。进入调用方包的声明集合，其名字按调用点解析，`pub` 表示调用包的公开。`resolve` 必须把它们当普通声明处理，不区分来源。
3. **诊断**。每个阶段返回自己的诊断列表，条目至少含 `Severity`、`Message`、`Span`（阶段输出的坐标系）和 `Origin`（可回溯到原始源文件与展开栈）。阶段之间不合并诊断，由调用方按 `Origin` 归并。

宏调用的识别属于展开阶段：`resolve` 遇到未展开的 `MacroExpr` 应按普通路径处理，不报"未定义"。

### 8.2 P2 宏与具体派生接入合同

宏在表达式、语句、类型、item 四种位置展开后进入同一条 `resolve → check → lower` 管线。多包展开保留完整的上下文、展开栈、来源与生成声明记录；生成的 Go 必须保留解析后的符号身份，调用点同名局部变量不能捕获定义点名字。

派生沿用 [RFC 0013](../rfcs/0013-derive.md) 与 [相等性合同](../rfcs/research/enum-equality-proposed.md) 的具体类型规则：`DefaultT() T`、`CloneT(*T) T`、`DebugT(*T) string`、`EqualsT(T, T) bool`；`Eq` 只登记资格。`resolve` 收集显式派生与手写协议并声明对应 helper，`check` 绑定真实类型、检查签名与唯一性，`lower` 验证字段协议并生成普通 Go。普通同名函数不构成协议登记；透明 alias 使用同一类型身份，新 defined 类型不继承协议。

P2 接通已有的具体手写 Default、PartialEq 和空 Eq impl。Default 按字段声明顺序构造，enum 只要求唯一默认变体的字段有 Default；Clone、Debug、PartialEq/Eq 按既定字段与容器规则处理。泛型派生、手写 Clone/Debug/serde 和完整 trait 后端不在此合同中新增语义。

serde 的 Serialize/Deserialize 按实际 `use` 绑定的 canonical owner 分别登记，owner 为 `github.com/goxide-lang/serde`。新管线直接调用该模块的协议，JSON 格式实现仍由 serde 模块提供。现有退出示例依赖的打印、Error 派生与单步错误转换按其已确认合同接通；不读取旧管线的元数据，也不调用旧 lowering pass。

## 9. 后续工作

本文只覆盖重写前端的第一步。其余几项修改意见各自独立，另行处理：

- 收缩项目工具链范围：`import-go`、vendor、构建脚本授权、workspace。
- 删除旧命名与旧格式的兼容检测。
- 语法人体工学：`Ok(v)` / `Some(x)` 的期望类型推断、穷尽 `match` 作为终止语句。新前端完成第 3 步后即可做。
- 用状态表和 golden 测试替换现有的验收文档体系。
- 把积压的小决定改为给出默认值。

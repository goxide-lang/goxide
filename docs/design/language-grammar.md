# Goxide 词法与文法

本文描述当前 `.gox` 前端的源语言：字节如何成为 token，token 如何组成声明、类型、表达式、语句和模式，以及哪些语法还停留在解析层。实现依据是 [token 定义](../../internal/syntax/token/token.go)、[词法器](../../internal/syntax/lexer/lexer.go)、[解析器](../../internal/syntax/parser/)和[语法 AST](../../internal/syntax/ast/ast.go)。AST 的职责与阶段接口见[前端规范](frontend-ast.md)，可运行能力见[当前能力状态](../mvp-gap-audit.md)。

这里的 EBNF 覆盖当前解析器的各类入口。名字是否存在、类型是否一致、引用是否可写、trait 是否满足和某个节点能否降低为 Go，由后续阶段检查。文法中的上下文条件与各节的实现边界一起构成阅读规则；单独满足产生式还不足以判定一个完整程序有效。

[RFC 0003](../rfcs/0003-declare-impl.md) 已确认移除 `declare impl`，但当前解析、检查和降低实现尚未删除。本文暂保留对应产生式以反映代码现状，不表示目标语言继续支持它；`declare func/type` 及 `declare` 关键字不在此次删除范围。

## 1. 记法与处理次序

```text
A = B C ;          先 B 后 C
A = B | C ;        二选一
A = [ B ] ;        可选
A = { B } ;        零次或多次
A = ( B | C ) D ;  分组
"text"             源码中的终结符
ε                  空序列
EOF                输入结束
```

产生式末尾的 `;` 是 EBNF 标点；引号内的 `";"` 才是源 token。`Identifier` 指 `IDENT` token，包含原始标识符。用 `? 条件 ?` 表示字符集合、前瞻条件等不能仅靠上下文无关产生式表达的约束。除词法规则外，空格和注释通常只负责分开 token；宏调用的紧邻要求是例外。

```text
source bytes
  -- Lex --> tokens + lexical diagnostics
  -- ParseFile --> syntax AST + syntax diagnostics
  -- configuration selection / Expand --> expanded AST
  -- ResolveExpanded --> bound names
  -- Check --> types, protocols, permissions
  -- Lower --> Go AST / formatted Go source
```

配置筛选要求先成功解析完整源文件，关闭的 `#[cfg(...)]` item 内仍须有合法语法。宏参数先保存为 token tree；其匹配器和模板在展开阶段解释。完整编译还会检查生成的 Go 包。

`ParseExpr`、`ParseType`、`ParsePattern`、`ParseStmt`、`ParseItem` 是片段入口：读取一个相应节点，跳过末尾分号后必须到达 EOF。`ParseFile` 要求文件首先具有 `package` 子句，顶层不能直接写可执行语句。

## 2. 字符、注释与位置

源文件使用 UTF-8。无效编码、NUL、文件中间的 BOM 均产生词法诊断；文件开头的 UTF-8 BOM 会被跳过。标识符按 Unicode 码点扫描，数字字面量中的数字采用 ASCII。

```text
WhiteSpace   = " " | "\t" | "\r" | "\n" ;
LineComment  = "//" { ? 非换行字符 ? } ;
BlockComment = "/*" { ? 尚未遇到第一个 */ 的字符 ? } "*/" ;
```

块注释不嵌套。空格、制表符和回车直接跳过；换行参与分号插入。行注释后的换行和块注释内的第一个换行同样可以结束语句。其他 Unicode 空白字符没有被当作普通空白接受。

词法器保留注释 token；解析器将其移入 `File.Comments`，把相邻文档注释挂到声明或字段。`//go:`、`//goxide:` 前缀另存为 `Directive`，具体指令的含义由对应阶段处理。

```text
Span = [Start, End)          原始 UTF-8 字节偏移，右端不包含
Token = Kind × Text × Span × Implicit × Raw
```

普通 token 的 `Text` 保留源码拼写；原始标识符的 `Text` 去掉 `r#`，`Span` 仍覆盖整个原文。自动分号的 `Start == End`、`Implicit == true`。AST 的 `NodeID` 在解析完成后按遍历顺序重新编号；`Span` 和 `File.LineStarts` 用于恢复诊断位置，不能把字节偏移直接当作字符列数。

## 3. 标识符与关键字

```text
Letter        = "_" | ? unicode.IsLetter 接受的码点 ? ;
UnicodeDigit  = ? unicode.IsDigit 接受的码点 ? ;
IdentifierText = Letter { Letter | UnicodeDigit } ;
RawIdentifier = "r#" IdentifierText ;
Identifier    = ? 非硬关键字的 IdentifierText，或 RawIdentifier ? ;
```

`r#match`、`r#type` 在语法层都是标识符，名称分别为 `match`、`type`。`r#` 后必须立即跟一个合法标识符起始字符。转义只改变源级识别；若名称最终仍是 Go 保留字，降低阶段会给出带位置的诊断，例如局部变量 `r#type`。

硬关键字如下，大小写敏感：

```text
break       case        chan        const       continue
default     defer       else        fallthrough for
func        go          goto        if          import
interface   map         package     range       return
select      struct      switch      type        var

enum        match       trait       impl        use
pub         mod         unsafe      declare     Self
```

`import` 被词法器识别，但 `.gox` 声明解析会拒绝它，包导入写 `use`。Go 文件仍使用自己的 Go 语法。

以下单词仍产生 `IDENT`，仅在特定位置具有特殊含义：

| 单词 | 特殊位置 |
| --- | --- |
| `mut` | 引用类型或引用表达式的 `&` 后，以及模式中的 `ref` 后 |
| `ref` | 模式绑定的起始位置 |
| `as` | `use` 的别名子句 |
| `where` | 带泛型参数的声明的约束子句 |
| `self` | trait / impl 方法的显式接收者参数，由语义检查识别 |
| `macro_rules` | 后接 `!` 的宏定义起始位置 |
| `super` | 记录为上下文词；当前拒绝以未转义 `super::` 开头的路径 |
| `in` | 记录为上下文词；当前没有对应的循环产生式 |

上下文识别要求 `Raw == false`，因此 `r#mut` 等总按普通名字读取。`&mut` 在后面没有可开始操作数的 token 时表示“对名字 `mut` 取引用”；`&mut x` 才读取为可变引用。类型位置采用相同的前瞻规则。

`async await let fn loop while macro` 是未来保留词：目前仍能作为标识符解析，未转义使用会产生警告。它们没有对应的当前语言特性。`true`、`false`、`nil`、`iota`、基本类型名和预声明函数名也都不是硬关键字，由名字解析和类型检查确定含义。

`default` 有两个局部例外：属性名 `#[default]`，以及直接写作 `impl Default for T` 时的成员名 `func default()`。后者只适用于该单段 trait 路径且没有 Go 风格接收者的成员；其他函数名位置仍需遵守标识符规则。

## 4. 字面量与标点

### 4.1 数值

```text
DecDigit  = "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" ;
BinDigit  = "0" | "1" ;
OctDigit  = "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" ;
HexDigit  = DecDigit | "a" | "b" | "c" | "d" | "e" | "f"
                    | "A" | "B" | "C" | "D" | "E" | "F" ;
DecDigits = DecDigit { [ "_" ] DecDigit } ;
BinDigits = BinDigit { [ "_" ] BinDigit } ;
OctDigits = OctDigit { [ "_" ] OctDigit } ;
HexDigits = HexDigit { [ "_" ] HexDigit } ;

DecimalInt = "0" | ( "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" )
                    { [ "_" ] DecDigit } ;
BinaryInt  = "0" ( "b" | "B" ) [ "_" ] BinDigits ;
OctalInt   = "0" ( "o" | "O" ) [ "_" ] OctDigits
           | "0" [ "_" ] OctDigits ;
HexInt     = "0" ( "x" | "X" ) [ "_" ] HexDigits ;
Integer    = DecimalInt | BinaryInt | OctalInt | HexInt ;

DecExponent = ( "e" | "E" ) [ "+" | "-" ] DecDigits ;
BinExponent = ( "p" | "P" ) [ "+" | "-" ] DecDigits ;
DecimalFloat = DecDigits "." [ DecDigits ] [ DecExponent ]
             | DecDigits DecExponent
             | "." DecDigits [ DecExponent ] ;
HexMantissa  = HexDigits [ "." [ HexDigits ] ] | "." HexDigits ;
HexFloat     = "0" ( "x" | "X" ) [ "_" ] HexMantissa BinExponent ;
Float        = DecimalFloat | HexFloat ;
Imaginary    = ( DecDigits | Integer | Float ) "i" ;
```

下划线只能隔开连续数字，或紧接显式基数前缀且后面有数字。十六进制小数必须带 `p`/`P` 指数；二、八进制尾数不能含小数点。指数中的数字始终是十进制。`0x_.8p0` 不满足前缀后必须有数字的限制。

`08` 作为整数非法，`08i` 和 `08.0` 分别按虚数和十进制浮点形式读取。`-12` 是一元运算符与整数字面量两个 token。数值范围、常量转换和溢出由类型检查及 Go 检查处理。

### 4.2 字符和字符串

```text
RuneLiteral        = "'" RuneUnit "'" ;
InterpretedString  = "\"" { StringUnit } "\"" ;
RawString          = "`" { ? 除反引号外的合法源字符 ? } "`" ;
StringLiteral      = InterpretedString | RawString ;
BasicLiteral       = Integer | Float | Imaginary | RuneLiteral | StringLiteral ;
RuneUnit           = ? 一个合法普通 rune 或下述 strconv.Unquote 接受的转义 ? ;
StringUnit         = ? 一个合法普通字符或下述 strconv.Unquote 接受的转义 ? ;
```

解释型字符串和 rune 字面量不允许未转义换行；原始字符串允许跨行，反斜杠没有转义意义。`RuneUnit` 是一个普通 Unicode 字符或一个合法转义，解码后必须恰好一个 rune；`StringUnit` 是普通字符或转义。普通字符不能是当前引号、反斜杠或换行。

转义由 `strconv.Unquote` 校验：支持 `\a \b \f \n \r \t \v \\`、与当前引号相应的引号转义、三位八进制、`\x` 后两位十六进制、`\u` 后四位十六进制、`\U` 后八位十六进制。字节转义的值和 Unicode 标量值必须合法。原始字符串中的回车按 Go 的字符串解码规则处理。

### 4.3 运算符与分隔符

```text
+    -    *    /    %    &    |    ^    <<   >>   &^
+=   -=   *=   /=   %=   &=   |=   ^=   <<=  >>=  &^=
&&   ||   <-   ++   --   ==   <    >    =    !    !=
<=   >=   :=   ...
(    [    {    ,    .    )    ]    }    ;    :
::   =>   ?    #    ~    $
```

词法器优先选择最长运算符，最多三个字节。`..` 没有独立 token，结构模式中的 `..` 是两个 `.`；`...` 是单个 token。`@` 不属于词法表。`$` 可进入 token tree，但在普通表达式或语句中没有独立含义。

## 5. 自动分号与列表结束

若上一个非注释 token 属于下列集合，换行或 EOF 会插入一个隐式分号：

```text
EndToken = IDENT | INT | FLOAT | IMAG | CHAR | STRING
         | "break" | "continue" | "fallthrough" | "return"
         | "++" | "--" | ")" | "]" | "}" | "?" | "Self" ;

insert(previous, boundary) =
  ";"  if previous ∈ EndToken and boundary ∈ {newline, EOF}
  ε    otherwise
```

结束外层属性的 `]` 是例外：词法器识别 `#[...]` / `#![...]` 的括号栈，在该 `]` 后不插入分号，因此可以将属性单独放在声明上一行。括号本身不会普遍禁用换行分号；函数参数、泛型实参和复合字面量跨行时仍需正确使用逗号。

本文复用两个结束规则：

```text
Semi  = ";" ;
Semis = { Semi } ;
End   = Semi | ? 下一 token 是 "}" 或 EOF 时的 ε ? ;
EndBeforeParen = Semi | ? 下一 token 是 ")" 时的 ε ? ;
EndBeforeBrace = Semi | ? 下一 token 是 "}" 时的 ε ? ;
PayloadFieldEnd = "," | EndBeforeBrace ;
```

分组声明还允许最后一条 specification 在 `)` 前省略分号。逗号列表各自定义尾逗号，不能把 `End` 任意代入列表。

| 位置 | 中间分隔 | 最后一项后的行为 |
| --- | --- | --- |
| 文件 item、块内语句、trait / impl 成员 | 显式或隐式分号 | `}` / EOF 前可省略 |
| `type (...)`、`const (...)`、`var (...)` | 分号 | `)` 前可省略 |
| 普通 struct 字段、interface 成员 | 分号 | `}` 前可省略 |
| enum 变体 | 逗号 | 同行 `}` 前可省略；换行前通常需尾逗号 |
| enum 命名 payload 字段 | 逗号或分号 | `}` 前可省略 |
| match arm | 逗号 | `}` 前可省略；还接受紧靠最终 `}` 的一个隐式分号 |
| 宏规则 | 分号 | 最后一条在 `}` 前可省略 |

因此 `match x { A => 1; B => 2 }` 非法，最后一臂后手写 `;` 也非法。`else` 须跟在前一块的 `}` 同一行；否则 `}` 后的隐式分号已经结束 `if`。

## 6. 文件、声明、可见性与导入

实现：[items.go](../../internal/syntax/parser/items.go)、[parser.go](../../internal/syntax/parser/parser.go)。

```text
SourceFile = "package" Identifier End { Semi | Item End } EOF ;
Item       = Attributes [ "pub" ] ItemCore ;
ItemCore   = [ "unsafe" ] FunctionDecl
           | TypeDecl | StructDecl | ConstDecl | VarDecl
           | UseDecl | ModuleDecl | EnumDecl | TraitDecl | ImplDecl
           | DeclareDecl | MacroDefinition | MacroInvocation ;

IdentifierList = Identifier { "," Identifier } ;
ExpressionList = Expression { "," Expression } ;

UseDecl    = "use" UsePath [ "::" "*" ] [ "as" Identifier ] ;
UsePath    = Identifier { "::" Identifier } ;
ModuleDecl = "mod" Identifier ;
```

`pub` 必须位于属性之后、声明及其 `unsafe` 修饰符之前。语法层使用同一个前缀入口，最终可见性还受声明种类约束：trait 成员及 trait impl 成员继承 trait 可见性，不能再写 `pub`；固有 impl 成员可以有自己的可见性。enum 变体继承 enum 可见性。

新语法由 `pub` 控制公开；Go 风格的 `func`、`type`、`var`、`const` 声明还保留首字母大写即公开的约定。`pub` 小写名在 Go 侧生成一个相应的公开名字，不生成额外转发包装。具体名字映射见[前端规范](frontend-ast.md)与[公开 API 设计](../rfcs/0015-public-api.md)。

`use fmt;`、`use encoding::json;`、`use source::Thing as Alias;`、`use source::*;` 均有文法入口；`as _` 用于副作用导入。路径各段不能写类型实参。分组 `use a::{b, c}`、根 `super::` 和内联 `mod child { ... }` 当前在解析阶段拒绝。外部 `mod child;` 由包加载器关联真实子包；单独解析一份文件不会凭空建立该包。

`use crate::child;` 使用同一条路径文法，`crate` 的所属 target 含义在包加载与名字绑定时确定。自动库名、依赖 alias、标准库 fallback 与真实 Go 子包解析遵循 [RFC 0028](../rfcs/0028-crate-roots.md)；文法不直接决定依赖来源或包可见性。

### 6.1 普通类型与值声明

```text
TypeDecl   = "type" ( TypeSpec | "(" { Semi | DecoratedTypeSpec EndBeforeParen } ")" ) ;
TypeSpec   = Identifier [ GenericParameters ] [ "=" ] Type [ WhereClause ] ;
DecoratedTypeSpec = Attributes [ "pub" ] TypeSpec ;
StructDecl = "struct" Identifier [ GenericParameters ] [ WhereClause ] StructBody ;

ConstDecl  = "const" ( ValueSpec | "(" { Semi | ValueSpec EndBeforeParen } ")" ) ;
VarDecl    = "var"   ( ValueSpec | "(" { Semi | ValueSpec EndBeforeParen } ")" ) ;
ValueSpec  = Attributes [ "pub" ] IdentifierList [ Type ] [ "=" ExpressionList ] ;
```

分组中的相邻 specification 必须有分号；仅最后一个可直接接 `)`。普通 `type Name T` 定义新类型，`type Name = T` 定义别名，`struct Name { ... }` 是具名 struct 声明语法糖。`struct Name` 后直接接字段块，不重复 `struct`。

`var` 至少给出类型或初值。`const` 的第一条 specification 必须有初值，后续省略初值时也不能另写类型；省略形式继承前面的常量表达式。空分组可以解析。

`WhereClause` 只在已经读取 `GenericParameters` 时启用。普通 `type` 的约束写在右侧类型之后，`struct` 语法糖的约束写在字段块之前。普通 `type` 后的 `[` 首先试作数组类型：若整个数组类型能成功解析，则数组解释优先；只有失败才按类型参数列表读取。例如 `type A [N]int` 是数组定义。

### 6.2 函数、参数与结果

```text
FunctionDecl = "func" [ Receiver ] Identifier [ GenericParameters ]
               Signature [ WhereClause ] [ Block ] ;
Receiver     = Parameters ;
Signature    = Parameters [ Result ] ;
Result       = Type | Parameters ;
Parameters   = "(" [ Parameter { "," Parameter } [ "," ] ] ")" ;
Parameter    = Attributes [ IdentifierList ] [ "..." ] Type ;
```

该规则同时覆盖 `func f(x int) int`、`func (x T) M()`、匿名参数 `func(int, string)`、同类型参数组 `func(x, y int)`、命名结果和可变参数。参数名与类型的区分采用前瞻和待定名字组；`x, y int` 在 AST 中成为一组名字及同一个类型。`x []T` / `x [N]T` 与 `T[A]` 的区分还查看方括号之后能否继续一个类型。

接收者数量、可变参数必须在最后、命名与未命名参数的合法组合等由语义及 Go 检查限制。文法能读取无函数体声明；它是否属于可使用的外部声明仍由后续阶段决定。带泛型参数的函数才把结果位置的未转义 `where` 识别为约束起始词；普通函数可以用名为 `where` 的类型作结果。

### 6.3 enum

```text
EnumDecl = "enum" Identifier [ GenericParameters ] [ WhereClause ]
           "{" [ Variant { "," Variant } [ "," ] ] "}" ;
Variant  = Attributes Identifier [ TuplePayload | StructPayload ] ;
TuplePayload = "(" [ PayloadField { "," PayloadField } [ "," ] ] ")" ;
PayloadField = Attributes Type ;
StructPayload = "{" { Semi | NamedPayloadField PayloadFieldEnd } "}" ;
NamedPayloadField = Attributes IdentifierList Type ;
```

三种变体分别写作 `End`、`Value(int)`、`Record { name string; count int }`。命名 payload 相邻字段必须用逗号或分号分开，最后一组可直接接 `}`。此处字段没有 `pub`、匿名嵌入或 Go 字段 tag 的产生式。

构造沿用路径调用或复合字面量：`E::Value(3)`、`E::Record{name: "a", count: 1}`；单元变体使用 `E::End`。类型与变体身份由名称解析区分。用户 enum 的密封接口、指针变体和 nil 规则见 [enum 表示规范](enum-representation.md)。

### 6.4 trait、impl 与 declare

```text
TraitDecl = "trait" Identifier [ GenericParameters ] [ WhereClause ]
            "{" { Semi | TraitMember End } "}" ;
TraitMember = Attributes ( [ "unsafe" ] FunctionDecl | AssociatedTypeDecl ) ;
AssociatedTypeDecl = "type" ( AssociatedTypeSpec
                     | "(" { Semi | DecoratedTypeSpec EndBeforeParen } ")" ) ;
AssociatedTypeSpec = Identifier [ GenericParameters ]
                     [ [ "=" ] Type ] [ WhereClause ] ;

ImplDecl = "impl" [ GenericParameters ] Type [ "for" Type ] [ WhereClause ]
           "{" { Semi | ImplMember End } "}" ;
ImplMember = Attributes [ "pub" ]
             ( [ "unsafe" ] FunctionDecl | AssociatedTypeBinding ) ;
AssociatedTypeBinding = "type" Identifier [ GenericParameters ] "=" Type [ WhereClause ] ;

DeclareDecl = "declare" ( DeclaredFunction | DeclaredImpl | DeclaredType ) ;
DeclaredFunction = Attributes [ "pub" ] [ "unsafe" ] "func" [ Receiver ]
                   UsePath [ GenericParameters ] Signature [ WhereClause ] ;
DeclaredImpl = Attributes [ "pub" ] "impl" [ GenericParameters ]
               Type [ "for" Type ] [ WhereClause ] ;
DeclaredType = Attributes [ "pub" ] "type" Identifier
               [ GenericParameters ] [ [ "=" ] Type ] [ WhereClause ] ;
```

`impl T` 是固有实现，`impl P for T` 是 trait 实现。trait / impl 方法必须有 `func` 前缀；成员主体里不能随意嵌套普通顶层 item。实例方法通过第一参数 `self Self`、`self &Self`、`self &mut Self` 表示接收者；没有 `self` 的成员为静态方法。函数解析器还可读取 Go 风格接收者，实际 trait 接收者规则由检查器约束。

当前运行子集使用非泛型 trait、具体具名类型的 impl、无默认值的关联类型、关联类型绑定、静态方法和单 trait 的函数泛型约束。trait 默认方法、泛型 trait/impl、多 trait 约束和泛型关联类型仍在该解析文法内，但语义检查会拒绝。trait 的分组关联类型可以解析，当前检查同样拒绝；impl 的关联类型必须用单项 `type Item = T` 绑定，不能仅写 `type Item` 或改成分组声明。

`declare impl P[Item = int] for Native;` 当前仍可用于 Go 侧适配，但已按 [RFC 0003](../rfcs/0003-declare-impl.md) 决定移除；上面的 `DeclaredImpl` 产生式和对应实现待删。`declare func`、`declare type` 具有解析入口，尚未完成相应降低。`declare func` 和 `declare impl` 不能带主体。解析器复用普通类型声明入口，因而还可读取分组 `declare type (...)` 及 `declare struct ...`；这些同样没有当前可运行的 declare 类型实现。

## 7. 类型、泛型与投影

实现：[types.go](../../internal/syntax/parser/types.go)。

```text
Type     = TypeAtom { "|" TypeAtom } ;
TypeAtom = NamedType | SelfType | PointerType | ReferenceType
         | SliceType | ArrayType | MapType | ChannelType
         | FunctionType | StructType | InterfaceType
         | "~" TypeAtom | "(" Type ")" | TypeMacro ;

NamedType = TypePath { Projection } ;
SelfType  = "Self" [ "::" Identifier [ GenericArguments ] { Projection } ] ;
TypePath  = TypeSegment { "::" TypeSegment } ;
TypeSegment = Identifier [ GenericArguments ] ;
Projection  = ( "." | "::" ) Identifier [ GenericArguments ] ;

PointerType   = "*" TypeAtom ;
ReferenceType = "&" [ "mut" ] TypeAtom ;
SliceType     = "[" "]" TypeAtom ;
ArrayType     = "[" ( Expression | "..." ) "]" TypeAtom ;
MapType       = "map" "[" Type "]" TypeAtom ;
ChannelType   = ( "chan" [ "<-" ] | "<-" "chan" ) TypeAtom ;
FunctionType  = "func" Signature ;
TypeMacro     = TypePath "!" TokenTree ;

GenericArguments = "[" [ GenericArgument { "," GenericArgument } [ "," ] ] "]" ;
GenericArgument  = [ Identifier "=" ] Type ;
GenericParameters = "[" [ GenericParameter { "," GenericParameter } [ "," ] ] "]" ;
GenericParameter  = IdentifierList [ Type ] ;
WhereClause = "where" WherePredicate { "," WherePredicate } [ "," ] ;
WherePredicate = TypeAtom ":" TypeAtom { "+" TypeAtom } ;
```

`Type` 中的 `|` 构造类型集合，不是 enum 声明或运行时联合值。`~` 表示底层类型项；它们的使用位置受类型检查限制。`*T | U` 解析为 `(*T) | U`。引用的 `mut` 使用第 3 节的前瞻规则。

`Self::Item` 与 `T::Trait::Item` 表示关联类型投影，`P[Item = int]` 表示关联类型绑定。`Self.Item` 明确报错。普通 `pkg.T` 能形成点式投影 AST，Goxide 的包路径和静态路径使用 `::`；能解析点式语法并不自动建立包导入关系。

类型参数的名字分组和函数参数类似：`[T, U any]` 为两个参数共享约束，`[T, U]` 保留无显式约束的参数。空 `[]` 也能形成参数或实参 AST，实际有效性由后续检查决定。`where` 至少有一条谓词；冒号必需。文法容纳 `T: P + Q`，当前实现的可运行约束见上节；普通 `where T: any` 也不能据此假定已经有对应降低。

### 7.1 struct 和 interface 类型

```text
StructType = "struct" StructBody ;
StructBody = "{" { Semi | StructField EndBeforeBrace } "}" ;
StructField = Attributes [ "pub" ]
              ( IdentifierList Type | EmbeddedType ) [ StringLiteral ] ;
EmbeddedType = Type ;

InterfaceType = "interface" "{" { Semi | InterfaceElement EndBeforeBrace } "}" ;
InterfaceElement = Attributes ( Identifier Signature | Type ) ;
```

普通 struct 的相邻字段和 interface 的相邻成员都要求分号，最后一项可省略。`StringLiteral` 是 Go 字段 tag。解析器可把较宽的 `Type` 读作嵌入字段；Go 对允许的嵌入类型形状仍有语义限制。

interface 方法写 `Read(p []byte) (n int, err error)`，没有 `func` 前缀；trait 方法写 `func read(self &Self, ...)`。interface 还容纳嵌入类型、`~int | ~string` 等类型集合。参数、interface 成员等位置的属性能进入 AST，但只有被具体语义支持的位置和属性名可通过后续检查。

## 8. 表达式

实现：[expr.go](../../internal/syntax/parser/expr.go)。

### 8.1 优先级和结合性

| 优先级（高到低） | 形式 | 结合方式 |
| --- | --- | --- |
| 后缀 | 调用、索引、切片、选择、路径、类型断言、宏调用、`?`、复合字面量 | 从左到右继续读取 |
| 一元 | `+ - ! ^ * & &mut <- unsafe` | 向右递归 |
| 5 | `* / % << >> & &^` | 左结合 |
| 4 | `+ - | ^` | 左结合 |
| 3 | `== != < <= > >=` | 左结合 |
| 2 | `&&` | 左结合 |
| 1 | `||` | 左结合 |

```text
Expression = OrExpression ;
OrExpression = AndExpression { "||" AndExpression } ;
AndExpression = Comparison { "&&" Comparison } ;
Comparison = Sum { ( "==" | "!=" | "<" | "<=" | ">" | ">=" ) Sum } ;
Sum = Product { ( "+" | "-" | "|" | "^" ) Product } ;
Product = Unary { ( "*" | "/" | "%" | "<<" | ">>" | "&" | "&^" ) Unary } ;
Unary = ( "+" | "-" | "!" | "^" | "*" | "<-" | "unsafe" ) Unary
      | "&" [ "mut" ] Unary
      | BarClosure { Postfix }
      | PrimaryExpression ;
```

`x++` / `x--` 是语句，不能放进表达式。赋值和 `:=` 也是语句。`?` 是后缀错误传播，不是三元条件运算符。比较运算符虽然能连续形成 AST，`a < b < c` 仍需满足每一步的类型要求。

### 8.2 操作数与后缀

```text
PrimaryExpression = Operand { Postfix } ;
Operand = Identifier | BasicLiteral | "(" Expression ")"
        | "Self" | FunctionOperand | MatchExpression | AnonymousTypeOperand ;
FunctionOperand = "func" Signature [ Block ] ;
AnonymousTypeOperand = SliceType | ArrayType | MapType | ChannelType
                     | StructType | InterfaceType ;

Postfix = Arguments | IndexOrInstantiation | Slice
        | "." Identifier | "." "(" ( Type | "type" ) ")"
        | "::" Identifier | "::" "<" Type ">"
        | "!" TokenTree | "?" | LiteralValue ;

Arguments = "(" [ Expression { "," Expression } [ "..." ] [ "," ] ] ")" ;
IndexOrInstantiation = "[" Expression { "," Expression } [ "," ] "]"
                     | GenericArgumentsWithBinding ;
GenericArgumentsWithBinding = ? 至少含一个顶层 Identifier = Type 的 GenericArguments ? ;
Slice = "[" [ Expression ] ":" [ Expression ] [ ":" [ Expression ] ] "]" ;

LiteralValue = "{" [ LiteralElement { "," LiteralElement } [ "," ] ] "}" ;
LiteralElement = LiteralAtom [ ":" LiteralAtom ] ;
LiteralAtom = Expression | LiteralValue ;
```

后缀还具有以下上下文条件：

- `LiteralValue` 前面的表达式必须可解释为类型；裸 `{...}` 仅能出现在嵌套复合字面量元素中。`[]T{{x: 1}, {x: 2}}` 使用该省略形式。
- `x[T]` 先形成 `IndexExpr`；结合名字和类型信息再确定是索引还是实例化。遇到 `Result[int, error]::Ok` 时，解析器把前面的索引形状重新解释为带类型实参的路径。
- 带 `Item = T` 的方括号只能用于可形成路径的前缀。多个普通索引、三索引切片缺失必要上界等情况可形成较宽 AST，由后续检查诊断。
- `x.(type)` 仅在 type switch guard 里有有效语义；一般类型断言使用 `x.(T)`。
- `::<P>` 必须接在值选择器后，如 `x.read::<P>()`，其中 `P` 必须形成 trait 路径。它不是通用的尖括号泛型实参。
- 宏后缀要求前缀可形成路径，且路径末尾、`!`、开括号三者在原始字节位置上相邻。`m!(x)` 有效，`m ! (x)`、`m! (x)` 不构成宏调用。
- `<-chan T` 首先识别为接收通道类型操作数，其他 `<-x` 按接收表达式读取。

`func(...) {...}` 是 Go 风格函数字面量；没有主体的 `func(...)` 在表达式位置可以形成类型操作数，供转换或内建调用使用。`T(x)` 在语法层统一为调用，由类型信息决定是函数调用还是类型转换。

### 8.3 控制头中的花括号

`if`、`for`、`switch` 的控制头以及 `match` 的被匹配表达式和 guard 使用受限表达式入口。该入口让具名表达式后面的 `{` 优先开始控制块：

```text
if ready { work() }           "{" 开始 if 块
match value { ... }           "{" 开始 arm 列表
match (T{}) { ... }           括号内仍允许具名复合字面量
```

圆括号、调用参数、索引内部重新允许复合字面量；匿名 struct、array、slice、map 类型之后的复合字面量也可识别。额外括号解决语法歧义，但当前检查器仍拒绝控制头中的 `match` 表达式。

### 8.4 短闭包与 unsafe 的解析形式

```text
BarClosure = ( "||" | "|" [ BarParameters ] "|" ) BarBody ;
BarParameters = BarParameter { "," BarParameter } [ "," ] ;
BarParameter = Attributes Identifier [ TypeAtom ] ;
BarBody = Block | Expression | Type Block ;
```

短闭包参数必须有名字，类型可省略。`|x| T{}` 优先读取为返回复合字面量的表达式主体；只有表达式解析失败且 `Type Block` 能成功解析时，才采用显式结果类型形式，如 `|x int| int { return x }`。当前短闭包与 `unsafe expr`、`unsafe func` 仅完成解析，降低阶段拒绝；可运行函数字面量使用 `func` 形式。Go 的 `unsafe` 包调用是另一条已有普通包调用路径，不依赖这些扩展产生式。

## 9. match 与模式

实现：[pattern.go](../../internal/syntax/parser/pattern.go)、[match 解析](../../internal/syntax/parser/expr.go)。

```text
MatchExpression = "match" HeadExpression "{"
                  [ MatchArm { "," MatchArm } [ "," | LastImplicitSemi ] ] "}" ;
MatchArm = Attributes Pattern [ "if" HeadExpression ] "=>" ( Block | Expression ) ;
HeadExpression = ? 使用控制头花括号规则解析的 Expression ? ;
LastImplicitSemi = ? 下一个 token 为最终 "}" 的隐式分号 ? ;

Pattern = PatternAtom { "|" PatternAtom } ;
PatternAtom = "_" | ReferenceBinding | LiteralPattern | PathPattern
            | TuplePattern | StructPattern ;
ReferenceBinding = "ref" [ "mut" ] Identifier ;
LiteralPattern = BasicLiteral | ( "+" | "-" ) ( Integer | Float | Imaginary )
               | "true" | "false" | "nil" ;
PatternPath = TypePath | "Self" "::" TypeSegment { "::" TypeSegment } ;
PathPattern = PatternPath ;
TuplePattern = PatternPath "(" [ Pattern { "," Pattern } [ "," ] ] ")" ;
StructPattern = PatternPath "{" Semis StructPatternBody "}" ;
StructPatternBody = ε | RestPattern [ "," ] Semis
                  | PatternField ( "," Semis StructPatternBody | Semis ) ;
PatternField = Identifier [ ":" Pattern ] ;
RestPattern = "." "." ;
```

一个没有类型实参的单段名字最初保存为 `BindPat`：它可以是新绑定，也可能由后续解析结果确定为单元变体。带路径或实参的无 payload 模式保存为 `PathPat`。`Self` 必须跟 `::` 变体路径，不能作为独立模式。未转义 `_` 是通配符，未转义 `true` / `false` / `nil` 按字面量模式读取。

`E::Pair(x, ref mut y)` 是 tuple payload 模式；`E::Record{name, count: n, ..}` 是命名 payload 模式。字段简写 `name` 等价于 `name: name` 的绑定形状。命名字段之间使用逗号；末尾 `..` 可带一个逗号，后面不能再有字段。tuple / struct 子模式递归使用 `Pattern`，因此 `A(x | y)` 在语法层可表示，但绑定一致性和匹配类型仍需检查。

当前没有独立 tuple 模式 `(x, y)`、括号模式、`x @ pattern`、范围模式 `a..b` 或 `&pattern`。`match &value` 的 `&` 属于被匹配表达式，payload 的引用绑定写 `ref` / `ref mut`。深层 payload 引用模式仍有检查限制。

enum 匹配在运行时分派一次，同一变体的 arm 按源顺序测试 guard。nil 不是合法 enum 值，分派前检查并 panic；`_` 不接住 nil。`OrPat`、guard、覆盖关系、引用槽位与分支结果的语义见[前端规范](frontend-ast.md)和[枚举表示](enum-representation.md)。arm 的 `{ ... }` 是普通语句块，不能凭文法给最后一个表达式附加隐式返回语义。

## 10. 语句与控制流

实现：[stmt.go](../../internal/syntax/parser/stmt.go)。

```text
Block = "{" { Semi | Statement End } "}" ;
Statement = Attributes StatementCore ;
StatementCore = SimpleStatement | Block | IfStatement | ForStatement
              | SwitchStatement | SelectStatement
              | GoStatement | DeferStatement | ReturnStatement | BranchStatement
              | LabeledStatement | LocalDeclaration ;

SimpleStatement = Expression
                | ExpressionList AssignmentOperator ExpressionList
                | Expression ( "++" | "--" )
                | Expression "<-" Expression ;
AssignmentOperator = "=" | ":=" | "+=" | "-=" | "*=" | "/=" | "%="
                   | "&=" | "|=" | "^=" | "<<=" | ">>=" | "&^=" ;
LocalDeclaration = ConstDecl | VarDecl | TypeDecl | StructDecl ;
LabeledStatement = Identifier ":" Statement ;
GoStatement = "go" CallExpression ;
DeferStatement = "defer" CallExpression ;
CallExpression = ? 顶层形状为 CallExpr 或 MacroExpr 的 Expression ? ;
ReturnStatement = "return" [ ExpressionList ] ;
BranchStatement = ( "break" | "continue" ) [ Identifier ]
                | "goto" Identifier | "fallthrough" ;
```

空分号形成 `EmptyStmt`。表达式列表只有接赋值符时才是合法简单语句。`go` / `defer` 在语法阶段已经要求顶层调用形状；宏产物随后还需满足相应调用要求。`goto` 必须有标签；标签存在性、跳转范围、`break` / `continue` / `fallthrough` 的位置属于后续检查。

### 10.1 if 与 for

```text
IfStatement = "if" [ [ SimpleStatement ] Semi ] HeadExpression Block
              [ "else" ( IfStatement | Block ) ] ;

ForStatement = "for" [ HeadExpression | ForClause | RangeClause ] Block ;
ForClause = [ SimpleStatement ] Semi [ HeadExpression ] Semi [ SimpleStatement ] ;
RangeClause = [ Expression [ "," Expression ] ( "=" | ":=" ) ]
              "range" HeadExpression ;
```

`for {}`、条件循环、三段式循环和 Go 风格 range 均使用 `for`。range 最多两个迭代变量，赋值符只允许 `=` / `:=`。三段式 `for` 的 post 不能是 `:=`。range 的源类型和可用迭代变量数由类型检查确定；当前仍拒绝 range 赋值目标以及函数迭代器源中的 `?`。

### 10.2 switch 与 select

```text
SwitchStatement = ExpressionSwitch | TypeSwitch ;
ExpressionSwitch = "switch" [ [ SimpleStatement ] Semi ] [ HeadExpression ]
                   "{" { Semi | ExpressionCase } "}" ;
ExpressionCase = ( "case" ExpressionList | "default" ) ":" CaseBody ;

TypeSwitch = "switch" [ [ SimpleStatement ] Semi ] TypeSwitchGuard
             "{" { Semi | TypeCase } "}" ;
TypeSwitchGuard = [ Identifier ":=" ] PrimaryExpression "." "(" "type" ")" ;
TypeCase = ( "case" Type { "," Type } | "default" ) ":" CaseBody ;

SelectStatement = "select" "{" { Semi | CommunicationCase } "}" ;
CommunicationCase = ( "case" SimpleStatement | "default" ) ":" CaseBody ;
CaseBody = { Semi | Statement End } ;
```

`CaseBody` 在下一 `case`、`default` 或当前 `}` 前结束。解析器通过 guard 的 AST 形状区分两种 switch：`x.(type)`，或单左值、单右值的 `:= x.(type)`。语法扫描先读较宽的简单语句，后续阶段限制 guard 左侧必须符合 Go 的绑定规则。

select 的 case 在解析层也先接受简单语句，后续检查要求通道发送或接收等有效通信形式。普通 switch 的 tag 可省略。类型 case 中的 `nil` 首先被读作名字形状，随后按 type switch 的语义处理。

## 11. 属性与条件配置

```text
Attributes = { Attribute } ;
Attribute = "#" "[" AttributePath { AttributeToken } "]" ;
AttributePath = AttributeName { "::" AttributeName } ;
AttributeName = Identifier | "default" ;
AttributeToken = TokenTree | ? 非分隔符 token ? ;
```

属性参数是 token tree 或未分组 token 序列，允许 `#[name(...)]`、`#[name = "value"]` 等形状。括号必须匹配；`#![...]` 会产生“文件级内部属性保留”的诊断。属性本身能挂到 item、字段、变体、参数、语句、match arm 等节点；展开或检查阶段验证属性名与位置，未知属性不会被静默忽略。

当前条件配置使用以下参数子文法：

```text
CfgAttribute = "#" "[" "cfg" "(" Predicate [ "," ] ")" "]" ;
CfgAttr = "#" "[" "cfg_attr" "(" Predicate ","
          AttributeBody { "," AttributeBody } [ "," ] ")" "]" ;
AttributeBody = AttributePath { AttributeToken } ;
CfgQuery = "cfg" "!" "(" Predicate [ "," ] ")" ;

Predicate = "feature" "(" StringLiteral [ "," ] ")"
          | "env" "(" StringLiteral [ "," StringLiteral ] [ "," ] ")"
          | TargetPredicate "(" StringLiteral [ "," ] ")"
          | ( "all" | "any" ) "(" [ Predicate { "," Predicate } [ "," ] ] ")"
          | "not" "(" Predicate [ "," ] ")" ;
TargetPredicate = "target_os" | "target_arch" | "target_platform"
                | "target_endian" | "target_pointer_width" ;
```

`cfg` / `cfg_attr` 当前只能筛选完整顶层 item，包括宏定义、宏调用和展开出的顶层 item；不能筛选单个字段、arm 或函数内语句。`cfg!` 生成布尔常量，不删除普通 `if` 的另一分支。配置参数解析会忽略隐式分号；这项处理不适用于所有宏和所有属性。

`feature` 接受一个合法且已登记的 feature 名；原生 Go 显式 feature 配置可提供相应集合。`env("K")` 查询是否存在，`env("K", "V")` 比较精确字符串，键不能为空或含 `=` / NUL。五个 target 谓词只接受当前目标表中的字符串值，`target_pointer_width` 也写字符串。`all()` 为真，`any()` 为假；`not` 恰好一个参数。`test`、`cgo`、`debug_assertions` 和其他未开放 target 谓词会诊断。配置上下文与构建目标见 [RFC 0022](../rfcs/0022-project-toolchain.md)，实现见 [config.go](../../internal/expand/config.go)。

派生属性沿用通用属性文法，当前入口包括：

| 属性 | 参数形状与位置 |
| --- | --- |
| `derive` | 协议路径的逗号列表；内建 `Default`、`Clone`、`Debug`、`PartialEq`、`Eq`、`Error`，以及 serde 所属的 `Serialize` / `Deserialize` |
| `default` | enum 的默认变体标记；与 `Default` 派生配合 |
| `serde` | `rename = "..."`、`value = 整数`、`skip`、`default`、`deny_unknown_fields` 的受限组合，位置与类型形状决定允许项 |
| `error`、`source`、`from` | `Error` 派生使用的描述、来源和转换标记，按 struct / enum 形状检查 |

这些名称不是通用用户自定义属性注册接口。精确字段和变体限制由[派生检查](../../internal/check/derive.go)、[serde 降低](../../internal/lower/serde.go)及 [Error 降低](../../internal/lower/error.go)定义；尤其 payload 变体的 serde wire 映射、变体字段属性仍有未实现部分。

## 12. 声明宏与 token tree

### 12.1 外层语法

```text
TokenTree = "(" { TokenTreeElement } ")"
          | "[" { TokenTreeElement } "]"
          | "{" { TokenTreeElement } "}" ;
TokenTreeElement = TokenTree | ? 非开闭分隔符、非 EOF 的 token ? ;

MacroInvocation = MacroPath "!" TokenTree ;
MacroPath = ( Identifier | "Self" ) [ GenericArguments ]
            { "::" TypeSegment } ;
MacroDefinition = "macro_rules" "!" Identifier
                  "{" { Semi | MacroRule EndBeforeBrace } "}" ;
MacroRule = TokenTree "=>" TokenTree ;
```

宏调用有第 8 节的字节紧邻要求；定义起始的 `macro_rules!` 由 item 解析器识别，不使用调用后缀的紧邻判定。宏规则之间必须有分号，最后一条可直接接 `}`。匹配器和模板各自可用三种括号，嵌套括号必须配对。词法阶段已经插入的分号会保留在 token tree 中。

调用可出现在表达式、语句、类型和 item 位置；展开后重新按位置验证产物。一个语句位置的宏可以展开语句序列。语法阶段只检查 token tree 结构，`$` 捕获和重复由展开器解释。

### 12.2 匹配器和模板子文法

```text
MatcherTerms = { MatcherTerm } ;
MatcherTerm = LiteralToken | MatcherGroup | Capture | MatcherRepeat | "$" "$" ;
MatcherGroup = "(" MatcherTerms ")" | "[" MatcherTerms "]" | "{" MatcherTerms "}" ;
Capture = "$" Identifier [ ":" FragmentKind ] ;
FragmentKind = "expr" | "ident" | "tt" | "type" | "item" ;
MatcherRepeat = "$" "(" MatcherTerms ")" [ SeparatorToken ] RepeatOperator ;
RepeatOperator = "*" | "+" | "?" ;
SeparatorToken = ? 一个非重复算子的非分组 token ? ;
LiteralToken = ? 此上下文中不以 $ 起始、且不是分隔符的 token ? ;

TemplateTerms = { TemplateTerm } ;
TemplateTerm = LiteralToken | TemplateGroup | "$" Identifier
             | TemplateRepeat | CallerPath | "$" "$" ;
TemplateGroup = "(" TemplateTerms ")" | "[" TemplateTerms "]" | "{" TemplateTerms "}" ;
TemplateRepeat = "$" "(" TemplateTerms ")" [ SeparatorToken ] RepeatOperator ;
CallerPath = "$" "caller" "::" Identifier [ TokenTreeBrackets ]
             { "::" Identifier [ TokenTreeBrackets ] } ;
TokenTreeBrackets = "[" { TokenTreeElement } "]" ;
```

省略捕获类别时默认为 `expr`。`ident` 捕获一个标识符，`tt` 捕获一个 token 或一个带括号组，`type` 和 `item` 分别捕获合法类型及声明片段。`stmt` / `pat` 不是当前可声明的捕获类别；内部的 `statements` 产物类别服务于调用位置，不是额外的 `$x:statements` 语法。

每条规则里的捕获名必须唯一，`caller` 是模板调用点路径保留名。相邻可变长捕获不能具有模糊边界；`expr` / `type` / `item` 捕获后若直接再写捕获，需要分隔符。重复体必须消费输入。`*` 表示零次或多次，`+` 表示至少一次，`?` 表示零次或一次；可选尾逗号写 `$(,)?`。

模板重复必须有捕获决定长度，同层多个捕获长度必须一致，引用的捕获层级必须匹配。`$$` 输出字面 `$`。第一条完整匹配全部输入的规则被选择；未消费完、没有匹配规则、未知类别或非法重复都会给出定义点和调用点诊断。实现见 [matcher.go](../../internal/expand/matcher.go)、[template.go](../../internal/expand/template.go)。

模板自由名按定义点解析，捕获片段保留原有绑定，每次展开的局部绑定获得新的语法上下文；`$caller::path` 明确切换到调用点。展开出的 item 放入调用包。这些绑定规则见[宏 RFC](../rfcs/0011-macros.md)，与 token 文法分开执行。

### 12.3 内建宏

| 入口 | 当前参数与位置 |
| --- | --- |
| `print!("...", args...)` | 直接语句；这里的 `args...` 表示若干逗号分隔实参，源码不接受 Go 的展开 `...` |
| `println!("...", args...)` / `println!()` | 直接语句；输出末尾换行 |
| `cfg!(predicate)` | 布尔表达式，参数使用上节配置文法 |
| `go_result!(call)` / `go_result!(T, call)` | 表达式或直接语句，将 Go 多返回值调用适配为 Result |
| `go_result_keep!(call)` / `go_result_keep!(T, call)` | 同一调用形状，使用保留值的适配规则 |

打印格式字符串必须是字面量，只支持 `{}`、`{:?}`、`{{`、`}}`，占位数必须等于后续参数数。`cfg`、`go_result`、`go_result_keep` 不能被同名用户宏定义替代。普通内建函数，如 `len`、`append`，仍使用普通调用文法，不带 `!`。

## 13. 解析、检查和设计占位的边界

| 类别 | 当前情况 | 直接依据 |
| --- | --- | --- |
| 普通 Go 结构 | 本文列出的类型、函数、复合字面量、语句和控制流均有解析入口；包导入改用 `use` | [parser 测试](../../internal/syntax/parser/parser_test.go)、[表达式测试](../../internal/syntax/parser/expr_test.go) |
| enum、match、引用、`?`、宏、派生 | 有完整编译路径；具体限制按对应语义检查执行 | [能力状态表](../mvp-gap-audit.md)及其端到端测试 |
| 外部 `mod` 与 `use` | 完整包图中可解析、检查和生成；内联 `mod`、分组 use 和 `super::` 当前拒绝 | [包检查](../../internal/check/packages.go)、[多包验收](../../internal/compile/project_e2e_test.go) |
| trait / impl / 关联类型 | 非泛型 trait、具体 impl、单 trait 函数约束和 Go 侧 `declare impl` 已实现；`declare impl` 已决定移除、实现待删 | [trait 检查](../../internal/check/traits.go)、[静态协议验收](../../internal/lower/p4_static_test.go)、[移除合同](../rfcs/0003-declare-impl.md) |
| 泛型 trait/impl、多 trait bound、GAT、trait 默认方法 | 相关形状能解析，当前检查拒绝 | [trait 检查](../../internal/check/traits.go)、[约束检查](../../internal/check/trait_types.go) |
| bar 闭包、`unsafe` 修饰符/表达式、`declare func/type` | 能解析，当前降低拒绝 | [表达式降低](../../internal/lower/expr.go)、[声明降低](../../internal/lower/decl.go) |
| `let`、`loop`、`while`、`await`、范围模式 | AST 留有节点类型，当前解析器没有相应语言产生式 | [AST](../../internal/syntax/ast/ast.go)、[语句](../../internal/syntax/parser/stmt.go)、[模式](../../internal/syntax/parser/pattern.go) |
| async、用户过程宏、用户可编程 derive | 设计或保留能力，当前没有可运行语法入口 | [决策](decisions.md)、[宏 RFC](../rfcs/0011-macros.md)、[派生 RFC](../rfcs/0013-derive.md) |

错误恢复生成的 `BadItem`、`BadStmt`、`BadExpr`、`BadType`、`BadPat` 用于保留后续结构，不属于有效程序文法。`ParseFile` 即使有错误也返回恢复后的树，调用者必须查看 diagnostics；片段入口还会拒绝未消费完的 token。检查器和降低器会拒绝错误节点，不把恢复成功当作编译成功。

维护这份文法时，先核对解析器与对应正反例，再核对完整编译路径。新增 AST 节点、增加一个 token 或写入 RFC，分别只证明结构、词法或设计决定；只有连通展开、解析、检查、降低并实际运行的行为才进入可运行能力表。

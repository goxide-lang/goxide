# 额外语法工具包：syntax、quote 与 syn

状态：独立 `github.com/goxide-lang/syntax` module 首批已实现手写 Go 的 `token` 和 `proc_macro` 两包，包含词法树、调用内引用表、冻结 Context、Output、静态分派及 JSON 编解码。syn/quote 后续作为同 module 的普通工具包实现；此批不实现它们，也不采用 Goxide 自举生成发行代码。[RFC 0030](../rfcs/0030-procedural-macros.md) 的源码登记、三类调用接线与 helper 已完成本地联调；依赖角色与受限宿主继续按该篇推进。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D16：独立 syntax 仓库/module；token 是唯一公共 TokenStream 类型所在包；proc_macro 是依赖 token 的 SDK；首批两包手写 Go。 |
| 暂定 | T14：后续 syn/quote 的 AST、ToTokens、模板插值与具体 API；如以过程宏实现 quote!，另设宏源码项目。 |
| 已实现 | 内部 token 直接解析、源码登记与 canonical SDK 验签、三类调用及 helper、冻结引用桥、诊断位置与语义来源分离；真实宏源码经测试 SDK command 展开，目标 Go build/run/test 通过。 |
| 未定 | U11：RFC 0030 的角色构建、实际 SDK 协议内容匹配、RuntimeRoot 供应与受限过程宏宿主待接入验收。 |

状态编号见[决策登记](decisions.md)。Rust 只用于参考语法工具与过程宏的分层，safe 标准库另以 [Go std](standard-library.md) 为基础设计。

## 1. 包与依赖方向

| 包或组件 | 职责 | 依赖与本批范围 |
| --- | --- | --- |
| `syntax/token` | 唯一公共 `token.TokenStream`、token 数据、解析和构造原语；位置与不透明上下文的公共表示 | 只依赖 Go 标准库；首批手写 Go |
| `syntax/proc_macro` | 宏作者 SDK：Context、Output、诊断与协议编解码 | 只依赖 token 与 Go 标准库；首批手写 Go |
| `syntax/syn` | 把 Goxide token 解析成公共语法树，提供遍历与重建 | 后续普通工具包；不依赖编译器 internal |
| `syntax/quote` | ToTokens、组合与构造 token 的普通运行时操作 | 后续普通工具包；不因导入而执行宏 |
| 编译器 internal | 公共 token 与私有 AST 的转换桥、宏启动器、输出验证与执行宿主 | 可使用 token/SDK；首批不依赖 syn/quote |

上述 `syntax/…` 均省略了完整 module 前缀 `github.com/goxide-lang/`。SDK 与后续工具统一使用 `token.TokenStream`，不各自定义同名镜像类型。syntax 不反向导入编译器；它也不包含 CLI、浏览器宿主或编译器已解析 IR。共享词法/解析代码时，只按真实公共消费者提取，不能让独立工具包通过 internal 取得编译器状态。

普通工具里的 `AST -> AST` 函数不因处理语法而成为过程宏。只有编译器在展开时调用用户程序，才进入过程宏机制。导入 token、syn 或 quote 不自动登记或执行编译期扩展，也不启用构建钩子。

## 2. 发布与使用

syntax 是一个 Go module，token、proc_macro 及后续普通 syn/quote 包随该 module 发布。它与编译器独立选版、发布，不要求双方版本号相同；公共协议兼容性按 RFC 0030 拟议的实际协议内容核验，不仅比较 import path、自报标志或版本字符串，也不提供旧协议兼容分支。

当前编译器通过 `go.mod` 的 `replace github.com/goxide-lang/syntax => ../syntax` 使用相邻 checkout，本地联调保留两仓库的目录关系。syntax 发布后固定实际公开的版本并移除该替换。

开发中的 Goxide 宏项目显式声明 `[dependencies.syntax] path = "../syntax"`；发行时改用 `go = "github.com/goxide-lang/syntax"` 和实际公开的版本。示例导入为：

```goxide
use syntax::token;
use syntax::proc_macro as pm;
```

普通 Go 工具可以直接导入 `github.com/goxide-lang/syntax/token`。首批两包直接编译其手写 Go 源码，不需要先运行 Goxide，也不在安装、导入或构建时自动生成文件。后续改用 Goxide 编写工具及生成 Go 发行方式需要另行确定。

## 3. 后续 syn/quote 与 quote! 的启动依赖

syn 解析 Goxide 语法，不复用 Rust AST，也不把编译器私有结构作为公共 API。可按消费者需要提供表达式、类型、声明、文件解析，以及 token 输出、只读遍历和显式重建；不要求首个消费者携带完整编译器。

quote 的普通运行时 API 构造 token 数据。模板与 `#value` 插值形式可以参考 Rust，但具体语法和 ToTokens 协议仍需设计。插值结果不立即执行所描述的代码；字符串插值默认生成字符串字面量，需要代码时走显式语法解析并处理错误。ToTokens 调用仍受普通类型、借用、impl 和 unsafe 规则约束。

若以后以过程宏实现 `quote!`，宏实现必须处于独立的 Goxide 源码项目/module；可以与 syntax 放在同一仓库，但拥有独立清单和构建角色。它依赖 token/builder 直接构造输出，不调用自身 `quote!` 来构建自己。普通 syntax module 不反向依赖或重导出该宏项目；否则宏项目使用 syn，而 syntax 又依赖宏项目，会形成源码构建环。消费者需要两者时显式依赖，不先增加包装门面。

Rust 的 quote、syn 与 proc_macro2 展示了工具可在编译器外运行的分层；Goxide 不据此承诺相同 API 或实现方式。[quote](https://docs.rs/quote/latest/quote/)、[syn](https://docs.rs/syn/latest/syn/)、[proc_macro2](https://docs.rs/proc-macro2/latest/proc_macro2/)

## 4. 语法、卫生与权限边界

解析成功只说明语法可表示，不说明名字已绑定、类型正确、trait 已实现、match 穷尽、借用合法或 unsafe 已授权。修改后的语法在真正编译时重新检查，不能复制旧节点的语义结果作为证明。syn 不自行补充第三方 declare、查询全盘 impl 或取得额外私有访问能力。

| 信息 | 用途与限制 |
| --- | --- |
| 诊断 span | 把生成语法关联到输入、调用或定义的源码位置；不改变原源码、名字绑定、依赖归属或权限 |
| 不透明绑定上下文 | 保留定义点、调用点及输入名字的卫生身份；不能由整数、字符串或文件路径伪造 |
| 可信来源与片段身份 | 由宿主签发和校验；不能从外部布尔字段恢复类型、借用或授权证明 |

公共 API 可以保留和转交已有片段携带的不透明上下文。普通 Go 调用方、反序列化数据和缓存对象同样需要协议验证；验过 Go 方法集不代表上下文真实。绑定和来源不能由文本显示往返恢复，指定诊断 span 也不能恢复它们。具体默认定义点与显式调用点规则见 [RFC 0030](../rfcs/0030-procedural-macros.md) 和[卫生宏设计](macros.md)。

普通工具可以显式执行“解析 → 修改 → 构造 → 输出文件”，再单独运行下一次编译。文件 I/O 由工具自身显式调用，语法包不会扫描目录、下载依赖或改写使用方源码；这也不隐含自动构建钩子。重新编译生成的文本时按普通源码解析，不承诺保留卫生身份或已经过期的语义证明。

## 5. 验收边界

首批库测试已覆盖词法与分组、显式/隐式分号、Fragment 视图与原引用分离、冻结上下文、JSON 拒绝/预算以及静态三类 handler；独立 Go command 的输出经过真实 Go build/run/test。编译器转换桥直接传递词法 token，保留 raw、复合标点和隐式分号；原输入及 Fragment 按请求内引用恢复，诊断位置单独映射。同一片段的重复输出生成独立 AST。当前联调用显式 Go SDK command 处理捕获表达式，输出重新经过 resolve/check/lower，生成程序完成 build/run/test，并检查重复生成的字节一致性。源码登记、三类入口接线、公共类型同一性、协议内容匹配、RuntimeRoot 供应和 CLI/浏览器受限宿主仍按 RFC 0030 验收。

后续 syn/quote 分别验证语法解析与重建、字符串和代码插值的区别、错误定位、同一片段的重复使用，以及普通运行时与编译期执行的区分。若引入 quote! 宏项目，还须验证其无环构建和独立消费者。内建 Default/Clone/Debug、库派生迁移与泛型 impl 不因公共语法包落地而自动完成，分别按其合同验收。

// Package expand 展开解析后的包级 macro_rules，输入 AST 始终只读。
//
// 每次展开分配一个大于所有输入 Ctx 的新 SyntaxCtx。模板标识符使用该
// Ctx，捕获片段保留原 Ctx 和拼写；重复中的局部名在普通词法作用域内区分。
// Contexts 记录新 Ctx 的定义作用域和调用作用域。resolve 应先按 Key 在
// 词法作用域查找，再按 Scopes 的定义点查自由名，不能退回调用点猜名字。
// $caller:: 标识符和生成的 item 名字使用调用 Ctx 与调用作用域。
// Introductions 给生成的包级声明保留模板别名，使其内部引用与调用方
// 引用能指向同一个声明。嵌套生成的包级名字退回最外层调用 Ctx。
// internal/resolve 的入口尚未接入这些旁表；本包不实现值名字解析。
//
// parser 目前只有源码入口。本包只把模板骨架交给它解析；捕获以有类别的
// 不透明占位槽传递，按槽的字节位置接回 AST，绝不拼接捕获表达式再解析。
// tt 是可拆解的 token tree，标识符、来源和非 type 类别仍逐 token 保留。
package expand

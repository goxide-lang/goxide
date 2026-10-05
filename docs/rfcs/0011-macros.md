# RFC 0011：声明宏与卫生展开

## 状态与范围

已确认目标：提供模式 / 模板声明宏；用户过程宏延期（D14）。
本文保留完整目标的候选规范；宏语法、定义点卫生、阶段和预算未全部冻结（T10、U09）。已有有限展开器及文末增量，具体边界见 [实现说明](../../internal/macros/README.md)。
依据：[原设计](../design/macros.md)。打印、派生和语法数据分别见 RFC 0012、0013、0014。

## 源级规则

- 捕获的是带语法类别、来源和上下文的结构化片段，不是重拼后的字符串。
- 候选匹配规则：按定义顺序选择第一条完整匹配；重复层级或类别不符报错。
- 模板局部绑定每次展开取得新身份；捕获语法保留调用者绑定；自由名字按定义点解析。
- 卫生只决定绑定。实参执行次数由模板决定，不能把“一次求值”作为所有宏的隐含性质。
- 展开后仍执行普通名字、类型、借用和 unsafe 检查，再生成 Go；宏本身没有运行时对象。
- 用户宏规则不执行文件、环境、网络或任意用户程序；可以生成合法的运行时 I/O 调用。

## 转译示例

**M01：显式保存实参，保留两侧同名变量。** goxide 片段；`macro_rules!` 为候选语法。

```goxide
func twice(value int) int { return value + value }
macro_rules! twice_plus_one {
    ($x:expr) => {
        func() int {
            saved := 1
            value := $x
            return twice(value) + saved
        }()
    };
}
// 以下片段位于调用方函数体。
saved := 41
twice := func(value int) int { return -1000 }
calls := 0
next := func() int { calls += 1; return 1 }
result := twice_plus_one!(saved + next())
```

Go 对照；片段置于函数体，依赖定义点 helper 的普通函数 `macroRuntimeTwice`。
该 helper 的函数体为 `return value + value`；输出名字只是示意。

```go
saved := 41
twice := func(value int) int { return -1000 }
_ = twice
calls := 0
next := func() int { calls++; return 1 }
result := func() int {
    goxideM1Saved := 1
    goxideM1Value := saved + next()
    return macroRuntimeTwice(goxideM1Value) + goxideM1Saved
}()
```

约束：`result == 85`、`calls == 1`、`saved == 41`；调用者的同名 `twice` 不能替换定义点绑定。
不得先把捕获表达式按文本粘贴进模板，再按生成后的局部名字重新解析。

**M02：重复捕获表达式会重复执行。** goxide 片段沿用 M01 的 `next`。

```goxide
macro_rules! duplicate {
    ($x:expr) => { $x + $x };
}
result := duplicate!(next())
```

Go 对照；依赖 M01 的 `next`，从 `calls == 0` 开始观察。

```go
result := next() + next()
```

约束：`result == 2`、`calls == 2`。不得将其擅自改为保存一次再相加。
短路、闭包、defer 和 return 同样遵循实际展开语法，不由卫生额外改写控制流。

**M03：unsafe 来源不随模板移动。** 下列 goxide 候选模板不能替调用者授权。

```goxide
macro_rules! consume {
    ($x:expr) => { unsafe raw::Consume($x) };
}
consume!(raw::Other()) // Other 在调用点没有 unsafe 授权
```

预期拒绝，不生成 Go；依赖 `raw` 为未声明 safe 视图的原生 Go 包。
模板中的显式 unsafe 只按其来源授权模板引入的操作。

## 拒绝与边界

- 表达式捕获插入类型位置、重复长度或层级冲突：拒绝，不通过字符串重解析兜底。
- 模板展开到外包后调用定义包私有 helper：拒绝；应显式提供公开 helper / shim。
- 模板不得生成未知 import、宏定义或外部 API 的 declare 契约；相关限制仍属候选阶段协议。
- 展开结果缺少 impl、权限冲突或重复定义：按普通源程序拒绝。
- 递归或输出超预算：诊断包含定义、调用链和预算类别，不把半成品当成成功缓存。

## 双向互操作

goxide 宏生成的原生 Go 调用仍遵守所选 `.d.gox` 契约或 unsafe 规则。
发布模块中的 `.go` 已是展开并检查后的普通代码；Go 消费者不需要宏展开器。
Go 可调用公开生成函数，不能调用 `m!(...)`；宏规则供 goxide 工具链加载，具体承载格式未定。
增强检查只覆盖 goxide 工具链管理的代码，不给原生 Go 调用者追加源级权限保证。

## 验收与未定项

1. M01/M02 分别验证一次与两次执行；不要只比较生成临时名。
2. 同一宏重复展开、缓存命中和并行构建必须保留相同绑定关系，不复用旧调用点身份。
3. M03、私有 helper 和类别错配产生源级错误；生成 Go 错误可追溯到模板与调用点。
4. 成本：运行时成本来自展开代码；编译期成本受匹配、递归和输出节点预算限制。
5. 未定：完整捕获类别 / 重复语法、导入拼写、阶段细节、预算默认值及缓存身份协议。
6. 完整目标与末尾已实现子集分开验收；独立执行 Go 片段只能检验指定展开的行为。

## 宏 builtin 首片：len/cap（2026-10-03）

模板可直接写 `len($x)` / `cap($x)`，无需 wrapper。定义端局部/函数绑定优先，变量、类型与 import 不能误当 builtin。编译器标记在 DSL 降低后恢复原生 Go 调用，保留常量上下文、数组/数组指针的不求值规则、实参次数与 `?` 所在 frame。调用端局部变量/常量与 import 按绑定改名；struct 字段键和 map 表达式键区分处理。

[单元执行](../../internal/macros/builtins_test.go)与[真实生成模块验收](../../tests/acceptance/macro_builtins_test.go)覆盖嵌套/跨包宏、局部及 import 遮蔽、定义端同名声明、常量/数组、None/Some 传播次数，以及生成模块 race/vet。调用包级同名声明和实际覆盖调用点的局部 named type 目前明确诊断；无关作用域的类型名称保持不变。它们是工程边界，不自动视为获批延期。其他 builtin（含 append/make/new）未开放，不宣称完整 Rust 宏或全面卫生已完成。零值/公开入口契约未修改。


## 显式 Go 返回值适配（2026-10-04）

内建 `go_result!` / `go_result_keep!` 已获用户授权，两种结果及传播契约见 [RFC 0005](research/0005-option-result-question.md#显式-go-result-适配宏2026-10-04)。宏可出现在声明宏模板和已支持的表达式位置；`m!(...)?` 的前处理现在保留整个 qualified 宏操作数。

实现复用宏展开器，先以内部 marker 保持捕获/定义点卫生，再展开具体适配器函数 literal；原调用位于实参，不能移进其函数体。类型参数不是通用 type fragment；首片显式 T，借用现有具体 Result 登记。原生调用必须返回 error 或 (T,error)，语义检查发生在问号类型解析稳定之后，防止暂时的 identity stub 误判原生泛型签名。局部 Result/error/nil 遮蔽、重复展开确定性和生成模块 race/vet 见[接受测试](../../tests/acceptance/go_result_test.go)。外部预编译宏规则仍未接通，独立模块测试对此明确拒绝。

### T/E 推导增量（已授权，先规范后实现）

两种 Result 适配内建改为在 typed 阶段按真实返回签名确定 arity/T/E，不增加一般声明宏的类型查询或 type fragment。规范、nil 黑白案例、显式 T 兼容、卫生/缓存/source map 及网站差分验收见 [RFC 0005 推导增量](research/0005-option-result-question.md#按静态返回签名推导-te已授权实施前规范)。此处是本轮实施契约；上节固定 error 的首片说明作为历史保留。

## 已有拒绝规则的精确诊断收口（2026-10-05，实施合同）

对应 MVP 台账 A03，本片只修诊断来源，不放宽模板语法或已有拒绝规则。
模板内部 import selector、具名 literal key、local type/struct/interface/label 等已有
诊断应定位实际触发规则的 AST token，而不是统一指向模板首 token。多问题模板按
确定的 AST 遍历顺序报告首个已识别问题，避免后续节点覆盖位置。定义文件、调用栈
及原始 token span 必须保留；捕获片段与模板来源不可混淆。没有对应 token 的合成
节点不能凭最近文字猜源位置，保留现有 fallback。

验收包括：前有合法语句的多行模板、同名 selector 重复出现、nested/package 宏、
CRLF、已有自由名和 budget 拒绝回归；诊断指向触发规则的 selector/key/声明 token，
保留既有拒绝含义。合法模板生成 Go 与执行行为不因诊断调整而改变。

宏错误保留结构化 token/span、坐标阶段和展开栈；`?` 前处理后的本文件 token
通过 prepared map 回溯，包定义的原始坐标不重复映射。定义/调用框端点来自各自
原始 token，不要求包含 `?` 改写的整个框保持等长。

单文件前端的定义点 import 解析也须复用前端 importer：原生使用既有离线导出数据，
WASM 使用已随附的真实 export archives。单文件无 package registry 不代表可以忽略
调用方传入的 importer。验收合法 `fmt.Println` 模板和其后错误 selector 在 CLI/WASM
一致；不增加浏览器包集合、文件系统、网络或外部宏包能力。

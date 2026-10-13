# 到 pre-MVP 的规划

目标：用新前端（`parser → expand → resolve → check → lower`）替换现有文本管线，恢复到 README 记录的能力集合，然后开始 MVP 工作。

规则：每个阶段一个分支、一组 Codex 任务、一次验收；阶段之间不混。旧管线在 P6 之前保持可用但冻结，只修阻塞性 bug。

每个阶段的通用检查（下文不再重复）：

```sh
go build ./...
go test -count=1 ./internal/... ./tests/...   # tests/ 视该阶段改动的范围
go vet ./internal/... ./tests/...
gofmt -l internal cmd
```

全部退出 0，无输出。另加两条硬规则：不许为了让新代码通过而放宽既有测试；每个阶段自己的端到端必须真的调用 `go build` / `go run`，不能只比文本。

## P0 现状基线（已完成）

新前端的 `syntax`（token/lexer/ast/parser/printer）、`expand`（宏展开与卫生）、`resolve`（名字解析含卫生上下文）、`lower`（纯 Go 子集降低）已落地。

检查：语料 253 个 `.gox` 解析往返；`lower` 的端到端测试生成模块并 build/run 通过。

## P1 语言核心：enum / match / `?` / Option / Result

这是与 Go 差异最大的一块，也是后面所有阶段的基础。

交付：

- enum 声明降低为闭合变体：标签加各变体独立内联槽，变体构造、载荷投影、零值语义。
- match 编译：穷尽性、guard、`ref` / `ref mut` 绑定、表达式体与块体、作为终结语句（不再要求补 `return`）。
- `?` 传播：返回到最近的函数边界，单步错误转换，失败后不执行后续实参。
- `Option[T]` / `Result[T, E]` 作为已登记的具体实例。
- 新阶段 `internal/check` 起步：穷尽性、非法值策略、不可达分支、`?` 的返回类型匹配。

检查：

- `.gox → .go` golden 加 `go run` 输出比对（沿用 `lower` 的 e2e 机制）。
- `check` 的正反例诊断测试，断言诊断码与 span。
- `examples/enum_trial` 在新管线下整条跑通（build、run、race、vet）。

退出：现有 `tests/acceptance` 里 enum / match / `?` 的行为在新管线复现。

不做：泛型 enum 的完整实例化、match 的复杂引用投影（留 P3）。

## P2 宏接入管线与派生

交付：

- 宏调用在表达式、语句、类型、item 位置接入 `lower`。
- `derive(Default, Clone, Debug, PartialEq, Eq)`。
- serde 派生的登记（JSON 实现本身接 serde 模块，不在本阶段改）。

检查：宏与派生示例在新管线生成并运行；派生的行为测试覆盖零值、字段参与规则、显式手写协议的冲突。

退出：`examples/macro_*`、`examples/manual_default` 等在新管线通过。

## P3 引用与 payload 投影

交付：`&T` / `&mut T`、取引用、`ref` / `ref mut` 模式、payload 原位投影、`match &e`。

检查：借用示例运行；投影身份测试（指向同一槽位）；读写权限的正反例。

退出：`examples/enum_borrows` 通过。

## P4 trait、impl 与关联类型

交付：非泛型 `trait`、本包具体类型的 `impl`、`T::method`、单约束泛型函数、关联类型、wrapper 与 marker、`declare impl` 的 Go 侧适配。

检查：`examples/static_protocol` 通过；生成的 wrapper 能被原生 Go 代码调用（互操作测试）。

退出：静态协议示例通过，且生成产物可被普通 Go 消费。

## P5 跨包与项目工具链（最小）

交付：多包目录解析、`use` 的跨包绑定、导入合并、最小 `goxide.toml`（`package`、依赖、`bin`）与 `goxide.lock`；第三方依赖经 Go 模块解析。

检查：多包端到端 build/run/test；拉取真实依赖（`github.com/goxide-lang/serde` 等）构建成功；lock 稳定（同输入重复运行不变）。

**明确砍掉**：workspace、`import-go`、vendor 迁入、构建脚本授权。现有实现与文档一并删除或归档，见"待你决定 1"。

退出：一个多包项目端到端通过。

## P6 CLI 切换与旧管线删除

交付：`cmd/goxide` 的三种输入模式（单文件、目录、项目）改走新管线；删除 `internal/frontend` 及 `enums`、`matchexpr`、`propagate`、`refs`、`equality`、`protocols`、`printing`、`sourcemap`；网站用新编译器重建。

检查：全树 `go test`；`examples/enum_trial/try.sh`；网站构建与带 `goxideweb` 的 race/vet；确定性（同一输入两次运行输出字节一致）。

退出：旧管线代码从仓库消失，全部门禁绿。

## P7 pre-MVP 收口

交付：一张状态表（特性 × 支持度）取代免责式说明；删除 `docs/reviews`、`tests/design`、`tests/research`、`IMPLEMENTATION.md` 的追加日志与 `ROADMAP.md`；CI 只保留 `go test` / `go vet` / 阶段端到端。

检查：文档门禁通过；状态表每个条目都有对应测试。

退出：仓库里只剩下"现在支持什么"和它的测试。

## 待你决定

1. **P5 砍掉的范围**：workspace、`import-go`、vendor、构建脚本授权是否直接删除，还是先归档到一个 `archive` 仓库。这四项合计约一万行实现与文档。
2. **P7 的历史文档**：直接删（历史留在 git），还是先搬去 `archive` 仓库。
3. **P1 的泛型范围**：先只做具体类型的 enum / Option / Result 实例（更快见到端到端），还是同时做泛型实例化。前者能把 P1 压到更短。

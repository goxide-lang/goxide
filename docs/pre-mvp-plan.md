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

交付：一次做完，不拆里程碑。

- enum 声明（含泛型参数与实例化）降低为闭合变体：标签加各变体独立内联槽，变体构造、载荷投影、零值语义。
- 泛型：类型参数与实例化，`Option[T]` / `Result[T, E]` 作为泛型类型而非固定实例；实例化取真实类型实参。
- match 编译：穷尽性、guard、`ref` / `ref mut` 绑定、表达式体与块体、作为终结语句（不再要求补 `return`）。
- `?` 传播：返回到最近的函数边界，单步错误转换，失败后不执行后续实参。
- 新阶段 `internal/check`：穷尽性、非法值策略、不可达分支、`?` 的返回类型匹配、泛型实参的约束检查。

实现顺序建议（给执行者，不是交付批次）：先把变体表示与构造落稳并用 e2e 验证，再接 match，最后接 `?` 与 `check` 诊断。

检查：

- `.gox → .go` golden 加 `go run` 输出比对（沿用 `lower` 的 e2e 机制）。
- `check` 的正反例诊断测试，断言诊断码与 span。
- 泛型：同一泛型 enum 的多个实例共存并各自正确。
- `examples/enum_trial` 在新管线下整条跑通（build、run、race、vet）。

退出：现有 `tests/acceptance` 里 enum / match / `?` 的行为在新管线复现。

不做：match 的复杂引用投影（留 P3）。

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

## P4 trait、impl 与关联类型（已完成）

交付：非泛型 `trait`、本包具体 struct/enum 的固有及 trait `impl`、`T::method`、单 trait 约束泛型函数、普通关联类型、wrapper/marker，以及 `declare impl` 的 Go 侧适配。struct 与 enum 使用同一套源级成员查询；新管线独立完成检查与降低。

已确认合同：

- 有体实现写 `impl Trait for T`，`Self` 始终为基础目标类型 T。实例首参数写 `self Self`、`self &Self` 或 `self &mut Self`；其他成员为关联函数，不引入 `static`。
- 关联类型在 impl 中显式定义，使用 `Self::Item` / `T::Trait::Item` 投影；运行时 trait 值全部绑定，如 `Trait[Item = int]`。Go 侧只有普通类型参数和固定方法签名。
- 静态调用和泛型实现证据使用 `std/typeinfo` 空 carrier。关联类型由 carrier 上仅用于类型推导的 witness 确定，Go 调用仍可写 `Make[User]()` / `Read[Box](x)`，不增加运行时字典参数。
- enum 保持密封接口和指针变体。接口声明精确 Self 类型锚，各变体返回同一个空 carrier；锚与 witness 不在运行时调用。操作 enum 槽位的方法在 carrier 参数中使用 `*E`，不要求 `*E` 也有类型锚，不产生或读取旧 enum 元数据。
- 实例业务只生成在选定 wrapper，target 为命名字段。固定包级工厂完成适配；As/For helper 只转发。enum 值视图保留 `e.AsNamed().Name()`；借用视图通过保存真实 owner 的 `ForE(&e).AsNamed()`，不复制 enum 后取临时地址。
- `declare impl` 按真实 Go T / *T 方法集及代入关联类型后的签名验签。外部类型不注入方法或类型锚；P4 提供运行时适配，不要求外部原生类型直接参加 carrier 泛型。
- nil 采用已有 Go / enum 规则，不新增统一非空工厂检查。引用权限属于类型检查；别名与逃逸风险分析仍不在 MVP。

实施分工按文件归属并行：主代理统一 AST、解析结果、检查旁表与调用合同；检查器负责成员选择、关联类型和引用权限；降低器负责 carrier、wrapper、工厂和 Go 适配；端到端用例覆盖两类源类型和原生 Go 消费。集成后独立审阅，再一次完成阶段验收与原子提交。

检查与退出条件：

1. `examples/static_protocol` 由新管线生成并运行，输出保持 `Ada Ada`；具体与泛型静态调用选择同一显式实现。
2. struct/enum 的固有成员、同名 trait、限定调用、方法值、`Self` 和关联类型均有行为正例与可证伪反例；泛型实参缺 impl、关联绑定冲突和权限提升在源级诊断。
3. 值视图保留既有复制语义，借用视图保持原对象/enum owner 身份；适配与方法值接收者只求值一次，同一视图不重复包装。
4. 原生 Go 实际调用公开静态入口、carrier 泛型、固定工厂及 As/For helper；declare 精确方法集、同名不同业务和不提升额外方法均经真实 build/run 验证。
5. 同输入两次生成字节一致；完成工作区规定的 build/test/vet/格式检查、生成模块的 Go 验证及文档检查。

范围边界：泛型 trait/impl、多 trait 约束、GAT、supertrait、默认关联类型、重叠实现和特化继续延期。跨包 Goxide 加载、导入合并与依赖信息恢复由 P5 接续；CLI 切换和旧管线删除仍在 P6。P4 不修改 `std` / `serde` checkout，不新增外部依赖。详细合同见[trait](design/traits.md)与[关联类型](design/associated-types.md)。

## P5 跨包与项目工具链（已完成）

交付：多包目录解析、`mod` 与 `use` 的跨包绑定、公开重导出、导入合并、最小 `goxide.toml`（`go_mod`、依赖、`bin`）与 `goxide.lock`；第三方依赖经 Go 模块解析。新管线由生产项目入口组装，P6 再接入 CLI。

实施合同：

- 复用 `internal/project` 的清单、Go 选版、锁与受管目录实现；不另建包管理器或锁格式。作者根只保存清单、源码和锁，Go 元数据与生成代码写入 target。
- 所选 Goxide 模块按真实 module/package 身份联合经过新管线；跨包 enum、trait、宏和引用权限从源码恢复，不产生或读取旧前端 metadata。原生 Go 依赖使用真实 Go 类型信息。
- `mod child` 只绑定选中的本地子包；依赖根和 `namespaces` 使用精确映射。`pub use` 保留 canonical 身份，函数和变量不生成转发包装；具体语义沿用[静态路径](design/static-paths.md)。
- 原生 Go 文件、测试与资源保留，生成文件不覆盖独立手写实现；目录发现不跨嵌套 module，也不把受管 target、vendor、testdata 作为源码包递归编译。
- 清单中的 bin 选择对应入口；依赖库沿用既有 lib 声明。保存选定版本和校验和；相同输入复用稳定产物，重复生成和锁文件字节一致，locked 模式拒绝改变依赖映射。

检查与退出条件：

1. 一个含多个包及本地 Goxide 依赖的项目真实 build/run/test，通过跨包 enum、trait、宏、公开重导出和原生 Go 消费验证。
2. 获取清单声明的真实 `github.com/goxide-lang/serde` / `std` 依赖，生成程序执行序列化往返；独立手写 Go 与资源参与实际构建。
3. 引用权限、私有成员、错误包来源、导入环和输出冲突给出可定位诊断；不靠放宽检查或静默丢弃文件通过验收。
4. 同输入两次生成 Go 与 lock 字节一致；offline + locked 重放成功，变更清单后的 locked 构建失败且不破坏已有产物。
5. 完成工作区规定的 build/test/vet/格式和文档检查。详见[最小项目合同](design/toml-manifest.md#p5-新管线入口)。

**不在本阶段**：workspace、`import-go`、vendor 迁入、构建脚本授权。它们是 MVP 的功能，实现仍保留在仓库里（旧模块不随 P6 删除），但要等 pre-MVP 之后另立阶段重启，见下"后续阶段（MVP）"。

## P6 CLI 切换与旧管线删除

交付：`cmd/goxide` 的三种输入模式（单文件、目录、项目）改走新管线；删除 `internal/frontend` 及 `enums`、`matchexpr`、`propagate`、`refs`、`equality`、`protocols`、`printing`、`sourcemap`；网站用新编译器重建。

不删：`internal/project`、`internal/buildscript`、`internal/scripttrust` 等工具链模块。它们的 CLI 入口在切换时暂时摘掉，实现保留给 MVP 阶段重启，避免重写。

项目编排收敛验收：

- CLI 只调用一个项目入口。
- 清单、依赖计划、锁、源码准备、生成模块物化各留一份实现。
- `internal/lower` 只处理已准备好的模块。
- 删除旧 `cmd/goxide` 的 `runProject` 编译分支，以及失去用途的重复辅助函数：`projectRoots`、`projectLayout` / `checkTarget`、`prepareProjectModule`、`projectUnitSpec`。
- `internal/compile` 可保留为组合入口，但不得长期保留两套编排。

检查：全树 `go test`；`examples/enum_trial/try.sh`；网站构建与带 `goxideweb` 的 race/vet；确定性（同一输入两次运行输出字节一致）；确认不存在两套项目编排，符合 `AGENTS.md` 禁止并行实现路径的要求。

退出：旧管线代码从仓库消失，全部门禁绿。

## P7 pre-MVP 收口

交付：一张状态表（特性 × 支持度）取代免责式说明；删除 `docs/reviews`、`tests/design`、`tests/research`、`IMPLEMENTATION.md` 的追加日志与 `ROADMAP.md`（历史留在 git 里）；CI 只保留 `go test` / `go vet` / 阶段端到端。

检查：文档门禁通过；状态表每个条目都有对应测试。

退出：仓库里只剩下"现在支持什么"和它的测试。pre-MVP 到此完成。

## P6.5a 已有 Go 项目试用入口

交付：`gen` 作为 `transpile` 的别名，同包手写 Go 只读参与类型检查，`.gox` 原位生成带标记的 `_gox.go`；缺运行时依赖时给出 `go get` 命令，不自动改 `go.mod`。生成文件使用行级 `//line`，与 `go generate` 配合；使用方式见[用户指南](using-goxide-gen.md)。

检查：真实 Go module 中的手写类型、函数与 enum/match 混合生成，执行 `go generate ./...`、`go build ./...`、`go run .`，再次生成保持字节一致；行映射、输出覆盖保护和依赖提示有对应测试。

待办：网站用户指南页面从 `docs/using-goxide-gen.md` 单源生成。支持库的 tag 与编译器发布入口须对应已公开的依赖 pin。

## 后续阶段（MVP）

pre-MVP 之后按下面的顺序重启被推迟的功能。这些不属本次规划，只记录顺序，细节另行设计。

| 阶段 | 内容 |
| --- | --- |
| MVP-1 | workspace：成员、依赖继承、根锁、按成员合并 feature；重启 `internal/project` 的 workspace 部分 |
| MVP-2 | `import-go`：把已有 Go 工程迁入统一项目模型；重接 `cmd/goxide` 的迁入入口 |
| MVP-3 | vendor 迁入与供应来源选择；重启 `internal/project` 的 vendor 与来源部分 |
| MVP-4 | 构建脚本授权：重启 `internal/buildscript` 与 `internal/scripttrust` 的 CLI 入口 |
| MVP-5 | LSP：诊断、符号导航、悬停、补全，与编译器共用同一套查询结果；补充完整 source map、列级与跨文件映射 |
| MVP-6 | safe std 首批入口、锁与集合类型；将 `std/sum` 的 Option/Result 从 tagged struct 统一为密封接口 |

顺序依据：workspace 与迁入都要求统一项目模型先稳定（P5 的最小 `goxide.toml` 是它的种子）；LSP 要求前面各阶段的语义表齐备。

`std/sum` 表示统一与 MVP-6 一起设计和验收：`Some[T]`、`Err[T, E]` 等变体必须携带 enum 的全部类型参数，且调整将改变公开 Go API。P6.5a 保持当前表示。

## 已定事项（2026-10-13）

1. workspace、`import-go`、vendor、构建脚本授权是 MVP 功能，不删实现，P6 切换时摘除入口，MVP 阶段重启。
2. P7 的历史文档直接删除，不建 archive 仓库，历史留在 git。
3. P1 做泛型，`Option[T]` / `Result[T, E]` 是泛型类型，一次交付。
4. `internal/check` 从 P1 开始建立，不早于语言核心，也不等到 P6。


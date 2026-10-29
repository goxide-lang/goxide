# 后续开发安排（pre-MVP 之后）

本文接 `pre-mvp-plan.md`，说明后面每一阶段交给 Codex 时怎么派、依据什么验收、以及针对模型倾向要加的限制。`.omc` 一类的自动流程不用于本仓库；每一步都是"一份提示词 + 一次人工核对 + 一次原子提交"。

## 1. 派工方式

每个阶段固定四件事：

1. **改规范**（Claude 做）。阶段涉及的语言语义、表示、接口先写进 `docs/design/` 或 RFC。规范是提示词的依据，没有规范就不派工。
2. **写提示词**（Claude 做）。一份文件放在 `notes/`，含：权威文档清单、逐条硬要求（带编号）、验收方式、边界、验收命令、报告格式。
3. **执行**（Codex 做）。一次给足范围，不拆交付批次。提示词里写明"歧义处选最简单的解释继续做，不要停下来问"。
4. **核对与提交**（Claude 做）。自己跑构建、测试、vet、gofmt；读关键实现而不是只看报告；确认没有放宽既有测试；然后原子提交。

派工时的模型与强度：

| 阶段 | 建议强度 | 理由 |
| --- | --- | --- |
| P2 宏接入与派生 | high | 有现成展开器，接入为主 |
| P3 引用与投影 | xhigh | 身份与槽位是静默错误高发区 |
| P4 trait / impl | xhigh | 涉及跨包互操作与 wrapper 生成 |
| P5 项目工具链 | medium | 大部分是与 Go 模块打交道，逻辑直白 |
| P6 CLI 切换与删除 | medium | 删除为主，风险在遗漏 |
| P7 收口 | low | 文档与删除 |

## 2. 阶段与验收

| 阶段 | 交付 | 验收 | 关键风险 |
| --- | --- | --- | --- |
| P2（已完成） | 宏在表达式/语句/类型/item 位置接入；`derive(Default, Clone, Debug, PartialEq, Eq)`；serde 派生登记 | [默认值业务与重复生成](../tests/acceptance/manual_default_test.go)；[跨包宏业务与重复生成](../tests/acceptance/macro_packages_test.go)；[派生行为与冲突](../internal/lower/derive_test.go)；[宏卫生](../internal/lower/macro_e2e_test.go) | 把派生做成通用泛型 trait；为 serde 顺手改 `serde` 仓库 |
| P3（已完成） | `&T` / `&mut T`、读写权限、`ref` / `ref mut`、原位 payload 投影、`match &e` / `match &mut e` | [引用生成与运行](../internal/lower/reference_e2e_test.go)；[权限与寻址反例](../internal/lower/reference_diagnostics_test.go)；[控制流资格](../internal/check/reference_flow_test.go)；原生 Go vet/race test；重复生成字节一致 | 用户 enum 复制共享变体对象，旧投影保留旧对象；Option/Result 保留 tagged struct 与内联槽，MVP-6 与 safe std 一起验证泛型表示和公开 API 变更 |
| P4（已完成） | 非泛型 trait、本包具体 struct/enum 的 `impl`、三种 self 接收权限、`T::method`、单 trait 泛型约束、关联类型 witness、wrapper/marker、`declare impl` | 新管线静态协议示例；struct/enum 共用成员查询；原生 Go 调用空 carrier 泛型及 As/For；重复生成一致 | 接收者求值与身份、引用权限、enum owner 或精确 Self 被改变 |
| P5（已完成） | 多包目录、`mod` / `use` / 公开重导出、导入合并、最小 `goxide.toml` 与 `goxide.lock` | 多包与本地依赖[真实 build/run/test](../internal/compile/project_e2e_test.go)；serde/std 依赖；原生 Go 消费；offline/locked 重放与字节一致 | 顺手复活 workspace / vendor / 迁入 |
| P6 | CLI 三种输入模式改走新管线；删除旧管线八个包；网站用新编译器重建 | 全树 `go test`；`examples/enum_trial/try.sh`；网站构建与 `goxideweb` race/vet；两次运行输出字节一致 | 删多了（工具链模块要留）或删少了 |
| P7 | 状态表取代免责式文档；删 `docs/reviews`、`tests/design`、`tests/research`、首尾追加日志、`ROADMAP.md`；CI 只留 `go test` / `go vet` / 阶段 e2e | 文档门禁通过；状态表每条有对应测试 | 把状态表写成第二份免责文档 |

每个阶段的通用验收命令：

```sh
go build ./...
go test -count=1 ./internal/... ./tests/...
go vet ./internal/... ./tests/...
gofmt -l internal cmd
```

节点上环境限制多（默认 Go 缓存不可写），提示词里要允许它用可写的 `GOCACHE` / `TMPDIR`，并要求在报告里写明实际用的命令。

## 3. 给 Codex 的限制与引导

这些是提示词与 `AGENTS.md` 里都要重复的硬约束。

### 3.1 禁止扩范围

- 只改提示词点名的目录。任何对 `internal/syntax` / `resolve` / `expand` / 其他既有包的改动都要单列并附最小复现。
- 不改 `cmd/`、`docs/`、`examples/`、`tests/acceptance/`、CI，除非该阶段的任务就是它们。
- 不改 `std`、`serde` 两个相邻仓库。需要它们配合时，报告里提出来，由人决定。
- 不新增依赖；只用 Go 标准库。
- 不写额外 Markdown、评审报告、证据文件。

### 3.2 禁止降低验收强度

- 不许为了通过而放宽、删除或改写既有测试。既有测试失败说明实现有问题。
- 不许只跑定向测试就声称通过；每个阶段要跑提示词里列的完整命令。
- 不许把"能编译"当成"行为正确"，行为类要求必须真的运行并比对输出。
- 报告里给原始命令与原始输出，不写"通过"两个字。

### 3.3 针对模型倾向的具体引导

模型会把"稳妥"读成"多写一层"，以下四条最容易出问题，提示词里点명：

1. **不要加兼容与版本层**。不写旧格式检测、版本号、指纹、迁移路径、适配层。pre-1.0 直接改。
2. **不要抽象成框架**。一个功能不做成可配置的注册表；没有第二个实现就不抽接口；不超过三层间接。
3. **不要两套路径**。新代码不调旧管线，旧管线不为了配合新代码改造；两套并存只在 P6 之前，且靠目录隔离。
4. **不要顺手"改善"未要求的东西**。重构、重命名、格式化无关文件都算越界。

### 3.4 报告格式（固定七节）

1. 状态一行（完成 / 部分 / 未完成 + 原因）
2. 改动文件清单（新建 / 修改 / 删除分开，越界的单列）
3. 验收命令与原始输出
4. 逐条要求的落点（编号 | 实现位置 | 测试名）
5. 该阶段要求的实物证据（生成的 Go 片段、端到端运行输出等）
6. 接口契约（新增或变更的公开签名与调用顺序）
7. 未完成与假设（解释性选择、留桩、既有缺陷附最小复现）

第 3、5、6 节是人工核对的主要入口。

## 4. 人工核对清单

每阶段提交前固定做这七项，不看报告只看代码与命令：

1. `go build` / `go test` / `go vet` / `gofmt -l` 自己跑一遍。
2. `git status` 确认改动范围在提示词允许的目录内。
3. `git diff` 里有没有既有测试被改松（删除断言、放宽期望）。
4. 生成的 Go 片段是不是"普通人会手写的样子"；有没有多余包装、转发函数、未使用的辅助类型。
5. 端到端测试是否真的调用 `go build` / `go run`，而不是只比字符串。
6. 有没有引入版本号、指纹、兼容分支、可选配置项。
7. 确定性：同一输入两次生成是否字节一致（`lower` 已有该断言，后续阶段保持）。

## 5. 何时需要人来定

只有以下三类需要用户决定，其余由执行者按"最简单解释"处理并记一行：

- 用户可见的语言语义（语法、求值顺序、错误行为）。
- 破坏相邻仓库（`std`、`serde`）或发布契约的改动。
- 与已确认决策（`docs/design/decisions.md` 中标记已确认的条目）冲突。

## 6. 提交约定

- 一个主题一个提交；跨主题的改动拆开（本次出现过一次 `docs/rfcs/README.md` 混了两处改动，已拆开重做）。
- 分支：`docs/*`、`chore/*`、`feat/*` 各自独立，链式推进时后一个分支包含前一个。
- 提交信息写"为什么"，不写"改了哪些文件"；末尾带 Claude 署名行。
- 不自动 push、不开 PR、不合并；由用户决定何时上远端。

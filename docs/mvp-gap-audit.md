# MVP 差距台账与接续路线（2026-10-05）

结论：**MVP 尚未完成；RFC 的 partial 不表示退出 MVP。** 现有路线图主要按实现切片追加，旧“未开始”、新“已通过”、设计待决和当前工程限制同时存在。不能据标题、网站状态或某次阶段停止，把未闭环的已确认工作改成远期。本表恢复可追踪的工作项，不批准新语法、不改变生产代码、不创建 Issues、不部署。

## 1. 审计基线与判定规则

| 对象 | 本次核对版本 / 证据 |
| --- | --- |
| 编译器 | `feat/vendor-local-modules`，`5d777820c5a617db57bcedb50b4d31ad6f2d3f15`；从远端 fetch 后创建 `docs/mvp-gap-audit`，不是环境预置 main `508f579` |
| std | `feat/goxide-naming`，`ce5bccb4ff9c7fb96b7f192a3a263051590d9288`；根 [go.mod](../go.mod) 固定 `v0.0.0-20261004173304-ce5bccb4ff9c`，sum ABI 2 |
| serde | 公开 `migration/core-json`，`a1b791e96d660c6be620e110c566a9a171631b7e`；固定 `v0.0.0-20261004120911-a1b791e96d66`，不是旧 compiler 内副本 |
| 网站源码 | 同一编译器仓库的 `feat/site-source-selection-5d77782`，`6dc779a651af3eb34121f53637a55bfb6feee387`；相对编译器基线只有 13 个 website 文件变化，未改生产编译器 |
| 静态产物仓库 | `goxide-lang/goxide-lang.github.io` main 为 `15179b2`、deploy 分支为 `b793245`；不能把该仓库 main 的旧 README 当线上状态 |
| 补充设计分支 | RFC0024：`docs/use-path-resolution@5e4b118f25262d8022d1b8f72575d89d69f096aa`；mod 研究 RFC0007：`feat/mod-feasibility@29d81ecf06d7a9cfb383aa1e264a7bcd31d1fb6b`。本地目录缺 0024 不等于没有这份 RFC |
| 工作区规则 | 已搜索四个仓库及 workspace 的 AGENTS.md / SKILL.md；当前 checkout 未提供仓库技能文件，executor skill 列表也为空。没有臆造额外技能规则 |

以下“已实现”以当前源码与具体测试为依据；“历史通过”指仓库已有报告；本轮实际重跑见文末。拒绝测试证明**限制存在**，不证明合法需求应永远拒绝。Go 手写机制测试不等于 `.gox → Go → 执行` 验收。未找到闭环证据时写“未找到”，不把搜索无结果当作绝对不存在。

状态分两轴：

- **工程未做 E**：目标或语义已定，但相应用例仍不支持；保留在已确认工作账。第一轮具体边界不明时另外标“首版细项待确认”，不能擅自降级。
- **设计待定 D**：目标可已确认，但拼写、ABI、字段规则或首版细项未冻结；先产出有反例的设计选择，不自动实施提案。
- **仅交付/记录未做 L**：已有源码与执行证据，仍有文档、网站、版本或分发同步欠账；不要重复开发。
- **完成 C**：只关闭明确列出的子集，不把整个 RFC 标全绿。
- **明确非 MVP N**：必须有独立范围证据。D20 的完整别名/借用风险分析属于此类；“本片不实施”“后续”“研究”本身不构成用户批准延期。

MVP 归属以 [决策登记](design/decisions.md) D14/D21–D23/D27–D36、RFC 内“用户已确认”及后续覆盖为准。当前顺序仍是宏 → 独立 enum → 指针拆分；D29 原位可写 match 是首版要求，但不前置阻塞独立 enum。通用 trait 方向已确认，不能猜成已取消或全量 T0；具体首版实现范围未由本次审计补批。模块目标 `pub/pub(scope)/use/pub use/mod` 见下文研究 RFC 的明确用户目标；仅路径提案未批准不撤销整个模块目标。

## 2. 不应重新打开的已完成切片

| 子集 | 当前实现与执行证据 | 仍不能由此推出 |
| --- | --- | --- |
| vendor → 普通显式 local 模块；多来源选择 | [来源选择报告](reviews/source-selection-20261005.md)、[supplied_sources.go](../internal/project/supplied_sources.go)、[CLI 测试](../cmd/goxide/import_sources_test.go)、[真实 PTY](../cmd/goxide/import_sources_process_test.go)；默认 local、精确 module/version remote、逐模块差异确认、offline 冷暖缓存均已实现 | 不存在 Goxide vendor 构建/拆出模式；不恢复旧 vendor 待决。Git/任意 URL/版本搜索不是该 remote 合同 |
| 正式命名 | [RFC0027](rfcs/0027-goxide-naming.md)、[迁移验收](reviews/goxide-naming-20261004.md)、[迁移测试](../internal/frontend/naming_migration_test.go)；Goxide/goxide/.gox/GOXIDE_*，enum/ref/use/sum 8/3/2/2 | 历史引用旧名不表示生产命名未完成；旧产物应重生成 |
| 共享 sum、构造/pattern 省参 | [builtins.go](../internal/enums/builtins.go)、[sum_inference.go](../internal/propagate/sum_inference.go)、[TestSharedStdCrossModuleAndContext](../internal/frontend/shared_std_test.go)、[公开库回归](reviews/public-library-migration-20261004.md) | 不做无上下文 Result 单侧、None 猜参，不做跨语句反推或完整泛型 |
| 原生返回适配及 T/E 推导 | [go_result.go](../internal/propagate/go_result.go)、[go_result_inference_test.go](../tests/acceptance/go_result_inference_test.go)；普通/保值、nilable 静态 E、typed-nil、单 E、跨模块 | 保值 `?` 不静默丢 Value；非 nilable E/多成功返回值并未自动开放 |
| 跨包协议与透明 alias | [default_external_test.go](../tests/acceptance/default_external_test.go)、[field_protocol_external_test.go](../tests/acceptance/field_protocol_external_test.go)、[serde_external_test.go](../tests/acceptance/serde_external_test.go)、[local_protocol_alias_test.go](../tests/acceptance/local_protocol_alias_test.go)、[same_package_alias_test.go](../tests/acceptance/same_package_alias_test.go) | 手写 Clone/Debug/serde、所有 payload/internal-origin/单文件恢复尚非完成 |
| Option/Result 字段 Clone/Debug、Option Default | [sum_clone_debug_test.go](../tests/acceptance/sum_clone_debug_test.go)；活跃分支、nil/空、跨模块协议 | Result 无隐式 Default；无协议载荷不因当前分支未用而取得资格 |
| match 已完成组合 | [assignment](../tests/acceptance/match_assignment_test.go)、[binary/index](../tests/acceptance/match_binary_index_test.go)、[named/dynamic](../tests/acceptance/match_named_dynamic_test.go)、[call signatures](../tests/acceptance/match_call_signatures_test.go) | 不能再列“命名单返回、函数变量/方法/variadic/原生显式 generic 调用全部未做” |
| workspace 核心及 go.work 渐进迁入 | [workspace_test.go](../cmd/goxide/workspace_test.go)、[native_enhanced_test.go](../cmd/goxide/native_enhanced_test.go)、[native_standalone_test.go](../cmd/goxide/native_standalone_test.go)、[迁入验收](reviews/workspace-import-20261004.md) | 不等于完整 workspace host resolver、publish 或任意 Go directive；旧“workspace 未实现”已过期 |

## 3. 全 RFC 覆盖矩阵

本目录 26 篇编号 RFC（0001–0023、0025–0027），加分支中的 0024，逐篇列出承诺、实现证据、剩余、归属、依赖及验收。细项 ID 在下一节可直接转成 Issues。TEMPLATE 是格式模板，不算功能 RFC。

| RFC / 设计承诺 | 已实现证据 | 未闭环用例与分类 / MVP 依据 | 依赖 → 验收 |
| --- | --- | --- | --- |
| [0001 包加载/完整 dist](rfcs/0001-package-loading.md)：Go 与增强消费者共用 module，声明须验真实 Go 身份 | [module.go](../internal/frontend/module.go)、[enum_external_test.go](../tests/acceptance/enum_external_test.go)、[reference_external_test.go](../tests/acceptance/reference_external_test.go)、version 源规划 | E：通用 `.d.gox`/trait 声明加载、独立宏发布、单文件恢复；L：开头“没有加载器”过期。D30 保留跨 module 要求；完整分发合同细项待确认。PKG-01/02 不能凭 enum metadata 全算完成 | A06/A19/A27；真实新主模块分别 Go/goxide 导入，错声明/陈旧产物拒绝，不依赖生产者绝对路径 |
| [0002 trait](rfcs/0002-traits.md)：显式 impl、期望类型适配、选中实现不被原生方法覆盖 | 特定 Default/Eq registry 已存在；[tests/design](../tests/README.md) 仅有机制证据，不是通用 trait | D/E：T01–T10 的通用前端未闭环。D04–D07 已确认方向；U03/U04 接收权限与泛型证据待定，通用 trait 首版切片待确认，不能直接称非 MVP | A23；先具体 trait/receiver 合同，再验证同名不同实现、一次求值、同视图不重包、跨包恢复 |
| [0003 declare impl](rfcs/0003-declare-impl.md)：C 包 wrapper 采用 B 包真实方法、相容声明合并 | 未找到通用解析/端到端；Default/Eq 特例不覆盖采用声明 | D/E：同键相容判定、declare/有体共现、泛型 receiver 未定；D05 方向保留 | A23+A27；D01–D09 跨 A/B/C、逆序 imports、不一致签名/关联类型拒绝，固定工厂归属 |
| [0004 关联类型](rfcs/0004-associated-types.md)：Item 由实现决定，运行时视图全部绑定 | 无通用端到端，Go interface 机制非前端实现 | D/E：投影/约束/证据/ABI；D04 方向保留，泛型细项 U04 待定；GAT/特化不能混入普通 Item 必需性 | A23；A01–A09 绑定、缺证据/重复绑定/同键不同 Item 拒绝 |
| [0005 读写引用/Arc](rfcs/0005-borrows.md)：可复制保存返回、权限不擦除 | [refs](../internal/refs/lower.go)、[flow](../internal/refs/flow.go)、[composite permissions](../tests/acceptance/composite_permissions_test.go)、[exposed flow](../tests/acceptance/refs_exposed_flow_test.go) | E：复杂签名/容器/回调/局部事实精度；D：Arc 构造与持有 API。D08/D21/D29/D31 属目标；D20 风险分析明确 N，不阻塞权限检查 | A20–A22；存储与旧 alias、跨函数返回资格、readonly 拒绝/合法重绑定接受分别验证 |
| [0006 foreign API](rfcs/0006-foreign-api.md)：真实 Go 视图、declare/unsafe，普通未知调用不默认 unsafe | [native_write_bridge_test.go](../tests/acceptance/native_write_bridge_test.go) 同型标量 mut& 参数，函数值/variadic/defer/go | E：复合 native pointer/返回/回调桥；D：递归声明视图及明确 unsafe 协议。D31 已批准自然调用，无需重开旧 AsPtr 必经候选 | A21/A27；共享原存储和留存返回，readonly 不升级，真实签名错配拒绝 |
| [0007 锁](rfcs/0007-locks.md)：共享 Lock/RWLock、独立结果类型、panic 解锁 | Go 机制夹具见 [tests/README](../tests/README.md)；公开 std 当前仅 sum | D/E：源级锁 API/Unit/回调权限未闭环；D09 方向已定，首批 API 待确认。非逃逸证明 N 不能成为门槛 | A24；返回/panic 解锁、同锁身份、多结果类型、读回调写入拒绝 |
| [0008 enum](rfcs/0008-enums.md)：封闭变体、名义身份、Option/Result、显式相等 | [enums/lower.go](../internal/enums/lower.go)、[builtin](../internal/enums/builtins.go)、[equality tests](../tests/acceptance/equality_test.go)、manual/container/sum 测试 | E：有限 payload/外部恢复/泛型；D：递归比较、公开入口及字段策略。D12/D21/D27/D32–35；raw/C enum 已撤回，不是 gap | A05–A09/A13；非法 nil/typed-nil/伪 case、活跃载荷、真实类型与 nil/空区分 |
| [0009 match](rfcs/0009-match.md)：一次 scrutinee、顺序 guard、穷尽、原函数控制流与原位借用 | 上节四组 match 测试及 [enums_test.go](../tests/acceptance/enums_test.go) | E：多返回上下文、unary/composite/控制头/复杂 place、不命名类型；D+E：原位 mut& 的 ABI。D29 首版要求，D31 明确复杂组合是工程账 | A04/A05/A20；副作用 trace、defer/recover/? 目标、goto、lhs panic 时点、修改原 payload 而非副本 |
| [0010 Vec/slice 视图](rfcs/0010-collections.md)：初始化、范围权限与复制/共享 | 原生 slice 与派生容器可用；无已发布 Vec API | D/E：赋值/Clone/索引/迭代/稳定槽位；D15 泛型 slice 方向保留，具体 Vec 首版合同未定，不能拿 Rust API 自动补全 | A24；V01–V05 越界 Option、逐项构造、扩容前后存储身份与只读资格 |
| [0011 声明宏](rfcs/0011-macros.md)：模式、模板、卫生、来源、预算 | [macros README](../internal/macros/README.md)、[package_test](../internal/macros/package_test.go)、[macro acceptance](../tests/acceptance/macros_test.go) | E：常用 builtin/绑定、组合与 source map、外部宏；D：完整 fragment/repetition 语法。D14/D21 及已授权 log/tracing 类场景是 T0；用户过程宏明确延期 | A01–A03/A19；M01/02 一次/两次语义，定义点/调用点同名、多层与预算负例 |
| [0012 格式宏](rfcs/0012-formatting.md)：格式检查、先参数后 formatter、真实 I/O 失败 | [printing/lower.go](../internal/printing/lower.go)、[formatting/print.go](../formatting/print.go)、[print_default_test](../tests/acceptance/print_default_test.go) | E/D：跨包 Debug/自定义 Display、named scalar、非 statement 场景；formatter 正式协议待定。D14 保留格式宏目标，完整 Rust 格式语法未批准 | A29/A07；顺序/一次求值、缺协议/占位符错配、短写与错误策略 |
| [0013 派生](rfcs/0013-derive.md)：字段协议、Default、Clone、Debug，不偷偷赋值替代 | 上节跨包/alias/sum 测试；[generate.go](../internal/deriveextras/generate.go)、[compile.go](../internal/frontend/compile.go) | E：manual Clone/Debug/serde 登记、静态递归 Default、泛型；D：blank/embedded。D14/D28；具体 derive 不能被完整 trait 研究无限阻塞 | A07–A09；调用次序/次数、缺协议拒绝、别名身份、Debug 内容、nil/空与 Clone 存储 |
| [0014 quote/syn](rfcs/0014-syntax-tools.md)：额外语法工具包 | 内部 token/source map 不是公开 quote/syn；无公共 API | D：首版范围/公共 token 协议 U11；D16 已确认工具方向。用户过程宏与普通运行时语法包分别记账，不能统称“已确认待实现” | A25；S01–S03 字符串转 token 不执行，绑定/来源不能凭 span 伪造 |
| [0015 pub](rfcs/0015-public-api.md)：源可见性与 Go 导出、公开输入契约 | pub enum/macro/use；[use.go](../internal/frontend/use.go)、[enum boundary tests](../tests/acceptance/enums_test.go) | E/D：普通 pub/pub(scope)、命名碰撞、统一函数入口验证；D13 + 模块目标保留，公开 enum 入参是否 body 前 panic 仍待定 | A13/A15；PUB-01/02 + Go/goxide 各自权限边界，不能全对象图安全宣传 |
| [0016 safe std](rfcs/0016-safe-std.md)：逐真实 Go API safe 视图，保留部分成功 | std/sum 与显式 go_result_keep 已可用；不是 safe std API 集 | D/E：首批声明/包装清单、计数/错误/权限合同；D15 方向，U10 具体首版覆盖待确认 | A24；STD-01 NaN；STD-02 n>0+EOF、(0,nil)、非法计数/typed-nil |
| [0017 Map](rfcs/0017-maps.md)：多实现、公用查询，不把 Go comparable 当 Eq | equality 中原生 map 键有限证明；无 GoMap/HashMap/TreeMap 库 | D/E：首版具体实现、赋值/迭代/稳定元素地址协议未定；用户目标不等于三个实现均已批准首版 | A24；MAP01–05 缺失/零值、删除后旧单元身份、回调副作用，不伪造原生 map 元素地址 |
| [0018 lowering](rfcs/0018-lowering-contract.md)：类型/求值/存储/控制/位置不变 | [propagate](../internal/propagate/lower.go)、[source_lowering_test](../tests/acceptance/source_lowering_test.go)、[source_mapping_test](../tests/acceptance/source_mapping_test.go) | E：剩余 guard fallback、完整来源链；L：正文“没有生成器/测试运行器”已过期。D01/D02 是跨所有 T0 的验收要求 | A03/A10；LOW-02 短路、LOW-03 跨模块往返及 changed tokens 明确 unmapped |
| [0019 wrapper](rfcs/0019-uniform-trait-wrappers.md)：固定 C 包工厂、marker、As/For 同实现 | 设计与手写 Go 研究；无通用前端验收 | D/E：具体 ABI/receiver/比较细项，R24 已确认统一 wrapper；不重开外部 marker 注入，不猜全量 MVP 归属 | A23；W01–W06 原对象身份、同名方法选中实现、工厂/facade 一致 |
| [0020 关联函数](rfcs/0020-associated-functions.md)：impl T、T::method、Self/self 身份，不引入 static | [manual_default_test](../tests/acceptance/manual_default_test.go)、[manual equality](../tests/acceptance/manual_equality_test.go) 是有限特例 | E/D：一般固有 impl/函数值/Self/泛型/导出命名。方向已确认；通用首版切片待确认，不把 Default 成功等同 C01–08 全成 | A23；C01–08 与 C-N1–10，普通 Go 同名 Self/self 原样保留 |
| [0021 use/pub use](rfcs/0021-reexports.md)：透明逐项/alias/glob，变量原槽、函数原帧 | [use_test](../internal/frontend/use_test.go)、[use_macro_test](../internal/frontend/use_macro_test.go)、[use_hygiene_test](../internal/frontend/use_hygiene_test.go) | E：裸 builtin alias 构造、宏 use 自由名、复杂 literal 绑定、外部宏；D：完整 mod/pub(scope)。该优先聚合目标已批准，不因 0024 未批准撤销 | A14/A15/A19；同源 diamond 去重、异源冲突、defer recover、地址相同、internal 合法 route |
| [0022 features/deps/targets](rfcs/0022-project-toolchain.md)：可选编译、version/Git/path、多 bin/example、0/1 lib | [project_test](../cmd/goxide/project_test.go)、[version_test](../cmd/goxide/version_test.go)、[cfg_test](../internal/frontend/cfg_test.go) | E：多行 cfg、`.gox` tests、未纳 lib 闭包子包等；D：声明 cfg/weak/dev/profile 首版语法。原目标已交付核心，不能从“不完全 Cargo”降级剩余合同 | A16–A18；A/B/A、关闭宏不装 import、选版固定点、冷暖 offline/locked、配置 metadata 不串用 |
| [0023 scripts](rfcs/0023-build-scripts.md)：生成/本地 host 闭包/显式许可/声明输入缓存 | [buildscript_test](../cmd/goxide/buildscript_test.go)、[buildscript layout](../cmd/goxide/buildscript_layout_test.go)、[scripttrust](../internal/scripttrust/store.go) | C：首片主合同；D/E：host 远端/workspace resolver、跨平台、lease 并发体验。native linking 文档明确后置；OS sandbox 是新选择，不能宣称已提供也不强设 MVP 门槛 | A18；许可先于命中/编译/运行，撤销/升级拒绝，失败无新缓存，host 不进 target init |
| 0024 use 路径（分支） | [固定提案](https://github.com/hxsf/goxide/blob/5e4b118f25262d8022d1b8f72575d89d69f096aa/docs/rfcs/0024-use-paths.md)，无 parser 实现 | D：manifest 依赖名/字符串 import path 根语法未批准；分仓方向已完成。不是 mod 取消证据，旧 JSON 示例不是当前 manifest | A15；批准后测精确 package path、同文件 alias 优先、私有/歧义、不自动激活 feature/下载 |
| [0025 统一项目/迁入](rfcs/0025-unified-project.md)：TOML/lock 唯一权威、只读 plan、新目录原子验证 | [import_go_test](../cmd/goxide/import_go_test.go)、[vendor local](reviews/vendor-local-modules-20261004.md)、[source selection](reviews/source-selection-20261005.md) | C：无损已选子集+local/remote。E/D：其他 Go 指令、旧格式升级、发布自动化细项待确认；不恢复 vendor 运行模式 | A17/A27；政策不丢失、原工程 hash 不变、失败无目的目录、离线零工具网络 |
| [0026 workspace](rfcs/0026-workspaces.md)：显式继承、根统一锁、选中闭包 features、独立成员 | 核心与迁入均已实现，见上节；schema3 作者/schema4 原生迁入各有测试 | E/D：统一 host/dev resolver、lock update、发布物化、其他配置分析；L：正文旧“未实现 flag/未测 CLI”过期。已确认核心不能降回 Proposed | A18/A27；全 workspace 选版但未选 feature 不泄漏；复制锁无关项不下载；成员独立重现 |
| [0027 命名](rfcs/0027-goxide-naming.md)：一次破坏迁移，旧输入拒绝 | 迁移报告、schema/CLI/name/WASM 测试 | C；仅历史措辞清理 L，不重新实现、不改 module path、不复活旧 vendor 拆出说明 | A28；CLI/WASM/std 同 ABI，新旧混合明确拒绝 |

## 4. 可直接转 Issues 的恢复工作项

每行已具备标题、类型、范围依据、最小用例/差距、依赖与验收。**这不是一次批准全部实施的列表**：E 行恢复已定方向的工程账；D 行必须先处理所列决策或核对首版边界。本次未创建任何 Issue/Project。

| ID / 建议标题 | 分类与范围依据 | 当前差距 / 最小用例 | 依赖与验收标准 |
| --- | --- | --- | --- |
| A01 常用声明宏绑定与 builtin 卫生收口 | E（importer、append/copy、固定类型 make/new 子项已交付），D14/D21；[宏边界](../internal/macros/README.md)、[allocation 验收](../tests/acceptance/macro_allocation_test.go) | 已接通固定预声明类型 make/new 的 AST 门禁、准确 alias 身份和既有原生行为；命名/导入类型、泛型、new(expr)、其他 builtin/绑定仍未收口。类型捕获不因白名单取得资格；调用包同名及覆盖调用点 local type 的既有边界仍开放，A01 不关闭 | 白黑例先规范，真实 CLI/Go/WASM、跨定义包 alias、数组长度表达式与显式类型、捕获/权限拒绝、原生顺序对照、? 最近帧、LF/CRLF及独立审查；合成传播节点来源仍属 A03 |
| A02 常见模式宏及 log/tracing 场景矩阵 | E+D，已批准常见宏/日志场景；RFC0011 的完整 grammar U09 未冻结 | nested/optional repetition、广义 tt/type/stmt/item fragments 尚无；现有 event_if 是惰性条件日志，未提供完整 tracing span 语义 | 先把具体调用形状及歧义规则定清，保留当前有限 expr/ident/tt；嵌套重复长度/零次/预算拒绝，disabled 日志不求值，source span 保留。span/subscriber API 无明确授权证据时标待确认 |
| A03 完整宏→降低诊断来源链 | E（已交付首片），RFC0011/0018；[source tests](../tests/acceptance/source_mapping_test.go)、[诊断收口](reviews/macro-derive-diagnostics-20261005.md) | 已修模板 AST token、`?` 前处理来源/栈、具体 derive 行级原始位置与 enum 阶段错位；单文件 importer 复用随 A01 收口；捕获生成括号、appended helper alias、合成传播节点及部分后端诊断仍 generated/unmapped，A03 不关闭 | 从真实 Go 编译失败倒查定义/调用/捕获位置；重复 token、多层、CRLF、跨文件保身份；真正无法证明的合成节点保留 unmapped |
| A04 match 普通表达式组合长尾 | E，D31 明确工程欠账；[matchexpr](../internal/matchexpr/README.md) | `return -(match ...)`、`S{N:match ...}`、控制头、package initializer、复杂 LHS；builtin/type-conversion call 未接入 | 从已有 prelude/value 扩展，逐上下文正反例；副作用一次、短路、lhs 先求值后写入/panic、goto、? 失败跳后续、direct recover 无额外帧；不一次新增所有模式 |
| A05 多结果/不可命名类型的 match 与传播 | E+D，RFC0009/0018 | `return match ... , other()` 组合、多结果 arm 语法/类型需分开；现负例 `func()(int,int){return match ...=>1}` 本身结果数不匹配，**不是合法多结果需求的充分复现**；`m.Hidden()(match...)` 缺可见类型拼写 | 先给合法 expected-type 用例及结果数规则；使用真实类型 witness 不退化 any；保返回槽/defer/原函数帧和 Go 原 arity 错误 |
| A06 单文件与外部增强恢复补齐 | E，D30；[enum artifact](../internal/frontend/enum_artifact.go)、[refs artifact](../internal/refs/artifact.go) | 单文件导入生成 enum/协议不能按目录模式恢复；internal-origin 不可达 validator/payload、外部受限载荷仍有界 | 明确单文件 package/config 来源；两独立 module + Go consumer，public alias/relay、移动路径、错 schema/hash/权限/owner 拒绝；internal Go 可见性不绕过 |
| A07 手写 Clone/Debug/serde 字段协议登记 | E+D，D14/D23 目标、RFC0013 明列工程账；精确入口未批准部分待定 | 只有显式 derive 来源可恢复（manual Default/Eq 已有）；已有 typed serde helper 不能作为自动字段协议 | 先选具体非泛型签名/登记语法，不依赖完整 trait；复用 canonical registry，跨包 alias/方向/重复/伪 helper/陈旧产物负例，调用一次且保 cause |
| A08 blank/embedded 字段规则 | D，RFC0013 与 [T0 终账](t0-acceptance.md) 明确待决定 | `type S struct{_ int;X int}`、`type S struct{Inner}` 当前拒绝。Default/Clone/Eq/Debug/serde 各自规则未定 | 分协议决定忽略/参与、Debug 名称、serde 嵌套/展开及冲突；决定前不偷偷沿 encoding/json flatten，不把拒绝当永远范围 |
| A09 递归/泛型协议边界分解 | E+D，D28/RFC0013、RFC0008 | DefaultItem→Wrapper→Item 静态环即便业务终止仍拒绝；循环 slice/map equality 未定；generic derive/enum 不支持；引用/pointer/interface 不自动有协议 | 分“有限真实类型可证明资格”和“新循环图/泛型协议”；新规则先定，记录终止/成本/身份。不要为了 concrete derive 要求先完成通用 trait |
| A10 问号控制流与残余 guard 路由 | E，D22/D31、[propagate README](../internal/propagate/README.md) | 未规划 private target/动态 tuple/unsafe 组合仍走 assignmentFrame；函数迭代器 range 源、迭代赋值目标、select candidates 及不求值 unsafe.Sizeof/Alignof/Offsetof 操作数中的 ? 未支持 | [private targets](../tests/acceptance/propagate_private_place_test.go) 等现有测试为基线；去 fallback 不改值/时序/原始 panic 与 recover；分别验 guard 路径和无正常失败 panic 的性能目标，不把所有 ? 都标未实现 |
| A11 Error/thiserror 具体派生扩展 | E+D，已批准 Error/错误链，D22；[error.go](../internal/deriveextras/error.go) | source 必须字面 error；from 仅 sole error 字段；enum Error、typed source、透明/复合格式尚无已定完整合同 | 先冻结 typed source/enum 的小片行为；errors.Is/As/Unwrap/typed-nil、显式一步 From、歧义拒绝；多字段业务构造不自动丢值；不要求完整 thiserror API 兼容 |
| A12 serde 剩余核心/JSON 验收逐项收口 | E+D，D23、“enum 后补 serde enum”；下节 S01–S12 | tuple/struct variant、Number/Value、字段 adapter、自定义字段 codec 等不能靠“JSON 已通”关账 | 先 concrete enum 表示和字段协议，再 typed derive→核心→JSON→visitor；每个未定语法单独选择，不恢复公开 core Value 强制全树 |
| A13 pub 与 enum 函数入口策略 | D+E，D13/RFC0015，当前待答复 | `func Public(x E)` 未保证业务体前校验；目前 match/协议使用点检查≠公开输入入口；pub(scope) 也非 Go 首字母规则 | 决定直接参数检查时点/panic、None 合法性、字段/回调范围；验证非法值副作用前失败、合法 Go 调用，不能声称递归共享图全安全 |
| A14 use/pub use 现有透明语义剩余绑定 | E，RFC0021 | `use api::Maybe as Local; Local[int]::Some(...)`；宏模板 free use 函数、具名 composite literal 的 field/map-key 分辨、受限泛型 alias 约束 | [use tests](../internal/frontend/use_test.go) 扩展到真实身份；函数仍原帧，变量仍原槽，private/internal 不泄漏；缺 macro 类别的 glob 不静默丢项 |
| A15 mod/pub(scope) 目标与路径提案分轨 | D+E，模块研究 RFC0007 明载用户目标；0024 仅新路径提案 | 完整 mod/普通 pub/pub(scope) 无实现；不能拿 `use "fmt"::Println` 未批来取消整个模块目标 | 先确定目录/模块边界和可见性，不强套 Rust 文件树；先已有 Go package + pub use，再 scope/装载；包环/同名/私有提升/Go 导出差异端到端。0024 另行决策 |
| A16 cfg 与可选编译作者体验 | E+D，RFC0022 已批准 feature 与相关宏可选编译 | 文件 cfg/单行 cfg! 已成；多行 cfg!、声明级 cfg、宏生成 cfg 尚无。当前不能把常量 if 当作关闭语义检查 | 多行用 source-map edits 验 CRLF/零缩进右括号/否定/后续诊断；声明 cfg 的阶段与 grammar 先定；关闭文件不装 import/宏，unknown feature 不短路吞掉 |
| A17 target/dep 作者闭环 | E+D，RFC0022/25 | 无 `.gox` 测试入口、run 程序参数、非 host target；依赖 lib 未引用的增强子包不保证构建，module DAG 不支持所有包级无环交错 | 逐项核对首版范围，保留已批准多 bin/example/0–1lib 成果；新增子包按实际 import 图、missing edge、跨 module metadata；跨目标须 host/target 分离，不能引用 CLI 交叉构建冒充用户项目支持 |
| A18 workspace/host resolver 与锁运维 | E+D，RFC0026 主合同已做，扩展 Proposed | 统一 host 依赖/继承 resolver、dev 分组、weak/profile、精细 lock update、自动物化/发布无实现；全局 trust lease 有并发限制 | 分角色 feature/来源闭包、根锁统一版本、更新限定来源且不动无关 pin；搬移/复制锁离线、未选成员不激活、脚本新版本重新许可。扩展首版范围待确认，不重做 workspace 核心 |
| A19 独立 module 宏发布合同与实现 | D→E，RFC0011/0021、D30 增强消费目标 | source registry 同 module 可用；仅生成 Go/metadata 的宏无法反序列化，mixed glob 明确拒绝 | 确定源码/规则承载、版本/配置闭包及定义点身份；两 module source/version 渠道、缓存篡改/旧 schema/internal helper/递归预算；普通 Go 消费不需展开器 |
| A20 原位 match mut& 的表示与实现 | D→E，D29 首版要求；独立 enum 后阶段 | 当前 interface+值 case 无可写原 payload 地址；不能取副本地址，不能偷偷改普通赋值为共享 | 按 [place 研究](rfcs/research/match-place-and-expression-options.md) 收敛 ABI；借用改原槽、enum 复制不变、旧别名与 serde/跨模块/Go API 回归；完整风险分析不是依赖 |
| A21 自然 Go pointer 桥复合用例 | E+D，D31，RFC0006 | 标量已成；struct/slice/map/返回/callback/定义指针边界未全通 | 真实 go/types 同型判断、原存储/GC/留存、readonly 不升级；递归视图语义先定，不用 unsafe 强转或 default-deny |
| A22 普通引用类型/局部权限精度 | E+D，D21/RFC0005 | named/multiple result、引用 method/callback/container，以及 switch/select/goto/暴露字段部分更新精度 | 按明确合法例逐个补，旧 alias/兄弟字段/同步重绑仍拒权限擦除；不要求 W01–W06、不发明 escape 禁令；Arc API D08 另定 |
| A23 trait/declare/关联类型/固有 impl 首版切片 | D→E，D04–D07/R24/RFC0002–04/19/20 | 方向已确认、精确 MVP 范围待确认；Default/Eq 特例不是通用实现；“完整 trait 后置”不能撤销全部具体 impl | 先 receiver/固定 wrapper 元数据和非泛型具体调用，后普通 Item/泛型证据；T/D/A/W/C 用例按各 RFC 编号转测试；优化不得变 impl/动态身份 |
| A24 safe std/锁/Vec/Map 首批入口 | D→E，D09/D15/U10 | 公开 std 目前 sum；不能以 Go std 原生可调用宣称 safe 集合已成 | 逐 API 选择真实契约/成本，先业务需要；部分 I/O、初始化/复制/范围/锁身份验收；不预设三个 Map 均首版，不以完整 borrow 阻塞 |
| A25 quote/syn 与 LSP 的首版边界 | D+E，D16/D17/U11/U12 | 公共 syntax API/完整 LSP 未闭环；内部 source map≠LSP；用户过程宏不是普通工具包 | 明确首版交付再排实现；LSP 复用同语义诊断/宏来源，跨包重命名等按支持子集验收；自举/过程宏不能由“工具”暗中带入 |
| A26 记录层对齐与状态拆分 | L，所有 RFC 的维护规则 | 同文前后矛盾，网站 accepted/partial 混合设计和工程维度 | 本文作为当前差距入口；以后每片关闭 ID、挂 tests/SHA、将旧标题标历史；网站借用风险显式非 MVP，traits/quote/过程宏分开 |
| A27 可复现分发及迁入剩余合同 | E+D，RFC0001/25/26 | 缺完整 publish/package、通用声明发行；旧 JSON/format/锁自动升级不支持；retract/tool/godebug 等须按迁入模式区别，不能一概说未支持 | 无源普通 Go 与增强消费各验；拷出继承/路径/锁、跨机器冷暖缓存；未支持 directive 全拒而非丢弃。逐功能确认首版范围；不创建 vendor 拆出任务 |
| A28 发布证据与源码状态统一 | L，网站已经在线当前基线，非生产功能 gap | 静态 main README 旧，编译器分支 handoff “待 owner”旧；网站分类粒度不足 | 记录 live manifest 的 compiler/site/ABI/std/serde；实际部署状态独立于源码与库 release，勿自动部署；更新摘要不得发布内部 RFC/完整源码 |
| A29 格式宏的跨包协议与调用位置 | E+D，D14/RFC0012，现有有限格式实现不是全协议 | 目前仅 builtin scalar/alias 与同包已登记 Debug；named scalar、导入/自定义 Display/Debug、defer/go/initializer/值位置未支持，见 [print_test](../internal/frontend/print_test.go) 和 [宏说明](../internal/macros/README.md) | 先明确 formatter 和 Unit/错误策略，再接已登记跨包协议；所有参数先求值一次再格式化、失败/短写/不自动反射或 Stringer/error fallback；新增位置保持 defer/go 求值语义，更多格式说明符不从 Rust 自动引入 |

## 5. serde S01–S12 逐条对照

依据 [serde 母 RFC §10](rfcs/research/0006-serde-core-and-formats.md) 和 [JSON 已实现合同](serde-json.md)。库代码固定在 [serde a1b791e](https://github.com/goxide-lang/serde/tree/a1b791e96d660c6be620e110c566a9a171631b7e)。下表不把母 RFC 的“首版后置”说明自动当成用户逐项批准：YAML 有 D23 的未来格式依据；Value 为可选格式层、adapters 为 Proposed，其他首版细项无独立证据则待确认。

| ID / 承诺 | 实现及测试证据 | 未闭环用例 / 归属、依赖与验收 |
| --- | --- | --- |
| S01 typed Order、跨格式复用 | `protocol.go` 的 Serializer/Deserializer/Visitor；[serde_test](../tests/acceptance/serde_test.go) 与 [external](../tests/acceptance/serde_external_test.go) | JSON 文本闭环已成；第二 Value 后端未成。D23 格式分层已定，Value 可选细项待确认；验同一个 helper/visitor 在第二后端，而非强制 core 树 |
| S02 精确数值 | `json/json_test.go:TestScalarsAndStrictNumbers`、`json/cause_test.go`；acceptance 大整数/范围路径 | 已覆盖 int64/uint64 与 float 策略；不应笼统列“数字未做”。进一步类型扩展需逐个证据，保留字段路径和实际 cause |
| S03 任意精度 Number | JSON Any 仅机器数值，Skip 能消费超范围 token，见 serde-json | E+D：Number/RawValue/Value 格式扩展未实现，非 core Decimal；选择 API 后验大数不经 float64、不把业务同名 struct 当协议 |
| S04 missing/null/default/Option/PATCH | [serde_defaults_test](../tests/acceptance/serde_defaults_test.go)、[option_serde_test](../tests/acceptance/option_serde_test.go) | 普通 Option 缺失/null→None、default/skip 已成；PATCH 三态未成，adapter 缺失策略 Proposed。不得重开普通 Option 规则；验 missing/null/value 独立能力后才宣传 PATCH |
| S05 bytes / adapter | `json/json_test.go:TestBytesAndPaths`，默认数字数组 | Base64 字段 adapter 未实现，入口 D；不自动沿 encoding/json []byte。依 A07/A12，验非法 string/元素范围、替换整个字段 codec |
| S06 enum 全外标记形状 | `protocol.go:UnitVariant/NewtypeVariant`、`VariantAccess.Unit/Newtype`；`json/json_test.go:TestEnums`、[enum_wire_test](../tests/acceptance/enum_wire_test.go) | unit/newtype/string/int64 映射已成；tuple/struct variant 接口尚无，源 enum 多 payload 语法待定。按 enum→serde 顺序，验 unknown/duplicate tag、实际载荷路径，不另造 serde enum 类型系统 |
| S07 字段规则 | rename/default/skip/deny_unknown_fields 与方向独立；上面 defaults/external 测试 | blank/embedded、custom fields/manual registry 未闭环。先 A08；验缺 Default/冲突/未知字段深层错误、skip 消费预算 |
| S08 严格 JSON | `json/json_test.go:TestStrictSyntaxEvenSkipped`；duplicate/escaped key、Unicode、第二顶层值 | 既有严格策略与 Go 原生差异已记录，不是未做；新增 format 不得悄悄改变已定策略 |
| S09 协议状态机 | `TestCallbacksAndStates`、`TestIgnoredProtocolErrorsRemainErrors`、core callback tests | 已覆盖单值消费、状态 poison；尚未有 tuple/struct/Seed 扩展。依新增协议各测 End/跳 value/重复回调，不能从已有有限协议外推全 serde-rs |
| S10 预算/循环/IO/cause | JSON 16MiB/深度128，acceptance `TestCycleAndDepthBound`；[serde_causes](../tests/acceptance/serde_causes_test.go) | 循环以预算有界失败，不是对象图协议；当前 byte-buffer API 无流式 IO 部分写合同，验收此格未全成。流式 API 首版范围待确认，不把普通 cause 已保留列欠账 |
| S11 泛型/原生字段/权限 | 具体容器/Option、跨包已登记 codec；[serde_protocol_artifact_test](../internal/frontend/serde_protocol_artifact_test.go) | general generic codec、只读/原生字段适配、自动 MarshalJSON 均非完成；用户泛型 ABI 与 adapter 合同先定。不能反射/any 自动补洞或拆 Arc/锁 |
| S12 owned/并发/成本 | JSON owned string/bytes、同步 callback 与预算文档；生成 race 用例已存在 | 未找到完整巨大数组峰值内存/分配基准闭环；race 通过不证明任意共享源安全。补输入缓冲复用与并发前提正反例、固定大小/工具链成本报告；性能承诺不能从有限样例推导 |

## 6. 研究 RFC 全量归档判定

| 研究文件 | 本次判定与接续 |
| --- | --- |
| [0001 scoped borrow](rfcs/research/0001-scoped-borrow-views.md) | D19/D20 warning-first，风险分析 N；B01–08 默认 Go 兼容、W01–06 不列 MVP 门槛 |
| [0001 usage](rfcs/research/0001-usage-and-tradeoffs.md) | 同一长期线场景补充，权限错误仍检查；不是新增默认拒绝依据 |
| [0002 native bridge](rfcs/research/0002-native-pointer-bridge.md) | D31 已覆盖旧 A/B 待选；标量自然桥 C，其余 A21；风险分析小节 N |
| [0003 pointer roles](rfcs/research/0003-pointer-roles-and-mvp.md) | Arc D08 保留，Box/Cell/H 合同 Proposed；A20–22，不能由功能重叠删 Arc，也不要求完整所有权 |
| [0004 trait lowering](rfcs/research/0004-trait-lowering-paths.md) | R24 固定 wrapper 已选；内部不可观察优化与 Go 机制保留，通用前端 A23 未成 |
| [0005 Option/Result ?](rfcs/research/0005-option-result-question.md) | D22/D36、多个控制流增量已成；A10/A11 和当前合法上下文长尾；通用 Try/Residual/转换链不属当前合同 |
| [0006 serde](rfcs/research/0006-serde-core-and-formats.md) | 母合同分层 D23；S01–S12 上表，不按“JSON 可跑”全关 |
| [enum equality](rfcs/research/enum-equality-proposed.md) | 标题 Proposed 不能覆盖 D32–35 后续确认与 explicit/container/manual 实现；循环图/更广键仍 A09 |
| [match place/expression](rfcs/research/match-place-and-expression-options.md) | D29 需求已定，tag-struct/IIFE 只是表示与机制候选；A20；原函数 prelude 现已交付多种表达式，不等于借用已成 |
| [serde adapters](rfcs/research/serde-field-adapters-proposed.md) | 明确 Proposed；time.Time/RawMessage typed helper 可手写，字段 with 等注解未批准/未实现；A07/A12，先入口和缺失策略 |
| [shared sum/inference](rfcs/research/shared-sum-abi-and-inference.md) | 历史“每包一套/省参未做”已被 shared std 实现与公开库报告覆盖；共享/有限省参 C；无上下文/联合推导细项不自动承诺 |
| [分支 0007 mod/reexports](https://github.com/hxsf/goxide/blob/29d81ecf06d7a9cfb383aa1e264a7bcd31d1fb6b/docs/rfcs/research/0007-modules-and-reexports.md) | 用户模块目标明确；Go 机制/绑定模型不是生产实现；0021 已超越当初不支持变量/宏的首片建议，mod/pub(scope) 仍 A15 |

## 7. 网站、源码与文档不一致的具体位置

2026-10-05 读取 [线上 examples.json](https://goxide-lang.github.io/examples.json) 成功（web reader 不可达后用 HTTPS curl 获取）。只核对公开清单，不把清单声明当本轮浏览器重新执行：compiler=`5d77782`、website=`6dc779a`、compilerDigest=`27323c6bf7a3066bab1d6546355e681e2b2d548e10c07389ffe2793f6541aa42`，std/serde 为 §1 版本，ABI=8/3/2/2。现站**不是旧 vendor 实现**。网站本身已有 source-selection 的 CLI/PTY 快照与历史 QA，[网站验收](https://github.com/hxsf/goxide/blob/6dc779a651af3eb34121f53637a55bfb6feee387/website/validation/source-selection/RESULTS.md) 可追溯；本次不重新发布。

| 位置 | 不一致 / 影响 | 应采用的结论 |
| --- | --- | --- |
| 编译器 [ROADMAP](../ROADMAP.md) M2–M8 与末尾追加 | M8“未开始”、workspace 研究“未实现”、早期 alias/match/Option 限制和已完成新节并存 | 是阶段历史，不是当前 backlog；以本表已完成/未完成细项区分 |
| [RFC 索引](rfcs/README.md) | 0026 仍 Proposed/未实现，根20篇与检查说明及“尚不存在前端”过期，漏列正式命名0027；0024 只在分支 | 已有实际前端、26篇本地编号规范；本表纳入远端0024与mod研究，不凭目录统计删工作 |
| RFC0001/0018 开头、shared-sum 研究及 [T0](t0-acceptance.md) 中途 | 无加载器/无生成器、共享ABI整树失败等为旧阶段 | 新生产实现、公开库回归与 source-selection 全树报告覆盖；旧失败应留历史而非继续作阻断 |
| 编译器 [website/LATEST-INTEGRATION](../website/LATEST-INTEGRATION.md) 开头与尾部 | 开头69c1ab0、旧TTY未交付、尾部已实现但待owner | 网站6dc779a开头已改为5d77782，线上清单也已更新；属于交接记录欠账 L |
| 静态仓库 main README | 仍自称ae61ec6/旧示例集 | 不作为实际部署版本证据；live manifest 为当前，只报告读取时点 |
| 编译器 `website/catalog-source.json` 与网站分支 | 编译器分支记录3a8f204（生产实现提交）；网站6dc779a及live catalog记录5d77782（后续文档验收提交），production digest相同；26个能力条目=20 partial/1 verified/4 accepted/1 proposal，不是26篇RFC完成率 | 版本差异是交接粒度，非生产代码倒退；不能用状态计数推算MVP百分比 |
| 网站 catalog `borrow-future` | accepted/unavailable 标签缺 MVP 维度，容易和首版 gap 混淆 | D20 明确非 MVP，仍可单独长期跟踪 |
| 网站 catalog `traits` / `safe-libraries` / `syntax-tools` | 合并通用 trait 与已成 Default 特例；合并未定首批库；quote/syn 与明确延期过程宏共享 accepted | 按 RFC 子项拆分“方向确认/首版待定/已成特例/延期”，不可视为全部已获首版实现授权 |
| 网站 catalog `vendor`/`projects`/`workspace` partial | 合同内已完成、受限边界和未定扩展被一个标签压平 | 当前迁入/local/remote/offline 已完成；host resolver 等另开细项，不重新制造 vendor 待决 |
| 网站 catalog `serde`/`match`/`macros` partial | 大类限制基本正确，但缺逐项依赖/验收，无法说明为何停止 | 对应 A01–A12 和 S01–S12；partial 不能解释为远期 |
| 网站 README/FEATURES vs 浏览器能力 | CLI 工作流以构建时快照展示，不是浏览器执行 | 单文件 WASM 与 CLI 多包/脚本/迁入分开；不用浏览器未支持多包来误判 CLI 未做 |

本次只在源码仓库保存审计与路线入口，不把内部 RFC、对话记录、绝对工作路径或私人内容发布到公共网站；不附私人历史消息标识。

## 8. 下一轮顺序与停止条件

1. **先消除假 gap，保留真账**：沿 A26/A28 清理当前入口的历史状态；本次已加本文入口。维护完成切片清单，vendor/命名/共享 sum/推导/workspace 不重做。
2. **宏与具体派生优先**：A01 的已知卫生/builtin 合法用例、A03 来源链；A02 的常见模式矩阵先定具体片；A07/A29 的具体协议/格式入口与 A11 Error 需求分设计/工程。不能以完整 trait 尚未完成推迟所有具体宏。
3. **独立 enum/match 收口**：A04 普通表达式 → A05 类型/多结果 → A06 外部恢复；A08 字段规则与 A13 入口策略并列待决，不用未决问题挡住已定控制流修复。A09 的循环/泛型另立边界，不混入原位借用。
4. **按 enum 真实元数据补 serde**：A12/S06 与 A07，随后 S03/S05 等格式/adapter 合同；先实现已定工程，未批准注解只给具体选择。JSON 已有成功不等于 serde 全部完工，YAML 不抢首版 JSON 收口。
5. **工具链与模块工作主题**：A14 现有 use 绑定可直接补；A15 先冻结 mod/pub(scope) 与新路径各自合同；A16–18/A27 按真实项目阻断与已批准边界排序。features/deps/0–1lib/多bin/example/scripts/workspace 主干已有，不重开作者模型。
6. **指针阶段仍在首版账**：A20 表示决定后实现原位 mut&，再 A21–22。D29 不被“独立 enum 阶段先不依赖指针”撤销；D20 完整借用分析始终不作 MVP 门槛。
7. **trait/库/工具明确最小首版范围**：A23–25 保留方向与待定边界，不能擅自全放远期或一次实现全套。每一项决定记录 RFC 节点与验收例，不以用户曾说继续 MVP 替具体新设计背书。

每片完成条件：源用例成功/应拒绝用例定位正确 → 生成普通 Go 实际运行（适用的 race/vet）→ 独立 module/Go 消费与 metadata/配置失效 → 明确依赖与残余 → 更新本表 ID 和实际 SHA。MVP 完成前须逐个关闭 E 或取得明确范围决定；D 项不能以沉默当批准，L 项不能伪作编译器缺陷。有限专项通过不代表全语言完成。

## 9. 本轮验证与证据边界

本轮只改文档。重跑结果、命令和线上身份记录见 [审计验证记录](reviews/mvp-gap-audit-20261005.md)。历史全树基线另见 [source-selection 报告](reviews/source-selection-20261005.md) 与 [全树摘要](reviews/source-selection-evidence/full-race-summary.json)：报告记录22包、516顶层/1086子测试通过；本轮不把它冒称自己重新执行的全树测试。


## 2026-10-06 接续：A01 直接 append 子项

交付源码 `a2c7300966f3bf2c0a446ff13818bd4a8ce06c68`，工作分支 `feat/macro-append-builtin`。
先写 RFC0011 白例/黑例合同，复用既有 builtin 卫生机制，未引入新语义决定。
[交付与独立审查记录](reviews/macro-append-builtin-20261006.md)保留原生 Mac、CLI/WASM、
生成模块与来源对照证据。整树 534 项通过加唯一缓存失败补齐依赖后三次通过，覆盖 535 项；
不是一次整树退出 0，Linux 未重跑。

| 子项 | 状态 | 证据与剩余 |
| --- | --- | --- |
| A01 单文件 importer | C（前片） | 复用前端离线/WASM importer；浏览器可用包集合不扩大 |
| A01 直接 append | C（本片） | 命名 slice/存储/展开、定义点函数与模板局部身份、调用点局部/import 卫生、条件日志、? 最近帧、重复输出与黑例 |
| A01 其他 builtin/绑定 | E | make/new/copy；定义点 builtin 被调用包级同名或覆盖展开点局部 type 名称遮蔽；imported type/var/generic helper 的合法需求仍待收口，不以拒绝测试永久排除 |
| A03 完整来源链 | E | 15 个旧/新对照证实捕获合成括号等后端错误仍 generated/unmapped；append() 与宏绑定错误保留准确位置及展开栈 |

只关闭上述具体子项，A01/RFC0011 整项及 MVP 仍开放；blank/embedded、完整 trait、
borrow checker、待定 serde adapters 未在本片实施。


## 2026-10-06 接续：基线整树补验与 A01 直接 copy

上一片交付 `1cf4b58` 已用完整缓存补跑一次整树 race：535 项顶层/1117 子测试、
17 个测试包通过，无失败或跳过；先前离线缓存失败已在整树中消除。
新片从该提交建立 `feat/macro-copy-builtin`，先写 RFC 白黑例，代码提交
`49c428767de35259ce66d4e50887725f2fc0b85f`。
[本批交付、精确验证与独立审查](reviews/macro-copy-builtin-20261006.md)记录原生 Mac
完整整树 544 项顶层通过、vet/design、真实 CLI 和 CLI/WASM 差分。Linux 未运行。

| 子项 | 状态 | 完成 / 剩余 |
| --- | --- | --- |
| A01 直接 copy | C | 原生计数/存储/重叠/string/浅共享、绑定、条件/?/defer时序；已有只读标量来源许可及目标/指针/容器元素拒绝保持 |
| A01 其他 builtin/绑定 | E | make/new、调用包级同名/覆盖展开点 local type、imported type/var/generic helper；前节 append 交付时列出的 copy 缺口在本节关闭 |
| A03 完整来源 | E | 捕获合成括号、helper alias、传播节点及 refs 中间坐标等剩余继续保留；没有伪造源码精度 |

只关闭直接 copy 子项，A01/RFC0011/MVP 整项不关闭。托管 CI 未查到的状态与本地验证
分开记录；没有引入新 trait/borrow、blank/embedded 或 serde adapters 规则。


## 2026-10-06 接续：A01 固定类型 make/new

从 `9ca38dc` 创建 `feat/macro-allocation-builtins`，精确源码 `ec9dcbfba578c8c0553e4b1bfa32d0909463ae02`。
[本批研究、交付与证据](reviews/macro-allocation-builtin-20261006.md)记录类型门禁、
完整 Mac 整树 555 顶层/1200 子测试通过、vet/design、CLI/WASM及独立审查。

| 子项 | 状态 | 完成 / 剩余 |
| --- | --- | --- |
| A01 固定类型 make/new | C | 预声明类型与固定组合、数组长度表达式、嵌套 primitive alias 真身份；阻止类型槽捕获和顺带泛型开放 |
| A01 其余合法分配/绑定 | E | new(expr)、同包/导入命名类型与泛型身份、其他 builtin、调用包同名/覆盖点 local type；不能以当前拒绝永久排除 |
| A01 已有类别泄漏 | E | 其他模板返回类型等明确类型位置的 expr 捕获泄漏是基线已有问题，本片不冒称全局修复 |
| A03 完整来源 | E | new($x?) 更深门禁触发既有合成传播节点 unmapped/无栈；其他后端/helper/捕获括号边界照旧，不能伪定位 |

一般 type fragment 与完整模式语法仍需设计确认。只关闭固定类型 allocation 子项，
A01/RFC0011/MVP 仍开放；Mac 本地通过不推导 Linux 或托管 CI 通过。

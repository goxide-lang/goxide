# hgo Roadmap 与接续入口

更新：2026-10-01。暂名 hgo / Hyper Go，仓库为 `hxsf/goxide`。本文件记录实现进度和依赖顺序；设计确认状态以[决策登记](docs/design/decisions.md)为准。路线本身是暂定建议，不是交付日期承诺。

## 1. 当前仓库实际具备什么

- 已整理总纲与专题设计，并分别列出已确认、暂定、未定事项。
- 已有用于核对 Go 降低机制的文档代码和可复现验证；它们不是 hgo 编译器或基础包实现。
- 已有单文件有限前端、源类型检查、Go 生成与 CLI 构建原型，见 IMPLEMENTATION.md；完整 hgo 解析/包类型系统、必经 linter、LSP、基础包与自举尚未完成。
- Go 1.27 是已确认的目标语言和工具链下限；实现分支已有根 go.mod，设计和研究夹具由各自模块隔离。

## 2. 里程碑与完成条件

### 当前优先级（D21）

T0 是优先级，不是要求全部功能一次交付；下列 M 编号是依赖主题，不能据数字把宏和 enum 排在 trait 完成之后。

| 优先级 | 工作 | 范围与边界 |
| --- | --- | --- |
| T0 | 卫生宏、有限声明宏与基础派生 | 用户明确优先；分批选择子集，仍验收卫生、求值、诊断/源码映射、生成代码与工具链行为；收益判断不等于已验证无成本 |
| T0 | enum / match | 强化既有数值/string const 枚举，并保留带载荷 enum 方向；零值问题不阻塞优先级，明确 raw 值、未知值和 match 行为即可，不要求先解决全对象图合法性 |
| T0 | 只读/可写访问分离 | 基础类型与投影权限；共享/内部可变可经 hgo/std 包装表达。D08 Arc 方向保留，不要求先建整套智能指针库 |
| 实现方案研究 | trait 适配与性能 | 对照二进制内部消除包装、源码稳定 wrapper；研究不等于批准替代 D05/D06，见 RFC 0004 |
| 独立长期 | 别名/借用风险分析 | 按 D20 完全非 MVP；未来默认 warning-first，不引入未证明即拒绝 |


D20：别名/借用风险静态分析是独立长期工作线，不是 MVP 或 M1–M8 的前置完成条件；不要求先实现任意 W01–W04 子集。语言类型检查与可选风险诊断分层，源码/符号/类型映射自然保留，不为未实现分析器强迫复杂注解。

| 阶段 | 状态 | 依赖与完成条件 |
| --- | --- | --- |
| M0 设计基线与交接 | 文档已整理，Go 机制有验证 | 决策 / 遗留问题可查，文档示例验证可重跑；这不表示暂定方案已确认 |
| M1 最小语义骨架 | 有限 AST / 元数据原型，完整包语义未完成 | 冻结最小语法、包 / 符号身份、位置；hgo AST、Go 包类型加载、诊断与 IR。为卫生 / LSP 预留身份，不先用文本拼接替代语义 |
| M2 显式 trait 与 Go 采用 | 未开始 | 基于 M1；无体 marker + 返回自身、有体 wrapper、限定分派与期望类型适配；原生源文件保持不变；完成正反例和 Go 双向调用 |
| M3 引用 / unsafe 与锁子集 | 标量读写引用已运行，复杂投影/桥接未完成 | 定义可用引用语法、真实类型/明确 unsafe、U03 接收类型及 U15 原生桥接，完成普通调用与 Go 降低；Lock / RWLock 机制测试可作证据。风险分析不属于此阶段或 MVP 完成条件 |
| M4 enum / match 与边界 | 本地 enum、有限表达式 match 与内建 Option/Result `?` 已运行，完整阶段未完成 | 明确初始化、公开边界及选定 ABI；实现构造、穷尽、控制流、非法 Go 输入诊断或检查；容器与共享数据的未检查边界明确记录，不因缺证明普遍拒绝 |
| M5 泛型 trait 与静态操作 | 未开始 | 普通关联类型、投影、约束与选定后端策略；Default / Clone 所需的静态 Self 操作；不以 any 抹掉语义 |
| M6 声明宏与基础 derive | 有限原型已运行，完整阶段未完成 | 身份机制始于 M1；结合 M4/M5，实现匹配 / 重复 / 卫生、格式检查、print / println、Default / Clone / Debug、展开位置与负例。用户过程宏不在此阶段 |
| M7 首批 safe Go std 与泛型 slice | 未开始 | 基于已完成的引用、枚举与 trait 子集逐项适配；每个入口有 Go 契约、成本、边界与失败验证；不是全量 std blanket safe |
| M8 包发布与可用 LSP | 未开始 | 查询层从 M1/M2 演进；声明锁定、构建条件、隔离视图及导出产物可在第二台机器复现；LSP 与编译器诊断使用同一语义结果 |
| M9 自举 | 未开始 | 先有稳定可用语言子集及工具链；保留可构建 Go seed，逐步迁移并比较阶段产物和测试结果；具体阶段规则见工具链专题 |
| 后续语法工具 | 暂定，未开始 | quote / syn 作为额外包已确认；可先做普通工具，但是否与首个可用版本一起交付未定，需要稳定 token 协议 |
| 后续扩展 | 明确延期 / 未定 | 用户过程宏、可编程 derive、supertrait、GAT、特化、借用模式等逐项设计；不将 Rust / LLVM 汇编后端加入首版必需路径 |

这些是依赖里程碑，不要求严格串行，执行优先级以上表 T0 为准。M2 与 M3 需共同确定接收者权限，不能先冻结会擦除权限的 trait ABI；泛型 derive 依赖 M5 的静态操作；具体类型内建生成和有限声明宏可先闭环，不把完整泛型 trait 研究作为所有宏的前提。最小 LSP 查询与位置支持从 M1 开始，完整体验在 M8 验收。

### 独立长期工作线：风险静态分析

状态未开始，无 MVP 截止要求。W01–W04 与来源/效果分析的复杂度、依赖和暂缓项见 [RFC 0002](docs/rfcs/0002-native-pointer-bridge.md#6-分析复杂度与-mvp-边界)。未来按独立规则评估价值；warning-first 不等于分析容易，不以未完成阻塞语言开发。

Option/Result 专用问号见 [RFC 0005](docs/rfcs/0005-option-result-question.md)：限域与显式 From 单步错误转换方向已确认，不公开通用 Try/Residual、不搜索转换链。thiserror 先生成可验证的显式转换，本分支已接入有限问号前端与 Error 派生转换注册；不代表通用泛型/trait 求解完成。

本轮已授权在文档标签 `v0.0.0-mvp-doc` 之后启动宏原型：模式匹配/重复/卫生/诊断、结构化日志，Debug/Clone/JSON serde/thiserror 内建派生；采用明确语法子集完成输入到 Go 编译运行。完整语言和未定 trait 后端不因此宣称已完成。

标签后最新优先级（D23）：**先 serde 闭环，仿照 serde-rs**。本线程整理官方协议/Visitor/Access 与格式分层，实施由独立会话推进；[RFC 0006](docs/rfcs/0006-serde-core-and-formats.md)规定核心协议→typed 派生 helper/visitor→serde-json、可选 JSON Value 和 S01–S12 验收，serde-yaml 后续。此前公开 core Value/强制全树中转候选已撤回；具体 Go ABI 与支持子集仍候选，派生不绑定 encoding/json。本分支已有具体类型核心/JSON 闭环，完整协议扩展与 YAML 仍待实现；准确范围见 IMPLEMENTATION.md。

## 3. 下一轮优先处理

1. 阅读[决策登记](docs/design/decisions.md)与[总纲](docs/design/overview.md)，不要从早期 `*T` 示例推导 safe 源级指针语法。
2. 回到真实语言场景、语法与 lowering：优先评审 [指针职责与 MVP](docs/rfcs/0003-pointer-roles-and-mvp.md)，按 §6.1 区分运行时最小能力与 D08 Arc 已确认方向。只读投影/接口断言是待闭合的基础类型设计，H 临时契约不是既定要求；U15 桥接不在本轮催定；风险分析按 D20 长期独立，不作为实现门槛。
3. 冻结最小解析 / 包 / 类型身份，再决定仓库代码布局；按 T0 选择宏/enum/match/读写类型的小闭环，trait 双路径先按 [RFC 0004](docs/rfcs/0004-trait-lowering-paths.md) 研究。当前未授权的未来功能不因列在本文件就自动启动。
4. 泛型字典 / 专门化、静态 Self、enum ABI、格式化和声明包选择应分别形成小范围决策，不在一个“全语言实现”中静默选定。

## 4. 在另一台机器继续

```sh
git clone git@github.com:hxsf/goxide.git hyper-go
cd hyper-go
git switch main
python3 scripts/check_design.py --docs-only
python3 scripts/check_design.py
```

已有 checkout 使用正常 fetch / pull 更新，不需要复制本机临时目录。需要 Python 3.10+，以及支持 `go 1.27` 的 Go 1.27 或更新工具链；完整验证包含 race 检查，平台须支持 Go race detector。SSH 不可用时可使用自己的 GitHub HTTPS 认证；仓库不保存机器专属凭证或路径。

验证入口的具体范围、负例与平台要求见 [tests 说明](tests/README.md)。完整检查只证明已覆盖 Go 降低示例的行为；设计文档中的完整 `.hgo` / `.d.hgo` 片段并非都能编译；可运行输入以 examples/prototype 和 IMPLEMENTATION.md 为准。

接续文档顺序：本文件 → 决策登记 → 总纲 → 当前要处理的专题。临时实验路径不作为设计输入，历史 Go 实验与持久复现范围须分开记录。

## 5. 进度更新规则

设计确认与实现完成分别更新：有设计、能编译一个 Go 示例、hgo 正反例通过、跨机器互操作通过，是不同证据。阶段完成时记录入口、实际检查与剩余限制；新增讨论同步状态表，避免另一台机器只见到过期“待写”或未经确认的规范。

## 实现分支阶段结果

本地已完成 `.hgo → 宏/derive 展开 → Go → 编译运行` 的具体类型闭环：serde 核心及 JSON、Debug/Clone/Error、有限模式宏和 slog 场景。见[可复现命令、测试范围及缺项](IMPLEMENTATION.md)。不将此结果标记为完整 M1/M4/M5/M6 或 LSP 完成；冻结文档标签不变。

用户明确实施顺序：先完成 enum/match 语言闭环，再以同一真实 enum 元数据补 serde enum；不先建立 serde 专用变体表示。当前本地已完成有限非泛型语句 match，用户泛型仍待实现；后续已交付有限表达式 match 与内建 Option/Result 专用 `?`，见[实际边界](IMPLEMENTATION.md)。

后续已基于真实 enum 元数据交付 Serialize/Deserialize、unit/newtype JSON 表示及与 struct/container 的组合测试；泛型/跨包/复杂载荷和变体属性仍不支持。

标量读写引用已有独立源类型检查与普通Go lowering，包含复制/返回/显式收窄和权限擦除反例；不是完整只读投影、Arc/AsPtr协议或借用分析器。


Option/Result 阶段已有真实 Go API 显式包装、不同成功类型的传播和 sole-error-field `#[from]` 单步转换示例；表达式 match 单独提供可运行入口。内建泛型不等于用户泛型，转换注册不等于通用 From 搜索。宏固定类型、新引用与传播组合、unsupported 表达式上下文及运行证据见 [实现边界](IMPLEMENTATION.md)。下一阶段组合工作与完整 M1/M4/M5 仍未完成；不改变已确认设计。

后续 T0 组合已打通普通 sum 载荷与标量引用/派生/传播，以及 JSON 配置→Result/From→enum match→结构化日志应用。引用载荷、宏引用签名函数别名、复合只读投影与完整 source map 仍有限或未做。每项实际测试与必需/后置划分见 [T0 验收矩阵](docs/t0-acceptance.md)；不能由应用成功推导任意跨功能组合已完成。


同 module 源码包声明宏现支持 `pub macro_rules!` 和普通 import 的 `alias.name!`，保留每个定义文件的作用域、私有嵌套宏和公开 runtime shim 边界。真实日志包应用覆盖禁用时不求值、跨文件别名、同名宏、enum/serde/Result/Error 组合；包和整个模块共享资源预算，诊断保留有证据的定义/调用原始跨度。见 [实现记录](IMPLEMENTATION.md) 和 [增量验收](docs/t0-acceptance.md)。外部预编译宏 metadata、过程宏、任意模板语法及完整来源映射仍未完成；M6/T0 未整体完成。

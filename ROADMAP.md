# hgo Roadmap 与接续入口

当前覆盖规则：D27 只保留封闭变体 enum；旧开放 raw/C enum 及其别名待决项撤回。数值/string 属于 serde 外部映射，未知 variant 报错，业务 validator 后置。历史阶段 raw 验收不再代表当前支持。RFC 0019/0020 已同步设计，尚不代表 trait/固有 impl 实现。
更新：2026-10-01。暂名 hgo / Hyper Go，仓库为 `hxsf/goxide`。本文件记录实现进度和依赖顺序；设计确认状态以[决策登记](docs/design/decisions.md)为准。路线本身是暂定建议，不是交付日期承诺。

## 1. 当前仓库实际具备什么

- 已整理总纲与专题设计，并分别列出已确认、暂定、未定事项。
- 新增[20 篇逐特性 RFC](docs/rfcs/README.md)：以转译前后、双向互操作和拒绝例子约束设计；部分已有有限端到端实现，完整范围见实现记录。
- 已有用于核对 Go 降低机制的文档代码和可复现验证；它们不是 hgo 编译器或基础包实现。
- 已有单文件有限前端、源类型检查、Go 生成与 CLI 构建原型，见 IMPLEMENTATION.md；完整 hgo 解析/包类型系统、必经 linter、LSP、基础包与自举尚未完成。
- Go 1.27 是已确认的目标语言和工具链下限；实现分支已有根 go.mod，设计和研究夹具由各自模块隔离。

## 2. 里程碑与完成条件

### 当前优先级（D21）

T0 是优先级，不是要求全部功能一次交付；下列 M 编号是依赖主题，不能据数字把宏和 enum 排在 trait 完成之后。

| 优先级 | 工作 | 范围与边界 |
| --- | --- | --- |
| T0 | 卫生宏、有限声明宏与基础派生 | 用户明确优先；分批选择子集，仍验收卫生、求值、诊断/源码映射、生成代码与工具链行为；收益判断不等于已验证无成本 |
| T0 | enum / match | 只保留封闭变体enum；serde负责数值/string外部映射，未知variant解码报错；现有Go非法表示边界检查保留，业务validator后置，不要求先解决全对象图合法性 |
| T0 | 只读/可写访问分离 | 基础类型与投影权限；共享/内部可变可经 hgo/std 包装表达。D08 Arc 方向保留，不要求先建整套智能指针库 |
| 实现方案研究 | trait 适配与性能 | R24/RFC0019已确认统一wrapper；优化不改变实现选择、身份与行为，旧双路径仅为历史研究 |
| 独立长期 | 别名/借用风险分析 | 按 D20 完全非 MVP；未来默认 warning-first，不引入未证明即拒绝 |


D20：别名/借用风险静态分析是独立长期工作线，不是 MVP 或 M1–M8 的前置完成条件；不要求先实现任意 W01–W04 子集。语言类型检查与可选风险诊断分层，源码/符号/类型映射自然保留，不为未实现分析器强迫复杂注解。

| 阶段 | 状态 | 依赖与完成条件 |
| --- | --- | --- |
| M0 设计基线与交接 | 文档已整理，Go 机制有验证 | 决策 / 遗留问题可查，文档示例验证可重跑；这不表示暂定方案已确认 |
| M1 最小语义骨架 | 有限 AST / 元数据原型，完整包语义未完成 | 冻结最小语法、包 / 符号身份、位置；hgo AST、Go 包类型加载、诊断与 IR。为卫生 / LSP 预留身份，不先用文本拼接替代语义 |
| M2 显式 trait 与 Go 采用 | 未开始 | 基于 M1；统一 wrapper + marker、固定包级工厂、本地 As / 外部 ForT.As helper、限定分派与期望类型适配；原生源文件保持不变；完成正反例和 Go 双向调用 |
| M3 引用 / unsafe 与锁子集 | 标量读写引用已运行，复杂投影/桥接未完成 | 定义可用引用语法、真实类型/明确 unsafe、U03 接收类型及 U15 原生桥接，完成普通调用与 Go 降低；Lock / RWLock 机制测试可作证据。风险分析不属于此阶段或 MVP 完成条件 |
| M4 enum / match 与边界 | 本地 enum、有限表达式 match 与内建 Option/Result `?` 已运行，完整阶段未完成 | 明确初始化、公开边界及选定 ABI；实现构造、穷尽、控制流、非法 Go 输入诊断或检查；容器与共享数据的未检查边界明确记录，不因缺证明普遍拒绝 |
| M5 泛型 trait 与静态操作 | 未开始 | 普通关联类型、投影、约束与选定后端策略；Default / Clone 所需的静态 Self 操作；不以 any 抹掉语义 |
| M6 声明宏与基础 derive | 有限原型已运行，完整阶段未完成 | 身份机制始于 M1；结合 M4/M5，实现匹配 / 重复 / 卫生、格式检查、print / println、Default / Clone / Debug、展开位置与负例。用户过程宏不在此阶段 |
| M7 首批 safe Go std 与泛型 slice | 未开始 | 基于已完成的引用、枚举与 trait 子集逐项适配；每个入口有 Go 契约、成本、边界与失败验证；不是全量 std blanket safe |
| M8 包发布与可用 LSP | 未开始 | 查询层从 M1/M2 演进；声明锁定、构建条件、普通 Go 模块及导出产物可在第二台机器复现；LSP 与编译器诊断使用同一语义结果 |
| M9 自举 | 未开始 | 先有稳定可用语言子集及工具链；保留可构建 Go seed，逐步迁移并比较阶段产物和测试结果；具体阶段规则见工具链专题 |
| 后续语法工具 | 暂定，未开始 | quote / syn 作为额外包已确认；可先做普通工具，但是否与首个可用版本一起交付未定，需要稳定 token 协议 |
| 后续扩展 | 明确延期 / 未定 | 用户过程宏、可编程 derive、supertrait、GAT、特化、借用模式等逐项设计；不将 Rust / LLVM 汇编后端加入首版必需路径 |

这些是依赖里程碑，不要求严格串行，执行优先级以上表 T0 为准。M2 与 M3 需共同确定接收者权限，不能先冻结会擦除权限的 trait ABI；泛型 derive 依赖 M5 的静态操作；具体类型内建生成和有限声明宏可先闭环，不把完整泛型 trait 研究作为所有宏的前提。最小 LSP 查询与位置支持从 M1 开始，完整体验在 M8 验收。

### 独立长期工作线：风险静态分析

状态未开始，无 MVP 截止要求。W01–W04 与来源/效果分析的复杂度、依赖和暂缓项见 [RFC 0002](docs/rfcs/research/0002-native-pointer-bridge.md#6-分析复杂度与-mvp-边界)。未来按独立规则评估价值；warning-first 不等于分析容易，不以未完成阻塞语言开发。

Option/Result 专用问号见 [RFC 0005](docs/rfcs/research/0005-option-result-question.md)：限域与显式 From 单步错误转换方向已确认，不公开通用 Try/Residual、不搜索转换链。thiserror 先生成可验证的显式转换，本分支已接入有限问号前端与 Error 派生转换注册；不代表通用泛型/trait 求解完成。

本轮已授权在文档标签 `v0.0.0-mvp-doc` 之后启动宏原型：模式匹配/重复/卫生/诊断、结构化日志，Debug/Clone/JSON serde/thiserror 内建派生；采用明确语法子集完成输入到 Go 编译运行。完整语言和未定 trait 后端不因此宣称已完成。

标签后最新优先级（D23）：**先 serde 闭环，仿照 serde-rs**。本线程整理官方协议/Visitor/Access 与格式分层，实施由独立会话推进；[RFC 0006](docs/rfcs/research/0006-serde-core-and-formats.md)规定核心协议→typed 派生 helper/visitor→serde-json、可选 JSON Value 和 S01–S12 验收，serde-yaml 后续。此前公开 core Value/强制全树中转候选已撤回；具体 Go ABI 与支持子集仍候选，派生不绑定 encoding/json。本分支已有具体类型核心/JSON 闭环，完整协议扩展与 YAML 仍待实现；准确范围见 IMPLEMENTATION.md。

## 3. 下一轮优先处理

1. 阅读[决策登记](docs/design/decisions.md)与[总纲](docs/design/overview.md)，不要从早期 `*T` 示例推导 safe 源级指针语法。
2. 回到真实语言场景、语法与 lowering：优先评审 [指针职责与 MVP](docs/rfcs/research/0003-pointer-roles-and-mvp.md)，按 §6.1 区分运行时最小能力与 D08 Arc 已确认方向。只读投影/接口断言是待闭合的基础类型设计，H 临时契约不是既定要求；U15 桥接不在本轮催定；风险分析按 D20 长期独立，不作为实现门槛。
3. 冻结最小解析 / 包 / 类型身份，再决定仓库代码布局；按 T0 选择宏/enum/match/读写类型的小闭环，trait 双路径先按 [RFC 0004](docs/rfcs/research/0004-trait-lowering-paths.md) 研究。当前未授权的未来功能不因列在本文件就自动启动。
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


本轮整体验收在上述同module宏基准上补齐host构建条件（显式tags及test隐含race）和同包多文件引用检查；新任务调度示例从安装CLI到独立生成模块验证宏/JSON/enum/OptionResult/From/基础权限组合。有限T0可运行子集与整项已确认方向分别记录于 [当前交付标准及验收](docs/t0-acceptance.md#有限-t0-的交付标准)。后续已补齐print/println与Default，开放raw路线现已撤回；完整trait、LSP和长期风险分析不是本轮交付前置条件。公开新引用签名、跨目标编译和.hgo测试源等限制保持明确，不以降低成Go代码代替权限验证。


具体类型Default与固定格式print/println已接通，沿已定unit默认、参数求值及写入错误契约，未扩完整trait/fmt生态。成功/拒绝范围见[验收矩阵](docs/t0-acceptance.md#print--println-与-default-增量)。


历史 raw 常量入口及 raw Clone 曾在 cd49537 验收；D27 已撤回该模型及旧别名问题，现移除代码/成功夹具，不再作为可交付范围。历史证据保留在实现记录。


wildcard-only/全值绑定-only match继续支持封闭enum，依据真实类型身份并先验证合法case。当前交付/待决/后置见[最终分类](docs/t0-acceptance.md#最后覆盖梳理交付待决与后置)。

远端文档同步：RFC 0019 已确认统一 trait wrapper，RFC 0020 已确认独立 impl 内关联函数/Self/self 与包级函数降低；这些新设计不表示本分支已实现通用 trait/固有 impl。D19/D20 的 warning-first 与风险分析非 MVP 边界保持，不采用远端旧文本中的默认逃逸拒绝。

最新交付：封闭 unit enum serde rename/value 映射、同包具体 struct 手动 Default、serde 字段 default/skip 调用已登记 Default，以及具体内建 Option serde 已端到端验收。enum 仅选中默认变体的 payload 要求 Default，非默认变体不要求；此项已确定，不再待答。具体子集与未完成范围见验收矩阵。

格式 JSON Value/任意精度 Number、JSON 专用 native adapter、PATCH 字段 adapter 仍是规范方向或候选入口，尚未实现，不能从 RFC 示例推导为可用 API。普通 Go encoding/json 可显式调用，手写 typed helper/visitor 可直接使用现有 serde 核心；派生不自动采用 MarshalJSON/UnmarshalJSON，也没有通用导入类型字段 codec 选择入口。Option 缺失/null 均为 None，不提供 PATCH 三态；enum 的 Option payload 与部分容器投影限制仍保持。当前错误链修复仅保留已有底层 Go cause，不增加这些能力。

字段显式适配器仅有 [Proposed 研究增补](docs/rfcs/research/serde-field-adapters-proposed.md)：比较函数对/with 入口及 typed helper/native JSON 桥，尚未批准或实施，不计作 T0 已交付能力。


2026-10-02 收口从 `fb74ba2380389b39afe92f35ebdb8500fab74adf` 继续：补齐带 initializer 的 if 问号传播，保持词法作用域和原函数早退。当前四类剩余项见 [T0 收口核对](docs/t0-acceptance.md#2026-10-02-t0-收口核对)；先前“后置”不能将所有原型缺口自动改为已获用户批准延期，整项 MVP 尚未完成。


T0 继续收口：在 if initializer 之后补齐标识符多 RHS、解引用/断言和 channel 发送中的问号；同步赋值、上下文位移类型、闭包和失败不发送均有真实前端验收。索引/切片/字面量/间接目标及循环/switch/select 的具体剩余例子见 [T0 第二组清单](docs/t0-acceptance.md#t0-收口第二组多-rhs解引用断言与发送)。不将完成一个切片记作整项完成。


T0 第三组继续接通索引/切片与复合字面量问号，保留 Go 规定的部分求值顺序、数组存储/地址性、常量检查、外部私有上下文类型及 len/cap 求值。间接目标、循环/switch/select 和不求值上下文仍有明确缺口；[第三组及剩余列表](docs/t0-acceptance.md#t0-收口第三组索引切片与复合字面量)给出最小场景与根因，不宣称整项 T0 完成。

T0 第四组接通普通间接赋值目标的问号：保存容器/索引，按 Go 两阶段求值和写入，保留数组存储、panic 时机、compound、上下文类型和既有权限。外包私有嵌入指针 promotion 仍是明确工程缺口，不能将间接赋值整体打勾；[第四组矩阵](docs/t0-acceptance.md#t0-收口第四组间接赋值的两阶段执行)与 [RFC 0005 §6.5](docs/rfcs/research/0005-option-result-question.md#65-t0-收口间接赋值的两阶段降低)记录证据和范围。循环/switch 未提前推进，select 及 Proposed adapters 继续待决，T0/MVP 尚未完成。

第四组私有嵌入指针功能路径已接通，但性能验收未通过、缺口未收口：保留完整 Go assignment 与原函数执行帧，通过仅内部使用的失败 guard 早退，保持可见性、私有 scalar 上下文和原权限，无反射/unsafe/生产包桥接。[补齐验收及下一切片](docs/t0-acceptance.md#t0-第四组补齐外包私有嵌入指针目标)区分 Go 偏序与两阶段写入，撤回“根本无法降低”的过强推断。先完成无 panic 正常控制流方案研究和实现验证，不把当前 guard 扩展到循环；select 与 adapters 仍待决，整体 MVP 未完成。


无 panic 私有赋值工程先完成 questionFlow 基础提取：显式失败出口替代 AST return 扫描，尚未替换任何真正私有目标 guard。下一步原始类型记录、局部赋值求值规划及完整赋值 emitter 的模块/风险见 [RFC §6.8](docs/rfcs/research/0005-option-result-question.md#68-显式传播控制流基础重构)；不把该重构算作性能收口。


首批私有赋值无 panic lowering 已实现：原 private.hgo 验收集及核心研究基准使用普通失败 return，保留完整最终赋值和 Go 上下文类型。仍有明确 guard 范围（含 unsafe 转换链的真实 checkptr 反例），[RFC §6.9](docs/rfcs/research/0005-option-result-question.md#69-首批无-panic-完整赋值-lowering)与最新验收矩阵逐项列出，不宣称全部私有目标或 MVP 完成。


无 guard 第二组接通静态函数返回类型提供的 private bool witness、signed contextual shift 原生检查及 planner 范围内的 len/cap；专门 fallback 用例由五条降至 tuple/unsafe 两条。其余组合和普通 expr 已知上下文缺口见 [RFC §6.10](docs/rfcs/research/0005-option-result-question.md#610-私有-bool上下文-shift-与-lencap-第二组)。for/range、switch 仍是待实现工程项，不等待 select/adapters 设计，也不默认为批准延期。

无 guard 第三组接通单 RHS 静态 tuple 调用与 map/assert/receive comma-ok 赋值，保留一次求值、私有返回类型、未类型化 ok 与完整两阶段写入。专门 fallback 文件仅剩 unsafe 链，但其余 guard 分组仍明确待办，见 [RFC §6.11](docs/rfcs/research/0005-option-result-question.md#611-单-rhs-tuple-与-comma-ok-的第三组替换)。本组独立验收，不等待 unsafe 长尾；循环/range、switch 和整体 T0 验收尚未完成。

普通循环首批已接通 for init/条件/post 和 slice/map range 源的问号传播，使用原函数普通 return，保留 Go 每轮变量身份、continue/post、标签重入及 defer。其他 range 源的问号、迭代赋值目标和需要旧 guard 的循环内组合仍待实现；原生无问号源不强制求值。[RFC §6.12](docs/rfcs/research/0005-option-result-question.md#612-for-与-slicemap-range-的首批传播)明确本阶段范围，不把 for/range 整项或整体 MVP 打勾。unsafe/其他 guard 长尾继续留表，switch 为后续独立工程。

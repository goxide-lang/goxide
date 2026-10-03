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

表达式 switch 首批接通 init/tag/case 与已有 body 传播：按 Go 顺序选中后执行原生分支，保留 default/fallthrough、标签、作用域和原类型诊断。type switch 传播、其他 range/guard 长尾仍未完成。[RFC §6.13](docs/rfcs/research/0005-option-result-question.md#613-表达式-switch-的传播与循环开销复核)同时记录 for 性能复核：零分配但慢于显式 match，去 first 探针只解释部分差距且未作不安全生产替换。整体 MVP 不标完成。

Type switch 的 init/subject 与既有 body 传播、string/整数/channel/数组/数组指针 range 源已增量接通，保留 Go case 变量类型、整数上下文、channel 行为与数组免求值边界；函数 iterator 源及 range 目标传播仍未实现。[最新 T0 优先表](docs/t0-acceptance.md#当前剩余-t0-优先级)明确下一步先常用 match，再公开权限元数据/普通权限跟踪修复，问号和性能长尾不继续阻塞这些工作。整体 MVP 不标完成。

### 2026-10-02：常用 match 组合增量

完整 arm 内嵌套 match、有限同文件已知参数调用已接通，真实配置/Result 早退和跨包非法 enum 由生成模块 race 验证；[精确范围与证据](IMPLEMENTATION.md#常用-match-表达式组合2026-10-02)。普通赋值/推断/任意运算符与字面量/其他调用仍是未完成项。此阶段后先做公开引用元数据，不把完整模式系统或长期别名分析带入本轮收口。

### 2026-10-02：公开标量引用元数据首片

同次目录 module 构建的公开标量引用参数/单返回已接通，含无新引用语法的 Go 消费者校验、多文件与返回转发。内部 v1 metadata 和 marker 防止缺失时自动降级；single-file 两个方向也检查 marker，尚不支持跨构建恢复。[范围与证据](IMPLEMENTATION.md#同模块公开标量引用元数据2026-10-02)。复合元素、外部依赖恢复、权限跟踪保守限制及其他 T0 组合仍未完成，不以此宣布 MVP 完成。

### 2026-10-02：公开复合引用元数据增量

同次 module 构建公开引用参数/单返回扩展到具体 struct、slice/map/array、具名类型和递归指针元素，保留私有/再导出/多文件真实类型身份与既有只读投影。[验收矩阵](docs/t0-acceptance.md#公开复合引用元数据增量2026-10-02)包含真实 CLI 生成模块的独立 race/vet。v2 元数据只供 hgo 构建校验，原生 go build 不执行权限规则。剩余签名/元素、外部恢复、普通权限误拒绝及其他 T0 组合继续留表，整体 MVP 尚未完成。

### 2026-10-02：普通权限跟踪误拒绝首组

for/range 局部重绑定、退出分支、元素地址后的局部重绑定及每轮零值声明已按控制流事实修复，普通独立副本可继续调用原生 Go 函数；旧 readonly alias 与接口擦除仍拒绝。字段替换/捕获/真正取址槽位及未建模控制流仍为工程欠账，不升级为长期 alias 分析。[最小收口清单](docs/t0-acceptance.md#当前最小收口清单与停止条件2026-10-02)将必须完成、联合验收、长期项目和用户待决明确分开；下一先修字段强更新，再回到常用 match/联合验收，不继续无限扩引用签名。

### 2026-10-02：局部值字段替换

非捕获/未取址 local struct 的直接/嵌套/value promotion 字段替换已接通，保留旧 alias、兄弟字段、回边及多赋值写序；独立审查补齐 pointer receiver 隐式取址。跨包真实生成模块 race/vet 验证，不扩长期 alias 分析。捕获/实际取址根和 pointer/index 等精度缺口仍列工程账；[本阶段范围与下一停止条件](docs/t0-acceptance.md#局部值字段替换验收2026-10-02)固定下一普通赋值/局部推断 match，再做多包联合验收，不以不断扩签名/模式范围替代收口。整体 MVP 仍未完成。

### 2026-10-02：match 赋值与局部推断

单标识符赋值/局部 `:=` 与 var 推断已按上下文类型和同型分支规则接通，不新增公共超类型或隐式转换；生成多包 race/vet 覆盖问号、引用、scope和副作用。复杂目标/其他表达式及不可命名结果仍留账。按既定收口顺序，下一先[多包联合验收](docs/t0-acceptance.md#局部值字段替换验收2026-10-02)，不继续无限扩模式/签名，也不在范围账或待决接口未经选择时宣称完整 MVP。

### 同模块联合验收（2026-10-02）

[四包业务](examples/t0_joint/main.hgo)与[真实安装 CLI 测试](tests/acceptance/t0_joint_test.go)已贯通宏、enum/match、Option/Result/错误链、Default、serde JSON 和读写权限。修复导入引用 metadata 绑定晚于 match 推断的误拒，生成模块独立 race/vet、仓库外运行、失败/诊断与确定性通过。联合项已完成；[剩余清单](docs/t0-acceptance.md#当前最小收口清单与停止条件2026-10-02)仍保留普通权限精度、问号欠账与范围决定，不把完整 MVP 标完成，也不将 Proposed 接口默认为获批。

### 当前值与暴露槽位（2026-10-02）

捕获/取址局部的直接整槽替换已修复，旧地址与闭包、未知调用和channel同步保持资格；实际生成模块 race/vet 正反例见[验收](docs/t0-acceptance.md#暴露局部整槽替换2026-10-02)。接下来先修普通问号私有 bool 类型丢失，然后推进用户明确要求的原位 `match mut&`；借用模式首版必需，不再作为可建议后置项。其他范围选择和新桥接尚待用户决定。

### 普通问号私有 bool（2026-10-02）

原普通 expr 的私有 bool 失型已用已有静态 witness 修复，真实多包 CLI/生成 race 验收保持类型身份、短路与最近用户函数传播。见[具体范围](docs/t0-acceptance.md#普通问号私有-bool-误拒2026-10-02)。原位可写借用 match、跨 module 增强恢复及自然 mut&→Go 指针互操作按 D29–D31 为当前方向；IIFE 仅是用户授权评估的表达式 lowering 候选，不能改变问号退出目标，也不能因机制基准通过就宣布前端支持。

### 具体标量自然 Go 指针调用（2026-10-02）

D31 首片已支持同型 `mut& T` 直接传原生 `*T` 标量参数，包括函数值、变参、defer/go；原指针保留/返回与 Go GC 存活按原生规则执行，只读路径不升级。见[实际验收](docs/t0-acceptance.md#具体标量原生写指针调用2026-10-02)。未完成的其他边界仍列工程账；enum ABI 迁移待用户选择期间未修改表示，原位借用 match、跨 module metadata 与复杂表达式仍需继续。

### 二元表达式与简单索引 match（2026-10-02）

`return 10 + match ...` 与 `a[index()] = match ...` 已通过[真实生成模块验收](docs/t0-acceptance.md#二元-match-与简单索引赋值2026-10-02)：在原函数展开，保持短路、问号/recover、求值/写入时点、类型身份、readonly 与 goto。优化构建对照和独立审查随本阶段提交。按用户要求完成此有界切片后交回；enum ABI 不迁移，跨 module 元数据待下一步，整体 MVP 尚未完成。

### 跨 module 引用 metadata 首片（2026-10-02）

独立模块 local replace 已恢复当前具体引用函数签名，支持 alias/reexport、只读拒绝和可写调用，普通 Go 消费不依赖 metadata。内部产物绑定真实包、选中源、传递依赖源内容和增强依赖 artifact；依赖结构漂移触发重新生成，避免旧权限分析失效。实现、独立审查与验收边界见 [T0 清单](docs/t0-acceptance.md#跨-module-引用产物2026-10-02)。未迁移 enum ABI；其他增强 metadata、未实现引用签名/投影、外部 cgo 输入及原位 match 留账，完整 MVP 尚未完成。

### 独立 enum 阶段（2026-10-03）

最新顺序为宏 → enum → 指针拆分。保留当前值型 case 与已有引用能力，先收口 enum 独立缺口，不以前置 Copy/move/原位借用扩大阶段。首片修复 enum 同线显式分号与旧假负例，见[阶段验收](docs/t0-acceptance.md#独立-enum-稳定阶段声明分隔符2026-10-03)。下一步为跨 module enum metadata；比较/零值/入口校验具体规则未在本片决定。

### 跨 module 具体 enum 首片（2026-10-03）

公开具体 enum 的两个独立模块恢复已接通，含构造/match、透明 alias 再导出、真实类型核验、内容/构建绑定与生产者 serde helper 调用，见[验收](docs/t0-acceptance.md#跨-module-公开具体-enum2026-10-03)。不迁值型 ABI，不引入 Copy/move 或新借用模型。internal facade 保留普通 API；公开 relay helper、跨包 derive、外部 builtin 家族等继续留账，比较与初始化/验证策略须准确收敛。

### 跨 module builtin 恢复（2026-10-03）

Option/Result 通过实际 registry 和生产者具体 validator 恢复，独立 go.mod 构造/match/传播、alias 与原生消费进入[真实验收](docs/t0-acceptance.md#跨-module-optionresult2026-10-03)。消费者跨包 derive 可作为后续独立切片，不扩全 trait 或隐式 From 注册。用户已选 enum 比较资格编译期判断，并确认显式 PartialEq/Eq derive 与 equals 方向；[有界比较提案](docs/rfcs/research/enum-equality-proposed.md)区分已确认方向、建议入口和待定边界，本片未实现比较协议。

### 显式 equality 首片（2026-10-03）

已接通有限具体类型的显式 PartialEq/Eq 派生、实例 equals、v3 跨模块登记和真实 helper 消费，见[验收](docs/t0-acceptance.md#显式-equality-首片2026-10-03)。直接 enum 比较及已知危险 enum 的间接比较/map键入口编译期诊断；原生 Go 与 enum-nil 现状保留。容器语义、manual、pointer/chan/interface、HashMap 未混入本片，剩余账不自动批准延期。

### 容器 equality 派生（2026-10-03）

依 D33/D34 推进 slice/map 递归比较及 Option/Result 条件载荷协议，保留 nil/空区别、既有短路和 enum 校验顺序，原生 map 限定能证明一致的稳定键。见 [RFC](docs/rfcs/0008-enums.md#容器-partialeqeq-派生2026-10-03)。手写入口、循环容器规则和其他 T0 欠账继续独立记录；指针拆分/原位 borrow 是后续阶段，不前置阻断本轮 enum 收口。

### 手写具体 equality（2026-10-03）

按 D35 复用比较 registry 接入具体值参数 PartialEq 和空 Eq impl，支持跨文件/跨模块与派生组合、来源元数据和唯一 enum 校验入口。见 [RFC](docs/rfcs/0008-enums.md#手写具体-partialeqeq2026-10-03)。不把业务相等自动映射到原生 map key，不新增借用签名、完整 trait 或循环图比较；完整 T0 仍按验收范围逐项收口。

### 宏 builtin 首片（2026-10-03）

按宏 → enum → 指针顺序补模板直接 len/cap，保原生求值及定义端绑定。调用包级同名声明与局部 named type 边界仍留账；其他 builtin 未开放。见[阶段验收](docs/t0-acceptance.md#宏-builtin-首片lencap2026-10-03)。下一独立片为已登记跨包 Default 字段协议，不改零值/公开入口契约。

### 跨包 Default 字段恢复（2026-10-03）

已接通具体类型显式 Default 的跨包字段消费，含 derive/manual 来源、透明 alias、真实 helper 签名及 artifact 身份；见[验收](docs/t0-acceptance.md#跨包已登记-default-字段2026-10-03)。其他字段派生、单文件外部元数据、不可命名载荷仍分别留账，零值与公开入口契约待明确，不宣称完整 MVP 已完成。

### 跨包 Clone/Debug 字段恢复（2026-10-03）

具体已派生 enum/struct 的 qualified 字段协议、公开透明 relay 与有限容器元素递归已接通，见[验收](docs/t0-acceptance.md#跨包已登记-clonedebug-字段2026-10-03)。消费者本地 alias 转发和手写入口准确留界；跨包 serde 单独下一片。零值/公开入口仍待用户定约，完整 MVP 尚未宣布完成。

### 跨包 serde 协议恢复（2026-10-03）

已接通生产者显式派生 Serialize/Deserialize 的 qualified 字段与既有容器/Option codec，协议方向独立、公开 relay 可转发。沿用 metadata v6（无新 schema 字段），旧产物缺协议记录必须重生成；真实三模块验收与协议身份/签名/来源/陈旧产物反例见 `tests/acceptance/serde_external_test.go`、`internal/frontend/serde_protocol_artifact_test.go`。消费者本地 alias、手写 serde 登记等仍是工程账；adapter Proposed 与公开入口零值契约仍待决定，不视为获批延期或完整 T0 已完成。

### 独立 enum 当前最小剩余清单（2026-10-03，alias 阶段后）

- 协议组合工程账：同包原始类型的本地 alias 转发（Default/Clone/Debug/serde；Eq 已支持）；Option/Result 未实现的 Clone/Debug 等组合及 blank/embedded 字段仍须逐项收口或明确首版范围。
- 外部恢复工程账：单文件外部 enum/协议恢复、既有限制外部 payload、不可命名载荷/internal-origin 的增强操作；公开 relay 不能代替所有这些能力。无源/远程发布不由当前 local replace 验收推导为完成。
- 常用 match 仍有复杂目标/表达式上下文长尾，按已定求值与最近函数传播规则验收，不扩大成完整 Rust 模式系统。
- 待决定：公开函数直接 enum 参数是否在函数体前验证并对非法值 panic，None 的合法性按 builtin Option；本轮未实施该待答复契约。

以上仅为独立 enum 最小账，不重新加入指针阶段；手写 serde/adapters/完整 trait 也不借此变成已批准设计。工程限制不能自行归为批准延期，完整 MVP 尚未宣布完成。

### 独立 enum 精确剩余（2026-10-03，同包 alias 后）

本地/外部具体已登记类型的包级 alias 转发已关闭，不能再列作待做。其余最小账为：

1. **已复现 match 限制**：`func F(x E, g func(int) int) int { return g(match x { E::A(n) => n, E::B => 0, }) }` 仍拒绝 embedded expression match；`func F(x E) (out int) { return match x { E::A(n) => n, E::B => 0, } }` 仍要求一个 unnamed result。直接已知函数参数调用、binary/短路及简单 index 目标已有验收，不重复列为未实现。
2. **协议组合**：`#[derive(Clone,Debug)] type S struct { V Option[int] }` 尚无对应具体派生支持；Result 的未实现组合同样须按实际资格验收。已有静态 Default 递归限制仍在，例如 DefaultItem→DefaultWrapper→DefaultItem 即便有终止条件也拒绝，不算本轮新增能力。
3. **外部恢复边界**：单文件外部 enum/协议、受限的外部 payload/不可命名载荷及 internal-origin 增强操作尚未全部接通，当前模块 local replace 正例不能代替这些验收。
4. **字段规则需明确到例子**：`type S struct { _ int; X int }` 的 blank 字段是否/如何参加各派生，`type S struct { Inner }` 的 Debug 字段名与 serde 嵌套/展开规则未在当前 RFC 明确；继续明确拒绝，不擅自跳过或沿用 encoding/json 自动展开。
5. **公开入口待答复**：直接 enum 参数在公开函数体前验证、非法值 panic 的契约未实施，Option None 合法性也不由本轮改动。

这些工程/设计账不自动成为批准延期；不混入指针阶段、full trait/manual serde 或 Proposed adapters，也不宣称完整 MVP 已完成。

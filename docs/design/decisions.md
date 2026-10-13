# 设计决策与讨论覆盖登记

更新：2026-10-01。项目暂称 goxide / Goxide；远端仓库名 `goxide` 不代表已经决定改名。

本文区分讨论中确认的方向、为使设计可评审而写出的候选规则，以及仍没有方案的部分。**“已确认”不表示“已实现”**；实现分支已有有限可运行原型，准确范围见[实现说明](../../IMPLEMENTATION.md)；完整 goxide 编译器、linter、LSP 与可发布基础包尚未完成。实现进度以 [roadmap](../../ROADMAP.md) 为准。

## 1. 状态含义与维护规则

- **已确认**：用户明确提出或接受的约束，后续设计应遵守。确认功能方向不等于确认其全部语法、算法或 ABI。
- **暂定**：已有可讨论的方案，包括代理提出的具体规则和 Go 降低示例。
- **未定**：尚未选定方案、缺少必要规则，或明确延期。

状态变化时更新本登记和对应专题。被替代的方案直接删除或标为历史，不留在正文里当规范读。新增语法用 RFC 说明源级语义、Go 降低、双向边界与适配成本。

当前 trait 方向见 [RFC 0019](../rfcs/0019-uniform-trait-wrappers.md)：统一 wrapper、保留 marker、固定包级工厂及本地 As / 外部 ForT.As 便利 helper。用户已确认该方向，替代原始类型 marker 注入；具体 ABI 细节仍未冻结。类型关联函数由 impl 块定义，T::method → Go 包函数、Self/self 绑定及原生 Go 声明边界见 [RFC 0020](../rfcs/0020-associated-functions.md)。

## 2. 已确认的方向

同步编号说明：远端 work 独立使用了 D19–D26，与本实现线程的 D19–D23 重号。本分支将远端新增决定记作 R19–R26，保留内容及 RFC 链接；本地 D19 warning-first、D20 风险分析非 MVP 等含义不变。

| ID | 已确认范围 | 文档与边界 |
| --- | --- | --- |
| D01 | 以转译器 + 转译前 linter 为架构，生成普通 Go，保留 Go 生态与运行时 | [总纲](overview.md)；不以独立所有权运行时或原生机器码后端替代 |
| D02 | 所有新语法同时设计 Go 降低与双向互操作 | [总纲](overview.md)；不能只展示 goxide 写法 |
| D03 | 改善安全性、显式实现与可读性、Go 包互操作是三项目标 | [符合程度](overview.md#21-三项目标的符合程度)；尚无完整安全性证明 |
| D04 | 使用 trait 表达显式实现，保留 Go interface 的结构式语义；纳入普通关联类型方向 | [trait](traits.md)、[关联类型](associated-types.md)；泛型和投影细节暂定 |
| D05 | `declare impl AorB::XXX for BBB::Type;` 可写在用户 C 包普通 .gox 内；无体采用原业务方法，通过 C 包 wrapper 显式转发并实现 marker | [Go 适配](go-interop.md)；原指针保持，接口动态类型是 wrapper，不向原始类型追加方法 |
| D06 | 有体 impl 的实现函数直接定义在对应 wrapper；多个 impl 的同名方法允许不同签名或行为；保持稳定 As 入口 | [trait](traits.md)；不再在原类型生成改名业务方法 |
| D07 | 根据使用处期望类型决定是否插入 trait 适配；保留值本身的类型 | [使用处转换](traits.md#6-期望类型驱动的转换)；泛型和容器细节暂定 |
| D08 | 拆分 Go 指针用途：Option 表达缺失、`mut& T` 表达写引用、`Arc[T]` 对应 Go 共享指针；不引入全局 move | [引用专题](pointers-and-borrows.md)；D19 更新默认诊断：别名/独占风险 warning，不承诺静态独占，Arc 使用 Go GC |
| D09 | Lock / RWLock 保存数据并采用指针句柄，避免复制 mutex；回调访问及返回结果 | [锁](synchronization.md)；`Do[P any]` 使用 Go 1.27，最低语言版本和工具链为 1.27 |
| D10 | 普通 declare 补充 / 覆盖原生 Go 的 goxide 类型视图，不写 unsafe declare；裸 API 与已声明 / 包装 API 分层 | [原生 API](foreign-api.md)；.d.gox 与类似 @types 的外置声明包均纳入方向 |
| D11 | 历史决定：使用 `unsafe <expr>`；拆分指针经 unsafe as_ptr / `Ptr[T]`、AsPtr。**普通可写引用到匹配 Go 指针调用已由 D31 纠正，不再要求冗余 AsPtr**；真正 unsafe 操作另保留 | [Ptr / AsPtr](foreign-api.md)；Go 候选表示 `type Ptr[T any] *T`，源级精确拼写未冻结 |
| D12 | 变体枚举、Option / Result、模式匹配；接受 Go 调用方能构造非法值，不能假称阻止了 Go 零值 | [枚举与 match](enums-and-match.md)；边界策略和布局见暂定项 |
| D13 | pub 标记公开边界的方向；需要处理 Go 调用生成 API 的情况 | [总纲](overview.md#34-go-边界与非法值)；完整可见性与检查入口未定 |
| D14 | 卫生宏参考 Rust；先做模式 / 模板声明宏及内建派生，提供 print / println、Default / Clone / Debug；用户过程宏后续考虑 | [宏](macros.md)；纯的用户 Syntax→Syntax 编译期程序仍是过程宏 |
| D15 | goxide 基础包提供现有 Go std 的 safe 视图与泛型 slice | [基础包](standard-library.md)；以 Go API 真实行为为依据，不以 Rust std 为规范 |
| D16 | quote / syn 风格能力放在额外包 | [语法工具](syntax-tooling.md)；是否列入首个可用版本及协议仍暂定 |
| D17 | 提供面向 goxide 的 LSP，未来考虑 goxide 自举 | [编译器与工具链](compiler-and-tooling.md)；启动语言、复用组件和阶段路线为暂定方案 |
| D18 | 先完成设计；总纲和各专题分开，维护决策状态与路线，支持另一台机器继续 | [README](../../README.md)、[roadmap](../../ROADMAP.md)；本次修改不代表开始编译器实现 |
| R19 | 发布完整 dist；同一 Go 模块携带 .go、.d.gox 与可选 .gox；Go 编译 .go，goxide 额外加载声明或源码 | [RFC 0001](../rfcs/0001-package-loading.md)；wrapper 与 marker 随 impl 包发布，无需外部方法注入 |
| R20 | 相容 declare 可合并，有体实现保持唯一 | [RFC 0003](../rfcs/0003-declare-impl.md)；契约矛盾和原生采用/有体混合语义仍需明确 |
| R21 | match 按模式区分值复制、只读借用与可变借用 | [RFC 0009](../rfcs/0009-match.md)；具体语法、作用域与支持原位借用的 ABI 未定 |
| R22 | 所有新特性和语法有独立 spec/RFC，以转译前后、双向调用和反例表达约束 | [RFC 清单](../rfcs/README.md)、[模板](../rfcs/TEMPLATE.md)；已写文档不代表已实现 |
| R23 | goxide 源级使用变体枚举分派，不直接采用 Go 的 .(T)/.(type) 作为分派语法；增强保证由 goxide 工具链提供 | 后端和原生 Go 仍可使用 Go 类型检查/反射；普通 Go 调用者不获得 goxide 静态权限保证 |
| R24 | 所有运行时 trait 视图统一 wrapper；trait 接口与 wrapper 保留 marker；固定包级函数适配；Go 使用本包 As 方法或外部 ForT.As helper | [RFC 0019](../rfcs/0019-uniform-trait-wrappers.md)；具体符号编码、权限、nil/比较与多包载体选择未定 |
| R25 | 构造函数等类型关联函数定义在独立 impl T 块中，用 T::method 调用，降低为普通 Go 包函数 | [RFC 0020](../rfcs/0020-associated-functions.md)；不将函数写入 struct 声明；func 首参数 self 区分实例成员，其余为关联函数，无 static；接收类型/权限、Go 名称与初始化细则仍待收敛 |
| R26 | impl 成员按 goxide 规则转译并支持 Self/self；直接书写的 Go 函数和方法保留原生 Go 语义 | [RFC 0020](../rfcs/0020-associated-functions.md)；Self 绑定源目标类型，self 仅绑定实例接收者；不全局替换同名 Go 标识符，不限制其他 goxide 特性只能位于 impl 内 |

已确认新增 **D19（2026-10-01）**：用户明确要求默认兼容 Go 式指针/别名；有具体风险依据先 warning，不以无法证明安全大面积拒绝。语法、类型、已确认不变量错误不降级；warning 与运行时 race 分开，不能宣称静态安全。该反馈否决 T01/T06 及前两轮严格默认，不撤销无关 D01–D18。

已确认新增 **D20（2026-10-01）**：别名/借用风险静态分析独立长期推进，完全移出 MVP，包括 W01–W04。MVP 保留正常语法/类型等基础检查；其他设计考虑可分析性，但不强制复杂 effect 注解、来源分析或保守限制。D19 仍约束未来风险诊断的默认级别。

已确认新增 **D21（2026-10-01）**：用户将宏、enum/match、指针能力中的只读/可写分离列为 T0。T0 是优先级，不表示全套特性一次实现。enum 的早期 raw 常量路线已被 D27 撤回；当前只有封闭变体，数值/string 通过 serde 映射，非法输入/匹配行为需明确。共享与内部可变考虑 goxide/std 包装，不先引入整套所有权或智能指针系统。用户认为宏收益明确；仍保留客观卫生、诊断和工具链验收，不把“100% 正向”写成实测结论。D20 不变。

已确认新增 **D22（2026-10-01）**：`?` 首先限于 Option/Result，不公开 Try/Residual。thiserror 核心派生生成显式 From；返回 Result 的目标错误类型已知时允许唯一的一步显式转换，不搜索转换链、不接受任意泛型 blanket 转换。静态 Go `error` 只能走明确兜底或业务分类，不能推断其动态变体。精确语法、impl 选择细则和前端实现仍待闭合，见 RFC 0005。

**实施交接授权**：先冻结本地文档基线 `v0.0.0-mvp-doc`，再在独立实现会话推进模式宏、log/tracing 场景及 Debug/Clone/serde/thiserror 派生。JSON 是推荐首版格式，完整 Rust 兼容并非目标。本线程继续研究/spec/RFC，实现发现回流；授权实施不表示其他候选已经确认。

已确认新增 **D23（文档标签之后，按最新反馈更新）**：用户要求 serde 优先，随后明确“仿照 serde-rs 的实现”。核心采用类型/格式分离的 Serialize/Deserialize、Serializer/Deserializer 与 Visitor/Access；Value 属于 serde-json 等格式层，首版 JSON、未来 YAML 独立适配。此前助手提出的公开 core Value、CoreValue-v1 与首版必须全树中转被这一决定覆盖，不再等待用户在旧候选中选择。Go/goxide ABI/子集/格式策略仍见 U18；本研究线程不实施宏或 serde 代码。

**新增研究授权，不是后端选择**：对照“二进制模式无冲突直接适配、有冲突最小 wrapper；Go 源码模式固定 wrapper”。与 D05/D06 现行 marker/固定有体 wrapper 的差异见 RFC 0004；不得因研究授权就修改已有 impl 的可观察语义。

## 3. 已有方案但尚未冻结

| ID | 暂定方案 | 需要确认或验证的部分 |
| --- | --- | --- |
| T01 | **默认策略已否决**：所有未声明 Go callable 统一 unsafe | D19：缺摘要不要求 unsafe；D11 明确操作及真实类型转换边界保留，U15 待协调 |
| T02 | 非空与构造、读写路径、持有类型定位均暂定；Box 不预设独占 | [RFC 0003](../rfcs/research/0003-pointer-roles-and-mvp.md)比较引用优先与保留持有层；Arc 的已确认 D08 不能静默撤销，新 as_ref/as_mut 与 Cell 未批准 |
| T03 | 非法 Go 输入在保持签名入口 panic，可另设显式返回错误的入口 | Result 的 nil 不能当合法变体或悄悄改签名；检查时机、pub 范围、回调和共享值重新验证仍需设计 |
| T04 | enum 降低为 interface + 值型 case；Option.None 使用 nil，Result.nil 非法 | ABI、完整递归检查、泛型身份、nil / typed nil / 伪造 case 等规则属于[当前候选](enums-and-match.md) |
| T05 | match 表达式一次求值，guard 不计穷尽覆盖，先提供值绑定 | 控制流保留、求值顺序、不可达分支和后续借用模式的完整规范 |
| T06 | **默认阻断已否决**；来源追踪改为有依据的风险 warning | D19 与 RFC B01–B08：未知不拒绝也不刷警告；不宣称静态安全 |
| T07（历史） | 原定义包隔离构建视图注入 marker / As | 已被 RFC 0019 的统一 wrapper 取代；旧夹具保留为历史机制，不作为当前实现要求 |
| T08 | impl 按接收者、trait、参数规范化；冲突报错、显式导入元数据 | 跨包可见性、接收权限身份、版本和构建条件如何参与选择 |
| T09 | 固有方法优先、唯一可见 trait 候选；稳定符号编码，限定调用 | 最终源码拼写、名称冲突策略和 ABI 编码未定；新增 impl 可能增加源码歧义 |
| T10 | 宏模板自由名字按定义点绑定、捕获语法保留调用点来源 | 此处有意不同于 Rust mixed-site hygiene；宏匹配、阶段、缓存、预算及授权规则是候选设计 |
| T11 | 字面量 `{}` / `{:?}` 对应 Display / Debug；打印失败 panic；固定字段式 derive 规则 | 格式子集、formatter、Default / Clone / Debug 精确协议及错误策略未冻结 |
| T12 | Default / Clone 的静态 Self 操作选择具体 impl 并降低为普通 Go 函数 | 成员类别规则已确认；接收类型/权限与泛型实现证据未定；不能宣称所有 trait 均可作为 Go 运行时接口 |
| T13 | 用户建议 std 提供 Vec/借用视图和多种 Map；既有 Slice、ReadOutcome、copy/clone_with 为候选 | [Vec](../rfcs/0010-collections.md)、[Map](../rfcs/0017-maps.md)；赋值语义、元素借用、ABI、名称和具体后端未定 |
| T14 | syntax 协议 + 普通 syn / quote 包可先于过程宏宿主提供 | 这是实现建议，不承诺首版交付；token 构造原语、卫生上下文和包版本尚未冻结 |
| T15 | Go 启动前端 / LSP，独立 goxide AST 和语义 IR；复用 Go 包加载、输出和类型信息 | 自举阶段、复用边界、仓库布局和工具进程协议见[工具链草案](compiler-and-tooling.md) |
| T16 | Unit 用 struct{}；第三方锁经普通 Lock/Unlock 原语接入 | 回调语法、没有结果的 return 插入、第三方适配构造器及来源检查 |
| T17 | R24 已确认的 wrapper/marker/helper 方向下的具体工厂、便利名称和载体协议 | [RFC 0019](../rfcs/0019-uniform-trait-wrappers.md)；编码、冲突、共享适配包和权限细则仍需定稿 |
| T18 | 实例接收类型/权限拼写、new → NewT、其他关联函数 → TMethod、首版不重载 | [RFC 0020](../rfcs/0020-associated-functions.md)；impl 块及 Self/self 支持、T::method 与 Go 包函数映射已确认，func 首参数 self 的分类规则已确认；具体接收类型/权限及导出规则未冻结 |

## 4. 未定与明确延期

这些项有可能讨论过候选方案，但尚不能据此实现并宣称规范完成。处理次序见 [roadmap](../../ROADMAP.md)。

| ID | 未闭合问题 | 影响 |
| --- | --- | --- |
| U01 | 完整词法 / 语法、类型表示、包和符号身份；最终语言名称 | 解析器、诊断与稳定输出 |
| U02 | 引用获取、保存/构造、字段及容器只读投影、输出槽位；持有类型必要性 | RFC 0003 §6.1–6.2 核对 D08/D11；内建容器写操作、浅复制元素引用及接口/any 断言须保留权限，类型表示尚未闭合。H 临时契约未批准且不是 MVP 前提；风险来源/别名分析另属 D20 长期工作 |
| U03 | 源级 `&T` / `mut& T` / Arc 如何参与 impl 键与方法接收权限 | 早期 `*T` 示例仅为 Go 机制示意，不能成为第四种 safe 源级指针 |
| U04 | 静态 trait 成员、裸 Self、泛型约束、关联类型投影和后端策略 | Default / Clone、迭代器；字典传递与专门化尚未选择 |
| U05 | 递归 Go 类型视图、嵌套 Ptr / slice / map、函数值和回调桥接 | 不能把单层 *T 转换实验外推成全 ABI 兼容 |
| U06 | pub、公开字段、回调及共享对象的验证时机和错误接口 | Go 双向边界与输入有效性 |
| U07 | 声明包命名、版本 / 构建条件锁定、raw 视图选择、依赖缓存与产物发布 | 另一台机器可复现相同语义与增强依赖 |
| U08 | 支持值复制/原位借用的 enum 最终 ABI、循环对象图校验与容器初始化算法 | 借用必须指向原 payload；严格值不变量、性能和互操作成本 |
| U09 | 宏匹配器完整语法、formatter 协议、静态派生协议、卫生与源码追踪协议 | 首版宏可用性；用户过程宏及可编程 derive 明确延期 |
| U10 | safe Go std 包路径、逐入口契约、Slice ABI / Clone 策略、首批覆盖范围 | 不能宣称整个 Go std 已 safe |
| U11 | 公共语法包协议、受控 token 构造、版本与过程宏宿主能力 | 普通语法工具与编译期扩展分别验收 |
| U12 | 增量查询、完整 LSP、构建驱动、自举一致性验收 | 尚未实现；保持 Go seed 的方案为暂定 |
| U13 | supertrait、GAT、特化、锁块语法 | 延期 / 另行设计；借用 match 已由 D21 纳入目标，具体实现见 U02/U08 |
| U14 | Rust / LLVM 到 Go 汇编的受限计算内核路线 | 不作为首版主后端；ABI / GC / 栈约定等尚未解决 |
| U17 | Option/Result 专用 `?` 的精确语法与实现、Go 多返回显式包装 | [RFC 0005](../rfcs/research/0005-option-result-question.md)；限域及显式 From 单步转换方向已确认，不公开 Try/Residual，不搜索转换链；Go error 静态类型需显式兜底或分类 |
| U18 | serde-rs 分层到 Go/goxide 的接口映射、字段/数字/enum 策略及派生子集 | [RFC 0006](../rfcs/research/0006-serde-core-and-formats.md)；分层已确定，具体 helper/visitor ABI 与严格 JSON 偏差仍候选；无公开 core Value，格式 Value 不强制中转 |
| U19（撤回） | 开放 raw/C enum 与同值别名 | 已由 D27 撤回；不再等待旧别名问题；serde 外部映射不创建开放 enum |
| U16 | trait 双路径、消除包装的语义条件及发布 ABI | [RFC 0004](../rfcs/research/0004-trait-lowering-paths.md)；比较直接调用/值视图/指针视图/字典，D05/D06 未被候选替换 |
| U15 | D31 已确认自然 mut&→Go 指针调用；其余边界逐项实现 | [RFC 0002](../rfcs/research/0002-native-pointer-bridge.md)：具体标量同型参数已接通；递归容器/接口/引用 callback 等未由此完成，不再将普通调用方向列作待批准 |

### 4.1 本轮候选细化

[RFC 0001](../rfcs/research/0001-scoped-borrow-views.md)与[业务场景](../rfcs/research/0001-usage-and-tradeoffs.md)已按 D19 重写：B01–B08 解释默认允许，W01–W06 要求具体风险链、修复建议与误报/漏报成本。前两轮“词法借用/唯一性证明作为默认准入”的推荐已被用户否决；U03 的 impl 权限形式仍未选择。Go 夹具仅验证机制，不是 goxide warning 实现。

## 5. 已被替代或纠正的方案

| 早期说法 | 当前记录 |
| --- | --- |
| 无法证明不逃逸/独占就默认拒绝；无摘要 callable 自动 unsafe | 用户 D19 明确否决；有依据风险 warning，未知不报；D11 明确操作另行保留协调 |
| 用 enum 默认变体使任意 Go 零值都合法 | 放弃普遍保证；Go 可制造非法 Result，显式 Default 也不使零值合法 |
| 只有出现同名 trait 方法时才切换 wrapper | declare 与有体 impl 均固定 wrapper，不因后来新增 impl 改变旧表示 |
| wrapper 转调原类型上的改名业务函数 | 业务函数直接在 wrapper 上；具体调用可优化，但不能改语义 |
| .d.gox 必须描述原生包已经存在的 wrapper | 原生包只需现有业务方法；declare 可在用户 .gox 内。后来恢复 .d.gox 作为可选 API 声明分发格式，不恢复“原生已有 wrapper”前提 |
| declare 无需 marker，或 declare 本身写 unsafe | 运行时 trait 的 marker 保留在接口和 wrapper；普通 declare 可信契约与函数 unsafe 属性分别处理 |
| declare 向原始类型补 marker，As 返回原始动态类型 | 统一 wrapper，保留内部原指针；固定包级工厂与 Go 便利 helper，不改写外部依赖 |
| raw Go *T 默认直接导入为 Option[Arc[T]] | 当前 raw 候选为 Ptr[T]；Option / Arc 等由声明或实际适配建立 |
| 使用 mut T / Ref 作为统一指针模型 | 当前方向以 mut& T、只读借用、Arc 和 Ptr 分工；未冻结部分见引用专题 |
| 连续下划线加一就能可逆编码组合名称 | 该编码有碰撞，不采用；正式符号编码未定 |
| 无 I/O 的 Syntax→Syntax 编译期用户函数是非过程宏 | 分类错误已纠正；仍属于过程宏，首版采用声明模式与内建派生 |
| safe 标准库参照 Rust std | 已纠正为 Go std；Rust 仅参考宏机制与派生协议 |
| 独立的相同 declare 一律冲突 | 相容无体声明可合并，有体唯一；矛盾契约仍诊断 |
| match 固定只有值绑定，借用模式延期 | 按模式区分值/只读借用/可变借用；旧值型 case 实验只是部分机制验证 |
| dist 需要另建包下载/加载体系 | 同一 Go 模块携带 .go/.d.goxide/可选 .gox；外部 marker 的 lowering 单独评审 |

本登记覆盖截至 2026-10-01 的实质讨论，包括最后被放弃或延期的方向。它不把聊天里的探索示例视为已经批准的完整语言规范。

当前用户讨论焦点是 *T 功能拆分；[RFC 0003](../rfcs/research/0003-pointer-roles-and-mvp.md)纠正仅按运行时冗余推荐去掉 Arc 的判断；现优先评估 Arc 名义保留/转交意图，与普通引用+保留契约对照，具体协议尚未批准。Box、Cell、RefCell 均不因举例自动加入。

实现结果回流：基准 `47af69f` 已实现限定 print/println 与具体 Default helper，详细成功/拒绝边界见 [宏专题回流](macros.md#33-实现结果回流限定打印协议)及 [Default 回流](macros.md#521-实现结果回流具体-default-与-go-零值)。实现证据不把 T11/T12 的通用 trait ABI 变成已确认设计，也不改原文档标签。


已确认 **D27（最新用户反馈）**：语言仅封闭变体 enum，撤回开放 raw/C enum；serde 内建宏负责数值/string 外部映射，未知 variant 解码报错，不制造合法未知值。业务 validator 后置，现有结构类型/enum边界验证保留。

已确认 **D28（最新用户反馈）**：struct 派生 Default 要求每个字段实现 Default，否则手写；enum 标记默认变体。后续用户确认：仅选中默认变体的 payload 字段需要 Default，非默认变体不要求；允许带 payload 的默认变体。手动 Default 采用 `impl Default for T { func default() T { ... } }`，不引入 static，首参数 self 区分实例成员与关联函数。Debug 必须清楚说明输出字段/payload内容与隐私、格式稳定性边界。

已确认 **D29（2026-10-13 用户确认）**：首版支持 `match &mut enum_value { Var1(inner) => *inner += 1 }` 的原位可写 payload 借用，引用指向真实存储。用户 enum 采用密封接口与指针变体，普通复制共享变体对象；整体替换 enum 后，旧 payload 投影仍指向旧对象。Option/Result 在 P6 前保留既有内联槽表示。该确认取代此前要求用户 enum 独立值复制的约束；[引用合同](enum-payload-borrows.md)规定两类存储的实际行为。D20 不变。

已确认 **D30（2026-10-02 用户收口纠正）**：goxide 跨 module 消费 goxide 库应恢复增强信息，普通 Go 消费生成库则不执行 goxide 静态增强检查，类似 TS/JS 消费边界。同模块联合验收不等于此能力已完成，也未获批准缩成同模块 MVP。

已确认 **D31（2026-10-02 用户收口纠正）**：`mut& T` 降低为 Go `*T` 应有自然互操作，不接受用权限擦除概念一刀切拒绝可写引用到普通 Go 指针 API；只读 `&T` 仍不能借该路径升级写权限。D11/U15 的旧显式桥候选须按这一新意图修订，不能作为拒绝新需求的既定前提；具体递归/返回/回调边界仍需核对，不由此宣布实现完成。复杂 match 表达式限制是工程欠账，不是获批语言禁令或范围缩减。

已确认 **D32（2026-10-03 显式比较）**：enum 相等走显式 PartialEq/equals，Eq 依赖 PartialEq 并标记等价关系；允许显式 derive 自动生成。含不可比载荷的已知 enum 比较资格编译期判断，不以 Go interface 运行时 panic 充当策略。首片具体值参数、两实参求值后左/右验证沿既有机制实现；普通原生 Go 比较与当前 enum-nil 例外保持。Go comparable 不等于 Eq，Eq 不自动使类型成为 Go map key；不扩 full trait、HashMap 或指针拆分。具体实现与未完成边界见 [RFC](../rfcs/0008-enums.md#显式-partialeqeq-首片2026-10-03)。

已确认 **D33（2026-10-03 容器 nil/空）**：同类型 nil slice 与非 nil 空 slice 不相等，nil map 与非 nil 空 map 不相等；两个 nil 相等，两个非 nil 空容器按内容相等。equals 先区分 nil，再比较内容，不做隐式 normalization。Option.None、Some(nil 载荷)、Some(非 nil 空载荷) 三者区分；serde null/[]/{}不因此改变。固定长度 Go array 没有 nil 状态。该语义已定，不再作为容器派生的待决项；map 键资格按后续 D34 有限实施，自定义相等和手写接口仍单列。[契约参考测试](../../tests/design/enum/equality_contract_test.go)仅为纯 Go 规则探针；实际容器实现证据另见 [本片验收](../t0-acceptance.md#容器-equality-派生2026-10-03)。

已确认 **D34（2026-10-03 容器比较派生）**：slice/map 按 D33 区分 nil/空并递归调用元素/值的 PartialEq/Eq 协议，不以 reflect.DeepEqual 替代。原生 map 首片仅接纳能证明原生查找与相等契约一致的稳定键，不能由 float NaN、自定义 equals 或 Go comparable 偷渡资格；不引入 HashMap。嵌套 struct/array/enum 与 Option 状态需要真实生成验收。手写比较入口可下一片，指针拆分/原位 borrow 不前置阻断本轮独立 enum 阶段。

已确认 **D35（2026-10-03 手写具体比较）**：接通具体 PartialEq/Eq 实现，首参 self 为实例、无 static，复用显式 equals registry，不新增完整 trait。手写 PartialEq 决定业务相等，派生包含该类型时调用其协议；手写 Eq 是作者承诺，不能自动授予原生 map 键资格。当前值参数 `func equals(self T, other T) bool` 与空 Eq impl 沿既有 helper 约定实施；不依赖或默认批准借用/所有权签名。enum 唯一比较入口先校验再执行用户体，保留原函数帧；不是所有公开函数的自动验证。[实施规则](../rfcs/0008-enums.md#手写具体-partialeqeq2026-10-03)区分本片边界与未定扩展。


已确认 **D36（2026-10-04 显式 Go 多返回适配）**：提供普通 Ok/Err 与保留错误时返回值两种显式宏，覆盖 Reader 的部分成功。保值版不能经 `?` 静默丢弃 Value；原始 error 身份、typed-nil 语义、单次求值与用户函数边界保持。首片工程 API、具体类型/数量边界和真实验收见 [RFC 0005](../rfcs/research/0005-option-result-question.md#显式-go-result-适配宏2026-10-04)。不授权隐式原生调用转换、Residual/Try、tuple 或新指针设计。


D36 补充（用户已授权，先规范后实现）：`go_result!(call)` / `go_result_keep!(call)` 按真实返回签名推导 T/E，保留具名 nilable 错误类型，按原类型 nil 判断；error 接口不动态猜 E，非 nilable struct 错误明确拒绝。显式 T 形式继续精确核对。完整黑白案例、typed 阶段/多文件/metadata 与网站验收见 [T/E 推导规范](../rfcs/research/0005-option-result-question.md#按静态返回签名推导-te已授权实施前规范)。


已确认 **工具链 UT01（2026-10-04，独立于语言决策编号）**：迁入后的项目根只保留 `goxide.toml` / `goxide.lock` 为配置/锁权威；混合项目表示 `.go` / `.gox` 共存。显式module/Go/toolchain建议及原生require/replace/exclude写入TOML，真实Go负责选版/校验/构建；Go module元数据只在生成视图存在。原Go项目经只读plan、独立staging构建验证后产生新工程，不自动删除/移动/覆盖原文件。具体首片字段、锁与拒绝边界见 [RFC 0025](../rfcs/0025-unified-project.md)，features/多目标见 [RFC 0022](../rfcs/0022-project-toolchain.md)，授权脚本见 [RFC 0023](../rfcs/0023-build-scripts.md)。首片已提交，[集成回归与独立复审](../reviews/unified-project-20261004.md)已完成；确认方向不等于批准完整Cargo兼容。

UT01 替代记录：早期 [TOML 设计](toml-manifest.md) 的长期根go.mod/go.sum兼容、hgo.lock名称和“纯TOML未来提案”标为superseded，原文仅留研究历史；Go proxy source发行物仍需一致传输元数据，不能把发布合同误当作者根双配置许可。


UT01 后续用户要求（2026-10-04）：自动迁移包括已有 vendor，不能以远端重新解析覆盖本地修改。按 [供应源码迁入合同](vendor-local-modules.md) 验证后物化普通本地模块；Goxide 不保留 vendor 模式、内容锁或独立拆出协议。原 modules.txt/版本/replace 仅作 provenance，go.sum 不证明修改字节。默认 local，模块级来源候选与交互另步交付；原工程保留，网站由 owner 同步。

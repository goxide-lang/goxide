# 设计决策与讨论覆盖登记

更新：2026-10-01。项目暂称 hgo / Hyper Go；远端仓库名 `goxide` 不代表已经决定改名。

本文区分讨论中确认的方向、为使设计可评审而写出的候选规则，以及仍没有方案的部分。**“已确认”不表示“已实现”**；当前没有 hgo 编译器、linter、LSP 或可发布基础包。实现进度以 [roadmap](../../ROADMAP.md) 为准。

## 1. 状态含义与维护规则

- **已确认**：用户明确提出或接受的约束，后续设计应遵守。确认功能方向不等于确认其全部语法、算法或 ABI。
- **暂定**：已有可讨论的方案，包括代理提出的具体规则和 Go 降低示例；可以验证可行性，但不能因写入文档或 Go 测试通过就当作已获确认。
- **未定**：尚未选定方案、缺少必要规则，或明确延期。记录阻塞的工作与下一步决策，不用默认值掩盖。

各专题开头列出三类状态。除开头明确列出的已确认范围外，正文中的规则、示例和验收条件均按暂定设计理解；“必须”表示候选方案内部需满足的条件，不自动升级为已确认决策。可复现的 Go 行为属于技术证据，不能代替设计确认。

后续改变状态时，同时更新本登记、对应专题和 roadmap；被替代的方案在第 5 节集中登记，专题若保留解释需明确标为历史，不混在当前规范中。新增语法一律说明源级语义、Go 降低、Go 导入和导出、检查边界与适配成本。

## 2. 已确认的方向

| ID | 已确认范围 | 文档与边界 |
| --- | --- | --- |
| D01 | 以转译器 + 转译前 linter 为架构，生成普通 Go，保留 Go 生态与运行时 | [总纲](overview.md)；不以独立所有权运行时或原生机器码后端替代 |
| D02 | 所有新语法同时设计 Go 降低与双向互操作 | [总纲](overview.md)；不能只展示 hgo 写法 |
| D03 | 改善安全性、显式实现与可读性、Go 包互操作是三项目标 | [符合程度](overview.md#21-三项目标的符合程度)；尚无完整安全性证明 |
| D04 | 使用 trait 表达显式实现，保留 Go interface 的结构式语义；纳入普通关联类型方向 | [trait](traits.md)、[关联类型](associated-types.md)；泛型和投影细节暂定 |
| D05 | `declare impl AorB::XXX for BBB::Type;` 可写在用户 C 包普通 .hgo 内；无体采用原方法并补 marker，As 返回原值 | [Go 适配](go-interop.md)；只由 hgo 看见，不改变既有纯 Go 项目 |
| D06 | 有体 impl 的实现函数直接定义在对应 wrapper；多个 impl 的同名方法允许不同签名或行为；保持稳定 As 入口 | [trait](traits.md)；不再在原类型生成改名业务方法 |
| D07 | 根据使用处期望类型决定是否插入 trait 适配；保留值本身的类型 | [使用处转换](traits.md#6-期望类型驱动的转换)；泛型和容器细节暂定 |
| D08 | 拆分 Go 指针用途：Option 表达缺失、`mut& T` 表达写引用、`Arc[T]` 对应 Go 共享指针；不引入全局 move | [引用专题](pointers-and-borrows.md)；D19 更新默认诊断：别名/独占风险 warning，不承诺静态独占，Arc 使用 Go GC |
| D09 | Lock / RWLock 保存数据并采用指针句柄，避免复制 mutex；回调访问及返回结果 | [锁](synchronization.md)；`Do[P any]` 使用 Go 1.27，最低语言版本和工具链为 1.27 |
| D10 | 普通 declare 补充 / 覆盖原生 Go 的 hgo 类型视图，不写 unsafe declare；裸 API 与已声明 / 包装 API 分层 | [原生 API](foreign-api.md)；.d.hgo 与类似 @types 的外置声明包均纳入方向 |
| D11 | 使用 `unsafe <expr>`；拆分后的指针通过 unsafe as_ptr 转成统一 `Ptr[T]`；提供 AsPtr trait 与关联类型 | [Ptr / AsPtr](foreign-api.md)；Go 候选表示 `type Ptr[T any] *T`，源级精确拼写未冻结 |
| D12 | 变体枚举、Option / Result、模式匹配；接受 Go 调用方能构造非法值，不能假称阻止了 Go 零值 | [枚举与 match](enums-and-match.md)；边界策略和布局见暂定项 |
| D13 | pub 标记公开边界的方向；需要处理 Go 调用生成 API 的情况 | [总纲](overview.md#34-go-边界与非法值)；完整可见性与检查入口未定 |
| D14 | 卫生宏参考 Rust；先做模式 / 模板声明宏及内建派生，提供 print / println、Default / Clone / Debug；用户过程宏后续考虑 | [宏](macros.md)；纯的用户 Syntax→Syntax 编译期程序仍是过程宏 |
| D15 | hgo 基础包提供现有 Go std 的 safe 视图与泛型 slice | [基础包](standard-library.md)；以 Go API 真实行为为依据，不以 Rust std 为规范 |
| D16 | quote / syn 风格能力放在额外包 | [语法工具](syntax-tooling.md)；是否列入首个可用版本及协议仍暂定 |
| D17 | 提供面向 hgo 的 LSP，未来考虑 hgo 自举 | [编译器与工具链](compiler-and-tooling.md)；启动语言、复用组件和阶段路线为暂定方案 |
| D18 | 先完成设计；总纲和各专题分开，维护决策状态与路线，支持另一台机器继续 | [README](../../README.md)、[roadmap](../../ROADMAP.md)；本次提交不代表开始编译器实现 |

已确认新增 **D19（2026-10-01）**：用户明确要求默认兼容 Go 式指针/别名；有具体风险依据先 warning，不以无法证明安全大面积拒绝。语法、类型、已确认不变量错误不降级；warning 与运行时 race 分开，不能宣称静态安全。该反馈否决 T01/T06 及前两轮严格默认，不撤销无关 D01–D18。

已确认新增 **D20（2026-10-01）**：别名/借用风险静态分析独立长期推进，完全移出 MVP，包括 W01–W04。MVP 保留正常语法/类型等基础检查；其他设计考虑可分析性，但不强制复杂 effect 注解、来源分析或保守限制。D19 仍约束未来风险诊断的默认级别。

已确认新增 **D21（2026-10-01）**：用户将宏、enum/match、指针能力中的只读/可写分离列为 T0。T0 是优先级，不表示全套特性一次实现。enum 以强化 Go 数值/string 常量枚举为重要场景，空值不是阻塞优先级的理由；带载荷 enum 仍保留，非法输入/匹配行为需明确。共享与内部可变考虑 hgo/std 包装，不先引入整套所有权或智能指针系统。用户认为宏收益明确；仍保留客观卫生、诊断和工具链验收，不把“100% 正向”写成实测结论。D20 不变。

已确认新增 **D22（2026-10-01）**：`?` 首先限于 Option/Result，不公开 Try/Residual。thiserror 核心派生生成显式 From；返回 Result 的目标错误类型已知时允许唯一的一步显式转换，不搜索转换链、不接受任意泛型 blanket 转换。静态 Go `error` 只能走明确兜底或业务分类，不能推断其动态变体。精确语法、impl 选择细则和前端实现仍待闭合，见 RFC 0005。

**实施交接授权**：先冻结本地文档基线 `v0.0.0-mvp-doc`，再在独立实现会话推进模式宏、log/tracing 场景及 Debug/Clone/serde/thiserror 派生。JSON 是推荐首版格式，完整 Rust 兼容并非目标。本线程继续研究/spec/RFC，实现发现回流；授权实施不表示其他候选已经确认。

已确认新增 **D23（文档标签之后，按最新反馈更新）**：用户要求 serde 优先，随后明确“仿照 serde-rs 的实现”。核心采用类型/格式分离的 Serialize/Deserialize、Serializer/Deserializer 与 Visitor/Access；Value 属于 serde-json 等格式层，首版 JSON、未来 YAML 独立适配。此前助手提出的公开 core Value、CoreValue-v1 与首版必须全树中转被这一决定覆盖，不再等待用户在旧候选中选择。Go/hgo ABI/子集/格式策略仍见 U18；本研究线程不实施宏或 serde 代码。

**新增研究授权，不是后端选择**：对照“二进制模式无冲突直接适配、有冲突最小 wrapper；Go 源码模式固定 wrapper”。与 D05/D06 现行 marker/固定有体 wrapper 的差异见 RFC 0004；不得因研究授权就修改已有 impl 的可观察语义。

## 3. 已有方案但尚未冻结

| ID | 暂定方案 | 需要确认或验证的部分 |
| --- | --- | --- |
| T01 | **默认策略已否决**：所有未声明 Go callable 统一 unsafe | D19：缺摘要不要求 unsafe；D11 明确操作及真实类型转换边界保留，U15 待协调 |
| T02 | 非空与构造、读写路径、持有类型定位均暂定；Box 不预设独占 | [RFC 0003](../rfcs/0003-pointer-roles-and-mvp.md)比较引用优先与保留持有层；Arc 的已确认 D08 不能静默撤销，新 as_ref/as_mut 与 Cell 未批准 |
| T03 | 非法 Go 输入在保持签名入口 panic，可另设显式返回错误的入口 | Result 的 nil 不能当合法变体或悄悄改签名；检查时机、pub 范围、回调和共享值重新验证仍需设计 |
| T04 | enum 降低为 interface + 值型 case；Option.None 使用 nil，Result.nil 非法 | ABI、完整递归检查、泛型身份、nil / typed nil / 伪造 case 等规则属于[当前候选](enums-and-match.md) |
| T05 | match 表达式一次求值，guard 不计穷尽覆盖，先提供值绑定 | 控制流保留、求值顺序、不可达分支和后续借用模式的完整规范 |
| T06 | **默认阻断已否决**；来源追踪改为有依据的风险 warning | D19 与 RFC B01–B08：未知不拒绝也不刷警告；不宣称静态安全 |
| T07 | 原定义包隔离构建视图加入 marker / As；独立依赖副本与本地 replace，按需 overlay | 已确认纯 Go 原项目不受影响；具体构建、缓存、发布方式暂定，GOROOT 与模块缓存限制分开处理 |
| T08 | impl 按接收者、trait、参数规范化；冲突报错、显式导入元数据 | 跨包可见性、接收权限身份、版本和构建条件如何参与选择 |
| T09 | 固有方法优先、唯一可见 trait 候选；稳定符号编码，限定调用 | 最终源码拼写、名称冲突策略和 ABI 编码未定；新增 impl 可能增加源码歧义 |
| T10 | 宏模板自由名字按定义点绑定、捕获语法保留调用点来源 | 此处有意不同于 Rust mixed-site hygiene；宏匹配、阶段、缓存、预算及授权规则是候选设计 |
| T11 | 字面量 `{}` / `{:?}` 对应 Display / Debug；打印失败 panic；固定字段式 derive 规则 | 格式子集、formatter、Default / Clone / Debug 精确协议及错误策略未冻结 |
| T12 | Default / Clone 的静态 Self 操作选择具体 impl 并降低为普通 Go 函数 | 静态成员语法与泛型实现证据未定；不能宣称所有 trait 均可作为 Go 运行时接口 |
| T13 | 名义 Slice 门面，ReadOutcome 等逐 API 包装，明确浅复制 / clone_with | 用户确认功能方向；容器 ABI、API 名称、safe std 首批清单与成本都是草案 |
| T14 | syntax 协议 + 普通 syn / quote 包可先于过程宏宿主提供 | 这是实现建议，不承诺首版交付；token 构造原语、卫生上下文和包版本尚未冻结 |
| T15 | Go 启动前端 / LSP，独立 hgo AST 和语义 IR；复用 Go 包加载、输出和类型信息 | 自举阶段、复用边界、仓库布局和工具进程协议见[工具链草案](compiler-and-tooling.md) |
| T16 | Unit 用 struct{}；第三方锁经普通 Lock/Unlock 原语接入 | 回调语法、没有结果的 return 插入、第三方适配构造器及来源检查 |

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
| U08 | enum 最终 ABI、循环对象图校验与容器初始化算法 | 严格值不变量、性能和互操作成本 |
| U09 | 宏匹配器完整语法、formatter 协议、静态派生协议、卫生与源码追踪协议 | 首版宏可用性；用户过程宏及可编程 derive 明确延期 |
| U10 | safe Go std 包路径、逐入口契约、Slice ABI / Clone 策略、首批覆盖范围 | 不能宣称整个 Go std 已 safe |
| U11 | 公共语法包协议、受控 token 构造、版本与过程宏宿主能力 | 普通语法工具与编译期扩展分别验收 |
| U12 | 增量查询、完整 LSP、构建驱动、自举一致性验收 | 尚未实现；保持 Go seed 的方案为暂定 |
| U13 | supertrait、GAT、特化、借用 match 模式、锁块语法 | 延期 / 另行设计；“显式继承”当前指显式 impl，不是 supertrait |
| U14 | Rust / LLVM 到 Go 汇编的受限计算内核路线 | 不作为首版主后端；ABI / GC / 栈约定等尚未解决 |
| U17 | Option/Result 专用 `?` 的精确语法与实现、Go 多返回显式包装 | [RFC 0005](../rfcs/0005-option-result-question.md)；限域及显式 From 单步转换方向已确认，不公开 Try/Residual，不搜索转换链；Go error 静态类型需显式兜底或分类 |
| U18 | serde-rs 分层到 Go/hgo 的接口映射、字段/数字/enum 策略及派生子集 | [RFC 0006](../rfcs/0006-serde-core-and-formats.md)；分层已确定，具体 helper/visitor ABI 与严格 JSON 偏差仍候选；无公开 core Value，格式 Value 不强制中转 |
| U16 | trait 双路径、消除包装的语义条件及发布 ABI | [RFC 0004](../rfcs/0004-trait-lowering-paths.md)；比较直接调用/值视图/指针视图/字典，D05/D06 未被候选替换 |
| U15 | D11 显式 AsPtr/unsafe 与普通 Go 指针实参的便利桥接 | [RFC 0002](../rfcs/0002-native-pointer-bridge.md)比较原生边界自动桥接与普通显式 AsPtr；推荐前者，D11 窄例外待用户确认 |

### 4.1 本轮候选细化

[RFC 0001](../rfcs/0001-scoped-borrow-views.md)与[业务场景](../rfcs/0001-usage-and-tradeoffs.md)已按 D19 重写：B01–B08 解释默认允许，W01–W06 要求具体风险链、修复建议与误报/漏报成本。前两轮“词法借用/唯一性证明作为默认准入”的推荐已被用户否决；U03 的 impl 权限形式仍未选择。Go 夹具仅验证机制，不是 hgo warning 实现。

## 5. 已被替代或纠正的方案

| 早期说法 | 当前记录 |
| --- | --- |
| 无法证明不逃逸/独占就默认拒绝；无摘要 callable 自动 unsafe | 用户 D19 明确否决；有依据风险 warning，未知不报；D11 明确操作另行保留协调 |
| 用 enum 默认变体使任意 Go 零值都合法 | 放弃普遍保证；Go 可制造非法 Result，显式 Default 也不使零值合法 |
| 只有出现同名 trait 方法时才切换 wrapper | 当前 D05/D06 仍固定语义；D21 后重新研究二进制内部优化候选（U16），尚未批准按构建模式改变外部动态身份或行为 |
| wrapper 转调原类型上的改名业务函数 | 业务函数直接在 wrapper 上；具体调用可优化，但不能改语义 |
| .d.hgo 必须描述原生包已经存在的 wrapper | 原生包只需现有业务方法；declare 可在用户 .hgo 内。后来恢复 .d.hgo 作为可选 API 声明分发格式，不恢复“原生已有 wrapper”前提 |
| declare 无需补 marker，或 declare 本身写 unsafe | 无体 impl 补 marker；普通 declare 可信契约与函数 unsafe 属性分别处理 |
| raw Go *T 默认直接导入为 Option[Arc[T]] | 当前 raw 候选为 Ptr[T]；Option / Arc 等由声明或实际适配建立 |
| 使用 mut T / Ref 作为统一指针模型 | 当前方向以 mut& T、只读借用、Arc 和 Ptr 分工；未冻结部分见引用专题 |
| 连续下划线加一就能可逆编码组合名称 | 该编码有碰撞，不采用；正式符号编码未定 |
| 无 I/O 的 Syntax→Syntax 编译期用户函数是非过程宏 | 分类错误已纠正；仍属于过程宏，首版采用声明模式与内建派生 |
| safe 标准库参照 Rust std | 已纠正为 Go std；Rust 仅参考宏机制与派生协议 |

本登记覆盖截至 2026-10-01 的实质讨论，包括最后被放弃或延期的方向。它不把聊天里的探索示例视为已经批准的完整语言规范。

当前用户讨论焦点是 *T 功能拆分；[RFC 0003](../rfcs/0003-pointer-roles-and-mvp.md)纠正仅按运行时冗余推荐去掉 Arc 的判断；现优先评估 Arc 名义保留/转交意图，与普通引用+保留契约对照，具体协议尚未批准。Box、Cell、RefCell 均不因举例自动加入。

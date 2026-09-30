# 设计决策与讨论覆盖登记

更新：2026-10-01。项目暂称 hgo / Hyper Go；远端仓库名 `goxide` 不代表已经决定改名。

本文区分讨论中确认的方向、为使设计可评审而写出的候选规则，以及仍没有方案的部分。**“已确认”不表示“已实现”**；当前没有 hgo 编译器、linter、LSP 或可发布基础包。实现进度以 [roadmap](../../ROADMAP.md) 为准。

## 1. 状态含义与维护规则

- **已确认**：用户明确提出或接受的约束，后续设计应遵守。确认功能方向不等于确认其全部语法、算法或 ABI。
- **暂定**：已有可讨论的方案，包括代理提出的具体规则和 Go 降低示例；可以验证可行性，但不能因写入文档或 Go 测试通过就当作已获确认。
- **未定**：尚未选定方案、缺少必要规则，或明确延期。记录阻塞的工作与下一步决策，不用默认值掩盖。

各专题开头列出三类状态。除开头明确列出的已确认范围外，正文中的规则、示例和验收条件均按暂定设计理解；“必须”表示候选方案内部需满足的条件，不自动升级为已确认决策。可复现的 Go 行为属于技术证据，不能代替设计确认。

后续改变状态时，同时更新本登记、对应专题和 roadmap；被替代的方案在第 5 节集中登记，专题若保留解释需明确标为历史，不混在当前规范中。新增语法一律用[独立 RFC](../rfcs/README.md)说明源级语义、Go 降低、Go 导入和导出、检查边界与适配成本，并用编号正反例作为约束。

当前 trait 方向见 [RFC 0019](../rfcs/0019-uniform-trait-wrappers.md)：统一 wrapper、保留 marker、固定包级工厂及本地 As / 外部 ForT.As 便利 helper。用户已确认该方向，替代原始类型 marker 注入；具体 ABI 细节仍未冻结。类型关联函数由 impl 块定义，T::method → Go 包函数、Self/self 绑定及原生 Go 声明边界见 [RFC 0020](../rfcs/0020-associated-functions.md)。

## 2. 已确认的方向

| ID | 已确认范围 | 文档与边界 |
| --- | --- | --- |
| D01 | 以转译器 + 转译前 linter 为架构，生成普通 Go，保留 Go 生态与运行时 | [总纲](overview.md)；不以独立所有权运行时或原生机器码后端替代 |
| D02 | 所有新语法同时设计 Go 降低与双向互操作 | [总纲](overview.md)；不能只展示 hgo 写法 |
| D03 | 改善安全性、显式实现与可读性、Go 包互操作是三项目标 | [符合程度](overview.md#21-三项目标的符合程度)；尚无完整安全性证明 |
| D04 | 使用 trait 表达显式实现，保留 Go interface 的结构式语义；纳入普通关联类型方向 | [trait](traits.md)、[关联类型](associated-types.md)；泛型和投影细节暂定 |
| D05 | `declare impl AorB::XXX for BBB::Type;` 可写在用户 C 包普通 .hgo 内；无体采用原业务方法，通过 C 包 wrapper 显式转发并实现 marker | [Go 适配](go-interop.md)；原指针保持，接口动态类型是 wrapper，不向原始类型追加方法 |
| D06 | 有体 impl 的实现函数直接定义在对应 wrapper；多个 impl 的同名方法允许不同签名或行为；保持稳定 As 入口 | [trait](traits.md)；不再在原类型生成改名业务方法 |
| D07 | 根据使用处期望类型决定是否插入 trait 适配；保留值本身的类型 | [使用处转换](traits.md#6-期望类型驱动的转换)；泛型和容器细节暂定 |
| D08 | 拆分 Go 指针用途：Option 表达缺失、`mut& T` 表达写引用、`Arc[T]` 对应 Go 共享指针；不引入全局 move | [引用专题](pointers-and-borrows.md)；独占权限靠源级检查，Arc 使用 Go GC |
| D09 | Lock / RWLock 保存数据并采用指针句柄，避免复制 mutex；回调访问及返回结果 | [锁](synchronization.md)；`Do[P any]` 使用 Go 1.27，最低语言版本和工具链为 1.27 |
| D10 | 普通 declare 补充 / 覆盖原生 Go 的 hgo 类型视图，不写 unsafe declare；裸 API 与已声明 / 包装 API 分层 | [原生 API](foreign-api.md)；.d.hgo 与类似 @types 的外置声明包均纳入方向 |
| D11 | 使用 `unsafe <expr>`；拆分后的指针通过 unsafe as_ptr 转成统一 `Ptr[T]`；提供 AsPtr trait 与关联类型 | [Ptr / AsPtr](foreign-api.md)；Go 候选表示 `type Ptr[T any] *T`，源级精确拼写未冻结 |
| D12 | 变体枚举、Option / Result、模式匹配；接受 Go 调用方能构造非法值，不能假称阻止了 Go 零值 | [枚举与 match](enums-and-match.md)；边界策略和布局见暂定项 |
| D13 | pub 标记公开边界的方向；需要处理 Go 调用生成 API 的情况 | [总纲](overview.md#34-go-边界与非法值)；完整可见性与检查入口未定 |
| D14 | 卫生宏参考 Rust；先做模式 / 模板声明宏及内建派生，提供 print / println、Default / Clone / Debug；用户过程宏后续考虑 | [宏](macros.md)；纯的用户 Syntax→Syntax 编译期程序仍是过程宏 |
| D15 | hgo 基础包提供现有 Go std 的 safe 视图与泛型 slice | [基础包](standard-library.md)；以 Go API 真实行为为依据，不以 Rust std 为规范 |
| D16 | quote / syn 风格能力放在额外包 | [语法工具](syntax-tooling.md)；是否列入首个可用版本及协议仍暂定 |
| D17 | 提供面向 hgo 的 LSP，未来考虑 hgo 自举 | [编译器与工具链](compiler-and-tooling.md)；启动语言、复用组件和阶段路线为暂定方案 |
| D18 | 先完成设计；总纲和各专题分开，维护决策状态与路线，支持另一台机器继续 | [README](../../README.md)、[roadmap](../../ROADMAP.md)；本次修改不代表开始编译器实现 |
| D19 | 发布完整 dist；同一 Go 模块携带 .go、.d.hgo 与可选 .hgo；Go 编译 .go，hgo 额外加载声明或源码 | [RFC 0001](../rfcs/0001-package-loading.md)；wrapper 与 marker 随 impl 包发布，无需外部方法注入 |
| D20 | 相容 declare 可合并，有体实现保持唯一 | [RFC 0003](../rfcs/0003-declare-impl.md)；契约矛盾和原生采用/有体混合语义仍需明确 |
| D21 | match 按模式区分值复制、只读借用与可变借用 | [RFC 0009](../rfcs/0009-match.md)；具体语法、作用域与支持原位借用的 ABI 未定 |
| D22 | 所有新特性和语法有独立 spec/RFC，以转译前后、双向调用和反例表达约束 | [RFC 清单](../rfcs/README.md)、[模板](../rfcs/TEMPLATE.md)；已写文档不代表已实现 |
| D23 | hgo 源级使用变体枚举分派，不直接采用 Go 的 .(T)/.(type) 作为分派语法；增强保证由 hgo 工具链提供 | 后端和原生 Go 仍可使用 Go 类型检查/反射；普通 Go 调用者不获得 hgo 静态权限保证 |
| D24 | 所有运行时 trait 视图统一 wrapper；trait 接口与 wrapper 保留 marker；固定包级函数适配；Go 使用本包 As 方法或外部 ForT.As helper | [RFC 0019](../rfcs/0019-uniform-trait-wrappers.md)；具体符号编码、权限、nil/比较与多包载体选择未定 |
| D25 | 构造函数等类型关联函数定义在独立 impl T 块中，用 T::method 调用，降低为普通 Go 包函数 | [RFC 0020](../rfcs/0020-associated-functions.md)；不将函数写入 struct 声明；块内成员拼写、Go 名称、初始化等具体规则为讨论草案 |
| D26 | impl 成员按 hgo 规则转译并支持 Self/self；直接书写的 Go 函数和方法保留原生 Go 语义 | [RFC 0020](../rfcs/0020-associated-functions.md)；Self 绑定源目标类型，self 仅绑定实例接收者；不全局替换同名 Go 标识符，不限制其他 hgo 特性只能位于 impl 内 |

## 3. 已有方案但尚未冻结

| ID | 暂定方案 | 需要确认或验证的部分 |
| --- | --- | --- |
| T01 | 所有未声明 Go callable 统一 unsafe，而非仅含指针的函数 | 用户提出过两种粒度；当前推荐统一规则，尚未单独确认；裸全局与操作分类也需细化 |
| T02 | Arc / 读写借用非空，缺失用 Option；`&T` 为只读借用；Box 暂不承担唯一所有权 | 非空构造、复制 / 再借用、解引用、Box 是否提供及具体 API，见[引用专题](pointers-and-borrows.md) |
| T03 | 非法 Go 输入在保持签名入口 panic，可另设显式返回错误的入口 | Result 的 nil 不能当合法变体或悄悄改签名；检查时机、pub 范围、回调和共享值重新验证仍需设计 |
| T04 | enum 的既有机制候选为 interface + 值型 case；Option.None 使用 nil，Result.nil 非法 | 候选尚不能直接支持 D21 原位借用，最终 ABI、递归检查与初始化仍未定；见[枚举 RFC](../rfcs/0008-enums.md) |
| T05 | match 目标一次求值，guard 不计穷尽覆盖，按模式保存值或原存储位置 | D21 已要求区分复制/借用；控制流、guard、借用范围和完整规范仍待定 |
| T06 | 保守检查借用范围、别名、重入、逃逸；检查失败阻断安全转译 | 具体数据流算法与可接受子集未定；-race 仅补充动态证据，不作静态证明 |
| T07（历史） | 原定义包隔离视图注入 marker / As | 已被 D24 统一 wrapper 取代；旧夹具仅保留历史机制，不作为当前实现要求 |
| T08 | impl 按接收者、trait、参数规范化；相容 declare 合并、有体唯一、矛盾报错 | 合并的完整等价判据、跨包可见性、接收权限身份、版本/构建条件及混合模式 |
| T09 | 固有方法优先、唯一可见 trait 候选；稳定符号编码，限定调用 | 最终源码拼写、名称冲突策略和 ABI 编码未定；新增 impl 可能增加源码歧义 |
| T10 | 宏模板自由名字按定义点绑定、捕获语法保留调用点来源 | 此处有意不同于 Rust mixed-site hygiene；宏匹配、阶段、缓存、预算及授权规则是候选设计 |
| T11 | 字面量 `{}` / `{:?}` 对应 Display / Debug；打印失败 panic；固定字段式 derive 规则 | 格式子集、formatter、Default / Clone / Debug 精确协议及错误策略未冻结 |
| T12 | Default / Clone 的静态 Self 操作选择具体 impl 并降低为普通 Go 函数 | 静态成员语法与泛型实现证据未定；不能宣称所有 trait 均可作为 Go 运行时接口 |
| T13 | 用户建议 std 提供 Vec/借用视图和多种 Map；既有 Slice、ReadOutcome、copy/clone_with 为候选 | [Vec](../rfcs/0010-collections.md)、[Map](../rfcs/0017-maps.md)；赋值语义、元素借用、ABI、名称和具体后端未定 |
| T14 | syntax 协议 + 普通 syn / quote 包可先于过程宏宿主提供 | 这是实现建议，不承诺首版交付；token 构造原语、卫生上下文和包版本尚未冻结 |
| T15 | Go 启动前端 / LSP，独立 hgo AST 和语义 IR；复用 Go 包加载、输出和类型信息 | 自举阶段、复用边界、仓库布局和工具进程协议见[工具链草案](compiler-and-tooling.md) |
| T16 | Unit 用 struct{}；第三方锁经普通 Lock/Unlock 原语接入 | 回调语法、没有结果的 return 插入、第三方适配构造器及来源检查 |
| T17 | D24 已确认的 wrapper/marker/helper 方向下的具体工厂、便利名称和载体协议 | [RFC 0019](../rfcs/0019-uniform-trait-wrappers.md)；编码、冲突、共享适配包和权限细则仍需定稿 |
| T18 | impl 内 static func 示例、实例接收权限拼写、new → NewT、其他关联函数 → TMethod、首版不重载 | [RFC 0020](../rfcs/0020-associated-functions.md)；impl 块及 Self/self 支持、T::method 与 Go 包函数映射已确认，全部成员语法/导出规则未冻结 |

## 4. 未定与明确延期

这些项有可能讨论过候选方案，但尚不能据此实现并宣称规范完成。处理次序见 [roadmap](../../ROADMAP.md)。

| ID | 未闭合问题 | 影响 |
| --- | --- | --- |
| U01 | 完整词法 / 语法、类型表示、包和符号身份；最终语言名称 | 解析器、诊断与稳定输出 |
| U02 | 引用作用域、再借用、可复制性、容器 / 闭包 / trait 视图传播、输出槽位 | 安全性首个可验收子集；Box、解引用与 **T 等不能靠 Go 表示自动解决 |
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

## 5. 已被替代或纠正的方案

| 早期说法 | 当前记录 |
| --- | --- |
| 用 enum 默认变体使任意 Go 零值都合法 | 放弃普遍保证；Go 可制造非法 Result，显式 Default 也不使零值合法 |
| 只有出现同名 trait 方法时才切换 wrapper | declare 与有体 impl 均固定 wrapper，不因后来新增 impl 改变旧表示 |
| wrapper 转调原类型上的改名业务函数 | 业务函数直接在 wrapper 上；具体调用可优化，但不能改语义 |
| .d.hgo 必须描述原生包已经存在的 wrapper | 原生包只需现有业务方法；declare 可在用户 .hgo 内。后来恢复 .d.hgo 作为可选 API 声明分发格式，不恢复“原生已有 wrapper”前提 |
| declare 无需 marker，或 declare 本身写 unsafe | 运行时 trait 的 marker 保留在接口和 wrapper；普通 declare 可信契约与函数 unsafe 属性分别处理 |
| declare 向原始类型补 marker，As 返回原始动态类型 | 统一 wrapper，保留内部原指针；固定包级工厂与 Go 便利 helper，不改写外部依赖 |
| raw Go *T 默认直接导入为 Option[Arc[T]] | 当前 raw 候选为 Ptr[T]；Option / Arc 等由声明或实际适配建立 |
| 使用 mut T / Ref 作为统一指针模型 | 当前方向以 mut& T、只读借用、Arc 和 Ptr 分工；未冻结部分见引用专题 |
| 连续下划线加一就能可逆编码组合名称 | 该编码有碰撞，不采用；正式符号编码未定 |
| 无 I/O 的 Syntax→Syntax 编译期用户函数是非过程宏 | 分类错误已纠正；仍属于过程宏，首版采用声明模式与内建派生 |
| safe 标准库参照 Rust std | 已纠正为 Go std；Rust 仅参考宏机制与派生协议 |
| 独立的相同 declare 一律冲突 | 相容无体声明可合并，有体唯一；矛盾契约仍诊断 |
| match 固定只有值绑定，借用模式延期 | 按模式区分值/只读借用/可变借用；旧值型 case 实验只是部分机制验证 |
| dist 需要另建包下载/加载体系 | 同一 Go 模块携带 .go/.d.hgo/可选 .hgo；外部 marker 的 lowering 单独评审 |

本登记覆盖截至 2026-10-01 的实质讨论，包括最后被放弃或延期的方向。它不把聊天里的探索示例视为已经批准的完整语言规范。

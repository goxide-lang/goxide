# goxide 设计总纲

交付边界（D20）：本文涉及的别名/借用风险 warning 是未来独立长期分析，不属于 MVP 验收；当前语言语法、类型、明确操作与真实 Go 行为照常设计。不得为未实现分析器强制复杂效果注解。

状态：设计阶段。本文记录方向与专题边界，不承诺文档中能力已经实现。

[RFC 0003](../rfcs/0003-declare-impl.md) 已确认移除 `declare impl` 原生采用入口及相容合并，当前编译器实现待删。下文保留的原生采用说明仅描述这部分现存实现；普通有体 impl、运行时 wrapper、Go 双向调用和 `declare func/type` 保留。

当前优先级（D21）：宏、enum/match、只读/可写访问分离为 T0；T0 不要求所有扩展一次交付。trait 后端已按 R24/RFC0019 确认统一 wrapper；旧[双路径研究](../rfcs/research/0004-trait-lowering-paths.md)仅保留历史比较，不覆盖该决定。

## 1. 目标

goxide 采用类似 TypeScript 到 JavaScript 的开发方式：源语言提供更强的静态语义，编译为标准 Go 源码，随后使用 Go 工具链构建。复用 Go 的运行时、GC、goroutine 和包生态；提供自己的 LSP，使 IDE 面向 goxide 源码工作。

实现边界明确为 **转译器 + 转译前 linter + 普通 Go 支持库**。源级类型与引用权限由前端检查，输出仍是普通 Go 类型、指针、闭包和同步操作。完整的跨包别名或所有权证明不作为首版前提；不能把尚未实现的检查或 `-race` 结果说成静态独占证明。

linter 与类型检查分别承担风险提示和语言合法性：语法、类型、impl、match 及明确 unsafe 操作的上下文错误仍阻断；借用返回、别名、并发及锁协议风险按 D19 默认 warning。仅无法证明安全或缺少效果摘要不拒绝、不普遍警告。未发现风险不等于安全证明，warning 产物不称为静态安全产物。

最低目标语言及工具链版本为 **Go 1.27**。支持库和生成模块使用 `go 1.27` 或更新的语言版本；运行新工具链但把模块语言版本保留为旧版本并不足够。Go 1.27 的泛型方法允许 `Lock[T].Do[P any]` 为每次调用独立推导返回类型。[Go 1.27 语言变化](https://go.dev/doc/go1.27#language)

主要语言能力：

1. 显式 trait 实现，一个类型可实现多个 trait，允许不同实现拥有同名方法。
2. 变体枚举、`Option`、`Result` 和带穷尽性检查的模式匹配。
3. 卫生宏，以及覆盖宏展开前后位置关系的诊断与 IDE 支持；首版参考 Rust 的声明宏（模式 / 模板 / 重复），提供 print / println 与 Default / Clone / Debug 内建派生，用户可编程过程宏及自定义过程派生延期。
4. Go 包互操作，普通原生函数、方法和 Go interface 按真实签名使用；需要适配而完整头不满足有体 impl 归属规则时，以自有包装类型提供普通有体 impl，并由固定工厂生成 trait 视图，见 [RFC 0003](../rfcs/0003-declare-impl.md)。
5. 基础包围绕现有 Go std 提供 safe 声明与实际包装，并补充泛型 slice 的权限与 API 视图；额外包提供 quote / syn 风格语法工具。
6. 用 Go 启动编译器与 LSP，之后逐步使用 goxide 自举。

不以实现 Rust 的所有类型系统能力为目标；不以替换 Go 运行时或首版直接输出 Plan 9 汇编为目标。

## 2. 决策状态

每项新特性必须有[独立 RFC](../rfcs/README.md)，附编号的转译前后例子、拒绝例子与验收条件；具体格式见[模板](../rfcs/TEMPLATE.md)。专题文档保留背景和机制讨论。

每项 goxide 新语法的详细设计都必须说明：源级语义、具体 Go 表示与代码生成、原生 Go API 如何导入并调用、goxide API 如何被 Go 调用，以及转译前 linter 能检查和不能检查的边界。不能只给出 goxide 写法而省略双向互操作；需要复制、适配、检查或生成辅助代码时，应明确其行为与成本。尚未确定的方向标为草案，不借用 Go 编译成功来证明源级约束成立。

文档统一采用三种状态：**已确认**、**暂定**、**未定**，定义与逐项范围见[决策登记](decisions.md)。此前“已确定 / 设计草案 / 待设计”分别按这三类解释。确认目标不表示确认全部细节，更不表示已实现。示例中的语法、ABI 和算法默认暂定，除非明确列为已确认。实现里程碑与跨机器接续见 [roadmap](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/ROADMAP.md)。

| 部分 | 当前记录（含暂定方案） | 仍需细化的内容 | 详细设计 |
| --- | --- | --- | --- |
| 编译目标 | 已确认生成 Go 源码，复用 Go 工具链与运行时 | 包输出布局、增量构建、发布格式 | [工具链](compiler-and-tooling.md) |
| trait | 原生使用 `trait`；显式 `impl`；Go `interface` 保留结构式语义 | 精确语法、可见性、完整泛型约束，Default / Clone 等静态 Self 操作 | [trait](traits.md) |
| 多接口实现 | 普通有体 impl 使用稳定 wrapper，业务方法与 marker 放在 wrapper；期望类型驱动固定工厂调用；原生 declare 采用已决定移除 | ABI 名称编码、权限和优化边界 | [trait](traits.md)、[移除合同](../rfcs/0003-declare-impl.md) |
| Go 适配 | impl 包持有 wrapper、marker 和工厂；本地类型 As 方法、外部类型 ForT.As 入口方便 Go 使用 | 正式命名、冲突策略与声明绑定格式 | [Go原生类型的显式适配](go-interop.md) |
| Go API 视图 | 普通 declare 补充类型契约；普通 Go 调用不因缺摘要要求 unsafe；Ptr/AsPtr 保留明确边界 | 统一标记未声明 callable 为 unsafe 已否决；U15 桥接待协调、声明分发与选择、引用诊断 | [原生 API 与裸指针](foreign-api.md) |
| 关联类型 | 纳入设计，先支持普通关联类型 | 投影语法、泛型降低；GAT 等延期 | [关联类型](associated-types.md) |
| impl 成员与类型关联函数 | 成员定义在 impl 块，支持 Self/self；T::method 降低为 Go 包函数；直接写的 Go 函数与方法保留原语义 | 块内 static 标记、接收权限、公开命名、与静态 trait 操作的限定规则 | [RFC 0020](../rfcs/0020-associated-functions.md) |
| 指针拆分 | 已确认 &mut 写引用、Arc 对应 Go 指针、Option 分担缺失语义；只读借用和非空细则暂定 | Box、解引用、来源 / 逃逸、再借用、输出槽位、接收权限身份 | [引用专题](pointers-and-borrows.md) |
| 锁与引用检查 | 锁句柄转为支持库指针；回调方法使用 Go 1.27 泛型返回类型；基础读写权限由类型检查，风险分析按 D20 长期独立 | Unit 映射、诊断范围、第三方锁适配 | [锁与作用域回调](synchronization.md) |
| 枚举与空值 | goxide 内部静态约束；接受原生 Go 调用方可能输入非法值 | interface 加值型 case 的首版布局草案、递归校验实现、可变别名 | [变体枚举与 match](enums-and-match.md) |
| 模式匹配 | 已确认功能方向；一次求值、穷尽、guard 覆盖与控制流为暂定具体规则 | 值/只读借用/可变借用模式的语法与对应 ABI、决策树实现 | [变体枚举与 match](enums-and-match.md) |
| 卫生宏 | 符号卫生、源码追踪；参考 Rust 的声明宏，print / println 与 Default / Clone / Debug；用户过程宏延期 | 模式语法、格式化协议、静态 trait 操作、阶段与缓存协议 | [卫生宏](macros.md) |
| 基础与标准库 | 为现有 Go std 提供逐 API 的 safe 声明或包装；补充泛型 slice | 包路径、权限契约、具体 API 清单与容器 ABI；保留 Go 的错误及别名语义 | [基础包与 Go std](standard-library.md) |
| 语法工具 | 额外包提供 quote / syn 风格能力；普通解析与生成不必等待过程宏 | 公共 token / AST 协议、受控上下文、token 构造原语；编译期用户代码执行延期 | [语法工具](syntax-tooling.md) |
| 可见性与边界 | 已确认使用 `pub` 表达公开边界的方向 | 函数、方法、回调、字段的完整规则未定 | [U06](decisions.md#4-未定与明确延期) |
| 编译器与 LSP | 已确认 LSP 目标；共享语义前端、Go 启动是暂定实现路线 | 查询层、源码映射、协议服务 | [工具链](compiler-and-tooling.md) |
| 自举 | 已确认未来自举目标；逐步迁移方案暂定 | seed、阶段产物及回归验证 | [工具链](compiler-and-tooling.md) |

这里的“接口显式继承”沿用此前讨论的意图，具体记为 **`impl Trait for Type`：类型显式实现接口**。`trait Child: Parent` 是 trait 之间的继承约束，属于独立的待设计专题，不能从显式实现规则直接推导。

### 2.1 三项目标的符合程度

| 目标 | 当前设计能提供的改善 | 尚未闭合的条件 |
| --- | --- | --- |
| 改善安全性 | 区分非空共享句柄、读借用、写借用和裸指针；Option 表达缺失；unsafe/declare 划定外部契约；枚举与 match 限制非法状态和遗漏分支 | 必须细化借用作用域、容器与闭包中的别名、trait 视图权限，以及来自 Go 的可变共享状态；外部声明仍是信任边界 |
| 显式实现与可读性 | impl 元数据、唯一实现检查、限定调用、稳定视图；同名不同实现不混淆；不因结构碰巧一致而取得 trait 能力 | 源级接收者权限与早期 Go `*T` 示例需要接合；父 trait 继承尚未设计；显式声明不证明方法业务逻辑正确 |
| 与 Go 包互操作 | 生成普通 Go；原生视图、声明/包装视图及明确 unsafe 操作；真实签名检查；本地 wrapper 与 marker；Go 可调用生成的普通 API | 声明版本与构建条件解析、递归类型与函数值桥接、可复现的普通模块与声明发布；Go 调用方不受 goxide 源级权限检查 |

结论是三项目标均有可实现的设计路径，显式实现部分相对完整；目前不是一个已闭合、已实现或经过安全性证明的语言规范。首版要验证的是语言错误被拒绝，以及生成代码保留合法程序的语义；具体风险 warning 按 D20 独立长期验收，不承诺自动获得 Rust 的完整所有权或全程序无数据竞争保证。

当前默认诊断策略见 [RFC 0001](../rfcs/research/0001-scoped-borrow-views.md)和[业务场景](../rfcs/research/0001-usage-and-tradeoffs.md)：前两轮保守拒绝已被用户否决，采用有具体风险依据的 warning；普通 Go API 缺少摘要不要求 unsafe。

## 3. 当前必须保留的约束

### 3.1 显式实现与稳定视图

显式实现是 goxide 语义事实；方法集相符不会自动建立 impl。当前编译器仍有两种实现模式；[RFC 0003](../rfcs/0003-declare-impl.md) 已决定只保留普通有体 impl，原生采用实现待删：

- 已决定移除的 `declare impl Trait for T;` 没有方法体，当前实现检查并采用 `T` 已有的业务方法，在 impl 包生成逐方法转发的 wrapper 与 marker。包级工厂返回 wrapper 视图；若接收者是指针，保留原目标指针而不保留原始接口动态类型。
- `impl Trait for T { ... }` 提供完整实现，由编译器生成 wrapper，方法体与 marker 直接定义在 wrapper 上；原始类型的同名方法不会覆盖该实现。

两种形式的行为来源分别为原生方法与用户方法体，运行时载体统一为 wrapper。[RFC 0019](../rfcs/0019-uniform-trait-wrappers.md) 已确认取代原始类型 marker 注入；具体命名和权限语法仍待定。

以上是 trait 实现形式。固有成员另用 `impl T { ... }`，关联函数降低为包函数，实例成员按接收者规则生成方法。impl 中 `Self` 表示源目标类型，`self` 只在实例成员中绑定接收者。直接书写的 Go `func F(...)` / `func (r T) M(...)` 保留原生声明和行为，不自动变成 impl 成员或 trait wrapper；例子见 [RFC 0020](../rfcs/0020-associated-functions.md)。

可形成运行时视图的 trait，其生成的 Go 接口包含业务方法和对应 marker。marker 表达显式采用，但不是不可伪造的安全证明：Go 代码可以自行实现或通过嵌入获得它。goxide 的精确 impl 判定仍由可见元数据与类型检查负责，读取到 Go 方法集中的 marker 不会自动建立 impl。Default 的无接收者构造和 Clone 的 Self 结果使用静态实现选择与普通 Go 函数，不强行改成返回 any 的接口方法；见[静态 trait 操作](macros.md#55-静态-trait-操作与-go-运行时接口)。

实现的行为来源由声明固定。后端通过元数据选择固定包级适配函数；类型与 impl 同包且满足 Go 方法接收者规则时可以生成 `t.AsXXX()`，其他类型生成 `adapters.ForT(t).AsXXX()`，便利 helper 只转发同一函数。新增 impl 不改变旧工厂、已发布 wrapper 归属或业务行为。

使用处先完成 goxide 类型检查与实现选择，再将必要的转换记录到 IR。后端不能通过输出 Go 源码后试编译来猜测使用者的意图。

### 3.2 声明来源与 Go 适配范围

以下是已决定移除、当前实现待删的原生采用入口；目标规则见 [RFC 0003](../rfcs/0003-declare-impl.md)。旧实现允许用户在 C 包自己的普通 `.gox` 文件中写：

```goxide
declare impl AorB::XXX for BBB::Type;
```

**声明的定义来源是 C，不是 AorB 或 BBB。** goxide 根据 C 的声明和显式导入的 impl 元数据形成可见实现集合，不扫描全磁盘或其他项目来收集声明，也不把它登记成 BBB 对所有项目公开的实现。AorB、BBB 的原源码无需修改，普通 Go 工具也不会读取 C 的 `.gox` 声明。

旧采用入口允许普通 `.gox` 对外部 trait 与外部 Go 类型声明无体关系；当前实现仍合并相容声明并检查真实 Go 方法集、签名与调用契约。这套采用与合并机制待删，不保留为普通有体 impl 的归属例外；新目标合同见 [RFC 0003](../rfcs/0003-declare-impl.md)。

普通 `declare` 也用于补充 Go API 的 goxide 类型视图，不要求写作 unsafe declare。函数契约可以放在调用方 `.gox`、包内 `.d.gox` 或明确依赖的外置声明包中；具体规则见[原生 API 与裸指针](foreign-api.md)。函数视图声明与生成 wrapper/marker 的 impl 声明分别检查，声明可信不等于编译器已证明外部实现行为。

`T` 与 `*T` 保持精确的接收者身份，不为满足 impl 自动取地址。外部 Go 类型的方法不能声明在调用方包中；当前生成的是 C 包自己定义的 wrapper，marker 与业务方法属于该 wrapper，工厂及 facade 也属于 C。已有 Go wrapper 可以按自身真实类型采用，不因此与原始结构体成为同一类型。

编译器和 LSP 必须读取同一份 impl 与方法解析结果。仅让补全“看起来有两个 impl”，而编译器不知道采用哪些方法，不算完成互操作。

### 3.3 普通 Go 构建与发布

发布采用普通 Go 模块携带 `.go`、`.d.gox` 与可选 `.gox` 的完整 dist；Go 只编译 `.go`，goxide 额外加载声明或源码。impl 包生成自己的 wrapper、marker、工厂与便利 helper，不要求给原生依赖或 GOROOT 增加任何方法，Go 下游可按正常包依赖构建。

此前隔离源码副本/replace/overlay 给原类型追加 marker 的方案已经被替代；保留的夹具只记录历史机制。具体发布对照见 [RFC 0001](../rfcs/0001-package-loading.md)，统一载体及 helper 见 [RFC 0019](../rfcs/0019-uniform-trait-wrappers.md)。

编译器仍要锁定实际模块图、构建条件及声明集合，识别同一 .gox 与其生成 .go，防止重复纳入业务定义。常规 Go 包循环、私有访问或签名不相容仍须诊断，wrapper 不自动消除真实依赖问题。

### 3.4 Go 边界与非法值

原生 Go 可以产生 goxide 不允许的状态。例如导出的 `Result[T, E]` 若以接口承载，`var values [6]Result[T, E]` 会产生六个 Go 零值，不能宣称它们自动成为有效的 `Ok` 或 `Err`。

已确认 Go 调用方能够制造非法值；以下具体处理规则为当前暂定边界方案（T03）：

- goxide 内部检查初始化与合法变体；公开边界按签名验证输入。
- 参数要求 `Result[T, E]` 时，Go 传入 `nil` 应被拒绝，不能悄悄改成 `Option[Result[T, E]]`。
- 保持原签名的入口可以在非法输入时 panic；返回错误的显式适配入口另行设计。
- 只有目标类型明确允许缺失时，才讨论把 Go `nil` 映射为 `None`；typed nil 和未知动态类型不能简单等同于 `None`。
- 回调、方法、返回值及共享可变对象也会跨边界；只检查一处构造函数并不能覆盖所有输入。

枚举首版候选采用 interface 加值型 case；Option 的 None 以 nil 接口承载，Some 始终有明确的 case。精确白名单、嵌套 `Some(None)` 与 `Some(nil)`、初始化和边界规则见[变体枚举与 match](enums-and-match.md)。具体布局仍是草案，不作为已经冻结的 ABI；值型 case 示例目前只覆盖值匹配。用户要求按模式区分值复制、只读借用和可变借用，ABI 必须满足 [RFC 0009](../rfcs/0009-match.md) 的原存储约束。

### 3.5 源级权限与 Go 表示必须分开

`&T`、`&mut T`、`Arc[T]` 即使都降低为 Go `*T`，也不是相同的源级权限。原生视图的 `Ptr[T]` 更不能因为具有同一个后端地址就自动获得 safe trait 视图。前端和 IR 必须保留权限、借用来源及有效范围，并在 trait 适配、泛型实例化、接口装箱、返回和存储时继续检查。

早期 trait 与关联类型示例中的 `*T` 用于说明 Go 接收者身份和方法集；在引用体系接合完成前，不能把这些写法当成新的通用 safe goxide 指针类型。安全源代码应通过已定义的引用/句柄视图表达权限；这些视图如何参与 impl 接收者声明与方法接收权限仍待细化，不能仅按 Go 类型归一后丢弃区别。

最低约束如下：

- 锁内引用经 trait、闭包或容器离开回调本身不报；未来仅具体协议违约可给 W01，但默认允许；来源保留用于诊断，不提供解锁后受保护保证。
- slice、map、Arc 和含引用字段的值复制保留别名来源，不自动取得可达对象图的独占权。`Lock.new(value)` 也不能凭浅复制消除外部别名。
- 借用相关存储上的读写、冲突重入和异步使用都要进入检查范围。未定义完整容器权限时不能将早期 `read(p []byte)` 的 Go 降低示例当成写权限已被证明。
- 对外部 enum 或非空状态的一次检查只验证当次读取的值；共享字段后续可能被 Go 改写。需要快照、重新校验、同步或明确外部契约，不能无限延长首次检查的结论。

具体 error/warning/无依据不报的边界见 RFC；warning 用风险链及不应告警的对照验收。Go 编译成功不等于静态安全，Go race 仅报告实际覆盖的执行路径。

## 4. 编译与工具链分层

建议的逻辑流程如下，具体数据结构待实现专题补齐：

```text
goxide 源文件（含 impl / 宏）+ 实际 Go 包类型信息 + 显式声明依赖
                  |
       名字解析、宏展开、goxide 类型检查
                  |
       已解析语义 IR（含 impl 与视图转换）
                  |
      标准 Go AST / 源码 + 声明元数据
                  |
       普通 Go 包、wrapper 与适配工厂
                  |
              Go 工具链

LSP 使用同一语义数据库，保留 goxide 与 Go 的源位置及生成代码映射。
```

宏先完成声明式展开，再按固定类型声明信息执行内建 derive，之后进行正常类型与权限检查；详细阶段草案见[卫生宏](macros.md)。名字身份和源码映射贯穿展开过程，不能先做字符串替换再补卫生。进入 Go 降低前必须完成 goxide 独有语义的判定。

Go 的包加载和类型检查基础设施可用于获取原生 Go 符号，但不能替代 goxide 的 trait、枚举或宏语义检查。`go/packages` 提供包和类型信息加载；`go/types` 提供 Go 类型对象与方法集等操作。[go/packages](https://pkg.go.dev/golang.org/x/tools/go/packages)、[go/types](https://pkg.go.dev/go/types)

## 5. 文档拆分与完成顺序

当前文档覆盖显式实现、Go 原生适配、关联类型、锁回调、变体枚举与 match、卫生声明宏和基础派生，以及 safe Go std、泛型 slice 与额外语法工具的首版设计。Rust 仅作为宏机制与派生协议的参考，safe 标准库以 Go std 的实际契约为准。引用权限的完整检查规则、静态 trait 操作的完整语法与泛型降低、父 trait 继承、包发布协议和过程宏仍需后续设计，不通过已有专题名称将这些缺口视为已完成。

具体顺序、状态及完成条件统一维护在 [roadmap](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/ROADMAP.md)，本篇不重复另一份阶段清单。trait 与引用 / unsafe 子集须共同确定接收者权限；静态 trait 操作是基础派生的前提。LSP 和卫生所需的位置与符号身份从前端第一步保留。quote / syn 作为独立暂定分支，不承诺与首个可用版本同时交付；用户过程宏宿主明确延期。

## 6. 本轮交付的验证边界

本轮仅编写设计文档。详细文档列出的验收项是未来实现应满足的条件，不代表相关工具已经实现。文档中完整 Go 示例可独立检查；`.gox` 示例暂时只能进行设计一致性审阅。

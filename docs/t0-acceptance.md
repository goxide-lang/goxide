# T0 子能力验收矩阵

本轮核对基准：`1300b77e1ca72ae9365f1ce47d5d24375d6c5f92`。以下先给有限交付标准，再保留历次逐项证据；历史阶段的“未做”若已有后续增量，以最新增量为准。具体类型、Go 宿主语法上的有限 T0 子集与整项 T0/M1–M8 完成是不同结论，不能混用。

设计优先级以[决策登记](design/decisions.md) D21 为准：宏、enum/match、只读/可写分离是用户必需方向；D22 约束 Option/Result 专用 `?` 与唯一显式一步 From，D23 要求 serde 核心与格式分层。优先级不意味着全部语法或 ABI 已冻结。后续工作按 [ROADMAP](../ROADMAP.md) 依赖推进，不以完整 trait 或长期风险分析阻塞已有子集。

## 有限 T0 的交付标准

本轮验收范围固定为已有语言子集的实际应用闭环，不以整个未来语言为完成条件，也不因指定子集通过而取消已确认但尚未实现的方向。

| 必须验收的范围 | 可交付标准 | 不由此承诺 |
| --- | --- | --- |
| 声明宏与日志包 | 同模块导入、定义点卫生、私有边界、禁用时不求值、确定展开与预算；明确拒绝未支持模板 | 任意 fragment、外部预编译宏、过程宏、完整 tracing |
| serde JSON 与 enum/match | 真实 JSON 解码、名义 enum、已支持的 match、派生和失败输入；生成产物独立运行 | YAML、借用解码、任意对象图/泛型载荷、完整表达式 match |
| Option/Result、`?` 与 From | 支持上下文的一次求值、传播/清理、错误链；赋值兼容优先与唯一显式一步转换 | 通用 Try/Residual、转换链或动态猜测 |
| 基础权限与普通 Go 互操作 | 已支持只读投影和可写访问在多文件使用时不擦除；普通 Go API 能真实调用；不支持的新引用边界明确诊断 | 独占、全局不可变、完整引用桥接或长期借用风险分析 |
| CLI 与构建 | 隔离安装、仓库外使用、Go 模块和已支持构建条件、一致输入可复现输出；真实错误位置或诚实 unmapped；失败清理与输出保护 | 包管理新体系、所有交叉编译目标、全部测试文件语法或 LSP |

验收必须包含新的业务源例子、真实安装 CLI、原生 Go 独立构建和生成模块 `-race`；既有测试只作为回归，文档/机制夹具通过不替代这些应用证据。生成模块可独立构建指无需再次运行 hgo，仍须可解析其 go.mod 中明确的 runtime 依赖，不等于无依赖单文件。

**整项已确认方向的剩余项不能隐藏：**D14 的内建 `print!`/`println!` 与 Default 已有后文限定实现；D21 的数值/string 常量 enum raw/未知值语言入口仍未实现，同值别名问题见 U19。一般格式/静态 trait 协议仍不等于完成。完整 trait、LSP、YAML、风险分析不加入本次有限交付的必需清单。

## 如何读状态与证据

- **完整（具体子能力）**：仅指该行写明的窄契约有正反例或运行断言，不代表所在特性完整。
- **有限**：已有运行路径，但重要语法、边界或组合未实现。
- **未做**：没有相应前端能力或验收；保留 Go 原有语法不算新能力已实现。
- **待验收**：本轮正在接入，尚不能用其他特性各自通过推断组合通过。

“用户必需”表示已确认方向或本轮应用闭环要求；“可后置”不等于取消要求。所有 `Test…Race` 须看测试体：本文引用的独立接受测试 helper 会对生成模块显式执行 `go test -race`；宿主带 race 本身不会使普通子进程自动带 race。下表首先列宿主测试；括号内生成模块测试是嵌在该文件字符串内的真实测试，不应重复算为宿主测试。

## 能力矩阵（基础范围与后续增量合读）

| 能力与实际验收契约 | 状态 | 必需/可后置与下一步 | 真实证据 |
| --- | --- | --- | --- |
| 本文件声明宏：有限 expr/ident/tt、单层重复、嵌套调用、规则顺序 | 有限 | 用户必需 D14/D21；同 module 跨包已接通（见最新增量）；维持应用组合和明确拒绝，任意片段后置 | [macros_test.go](../internal/macros/macros_test.go)：`TestRepetitionNestedInvocation`、`TestIdentAndTT`、`TestErrors`；[独立接受测试](../tests/acceptance/macros_test.go)：`TestMacroAcceptanceRace`、`TestMacroAndCloneExplicitRejections` |
| 已支持模板的局部身份、定义点函数绑定、原控制流和按模板次数求值 | 完整（具体子能力） | 用户必需；不能推广到未支持语法的完整卫生 | [宏内部测试](../internal/macros/macros_test.go)：`TestHygieneEvaluationAndHelperShadow`、`TestLogAndReturnDefer`、`TestTemplateRepetitionEvaluationCount`、`TestNestedLocalScopes`；`TestMacroAcceptanceRace` 内 `TestHygieneAndEvaluation`、`TestDisabledLogArgumentsAreEager`。禁用 slog 仍求值，非惰性 tracing |
| 固定内建 Option/Result 类型的宏模板及显式闭包传播边界 | 有限 | 用户必需组合；捕获类型参数、用户 enum 构造和一般泛型后置 | [sums_test.go](../internal/macros/sums_test.go)：`TestBuiltinSumTemplateDefinitionBindings`、`TestSumHelperDoesNotMovePropagation`、`TestUnsupportedSumTemplateTypes`；[传播接受测试](../tests/acceptance/propagate_test.go)：`TestPropagationMacroClosureRace`（`TestExplicitMacroClosureOwnsReturn`） |
| Debug/Clone：具体标量、无指针容器、嵌套本地类型 | 有限 | 用户已授权基础派生；通用 trait/泛型/环和共享拓扑复制后置 | [generate_test.go](../internal/deriveextras/generate_test.go)：`TestGeneratedDerivesRun`、`TestRejectImplicitProtocols`；[宏接受测试](../tests/acceptance/macros_test.go)：`TestCloneAndErrorAcceptanceRace`（`TestCloneNestedAndAliasPolicy`）、`TestMacroAndCloneExplicitRejections`。Clone 保留 nil/空区别，不保留别名拓扑/容量 |
| 本地非泛型 enum：unit、单标量/有限struct和容器/无环enum payload；名义身份 | 有限 | 用户必需 D12/D21；候选 interface+值 case ABI 尚未冻结，复杂payload继续分期；跨包身份已有限接通，见末尾增量和剩余完成标准 | [enums_test.go](../tests/acceptance/enums_test.go)：`TestEnumNominalIdentity`、`TestEnumExplicitRejections`；[lower_test.go](../internal/enums/lower_test.go)：`TestPublicEnumIsNotSilentlyLocal` |
| 语句 match：一次求值、guard 次序、绑定、穷尽/不可达、return/defer/循环控制流 | 完整（具体子能力） | 用户必需；嵌套模式和完整表达式语法后置 | [enums_test.go](../tests/acceptance/enums_test.go)：`TestEnumControlFlowRace`（`TestOrderAndSingleEvaluation`、`TestReturnDeferLoop`、`TestNested`）、`TestEnumExplicitRejections` |
| 表达式 match：整个显式类型局部初始化，或单个非具名 Go 可解析返回类型的整个 return | 有限 | 用户必需方向；下一步按应用需要扩展，尚不支持 `:=`/参数/运算符内嵌等 | [match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestMatchExpressionBusinessRace`（`TestScopeEffectsAndGuards`、`TestReturnClosureAndHygiene`）、`TestMatchExpressionUnsupportedContexts`、`TestBuiltinMatchExpressionRace` |
| 使用点拒绝非法 enum Go 值：nil/typed-nil/case 指针/伪造值先于 wildcard 副作用；serde 编码返回错误 | 完整（具体子能力） | 用户必需边界说明；全入口/初始化/任意对象图校验可后置，不能称 Go 调用者无法造非法值 | [enums_test.go](../tests/acceptance/enums_test.go)：`TestEnumInvalidBoundaryRace`；[enum_serde_test.go](../tests/acceptance/enum_serde_test.go)：`TestEnumSerdeBusinessRace`（`TestEnumInvalidGoBoundaryReturnsError`）；[match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestBuiltinMatchExpressionRace`（`TestInvalidBuiltinBeforeEffect`） |
| 强化既有数值/string 常量枚举的 raw/未知值语义 | 未做 | D21 用户重要场景；建议下一批小闭环，不能拿当前带载荷 enum 代替 | 当前无对应前端验收。上述 `TestEnum…` 验证变体 enum，不验证 const enum 的 raw 转换/未知值 |
| serde 核心 Serializer/Deserializer、Visitor/Access 与结构化错误/cause；格式独立 | 有限 | 用户必需 D23；完整协议扩展、借用解码和用户实现协议后续逐项验收 | [core_test.go](../serde/core_test.go)：`TestErrorPathAndCause`、`TestVisitorDefaultsAndCallbackErrors`、`TestScalarReaders`、`TestScalarProtocolFailures`；[json_test.go](../serdejson/json_test.go)：`TestCallbacksAndStates`、`TestIgnoredProtocolErrorsRemainErrors` |
| 具体 struct JSON：标量、本地嵌套、数组/slice/string-key map、原生 Go `*T` 可空；rename/default/skip/deny_unknown_fields | 有限 | 用户必需首版闭环；default 仅 Go 零值，`*T` 非新引用/Option 桥接 | [serde_test.go](../tests/acceptance/serde_test.go)：`TestSerdeBlackBoxRace`（`TestBusinessRoundtrip`、`TestSkippedFieldAndWhitespace`、`TestNumericBoundaries`、`TestNestedErrorPath`、`TestCycleAndDepthBound`）、`TestUnsupportedSerdeIsDiagnosed`；[compile_test.go](../internal/frontend/compile_test.go)：`TestAttributesCannotBeSilentlyIgnored` |
| JSON 严格数字/Unicode/重复键、无效值和深度限制 | 完整（具体子能力） | 用户必需可信输入路径；不推导支持完整 Rust serde/全部格式 | [json_test.go](../serdejson/json_test.go)：`TestScalarsAndStrictNumbers`、`TestStrictSyntaxEvenSkipped`；`TestSerdeBlackBoxRace` 内 `TestInputRejections`、`TestEncodingRejectsInvalidValues`、`TestZeroAndTinyNumbers` |
| 真实 enum serde：unit 字符串/newtype 单键外标记对象，struct/container 嵌套，Variant 路径 | 有限 | 用户必需既定顺序；变体属性、复杂/泛型/递归 payload、内建 Option/Result serde 后置 | [enum_serde_test.go](../tests/acceptance/enum_serde_test.go)：`TestEnumSerdeBusinessRace`（`TestEnumWireAndRoundTrip`、`TestEnumContainers`、`TestEnumInvalidInput`、`TestEnumVariantErrorPath`）、`TestEnumSerdeIndependentDirectionsRace`、`TestEnumSerdeUnsupportedAttributes` |
| Error/From：消息、显式 source、唯一静态 error 字段的 FromType 与 Unwrap | 有限 | 用户必需已授权；enum Error 已有限支持（见增量）；任意具体错误转换、通用 From trait 后置 | [宏接受测试](../tests/acceptance/macros_test.go)：`TestCloneAndErrorAcceptanceRace`（`TestErrorChainsAndNil`）、`TestMacroAndCloneExplicitRejections`；[前端测试](../internal/frontend/compile_test.go)：`TestErrorDeriveFieldMethodCollisions` |
| 内建 Option[T]/Result[T,E] 显式实例化与名义识别：Option nil=None；Result nil 非法；保留 typed-nil payload | 有限 | 用户必需 D12/D22；不是用户泛型声明或任意 interface 分类 | [builtins_test.go](../internal/enums/builtins_test.go)：`TestBuiltinSums`、`TestBuiltinRejects`、`TestBuiltinNominalAndInvalidCases`；[match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestBuiltinMatchExpressionRace`（`TestBuiltinDistinctions`）；[propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationControlFlowRace`（`TestNativePartialAndTypedNil`、`TestInvalidResultIsNotBusinessFailure`） |
| `?` 在支持上下文保持一次求值、调用顺序、短路、最近闭包和 defer；成功类型可变化 | 有限 | 用户必需 D22；循环/switch/索引/复合字面量/多 RHS 等先明确拒绝 | [propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationControlFlowRace`（`TestDifferentSuccessTypesAndOrder`、`TestCalleeBeforeArguments`、`TestUnselectedBranchStaysUnevaluated`、`TestShortCircuit`、`TestNearestClosureAndDefer`）、`TestPropagationExplicitRejections` |
| `?` 错误先赋值兼容，再唯一显式单步转换；拒绝缺失/歧义/两跳 | 完整（具体子能力） | 用户必需 D22；前端仅注册上述 Error 派生，不是一般转换求解器 | [propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationFromRace`（`TestOneStepFromAndAssignableFirst`）；[lower_test.go](../internal/propagate/lower_test.go)：`TestFromAndRejections`；[propagation_test.go](../internal/frontend/propagation_test.go)：`TestPropagationAliasIdentity`、`TestPropagationMatchCompositionRace` |
| 标量 `&T`/`mut& T` 参数、单返回、局部、复制/重绑定、取址/解引用、显式收窄 | 有限 | 用户必需 D08/D21；当前权限检查不是独占或无竞争保证 | [refs_test.go](../tests/acceptance/refs_test.go)：`TestReferenceGeneratedModuleRace`（`TestReferenceLifetimeIdentity`）、`TestReferenceLegacyGoUntouched`；[lower_test.go](../internal/refs/lower_test.go)：`TestScalarEndToEnd`、`TestReadAddress`、`TestNestedReborrowCannotUpgrade` |
| 标量只读写入及权限擦除拒绝：range、原生指针/接口/容器/泛型及不支持的引用函数签名 | 完整（具体子能力） | 用户必需基础类型规则；不以此宣称长期借用风险分析已做 | [refs_test.go](../tests/acceptance/refs_test.go)：`TestReferenceConsumerRejections`、`TestReferenceFrontendRejectsEscapes`；[lower_test.go](../internal/refs/lower_test.go)：`TestReject`、`TestExtraReject` |
| 具体 struct/array/slice/map/pointer 只读投影；独立值副本可写，共享字段保留只读资格 | 有限，所测基础规则完整 | 用户必需 D08/D21；符号摘要加局部点事实，无长期别名分析；直线及双分支 fresh 重绑定可解除局部资格，捕获/取址/循环和字段替换仍保守 | [composite_permissions_test.go](../tests/acceptance/composite_permissions_test.go)：`TestCompositePermissionValuesAndAliasesRace`、`TestCompositePermissionConsumerRejections`、`TestCompositePermissionGenericCheckerBoundary`；[composite_test.go](../internal/refs/composite_test.go)：`TestCompositeRecursiveQualificationConverges`、`TestCompositeHiddenBoundaryRejections` |
| 引用容器、引用函数值/回调、原生桥接、Arc/AsPtr | 未做 | 完整共享包装/桥接可分期；接口/函数/泛型/重取引用不得擦除只读 | [refs_test.go](../tests/acceptance/refs_test.go)：`TestReferenceConsumerRejections`；复合投影接受测试另验证共享路径不得通过这些边界逃逸 |
| Default / print / println 内建协议 | 有限，见本轮增量 | D14 已接通具体类型 helper 和固定格式子集；通用 trait/格式生态未完成 | [独立新业务验收](../tests/acceptance/print_default_test.go)及下文限定范围 |
| 外部预编译宏、用户过程宏、完整泛型 derive、YAML/格式 Value、完整 LSP | 未做/明确后置 | 不加入当前有限交付必要清单，不能一并标完成 | 泛型 derive 的明确拒绝见 `TestDiagnosticLocationsAndUnsupported` 和 `TestMacroAndCloneExplicitRejections` |

## 组合矩阵

| 组合 | 状态与明确边界 | 真实证据 |
| --- | --- | --- |
| 宏 + enum serde + 表达式 match | 完整（所测路径）；同文件声明、JSON 往返后 match，与宏求值/guard 路径同模块验收，不代表任意位置组合 | [match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestMatchExpressionBusinessRace`，其中 `TestSerdeMacroMatchIntegration`、`TestScopeEffectsAndGuards` |
| match arm 内 `?` + Error From 遮蔽保护 | 完整（所测路径）；成功/失败/未选中 arm 均检查，局部同名 From 不截获派生转换 | [propagation_test.go](../internal/frontend/propagation_test.go)：`TestPropagationMatchCompositionRace`，实际生成程序执行 `go run -race` |
| 宏显式闭包 + Option/Result + `?` | 完整（所测路径）；闭包是显式传播边界，宏不插入隐藏闭包 | [propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationMacroClosureRace`；[sums_test.go](../internal/macros/sums_test.go)：`TestSumHelperDoesNotMovePropagation` |
| 新标量引用 + `?` + Error/Clone derive + 宏 | 有限，所列正反例已通过：引用复制/读捕获、宏解引用后调用普通标量函数、一次求值、Error From、defer/最近闭包、Option 普通 payload 与 Clone 同文件可用；引用 payload、泛型携带引用、宏定义点引用签名函数别名仍明确拒绝 | [t0_combination_test.go](../tests/acceptance/t0_combination_test.go)：`TestT0ReferencePropagationDeriveMacroRace`（`TestIdentityEvaluationAndCleanup`、`TestClosureBoundaryAndOption`、`TestCloneBesideReferences`、`TestDirectDeferredReferenceArgument`）、`TestT0ReferenceSumAndGenericBoundaries`、`TestT0ReferenceGenericInstantiationBoundary`；[reference_composition_test.go](../internal/frontend/reference_composition_test.go)：`TestReferencePropagationAndDerive`、`TestReferenceCompositionRejections` |
| 配置应用：真实 enum/match + serde JSON + Error/From + Result/`?` + JSON slog | 完整（所测应用路径）；应用读取 JSON、校验业务字段、两次 `?` 显式转换、输出一个结构化事件和可检查错误链；该应用本身不含宏、新引用或 Option | [config_pipeline.hgo](../examples/prototype/config_pipeline.hgo)；[config_pipeline_test.go](../tests/acceptance/config_pipeline_test.go)：`TestConfigPipelineApplicationRace`，生成模块 `TestAcceptedConfig`、`TestRejectedConfig`（bad-json/bad-enum-payload/zero-workers/too-many-workers/missing-name）。运行通过；宏/引用/Option 组合由上一行单独验收；该应用使用含本地 enum 字段的具体 struct Result payload，不能推导任意对象图投影可用 |

有限值投影另由 [sum_projection_test.go](../tests/acceptance/sum_projection_test.go) 的 `TestSumValueProjectionRace` 验证分组声明、别名、重复字段及嵌套 Option 的非法值在 guard/传播副作用前被拒绝；`TestSumProjectionUnsupportedPaths` 明确拒绝指针/集合、嵌入、空白字段及递归投影。不是任意动态对象图校验。

## 诊断与 source-map 保真范围

状态：**有限，跨阶段来源已接入，未知区域明确 unmapped**。没有完整逐 token 双向映射或 LSP。实现见 [sourcemap](../internal/sourcemap/map.go)、[前端来源](../internal/frontend/source_origins.go)；独立证据见 [source_mapping_test.go](../tests/acceptance/source_mapping_test.go)。

| 产物 | 已验证契约 | 精确边界 |
| --- | --- | --- |
| `diagnostics` | 最终 Go 字节范围；仅凭显式宏 token 来源、已知复制范围、完整 token 序列不变的格式化传递；重复同值 token 不以文本搜索猜来源 | enum/match 显式复制片段与 ? 准备/存活 AST 身份已传递；引用实质改写、重建表达式及生成宏括号仍可能 unmapped。格式化仅按完整顺序流证据传递 |
| `macroTokens` | 来源字节范围、定义和调用范围、完整嵌套展开栈；重复字面量及多调用最终 Go 位置验收 | OutputStart/End 属于宏展开缓冲区；输入阶段仍为 propagate.Prepare output；diagnostics 的宏栈另转回可证明的原始端点，不混用两套坐标 |
| `deriveFields` 和 derive 诊断 | 具体字段类型的连续原始字节范围；生成 helper 的名称、具有正确词法接收者身份的字段 selector 附因果来源 | 未知字段来源为 null；生成 API、同名用户 helper 不冒充用户字段；未知 helper 语法保持 unmapped。旧 `derives.line` 仍为中间输入位置 |
| CLI Go 失败 | 真实 Go stderr 原样保留，再附可证明的原始文件/行/列和宏栈；未知 `main.go` 位置补 generated/unmapped | 只处理当前生成 main.go，不篡改其他文件；旧前端阶段错误明确标注 intermediate coordinates/generated-unmapped，未宣称它们已有原始列映射 |
| `references` / `Expanded` | 权限、元素类型、符号身份保留；Expanded 仍与最终 Go 相同 | references 的位置仍对应引用阶段输入；不能拿 Expanded 解释宏阶段偏移 |

独立验收包含 `TestSourceMappingRepeatedTokensNestedExpansion`、`TestSourceMappingChangedRepeatedTokensStayUnmapped`、`TestSourceMappingDeriveFieldDiagnosticSpan`、`TestSourceMappingDeriveFieldMetadata`、`TestSourceMappingUserHelperNameIsNotGeneratedProvenance`、`TestSourceMappingRealCompilerCLI`、`TestSourceMappingRealCompilerGeneratedHelperStaysUnmapped`；前端另验收 `TestSourceOriginsNestedRealGoError`、`TestSourceOriginsSerializerMethodIsNotField`、`TestTransformedPhaseErrorIsExplicitlyUnmapped`。实际 Go 编译失败是证据，不是拼造一条诊断当作集成验收。

只读缓存示例见 [readonly_cache.hgo](../examples/prototype/readonly_cache.hgo)，`TestReadonlyCacheApplicationRace` 直接读取该文件，对实际生成模块启用 race，检查独立快照和可写别名更新的可见性。

## 下一优先级与可后置项

1. **本轮先闭合组合**：应用运行、失败传播、错误链、原生 Go 部分成功/typed-nil、宏求值、match 分支、标量只读权限与负例均须对生成产物验收；新增应用成功不取消已有拒绝边界。
2. **T0 剩余基础能力**：继续扩展已接入的有限复合投影，推进数值/string 常量 enum 的 raw/未知值行为；补充目前 unmapped 的跨阶段诊断来源。具体语法/ABI 未冻结时保持候选状态，不能由测试代替设计确认。
3. **扩展而非整体完成声明**：表达式上下文、跨包身份和一般泛型逐项排期。完整 trait、外部预编译宏、用户过程宏、泛型 derive、格式 Value/YAML、完整 LSP 和整套共享包装可后置，不能因未做而阻塞已授权的有限闭环。
4. **D20 长期独立**：别名/借用风险静态分析不属于 MVP 前置条件；race 只检查实际执行，不证明静态内存/并发安全。原生 Go 可构造 nil/别名的公开边界仍须如实说明。

## 本轮运行记录

真实工具链使用 Go 1.27，`GOTOOLCHAIN=local GOWORK=off GOPROXY=off`，避免把系统中其他同名 `go` 程序当作编译器。

矩阵定向验收命令：

```sh
go test -race -count=1 ./tests/acceptance ./internal/macros ./internal/frontend ./internal/propagate ./internal/refs ./cmd/hgo
```

上述定向命令实际通过六个包（运行开始时的工作树）；随后新增/修改的组合测试须单独复验，不能自动包含在该记录内。新增组合及配置应用另以 `go test -race -count=1 ./tests/acceptance -run 'TestT0|TestConfigPipeline'` 实际通过（5.964s），生成模块显式启用 race。全套包/宿主顶层测试/子测试与生成模块数量由本轮总验收单独统计，不复制 IMPLEMENTATION 历史轮次数字。`scripts/check_design.py` 仅证明设计文档/Go 夹具的可复现性，不计为新语言特性完成证据。


本轮复合投影/映射增量最终验证：全套 race JSON **14 包、134 宿主顶层测试、234 子测试**通过；独立生成模块 **21 个成功模块、56 测试、41 子测试**显式启用 race，另 1 模块预期 Go 编译拒绝。`go vet`、完整设计检查 13 Go 命令、只读缓存 CLI/转译产物 race、配置应用成功/失败均通过。非作者审查发现的递归收敛、命名结果/range/括号目标逃逸、闭包 var 过拒绝和阶段坐标问题均已修复复验；详细证据与剩余边界见 [当前实现记录](../IMPLEMENTATION.md)。


## 本轮精度与常用模式增量

- 局部直线重绑定、双方 fresh 分支、闭包内本地复制和标量容器 append 复制：
  [refs_flow_test.go](../tests/acceptance/refs_flow_test.go) 的 `TestReadonlyRebindingFlowRace` 显式运行生成模块 race；`TestReadonlyRebindingRetainsAliases` 保留旧别名、单分支、同时赋值、捕获、循环及含指针元素浅复制的拒绝。
- enum/match/? 实质 lowering、嵌套宏、Clone/Debug 同文件的精确重复 token 来源：
  [source_lowering_test.go](../tests/acceptance/source_lowering_test.go) 验证每次原始跨度和 Prepare 改写后的宏栈；真实 go/types 错误分已知原始 token 与生成括号 unmapped。不是任意引用组合完整映射。
- 显式条件日志宏：[conditional_log.hgo](../examples/prototype/conditional_log.hgo) 与 `TestConditionalLogApplicationRace`。禁用时 message/field 零次，启用时顺序各一次；真实 JSON request_id/error、errors.Is/As 链已验收。状态**有限**：普通条件宏，不是 span/tracing runtime；旧 eager slog 宏保持原语义。
- 审查矩阵后的高价值后续依赖：配置 enum 字段的 Debug/Clone 需先定义并实现 enum 派生；enum Error/具体 source From 需有限类型与转换设计；常量 enum raw/未知值仍未做。serde 已有静态 struct/enum JSON 路径继续复验，不把 missing/null 或 wire 语义顺手改动。本轮不展开这些独立新语义。

本轮新增接受测试由实施者编写，fresh 非作者只读审查另行复验；不冒称新增测试作者独立。历史线程上限使本轮不能再开两名 fresh 实施者，实际采用单实施者与一名 fresh 只读审查者。

本轮最终实际回归：**14 包、141 宿主顶层测试、241 子测试**通过全套 race；**23 个成功生成模块、58 测试、41 子测试**显式 race，另 1 模块预期编译拒绝。vet、完整设计检查 13 Go 命令、条件日志 CLI 转译产物 race、配置应用成功/两类失败均通过。详细范围及验收区分见 IMPLEMENTATION 当前增量。

## enum 派生增量与剩余完成标准

| 子能力 | 当前状态与证据 | 剩余完成标准 |
| --- | --- | --- |
| 本地 enum 的普通多字段业务载荷 | 有限：普通 struct 单payload，另支持有限array/slice/map；`TestEnumDerivesApplicationRace` | 不冒称多字段variant；tuple/record源语法须后续扩展设计，格式协议另验收 |
| enum Debug/Clone/serde 组合 | 有限：JSON→真实enum→match/Debug/深复制，`TestEnumDerivesApplicationRace`；拒绝见 `TestEnumDerivesExplicitRejections` | pointer/锁/interface无隐式Clone；enum-bearing容器/递归图待明确策略及真实正负例 |
| enum Error/Unwrap/唯一 From | 有限：显式variant message/source/from与Result传播，独立生成模块race | 具体source From、transparent、泛型不冒称完成；必须先定义静态唯一转换条件 |
| T0跨包身份与Go互操作 | 有限：真实多文件包、Go module、pub enum构造/派生API，`TestPackagesApplication` | 公开metadata驱动的qualified match和跨包Result传播已在下文增量接通，同 module 声明宏已由下文增量接通；公开入口全面校验、文件选择和完整源映射继续验收，不能称跨包能力整体完成 |

后续优先级：先维持 enum 派生应用与失败诊断的真实回归，再处理常量 enum raw/未知值与最小跨包身份/公开入口；完整跨阶段来源继续补有证据的映射。常量例 `type Status string` 的未知 `"future"` 可存在、开放输入需wildcard，已是设计方向；尚未定案的是精确源语法、未知值模式及公开校验入口。后续最小建议：先实现开放raw匹配必须有wildcard的入口，再明确显式闭合校验的拼写，不重开保留raw的既定方向。通用引用桥接/容器/函数值、一般泛型及完整trait分别分期，长期借用分析独立。前面各历史阶段“enum派生未做”的记录由此增量取代，整项T0仍未完成。

本增量最终验收：14包、144宿主顶层/262子测试通过全套race；25个成功生成race模块共71测试/48子测试，另1模块预期名义类型编译拒绝。新增2个生成模块验证13测试/7子例，含值struct内enum非法值先于Clone/Debug/guard副作用拒绝。vet、完整设计检查13Go命令与示例转译产物race通过。两位复用非作者分别编写用例/只读审查，无fresh线程可用；最终无未修复阻断。


## 多文件与跨包入口增量

基准 `3a48c8356c535f5852ee4044e2a973fc8e387848` 后，新增 [packages_test.go](../tests/acceptance/packages_test.go) 与 [真实模块应用](../examples/packages/main.hgo)。它们不依赖文本拼接：独立文件 AST/import scope、同包声明和派生共享、实际 module path、Go 导出数据导入共同构成有限包入口。

| 子能力 | 当前验收 | 尚未完成的契约 |
| --- | --- | --- |
| 多文件同包与原生 Go 文件 | 跨文件 enum/struct/函数/derive；不同文件 import 别名各自解析，重复声明由包检查拒绝 | `.hgo` 测试文件、平台后缀及构建条件；目录引用语法暂不支持，native Go 测试可用 |
| hgo 包调用 hgo/Go 包 | 两个 hgo 包及 native Go 包；生成模块独立 test-race/build，真实成功/失败日志 | qualified enum match、导入 Result 直接 `?` 已由下文增量接通；同 module 声明宏由下文增量接通，导入 selector derive 尚未完成 |
| 公开 ABI 与类型身份 | pub enum 的 New/Validate、serde/Clone/ErrorFrom API；私有符号不可访问，不同包 enum 不可赋值混用 | 正式 metadata/ABI 稳定性、全面 Go 输入检查与跨包语言协议；不能只靠公开Go函数称完整hgo导入 |
| 同包 From | 本地唯一显式 error From；两个文件生成绑定用稳定命名空间去重 | 任意导入错误类型的别名归一与通用转换注册；不由包支持扩大D22到动态猜测 |
| 诊断 | 各原文件分别维护来源；真实跨文件类型错误、私有访问、nominal错误定位 | 未映射生成区域明确保留；不承诺完整双向LSP映射 |

下一完成标准：先保持这些生成包可由原生 Go 工具独立构建，再补 `.hgo` 文件选择/测试和目录引用能力；随后以声明包 metadata 接通消费者的 enum 模式与 Result，而不是生成同名替代类型。跨包仍是 T0 必需方向，以上剩余项没有一概降级为后置。同 module 跨包声明宏已在下文增量接通；外部预编译宏仍需独立依赖元数据。常量 enum 的 raw/未知值既定方向不变，精确拼写的设计不阻塞本轮模块工作。

本阶段最终验收：14 包、154 宿主顶层测试、278 子测试通过全套 race；26 个成功生成模块显式 race，74 测试、48 子测试，另 1 预期名义类型编译拒绝。真实两包 hgo 加 native Go 应用独立构建并执行，模块中 3 个应用测试通过；vet、13 条设计检查命令及文档链接检查通过。最终全套已重跑并包含 grouped type 跨文件载荷校验修复；新边界另经独立复验，完整范围与证据见实现记录。


## qualified enum 与导入 Result 增量

基准 `2cf2b4e3b86aab553f0d502210f2519c38ed1177` 后，本轮实施者共同编写真实业务与内部测试，fresh 非作者只读审查另行定向复验；不称新增测试由独立作者编写。

| 子能力 | 当前验收与证据 | 有限契约和剩余项 |
| --- | --- | --- |
| 直接导入的公开 enum qualified 构造/match | [内部测试](../internal/frontend/imported_test.go) `TestQualifiedEnumsKeepFileAliasesAndValidation`、`TestQualifiedEnumsRejectPrivateAndMixedIdentity`：同文件多alias、跨文件alias、同名异包、局部shadow、非法case先于wildcard | 仅同module已成功编译producer的公开metadata；Go canonical path+声明身份，不猜同名native类型；private enum不发布 |
| 导入具体 Option/Result 直接 `?` | [三包验收](../tests/acceptance/qualified_packages_test.go)及[真实应用](../examples/qualified_packages/main.hgo)：JSON/serde/Clone、qualified expression match、嵌套宏、Result、errors链、typed-nil、独立build/run；生成模块显式race | producer具体实例validator保留嵌套payload检查；缺登记实例、不可命名私有/缺import类型参数明确拒绝；qualified builtin构造/pattern未做 |
| From作用域与绑定 | 同上验收：imported error进入当前包From，局部From同名不截获；重复注册歧义拒绝 | 先assignability，再当前包唯一显式一步From；不自动收集依赖包From，不搜索链 |
| 来源与诚实诊断 | `TestQualifiedMatchBodyDiagnosticOrigin`断言真实consumer arm错误原行；错包身份拒绝和缺实例metadata验收 | typed match临时变量的错包赋值错误仍可能为生成行，明确`generated/unmapped`；不宣称跨包完整映射 |
| ABI范围 | pub enum公开case alias/ctor/validator；builtin公开case/ctor及具体instance validator，private marker保持Go名义身份 | 有限原型，不是稳定导出元数据格式；外部预编译包、一般泛型trait/Try/Residual仍未做；同 module 声明宏见下文 |

下一步继续按应用需要扩展文件选择/目录引用、可证明来源和独立依赖metadata；常量enum raw/未知值方向保持，不扩大为完整T0或长期分析。本轮验证记录见实现记录最新段落。

本增量最终冻结树：全套 race **14 包、163 宿主顶层/281 子测试**；接受测试显式 race 的 **27 个成功生成模块、79 测试/48 子测试**，另 1 预期类型拒绝。新三包模块 5 个 runtime 测试及额外内部 qualified enum 生成模块通过；vet、完整设计检查 13 Go 命令、冻结示例 CLI 转译与独立 build/run 通过。无剩余审查阻断；诚实来源限制和协议边界如上，完整日志路径见实现记录。


## 同 module 源码包声明宏增量

基准 `89185278e593707b943af1fc1dd32c8c22f64c7f` 后，同包跨文件声明宏和跨包公开宏接通；前文阶段的跨包宏拒绝边界由此有限实现替代。

| 子能力 | 当前验收与证据 | 有限契约和剩余项 |
| --- | --- | --- |
| 定义点绑定与导入身份 | [宏测试](../internal/macros/package_test.go)、[前端测试](../internal/frontend/macro_package_test.go)：私有嵌套宏、同名调用方不捕获、native runtime helper、跨文件生成名；[接受测试](../tests/acceptance/macro_packages_test.go)验证同文件同path多alias | 普通显式 import 的 `alias.name!` 原型；不是设计草案的 `import macro`/`::` 最终拼写 |
| 可见性与 runtime 隔离 | 直接私有宏/私有 runtime helper 拒绝；[日志包](../examples/macro_packages/logkit/macros.hgo)通过公开 shim 使用私有实现；未知自由名字不回退调用者 | 宏不能绕过 Go 私有访问；外部预编译 metadata 与用户过程宏未做 |
| 真实业务与原生 Go | [应用](../examples/macro_packages/main.hgo)实际跨包日志、enum/serde/Clone、Result `?`、Error/From；生成模块明确 `go test -race`、独立 build/run，验收禁用零次消息/字段求值与启用顺序 | 不扩展有限 fragment/模板语法，不宣称任意 enum/用户类型可出现在模板中 |
| 来源与资源边界 | 精确类型错误 literal 原跨度、定义和调用栈；同文件及外包在 `?` 预处理后的坐标；包/多文件预算和实际两个包累计超限，依赖循环/宏递归拒绝 | 200,000 输入/输出 token、4,096 次展开及既有深度限制；未知生成区域保持 unmapped |

新增测试由实施及接续集成代理编写，fresh 非作者独立只读审查和定向复验已完成；接续集成另修复同文件同路径多alias的不确定 runtime alias 选择及未用导入。最终验证数量见实现记录，不把宿主 race 当作生成模块 race。


## 整体验收：安装、多文件权限与构建条件

基准 `4e2a9138b9103b5e08b306f585c297aac467fc60` 后，本轮以 [新任务调度示例](../examples/prototype/delivery_job.hgo) 验收，而非重复旧配置/日志应用。复用非作者独立验收者编写 [t0_delivery_test.go](../tests/acceptance/t0_delivery_test.go)；新建空上下文线程请求因平台线程上限未成功，不将复用审查称为空上下文审查。

| 领域 | 本轮状态与实际证据 | 仍有的有限边界 |
| --- | --- | --- |
| 宏 / serde JSON / enum match / OptionResult 与有限 From / 普通 Go API | 新业务同用全部能力：JSON任务、可选名称、批次enum、逐步传播、错误链、条件slog；单文件及拆出只读函数的多文件生成模块实际 race | 保持既有类型/语法子集，不由成功组合推导任意表达式或泛型载荷 |
| 目录引用 | [目录权限测试](../internal/frontend/directory_reference_test.go) 正向跨文件读/写/收窄；拒绝只读→写、native *T擦除、只读返回写入及公开出口；新业务验证权限阶段与serde/传播共存 | 同包私有引用函数；公开新引用签名仍明确拒绝，跨包权限元数据未实现。普通 Go *T API 保持原定位 |
| Go 构建条件 | [选择测试](../internal/frontend/build_selection_test.go)：平台后缀、go:build/+build、显式tags、忽略文件与被排除语法；[CLI测试](../cmd/hgo/directory_test.go)：真实run/test与race/!race选择 | host目标；不同GOOS/GOARCH明确拒绝，.hgo测试源未实现。产物属于选定配置，换条件需重新转译 |
| 安装与独立使用 | 独立验收实际 `go install` 到临时GOBIN、仓库外CLI run、生成模块原生Go build/run及正负业务结果 | runtime依赖由go.mod/replace明确提供；没有下载或发布包的验收 |
| 输出与诊断 | 输入symlink覆盖拒绝、失败目录清理；真实跨包宏错误同时显示定义和调用位置；相同源两次Go/map产物比较 | 未映射来源仍标unmapped；不承诺移动源码根后绝对路径映射不变，也不承诺二进制逐字节复现 |

独立验收实际发现并推动修复两项：`sourcemap.Error.Error()` 丢失已保存的宏调用栈；CLI `test` 的隐含 `race` 条件未同步到前端和导入数据读取。后者由统一 `ModuleOptions{Tags,Race}` 接通，并由原生测试调用race限定.hgo函数验证。模块依赖文件遍历固定顺序，避免map迭代顺序影响处理次序。

**交付结论：**上述有限T0应用子集可作为本地可运行交付；这不是整项T0或M1–M8完成。整项已确认方向最短剩余清单仍为常量enum raw/未知值入口、内建print/println格式入口、Default；跨包新引用接口和更宽语法仍明确列为当前有限边界，不把风险分析/完整trait/LSP混入本次验收。

最终全套race：**14包、186宿主顶层测试、297子测试**通过；新独立业务包含**2个生成race模块、4个业务测试**，并已验收安装、仓库外运行、真实失败诊断和重复生成一致性。vet、完整设计检查13命令和文档检查通过；完整复现与日志见 [实现记录](../IMPLEMENTATION.md)。本轮没有剩余已确认缺陷，未支持能力仍按上表保留，不能称全语言完成。


## print / println 与 Default 增量

本轮遵循 [既有打印和默认值协议](design/macros.md)，不扩完整 fmt/trait 系统。

| T0 必需子能力 | 实际已支持 | 明确后置/不支持 |
| --- | --- | --- |
| `print!` / `println!` | 字面量 `{}` / `{:?}` / 花括号转义；明确标量协议及同包派生Debug；所有参数先按序一次求值，再格式化和写入；空输出/LF、错误和短写panic保留cause | 动态格式、宽度精度、自动Stringer/error/any、导入Debug协议、用户Display impl；go/defer和值位置打印 |
| Default | `DefaultType()`；标量字段、同包已派生字段、空struct、唯一指定unit enum、具体Option None；宏定义点helper登记 | 自动Result/容器/引用/外部类型默认、手写协议注册、完整泛型trait |
| 组合和边界 | 新 [启动配置业务](../examples/prototype/print_default.hgo)：显式默认→JSON覆盖→Clone→宏打印；默认选择第二个unit，不改Go var/make/map缺失或serde(default) | 不将合法显式默认值等同于所有Go零值合法；不把派生Debug当Display |

独立非作者新增 [验收测试](../tests/acceptance/print_default_test.go)，覆盖28项拒绝、宏/Default/Debug局部遮蔽、嵌套打印、所有参数先于formatter、短写/部分输出/原始cause，以及未支持协议的准确错误。实际暴露的嵌套打印遗漏和打印参数中 `?` 被当普通未知函数冻结已修复；基础类型同名遮蔽原已有守卫，本轮补跨文件验证，不冒称新修复。审查者复用已有线程，非新空上下文。

**最短剩余必要项收敛为常量 enum raw/未知值入口。** [表层语法提案与唯一待选项](design/enums-and-match.md#01-常量入口的最小实现提案尚未批准)保留候选状态；已定开放raw方向不重开。本轮不实现依赖同值别名选择的入口，也不把其他表中明确的有限边界伪称全部完成。最终全套验证由本轮实现记录汇总。

本增量最终验证：全套race为**15个有测试包、198宿主顶层、328子测试**，另internal/printing无包内测试而由前端/接受测试覆盖；新独立业务为**1个生成race模块、4个运行测试**，28项拒绝子例和真实CLI通过。vet、完整设计检查13命令及最终文档检查通过，详见实现记录。无已知未修复阻断；常量enum候选仍未批准或实现。

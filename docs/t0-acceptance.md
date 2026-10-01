# T0 子能力验收矩阵

核对基准：`0f48259369d0940d298b76d3fc657dd6d0cb28d9`，并单列本轮新增组合验收。本文依据实现和测试源码，不把设计夹具或 README 的描述当作前端实现证明。当前是具体类型、单文件、Go 宿主语法的有限原型；**整项 T0、M1/M3/M4/M6、核心设计和 LSP 均未完成**。

设计优先级以[决策登记](design/decisions.md) D21 为准：宏、enum/match、只读/可写分离是用户必需方向；D22 约束 Option/Result 专用 `?` 与唯一显式一步 From，D23 要求 serde 核心与格式分层。优先级不意味着全部语法或 ABI 已冻结。后续工作按 [ROADMAP](../ROADMAP.md) 依赖推进，不以完整 trait 或长期风险分析阻塞已有子集。

## 如何读状态与证据

- **完整（具体子能力）**：仅指该行写明的窄契约有正反例或运行断言，不代表所在特性完整。
- **有限**：已有运行路径，但重要语法、边界或组合未实现。
- **未做**：没有相应前端能力或验收；保留 Go 原有语法不算新能力已实现。
- **待验收**：本轮正在接入，尚不能用其他特性各自通过推断组合通过。

“用户必需”表示已确认方向或本轮应用闭环要求；“可后置”不等于取消要求。所有 `Test…Race` 须看测试体：本文引用的独立接受测试 helper 会对生成模块显式执行 `go test -race`；宿主带 race 本身不会使普通子进程自动带 race。下表首先列宿主测试；括号内生成模块测试是嵌在该文件字符串内的真实测试，不应重复算为宿主测试。

## 能力矩阵

| 能力与实际验收契约 | 状态 | 必需/可后置与下一步 | 真实证据 |
| --- | --- | --- | --- |
| 本文件声明宏：有限 expr/ident/tt、单层重复、嵌套调用、规则顺序 | 有限 | 用户必需 D14/D21；先维持应用组合和明确拒绝，跨包/任意片段后置 | [macros_test.go](../internal/macros/macros_test.go)：`TestRepetitionNestedInvocation`、`TestIdentAndTT`、`TestErrors`；[独立接受测试](../tests/acceptance/macros_test.go)：`TestMacroAcceptanceRace`、`TestMacroAndCloneExplicitRejections` |
| 已支持模板的局部身份、定义点函数绑定、原控制流和按模板次数求值 | 完整（具体子能力） | 用户必需；不能推广到未支持语法的完整卫生 | [宏内部测试](../internal/macros/macros_test.go)：`TestHygieneEvaluationAndHelperShadow`、`TestLogAndReturnDefer`、`TestTemplateRepetitionEvaluationCount`、`TestNestedLocalScopes`；`TestMacroAcceptanceRace` 内 `TestHygieneAndEvaluation`、`TestDisabledLogArgumentsAreEager`。禁用 slog 仍求值，非惰性 tracing |
| 固定内建 Option/Result 类型的宏模板及显式闭包传播边界 | 有限 | 用户必需组合；捕获类型参数、用户 enum 构造和一般泛型后置 | [sums_test.go](../internal/macros/sums_test.go)：`TestBuiltinSumTemplateDefinitionBindings`、`TestSumHelperDoesNotMovePropagation`、`TestUnsupportedSumTemplateTypes`；[传播接受测试](../tests/acceptance/propagate_test.go)：`TestPropagationMacroClosureRace`（`TestExplicitMacroClosureOwnsReturn`） |
| Debug/Clone：具体标量、无指针容器、嵌套本地类型 | 有限 | 用户已授权基础派生；通用 trait/泛型/环和共享拓扑复制后置 | [generate_test.go](../internal/deriveextras/generate_test.go)：`TestGeneratedDerivesRun`、`TestRejectImplicitProtocols`；[宏接受测试](../tests/acceptance/macros_test.go)：`TestCloneAndErrorAcceptanceRace`（`TestCloneNestedAndAliasPolicy`）、`TestMacroAndCloneExplicitRejections`。Clone 保留 nil/空区别，不保留别名拓扑/容量 |
| 本地非泛型 enum：unit、单标量或无环本地 enum payload；名义身份 | 有限 | 用户必需 D12/D21；候选 interface+值 case ABI 尚未冻结，复杂 payload/跨包后置 | [enums_test.go](../tests/acceptance/enums_test.go)：`TestEnumNominalIdentity`、`TestEnumExplicitRejections`；[lower_test.go](../internal/enums/lower_test.go)：`TestPublicEnumIsNotSilentlyLocal` |
| 语句 match：一次求值、guard 次序、绑定、穷尽/不可达、return/defer/循环控制流 | 完整（具体子能力） | 用户必需；嵌套模式和完整表达式语法后置 | [enums_test.go](../tests/acceptance/enums_test.go)：`TestEnumControlFlowRace`（`TestOrderAndSingleEvaluation`、`TestReturnDeferLoop`、`TestNested`）、`TestEnumExplicitRejections` |
| 表达式 match：整个显式类型局部初始化，或单个非具名 Go 可解析返回类型的整个 return | 有限 | 用户必需方向；下一步按应用需要扩展，尚不支持 `:=`/参数/运算符内嵌等 | [match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestMatchExpressionBusinessRace`（`TestScopeEffectsAndGuards`、`TestReturnClosureAndHygiene`）、`TestMatchExpressionUnsupportedContexts`、`TestBuiltinMatchExpressionRace` |
| 使用点拒绝非法 enum Go 值：nil/typed-nil/case 指针/伪造值先于 wildcard 副作用；serde 编码返回错误 | 完整（具体子能力） | 用户必需边界说明；全入口/初始化/任意对象图校验可后置，不能称 Go 调用者无法造非法值 | [enums_test.go](../tests/acceptance/enums_test.go)：`TestEnumInvalidBoundaryRace`；[enum_serde_test.go](../tests/acceptance/enum_serde_test.go)：`TestEnumSerdeBusinessRace`（`TestEnumInvalidGoBoundaryReturnsError`）；[match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestBuiltinMatchExpressionRace`（`TestInvalidBuiltinBeforeEffect`） |
| 强化既有数值/string 常量枚举的 raw/未知值语义 | 未做 | D21 用户重要场景；建议下一批小闭环，不能拿当前带载荷 enum 代替 | 当前无对应前端验收。上述 `TestEnum…` 验证变体 enum，不验证 const enum 的 raw 转换/未知值 |
| serde 核心 Serializer/Deserializer、Visitor/Access 与结构化错误/cause；格式独立 | 有限 | 用户必需 D23；完整协议扩展、借用解码和用户实现协议后续逐项验收 | [core_test.go](../serde/core_test.go)：`TestErrorPathAndCause`、`TestVisitorDefaultsAndCallbackErrors`、`TestScalarReaders`、`TestScalarProtocolFailures`；[json_test.go](../serdejson/json_test.go)：`TestCallbacksAndStates`、`TestIgnoredProtocolErrorsRemainErrors` |
| 具体 struct JSON：标量、本地嵌套、数组/slice/string-key map、原生 Go `*T` 可空；rename/default/skip/deny_unknown_fields | 有限 | 用户必需首版闭环；default 仅 Go 零值，`*T` 非新引用/Option 桥接 | [serde_test.go](../tests/acceptance/serde_test.go)：`TestSerdeBlackBoxRace`（`TestBusinessRoundtrip`、`TestSkippedFieldAndWhitespace`、`TestNumericBoundaries`、`TestNestedErrorPath`、`TestCycleAndDepthBound`）、`TestUnsupportedSerdeIsDiagnosed`；[compile_test.go](../internal/frontend/compile_test.go)：`TestAttributesCannotBeSilentlyIgnored` |
| JSON 严格数字/Unicode/重复键、无效值和深度限制 | 完整（具体子能力） | 用户必需可信输入路径；不推导支持完整 Rust serde/全部格式 | [json_test.go](../serdejson/json_test.go)：`TestScalarsAndStrictNumbers`、`TestStrictSyntaxEvenSkipped`；`TestSerdeBlackBoxRace` 内 `TestInputRejections`、`TestEncodingRejectsInvalidValues`、`TestZeroAndTinyNumbers` |
| 真实 enum serde：unit 字符串/newtype 单键外标记对象，struct/container 嵌套，Variant 路径 | 有限 | 用户必需既定顺序；变体属性、复杂/泛型/递归 payload、内建 Option/Result serde 后置 | [enum_serde_test.go](../tests/acceptance/enum_serde_test.go)：`TestEnumSerdeBusinessRace`（`TestEnumWireAndRoundTrip`、`TestEnumContainers`、`TestEnumInvalidInput`、`TestEnumVariantErrorPath`）、`TestEnumSerdeIndependentDirectionsRace`、`TestEnumSerdeUnsupportedAttributes` |
| Error/From：消息、显式 source、唯一静态 error 字段的 FromType 与 Unwrap | 有限 | 用户必需已授权；任意具体错误转换、enum Error、通用 From trait 后置 | [宏接受测试](../tests/acceptance/macros_test.go)：`TestCloneAndErrorAcceptanceRace`（`TestErrorChainsAndNil`）、`TestMacroAndCloneExplicitRejections`；[前端测试](../internal/frontend/compile_test.go)：`TestErrorDeriveFieldMethodCollisions` |
| 内建 Option[T]/Result[T,E] 显式实例化与名义识别：Option nil=None；Result nil 非法；保留 typed-nil payload | 有限 | 用户必需 D12/D22；不是用户泛型声明或任意 interface 分类 | [builtins_test.go](../internal/enums/builtins_test.go)：`TestBuiltinSums`、`TestBuiltinRejects`、`TestBuiltinNominalAndInvalidCases`；[match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestBuiltinMatchExpressionRace`（`TestBuiltinDistinctions`）；[propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationControlFlowRace`（`TestNativePartialAndTypedNil`、`TestInvalidResultIsNotBusinessFailure`） |
| `?` 在支持上下文保持一次求值、调用顺序、短路、最近闭包和 defer；成功类型可变化 | 有限 | 用户必需 D22；循环/switch/索引/复合字面量/多 RHS 等先明确拒绝 | [propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationControlFlowRace`（`TestDifferentSuccessTypesAndOrder`、`TestCalleeBeforeArguments`、`TestUnselectedBranchStaysUnevaluated`、`TestShortCircuit`、`TestNearestClosureAndDefer`）、`TestPropagationExplicitRejections` |
| `?` 错误先赋值兼容，再唯一显式单步转换；拒绝缺失/歧义/两跳 | 完整（具体子能力） | 用户必需 D22；前端仅注册上述 Error 派生，不是一般转换求解器 | [propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationFromRace`（`TestOneStepFromAndAssignableFirst`）；[lower_test.go](../internal/propagate/lower_test.go)：`TestFromAndRejections`；[propagation_test.go](../internal/frontend/propagation_test.go)：`TestPropagationAliasIdentity`、`TestPropagationMatchCompositionRace` |
| 标量 `&T`/`mut& T` 参数、单返回、局部、复制/重绑定、取址/解引用、显式收窄 | 有限 | 用户必需 D08/D21；当前权限检查不是独占或无竞争保证 | [refs_test.go](../tests/acceptance/refs_test.go)：`TestReferenceGeneratedModuleRace`（`TestReferenceLifetimeIdentity`）、`TestReferenceLegacyGoUntouched`；[lower_test.go](../internal/refs/lower_test.go)：`TestScalarEndToEnd`、`TestReadAddress`、`TestNestedReborrowCannotUpgrade` |
| 标量只读写入及权限擦除拒绝：range、原生指针/接口/容器/泛型及不支持的引用函数签名 | 完整（具体子能力） | 用户必需基础类型规则；不以此宣称长期借用风险分析已做 | [refs_test.go](../tests/acceptance/refs_test.go)：`TestReferenceConsumerRejections`、`TestReferenceFrontendRejectsEscapes`；[lower_test.go](../internal/refs/lower_test.go)：`TestReject`、`TestExtraReject` |
| 具体 struct/array/slice/map/pointer 只读投影；独立值副本可写，共享字段保留只读资格 | 有限，所测基础规则完整 | 用户必需 D08/D21；按词法符号固定点传播，无长期别名分析；fresh 重绑定不能消去已有资格 | [composite_permissions_test.go](../tests/acceptance/composite_permissions_test.go)：`TestCompositePermissionValuesAndAliasesRace`、`TestCompositePermissionConsumerRejections`、`TestCompositePermissionGenericCheckerBoundary`；[composite_test.go](../internal/refs/composite_test.go)：`TestCompositeRecursiveQualificationConverges`、`TestCompositeHiddenBoundaryRejections` |
| 引用容器、引用函数值/回调、原生桥接、Arc/AsPtr | 未做 | 完整共享包装/桥接可分期；接口/函数/泛型/重取引用不得擦除只读 | [refs_test.go](../tests/acceptance/refs_test.go)：`TestReferenceConsumerRejections`；复合投影接受测试另验证共享路径不得通过这些边界逃逸 |
| Default/print/println 内建协议、跨包宏、用户过程宏、完整泛型 derive、YAML/格式 Value、完整 LSP | 未做 | Default/print/println 是 D14 方向仍有缺口；其余可后置且分别设计，不能一并标完成 | 当前无对应成功前端验收；泛型 derive 的明确拒绝见 `TestDiagnosticLocationsAndUnsupported` 和 `TestMacroAndCloneExplicitRejections` |

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
| `diagnostics` | 最终 Go 字节范围；仅凭显式宏 token 来源、已知复制范围、完整 token 序列不变的格式化传递；重复同值 token 不以文本搜索猜来源 | changed pass 未提供显式来源则整个结果 unmapped；enum、引用、传播的实质改写会丢弃旧精确来源。格式化仅在去掉括号后的完整顺序流一致时传递非括号 token |
| `macroTokens` | 来源字节范围、定义和调用范围、完整嵌套展开栈；重复字面量及多调用最终 Go 位置验收 | OutputStart/End 属于宏展开缓冲区；输入阶段明确记录为 propagate.Prepare output，该阶段实质改写后不冒充原始来源 |
| `deriveFields` 和 derive 诊断 | 具体字段类型的连续原始字节范围；生成 helper 的名称、具有正确词法接收者身份的字段 selector 附因果来源 | 未知字段来源为 null；生成 API、同名用户 helper 不冒充用户字段；未知 helper 语法保持 unmapped。旧 `derives.line` 仍为中间输入位置 |
| CLI Go 失败 | 真实 Go stderr 原样保留，再附可证明的原始文件/行/列和宏栈；未知 `main.go` 位置补 generated/unmapped | 只处理当前生成 main.go，不篡改其他文件；旧前端阶段错误明确标注 intermediate coordinates/generated-unmapped，未宣称它们已有原始列映射 |
| `references` / `Expanded` | 权限、元素类型、符号身份保留；Expanded 仍与最终 Go 相同 | references 的位置仍对应引用阶段输入；不能拿 Expanded 解释宏阶段偏移 |

独立验收包含 `TestSourceMappingRepeatedTokensNestedExpansion`、`TestSourceMappingChangedRepeatedTokensStayUnmapped`、`TestSourceMappingDeriveFieldDiagnosticSpan`、`TestSourceMappingDeriveFieldMetadata`、`TestSourceMappingUserHelperNameIsNotGeneratedProvenance`、`TestSourceMappingRealCompilerCLI`、`TestSourceMappingRealCompilerGeneratedHelperStaysUnmapped`；前端另验收 `TestSourceOriginsNestedRealGoError`、`TestSourceOriginsSerializerMethodIsNotField`、`TestTransformedPhaseErrorIsExplicitlyUnmapped`。实际 Go 编译失败是证据，不是拼造一条诊断当作集成验收。

只读缓存示例见 [readonly_cache.hgo](../examples/prototype/readonly_cache.hgo)，`TestReadonlyCacheApplicationRace` 直接读取该文件，对实际生成模块启用 race，检查独立快照和可写别名更新的可见性。

## 下一优先级与可后置项

1. **本轮先闭合组合**：应用运行、失败传播、错误链、原生 Go 部分成功/typed-nil、宏求值、match 分支、标量只读权限与负例均须对生成产物验收；新增应用成功不取消已有拒绝边界。
2. **T0 剩余基础能力**：继续扩展已接入的有限复合投影，推进数值/string 常量 enum 的 raw/未知值行为；补充目前 unmapped 的跨阶段诊断来源。具体语法/ABI 未冻结时保持候选状态，不能由测试代替设计确认。
3. **扩展而非整体完成声明**：表达式上下文、跨包身份和一般泛型逐项排期。完整 trait、跨包宏、用户过程宏、泛型 derive、格式 Value/YAML、完整 LSP 和整套共享包装可后置，不能因未做而阻塞已授权的有限闭环。
4. **D20 长期独立**：别名/借用风险静态分析不属于 MVP 前置条件；race 只检查实际执行，不证明静态内存/并发安全。原生 Go 可构造 nil/别名的公开边界仍须如实说明。

## 本轮运行记录

真实工具链使用 Go 1.27，`GOTOOLCHAIN=local GOWORK=off GOPROXY=off`，避免把系统中其他同名 `go` 程序当作编译器。

矩阵定向验收命令：

```sh
go test -race -count=1 ./tests/acceptance ./internal/macros ./internal/frontend ./internal/propagate ./internal/refs ./cmd/hgo
```

上述定向命令实际通过六个包（运行开始时的工作树）；随后新增/修改的组合测试须单独复验，不能自动包含在该记录内。新增组合及配置应用另以 `go test -race -count=1 ./tests/acceptance -run 'TestT0|TestConfigPipeline'` 实际通过（5.964s），生成模块显式启用 race。全套包/宿主顶层测试/子测试与生成模块数量由本轮总验收单独统计，不复制 IMPLEMENTATION 历史轮次数字。`scripts/check_design.py` 仅证明设计文档/Go 夹具的可复现性，不计为新语言特性完成证据。


本轮复合投影/映射增量最终验证：全套 race JSON **14 包、134 宿主顶层测试、234 子测试**通过；独立生成模块 **21 个成功模块、56 测试、41 子测试**显式启用 race，另 1 模块预期 Go 编译拒绝。`go vet`、完整设计检查 13 Go 命令、只读缓存 CLI/转译产物 race、配置应用成功/失败均通过。非作者审查发现的递归收敛、命名结果/range/括号目标逃逸、闭包 var 过拒绝和阶段坐标问题均已修复复验；详细证据与剩余边界见 [当前实现记录](../IMPLEMENTATION.md)。

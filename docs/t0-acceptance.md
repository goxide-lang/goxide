# T0 子能力验收矩阵

> 当前统一入口：[README](../README.md)；最小试用见 [enum_trial](../examples/enum_trial/README.md)。宏 + 独立 enum 历史基线 `55ef3b3020b3984a8ae360ca469fc13fee345bd2`。本文件保留阶段历史，当前范围以末尾交付清单及统一入口为准；2026-10-04 已增加文末 Go Result 适配宏。

本轮实现基线：`fb74ba2380389b39afe92f35ebdb8500fab74adf`；最新四类状态见文末 T0 收口核对。历史核对包含 `cd495372377cac8bf139427e21fc4490e48a0348` 及远端 work 的 RFC/检查器。**D27 已撤回下文历史开放 raw/C enum 成果；当前只支持封闭变体及 serde 外部映射，最新验收见末节。** 以下先给有限交付标准，再保留历次逐项证据；历史阶段的“未做”若已有后续增量，以最新增量为准。具体类型、Go 宿主语法上的有限 T0 子集与整项 T0/M1–M8 完成是不同结论，不能混用。

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

**整项已确认方向的剩余项不能隐藏：**D14 的内建 `print!`/`println!` 与 Default 已有后文限定实现；D27 已撤回开放 raw/C enum；数值/string 仅作为封闭变体 serde 外部值，见末节。一般格式/静态 trait 协议仍不等于完成。完整 trait、LSP、YAML、风险分析不加入本次有限交付的必需清单。

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
| 表达式 match：整个显式类型局部初始化、有限整个 return、完整 arm 嵌套与有限已知参数调用 | 有限 | 用户必需方向；普通赋值、`:= match` 推断、运算符/字面量和其他调用仍未完成；新增证据见末节 | [match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestMatchExpressionBusinessRace`（`TestScopeEffectsAndGuards`、`TestReturnClosureAndHygiene`）、`TestMatchExpressionUnsupportedContexts`、`TestBuiltinMatchExpressionRace` |
| 使用点拒绝非法 enum Go 值：nil/typed-nil/case 指针/伪造值先于 wildcard 副作用；serde 编码返回错误 | 完整（具体子能力） | 用户必需边界说明；全入口/初始化/任意对象图校验可后置，不能称 Go 调用者无法造非法值 | [enums_test.go](../tests/acceptance/enums_test.go)：`TestEnumInvalidBoundaryRace`；[enum_serde_test.go](../tests/acceptance/enum_serde_test.go)：`TestEnumSerdeBusinessRace`（`TestEnumInvalidGoBoundaryReturnsError`）；[match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestBuiltinMatchExpressionRace`（`TestInvalidBuiltinBeforeEffect`） |
| 数值/string 外部枚举映射 | 有限 | 封闭 unit enum 的 serde 映射；开放 raw/C enum 已撤回 | [独立业务验收](../tests/acceptance/enum_wire_test.go)：已知往返、未知报错、未执行业务、Go 调用与生成race |
| serde 核心 Serializer/Deserializer、Visitor/Access 与结构化错误/cause；格式独立 | 有限 | 用户必需 D23；完整协议扩展、借用解码和用户实现协议后续逐项验收 | [core_test.go](../serde/core_test.go)：`TestErrorPathAndCause`、`TestVisitorDefaultsAndCallbackErrors`、`TestScalarReaders`、`TestScalarProtocolFailures`；[json_test.go](../serdejson/json_test.go)：`TestCallbacksAndStates`、`TestIgnoredProtocolErrorsRemainErrors` |
| 具体 struct JSON：标量、本地嵌套、数组/slice/string-key map、原生 Go `*T` 可空；rename/default/skip/deny_unknown_fields | 有限 | 用户必需首版闭环；字段 default/skip 解码调用已登记 Default，缺协议拒绝；`*T` 可空但无 Default，非新引用桥接；内建具体 Option 见下文增量 | [serde_test.go](../tests/acceptance/serde_test.go)：`TestSerdeBlackBoxRace`（`TestBusinessRoundtrip`、`TestSkippedFieldAndWhitespace`、`TestNumericBoundaries`、`TestNestedErrorPath`、`TestCycleAndDepthBound`）、`TestUnsupportedSerdeIsDiagnosed`；[compile_test.go](../internal/frontend/compile_test.go)：`TestAttributesCannotBeSilentlyIgnored` |
| JSON 严格数字/Unicode/重复键、无效值和深度限制 | 完整（具体子能力） | 用户必需可信输入路径；不推导支持完整 Rust serde/全部格式 | [json_test.go](../serdejson/json_test.go)：`TestScalarsAndStrictNumbers`、`TestStrictSyntaxEvenSkipped`；`TestSerdeBlackBoxRace` 内 `TestInputRejections`、`TestEncodingRejectsInvalidValues`、`TestZeroAndTinyNumbers` |
| 真实 enum serde：unit 字符串/newtype 单键外标记对象，struct/container 嵌套，Variant 路径 | 有限 | 用户必需既定顺序；变体属性、复杂/泛型/递归 payload、Result serde 后置；Option 包装具体 enum 见下文增量 | [enum_serde_test.go](../tests/acceptance/enum_serde_test.go)：`TestEnumSerdeBusinessRace`（`TestEnumWireAndRoundTrip`、`TestEnumContainers`、`TestEnumInvalidInput`、`TestEnumVariantErrorPath`）、`TestEnumSerdeIndependentDirectionsRace`、`TestEnumSerdeUnsupportedAttributes` |
| Error/From：消息、显式 source、唯一静态 error 字段的 FromType 与 Unwrap | 有限 | 用户必需已授权；enum Error 已有限支持（见增量）；任意具体错误转换、通用 From trait 后置 | [宏接受测试](../tests/acceptance/macros_test.go)：`TestCloneAndErrorAcceptanceRace`（`TestErrorChainsAndNil`）、`TestMacroAndCloneExplicitRejections`；[前端测试](../internal/frontend/compile_test.go)：`TestErrorDeriveFieldMethodCollisions` |
| 内建 Option[T]/Result[T,E] 显式实例化与名义识别：Option nil=None；Result nil 非法；保留 typed-nil payload | 有限 | 用户必需 D12/D22；不是用户泛型声明或任意 interface 分类 | [builtins_test.go](../internal/enums/builtins_test.go)：`TestBuiltinSums`、`TestBuiltinRejects`、`TestBuiltinNominalAndInvalidCases`；[match_expression_test.go](../tests/acceptance/match_expression_test.go)：`TestBuiltinMatchExpressionRace`（`TestBuiltinDistinctions`）；[propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationControlFlowRace`（`TestNativePartialAndTypedNil`、`TestInvalidResultIsNotBusinessFailure`） |
| `?` 在支持上下文保持一次求值、调用顺序、短路、最近闭包和 defer；成功类型可变化 | 有限 | 用户必需 D22；常见 for、两类 switch 与非函数 range 源已增量接通；函数 range 源/迭代目标及部分表达式/私有赋值长尾未完成，见末节 | [propagate_test.go](../tests/acceptance/propagate_test.go)：`TestPropagationControlFlowRace`（`TestDifferentSuccessTypesAndOrder`、`TestCalleeBeforeArguments`、`TestUnselectedBranchStaysUnevaluated`、`TestShortCircuit`、`TestNearestClosureAndDefer`）、`TestPropagationExplicitRejections` |
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
2. **T0 剩余基础能力**：继续扩展已接入的有限复合投影，维持封闭变体及serde数值/string映射边界；补充目前 unmapped 的跨阶段诊断来源。具体语法/ABI 未冻结时保持候选状态，不能由测试代替设计确认。
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

**历史计划（raw部分已由D27撤回）：** 后续优先级：先维持 enum 派生应用与失败诊断的真实回归，再处理常量 enum raw/未知值与最小跨包身份/公开入口；完整跨阶段来源继续补有证据的映射。常量例 `type Status string` 的未知 `"future"` 可存在、开放输入需wildcard，已是设计方向；尚未定案的是精确源语法、未知值模式及公开校验入口。后续最小建议：先实现开放raw匹配必须有wildcard的入口，再明确显式闭合校验的拼写，不重开保留raw的既定方向。通用引用桥接/容器/函数值、一般泛型及完整trait分别分期，长期借用分析独立。前面各历史阶段“enum派生未做”的记录由此增量取代，整项T0仍未完成。

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

**历史计划（raw部分已由D27撤回）：** 下一完成标准：先保持这些生成包可由原生 Go 工具独立构建，再补 `.hgo` 文件选择/测试和目录引用能力；随后以声明包 metadata 接通消费者的 enum 模式与 Result，而不是生成同名替代类型。跨包仍是 T0 必需方向，以上剩余项没有一概降级为后置。同 module 跨包声明宏已在下文增量接通；外部预编译宏仍需独立依赖元数据。常量 enum 的 raw/未知值既定方向不变，精确拼写的设计不阻塞本轮模块工作。

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

**历史计划（raw部分已由D27撤回）：** 下一步继续按应用需要扩展文件选择/目录引用、可证明来源和独立依赖metadata；常量enum raw/未知值方向保持，不扩大为完整T0或长期分析。本轮验证记录见实现记录最新段落。

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
| 目录引用 | [目录权限测试](../internal/frontend/directory_reference_test.go) 正向跨文件读/写/收窄；拒绝只读→写、native *T擦除、只读返回写入及公开出口；新业务验证权限阶段与serde/传播共存 | 同包私有引用函数及同模块公开标量引用函数；后续元数据首片见末节。复合公开引用与外部恢复未完成，普通 Go *T API 保持原定位 |
| Go 构建条件 | [选择测试](../internal/frontend/build_selection_test.go)：平台后缀、go:build/+build、显式tags、忽略文件与被排除语法；[CLI测试](../cmd/hgo/directory_test.go)：真实run/test与race/!race选择 | host目标；不同GOOS/GOARCH明确拒绝，.hgo测试源未实现。产物属于选定配置，换条件需重新转译 |
| 安装与独立使用 | 独立验收实际 `go install` 到临时GOBIN、仓库外CLI run、生成模块原生Go build/run及正负业务结果 | runtime依赖由go.mod/replace明确提供；没有下载或发布包的验收 |
| 输出与诊断 | 输入symlink覆盖拒绝、失败目录清理；真实跨包宏错误同时显示定义和调用位置；相同源两次Go/map产物比较 | 未映射来源仍标unmapped；不承诺移动源码根后绝对路径映射不变，也不承诺二进制逐字节复现 |

独立验收实际发现并推动修复两项：`sourcemap.Error.Error()` 丢失已保存的宏调用栈；CLI `test` 的隐含 `race` 条件未同步到前端和导入数据读取。后者由统一 `ModuleOptions{Tags,Race}` 接通，并由原生测试调用race限定.hgo函数验证。模块依赖文件遍历固定顺序，避免map迭代顺序影响处理次序。

**历史计划（raw部分已由D27撤回）：** **交付结论：**上述有限T0应用子集可作为本地可运行交付；这不是整项T0或M1–M8完成。整项已确认方向最短剩余清单仍为常量enum raw/未知值入口、内建print/println格式入口、Default；跨包新引用接口和更宽语法仍明确列为当前有限边界，不把风险分析/完整trait/LSP混入本次验收。

最终全套race：**14包、186宿主顶层测试、297子测试**通过；新独立业务包含**2个生成race模块、4个业务测试**，并已验收安装、仓库外运行、真实失败诊断和重复生成一致性。vet、完整设计检查13命令和文档检查通过；完整复现与日志见 [实现记录](../IMPLEMENTATION.md)。本轮没有剩余已确认缺陷，未支持能力仍按上表保留，不能称全语言完成。


## print / println 与 Default 增量

本节为历史阶段记录；当前 Default 和 serde(default/skip) 限制以下文增量为准。该轮遵循 [既有打印和默认值协议](design/macros.md)，不扩完整 fmt/trait 系统。

| T0 必需子能力 | 实际已支持 | 明确后置/不支持 |
| --- | --- | --- |
| `print!` / `println!` | 字面量 `{}` / `{:?}` / 花括号转义；明确标量协议及同包派生Debug；所有参数先按序一次求值，再格式化和写入；空输出/LF、错误和短写panic保留cause | 动态格式、宽度精度、自动Stringer/error/any、导入Debug协议、用户Display impl；go/defer和值位置打印 |
| Default | `DefaultType()`；标量字段、同包已派生字段、空struct、唯一指定unit enum、具体Option None；宏定义点helper登记 | 自动Result/容器/引用/外部类型默认、手写协议注册、完整泛型trait |
| 组合和边界 | 新 [启动配置业务](../examples/prototype/print_default.hgo)：显式默认→JSON覆盖→Clone→宏打印；默认选择第二个unit，不改Go var/make/map缺失或serde(default) | 不将合法显式默认值等同于所有Go零值合法；不把派生Debug当Display |

独立非作者新增 [验收测试](../tests/acceptance/print_default_test.go)，覆盖28项拒绝、宏/Default/Debug局部遮蔽、嵌套打印、所有参数先于formatter、短写/部分输出/原始cause，以及未支持协议的准确错误。实际暴露的嵌套打印遗漏和打印参数中 `?` 被当普通未知函数冻结已修复；基础类型同名遮蔽原已有守卫，本轮补跨文件验证，不冒称新修复。审查者复用已有线程，非新空上下文。

**历史结论已撤回：**当时计划开放raw入口并等待别名选择；D27现已撤回该路线和问题。当前按[封闭变体与外部映射](design/enums-and-match.md#01-封闭-enum-与外部映射)实施，不再把旧候选作为待选任务。

本增量最终验证：全套race为**15个有测试包、198宿主顶层、328子测试**，另internal/printing无包内测试而由前端/接受测试覆盖；新独立业务为**1个生成race模块、4个运行测试**，28项拒绝子例和真实CLI通过。vet、完整设计检查13命令及最终文档检查通过，详见实现记录。无已知未修复阻断；常量enum候选仍未批准或实现。


## 常量 enum 唯一值子集增量

**历史记录，已被 D27 撤回，不代表当前支持；对应成功夹具已删除。**

已支持 `enum Status string { Pending = "pending", Paid = "paid", }` 和 builtin 整数底层类型；成员必须显式常量，lowering 为 Go 命名类型及真正 typed const，能用于常量表达式和数组长度。同包多文件、同模块公开跨包使用保留名义身份。Go 转换保留零值、未知值及运行期整数截断，编译期溢出仍报类型错误；不插入成员集合验证器。

开放 match 必须有无 guard 的 `_` 或全值绑定，已列出所有已知成员也不例外。沿用既有语句和限定表达式位置；目前至少一个 qualified 成员模式用于识别 enum，wildcard-only 不支持。raw derive、隐式 iota、外部预编译 metadata 未实现。同一 enum 求值后相同的成员报“别名待用户决定，当前子集不支持”，不是永久禁止；不同 enum 使用相同 raw 值合法。

业务示例（旧 raw 夹具已撤回，历史提交 cd49537）由原生 Go JSON webhook 输入 string/数字，经过跨包 raw enum 和开放 match，再由原生 Go 断言输出。独立非作者验收（旧 raw 夹具已撤回，历史提交 cd49537）实际安装 CLI、两次生成字节一致、生成模块 3 项 race 测试、独立 binary 与仓库外运行，并覆盖 14 项拒绝；print/Default 旧业务同时回归。部分 lowering 诊断明确 generated/unmapped，不承诺全部精确原始行列。

本轮限定 T0 必需清单已有实现与应用验收路径；没有因此宣称整项 T0 或完整语言完成。未决同值别名不阻塞唯一值子集；无需为等待选择扩展全套 trait、智能指针或风险分析。最终全套统计见实现记录。


## 最后覆盖梳理：交付、待决与后置

| 分类 | 当前范围 |
| --- | --- |
| 有限可交付 | 同模块声明宏、具体 serde/JSON、封闭 enum/match、Option/Result 与专用 `?`/一步 From、已有读写权限、固定打印、具体 Default/Clone/Debug、CLI及生成Go模块 |
| 本轮更新 | 撤回开放raw/C enum和raw Clone；封闭unit enum字符串rename或全体显式int64 value；未知外部值UnknownVariant，Type/Range保留；业务不执行 |
| 已撤回问题 | U19同值raw别名不再等待；重复serde外部映射因解码歧义拒绝，不将其当成语言别名 |
| 已由后续用户确认 | enum 仅选中默认变体的 payload 要求 Default；非默认变体不要求。下述旧验收不代替新增实现验收 |
| 明确后置 | 业务validator、通用手写impl注册、统一trait wrapper前端、关联函数/Self/self前端、完整泛型/表达式/借用模式、复杂映射属性、YAML/LSP/外部metadata等 |
| 独立长期 | 别名/借用风险warning分析不属MVP；基础类型权限检查仍执行 |

## 封闭变体与 serde 外部映射验收

[业务模块](../examples/enum_wire/main.hgo)的Delivery映射字符串、Urgency映射整数；两者始终是声明的封闭变体。原生Go桥接调用生成codec，已知输入进入业务并Clone，未知输入返回带Field路径的UnknownVariant，业务计数为零。由此证明的是结构转换边界，不是业务validator功能。

[独立验收](../tests/acceptance/enum_wire_test.go)实际安装CLI、转译、生成模块3项race运行测试、独立build、4种二进制输入和仓库外CLI运行。另[归档业务](../tests/acceptance/t0_final_test.go)已改为封闭变体，生成模块2项race运行测试；print/Default旧业务生成4项race测试回归。反例覆盖已撤回raw语法、未知映射、混合/重复/越界属性、未登记协议和非法case；准确数量见实现记录。

历史验收时 Default struct 每字段必须有协议，手写 impl 尚未实现，enum 仅支持唯一 unit 默认。后续已确认并扩展为同包非泛型 struct 的显式手动 Default，以及允许默认变体 payload、仅检查选中字段；普通 helper 名称仍不认领协议。当前范围见 RFC 0013/0020 与 IMPLEMENTATION.md。Debug显示源变体/payload及struct字段内容（含私有字段），不跟随wire值、不自动脱敏、不保证稳定格式。

RFC0019/0020、全部逐特性RFC及检查器已同步；这些方向不因为文档合入就成为已实现能力。下文历史成果与当前闭环须区分，未宣称完整T0/Rust。


## 手动 Default 与默认 enum payload 增量

用户确认无 static：func 首参数 self 为实例成员，其余为关联函数。有限入口 `impl Default for T { func default() T { ... } }` 支持同包非泛型 struct，唯一零参具体返回目标；外层 derive 调用登记的业务实现。enum 仅选中默认变体的 payload 递归要求 Default，非选中变体不要求。跨包协议消费、通用 Self/trait 后端与动态递归分析未扩展。

[独立业务验收](../tests/acceptance/manual_default_test.go) 运行真实 CLI 与生成模块 3 项 race 测试，覆盖多文件、同名异包、调用顺序/次数、非零默认值、独立 channel、Go 零值不变和非法协议诊断。[前端回归](../internal/frontend/manual_default_test.go) 补签名、别名、冲突、静态递归及源位置。此增量替代旧表中的“手写协议注册未实现”和“仅unit”限制，不代表完整 T0。


## serde 字段 default/skip 协议增量

本轮将旧缺失/跳过字段的 Go 零值策略改为登记的 Default 调用，是显式兼容变化。旧代码中无 Default 的 skipped 字段（包括指针和容器）在带 Deserialize 派生时现在拒绝；Serialize-only 不要求解码 Default。普通同名 helper 仍不能认领协议。

| 验收切片 | 当前契约 | 保留边界 |
| --- | --- | --- |
| default 缺失与显式值 | 缺失调用一次 Default，显式零值正常解码；null 按目标 codec，未知变体和错误 payload 不回退 | 原生指针可解码 null，但无 Default，不能标记解码 default/skip |
| skip 输入与输出 | 编码省略；解码完整消费/验证输入而不调用字段 codec，无论字段是否存在均默认构造一次 | 非法格式/超预算仍失败，缺 Default 编译拒绝 |
| 调用顺序与失败 | 本层读取和 required 检查成功后按声明顺序初始化缺失 default/全部 skip；显式字段不调用 | 嵌套字段已执行的默认副作用不回滚，不承诺文档级事务 |
| 协议组合 | 同包多文件手动 Default、嵌套 struct、选中默认 enum payload 复用当前登记与诊断 | 本阶段不扩大跨包协议、容器级 default、新注解或完整 trait；Option serde 见后续增量 |

[独立业务验收](../tests/acceptance/serde_defaults_test.go)实际安装 CLI、仓库外转译并运行生成模块 4 项 race 测试，另有 8 项缺协议定位反例；[多文件示例](../examples/serde_defaults/main.hgo)输出 `demo 8080 8080 STSS`。[布尔名字捕获回归](../tests/prototype/serde_defaults_test.go)使用普通生成子进程，不计入独立 race 模块。具体命令见 [实现记录](../IMPLEMENTATION.md)。本增量替代上文历史阶段中“serde(default) 不改 Go 零值”的边界，不代表完整 serde-rs 属性或整项 T0 已完成。


## 内建具体 Option serde 增量

内建 `Option[T]` 复用现有 None/Some 格式协议。缺失 struct 字段与显式 null 都是 None，非 null 值解码为 Some；None 编码为 null，Some 编码载荷。普通缺失/null 是 serde Option 协议的 None，不依赖 Default 登记，也不要求或调用 T 的手动 Default；`serde(default)` 不构造 Some(default T)。这不改变普通 struct 字段显式登记的手动默认。

| 能力 | 当前范围 | 边界 |
| --- | --- | --- |
| 具体载荷与方向 | 已支持具体类型的 Option 字段、外层数组/slice/string-key map、本地 struct/enum 载荷；Serialize/Deserialize 独立派生 | 未 skip 字段的 T 必须有对应方向 codec，即使值总是 None；不开放跨包 codec、Result serde、通用泛型 trait |
| 缺失/null/错误 | 缺失或 null 为 None，存在值走载荷 codec，正常保留字段/索引/变体路径 | 错误值不回退 None 或手动 Default；原生指针可空性与 Option 是不同语义 |
| default/skip | default 缺失产生 None；skip 输入不调用载荷 codec，输出省略，成功后为 None | skip 仍验证语法/Unicode/重复键/预算，不按目标类型检查；skipped 载荷可无 serde/Default，但仍遵循既有 Option 类型合法性检查 |
| 嵌套与表示 | 合法 `Option[Option[int]]` 和 `Option[*int]` 使用正常可空协议 | Some(None)/Some(nil) 与 None 可折叠为 JSON null；不保证 PATCH 三态或任意 JSON 往返无损 |
| enum 与投影边界 | `Option[Enum]` 可用 | 普通 enum 直接 Option payload、含 Option 的 struct payload 仍拒绝；`Option[[]Option[int]]` 等穿过指针/容器的 enum 投影仍拒绝，不借新 codec 绕过检查 |

本增量不改变已有 Default/skip 调用顺序：读取与 required 检查后按字段声明顺序构造缺失 default/全部 skip，嵌套业务副作用不回滚。skip 的“验证输入”指格式级消费，不是强制匹配目标字段类型。

[独立接受测试](../tests/acceptance/option_serde_test.go)验证实际安装 CLI、仓库外转译、生成模块 9 项 race 测试与 9 项边界拒绝；[多文件业务](../examples/option_serde/main.hgo)输出 `option 7 -1 4 0`。[名字捕获回归](../tests/prototype/option_serde_test.go)使用普通生成子进程，验证具体类型名不被 helper 局部变量捕获。完整验证记录见 [实现记录](../IMPLEMENTATION.md)。


## JSON 底层 Go 错误链补缺

[格式层测试](../serdejson/cause_test.go)验证 Token、容器结束、尾部输入、数值转换和 Unicode 数字解析中的真实 Go cause，保持原 Kind/Path；可通过 errors.As/Is 取回 SyntaxError 的原始 Token Offset、NumError 与 sentinel。自检错误没有伪造 cause，已有路径 wrapper 指向内层 serde.Error 属于正常链。

[业务示例](../examples/serde_causes/main.hgo)与[独立验收](../tests/acceptance/serde_causes_test.go)使用真实 CLI 与生成模块 race，覆盖嵌套 Field/Index/Variant、正常订单、语法/范围/类型错误。此项补齐既有 cause 契约，不放宽 enum payload、不自动采用 MarshalJSON，也不新增适配 API。格式 Value、native adapter、PATCH adapter 仍为未实现方向，手写 typed helper 和普通 Go 调用可用。


## 普通 Option 缺失与 Default 解耦

普通 Option 缺失直接进入 serde None 分支，不调用 Default 检查/表达式生成；null 保持既有 Option/VisitNone。显式 default/skip 继续按独立 Default 契约处理。扩充的 [Option 验收](../tests/acceptance/option_serde_test.go)以无 Default 的 InputPayload 证明缺失/null/Some 可反序列化，并用 Default 派生拒绝确认该 payload 未取得协议；带副作用的 Settings Default 不被普通缺失/null 调用，显式 Option default/skip 不回归。生成模块现有 9 项 race 测试，既有 serde-defaults 生成模块 4 项 race 同时复验；普通与自定义 adapter 路径不能混同。


## 2026-10-02 T0 收口核对

实现基线为 `fb74ba2380389b39afe92f35ebdb8500fab74adf`（feat/manual-default），不是环境默认 main/work。本节覆盖此前把逐轮原型限制笼统称为“后置”的表述；不能据此宣称整个 MVP 完成。

| 分类 | 当前剩余范围及处理 |
| --- | --- |
| 必须继续完成/核验 | 已确认宏、闭合 enum/match、只读/可写分离、Option/Result 问号及 JSON serde 的实用闭环；常用传播上下文、权限跟踪的普通用法误拒绝、跨功能组合和诊断仍需逐项证据。尚未支持的语法不自动成为获批延期，也不要求完整 Rust 语言 |
| 已有实现，按当前树复验 | 同 module 宏/enum、有限 match、基础投影、Default/print、手动 struct Default、默认 enum payload、serde default/skip、Option 缺失/null 与 Default 解耦、JSON cause、安装 CLI 和生成模块独立 race |
| 明确不作为本轮前置 | D20 全部复杂别名/借用风险分析；完整 trait/native bridge、LSP、YAML、格式 Value/PATCH、用户过程宏/完整泛型生态不顺手扩展；其中桥接/格式扩展还有未定设计，不宣称全部永久延期 |
| 需要用户新设计决定 | Proposed 字段 adapters 的具体入口及自定义 deserialize_with 缺失策略；U02/U15 尚未闭合的引用类型/原生桥接协议、最终发布 ABI 等。与既定的只读路径语义、非独占 mut&、Default 规则分开 |

本阶段选取已定语义的 `if` initializer/condition `?` 缺口；[接受测试](../tests/acceptance/propagate_if_test.go)新增实际 hgo→Go 生成模块四项 race 测试、四项前端拒绝和安装 CLI/业务转译产物 race。原函数内词法块保留作用域、一次求值、短路、else-if 和 defer。普通 Option 缺失/null 及 Proposed adapter 不在本次实现改动内。

下一阶段仍需处理：循环/switch/间接赋值传播（多 RHS、索引/切片/复合字面量已由后续增量接通）；常用 match 表达式组合；权限跟踪中的捕获/取址/循环与字段替换限制；公开引用元数据和剩余组合/来源诊断。先区分已定类型规则和未定接口，再选可验收切片；不以“无法证明别名安全”为普通 Go 用法设置新门槛。


## T0 收口第二组：多 RHS、解引用/断言与发送

基于 `ab02dc3a26860447a3eb6927013271725a0b3868`，实现标识符目标多 RHS、单 var spec 多值、指针解引用、类型断言/comma-ok、channel/value 中的传播。类型/错误/权限规则不扩大。[接受测试](../tests/acceptance/propagate_values_test.go)实际转译 hgo 并运行生成模块 race；两文件[任务投递业务](../examples/propagate_values/main.hgo)经安装 CLI、仓库外转译和独立生成模块 race，核验真实 NumError cause 与失败不发送。

| 剩余常用传播位置 | 最小业务例子 | 状态与下一步 |
| --- | --- | --- |
| 索引/切片 | `items[loadIndex()?]`、`items[low()?:high()?]` | 后续第三组已接通，见文末准确证据与限制 |
| 复合字面量 | `Config{Port: parsePort(text)?, Name: name()}` | 后续第三组已接通，见文末准确证据与限制 |
| 间接赋值目标 | `items[index()] = load()?` | 未实现；需保留目标操作数求值及赋值两阶段，不能靠把全部 RHS 提到前面实现 |
| for/range | `for ready()? { ... }`、`for _,x := range load()? { ... }` | 未实现；每轮测试、post、continue 和标签要单独降低，不是统一前置临时量；不等同已批准延期 |
| switch | `switch load()? { ... }`、`case predicate()?` | 未实现；initializer 作用域、case 顺序、fallthrough/break 需保持 |
| select | `case jobs <- load()?` | 未实现；候选求值/提前失败与选择后操作的组合规则未明确，需明确后实施，不猜测为只求选中候选 |

其他实质缺项仍是 match 常用表达式组合、权限跟踪对捕获/取址/循环/字段替换的保守限制、公开引用元数据及部分跨功能诊断；Proposed adapters 入口/自定义缺失策略、原生桥接的未定规则另列。长期别名分析、完整 trait/LSP/YAML/Value/PATCH 不成为本次收口的隐性前置。此组完成不等于 T0 整体完成。


## T0 收口第三组：索引、切片与复合字面量

基于 `a229e5f12dfebc953dea75beae867af5161abd3d`，补齐既有 Option/Result 的索引/切片及 struct、数组、slice、map 字面量传播。[接受测试](../tests/acceptance/propagate_aggregate_test.go)包含 **10 项**主生成模块 race、跨包批次业务 **2 项** race、只读数组投影 **1 项** race，另有 **14 项**前端拒绝。拒绝包括不可取址数组切片/元素指针方法、静态边界、重复键/字段、非法 literal key、只读共享字段装入可写容器及不求值 unsafe 参数。

覆盖证据明确分开：源码真实经过 hgo→Go；安装 CLI 在仓库外转译[批次业务](../examples/propagate_aggregate/main.hgo)，独立生成模块执行；纯 Go 规范探针不是前端实现证明。业务保留 strconv.NumError cause，并覆盖外部包私有 scalar、struct/array、隐式指针、map 键值、命名 bool 短路及 comparable 遮蔽。对应 compiler-support 约束见 [runtime](../runtime/constraints.go)，不是新 trait/公共协议；只有遮蔽且需要该 witness 的生成路径引入此包，目录模块须声明 goxide 模块 依赖。

本组保留规定的调用/接收/逻辑顺序；不对变量读取、索引及 map 插入的未规定顺序作新增承诺。动态重复键接受 Go 允许的任一结果。数组原存储和可取址类别、map comma-ok、省略 bounds/类型、常量 key 错误及 len/cap 原求值属性均分别验收。

实质剩余：

- **间接赋值目标**：`items[index()] = load()?`、`*destination() = load()?`；需要保存 LHS 位置及两阶段赋值，不能复用读取临时量后直接写副本。
- **循环/range**：每轮 condition/post、continue/标签及 range 源中的传播；仍是具体 CFG 工程缺口。
- **switch**：initializer/tag/case 的顺序、break/fallthrough；仍待实现。
- **select 候选**：传播失败与候选求值/选择的组合规则仍待明确，本组未猜测。
- **不求值上下文**：unsafe.Sizeof/Alignof/Offsetof 的参数；当前 prelude 会错误求值，已明确拒绝，需后续非求值类型降低。
- **其他 T0**：match 常用表达式组合、权限跟踪对捕获/取址/循环/字段替换的保守限制、公开引用元数据与跨阶段诊断；Proposed adapters/原生桥接待决接口另列。长期别名分析、完整 trait/LSP/YAML/Value/PATCH 不加入隐性前置。

以上是剩余工作，不因阶段测试通过自动变为获批延期；整项 T0/MVP 仍未完成。

## T0 收口第四组：间接赋值的两阶段执行

本节记录 `067ead774bf27a0366e7b69c5349f25ae5fa3574` 的交付边界；其中私有嵌入指针下一节仅接通功能；性能复核未通过，尚未收口。

[实现](../internal/propagate/place.go)与[真实前端验收](../tests/acceptance/propagate_place_test.go)接通 pointer、field、slice/array/pointer-array/map 目标及多目标/compound/tuple/comma-ok 组合。保存第一阶段的容器和索引、保持数组原存储，传播失败不进入写阶段；最终 nil/bounds/map panic 和从左到右的部分写入保持 Go 行为。用户调用在求值阶段的副作用不会回滚。现有只读权限仍验证，同时接受可写别名更新经只读路径观察，不新增借用分析。

三个新增生成模块分别执行 race：主语义 **9 项**、安装 CLI 跨包业务 **2 项**、unsafe 边界 **1 项**。另有 **10 项**静态拒绝子例、三种 unsafe 非求值入口拒绝，以及外包私有嵌入指针限制诊断。真实业务输出 `42 9 <nil>` 与 `invalid value 42 9`，保留 NumError cause。纯 Go 阶段/panic 探针只作研究证据。

| 分类 | 当前具体剩余 | 本阶段结论 |
| --- | --- | --- |
| 必须完成的工程缺口 | 外包私有嵌入指针 promoted field 目标；循环/range；switch；match 常用组合；权限保守限制；公开引用元数据与诊断 | 不将已定语义的缺口改名为批准延期；间接赋值仍为部分完成 |
| 已实现并验证 | 普通间接目标的保存与两阶段写入、只读/可写已有约束、私有 scalar 上下文及纯值嵌入 promotion | 新增真实 hgo 生成模块和 CLI 验收；不代表其他 T0 行自动完成 |
| 已有实现仍需整体验收 | 历史矩阵中的组合、公开跨包权限元数据及逐项源码位置保证 | 阶段全树测试不能代替每个边界的验收证据 |
| 明确长期不纳入 | 复杂别名分析、完整 trait/Rust 能力、完整 LSP；完整 Value/PATCH/YAML 不顺手扩大 | 不用拒绝普通 Go 用法冒充安全保证 |
| 需要新设计决定 | select 候选失败规则；Proposed adapters 入口与自定义 deserialize_with 缺失策略；完整 native bridge 的候选接口 | 本组不选定或实现这些接口 |

unsafe 的现有限制精确匹配真实 Sizeof/Alignof/Offsetof builtin；Add/Slice 和普通同名函数不被误拒绝。非求值降低仍未实现，本轮不扩其语义。外包私有嵌入指针目标是编译器工程障碍，不需要用户重定语言语义；未通过反射/unsafe/跨包代码注入掩盖它。暂不切换到循环/switch，也不宣称本切片或整项 MVP 全部完成。

## T0 第四组补齐：外包私有嵌入指针目标

覆盖上一节的私有嵌入指针限制：当前 [frame lowering](../internal/propagate/frame.go)保留原 Go assignment，不生成不可访问 selector，也不复制数组/根结构体来冒充原位置。原生包无需配合修改或导出私有字段。仅这类目标使用 [runtime guard](../runtime/assignment.go)，问号失败不进入写阶段；private scalar 的上下文类型与只读权限由原赋值继续保留。

实际[跨包业务](../examples/propagate_places/private.hgo)新增 **10 项**生成模块 race，另 **6 项**前端拒绝，旧限制拒绝用例已替换为正向执行。与既有两项业务合计 **12 项**生成模块测试；legacy `GODEBUG=panicnil=1` 另重跑 panic/Goexit 一项，属于配置验证而非新增测试计数。CLI 业务新增输出 `stock 7 <nil>`、`invalid stock 7`。命名返回遮蔽、非法裸 return、goto 声明作用域三个独立审查发现均已修复并留真实用例。

这仅接通已识别的普通间接赋值目标功能；后续性能复核未通过，不能称该缺口或 T0 完成。需要纠正旧分析中的过强顺序假设：Go 不保证隐式指针读取早于全部 RHS 调用；本轮只承诺规定的调用顺序与两阶段写入，对未规定的读取顺序不作额外保证。根本不可行的推断撤回，也没有把缺口变成批准延期。

当前先处理私有目标正常控制流方案，不将 guard 扩到循环。for/range、switch、match 组合、权限保守限制、公开引用元数据及诊断仍是工程待办；select 候选失败、Proposed adapters 入口和 custom deserialize_with 缺失策略继续待决。

### 私有目标性能复核：未收口

`a3f064fbcd0e75bb06bdba406f007e5abd68bd71` 功能测试通过，但性能验收失败：单条 guard 成功 32 B/1 alloc，失败 48 B/2 alloc；八条执行成功 256 B/8 alloc，末条失败 296 B/10 alloc。后者包含实测 Go runtime recovery 的额外分配；不把全部成本归因于编译器不能内联。[可复现研究](../tests/research/assignment_guard/README.md)保存原始测量及后续复核，区分真实 hgo 执行与手写 Go 机制探针。

本次修复只读提升字段作为索引/key/调用参数的误路由，新增一项生成业务测试（现共 13 项）与生成代码无 guard 断言。真实索引成功路径测得 0 B/0 alloc。真正私有目标仍使用未获性能验收的 guard，不能勾为完成。手写 Go 的无 panic 完整赋值 IR 子集探针可行，通用实现和验收未完成；没有新的语言语义决定或批准延期。


### 正常传播控制流基础切片

本阶段仅提取 [questionFlow](../internal/propagate/question.go)，把失败出口从 sum 校验/From/结果构造中分离；普通 expr 显式 return，私有 frame 显式 Fail，移除 AST return 扫描改写。没有扩大支持范围，也没有替换真正私有目标 guard。所有 private.hgo 中的此类赋值和研究 Guard1/Guard8/PackedGuard 仍走原 guard，性能门槛继续未通过。

[下一阶段模块与风险](rfcs/research/0005-option-result-question.md#68-显式传播控制流基础重构)明确原始类型记录、赋值求值规划和完整赋值 emitter；不能仅因已有手写子集就把这些工程项打勾。新增生成 Go 控制流机制测试单列，既有真实 hgo 私有目标/权限/跨包验收继续保留。循环、select 和 adapters 没有改动。


### 私有目标无 panic lowering：首批真实替换

本阶段在 questionFlow 基础上实现原始类型/地址性快照与局部求值 planner。原 private.hgo、新增 effects.hgo 和研究 Guard1/Guard8/PackedGuard 已采用普通失败 return，生成代码断言不含 Assignment/Catch/Fail；失败不写、两阶段部分写入、重绑、私有 scalar/泛型 bool 类型、直接 recover/defer/Goexit、只读约束和 goto 均通过真实前端验收。

仍有 guard：专门的 effects_fallback.hgo 包含 **5 条**回归（右臂私有 bool、有符号 contextual shift、含问号 len、tuple、unsafe.Pointer 链），其余未规划形状见 [RFC §6.9](rfcs/research/0005-option-result-question.md#69-首批无-panic-完整赋值-lowering)。本组不是全部私有目标收口；guard 成本只从已覆盖路径移除，runtime 仍用于明确剩余范围。unsafe 拆链反例由独立审查发现并修复，race/checkptr 成功及失败不写成为永久真实 hgo 回归。新增混合目标负例防止临时复制把不可赋值 struct 返回值变合法。


本阶段验证：全树 race **15 包 / 244 顶层 / 441 子测试**（另 2 包无测试），vet、13 条设计检查通过；private 业务 **18 项**生成 race。独立研究模块另跑 race/legacy/vet，非 race 200ms × 5 检查核心三种成功路径全部 **0 B/0 alloc**，失败 **16 B/1 alloc**；与显式 match 的原始对照已入[研究记录](../tests/research/assignment_guard/README.md)。该门槛仅覆盖已替换路径，不把仍有 guard 的组合或整项 MVP 打勾。


### 无 guard 第二组：bool witness、signed shift、len/cap

[effects_context.hgo](../examples/propagate_places/effects_context.hgo)新增无 guard 形状断言，迁入此前 mixed private bool、有符号 contextual shift、含问号 len 三条用例，并加入私有泛型类型身份/右臂未调用、nil 目标、负 shift 不写、private 参数上下文、cap、常量 keyed literal、nil pointer-array 与真实解引用边界。duplicate-key 继续作为前端负例。专门 fallback 文件从五条降到 **两条**（tuple/unsafe），不改变这两条的语义保证。

本组仅覆盖 [RFC §6.10](rfcs/research/0005-option-result-question.md#610-私有-bool上下文-shift-与-lencap-第二组)列明的签名来源和 operand。字段/动态或泛型 bool witness、未规划 literal/表达式及旧普通 expr 的私有上下文问题仍是工程缺口；不能称所有 bool/len 或全部 guard 已收口。循环/range、switch 仍列待办，select/adapters 继续待决。


本组最终验证：全树 race **15 包 / 244 顶层 / 442 子测试**，另 2 包无测试；vet、13 条设计检查通过。业务生成模块 **22 项** race；独立研究模块 **11 项**另跑 race/legacy/vet。Bool/Shift/Len 新三组与旧三组成功全部 5 次样本 **0 B/0 alloc**，新三组失败 **16 B/1 alloc**与显式 match 一致。[分组原始基准](../tests/research/assignment_guard/README.md)和独立反例证据明确区分真实 hgo 与纯 Go 探针。

### 无 guard 第三组：赋值 tuple 与 comma-ok

[多值用例](../examples/propagate_places/effects_multivalue.hgo)与 [RFC §6.11](rfcs/research/0005-option-result-question.md#611-单-rhs-tuple-与-comma-ok-的第三组替换)同步实现单 RHS 静态 tuple 调用及 map/assert/receive 二值操作。新增三项真实生成业务测试验证一次求值、跨包私有类型、空白目标、原位置/部分写入、失败不写和 comma-ok 的 private bool；新增三类非法多值语境拒绝。业务模块共 **25 项**，多值生成文件断言无 guard，unsafe fallback 文件恰好一条 Catch。

| 分类 | 当前状态 |
| --- | --- |
| 本阶段已实现、须随阶段验证 | tuple/comma-ok 的限定赋值范围；整树 race/vet/设计、生成模块 race/checkptr 与非 race 基准 |
| 必须继续完成 | 其余私有目标 guard 替换及普通 expr 上下文缺口；for/range、switch；完整 T0 逐项验收 |
| 已实现继续保留回归 | 原私有目标、bool/shift/len、宏/enum/match、Default 与 serde/Option 已定协议 |
| 明确延期 | 复杂别名分析；不能借此拒绝普通 Go 别名用法，readonly 不代表全局不可变，mut& 不承诺独占 |
| 需要新设计决定 | select 候选语义、Proposed serde adapters 的入口与自定义缺失策略；不阻塞以上独立工程 |

unsafe 没有扩大 planner 支持范围，原转换链仍保留；方法/动态调用、多值唯一实参等其他明确剩余范围见 RFC。完整 Rust 能力不纳入本阶段，整体 MVP 不标完成。

最终验证通过：全树 race **15 包 / 244 顶层 / 445 子测试**（另 2 包无测试），vet、完整设计检查 13 条 Go 命令；业务生成模块 25 项 race，研究模块 12 项 race/legacy/vet 与额外 checkptr=2。Tuple/Map 新两组及原六组成功全部五次样本零分配，新两组失败与显式 match 同为 16 B/1 alloc；[原始数据](../tests/research/assignment_guard/results/multivalue/summary.json)入库。独立非作者审查及安装 CLI 复验通过。

### 普通循环首批：for header 与 slice/map range 源

[真实示例](../examples/prototype/loop_propagation.hgo)由前端生成独立模块并运行 race，另有安装 CLI transpile/run 验收。覆盖 init/每轮条件/post、失败早退、continue/break/标签与 goto 重入、嵌套循环、defer、最近闭包返回、每轮闭包/地址身份，及 slice/map 源一次求值与两种迭代变量声明。生成代码断言无 Assignment/Catch/Fail。

[精确范围与边界](rfcs/research/0005-option-result-question.md#612-for-与-slicemap-range-的首批传播)：只新增 slice/map 源传播；无问号源保留 Go 原行为，验证 nil 数组指针（含原本不求值的解引用）、整数与单值函数 iterator 的循环体传播。其他种类的 range 源问号、range 目标问号及循环内尚需旧 guard 的赋值组合继续列工程缺口。重复/空白标签目标反例已由独立审查发现并修复，不吞掉原 Go 静态错误。

for/range 不再整体列作完全未实现，但也不整体打勾；switch、普通 expr 上下文与剩余 guard 继续待办。select/adapters 仍单列设计问题，复杂别名分析长期延期，不把普通循环或普通 Go 别名用法视为设计待决。

本阶段验证通过：全树 race **15 包 / 247 顶层 / 454 子测试**（另 2 包无测试）、vet、13 条完整设计命令；新增 **10 项**生成循环模块 race 测试、安装 CLI 生成程序 race 与 10 类负例。独立审查及反例复验通过。研究模块 1 项真实 hgo 等价测试另跑 race/vet；成功/失败四组全部五次样本零分配，但耗时高于显式 match，具体原始数据见[循环研究记录](../tests/research/loop_propagation/README.md)。

### 表达式 switch 首批：init、tag、case 与原生分支

[真实 hgo 示例](../examples/prototype/switch_propagation.hgo)验证 case 逗号列表/跨 clause 的求值顺序、匹配后停止、失败不进 body、default 在前/中间、fallthrough、标签重入与外循环 continue、作用域/最近闭包、defer 和接口动态比较 panic。[跨包用例](../examples/propagate_places/effects_switch.hgo)验证 private mask 移位与 private bool 的上下文保持。代码形状断言无 Assignment/Catch/Fail，独立生成模块 race 与安装 CLI transpile/run 另行执行。

[范围说明](rfcs/research/0005-option-result-question.md#613-表达式-switch-的传播与循环开销复核)：表达式 switch 已接通，type switch 中的传播本组未接通。重复 case/default、错误 case 类型、不可比较 tag、非法 fallthrough/continue、作用域仍拒绝。unsafe 转换链、其他 range 来源/目标、控制流内未规划私有赋值、旧 expr 上下文与其余 guard 继续列工程缺口，不称完成或批准延期。

for 的耗时差异已单独调查，first 状态只解释一部分，三条路径均未内联；危险的去 first 探针未用于生产。[具体数值与可复现证据](../tests/research/loop_propagation/README.md#first-flag-follow-up-isolate-part-of-the-overhead)区分零分配、耗时和语义范围，不以零分配代替性能说明。

本组最终验证：全树 race **15 包 / 250 顶层 / 462 子测试**，另 2 包无测试；vet、13 条完整设计命令通过。独立审查与跨包原反例复验通过；switch 模块 6 项、private 业务模块 26 项各自生成 race，另有安装 CLI 生成程序 race。switch 研究模块 1 项等价测试另跑 race/vet，四组五次非 race 样本均零分配；[原始基准](../tests/research/switch_propagation/README.md)不声称与显式 match 耗时相等。for 手写机制探针单列，不冒充生产优化。

### type switch 与非函数 range 源增量

[真实 hgo 示例](../examples/prototype/type_range_propagation.hgo)接通 type switch init/subject/body 传播，保留原生单类型具体变量、多类型接口变量、nil/typed-nil、作用域、标签和最近函数 return。新增 string/整数/channel/数组/数组指针 range 源，按 [RFC §6.14](rfcs/research/0005-option-result-question.md#614-type-switch-与其他非函数-range-源)区分完整数组保存、指针迭代、整数上下文与原生免求值。非求值子树中的传播仍拒绝，不能笼统强制执行数组源。

专项生成模块 **8 项** race 测试，另有安装 CLI transpile/run 及 **11 类**静态拒绝。跨包 private 业务模块新增一项整数 range 上下文测试，现 **27 项**独立生成 race；包含 1<<63 不被默认成 int、失败不写和负 shift 检查次序。独立审查自建的零长数组、blank 第二变量与 case/init 闭包反例已纳入永久验收。函数 iterator 源问号、range 目标问号继续留工程缺口，不冒称全 range 完成。

### 当前剩余 T0 优先级

| 优先级 | 可验收的下一工作 | 边界 |
| --- | --- | --- |
| P1 | 常用 match 表达式组合：先普通赋值/已知参数类型位置，再按 RFC 补齐局部 `:=` 等推断范围；保持一次求值、仅选中 arm、穷尽检查与最近函数控制流 | 现有显式类型局部整个 initializer、有限整个 return，新增完整 arm 嵌套和有限同文件已知参数调用；普通赋值、推断与其他组合仍未完成，不要求完整 Rust 模式系统 |
| P2 | 公开引用权限元数据：先同模块/源码依赖的导出签名、导入还原及消费者校验，加入缺失/错包/擦除反例 | 已完成同次模块构建内公开标量及具体复合元素参数/单返回，剩余签名/元素与外部恢复仍未完成；最终外部 ABI、native bridge 未定部分另列 |
| P3 | 权限跟踪误拒绝：循环/早退分支及元素取址的局部重绑定已补；继续字段替换、捕获/真实槽位取址、switch/select/goto 的局部事实修复 | 不引入复杂别名分析，不把只读当全局不可变或 mut& 当独占，不用“安全”包装原型限制 |
| P4 | 剩余传播：函数 iterator 源、range 逐轮赋值目标、unsafe 链/其他 guard、旧 expr 私有上下文；for 已测耗时差距继续有证据优化 | 不阻塞 P1/P2；这些仍是工程欠账，不是获批延期，也不新增正常传播 panic |
| P5 | 整体 T0 组合验收与来源诊断：宏/enum/match/权限/Default/serde/传播和安装 CLI 一起逐项核验 | 已有测试通过不等于全部能力打勾；重复旧迁移/云还原不计新增验收 |

Proposed serde adapters 入口与自定义缺失策略、select 候选语义及未定原生桥接另列设计问题；复杂别名分析长期延期。完整 trait、Value/PATCH/YAML 等不扩大为本轮前置。整体 MVP 尚未完成。

本阶段最终验证通过：全树 race **15 包 / 253 顶层 / 470 子测试**（另 2 包无测试）、vet、13 条完整设计命令；新增 type/range 模块 8 项、private 业务模块 27 项各自生成 race，安装 CLI 生成程序 race/run。独立审查无新增阻断，相关反例永久化。整树测试与新增具体边界区分记录，不由本组通过推导整体 T0 完成。

### 常用 match 组合切片验收（2026-10-02）

旧范围：整个显式类型局部 initializer、有限整个 return。新增：完整 arm 嵌套、有限同文件已知参数类型调用，调用处限完整 return/独立调用及完整 arm/实参递归组合。[接受测试](../tests/acceptance/match_composition_test.go)走真实 frontend/CLI→Go，覆盖 JSON 配置、Result 传播、参数/guard 顺序、上下文类型、goto、最近闭包和跨包非法 enum 验证；4+2 个生成模块测试单独 race，跨包另 vet。具体边界及性能样本见 [实现记录](../IMPLEMENTATION.md#常用-match-表达式组合2026-10-02)和 [RFC](rfcs/0009-match.md)。

普通赋值、推断、任意字面量/运算符及动态等调用仍未完成，不默认为已批准延期。之后先推进公开引用元数据，同时保留权限跟踪与 `?` 长尾、跨阶段来源诊断清单。Proposed serde adapters 尚待设计决定；长期别名分析及完整 Rust/trait/Value/YAML/PATCH 不扩大。

### 公开标量引用元数据首片（2026-10-02）

| 范围 | 验收状态与证据 |
| --- | --- |
| 同次 module 构建、公开普通函数的内建标量引用参数/单返回 | 已实现有限子能力；多文件、alias import、返回转发和读写方向由[真实 CLI 模块验收](../tests/acceptance/reference_metadata_test.go)验证 |
| Go 消费者权限保留、合法 native 调用 | 无新引用语法也校验；只读写入/升级/接口/指针/闭包擦除拒绝，普通 unmarked Go API、同名方法、私有/传递普通 slot 通过 |
| 身份、版本、确定性 | 真正 import path/包函数对象，v1/摘要/签名/透明 alias identity；重复转译字节一致，错误元数据见[单测](../internal/refs/metadata_test.go) |
| 跨构建缺失与 source | single→module 与 module→single marked 产物均明确缺 metadata；import/函数值原 source span 已验证，下游 refs 重建仍可能 generated/unmapped |
| 产物验证 | 真正 hgo→Go 的独立跨包模块 `go test -race`、`go vet`；宿主单测和生成模块统计分开 |
| 尚未完成 | 复合/具名元素、方法/泛型/引用容器/回调、named/multiple result、dot import、外部恢复及完整来源映射；不自动改为批准延期 |

[实现记录](../IMPLEMENTATION.md#同模块公开标量引用元数据2026-10-02)与 [RFC 0005](rfcs/0005-borrows.md)说明内部格式、marker 和信任边界。本片不引入长期别名分析、独占承诺、全局只读或完整 trait，也不决定 D11 尚待协调的便利桥接。

### 公开复合引用元数据增量（2026-10-02）

| 范围 | 验收与剩余边界 |
| --- | --- |
| 公开具体 struct/slice/map/array、具名类型和递归指针元素 | 同次模块构建内参数/单非具名返回接通；[真实 CLI 模块测试](../tests/acceptance/composite_metadata_test.go)覆盖多文件/三包转发、别名与私有返回 |
| 真实类型身份和显式收窄 | 不同 import 拼写同型通过；不同 defined type/异包同名拒绝；写转读显式重借用通过，反向升级拒绝 |
| 既有只读投影 | 字段、数组、slice/map/pointer 写入和复制共享字段/接口/原生指针擦除拒绝；标量及数组/struct 副本的独立值部分可写，共享 slice/map/pointer 投影仍受限；普通 native 调用和其他可写 alias 合法 |
| 验证证据 | 15 个宿主子项；生成模块独立 race 执行 3 项业务测试并 vet；两次转译字节一致；独立审查另验证私有递归返回与 5 个拒绝反例 |
| 信任范围 | v2 内部 registry/marker/真实签名与元素 alias identity；只在 hgo 流程校验，原生 go build 不执行权限检查 |
| 仍未完成 | interface/channel/function/用户泛型元素、方法/回调/引用容器、named/multiple result、dot import、外部恢复、投影保守限制及完整 source map |

[实现记录](../IMPLEMENTATION.md#同模块公开复合引用元数据2026-10-02)与 [RFC 0005 §6.16](rfcs/0005-borrows.md#616-同模块复合元素权限元数据2026-10-02)同步。本阶段没有新语言设计选择；Proposed serde adapters、自定义缺失策略及 D11 待决接口继续单列，长期复杂 alias 分析不纳入 T0。整体 MVP 不标完成。

### 普通权限跟踪误拒绝首组（2026-10-02）

[5 个真实最小例及依据](rfcs/0005-borrows.md#617-普通局部权限事实与控制流2026-10-02)区分类型违规与原型分析限制。循环内重绑定、局部复制后普通调用、退出分支和元素取址后的局部重绑定现已允许；字段替换仍误拒，继续列工程缺口。[模块验收](../tests/acceptance/refs_control_flow_test.go)包含 1 个正向子项（9 个业务函数、生成模块单独 3 项 race 测试及 vet）和 20 个拒绝子项；接口/旧 alias/回边/零轮/range 冻结/实际取址槽位不能擦除权限。独立审查补齐每轮 var 零值资格重建。没有引入堆别名分析、风险告警或自动 native bridge。

### 当前最小收口清单与停止条件（2026-10-02）

以下收敛到已有有限 T0 的交付标准，不要求完整 Rust 能力，也不把原型未实现项自动视为获批延期。只有每项有可复现证据或明确的范围决定后，才能宣布所述有限 MVP 完成。

| 类别 | 剩余工作与可验收停止条件 |
| --- | --- |
| 必须完成：原位借用与跨模块 | D29 原位可写 payload 借用不得用复制/隐式共享替代；enum 表示迁移等待具体选择。D30 已接通下文现有具体引用签名的跨 module local replace 恢复；其他增强协议与签名仍留工程账，同 module 不作为获批首版冻结 |
| 必须完成：自然 Go 指针调用 | D31 具体标量同型参数已通过下文验收，其余明确需求按真实 Go 类型逐项接通，不用未知 effects 或独占证明阻断普通用法 |
| 必须完成：常用 match | 单标识符赋值/局部推断、二元表达式/短路与简单索引目标已通过下文限定验收；剩余更复杂目标/其他表达式上下文留范围账；停止条件为已定类型规则、一次求值/仅选中分支/最近函数传播，不无限扩大模式系统 |
| 必须完成：普通权限误拒 | 局部值字段替换已通过下文限定验收；捕获/真正取址局部整槽替换也已修复；继续部分字段、指针/索引路径与未建模控制流中的合法普通用法；同时保留只读写入与擦除错误，不要求复杂 alias 风险证明 |
| 必须完成：已列传播欠账 | 现存函数 iterator/range 目标、unsafe/guard 与上下文长尾逐项给出实现证据或明确范围决定；for 性能已有差距不能称等价，不将其扩成新优化项目 |
| 已通过限定联合验收 | 下文联合业务贯通同 module 宏/封闭 enum/match/OptionResult/Default/serde JSON/权限；安装 CLI 仓库外构建、失败路径、生成模块 race、确定性及诊断矩阵通过。联合验收不抹去其余范围账 |
| 范围不能暗改 | 当前公开引用是具体元素参数/单非具名返回，现支持同次 registry 和下文跨 module artifact 恢复；其他增强协议/签名/引用容器与完整来源映射保留工程账。先按已定有限交付标准核验实际需求，若要从已列工程项移出 MVP，须明确范围决定，不能由每阶段自行宣布延期 |
| 明确独立长期/不纳入本轮 | D20 复杂别名/借用风险分析；完整 trait、LSP、自举、YAML、公开 Value/PATCH、完整 Rust 模式能力和稳定外部 ABI，不成为本轮隐性前置 |
| 明确等用户设计 | Proposed serde field adapters 的具体入口与 custom deserialize_with 缺失策略；enum 原位表示迁移的 Go ABI/源比较取舍；方法接收权限/impl 键 U03、引用容器与 any 保留资格的未定表示 U02；select 候选失败语义若扩展问号。均不阻塞其他已定工程 |

局部值字段与暴露局部整槽替换、联合验收及具体标量原生参数均已完成下文限定切片。继续原位 match、跨 module metadata 与常用表达式，同时保留权限边界的工程账。当前结论仍是“有限能力持续补齐”，不是完整 MVP 已完成。

### 局部值字段替换验收（2026-10-02）

| 范围 | 当前证据与边界 |
| --- | --- |
| 独立字段与嵌套/value promotion | 非捕获/非取址局部 struct 值的替换允许后续独立写；slice/map/pointer 整字段替换均验证，经过共享 pointer 或索引不当成独立路径 |
| 旧 alias 与其他字段 | 资格持久化更新；旧 struct 副本、旧字段 alias、兄弟 map/pointer/slice、any 擦除仍拒绝 |
| 控制流 | 多赋值 RHS 快照与左到右写序，分支/循环/range 资格保持；回边及零轮不升级 |
| 隐式取址 | 指针接收者方法调用/方法值和值嵌入按 Go 实际取址处理；已有 pointer receiver 不误判为取 local header 地址，不分析方法 effects |
| 真实验收 | [模块测试](../tests/acceptance/refs_field_flow_test.go)：10 个业务函数，2 个生成模块测试单独 race/vet，24 个拒绝反例；独立审查另 8 个实际 hgo assignment/range 探针 |
| 剩余精度 | 捕获/真实取址根、pointer/索引路径强更新与 switch/select/goto；逐项处理存储或控制流事实，当前拒绝不是获批语言规则，也不要求全程序 alias 分析 |

[RFC §6.18](rfcs/0005-borrows.md#618-局部值字段替换2026-10-02)与[实现记录](../IMPLEMENTATION.md#局部值字段替换与资格保持2026-10-02)同步。最小收口清单的下一项为常用 match：先普通赋值与 `:=` 局部推断，限定已支持的封闭 enum/模式，验收一次求值、仅选中 arm、类型身份、控制流和 `?` 最近函数退出；不以任意运算符/动态调用/完整 Rust 模式全部完成作为这一阶段终点。

联合验收停止条件为：一个同 module 多包业务实际贯通宏、封闭 enum/match、Option/Result/错误链、手动及派生 Default、serde JSON 与读写权限；安装 CLI 在仓库外生成，两次输出一致，生成模块独立 race/vet；失败输入、只读错误、缺元数据和源码位置/诚实 unmapped 有断言。联合测试通过也不能抹去上表、`?` 既列欠账及外部元数据等范围账；移出 MVP 需要明确范围决定。Proposed adapters、U15 bridge、U03 receiver 与 U02 表示、select 失败策略继续单列待设计，不阻塞上述已定工作。

### Match 赋值与局部推断验收（2026-10-02）

单标识符普通赋值、`:=` 与无类型 var 接通；有目标上下文按实际类型赋值，无上下文仅同一 typed type 或同一 Go 默认类型推断，常量/nil按候选类型检查。无公共超类型搜索、自动数字转换或 any 降级。[模块测试](../tests/acceptance/match_assignment_test.go)为真实 CLI→多包 Go，3 项生成模块 race 测试及 vet，15 个负例与两次确定性输出；源级类型补充诊断有原位置，readonly 与用户未初始化引用仍拒绝。

停止条件已按本片完成：跨包具名身份、上下文大整数/shift、nested match/问号、一次 scrutinee/选中 arm、原函数失败退出、scope/goto/direct recover 有执行证据。复杂 LHS/control-header、其余表达式/调用、private不可命名类型仍留工程账；导入引用结果阶段顺序已在下一联合验收修复。[RFC](rfcs/0009-match.md)不将这些自动延期；接下来先做此前固定的联合验收，保留范围账及用户待决项，不宣称完整 MVP。

### 同模块联合业务验收（2026-10-02）

[业务源](../examples/t0_joint/main.hgo)与[安装 CLI 验收](../tests/acceptance/t0_joint_test.go)把 config/store/logkit/入口四包串成 JSON 订单：封闭 enum、两种 match 赋值、Option 缺失/null、Result 问号与 Go cause 链、手动及派生 Default、serde default/skip、条件宏和读写路径都参与真实执行。独立副本字段替换与可写/native 别名更新可见，未许诺全局不可变或独占。

联合业务发现并修复元数据绑定晚于 match 推断的实际误拒。现在先绑定同次构建 registry，再推断表达式和传播；原始签名留作导出，完整权限校验后才还原 Go 调用。只读写入/擦除和跨构建无 registry 仍拒绝，不放宽 D11。

停止条件证据：安装 CLI 离开仓库生成两次逐字节一致；四包生成模块单独 **3 项 race 测试**与 vet；原生二进制仓库外成功输出 `accepted 42 units=3 retry=-1 port=8080 observed=6`，三类失败输入非零退出且无成功日志。四个拒绝子项验证 readonly 写/any、类型错误原始源位置和缺元数据（权限重建诊断诚实标注 unmapped）。独立复验另有跨消费文件 import alias 与共享 slice 反例。此联合项已通过，整体 MVP 尚不能宣布完成：上表的权限精度、传播上下文/性能和范围账仍待逐项关闭，待设计入口仍保持 Proposed。

### 暴露局部整槽替换（2026-10-02）

[真实模块验收](../tests/acceptance/refs_exposed_flow_test.go)接通捕获/取址局部整个值的直接替换：8 个业务函数，生成模块 2 项测试单独 race/vet；14 个负例保留旧副本、旧地址、闭包重污染、同语句调用、间接写序、channel 同步与隐式 range 接收的 readonly。地址生命周期资格与当前值事实分开，未知效应恢复 summary；不扩长期 alias 分析。部分字段/未知效应后精度等仍未完整关闭。

用户已进一步明确：`match mut& enum_value { Var1(mut& inner) => *inner += 1 }` 的原位可写借用是首版必需，不能后置；绑定必须指向原 payload，不能用隐藏共享指针改变值复制语义。具体表示仍需研究取舍，下一在两项真实 bug 修复之后推进。跨模块增强元数据、复杂上下文收窄及新引用 Go 桥接尚未批准任何范围冻结；剩余 guard 不在本阶段删除或缩减。

后续用户纠正已记入 D29–D31：跨 module hgo 库消费恢复增强信息也是所需方向，未批准同模块冻结；`mut& T` 到 Go `*T` 应自然互操作，readonly 不得升级。复杂 match 上下文是工程缺口。以上不由本次权限误拒修复完成，也不再把旧 D11 桥接候选当成拒绝新意图的前提。

### 普通问号私有 bool 误拒（2026-10-02）

[真实 CLI 验收](../tests/acceptance/propagate_private_bool_test.go)修复普通局部值/调用/len/cap 中右臂私有 bool 给左侧未类型化比较提供上下文时的失型。已有静态类型见证在改写前复用，短路、私有类型身份、最近函数失败、defer/direct recover 均保持；生成模块三项独立 race 测试与 vet、两项非法类型负例。无新转换、IIFE、guard 或范围冻结。其他 witness/unsafe 欠账继续留表；原位可写借用 match、跨 module 增强恢复和自然 mut&→Go 指针互操作按 D29–D31 继续推进。

### 具体标量原生写指针调用（2026-10-02）

[真实 CLI 三包验收](../tests/acceptance/native_write_bridge_test.go)接通同型 `mut& T` → Go `*T` 参数，包含跨 hgo 包引用、atomic、普通/定义函数值、变参、defer/go、具名标量/别名、求值一次、原生留存与返回指针、GC 后存活及 readonly 观察合法别名更新。生成模块三项 race 测试与 vet；只读直接/缩窄/再借用、异型元素及未开放擦除位置保持拒绝。

这是 D31 的首个实现切片，D11/U15 的普通调用待批描述已更新。复合元素、定义指针类型、接口/容器/引用 callback 适配及其他转换位置未完成，不以本片限制作为范围冻结。原位 match 等待 enum 表示迁移选择；跨 module metadata、复杂 match 表达式与剩余普通工程缺口继续留账，完整 MVP 尚未完成。

### 二元 match 与简单索引赋值（2026-10-02）

[真实 CLI 多包验收](../tests/acceptance/match_binary_index_test.go)接通 `return 10 + match ...`、`a[index()] = match ...`、二元/短路及既有调用/arm 组合。四项生成模块 race 测试、vet、两次确定性和七个类型/readonly 负例通过。涵盖数组原存储、slice/map 重绑、nil/bounds 在 RHS 后、问号失败不写、defer/direct recover、真实用户闭包退出边界、forward goto、局部/具名/大整数与双 match 类型依赖。

生产使用原函数 prelude，所有编译期 atom 包装均在输出前去除，readonly 不擦除；没有迁移 enum ABI。优化构建五次样本的二元/索引输出与显式 match 对照均零分配，原始值与复现见[研究记录](../tests/research/match_lowering/README.md#production-binaryindex-preludes)，不泛化为任意 match 成本承诺。

本有界阶段完成后交回；原位 match 等待 enum ABI 选择，跨 module metadata 留给下一阶段。更复杂目标/表达式、不可命名结果及已列传播/权限欠账未冻结为延期，完整 MVP 尚未完成。

## 跨 module 引用产物（2026-10-02）

[真实 CLI 验收](../tests/acceptance/reference_external_test.go)创建独立库/应用 go.mod，库生成 Go+metadata，应用经离线 local replace 恢复具体引用权限；库、应用及第三个普通 Go 消费模块分别 race/vet。两次库/应用输出字节一致。alias/reexport、mut& 写入及已有可写别名保持；readonly 返回、字段写入、升级和擦除拒绝。单文件消费者也应用权限绑定。

缺失、版本/包错误、权限摘要、文件内容/选集和依赖漂移均有负例；增强依赖及未marked普通 Go依赖的具名 struct 新增 slice 都使旧产物失效。独立审查的真实 hgo→Go 探针曾复现旧实现绕过，已用传递源指纹及增强 artifact pin 修复并纳入回归。这是编译期产物一致性校验，不是全局不可变或密码学认证；普通 Go 仍可直接写生成指针，移除 JSON 也能构建运行。

最小内部格式与边界见 [RFC 0005](rfcs/0005-borrows.md#621-跨-module-引用产物恢复2026-10-02)。源可用 local replace 子集完成；其他增强协议、纯预编译无源恢复、外部 cgo 输入验证和未实现引用签名继续留账。原位 match mut& 等待 enum ABI 决定，Proposed serde adapters 未批准，完整 MVP 尚未完成。

## 独立 enum 稳定阶段：声明分隔符（2026-10-03）

按最新用户优先级，先宏、再独立 enum、后续指针拆分增强；原位 `match mut&` 与 Copy/move 不作为当前 enum 阶段的前置，未来目标保留，已有引用功能不删除。当前保留 interface＋值型 case。比较/map key 资格、非法零值和自动 Go 入口校验仍需准确契约，本片未改变其行为。

[真实回归](../tests/acceptance/enum_separator_test.go)验证 enum 声明后同线显式分号、混合 type/const/函数、同线多 enum、注释及文件末尾，包含单文件表达式/语句 match 执行和 CLI 多文件生成模块 race/vet。修复只删显式 separator token，保留注释与自动换行；旧 expression-match 假负例已移除。真正未知变体/guard 非穷尽仍拒绝，保留文件定位，不宣称补齐完整 source map。

下一切片为两个独立 go.mod 的 enum 元数据恢复；引用 artifact 已实现不代表 enum 信息已恢复。同 module 或普通 Go API 消费不能代替外部 hgo 变体匹配验收。完整 enum/MVP 稳定仍未宣布。

## 跨 module 公开具体 enum（2026-10-03）

[双模块 CLI 验收](../tests/acceptance/enum_external_test.go)真实生成独立库 go.mod 的 Go+enum/ref artifacts，再由独立 hgo 应用经 local replace 消费：构造、穷尽 match、双 alias/透明 type alias 再导出、函数转发、私有载荷推断及生产者 serde helper 均贯通；另一个普通 Go 模块直接消费 API。库/应用生成两次字节一致，三模块分别 race/vet；非法 nil/typed nil/指针 case/嵌入伪造值在 guard 前拒绝。它不等于所有公开函数都自动校验。

缺失、错包、版本、变体摘要、源码/构建变化均有负例；重新生成私有载荷结构后即便逻辑变体摘要未变，旧下游产物也因依赖内容变化失效。定义新类型和同名不同包不能冒充原 enum 身份。引用元数据组合回归保持通过。

独立审查发现并修复内部 origin 自动导入破坏 Go facade 的问题：合法普通 API 调用保留；不可导入定义包的 alias 变体语法给明确未支持诊断。公开 relay helper 的选择仍留工程账。消费者跨包 derive、外部 Option/Result 家族、单文件外部 enum 语法恢复、无源产物及远程发布未由本片完成，不视为批准延期。当前值型 ABI、比较/零值/入口策略不变，完整 enum/MVP 稳定尚未宣布。

## 跨 module Option/Result（2026-10-03）

[真实 CLI 验收](../tests/acceptance/builtin_external_test.go)用独立库/应用 go.mod 与第三个原生 Go 消费模块，覆盖构造、穷尽 match、`?`、固定/换序/部分 generic alias、复合类型实参、公开 alias 暴露的私有 payload、本包 Error derive 一步 From、cause/typed nil、None/Some(None)、非法动态 case 和嵌套 validator。生产者 JSON helper 另验证 Option 字段缺失/null 与普通值。三个模块各自 race/vet，库和应用重复生成字节一致。

负例包括未登记 instance 的传播、伪同名 family、新 defined 类型、跨家族、非穷尽、外部 From helper 未授权、不可命名私有载荷、generic alias 约束违规、family marker 篡改及 artifact 缺失；移除 artifact 后原生 Go 消费继续执行。独立审查发现并修复类型参数误替换字段名和 canonical helper 擦除 alias 约束两项问题。

协议升级 v2 需重生成，实际 Go origin/实例类型和完整产物指纹仍校验。未完成项：完全不可命名 payload、internal facade 增强操作、消费者跨包 derive、单文件/无源外部恢复、外部 From 注册入口及已列其他 T0 欠账；不把当前限制自动批准延期。比较协议另见[有界提案](rfcs/research/enum-equality-proposed.md)，本工程片不改变比较、零值或全入口校验，整体 MVP 尚未完成。

## 显式 equality 首片（2026-10-03）

[真实 CLI 双模块与 Go 消费验收](../tests/acceptance/equality_test.go)覆盖 scalar/struct/array/enum 显式 PartialEq/Eq、跨模块透明 alias、消费者组合派生、NaN/complex/±0、活跃变体、全局初始化、大整数上下文、`?`、局部遮蔽、nil 例外与左右实参/validator 顺序。库与应用重复生成一致；库、应用、原生消费模块分别 race/vet。原生 Go 仍可直接使用自身 `==`，不是 hgo 静态规则的执行入口。

负例包括缺 PartialEq 的 Eq、float/complex Eq、导入仅 PartialEq 的字段尝试 Eq、slice/pointer 自动协议、新 defined 类型冒充协议、helper 冲突、直接 enum 比较、已知危险载荷的包裹/混合 interface 比较、switch tag/case、map 声明/索引/literal/delete、丢失 metadata 和 scalar bool 结果错误赋给具名 bool。独立审查修复布尔标识符遮蔽、typed bool 丢失及间接比较检查漏口；只有 builtin delete 适用，普通同名函数保留。

此次收口还修复 `type Row [2]Point` 被 enum 类型收集误当作用户泛型的问题；用户泛型仍不因此开放。[内部执行测试](../internal/equality/lower_test.go)补 native 非 bool equals 方法、传播后 native receiver、卫生绑定等；[单文件入口](../internal/frontend/equality_test.go)覆盖空格分隔的 scalar 方法调用。

剩余：blank/embedded struct 字段、Option/Result 条件泛型比较协议、容器派生实现（nil/空区别已由 D33 确认）、manual 接口、pointer/chan/interface 协议、私有 canonical 的跨包 equality alias、其他 derive 协议与既列 T0 工程账。Eq 不新增 map key 许可、不证明任意手写函数的等价关系；不进行任意 interface 擦除后的全局值流分析，也不将等值 helper 的验证扩大到所有公开入口。完整 enum/MVP 尚未宣布完成。

最新 [D33](design/decisions.md) 已确认 nil/非 nil 空 slice/map 不相等；[契约探针](../tests/design/enum/equality_contract_test.go)只验证参考规则，不计作 hgo 容器执行证据，不隐式改 Option 状态或 serde null/[]/{}。

## 容器 equality 派生（2026-10-03）

[实际 CLI 模块验收](../tests/acceptance/equality_test.go)扩展为 slice/map 的 nil/空、有限嵌套 struct/array/enum、稳定 bool/整数/string/结构键、缺失键与零值区分、Option.None/Some(nil)/Some(empty)、float/complex NaN 仅 PartialEq、访问元素的非法 enum/typed-nil，以及 struct/container 短路与外层 enum 先验证的区别。继续验证跨 module 透明 alias/组合派生、原生 Go helper、重复生成确定和三个生成/消费模块各自 race/vet。

有限 array/slice/map enum validator 投影现已接通；原 Option 嵌套容器的负例迁到 [真实 serde 执行验收](../tests/acceptance/option_serde_test.go)，并在 [lowering 单元执行](../internal/enums/builtins_test.go)覆盖 Result 内 Option 容器的非法输入先于 match arm 拒绝。另将 grouped type、普通 enum 载荷与传播/projection 的旧拒绝迁为实际生成模块，覆盖非法内层值在 guard 或 `?` 继续执行前拒绝；这些生成模块独立 race/vet。这不是以纯 Go 机制测试代替 hgo 执行。

边界：递归容器/环相等、导入具名键的原生一致性证明、手写比较、blank/embedded 字段及 pointer/chan/interface 协议仍待收口；本片没有新增直接匿名 slice/map/Option/Result equals 入口，也不实现 HashMap。原位 borrow/指针拆分是后续独立阶段，不作为此次 enum 容器派生的前置阻断。完整 T0 MVP 尚未宣布完成。

独立复审修复同 import 别名指向不同包时的错误身份解析，以及同一家族不同具体实例的 validator 查找；公开泛型载荷按真实 origin/参数递归命名并验证类型身份。真实独立模块复验包括外部 Option[[]Option[int]]，私有不可命名形状仍明确诊断，不输出错误私有拼写。

## 手写具体 equality（2026-10-03）

[真实三模块 CLI 验收](../tests/acceptance/manual_equality_test.go)包含多文件 Person 手写按 ID 相等而忽略 Name、派生 enum/struct/slice/map 值调用用户协议、用户显式 NaN wrapper 与普通浮点 PartialEq 对照、manual Eq + 派生 PartialEq 的稳定键证明，以及独立 Go 消费公开 helper。非法 enum nil/typed-nil/指针 case/伪造动态值在用户体前拒绝；两实参先求值、左 validator 优先，native Go helper 同样受检。

函数体保持原 frame，实际 `defer .equals` 的直接 recover 继续有效。普通手写递归与经过另一函数的递归、全局初始化、局部同名函数/类型及反射名称纳入执行验收；不以“循环容器尚未定义”拒绝普通用户函数递归。重复生成保持一致。重复 impl、同协议 manual/derive 冲突、错 self/参数/返回类型、别名目标、额外成员、helper 碰撞、缺 PartialEq、误用 derive Eq 证明手写业务函数及手写 map 键均拒绝。

[前端测试](../internal/frontend/manual_equality_test.go)验证跨文件注册、冲突和原始 body 错误行号；注册汇总位于宏展开之后，但上游宏模板尚不接受 impl，本片不声称宏生成比较 impl 已可用；metadata v4 记录各协议 derive/manual 来源，alias 与定义包一致，身份与精确 Go helper 签名仍核验。本片不新增 Self 简写、借用参数、泛型 impl、外部目标、自动循环图比较或完整 trait；手写函数的 Eq 性质由作者负责。完整 T0 仍有已列工程账，不由本阶段宣称完成。

## 宏 builtin 首片：len/cap（2026-10-03）

模板可直接写 `len($x)` / `cap($x)`，无需 wrapper。定义端局部/函数绑定优先，变量、类型与 import 不能误当 builtin。编译器标记在 DSL 降低后恢复原生 Go 调用，保留常量上下文、数组/数组指针的不求值规则、实参次数与 `?` 所在 frame。调用端局部变量/常量与 import 按绑定改名；struct 字段键和 map 表达式键区分处理。

[单元执行](../internal/macros/builtins_test.go)与[真实生成模块验收](../tests/acceptance/macro_builtins_test.go)覆盖嵌套/跨包宏、局部及 import 遮蔽、定义端同名声明、常量/数组、None/Some 传播次数，以及生成模块 race/vet。调用包级同名声明和实际覆盖调用点的局部 named type 目前明确诊断；无关作用域的类型名称保持不变。它们是工程边界，不自动视为获批延期。其他 builtin（含 append/make/new）未开放，不宣称完整 Rust 宏或全面卫生已完成。零值/公开入口契约未修改。

## 跨包已登记 Default 字段（2026-10-03）

目录/模块编译恢复生产包显式登记的具体 Default：消费 struct 的 `m.E` / `m.Settings` 字段可调用生产包 helper，包含透明 alias 和手动实现。没有登记的同名 `DefaultT` 函数、新 defined 类型不继承协议；不猜 helper 名。schema v5 记录目标、canonical 定义、helper 及 derive/manual 来源，核验真实 named 身份与精确非泛型 `func() T` 签名，alias 的来源必须与定义包一致。旧产物需重新生成，内容、依赖及 marker 绑定继续核验；Default-only 包也写出 artifact。

[真实 CLI 三模块验收](../tests/acceptance/default_external_test.go)覆盖私有字段的业务默认值、字段调用次数、跨文件同名 import、透明 relay、宏 len 的 import 改名组合、serde skip 共用 Default，以及独立原生 Go 消费；三个生成/消费模块分别 race/vet，重复生成一致。[协议反例](../internal/frontend/default_artifact_test.go)覆盖 helper 变量/泛型/参数/错误返回、假注册、new defined、alias 来源改写及陈旧 producer。原跨包 Default 字段负例已迁为[真实源包正例](../tests/acceptance/manual_default_test.go)，外部目标 impl 仍拒绝。

struct 仍要求所有字段有协议；enum 仅选中默认变体要求 payload Default。此次不放开 enum 的外部载荷类型限制，不新增跨包 Clone/Debug/Serialize/Deserialize、完整 trait 或字段 adapters。internal-origin 不可直接导入的协议暂不可用，普通公开 Go API 保留。单文件外部协议恢复、容器默认语义及零值/公开入口契约不在本片修改；普通 Option 缺失/null 到 None 仍独立于 Default。

## 跨包已登记 Clone/Debug 字段（2026-10-03）

目录/模块模式支持消费生产包明确派生的具体 struct/enum Clone、Debug。qualified 字段可经生产者公开透明 alias 转发，跨文件相同 import 名按实际包路径区分。内部 metadata v6 在 v5 校验链上新增 Kind/目标/helper/derive 来源，检查 canonical named 身份及精确 `func(*T) T` / `func(*T) string` 签名；alias 不得凭同名函数或正确签名自行增加协议。内容、marker、依赖产物及 canonical 注册均须一致，旧生成模块需重新生成。

[真实 CLI 三模块验收](../tests/acceptance/field_protocol_external_test.go)覆盖具体 enum/struct、透明 relay、跨文件/包名差异、array/slice/map 嵌套元素协议、nil/空区别、普通 hgo/Go 赋值仍共享容器、显式 Clone 的独立副本及不保留别名拓扑。容器类型以包级 type alias 绑定，避免生成的 src/item/out 等局部名字截获源 import。Debug 保留生产类型/变体名、字段名、字符串和 map key 引号及内容（含私有字段），不自动脱敏；无需消费者反射或绕过字段可见性。三个生成/消费模块分别 race/vet，生成结果重复一致，原生 Go 可调用公开 helper。

[协议反例](../internal/frontend/field_protocol_artifact_test.go)核验参数/返回类型、函数变量/泛型/variadic 拒绝、重复/未知/手写来源拒绝、relay 伪造另一 Kind 及陈旧 producer；CLI 对摘要篡改和源文件变化在消费者 import 位置报错。手写 Clone/Debug 尚无已实现登记入口，本片仅 derive 来源；消费者本地 `type Local = m.T` 字段的协议转发仍未接通，不能把该工程缺口说成类型身份不同。不可直接导入的 internal-origin 协议仍不可用，普通 facade API 保留。外部 enum 载荷类型范围、pointer/interface 自动协议、完整 trait、serde 和零值/公开入口验证不在本片扩展；borrow/Copy/指针拆分不作为本片前置条件。

## 跨包已登记 Serialize/Deserialize 字段（2026-10-03）

目录/模块模式按实际 import 路径恢复生产者显式 derive 的具体 struct/enum serde 协议，消费 struct 的 qualified 字段及既有 array/slice/map/pointer/Option codec 递归调用生产者 helper；公开透明 relay alias 可转发，多文件同名 import 不混淆。Serialize 与 Deserialize 独立登记/要求，不通过 helper 名、反射、Go 方法集或 encoding/json 自动实现兜底。

复用 metadata v6 的 FieldProtocols 记录，不新增 schema 字段或版本；Kind 扩展为 Serialize/Deserialize。核验 canonical named 类型、derive 来源、alias 定义包登记，以及精确非泛型 `func(serde.Serializer, *T) error` / `func(serde.Deserializer) (T, error)`，其中 core 类型必须与实际 serde 包身份一致，error 必须是内建 error。内容、依赖、marker 校验保持。旧 v6 产物若缺少对应协议记录必须重新生成；不能把旧 helper 的存在当作登记。

真实 CLI 三模块验收覆盖 producer struct/enum、透明 relay、单向协议、跨文件导入、容器和 Option、原生 Go helper 消费，三个生成/消费模块分别 race/vet。验证嵌套 field/variant path、Range/Syntax 的 Go cause、未知变体、生产者 deny_unknown_fields 与消费端忽略额外字段、忽略值深度预算、非法 enum nil/typed-nil/指针 case、nil 容器编码为空且解码为非 nil 空、普通 Option 缺失/null 为 None。没有把它们改为 Default 或其他新语义。重复生成一致；伪 helper、新 defined 类型、缺方向、篡改及陈旧产物拒绝，产物错误定位消费 import。

边界延续：消费者本地 `type Local = m.T` 字段转发尚未接通；手写 serde 登记、单文件外部协议恢复、不可直接导入的 internal-origin、普通 enum 外部 payload 类型范围均未借本片扩大。字段 adapters 的入口/缺失策略仍 Proposed，完整 trait/native bridge、Value/PATCH/YAML、公开函数 enum 参数入口验证不在本片实施。完整 T0 MVP 尚未据此宣告完成。

## 消费者本地透明 alias 的外部协议恢复（2026-10-03）

消费者包级 `type Local = producer.T`（包括多级 alias、native `.go` 中声明、生产者公开 relay）现统一恢复已登记 Default、Clone、Debug、Serialize、Deserialize 和 PartialEq/Eq。使用保留各文件 import 作用域的 Go 类型解析，只对真正 alias 的 Unalias 结果与已验证 canonical 类型作 Identical 匹配；新 defined 类型、alias 指向新 defined 类型、同名/相同底层结构不继承协议。非类型语句留给既有最终完整包检查，不从源码名称猜 helper。

沿用 metadata v6 及既有签名、来源、内容、依赖、marker 校验，无新 schema 或旧数据宽松恢复。协议绑定放在包级，hgo 文件没有源 import 时也能在 package 声明后插入生成 import。收集后的真实 TypeSpec/Imports 保留 `=` 和宏卫生改名；不新增宏模板 type selector 语法。

真实三模块验收 `tests/acceptance/local_protocol_alias_test.go` 覆盖各协议、生产者 manual Default、多级/重复导入/native-only alias、不同包相同导入名、同名局部类型拒绝、同名局部变量、容器与已支持的 Option serde/equality、独立 Go helper 消费、重复生成一致、新 defined/假 helper 拒绝及错误摘要定位原生 consumer import。生产/消费/Go消费模块分别 race/vet；独立审查另验证宏 import 改名与 alias 共存。此处 Option 不宣称 Clone/Debug 已实现。

边界：本片恢复外部 canonical 的已登记协议；同包原始类型经本地 alias 的 Default/Clone/Debug/serde 转发仍未接通，既有 Eq 已支持。单文件外部协议、不可直接导入 internal-origin、其他载荷/字段形状和未实现协议入口继续留账；不是透明 alias 身份发生变化，也不是获批延期。公开 enum 函数入口验证仍待用户决定，未混入指针阶段、完整 trait/manual serde 或字段 adapters。

### 独立 enum 当前最小剩余清单（2026-10-03，alias 阶段后）

- 协议组合工程账：同包原始类型的本地 alias 转发（Default/Clone/Debug/serde；Eq 已支持）；Option/Result 未实现的 Clone/Debug 等组合及 blank/embedded 字段仍须逐项收口或明确首版范围。
- 外部恢复工程账：单文件外部 enum/协议恢复、既有限制外部 payload、不可命名载荷/internal-origin 的增强操作；公开 relay 不能代替所有这些能力。无源/远程发布不由当前 local replace 验收推导为完成。
- 常用 match 仍有复杂目标/表达式上下文长尾，按已定求值与最近函数传播规则验收，不扩大成完整 Rust 模式系统。
- 待决定：公开函数直接 enum 参数是否在函数体前验证并对非法值 panic，None 的合法性按 builtin Option；本轮未实施该待答复契约。

以上仅为独立 enum 最小账，不重新加入指针阶段；手写 serde/adapters/完整 trait 也不借此变成已批准设计。工程限制不能自行归为批准延期，完整 MVP 尚未宣布完成。

## 同包已登记类型的透明 alias 协议恢复（2026-10-03）

同包具体原始类型的包级透明 alias 现与外部 alias 共用 `bindLocalProtocolAliases` 的 Go Unalias/Identical 身份解析。Default/Clone/Debug/Serialize/Deserialize 从既有 models/manual Default 显式登记恢复，不逐派生新增 alias 分支；既有 Eq 继续按真实类型身份工作。支持跨文件、多级 alias、native `.go` alias 和单文件编译。新 defined 类型（即使底层相同）、普通同名 helper 不产生登记；alias 上再 derive/impl 及重复实现按既有规则拒绝。

本地 helper 直接引用登记对应函数，不新增函数值全局初始化依赖；这不撤销现有 `checkDefaultCycles` 的静态递归拒绝策略，也不声称递归 Default 已支持。手写函数签名/返回、冲突、最终完整 Go 包仍照常检查。metadata 保持 v6，外包签名/来源/摘要校验不变。

真实证据 `tests/acceptance/same_package_alias_test.go`：独立目录生成模块与单文件生成模块分别 race/vet，执行 manual Default、enum 默认 payload、Clone、Debug、serde 往返、equality、容器独立副本、反射身份及局部同名 helper/变量；反例覆盖新 defined、无登记 helper、alias impl/derive、重复 Default、错误方向和错误手写返回类型。外部 alias 三模块继续回归。blank/embedded 字段没有悄悄新增跳过/展开/命名规则。

### 独立 enum 精确剩余（2026-10-03，同包 alias 后）

本地/外部具体已登记类型的包级 alias 转发已关闭，不能再列作待做。其余最小账为：

1. **已复现 match 限制**：`func F(x E, g func(int) int) int { return g(match x { E::A(n) => n, E::B => 0, }) }` 仍拒绝 embedded expression match；`func F(x E) (out int) { return match x { E::A(n) => n, E::B => 0, } }` 仍要求一个 unnamed result。直接已知函数参数调用、binary/短路及简单 index 目标已有验收，不重复列为未实现。
2. **协议组合**：`#[derive(Clone,Debug)] type S struct { V Option[int] }` 尚无对应具体派生支持；Result 的未实现组合同样须按实际资格验收。已有静态 Default 递归限制仍在，例如 DefaultItem→DefaultWrapper→DefaultItem 即便有终止条件也拒绝，不算本轮新增能力。
3. **外部恢复边界**：单文件外部 enum/协议、受限的外部 payload/不可命名载荷及 internal-origin 增强操作尚未全部接通，当前模块 local replace 正例不能代替这些验收。
4. **字段规则需明确到例子**：`type S struct { _ int; X int }` 的 blank 字段是否/如何参加各派生，`type S struct { Inner }` 的 Debug 字段名与 serde 嵌套/展开规则未在当前 RFC 明确；继续明确拒绝，不擅自跳过或沿用 encoding/json 自动展开。
5. **公开入口待答复**：直接 enum 参数在公开函数体前验证、非法值 panic 的契约未实施，Option None 合法性也不由本轮改动。

这些工程/设计账不自动成为批准延期；不混入指针阶段、full trait/manual serde 或 Proposed adapters，也不宣称完整 MVP 已完成。

## 已登记具体 Option/Result 的 Clone/Debug 组合（2026-10-03）

最小旧失败例 `#[derive(Clone,Debug)] type S struct { V Option[[]int] }` 及对应 Result 字段现已接通。既有 Option serde/equality 与普通 enum Clone/Debug 不被当成此项证据；新实现经统一字段协议 resolver 分派，并用本地 lowering 或已验证跨包 family registry 中 validator 的精确参数类型核对真实 origin 和全部具体参数。类型名相似、同 family 不同具体实例、新 defined 类型均不能冒认协议；metadata 保持 v6，无新 schema。

Clone 在通过既有 validator 后只复制活跃分支：Option None 无载荷操作，Some 按载荷 Clone；Result 只处理 Ok 或 Err 实际载荷。静态资格仍要求所有可能载荷具备所需协议，不能因某个调用当前是 Ok 而放过无 Debug 的 Err 类型。递归复用既有字段/容器协议，普通 Go 赋值与别名拓扑规则不变。Debug 明确展示 `Option::None`、`Option::Some(nil)`、`Option::Some([])`、`Result::Ok(...)` / `Result::Err(...)`，保留字符串引号、nil/空区别及载荷内容。

Default 仅在实例身份确认是已登记 Option 后返回 canonical None；不要求载荷 Default，也不要求私有载荷可命名，不实例化无用 Some constructor。Result 继续没有隐式默认值；普通 enum 显式选中变体的既定 Default 规则不改。非法 nil Result、typed-nil/指针 case/伪造动态 case 仍按现有 validator 拒绝；没有扩大公开函数入口验证。

`tests/acceptance/sum_clone_debug_test.go` 提供真实单文件生成模块与独立 producer/consumer/Go-consumer 三模块 race/vet，覆盖深 clone、嵌套 sum、活跃分支、None/Some(nil)/Some(empty)、Err nil/空 map、跨文件/多级/native-only alias、反序 alias 参数、外部具体 Item 载荷、私有载荷 Option Default、不同实例拒绝、new defined/缺载荷协议/Result Default 反例，以及错误 artifact 定位消费 import、重复生成一致。跨文件收集保留必要 validator/constructor 函数签名；基本类型与容器优先按结构拼写，避免无关 slice alias 污染协议选择。独立审查对上述两问题修复后分别执行真实生成模块 race/vet。

边界：Clone/Debug 仍要求载荷的已有协议和可访问类型表达，未新增 pointer/interface 自动协议、手写 Clone/Debug、完整 trait 或递归对象图系统。blank/embedded 字段、公开 enum 入口契约不变。本片完成后先交回；命名返回 match 与动态已知类型函数参数 match 是下一独立工程片，未在此实施。

## 命名返回与静态类型已知的函数变量 match（2026-10-03）

两个旧最小失败例现已接通：`func F(x E)(out int){return match x{E::A(n)=>n,E::B=>0}}` 与 `func F(x E,g func(int)int)int{return g(match x{E::A(n)=>n,E::B=>0})}`。命名单结果从真实用户函数签名获得类型，避免局部同名类型捕获。arm 先求值到私有 block 内的临时槽，普通 return 再写命名返回槽；defer 观察/修改最终槽，arm panic 不提前改槽，direct recover 保持用户帧，goto 不跨生成声明。

函数变量调用仅接受标识符、静态已知非 variadic 签名及精确 arity。类型检查沿实际 enclosing call 获取参数类型并确认变量对象，不复制同名包函数的参数拼写，不执行类型 witness。既有 propagation 求值机制先保存函数值，再按顺序求实参；实参中重绑 g 不改变已取出的函数值，nil 函数在参数求值完成后才触发调用 panic。scrutinee/guard 保留一次求值，`?` 失败仍返回最近用户函数，跳过后续实参与函数调用。编译器暂存 atom 在 Go 输出前消除，不新增 IIFE/反射/类型擦除。

`tests/acceptance/match_named_dynamic_test.go` 为真实 CLI→Go 模块 race/vet 验收，含确定性、返回槽/defer/panic/goto、direct recover、named 类型遮蔽、函数值捕获、参数 uint64 上下文/shift、局部/statement/嵌套组合、nil 时机、Result 失败/defer/用户闭包，以及负例原 match 行列。旧 named return/dynamic argument 及四条遮蔽/括号/分组函数变量拒绝测试已迁入正向执行验收，未删除其场景覆盖。独立审查另验命名槽旧值、首实参重绑函数变量与包函数同名遮蔽。纯 Go 设计探针单独报告，不替代这些执行证据。

该片之后的调用形状缺口已由下节统一签名组继续接通；多结果 `return match` 仍未实现，参数数量、溢出和不兼容类型仍正常拒绝。这些是当前实现边界，不自动批准延期。blank/embedded 字段、公开 enum 入口、Proposed adapters 不改，整体 MVP 未宣布完成。

## 统一静态调用签名与 match 参数（2026-10-03）

`tests/acceptance/match_call_signatures_test.go` 接通函数字段/直接方法 `s.F(M)`、返回的函数 `factory()(M)`、variadic `g(M)`/固定参数加 `g(prefix,M,rest...)`、展开参数本身为 `g(M...)`，以及原生 Go 声明的显式 `call[int](M)`（M 为完整 match 表达式）。同文件静态函数、跨文件/跨包函数、函数变量、方法表达式共用实际 Go 签名和参数身份；不再扫描同名函数或复制其参数类型拼写。variadic 普通尾项取元素类型，`...` 取 slice 类型，Go 自身仍执行展开/调用；禁止将额外 variadic 元素与 `rest...` 混合的非法 arity。`.hgo` 用户泛型定义仍在 enum 前端明确拒绝，不能与原生 Go generic 调用支持混淆。

函数值沿既定规则在参数前保存一次；nil 函数直到调用时才 panic。直接方法不能变成提前创建 method value：先执行 receiver 中调用、receive、短路运算，普通读取、索引、类型断言和隐式解引用留在最终直接调用。为提前执行某个 receiver 调用所必需的操作仍随之执行，例如 `a.(*S).Get().Method(M)` 必须先断言才能调用 Get。隐式取址继续由 Go 对原 receiver 判定，原本不可寻址的函数返回 struct/array 元素不会因编译器临时变量而获得资格。`?` 失败直接返回最近用户函数，不调用最终 callee；direct recover 不增加用户调用帧。只读到原生可写指针/接口/方法的擦除仍拒绝。

Go 规范 Order of evaluation 只规定调用、接收和逻辑运算等的相对次序；普通读取/断言/索引相对实参调用并非全部规定。独立矩阵发现同一 native receiver 断言在默认与 race 构建中可出现不同先后，因此验收分开验证：确定的调用顺序、hgo 一致的 late receiver 规则、原生未规定次序的允许结果。不把某次优化器时点伪装成语言保证，也不按指针/值断言逐条模拟 Go 优化器。

真实 CLI 输出覆盖独立生成模块 race/vet、输出确定性、callee/receiver 一次求值、值/指针 receiver、nil receiver/interface/callee、嵌入 receiver、索引及断言、私有名义类型的公开 alias、scope shadow、变参 nil/empty 和 backing slice 身份、原生显式 generic 的上下文/溢出、`?`/defer/recover、goto 和权限负例。旧 selector/factory/variadic/generic-call 与静态函数被取值的拒绝用例已迁为正例，不重新添加为缺口。独立审查修复 method-value 提前检查后通过真实 race/vet。

优化构建探针（非 race；Go 1.27.1，单 CPU，100ms×3）中，方法、slice 展开、显式 generic 的真实 hgo 输出均为 0 B/op、0 allocs/op；中位数约 1.963/1.706/1.655 ns/op，内联原生 Go 对照约 0.5635/0.2835/0.2828 ns/op。微基准可被内联/常量折叠，不代表业务性能等价，记录见 `tests/research/match_lowering/call-signatures-2026-10-03.txt`。原生 receiver 顺序研究属于机制探针，未替代真实 hgo 执行证据。

仍明确未接通：多结果 `return match`；完全无可见类型拼写的私有调用参数（如 `m.Hidden()(match ...)`）；既有 unary/composite/block-arm 等非本组表达式上下文。非法 arity、常量溢出、不可寻址 pointer receiver 和权限擦除属于应拒绝输入，不算功能欠账。带编译器指令的本地函数保持既有明确诊断，不擅改调用的逃逸/活性约束。blank/embedded 字段、公开 enum 入口验证、serde adapters Proposed 均未在此决定。

## 独立 enum 现阶段交付验收与待决清单（2026-10-03，统一调用组后）

这是可供阶段交付审查的有限业务闭环，不是宣布完整 T0/MVP 或执行发布。后续发布还须使用该提交的最终全树 race/vet、设计检查和生成模块日志；不能把纯 Go 机制探针替代 hgo 验收。

| 已有真实执行验收 | 主要证据 |
| --- | --- |
| 闭合 enum、名义类型身份、构造、穷尽 match、guard 与使用点非法值拒绝 | `enums_test.go`、`enum_external_test.go` |
| 独立 go.mod 间具体 enum/Option/Result 恢复、透明 alias 和原生 Go 消费 | `enum_external_test.go`、`builtin_external_test.go` |
| Default/Clone/Debug/serde JSON 的显式登记、跨包及同包 alias；Option 缺失/null 与 Default 分离 | `default_external_test.go`、`field_protocol_external_test.go`、`serde_external_test.go`、`local_protocol_alias_test.go`、`same_package_alias_test.go` |
| 具体 Option/Result 的 Clone/Debug，None/Some(nil)/Some(empty)，Option None Default | `sum_clone_debug_test.go` |
| 显式 PartialEq/Eq 派生、手写具体 equality、容器内容比较 | `equality_test.go`、`manual_equality_test.go` |
| 命名单结果和统一静态调用签名的 match 参数，传播目标、求值及权限边界 | `match_named_dynamic_test.go`、`match_call_signatures_test.go` |
| 与宏、错误链、Default、JSON 和既有读写权限联合业务执行 | `t0_joint_test.go` |

仍有工程边界，不能默认为已批准延期：多结果 return match、未接通的 unary/composite/block-arm 组合；完全不可命名载荷、单文件外部增强恢复和不可直接导入 internal-origin 的增强操作；静态 Default 递归资格仍保守拒绝；手写 Clone/Debug/serde 登记尚无入口。独立 local-replace module 成功不等于已经验收无源产物分发。循环容器 equality/命名 map 键证明也不能由有限容器正例推导为已覆盖；新的循环图规则需单独设计，不能自行引入 visited 语义。

需用户明确的既有问题：

1. blank 字段参与哪些派生；embedded 字段的 Debug 命名与 serde 嵌套/展开规则。
2. 是否在公开函数体执行前验证直接 enum 参数，非法值是否 panic。当前 match/协议 helper 的使用点验证不等于统一入口验证；None 合法性沿既定 Option 协议。
3. serde field adapters 的具体入口、自定义 deserialize_with 的缺失策略仍为 Proposed；普通 Option 缺失/null→None、显式 default/skip 才使用 Default 已经确定，不重新列为待决。

本轮没有修改这些契约，也未前置完整 trait/Rust、指针拆分、借用或复杂别名分析来充当 enum 收口条件。


## 显式 Go Result 宏验收（2026-10-04）

D36：普通 `go_result!(T,call)` 与保值 `go_result_keep!(T,call)` 接通；error-only 使用单参数和 struct{}。真实执行入口为 [go_result 示例](../examples/go_result/README.md)，自动化为 [TestGoResultMacros / TestGoResultIndependentModules](../tests/acceptance/go_result_test.go)。

| 核对项 | 证据/结果 |
| --- | --- |
| 真实 hgo 到 Go | CLI build/transpile，生成模块单独 test -race/vet；示例 CLI run 和生成 go run -race 输出 next 42、parse error true、partial 3 abc true |
| Reader/解析 | n>0,error 同时保留；普通版明确只保留 error；缓冲区、次数、原始身份、errors.Is/As、typed-nil（含 error-only）断言 |
| 顺序/帧 | 工厂/参数调用一次、variadic、原生泛型、内层问号失败后不调用原生函数、defer清理、直接 recover、panic身份、defer/go实参先求值 |
| 类型/卫生 | T 和 error 精确身份；transparent alias、包初始化、重复输出确定性、局部 error/nil/Result/type Result 遮蔽；非法arity/具体error指针/非call/错误T拒绝 |
| 传播 | 普通error的已有唯一From成功；保值同型载荷问号保留Value/Err；直接到Result[U,error]拒绝，不合成Error/Unwrap/From |
| 导入 | 同模块源码跨包宏成功；两个独立go.mod恢复Result metadata并执行match/问号、消费原生alias；外部预编译宏导入明确拒绝 |
| 审查 | 独立代码审查发现Result局部遮蔽，修复并独立复验变量/type两例；另有独立原生泛型+内层问号的真实生成race探针 |

边界仍包括显式 T、包作用域可命名载荷、单非 error 返回、既有受限宏模板/问号上下文、预编译宏规则不可导入以及部分 generated/unmapped 诊断。这些是明确记录的工程边界，不是默认为用户批准的延期。没有新增隐式转换、tuple、Residual/Try、泛化 From 或指针 ABI；本片不代表完整 T0 MVP。

本片最终验证环境为 `go1.27.1 linux/amd64`：`go test -race -count=1 ./...` 全树通过（acceptance 294.974s），`go vet ./...` 通过。`python3 scripts/check_design.py` 的文档结构及 13 个 Go 机制命令通过；最终文档复核为 66 Markdown 文件、890 本地链接/锚点、20 RFC。生成模块 race/vet 由上述接受测试单独执行；README CLI run 与生成 `go run -race` 均实跑得到三行预期输出。独立审查修复并复验后无剩余阻断。

首次全树 race 因 `/tmp` 满导致链接器 `no space left on device`，不计通过；将可再生成缓存从 `/tmp/goxide-cache` 迁到 `/workspace/goxide-cache`（保留兼容 symlink），并将 TMPDIR 指向 `/workspace/go-result-tmp` 后才获得上述完整成功结果。没有删除仓库/用户数据，也未修改构建语义或跳过失败测试。


## 静态 T/E 推导验收补充（2026-10-04）

D36 后续：入口无需 T，从 `(T,E)` 或 E 推导具体 Result；保留显式 T 兼容，E 实现 error 且 nilable，按原 E 判断 nil。证据为 [真实 CLI 推导验收](../tests/acceptance/go_result_inference_test.go)，不是纯 Go 机制探针：

- Atoi、Reader 部分读取、单 E、具名接口、指针、slice/map/func/channel，保留 typed nil 与错误身份。
- 保值结构与精确 From 边界、求值次序、recover、嵌套问号/泛型/适配器、跨文件初始化两种顺序。
- 独立模块公开 alias、metadata/match/传播、具名容器内外部 enum 校验；不可命名私有 T/E 给出明确诊断。
- 原显式形式继续验收；全树 race/vet 与生成模块 race/vet，官网单文件 CLI/WASM 差分单独记录。

这不关闭此前 T0 其余条目，也不将无公开类型写法、预编译宏或待批准 serde adapters 视作已获批延期。

本轮验证完成：`go test -race -tags hgoweb -count=1 ./...`、`go vet -tags hgoweb ./...` 通过；新增推导验收最终另跑 `go test -race ./tests/acceptance -run '^TestGoResultInference' -count=1`，内部生成模块独立 race/vet 通过。设计检查通过 76 个 Markdown、23 个 RFC 及 13 条 Go 机制命令；后者不计作 hgo 执行证据。官网 5 示例、8 适配正反例 CLI/WASM 差分通过，记录见 [网站集成](../website/INTEGRATION.md#te-推导分支验证2026-10-04)。两位独立审查者复核无剩余阻断，包含跨文件同名 import 指向不同 enum 包的额外真实执行；该回归也已纳入模块验收。


## 2026-10-04 共享 std 接入阶段：仍未完成 MVP

| 分类 | 当前事实 / 下一步 |
| --- | --- |
| 已实现并有本阶段执行证据 | std透明alias、schema7 origin/owner分离；独立模块直接互赋与`?`；公开facade可达validator；缺参构造与pattern；原生alias的Clone/Debug/Default/Eq。`TestSharedStd*` 含生成模块独立race/vet。 |
| 必须完成 | std/serde实际repo提交及精确依赖、CLI与既有fixture依赖迁移、schema7组合负例补齐、serde core/json同一身份、网站export data及CLI/WASM、可读性合流和最终全树race。 |
| 已有实现待最终组合验证 | 宏、只读/可写权限、From/清理、serde default/skip/普通Option缺失/null等前期能力，必须在新共享依赖闭环重跑，不能沿用旧ABI测试证明完成。 |
| 明确不纳入本阶段 | 复杂别名/借用分析、完整Rust/trait能力、YAML、Value、PATCH和完整native bridge。 |
| 仍需新设计决定 | serde-field-adapters-proposed 的入口与自定义deserialize_with缺失策略；本阶段不依赖它。 |

独立审查实际发现并修复：pattern扫描误入arm构造器的重复展开、延迟AST节点词法作用域、公开owner被internal条目整体屏蔽、表达式match构造器未取得上下文及整数常量过早默认。相关正反例已进入共享std测试；typednil与引用投影另有独立真实CLI复核。

全树race已尝试但未通过：既有临时模块尚未声明新std依赖，低层测试importer与历史预期尚需迁移；另见项目fixture的ziphash/离线module规划失败，尚不能归因于本改动。不得将这些失败标作已批准延期。根go.mod未写入伪造的可下载std版本或本机绝对replace。


本阶段可获取提交：生产接入 `7cb7f0b825b71bfefdb6160600f55bc4ba0acf5d`；serde隔离候选交接 `57c858ee1d1334e52fedf144dd7461785bece65e`，均在 `feat/shared-std-sums` push/fetch核对。后者只允许按provenance白名单迁入独立serde repo，不包含编译器路径切换，也不包含项目manifest；`goxide.toml`由工具链任务统一实施。

验证结论：`go test -race ./internal/frontend -run 'TestSharedStd|TestEnumFamily' -count=1`通过，并在测试内分别执行producer/consumer生成模块race/vet；最新真实CLI多包转译输出另行race/vet通过。`go vet ./...`通过，完整设计检查13条Go命令通过（机制证据），文档链接/RFC检查通过。全树`go test -race ./...`仍失败，最近整树快照为98个顶层失败，涉及8个包；主类为std依赖缺失/低层importer未迁移，另外保留项目ziphash/离线规划调查。整树失败后修复显式分号package的重复分隔符并重跑上述重点race/vet；没有把重点通过冒充整树绿灯。

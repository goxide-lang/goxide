# 可运行的 hgo 语言子集

此实现分支从 `v0.0.0-mvp-doc` 开始；冻结标签保持不变，本文件只描述实现分支。
这是具体类型、单文件、Go 宿主语法上的可执行子集，不是完整 hgo 编译器、trait ABI 或 LSP。

## 运行

需要真正的 Go 1.27+ 编译器，无第三方 Go 依赖。若系统 `go` 是其他程序，先把编译器目录放到 PATH，或为 CLI 指定 `-go /path/to/go`。

```sh
go test ./...
go run ./cmd/hgo run examples/prototype/order.hgo
go run ./cmd/hgo run examples/prototype/log_error.hgo
go run ./cmd/hgo transpile -o /tmp/order.go -source-map /tmp/order.map.json examples/prototype/order.hgo
go run ./cmd/hgo build -o /tmp/order examples/prototype/order.hgo
```

`transpile` 输出生成 Go，`expand` 输出可检查的展开结果；`-expanded` 可另存展开文件。
`run/build` 使用临时模块和本地 runtime 替换，关闭自动工具链和依赖下载。
从仓库之外使用时指定 `-runtime /path/to/goxide`。单文件输入不是多包构建系统；现有输出文件可被覆盖，输入及输出互相冲突则拒绝。

## 实际支持

- 声明宏：本文件 `macro_rules!`，有限 `expr/ident/tt` 捕获、单层重复和嵌套调用。模板局部变量按 AST 身份改名，支持的自由函数按定义点绑定；不插入隐藏 IIFE。模板出现两次的参数仍求值两次，不自动缓存。详见 [宏限制](internal/macros/README.md)。
- 内建 `Serialize/Deserialize`：具体命名 struct，标量、嵌套本地 struct、数组、slice、string-key map、可空 Go `*T`。`rename/default/skip/deny_unknown_fields`；default 是明确的 Go 零值子集，不是完整 Default trait。`*T` 仅是原型 Go 宿主语法，不代表 hgo 引用/Ptr 桥接已经定案。
- 核心 `serde`：Serializer/Deserializer、Visitor 与容器访问协议、结构化错误及 cause；生成静态 helper，不采用反射 struct 编解码，不强制 Value 树。
- 独立 `serdejson`：[支持范围与限制](serdejson/README.md)。精确整数、拥有解码数据、严格数字/Unicode/重复键/大小与深度检查。核心 enum 协议及本地 enum 的 Serialize/Deserialize 派生支持 unit/newtype。
- `Debug/Clone`：具体标量及上述非指针容器、嵌套本地类型。Debug 的 map 按键排序；Clone 递归复制支持的容器。指针、锁、外部未知类型、接口等没有隐式复制协议，明确拒绝。
- `Error`：`#[error("failed {Code}")]`、显式 `#[source]` 的 error 字段生成 Unwrap；单个 error 字段的 `#[from]` 生成 `FromType`。其显式转换已接入下述有限 `?` 前端；不推断动态错误变体，不支持 enum Error 派生。
- 日志示例调用 Go `log/slog`，验证重复字段参数和错误链；不是完整 tracing/span 运行时。

代表写法见 [订单](examples/prototype/order.hgo) 和 [日志与错误](examples/prototype/log_error.hgo)。源文件采用 Go package/import/function/field 语法，派生属性独占行；也支持有限 `struct Name` 声明简写。未实现 Rust/hgo 整套语法。

## 验证边界

`go test ./...` 包含运行生成 Go 的测试，覆盖宏卫生/求值/控制流、派生、serde 正负例与 CLI 输出保护。
`go test -race ./...` 检查测试进程和 runtime；测试中另启的生成模块普通 `go test` 不自动继承 race。
`python3 scripts/check_design.py` 仍检查旧设计夹具与文档，不作为编译器实现完成证明。
`tests/design` 和 `tests/research` 保留独立模块，防止各自隔离示例被根模块错误合并。

源码映射是原型：derive 记录 helper 范围及展开输入的类型来源；宏 token 保存格式化前的输出偏移及调用位置。
它不是最终 Go 文件逐 token、逐字段的完整映射，也不是 LSP。

未完成：泛型 derive、跨包宏、用户过程宏、完整 trait、用户泛型/跨包 enum、完整 Option/Result/引用前端、YAML、格式 Value、借用解码和原生 JSON codec 自动适配。
别名/借用风险分析继续独立于 MVP；本原型不作静态内存或并发安全保证。

## 本轮验收记录

环境：Go 1.27.1 linux/amd64，CGO_ENABLED=1，Python 3.12.14。
实际设置 `GOTOOLCHAIN=local GOWORK=off GOCACHE=/tmp/goxide-macro-cache`，PATH 指向真实 Go 编译器。

- `go test -race -count=1 ./...`：全部 7 个包通过。
- `go vet ./...`：通过。
- `python3 scripts/check_design.py`：完整检查通过，13 个 Go 命令含 3 个预期编译拒绝；锁夹具启用 race。
- 两个 `.hgo` 示例经 CLI run 通过；订单经 CLI build 后的二进制运行通过。
- 订单经 CLI transpile 后，另以 `go run -race /tmp/goxide-order-generated.go` 编译运行通过。

独立审查驱动修复：生成代码重复声明、错误路径重复、float32 下溢、字符串内伪属性、JSON 错误被忽略、模板局部名字与导入遮蔽、Error/Unwrap 字段冲突。CLI 另修复输出别名保护、最低 Go 版本检查及临时模块 VCS stamping。

## 独立黑盒验收后的精确边界

- serde 属性必须有 Serialize 或 Deserialize；组类型声明不能只在组头 derive，需分别标注类型。不能静默丢弃属性。
- `skip` 字段即使与 `deny_unknown_fields` 组合也消费并验证输入，解码保持零值；所有字段（含 skip）的 wire name 必须唯一。
- 错误路径区分 Struct 的 Field 与 Map 的 MapKey；JSON 未知值中的对象按 MapKey 记录。
- nil slice/map 编码为空数组/对象，解码为空容器；原生 `*T` 才接受 null。nullable 字段缺失仍须显式 default，本原型不是 Option 派生。
- Clone 递归复制已支持的无环容器内容，保留 nil/空区别，但不保留共享别名拓扑和 slice 容量；不处理循环对象图。Debug 同样要求无环输入。二者没有动态深度预算；这不是通用深复制承诺。serde 自身递归编码有协议深度预算，循环输入返回 Limit。
- `FromFailure(nil)` 返回 Failure 值，转成 error 后非 nil；Unwrap 为 nil。typed-nil source 原样保留。From 只支持唯一一个静态 `error` 字段，不支持任意具体错误类型转换。
- 示例日志是普通 slog 调用：日志级别禁用时实参仍按模板求值一次，不是惰性 tracing。
- CRLF 原始字符串按原始 token 范围处理；字符串值仍遵循 Go 的 raw-string CR 规则。

新独立测试位于 `tests/acceptance`，生成模块明确运行 `go test -race`；与旧测试子进程未继承 race 的情况分开记录。
研究线需要保留的验收项：属性不可忽略、skip 与未知字段组合、结构化 MapKey 路径、CRLF token 跨度、Clone 别名/环策略与日志禁用求值。以上是原型边界和反例，不替用户批准新的语言核心语义。

第二轮独立验收：`go test -race -count=1 -json ./...` 通过 8 包、44 个顶层测试及 30 个子测试；新增独立生成模块共 4 个，以 `-race` 运行 14 个测试及 19 个子测试。这些数量分开统计，不把生成模块日志重复算作宿主测试。`go vet ./...` 与 docs-only 均通过。

## 后续阶段：enum/match 先于 serde enum

本地非泛型 enum 已使用独立 AST/元数据实现，零载荷、单标量或无环本地 enum 载荷；构造保持名义身份。
语句 match 支持值绑定、guard、穷尽与不可达检查，目标一次求值，匹配前精确校验非法 Go 值，保留 return/defer/break/continue。见[精确范围](internal/enums/README.md)及[任务状态示例](examples/prototype/events.hgo)。

```sh
go run ./cmd/hgo run examples/prototype/events.hgo
go test -race -count=1 ./internal/enums ./tests/acceptance -run TestEnum
```

这是候选 interface+值型case ABI的有限实现，未冻结ABI；仅match使用点验证，未实现全入口/初始化检查。case/helper当前未导出，外部Go构造尚未支持。用户泛型、复杂payload、嵌套模式仍未实现；有限表达式 match 和内建 Option/Result 的后续交付见下文。枚举声明不是serde专用表示；下一阶段在这份元数据上补serde派生。

独立enum审查新增6顶层测试及8拒绝子例；3个生成模块显式race运行6测试及3卫生子例，另1模块验证名义类型编译拒绝。修复生成代码被局部true/false/any捕获，以及包级panic和标量类型遮蔽影响验证的问题。

## 后续阶段：真实 enum 的 serde 派生

已在上一阶段的 Decl/Variant 元数据上实现 `#[derive(Serialize, Deserialize)] enum`，并允许 struct 和容器字段使用这些 enum；未另造 serde 专用变体表示。
无载荷变体编码为字符串，单载荷变体为一个键的外标记对象；例如 `JobState::Running(70)` 对应 `{"Running":70}`。独立派生方向受检查，嵌套 enum 必须具有相应派生。

```sh
go run ./cmd/hgo run examples/prototype/event_json.hgo
go test -race -count=1 ./tests/acceptance -run TestEnumSerde
```

[通知往返示例](examples/prototype/event_json.hgo) 先 JSON 往返，再对解码的真实 enum 执行 match。
未知变体、错误载荷、null、多键/重复键等拒绝；非法 nil/typed-nil/case 指针/伪造值编码返回 serde 错误，不把它当业务变体或 panic。载荷错误使用 Variant 路径段，保留 cause。

独立验收：3 个宿主测试及6个拒绝子例；2个实际生成模块显式race执行8个测试及11个坏输入子例，全部通过。
仍未支持泛型/跨包 enum、复杂或递归载荷、enum 的 Debug/Clone/Error 和变体 rename/skip 等属性；这些明确诊断，不静默忽略。JSON 默认表示不是任意现有 Go interface 的自动反射协议。

## 后续阶段：标量读写引用

已接入独立类型检查阶段，保留源码权限和符号身份，再降低普通 Go 指针。支持 `&int` / `mut& int` 等内建标量、参数/单返回/局部、复制/重绑定、取址/解引用、复合赋值及 range 写位置检查；`&(*w)` 显式收窄。允许返回局部引用及标量闭包捕获，不声明独占、全局不变或无竞争。

```sh
go run ./cmd/hgo run examples/prototype/references.hgo
go test -race -count=1 ./tests/acceptance -run TestReference
```

[完整示例](examples/prototype/references.hgo) 输出 `shared: 6 snapshot: 105`；读引用观察同一对象更新，独立标量副本自行变化。旧宿主 `raw := &v` 不被改为新引用；只有明确引用期望类型或已知引用的重借用位置才解释为读引用。这是混合原型兼容边界，不是最终取址语法决定。

[准确范围](internal/refs/README.md)：复合类型投影、引用容器、接口/原生指针桥接、泛型引用调用、引用签名的函数值/回调、引用零值、具名返回等未实现，明确诊断。标量闭包捕获的读写照常检查；不携带新引用的普通 Go 调用不要求效果摘要。含新引用且调用尚未生成derive helper或缺包导出数据的文件，当前明确报未支持，不跳过类型检查。复杂投影和组合工作仍待完成。

独立审查新增24个检查器拒绝反例、3个前端拒绝反例及实际生成模块race正例；修复range写位置、泛型擦除和bound-mut误解析。包内另覆盖嵌套重借用，防止经过读视图恢复写权限。未经检查的Go调用方仍可提供nil/别名，未实现公开边界非空检查；没有恢复长期借用风险分析。

本阶段末：`go test -race -count=1 -json ./...` 通过10包、76顶层测试、115子测试；`go vet ./...`通过。以上是宿主JSON事件统计，生成模块测试另计；新独立模块均显式启用race，旧prototype子进程普通测试的限制仍保留。引用CLI transpile产物另用 `go run -race`通过。

最终独立验收累计：10个实际生成模块显式race运行29个测试、33个子测试，另1个模块验证预期Go名义类型编译拒绝。完整设计检查通过30篇Markdown/205个本地链接及13个Go命令（含预期拒绝）；该夹具检查仍不作为新编译器功能的证明。


## 后续阶段：Option/Result 专用 `?` 与有限表达式 match

已接入 `internal/propagate` 独立阶段，以 enum 元数据和真实 Go 类型识别内建 `Option[T]` / `Result[T,E]`，在原函数/最近闭包内生成一次求值、校验、分支和早退，不插入隐藏闭包。成功类型可以变化；错误先按 Go 赋值兼容保留，否则只查唯一注册的显式单步转换。前端当前仅注册 Error 派生的唯一 `#[from] Cause error` 字段所生成的 `FromType`，不是通用 From trait 搜索，不搜索链、不猜动态 error 变体。

[可运行传播示例](examples/prototype/propagation.hgo) 显式调用真实 Go `strconv.Atoi` 后选择如何处理 `(int,error)`，再展示 Result、Option 和 `error → PortError`；没有隐式原生多返回适配。[表达式 match 示例](examples/prototype/match_expression.hgo) 展示带显式类型的局部初始化和单返回位置。

```sh
go run ./cmd/hgo run examples/prototype/propagation.hgo
go run ./cmd/hgo run examples/prototype/match_expression.hgo
go run ./cmd/hgo transpile -o /tmp/goxide-propagation-example.go examples/prototype/propagation.hgo
go run -race /tmp/goxide-propagation-example.go
go run ./cmd/hgo transpile -o /tmp/goxide-match-expression-example.go examples/prototype/match_expression.hgo
go run -race /tmp/goxide-match-expression-example.go
```

以上两个 CLI run 和两个转译产物 `go run -race` 本轮均通过。传播输出为 `next port: 8081`、`port error: invalid port syntax: true`、`name length: 3`、`name length: absent`；match 输出为 `count: 3`、`count: 0`、`empty`。验证使用真实 Go 工具链，`GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOCACHE=/tmp/goxide-macro-cache`。

准确支持范围见 [内建枚举](internal/enums/README.md)、[传播](internal/propagate/README.md)、[表达式 match](internal/matchexpr/README.md) 和[宏](internal/macros/README.md)：

- 这是内建容器的有限泛型实例化，必须显式写类型参数；不是用户泛型函数/声明、通用 Try/Residual 或跨包泛型 enum。Option 的 nil 是 None，Result 的 nil 非法；typed-nil error payload 原样保留。
- `?` 支持单 RHS 绑定、单项 var 声明、return、调用参数（含 defer/go）、普通一元/二元/selector、短路布尔和无 initializer 的 if 条件。循环、switch、索引/切片、复合字面量、赋值目标、多 RHS 及带 initializer 的 if 条件中的传播明确拒绝；先绑定或显式 match。
- 宏模板仅支持固定内建容器类型：类型参数限 Go 内建标量及其指针/数组/slice/map/channel 组合，不支持捕获调用者类型、未知具名类型或用户 enum 构造。类型别名/构造 helper 不改变 `?` 的词法目标；模板显式闭包仍是传播边界。
- 表达式 match 仅支持整个显式类型局部初始化，以及单个非具名、Go 可解析返回类型的整个 return 表达式。`:=` 推断、参数/运算符/另一个 arm 内嵌 match、表达式块、包初始化、具名/多返回未实现。
- 该阶段新引用语法与 `?`/尚未生成的 derive helper 的同文件组合尚未打通（下节已补有限支持）：引用检查早于传播与派生 lowering，不能把分开可运行的功能推断为任意组合可用。原生 Go 指针示例不代表新引用组合已经支持。
- 内建容器不能派生 serde；该阶段已知含 enum 的 struct/container/pointer payload 在缺投影校验时拒绝（下节补值 struct 投影）。校验不遍历任意 interface 动态对象图。精确原始列映射、公开边界全面校验和借用风险分析仍未实现。

独立接受测试见 `tests/acceptance/propagate_test.go`，覆盖原顺序、短路、defer、最近闭包、typed nil、原生部分成功及显式拒绝；转换注册器层另测歧义、缺失和两跳拒绝。全套统计按本轮实际运行另记，不沿用旧阶段数量作为当前证据。

本阶段最终验收：`go test -race -count=1 -json ./...` 通过 **13 包、102 个顶层测试、150 个子测试**；`go vet ./...` 通过。新增传播与表达式 match 的独立接受测试让 **6 个实际生成模块**显式执行 `go test -race`，共 **15 个生成模块测试**通过；宿主统计与生成模块统计分开，旧 prototype 子进程未继承 race 的限制仍不变。另有前端组合回归直接 `go run -race`，两个 CLI 示例的转译产物也分别 `go run -race`；传播例子经 CLI build 后运行输出一致。

全新上下文集成审查及自审修复：宏显式闭包的返回类型别名、载荷/导入/From 目标别名不再按字符串误判；选中的 From helper 在包级绑定到保留标识符，防止调用者局部同名函数截获。内建 sum 或表达式 match 即使没有 `?` 也执行最终完整 Go 类型检查，临时类型推导允许的错误不会被静默放过。实际组合回归覆盖 match arm 内 `?` 的成功、失败和未选中分支，以及 From 遮蔽。

完整设计检查另通过 **32 篇 Markdown、218 个本地链接、13 条 Go 命令**（含三个预期编译拒绝、锁 race）；它仍只验证设计机制，与上述前端验收分开。环境为 Go 1.27.1 linux/amd64、CGO_ENABLED=1、Python 3.12.14；无工具链/依赖下载。

## 后续阶段：T0 跨功能组合与配置应用

引用阶段拆为 `Prepare → derive/propagate → Finish`：中间表示保留读写权限，完成整个生成程序的类型与权限检查后才擦成 Go 指针。现在普通 Option/Result 载荷可与标量引用参数/局部、Error/Clone/Debug 派生、最近闭包及直接 defer 调用同文件使用。传播不再把不可变的直接函数名冻结成函数值，避免凭空制造尚不支持的引用函数值；真实函数变量仍按原求值顺序捕获。泛型实例（包括不使用类型参数的 phantom 容器）不能洗掉引用权限。

这不是任意引用容器支持：`Option[&int]` / `Result[mut& int,error]` 明确报引用载荷未支持；宏定义点绑定引用签名函数产生的包级函数别名仍被拒绝。已有正例宏可以解引用后调用普通标量函数，也可以展开 `?`；调用处同名变量不截获宏绑定。完整投影权限、原生桥接、引用函数值和借用分析未加入本阶段。

新增 [配置应用](examples/prototype/config_pipeline.hgo) 将真实 JSON 输入解码为含 enum 的 Config，经显式 Result 包装和 `#[from] ConfigError` 传播、业务 match 校验，输出一个带 request_id 的 JSON slog 事件。`errors.Is/As` 保留业务及 serde 错误链。应用验收直接读取这个示例，不维护另一份近似代码：

```sh
go run ./cmd/hgo build -o /tmp/hgo-config examples/prototype/config_pipeline.hgo
/tmp/hgo-config
/tmp/hgo-config '{'
/tmp/hgo-config '{"Name":"worker","Mode":{"Workers":0}}'
```

首个执行成功（exit 0、INFO、workers=4）；后两个执行失败（exit 1、ERROR，分别保留 JSON syntax 和 workers 范围原因）。日志时间不固定，测试检查结构化字段和错误链。应用本身不混入新引用或宏；这些组合由独立测试覆盖。

为承载 `Result[Config,error]`，sum 使用点校验已扩展到有限本地值 struct 的命名字段、别名及嵌套 sum。分组 type 声明同样被检查；无效字段在 match guard 或 `?` 后续副作用前失败。含 enum 的指针/集合、嵌入/空白字段及递归投影继续明确拒绝，不遍历任意 interface 动态对象图。

[T0 验收矩阵](docs/t0-acceptance.md)逐项映射实际测试，区分完整窄契约、有限实现、未做和必需/后置。source-map 验收现检查 derive 起点确实指向最终 Go helper、宏调用行及引用权限/符号身份；宏偏移仍针对预格式化缓冲区，引用位置仍针对中间输入，未宣称完整逐 token 映射或 LSP。

本阶段全套 `go test -race -count=1 -json ./...` 通过 **13 包、112 顶层测试、185 子测试**；`go vet ./...` 通过。新增 **3 个实际生成模块**显式执行 `go test -race`，**8 个测试、8 个子测试**通过，另有应用 CLI build 后的成功/失败真实执行。宿主统计与生成模块统计分开；旧 prototype 子进程未继承 race 的限制仍保留。未改变冻结标签、未实现完整 Try/Residual 或风险分析器。

完整设计检查通过 **33 篇 Markdown、275 个本地链接、13 条 Go 命令**（含三个预期编译拒绝及锁 race）。自审回归另保证局部同名类型不能覆盖包级值字段校验描述；独立审查明确保留宏引用函数别名与引用载荷的限制，没有以拒绝用例冒充功能实现。


## 当前增量：复合只读投影与跨阶段诊断

在 `0f48259369d0940d298b76d3fc657dd6d0cb28d9` 的组合基础上，引用元素扩展到具体 struct、array、slice、map 和原生 pointer 路径。只读沿字段（含提升字段）、索引、range、解引用和切片视图传播；struct/array 的独立值部分可写，复制出的共享字段保留只读。接口、函数、泛型、重取写引用、普通返回/调用及容器边界不能擦除资格；命名结果和 range 目标也检查。显式类型本地变量与闭包局部保留资格。上述更新取代前文历史阶段“复合投影未实现”的说明，完整边界见 [引用实现](internal/refs/README.md) 和 [T0 矩阵](docs/t0-acceptance.md)。

资格按 `go/types.Object` 和字段选择身份求固定点；传递只读的 `deep` 资格吸收更深资格，使递归指针 `p=&p` 收敛。没有长期别名风险分析；局部重新绑定或字段换成 fresh 存储仍保守保留旧资格。接口/channel/function 投影、方法效果、带共享字段的容器封装、引用容器/函数值/原生桥接仍有限或明确拒绝。不会因外层值复制而推断其共享内容独立。

[只读缓存示例](examples/prototype/readonly_cache.hgo) 在同一对象上展示读取快照、独立值副本修改，以及可写别名更新后的可见性。独立接受测试直接读取示例、显式运行生成模块 `go test -race`，不以宿主 race 代替生成程序验证。

新增 `diagnostics` 使用最终 Go 字节坐标，记录有证据的原始 span 和宏展开栈；`deriveFields` 记录字段类型原始范围。已知复制、显式宏来源与完整 token 顺序保持的格式化可传递来源，重复 token 不靠文本搜索猜测。生成 helper 名称和可证明接收者身份的字段 selector 记录因果来源；未知 helper 语法和没有显式映射的实质改写保持 `generated/unmapped`。Go stderr 原样保留后追加映射信息；旧阶段错误明确标为中间坐标，不冒充原始行列。enum、引用、match、`?` 的实质改写尚没有完整来源传递，不能宣称所有组合都保留精确位置，也不是 LSP。详见 [映射契约](internal/sourcemap/README.md)。

非作者独立审查实证并复验修复：递归指针资格不收敛、命名普通结果裸 return 逃逸、range 写全局逃逸、括号赋值/range 目标资格丢失、闭包 var 过度拒绝、旧阶段误报原始位置。保留原有独立投影与映射验收成果，另加入对应回归；最终审查无未修复阻断。

本轮最终代码验证（Go 1.27.1 linux/amd64、CGO_ENABLED=1；真实工具链 PATH，`GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOCACHE=/tmp/goxide-macro-cache`，未下载）：

- 全套 `go test -race -count=1 -json ./...`：**14 包、134 宿主顶层测试、234 宿主子测试**通过。
- 独立接受测试另启动 **21 个成功生成模块**，显式 `go test -race` 通过 **56 个测试、41 个子测试**；另 **1 个生成模块**验证预期名义类型编译拒绝。宿主 JSON pass 与生成程序日志分开统计。旧 prototype 普通生成子进程的 9 个测试、14 个子测试不计入生成 race 数量。
- `go vet ./...` 通过；完整 `python3 scripts/check_design.py` 的 **13 个 Go 命令**通过（含三个预期拒绝及锁 race）。设计夹具仍不代替前端证明。
- CLI `run readonly_cache.hgo` 与其 transpile 产物 `go run -race` 均输出 `totals: 40 44 stored: 3 3 6 8 12`；同时生成可解析 source-map JSON。
- CLI build 配置应用后，默认输入 exit 0 / INFO / workers=4；坏 JSON 与零 workers 分别 exit 1 / ERROR，并保留 syntax 与业务范围原因。

整项 T0 仍未完成：数值/string 常量 enum raw/未知值、完整跨阶段来源、通用引用桥接、一般泛型/跨包能力继续按矩阵分期；未变更冻结设计、未新增长期借用分析。

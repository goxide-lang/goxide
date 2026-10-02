# 可运行的 hgo 语言子集

**当前覆盖：最新 D27 撤回开放 raw/C enum；旧 raw 阶段仅为历史记录，代码/成功夹具已移除。当前封闭 enum、serde 外部映射、手动 Default 与具体 Option serde 见末节；enum 仅选中默认变体的 payload 要求 Default。**

此实现分支从 `v0.0.0-mvp-doc` 开始；冻结标签保持不变，本文件只描述实现分支。
这是具体类型、Go 宿主语法上的可执行子集；单文件入口与后文有限 Go module 目录入口并存，不是完整 hgo 编译器、trait ABI 或 LSP。

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
单文件模式从仓库之外使用时指定 `-runtime /path/to/goxide`。目录模式使用输入模块自己的 go.mod/replace，见后文；现有单文件输出可被覆盖，输入及输出互相冲突则拒绝。

## 实际支持

- 声明宏：单文件 `macro_rules!`，以及同 module 源码包 `pub macro_rules!` / `alias.name!`，有限 `expr/ident/tt` 捕获、单层重复和嵌套调用。模板局部变量按 AST 身份改名，支持的自由函数按定义点绑定；不插入隐藏 IIFE。模板出现两次的参数仍求值两次，不自动缓存。详见 [宏限制](internal/macros/README.md)。
- 内建 `Serialize/Deserialize`：具体命名 struct，标量、嵌套本地 struct、数组、slice、string-key map、可空 Go `*T`。`rename/default/skip/deny_unknown_fields`；解码时 default/skip 复用已登记的具体 Default，缺协议报错；不是完整 Default trait。`*T` 仅是原型 Go 宿主语法，不代表 hgo 引用/Ptr 桥接已经定案。
- 核心 `serde`：Serializer/Deserializer、Visitor 与容器访问协议、结构化错误及 cause；生成静态 helper，不采用反射 struct 编解码，不强制 Value 树。
- 独立 `serdejson`：[支持范围与限制](serdejson/README.md)。精确整数、拥有解码数据、严格数字/Unicode/重复键/大小与深度检查。核心 enum 协议及本地 enum 的 Serialize/Deserialize 派生支持 unit/newtype（含有限 struct/container 单载荷）。
- `Debug/Clone`：具体标量及上述非指针容器、嵌套本地类型。Debug 的 map 按键排序；Clone 递归复制支持的容器。指针、锁、外部未知类型、接口等没有隐式复制协议，明确拒绝。
- `Error`：`#[error("failed {Code}")]`、显式 `#[source]` 的 error 字段生成 Unwrap；单个 error 字段的 `#[from]` 生成 `FromType`。其显式转换已接入下述有限 `?` 前端；不推断动态错误变体，enum Error 的显式变体属性见本文最新增量。
- 日志示例调用 Go `log/slog`，验证重复字段参数和错误链；不是完整 tracing/span 运行时。

代表写法见 [订单](examples/prototype/order.hgo) 和 [日志与错误](examples/prototype/log_error.hgo)。源文件采用 Go package/import/function/field 语法，派生属性独占行；也支持有限 `struct Name` 声明简写。未实现 Rust/hgo 整套语法。

## 验证边界

`go test ./...` 包含运行生成 Go 的测试，覆盖宏卫生/求值/控制流、派生、serde 正负例与 CLI 输出保护。
`go test -race ./...` 检查测试进程和 runtime；测试中另启的生成模块普通 `go test` 不自动继承 race。
`python3 scripts/check_design.py` 仍检查旧设计夹具与文档，不作为编译器实现完成证明。
`tests/design` 和 `tests/research` 保留独立模块，防止各自隔离示例被根模块错误合并。

源码映射是原型：derive 记录 helper 范围及展开输入的类型来源；宏 token 保存格式化前的输出偏移及调用位置。
它不是最终 Go 文件逐 token、逐字段的完整映射，也不是 LSP。

未完成：泛型 derive、外部预编译宏 ABI、用户过程宏、完整 trait、用户泛型/跨包 enum、完整 Option/Result/引用前端、YAML、格式 Value、借用解码和原生 JSON codec 自动适配。
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
- `skip` 字段即使与 `deny_unknown_fields` 组合也消费并验证输入，解码调用 Default 构造字段（本轮兼容变更）；所有字段（含 skip）的 wire name 必须唯一。
- 错误路径区分 Struct 的 Field 与 Map 的 MapKey；JSON 未知值中的对象按 MapKey 记录。
- nil slice/map 编码为空数组/对象，解码为空容器；原生 `*T` 才接受 null。nullable 指针字段可显式为 null；缺失仍报 MissingField。本轮没有指针 Default，不能给指针字段加 default/解码 skip，本原型不是 Option 派生。
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

## 当前增量：T0 常用重绑定、lowering 来源与条件日志

基于 `434ad22b23f7809b70a0cabacfe99ad02b5bd4a1`，本轮对上述保守规则作有限精化：局部直线 fresh 重绑定、双方 fresh 的 if 分支、同时赋值 RHS 和闭包自身的本地变量采用词法点事实；旧别名保留旧资格。`copy` 与 `append(writable, readonly...)` 可复制不含共享资格的值元素；含指针/共享字段的浅复制仍不能恢复写权限。捕获、取址、循环、switch/select/goto 涉及的槽位和字段 fresh 替换仍保守；相关诊断明确标出 `prototype tracking limitation`，建议独立 fresh local。确定只读写入、资格擦除及不支持边界仍是错误，没有引入全程序借用或效果分析。

enum/语句 match 和表达式 match 的 renderer 现在记录显式复制范围，`?` 准备阶段保留操作数与未改来源；传播 AST lowering 只传递存活节点身份、完整顺序流对应以及结构一致时的起始 token。生成 AST 恰好重用位置不构成来源证据。嵌套宏诊断栈从 Prepare 坐标回传原始跨度；legacy `macroTokens` 仍明确使用 Prepare 坐标。新增验收逐一核对九次重复字面量的原始位置及宏栈，并把真实 go/types 错误分成原始字面量可映射、生成宏括号明确 unmapped 两类。derive 与这些阶段同文件组合已验收；引用 Prepare/Finish 的实质改写仍会丢弃精确来源，重建表达式和未知生成区域也不猜测来源。

[条件日志应用](examples/prototype/conditional_log.hgo) 使用已有声明宏的显式条件分支。禁用时消息与昂贵字段零次求值，启用时条件、消息与字段各一次且顺序保持，JSON request_id/error 与 Error 的 errors.Is/As 链均验收。它提供常用条件日志模式，不实现 span/tracing runtime；现有 eager event 宏不改变语义。现有 serde JSON、thiserror 风格 Error/From 和 Debug/Clone 组合持续回归；enum Debug/Clone、enum Error/具体 source From 等尚未实现，没有因组合示例通过而标完成。

本轮新增测试由实施者编写；fresh 非作者只读审查完成源码/矩阵检查并独立复跑七个新增顶层测试，其中两个真实生成模块显式 race。平台历史线程上限阻止创建另外两名 fresh 实施者，实际为单实施者加 fresh 非作者审查，不冒称并行独立实施。审查促成 Prepare 后宏栈原始坐标、NUL 私有分隔符输入拒绝、重复 token 精确序列与真实生成括号 unmapped 验收；最终无未修复阻断。

最终实际验证（Go 1.27.1、linux/amd64、CGO_ENABLED=1；`GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOCACHE=/tmp/goxide-macro-cache`）：

- 全套 `go test -race -count=1 -json ./...`：**14 包、141 宿主顶层测试、241 子测试**通过。
- 接受测试实际生成 **23 个成功模块**，显式 `go test -race`：**58 测试、41 子测试**通过；另 **1 个模块**保留预期名义类型编译拒绝。宿主和生成模块分开计数；旧 prototype 子进程不继承 race 的限制不变。
- `go vet ./...`、完整设计检查 **13 Go 命令**通过（含三个预期拒绝和锁 race）。设计检查只证明文档/夹具，不代替语言前端覆盖。
- 条件日志经 CLI transpile，source-map JSON 可解析，产物 `go run -race` 输出 INFO/request_id=7 与 `cause preserved: true`。
- 配置应用 CLI build 后默认输入 exit 0/INFO/workers=4；坏 JSON 与零 workers 分别 exit 1/ERROR，保留 JSON syntax 与业务范围原因。

本轮无待批准阻断，不改 tag、不 push、不冻结新核心语义。T0 整体仍为有限可运行子集：常量 enum raw/未知值规则需后续明确；通用引用桥接、引用容器/函数值、完整跨阶段映射、enum 派生扩展、一般泛型和跨包能力继续按验收矩阵分期。长期借用分析仍独立，race 不代表静态安全保证。

## 当前增量：enum 常用派生与多字段业务载荷

在 `f8466ae90a38a3d494b7e9127fe2710475c05cad` 上补齐本地 enum 的 Debug/Clone 与显式 Error/Unwrap/From。严格沿用设计的 unit/单 payload：多字段放普通 struct；有限 array/slice/map 单载荷可参与 serde 和深复制。没有新增 tuple/record variant 语法或格式协议。新[任务派生示例](examples/prototype/enum_derives.hgo)及[独立接受测试](tests/acceptance/enum_derives_test.go)验证真实 JSON 往返、match、容器 Clone、精确 Debug、错误链及唯一单步传播。

Clone 递归复制支持容器、保留 nil/空区别，不复制原生指针、不保留别名拓扑/容量；不支持循环对象图。Debug 保持字段声明顺序、字符串引用及 string-key map 排序，variant 名称显式保留。嵌套命名类型必须具有相应派生。enum-bearing 容器仍拒绝，有限值 struct 中的 enum 投影在 match/Debug/Clone 前校验；没有扩大到任意对象图。

Error 使用变体独占行 `#[error("message {Value}")]`，unit 可仅 literal；`#[source]`/`#[from]` 仅接受单个静态 error payload。每变体必须有 message；From 唯一，生成 `FromEnum(error)` 并接入现有包级绑定的一步转换。Unwrap 原样保留 typed-nil interface；不猜动态变体、不推断两跳，不隐式透明转发 struct Error。完整有限拼写见 [enum实现](internal/enums/README.md)。variant payload 独立 AST 的拒绝诊断明确使用 enum 阶段坐标及 generated/unmapped，不把位置误当 package 的原始 token。

独立用例与只读审查分别由两位复用的非作者完成；平台线程上限拒绝 fresh 新线程，未宣称 fresh。审查促成无derive变体属性拒绝和payload诊断坐标修复。

最终验证（Go 1.27.1、linux/amd64、CGO_ENABLED=1，同前述离线环境）：全套 `go test -race -count=1 -json ./...` 通过 **14包、144宿主顶层测试、262子测试**；接受测试实际生成 **25个成功race模块，71测试、48子测试**，另1模块验证预期名义类型编译拒绝。新增部分为3宿主测试（含21拒绝子例）、2个生成race模块的13测试/7子例。宿主与生成日志分别统计；旧prototype普通子进程不计入race模块。`go vet ./...`、完整设计检查13个Go命令通过；最终文档检查34篇Markdown、285本地链接通过。示例CLI transpile产物 `go run -race` 展示容器独立复制和JSON Syntax错误链。非作者最终只读审查无阻断。

T0仍有明确缺口：enum-bearing容器/递归图、具体source From、跨包公开身份/构造入口、精确来源及其他矩阵项；本轮无设计决策阻断，不push/tag，不冻结新核心语义。

## 后续阶段：多文件包与 Go module 入口

目录模式沿用现有 `go.mod` 的模块路径和依赖关系，不引入另一套包管理器。每个 `.hgo` 文件分别展开、降低和解析；同包声明/派生模型共享，最终多个 Go AST 在真实 import path 下共同类型检查。各文件的 import 别名仍只在本文件可见，不拼接文本来消除文件边界。包依赖按导入顺序处理，消费者读取实际生成 Go 的导出数据。当前准备并检查整个模块内所有可见源码包，输入的目标目录仅决定 run/build/test 的执行目标，尚不是只处理目标依赖闭包。包级 From 绑定带稳定文件命名空间，避免两个文件各自使用 `?` 时生成重名。

```sh
go run ./cmd/hgo transpile -o /tmp/hgo-packages-output examples/packages
cd /tmp/hgo-packages-output
go test -race ./...
go build -o /tmp/hgo-packages-app .
/tmp/hgo-packages-app
```

输出目录必须尚不存在并位于输入模块之外。嵌套模块不参与本模块构建；testdata 和隐藏资源复制保留但不作为源码包发现入口。生成结果是普通 Go module，可直接由 Go 工具构建；依赖仍通过 go.mod 解析，保留本地 replace 的实际目标，不代表已经打包所有依赖。工具离线运行，不自动下载模块或切换工具链。目录 `run/build/test` 在临时生成模块执行；`test` 对目标目录子树运行 Go tests 并启用 race。`expand` 仍只接受单文件。

[真实应用](examples/packages/main.hgo)的 model 包在多个文件中声明公开 enum、serde/Clone/Error 派生、JSON 解码及本地 Result 传播；调用包使用别名 import 和公开 API，另调用普通 Go native 包，输出结构化成功/失败日志。公开 enum 采用显式 `pub enum`，提供 `New<Enum><Variant>`、`Validate<Enum>` 和已支持派生的公开 helper；case 实现保持私有（后续 qualified 阶段提供受验证的导出 case alias）；Validate 无返回值，非法输入沿既有 validator 规则 panic。目录模式中大写 enum 必须写 pub，避免无 pub 的声明因 Go 大写规则意外公开。这是有限原型 ABI，不是最终跨包 trait 协议；普通 Go 声明遵循 Go 大小写可见性。

公开构造函数不意味着任意 Go 输入已被全图验证：Go 调用者仍可传入 nil/别名，已有 match、Clone/Debug、serde 和显式 Validate 的使用点规则保持。私有 struct 字段不会因为序列化 helper 自动变成可从外包访问的 Go 字段；wire 字段是否出现仍按派生规则，不应把 Go 可见性当作数据脱敏。

明确边界：目录模式暂不支持新引用语法、`.hgo` 测试文件、`.hgo` 平台后缀或构建条件；使用普通 Go 测试，native Go 文件由 Go 构建选择处理。尚未承诺完整 cgo、go.work 或自定义构建配置支持。此阶段的宏定义仍局限于文件；后文同 module 声明宏阶段已接通跨文件/跨包宏。该阶段尚未支持跨包 qualified enum match 和导入泛型 Result 的直接 `?`；后续阶段实现如下。从导入 selector 类型直接派生仍未支持。不能把这个闭环说成所有单文件语法已经可以跨包使用。跨包来源只在有原文件/跨度证据时附加，未知生成区域仍标明 unmapped。

独立验收见 [packages_test.go](tests/acceptance/packages_test.go)：真实生成模块显式 `go test -race`、独立 build/运行、公开构造及派生调用、错误链，以及跨文件诊断/import 不泄漏/私有访问/名义类型不互换/跨包宏拒绝。全套统计由本阶段最终运行记录另列，不沿用上一阶段数量。

本阶段最终全套 `go test -race -count=1 -json ./...` 通过 **14 包、154 个宿主顶层测试、278 个子测试**。独立接受测试中 **26 个成功生成模块**显式 race，共 **74 个测试、48 个子测试**，另有 1 个预期名义类型编译拒绝；新增真实三包模块包含 3 个应用测试，model/native 两包参与编译但没有测试函数。宿主统计与生成模块统计不混算。`go vet ./...`、完整设计检查 13 条 Go 命令、最终 docs-only（34 篇 Markdown、291 个内部链接）通过。最终全套重新运行，已包含 CLI 帮助文字、worktree `.git` 排除和 grouped type 跨文件载荷校验修复；受影响包及新 grouped 边界另经独立定向复验。

新鲜上下文的非作者用例与只读审查分别完成；修复多文件 From 绑定冲突、重复 Result receiver、跨文件递归诊断越界、Error helper 位置、quoted replace 和资源复制，以及隐藏输出路径/祖先模块覆盖保护。生成应用成功输出 INFO/request-42/workers=4；坏 JSON 输出 ERROR 并保留 Syntax cause。无长期分析器或新模块体系；无标签变更、无推送。证据日志为 `/tmp/goxide-packages-final-race.json`、`/tmp/goxide-packages-vet.log`、`/tmp/goxide-packages-design.log`。


## 后续阶段：qualified enum 与导入 Result

目录编译按依赖顺序生成并检查每个包，只把成功编译包的受信任元数据交给其直接导入方。公开 enum 可以使用 `alias.Enum::Variant` 构造、statement match 和既有有限 expression match；匹配前调用生产包 validator，保持穷尽性、guard 顺序和非法表示先于 wildcard 副作用的规则。包身份使用 canonical Go import path，每个文件单独解析 import alias；同名不同包的 enum 不互换，私有 enum 和普通 Go 同名类型不获得协议。

导入函数返回的具体 `Result[T,E]` 可以直接 `dep.Load()?`；Option 使用同一名义协议。生产包提供公开 case alias、构造 wrapper 和具体实例 validator wrapper；具体 validator 保留嵌套 enum payload 校验，不退化成仅外壳检查。生成类型和函数在包级保留名字下绑定，局部 import/type 同名变量不会截获验证或构造。导入实例必须有生产包登记，类型参数必须能在消费文件 import 作用域中命名；无法命名的私有类型或缺少导入的参数明确拒绝。

From 仍只使用当前包注册项：先检查 Go assignability，再要求唯一显式一步转换；不会自动把依赖包所有 From 导入候选，也不搜索转换链。错误接口的 typed-nil 原样保留。诊断仅对有复制跨度证据的消费方 arm/body 保留原文件行号；类型化 match 临时变量上的错包赋值错误仍可能报告生成行并明确标记 `generated/unmapped`，不宣称已完成跨包全阶段来源。跨包宏、用户泛型 enum/trait ABI、导入 builtin 的 qualified 构造/pattern、独立预编译依赖的元数据发布和 imported selector derive 不在此有限实现内。

[三包应用](examples/qualified_packages/main.hgo)和[验收](tests/acceptance/qualified_packages_test.go)覆盖真实 JSON/enum/Result/errors 分包；[内部边界测试](internal/frontend/imported_test.go)覆盖文件 alias 隔离、局部 shadow、同名不同包、私有/native 元数据拒绝及非法 enum 验证。全套验证和本地提交由本轮最终记录给出；无 push/tag，无长期分析或核心语义重选。

本阶段最终冻结树验证：`go test -race -count=1 -json ./...` 通过 **14 包、163 宿主顶层测试、281 子测试**；接受测试日志中的 **27 个成功生成模块、79 测试、48 子测试**显式 race，另 1 模块按预期名义类型编译拒绝。本轮新增三包模块含 5 个 runtime 测试；内部 qualified enum 边界另运行一个生成模块 race。`go vet ./...`、完整设计检查 13 条 Go 命令、文档链接及 diff whitespace 均通过；冻结示例经 CLI 转译后再次独立 `go test -race`、`go build -buildvcs=false` 和运行，输出 INFO/mode=4/value=8/error=null 及 other。示例本身未携带测试文件，runtime 断言由接受测试注入，二者不混计。fresh 只读审查和类型别名续审无剩余阻断；测试由本轮实施者共同编写。证据：`/tmp/goxide-qualified-final-race.json`、`/tmp/goxide-qualified-final-vet.log`、`/tmp/goxide-qualified-final-design.log`、`/tmp/goxide-qualified-final-frozen`。未 push/tag。


## 后续阶段：同 module 源码包声明宏

目录模式支持公开 `pub macro_rules!`，普通 import 别名下用 `alias.name!(...)` 调用；同包多个文件共享宏及普通函数声明，定义文件的 import 作用域保持独立。运行时 helper 必须公开才能由跨包展开引用，私有嵌套宏仍可在定义包上下文展开。未解析自由名字明确拒绝，不能捕获调用者同名函数、宏或局部变量；宏身份不取决于包声明短名。

复用现有 Go module 图、构建文件选择和 importer，不读取外部宏 metadata。只有成功编译源码包的 opaque registry 进入依赖元数据，普通 Go 同名声明不能伪造宏。共享构建预算限制 200,000 输入/输出 token、4,096 次展开，token tree 深度 256、展开遍历深度 64；内部收集/生成两遍不重复记账。展开状态属于本次编译，registry 可并发只读使用。

定义和调用 token 分别记录源文件、原始/Prepare 阶段及嵌套栈，防止跨文件同偏移误归因；生成 import/helper 和无法证明的改写保持 unmapped。真实模块应用及独立业务测试见 [macro_packages](examples/macro_packages/main.hgo) 和[验收测试](tests/acceptance/macro_packages_test.go)。保留有限宏 fragment/模板语法、外部预编译依赖 ABI、用户过程宏和完整跨阶段映射等边界；T0 整体没有因此完成。


本轮 fresh 非作者审查实际 diff 并定向复验，接续原生集成代理独立检查最终代码后修复同一文件同路径多 import alias 的选择不确定及 unused-import 问题；添加实际模块累计预算、同文件/外包 `?` 预处理后精确 literal 与展开栈、同文件双 alias 验收。测试由实施及接续代理编写，不称为独立测试作者。公开 `Emit` shim 调用私有 `emit` 的生成应用另经最终定向 race/build/run。


最终验证（真实 Go 1.27.1，`GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOCACHE=/tmp/goxide-macro-cache`，未下载）：

- 全套 `go test -race -count=1 -json ./...`：**14 包、179 宿主顶层测试、289 子测试**通过。
- 接受测试另有 **28 个成功生成模块、81 测试、48 子测试**显式 race；另 1 模块验证预期名义类型拒绝。新增日志应用贡献 1 模块、2 测试，并独立 build/run，输出 JSON request_id=42/workers=4 及 `workers 4 <nil> 102`。宿主和生成测试分别统计，旧普通 Go 子进程不计为 race。
- 全套启动后示例 `Emit` 增加私有 `emit` shim，最终示例另以 `TestMacroPackagesApplicationRace` 定向 race/build/run 通过；没有声称较早全套读取了这一最后改动。
- `go vet ./...`、完整设计检查 **13 条 Go 命令**、最终 docs-only **34 篇 Markdown / 310 个本地链接**和 diff whitespace 检查通过。设计夹具不是新宏实现的功能证据。

日志：`/tmp/goxide-macro-final-race.json`、`/tmp/goxide-macro-final-shim-race.json`、`/tmp/goxide-macro-final-vet.log`、`/tmp/goxide-macro-final-design.log`、`/tmp/goxide-macro-final-docs.log`。仅本地提交，无 push/tag；无已知阻断，有限语法/metadata/来源限制如上。


## T0 接续：目录引用和 Go 构建条件

目录模式不再一律拒绝引用或 `.hgo` 构建条件。同包引用先保留 nominal 读/写签名，各源文件保留独立 AST/import scope，逐文件以同份包声明运行既有权限检查后统一擦除。peer 函数体在本文件检查时使用无返回的循环占位，原函数体必须在自己的检查轮次通过，不能通过忽略临时类型错误把权限绕过。native Go 文件也参与权限边界检查。公开新引用函数签名明确拒绝，直至跨包 metadata 能保留权限；原有引用 alias、global、callback、method 限制保持，不把普通 Go 指针 API误当新引用。

`.hgo` 通过标准 `go/build.Context.MatchFile` 的虚拟 `.go` 文件选择，因此沿用平台 suffix、现代/旧构建条件、忽略文件规则；未选中源无需可解析且不进入声明/宏 registry。`-tags` 同步传递前端、native Go 导出数据与最终命令。目录 test 使用显式 Race 配置，同时选择 race 源并以 `go list -race` 读取 export data，最终生成测试也开启 race。仅实现宿主目标；不同 GOOS/GOARCH 在写产物前明确拒绝，避免错误 target sizes。生成模块是单构建配置视图，换 tags/race/平台要重新转译，不能把保留的 native 文件误当全配置可用承诺。

定向验证涵盖同包跨文件读/写正例、readonly→write、native pointer 擦除、返回 readonly 后写、exported/global/alias/function-value/method 拒绝，且断言真实权限原因；标准选源涵盖 Linux/Windows/Darwin suffix、现代/旧 tag 与字符串中的伪注释。真实 CLI 在错误 PATH 的 go 前仍通过明确 `-go` 执行 run/test，包含 tags 依赖导出、race/!race 选择及生成模块 race。前端 importer 固定构建工具链 GOROOT，目录执行的 `-go` 版本必须一致；不声称支持多工具链 ABI 混用。最终综合统计由主集成验收记录。


最终冻结代码验证（Go 1.27.1 linux/amd64、CGO=1；离线、本地工具链）：

- `go test -race -count=1 -json ./...` 全部 **14 包、186 宿主顶层测试、297 子测试**通过，无失败。
- 新独立 `TestT0DeliveryInstalledCLI` 实际安装CLI，并对单文件与多文件业务产物分别执行 `go test -race`：**2 个生成模块、4 个业务测试**通过。宿主带race与这些子进程显式race分别核实；目录CLI另执行真实race选源测试。
- `go vet ./...`、`python3 scripts/check_design.py` 的 **13 个 Go 命令**、最终文档链接与diff检查通过。文档/旧Go机制检查不作为新前端能力证明。
- 新验收者是复用非作者，实际编写新业务/测试并发现CLI宏栈和race选源两项缺陷；两项均修复后独立复验通过。没有声称获得额外空上下文审查。

日志在 `/tmp/goxide-t0-delivery-race.json`、`/tmp/goxide-t0-delivery-vet.log`、`/tmp/goxide-t0-delivery-design.log`；可复现新业务如下（真实Go编译器已在PATH，目录为本仓库根）：

```sh
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off
repo="$PWD"
work="$(mktemp -d)"
GOBIN="$work/bin" go install ./cmd/hgo
cd "$work"
"$work/bin/hgo" run -runtime "$repo" "$repo/examples/prototype/delivery_job.hgo"
cd "$repo"
go test -race -count=1 ./tests/acceptance -run '^TestT0DeliveryInstalledCLI$' -v
```

输出包含结构化 `job accepted` 事件和 `scheduled mail: 3 units`。完整复现运行根模块 `go test -race -count=1 ./...`、`go vet ./...` 及上述设计脚本；runtime由当前checkout提供，不依赖未发布远端提交。只做本地提交，不push或改tag。

### Default 具体类型派生（标签后增量）

`#[derive(Default)]` 生成 `DefaultType() Type`，沿用具体类型 helper，不声称完整 trait/impl 后端。struct 按声明顺序显式构造字段：已登记的 bool/string、整数（含 byte/rune）、float32/64 使用基础默认实现；同包已派生类型调用其 `DefaultType()`。空 struct 可构造。不会因出现同名手写函数就自动认领 Default 协议；基础类型被同包声明遮蔽时拒绝。

普通 enum 必须恰好有一个 `#[default]` 无载荷变体，生成函数调用该变体的已有 nominal constructor；缺失、多选、重复属性及标记 payload 变体均拒绝。当时原型未检查其他未选变体的 payload Default；最新用户保留该约束待定，不把这个历史实现行为当成已确认规则或验收要求。`Option[T]` 具体实例字段显式调用 None constructor，不要求 T 有 Default；现有 ABI 的 None 表示为 nil，不能据此推断普通 enum/Result 的 nil 合法。

容器、引用/指针、Arc、外部类型、Result 的自动默认及手写 Default 注册暂不支持；这不是给未决容器默认语义做决定。原生 Go `var`/`make` 不变。**历史阶段：当时 `#[serde(default)]` 仍是缺失字段 Go 零值策略，不调用语言 Default**；特别是它不能保证产生有效的非空 enum 字段，需要业务显式处理，后续不得无说明改变 wire 行为。

生成 helper 参与冲突检测和源码映射；宏从显式 derive 声明登记其定义点绑定，包含 grouped TypeSpec。调用方同名局部变量不能截获 Default helper。跨包宏不能通过自动登记的 helper 暴露私有目标类型；公开手写 shim 仍按原有规则使用。跨包字段协议自动推导不在本次 Default 子集内。

新增 `tests/prototype/default_test.go` 运行实际生成 Go，验证单个合法 enum 变体、Option None、嵌套 struct、空 struct、原生零值不变、宏局部遮蔽和分组声明；反例覆盖错误 default 标记、无注册类型、Result/容器/指针、helper 冲突、跨文件基础类型遮蔽。本轮独立 print/Default 业务另覆盖真实 CLI 和生成模块 race；安装与目录多文件使用由既有 T0 交付回归另验收。


## T0 接续：内建 print / println 格式计划

`print!` / `println!` 是保留的内建宏入口，要求圆括号参数和直接语句位置。首参数为字符串字面量，支持顺序 `{}` Display、`{:?}` Debug 和 `{{`/`}}`；数量、括号、动态格式串与缺少协议均在编译期拒绝。内建 bool/string/整数/浮点/复数及 Go alias 使用 `formatting` 的具体 typed formatter（基于 strconv）；用户 defined scalar 不因底层类型相同自动获得协议。同包受信任 `#[derive(Debug)]` struct/enum 支持 `{:?}`，跨文件保留类型身份并在包级绑定 helper，局部同名变量不能截获；导入包 Debug 类型、任意用户 Display impl、Stringer/error/reflect/any 兜底均未实现。

宏模板内可调用打印入口。格式计划在普通表达式类型信息可用后降低：所有显式参数先按从左到右顺序各求值一次，之后按占位符顺序 formatter，最后执行 stdout 写入。生成普通语句块，不插入 IIFE，参数中的 `?` 仍退出真实 enclosing Result/Option 函数；显式参数闭包内的打印通过后续 lowering 轮次处理。`go`/`defer` 打印、值位置、if initializer/loop post 暂明确拒绝，不暗中改变求值时机。类型推导只把打印参数临时作为独立表达式检查，不引入 variadic any formatter。

`formatting.Write` 在运行时读取 os.Stdout。`print!("")` 零次写入；`println!()`/空字符串输出一个 LF，已有换行仍追加一个 LF。`WriteTo(io.Writer,string)` 一次写入，不重试/回滚；错误或短写 panic `*formatting.WriteError`，保留 Written/Wanted 和可 errors.Is/As 的原 Cause（无错误短写为 io.ErrShortWrite）。不承诺输出原子性或持久化。格式参数的复制范围保留来源，编译期格式诊断可带原始调用及宏栈；生成 writer/helper 区域保持 unmapped，不承诺运行时 panic 自动映射为 hgo 行号。

定向测试覆盖标量/alias、派生 Debug、定义类型拒绝、格式错误、嵌套闭包、参数 `?`、同包跨文件及 helper 遮蔽、捕获参数诊断来源，以及空写/短写/原始错误 cause。独立业务验收另运行生成模块；最终数量由主集成记录，不把宿主 race 当作生成模块 race。


## print / Default 最终验收

基准 `1300b77e1ca72ae9365f1ce47d5d24375d6c5f92`；复用非作者独立审查并编写新业务验收，没有声称新建空上下文。新例子为 [print_default.hgo](examples/prototype/print_default.hgo)，串联显式默认配置、JSON覆盖、Clone、宏和格式化输出。

最终冻结代码在 Go 1.27.1 linux/amd64、CGO=1、`GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOCACHE=/tmp/goxide-macro-cache` 下：

- `go test -race -count=1 -json ./...` 成功：**15个有测试的包、198宿主顶层测试、328子测试**；另 `internal/printing` 无自身测试文件，通过编译，其行为由前端/独立接受测试覆盖。没有将 no-test 包计成有包内测试。
- 新独立接受测试为 **3宿主顶层、28拒绝子例**；业务产物是 **1个生成模块、4个实际race运行测试**。真实CLI执行输出 `startup=standby retries=0`、snapshot与`ready nightly active`。
- `go vet ./...`、完整设计检查 **13条Go命令**、最终 docs-only 和 diff 检查通过。设计夹具不替代前端实现测试。
- 已修复并复验嵌套打印遗漏、打印实参 `?` 冻结compiler intrinsic，以及生成括号遮挡可信参数来源的诊断问题；派生helper登记使Default宏按定义点绑定。scalar同名遮蔽原已有守卫，本轮新增跨文件反例，不计为新修复。

日志：`/tmp/goxide-print-default-race.json`、`/tmp/goxide-print-default-vet.log`、`/tmp/goxide-print-default-design.log`。快速复现：

```sh
go run ./cmd/hgo run examples/prototype/print_default.hgo
go test -race -count=1 ./tests/acceptance -run '^TestPrintDefault' -v
go test -race -count=1 ./...
go vet ./...
python3 scripts/check_design.py
```

本轮不实现常量enum；既定raw/未知值方向保持，表层入口与同值别名选择见U19候选。T0最短必要剩余是该入口，其他明确后置边界不因本轮通过变成已实现。只做本地commit，不push/tag改。


## T0 接续：显式整数 / string raw 常量 enum

**历史阶段：raw部分已由D27撤回，不是当前支持或待选任务；封闭enum的纯兜底match仍保留。**

原型入口为 `enum Status string { Pending="pending", Paid="paid", }` / `enum Code uint8 { Accepted=1, Retry=1<<1, }`，成员引用复用 `Status::Pending`。明确底层类型限 Go 内建整数/string（含 byte/rune），降低为真实 Go named type 和 typed const；普通同包跨文件 const、运算表达式及成员常量引用由 Go 类型检查求值，数组长度/const initializer 保持常量身份。溢出、非const、类型不兼容、循环常量按 Go 检查拒绝；不实现隐式成员值或 iota。定义点底层类型必须确为指定 predeclared builtin，不能被同包其他文件的同名类型替换。

raw 输入不封闭：原生 `Status("future")`、零值与 Go 合法 runtime narrowing 转换原样保留，不自动变成第一个成员，不生成 Unknown，不调用带载荷 enum 的非法表示 validator。statement 和既有有限 expression match 都要求无 guard `_` 或 whole-value binding 兜底；列完所有已知成员也不覆盖未来值。guard 保序、目标一次求值、无guard同成员后续分支不可达；绑定兜底保留原始未知值，控制流不新增隐藏循环/IIFE。既有至少一个 qualified pattern 用于确定名义类型的限制保持。

同 module `pub enum` 支持消费者 `alias.Status::Pending` 与 qualified match，生产包成功检查后才发布可信元数据。包级 type/const 绑定防止消费方局部 alias/type 同名捕获，const 不退化为 var。Go 端公开常量名为 `HgoConst<len>_<Enum>_<Member>`，例如 `HgoConst6_Status_Pending`；普通 exported named type 可直接由 native Go 转换、接收及返回。它是有限原型 ABI，不是外部预编译 metadata 标准。

同一 enum 的重复 raw 值按 Go `constant.Value` 求值后比较，报 `same-value aliases pending user decision; unsupported in current subset`；等值不同拼写不会漏判。这只隔离尚未决定的同值别名能力，不把永久禁止写入语言规则。不同 enum 可有相同 raw 值，但不能因此混赋/混match。不扩展 raw enum 的 Default/Debug/Clone/serde/Error derive，已有带载荷 enum 行为不改变。

验证：内部测试覆盖显式常量表达式/数组长度、同包native const、溢出/非const/别名待决诊断、开放兜底/guard不可达、local type shadow、不同enum名义性与跨文件builtin shadow；`internal/enums` 和 `internal/frontend` 完整 race 通过，最后显式分号分隔raw声明的修复另以所有 `TestRawEnum` 定向race复验。复用非实现作者独立提供 三包业务（旧 raw 夹具已撤回，历史提交 cd49537） 和 接受测试（旧 raw 夹具已撤回，历史提交 cd49537）：安装CLI后仓库外转译、重复产物一致、实际生成模块race、独立build/run、native JSON未知/零值/窄化转换及14类拒绝通过。最终综合统计由主集成记录；无本代理独立提交或标签操作。


### 常量 enum 最终集成验收

**历史阶段：raw部分已由D27撤回，不是当前支持或待选任务；封闭enum的纯兜底match仍保留。**

基准 `47af69fcd66820ab4b7edb8b4c22e5eaf9a928ad`。代码冻结后的 Go 1.27.1 linux/amd64、CGO=1、本地离线工具链验证：全套 `go test -race -count=1 -json ./...` 通过 **15 个有测试包、204 宿主顶层测试、353 子测试**；另一个包无自身测试。`go vet ./...`、设计检查 **13 条 Go 命令**、最终文档检查和 diff 检查均通过。

新增独立验收由复用非实现作者完成，非新建空上下文：**1 个生成模块、3 个真实 race 测试、14 项拒绝子例**，另实际安装 CLI、仓库外执行、原生 Go 独立构建及重复生成一致性通过。既有 print/Default 生成模块 4 项 race 测试与 28 项拒绝同时回归。宿主测试数不重复计入子进程运行测试。

日志：`/tmp/goxide-raw-final-race.json`、`/tmp/goxide-raw-final-vet.log`、`/tmp/goxide-raw-final-design.log`。快速复现 `go run ./cmd/hgo run examples/raw_enum`；独立验收 `go test -race -count=1 ./tests/acceptance -run '^TestRawEnum' -v`。运行这些命令需真实 Go 编译器在 PATH。

本轮没有已证实未修复阻断；同值别名保持待用户决定，不据此新增其他功能。只做本地提交，不推送，也不改原文档标签。环境断连通知后，实际文件访问、命令和完整验证均正常。


## 最后 T0 覆盖补缺：纯兜底 match 与 raw Clone

**历史阶段：raw部分已由D27撤回，不是当前支持或待选任务；封闭enum的纯兜底match仍保留。**

基准 `94e5d5790822dafb902e57f288da16ac78ee4e4c`。独立非实现作者核对规范并运行反例，确认纯通配/全值绑定 match 和 raw Clone 均是已有语义内的实现缺口，而不是同值别名决策的依赖。

match 先保存目标一次，编译器内部标记在已有类型阶段用真实 Go nominal identity 和可信 enum 元数据解析。raw 保留所有原值；payload/Result 调用既有 validator，先于 guard/body；Option 保持 None 行为。普通标量、不同 defined type、any 擦除后的值和无法识别的泛型不靠底层形状猜成 enum。标记在生成 Go 前消除，未改 return/break/continue 的作用域；现有限表达式位置仍保持。

`#[derive(Clone)]` raw 生成同类型同值复制，未知值和零值不验证成员资格，显式派生才能让嵌套 struct 调用其协议；宏定义点绑定仍适用。raw Debug、serde、Default 规则未完整规定，本轮明确拒绝；详见枚举专题 §0.2，不把 Go JSON DTO 业务当成 raw serde 派生。

独立新 [归档业务](examples/prototype/final_audit.hgo) / [验收](tests/acceptance/t0_final_test.go)运行真实生成模块的 **2 项 race 测试、6 项拒绝子例**，检查未知/零值与嵌套Clone、一次读取/defer、非法payload nil/typed-nil/case指针在guard/body之前拒绝；仓库外CLI输出 `audit webhook future-v3`。审查者复用非作者线程，不冒称新空上下文。最终整树统计见下文；之前阶段wildcard-only与raw全部派生不支持的记录由本增量覆盖。


最终冻结验证（Go 1.27.1 linux/amd64、CGO=1，本地工具链离线）：`go test -race -count=1 -json ./...` **15 个有测试包、213 宿主顶层测试、370 子测试全部通过**；另一个包无自身测试。`go vet ./...`、设计检查 13 条 Go 命令、最终 docs-only 与 diff 检查通过。新增独立生成模块 2 项 race 运行测试以外，前端新增直接导入模块的真实 race 测试验证 raw/payload、import alias 遮蔽、Option None 与 Result nil；prototype 的两个生成运行例验证 raw Clone 及跨文件嵌套。没有把宿主 race 当作子进程 race。

日志：`/tmp/goxide-t0-final-audit-race.json`、`/tmp/goxide-t0-final-audit-vet.log`、`/tmp/goxide-t0-final-audit-design.log`。本轮无已知未修复阻断；新增有限能力已验收，未决定及后置范围见 T0 矩阵，不能称完整 T0/Rust。安装和复现（当前环境）：

```sh
cd /workspace/goxide-macro-mvp
export PATH=/workspace/goxide-environment/go/bin:$PATH
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off
mkdir -p /tmp/hgo-final-bin
GOBIN=/tmp/hgo-final-bin go install ./cmd/hgo
/tmp/hgo-final-bin/hgo run -runtime "$PWD" examples/prototype/final_audit.hgo
/tmp/hgo-final-bin/hgo run examples/raw_enum
go test -race -count=1 ./tests/acceptance -run '^(TestT0FinalAudit|TestRawEnum)' -v
```

实际期望业务输出分别为 `audit webhook future-v3` 与 `webhook review:deferred-v2/unknown:7`。只做本地提交，不 push、不改标签；停止新增范围，不重问同值别名。


## 当前交付：封闭 enum、serde 映射与文档同步

基准 `cd495372377cac8bf139427e21fc4490e48a0348` 的开放raw/C enum parser、常量lowering、metadata、Go常量导出和raw Clone路径已删除；底层整数/string enum语法明确拒绝。只保留声明的封闭变体，match/Clone/Debug验证合法表示，不通过未知值或任意Go转换扩充变体。

unit enum的 `#[serde(rename="pending")]` 使用字符串协议；或全部unit变体显式 `#[serde(value=1)]`，首版限定signed int64十进制字面量。数值映射使用既有Int64/ReadInt64而非新raw类型；两个方向可独立派生。重复/混合/缺失/越界/带payload的映射和未知属性明确诊断。未知有效wire值为UnknownVariant，Type/Range错误保持原类别及嵌套Field路径；错误不被默认变体吞掉，业务函数不执行。用户自定义业务validator后置，现有结构边界检查不属该延期项。

该阶段历史状态：Default struct要求所有字段有Default，否则手写；当时通用手写impl注册未实现，可显式业务构造，不凭helper名字注册。enum自动默认当时仅限unique unit标记；非默认payload约束当时待回答，现已确认并实现选中payload规则，见末节。Debug显示源变体/payload和struct字段内容，含私有内容，不按serde映射重命名、不自动脱敏、不保证稳定wire格式。

安全同步：先将cd49537实现分支和原文档标签发布，再把远端work `b6b746c4267962b1c61d13bff4b3f2f55638bf91` 的36项文档/检查器变更合入当前feature分支。RFC0019统一wrapper与0020关联函数原文保留；实现仍未覆盖它们的通用前端。独立编号冲突通过R19–R26和研究RFC子目录消解，链接全部重定位；远端旧strict/unknown-call-unsafe规则按D19/D20逐篇修正，没有恢复MVP借用分析。未重写work/main或原标签。

新[业务模块](examples/enum_wire/main.hgo)与[独立验收](tests/acceptance/enum_wire_test.go)由非实现作者编写，实际安装CLI、转译后生成模块3项race运行测试、独立build与4输入、仓库外run；另封闭归档业务2项race与PrintDefault业务4项race回归。未知字符串/数字（含空/0）返回带Field路径的UnknownVariant，业务计数0；已知值往返、原生Go构造/调用、Default第二unit和Debug源内容验证通过。

原型CLI复现（当前环境已具备工具链）：

```sh
cd /workspace/goxide-macro-mvp
export PATH=/workspace/goxide-environment/go/bin:$PATH
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off
GOBIN=/tmp/hgo-closed-bin go install ./cmd/hgo
/tmp/hgo-closed-bin/hgo run examples/enum_wire
go test -race -count=1 ./tests/acceptance -run '^(TestEnumWire|TestT0FinalAudit|TestPrintDefault)' -v
```

正常输出 `dispatch delivered true 1`；未知输入打印 `rejected 0`。完整运行次数与回归结果追加在本节末尾。


最终验证：Go1.27.1 linux/amd64、CGO=1、本地离线工具链。首轮整树race中其余14包通过，唯一失败是旧接受测试仍要求拒绝现已支持的variant rename；将该旧断言改为仍未支持的alias属性后，完整acceptance包重新race通过。合并这两次互不重复的最终包结果：**15个有测试包、209宿主顶层、350子测试通过**，另一个包无自身测试。不声称首轮命令成功，也没有将生成模块测试重复计入宿主数量。

`go vet ./...`、设计检查13条Go命令通过；最终Markdown 56文件、486本地链接/锚点和20篇逐特性RFC结构检查通过。独立文档审查发现的strict/unknown-call-unsafe回流、raw旧当前状态、Default待决误标均已修正；0019/0020对origin/work全文无差异。日志 `/tmp/goxide-closed-wire-final-race.json`、`/tmp/goxide-closed-wire-acceptance-final.json`、`/tmp/goxide-closed-wire-final-vet.log`、`/tmp/goxide-closed-wire-final-design.log`。

本轮按用户授权提交并推送feat/macro-mvp工作分支；原文档tag保持同一对象，work/main不更新。未决非默认payload Default约束保留，不重问已撤回的raw别名问题。


## 当前增量：手动 struct Default 与默认 enum payload

用户已确认 `impl` 内不使用 `static`：首参数为 `self` 的函数是实例成员，否则是关联函数；零参 `default` 属于关联函数。本轮仅实现以下具体入口，不代表完整关联函数、Self/self 或 trait wrapper 后端：

```hgo
type Settings struct { Port int; Ready chan int }
impl Default for Settings {
    func default() Settings {
        return Settings{Port: 8080, Ready: make(chan int, 1)}
    }
}
#[derive(Default)]
type Service struct { Config Settings }
```

手动入口限定义包内非泛型、非别名 struct（可在同包另一 `.hgo` / native `.go` 文件定义），恰好一个零参成员，显式返回具体目标类型。签名及函数体由前端和真实 Go 类型检查共同验证；降低为 `DefaultSettings() Settings` 并登记协议，嵌套派生按字段声明顺序调用。手动与手动、手动与 derive、生成 helper 与已有声明冲突均拒绝。普通同名函数、跨包同名类型和 Go 零初始化不会创建协议。

enum 的 `#[default]` 可以选择现有有限单 payload 变体，只递归要求选中 payload 的 Default；非选中变体不需要该协议，但继续遵循原有 enum 类型和 payload 限制。未扩展递归 enum 的表示范围，也未支持手动 enum impl、通用容器默认或跨包协议消费。该阶段的 `#[serde(default)]` 仍保持 Go 零值策略；后续字段 default/skip 兼容变更见末节。

静态函数引用图检查拒绝 Default 可达的直接/间接递归调用依赖（包括经外层 derive 回到手动入口），按解析后的函数身份检查，局部同名函数不误判。它是有限静态递归诊断，不是动态回调分析或终止性证明。宏可引用明确登记的 helper，保留定义点身份及私有类型边界；item 宏生成 impl、泛型/外部 impl、`Self` 返回写法、额外成员和 impl/member 属性尚不支持，明确拒绝。手动 helper 的源码映射保留 impl 来源，递归错误回到 `.hgo` 而非格式化 Go 行号。

真实 [多文件业务](examples/manual_default/main.hgo) 包含两种手动构造、嵌套 struct、选中 payload、未选中无 Default 类型、独立 channel 和另一个包的同名 Settings。独立非作者 [接受测试](tests/acceptance/manual_default_test.go) 实际安装 CLI、仓库外转译、运行生成模块的 3 项 race 测试，并验证业务输出 `service 8080 service 8080 service 9090 SCSC`、跨包协议拒绝及定位反例。默认构造由业务显式调用，不改变 Go var/make/map 缺失读取。

复现（替换为实际 checkout 路径，需真实 Go 1.27+）：

```sh
cd /workspace/goxide
export PATH=/workspace/goxide-environment/go/bin:$PATH
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off
export GOCACHE=/workspace/goxide-restore-check/cache
export TMPDIR=/workspace/goxide-restore-check/tmp
mkdir -p "$GOCACHE" "$TMPDIR" /workspace/goxide-restore-check/bin
GOBIN=/workspace/goxide-restore-check/bin go install ./cmd/hgo
/workspace/goxide-restore-check/bin/hgo run examples/manual_default
go test -race -count=1 ./tests/acceptance -run '^TestManualDefault' -v
```

旧章节中“手动 impl 未实现”“仅 unit 默认”“非默认 payload 待选”描述的是先前阶段，由本节替代。完整回归及审查结果见本节末记录。


最终验收：Go **1.27.1 linux/amd64**、CGO=1、GCC 14.2.0、Python 3.12.14，本地离线工具链。最终完整 `go test -race -count=1 -json ./...` 返回成功：**15 个有测试包、217 宿主顶层测试、377 子测试**；另 1 包无自身测试。新增独立业务生成模块的 **3 项实际 race 测试**单独统计，不混入宿主数量。`go vet ./...`、完整设计检查 **13 条 Go 命令**及最终文档检查 **56 Markdown / 492 本地链接与锚点 / 20 RFC**通过。设计夹具不替代编译器验收。

独立非作者审查发现并复验修复：手动 Default 递归错误误用格式化 Go 行号；现为 impl 原始 `.hgo` 来源，单文件/多文件均有回归。引用降低后的映射也在依赖检查前刷新。宏定义点卫生、私有目标 helper 跨包泄露拒绝及非法属性另获独立复核。前一次整树运行只在新增源码定位断言失败；修复后上述最终整树命令完整成功，没有把失败轮与成功轮拼成通过。

本轮日志保存在新环境 `/workspace/goxide-restore-check/manual-default-final-race.json`、`manual-default-final-vet.log`、`manual-default-design.log`、`manual-default-final-docs.log`；重跑只需要当前 checkout 和工具链，不依赖这些日志或旧执行器临时文件。按用户授权提交并推送 `feat/manual-default`，不修改 main/work、原文档标签或另一工作区的研究改动。


## 当前增量：serde 字段 default / skip 接入 Default

基准 `71b4e9888843cbd93de683dcc31a86aa99e745fe`。这是明确的兼容变化：反序列化字段 `#[serde(default)]` 从“缺失时保留 Go 零值”改为“缺失时调用已登记 Default”；`#[serde(skip)]` 保持编码省略与输入消费验证，解码始终调用该字段的 Default 一次，不采用输入内容。两者都不再为缺协议字段静默兜底，诊断带字段来源；同名普通 `DefaultType` 函数不创建协议。

复用当前标量与同包具体类型 Default，包括手动非零 struct 构造、struct 派生及选中 payload 的 enum 默认。当前 struct 的容器读取与所有 required 字段检查成功后，按字段声明顺序处理缺失 default 与全部 skip；显式字段不调用自身 Default。嵌套字段解码可先调用自己的默认构造，后续外层错误、尾部输入错误或自定义副作用不回滚。

显式 null 仍走目标 codec：非空 enum/普通 struct 报 Type，原生 nullable `*T` 仍能接受 null；未知 variant、非法 payload、重复键、被跳过内容中的非法 JSON 均不会被默认值掩盖。指针与容器本轮没有新增 Default，所以标记 default/解码 skip 的此类字段会因缺协议而拒绝；无该属性的 nullable 字段仍可使用。Serialize-only 不构造字段，skip 不需要 Default 或该字段 serde 协议。

实现复用 Default 表达式生成与协议登记，不增加 JSON 特有的派生回填逻辑。缺字段 seen 标志和 bool 默认值使用不受包级 true/false 同名声明影响的表达式，防止显式字段被误当缺失。已有静态 Default 循环检测继续覆盖 Default → Deserialize → Default 依赖；不承诺动态回调或终止性分析。

[独立业务](examples/serde_defaults/main.hgo)与[非作者接受测试](tests/acceptance/serde_defaults_test.go)验证多文件、手动默认、嵌套、默认 enum 载荷、调用次数和顺序、skip 输入/输出、nullable null、结构化错误路径及精确字段诊断。真实 CLI 输出 `demo 8080 8080 STSS`。生成模块单独运行 4 项 race 测试；缺协议有 8 项反例。另有[布尔名字捕获回归](tests/prototype/serde_defaults_test.go)，其普通生成子进程不冒充 race 产物。

复现沿用上节的真实 Go、缓存和离线环境：

```sh
cd /workspace/goxide
GOBIN=/workspace/goxide-restore-check/bin go install ./cmd/hgo
/workspace/goxide-restore-check/bin/hgo run examples/serde_defaults
go test -race -count=1 ./tests/acceptance -run '^TestSerdeDefaults' -v
```

该阶段未增加容器级 serde(default)、自定义属性、Option serde、指针/容器默认、跨包协议消费或完整 trait；后续 Option 增量见末节。旧章节的 serde Go 零值记录由本节明确取代；Go 自身 var/make/map 缺失读取仍保持零初始化。

最终验收（Go 1.27.1 linux/amd64、CGO=1、本地离线工具链）：完整 `go test -race -count=1 -json ./...` 一次通过 **15 个有测试包、220 宿主顶层测试、385 子测试**，另 1 包无自身测试。新增生成模块 **4 项独立 race 测试**另计；`go vet ./...`、完整设计检查 **13 条 Go 命令**、文档检查 **56 Markdown / 501 本地链接与锚点 / 20 RFC**及 diff 检查通过。

独立非作者审查未发现阻断，实际额外核对普通同名函数、跨文件原生 Go 基础类型遮蔽、Default→Deserialize→Default 静态循环、外部类型缺协议与 Serialize-only skip。共享默认值生成与 seen 标记使用不可被 true/false 同名声明捕获的布尔表达式，真实生成代码回归通过。日志位于 `/workspace/goxide-restore-check/serde-defaults-final-race.json`、`serde-defaults-final-vet.log`、`serde-defaults-final-design.log`、`serde-defaults-final-docs.log`；无需旧执行器文件即可重跑。

按用户阶段同步授权提交并推送 `feat/manual-default`；未更新 main/work、冻结标签或另一工作区研究文件。当时后置的 Option serde 由下述独立切片实现。


## 当前增量：具体内建 Option 的 serde

基准 `4e239be12a34cbca0bba2a56363418876e47ee7a`。派生识别已登记的具体 `Option[T]` 元数据，使用核心 Serializer.None/Some 与 Deserializer.Option/Visitor，不依赖 serdejson 特有回填。struct 的 Option 字段缺失、显式 null 均为 None；非 null 值由 T 的对应方向 codec 解码为 Some。None 编码 null，Some 编码载荷；存在的错误值不回退默认。嵌套 `Option[Option[int]]` 与 `Option[*int]` 的 Some(None)/Some(nil) 会折叠为 JSON null，不提供 PATCH 三态保证。

Option 自身的 Default 是 None，不要求或调用 T.Default，`serde(default)` 也不产生 Some(default T)。但显式 Some 的正常 payload 解码会执行其内部字段声明的 Default。`serde(skip)` 输出省略，输入调用格式级 Skip，验证语法、重复键与预算，不调用目标字段或 payload codec；因此合法的任意 JSON 值都能被跳过，最终字段为 None。默认与 skip 的原有初始化顺序、副作用不回滚边界不变。

支持现有具体 scalar、struct、enum、原生指针、数组/slice/string-key map 载荷，以及外层容器中的 Option。未 skip 的 payload 必须具有请求方向的 serde 协议，即使实际值总为 None；两个方向独立检查。仍拒绝普通 enum 的直接 Option payload 或含 Option 的 struct payload，以及 Option 内穿过指针/容器的 enum 投影，例如 `Option[[]Option[int]]`。不扩展 Result serde、通用泛型 trait、跨包 codec 或新属性。已有 enum 投影拒绝保留 generated/unmapped 标记；新增 codec 拒绝保留原字段位置。

[多文件业务](examples/option_serde/main.hgo)与[独立接受测试](tests/acceptance/option_serde_test.go)覆盖缺失/null、Some、嵌套容器、普通 enum payload 的现有限制、真实单向派生、手动默认调用次数、skip 格式消费、原始字段诊断与 Field/Index/MapKey/Variant 错误路径。非法 native Go case 指针/typed nil/伪造值编码返回 Type，不 panic。真实 CLI 输出 `option 7 -1 4 0`。生成模块显式运行 8 项 race 测试，另有 9 项边界拒绝。

独立审查发现并修复合法 payload 类型名被生成局部变量捕获：容器分配类型改在包级解析，struct 解码使用命名结果，builtin 实例 validator 使用编译器保留变量名。[名字捕获回归](tests/prototype/option_serde_test.go)运行实际生成代码；此普通子进程不计为 race 产物。

复现沿用上节工具链和离线环境：

```sh
GOBIN=/workspace/goxide-restore-check/bin go install ./cmd/hgo
/workspace/goxide-restore-check/bin/hgo run examples/option_serde
go test -race -count=1 ./tests/acceptance -run '^TestOptionSerde' -v
go test -race -count=1 ./...
go vet ./...
python3 scripts/check_design.py
```

最终验收：Go 1.27.1 linux/amd64、CGO=1、本地离线工具链。完整 `go test -race -count=1 -json ./...` 一次通过 **15 个有测试包、223 宿主顶层测试、394 子测试**，另 1 包无自身测试；新增生成模块 **8 项独立 race 测试**另计。`go vet ./...`、完整设计检查 **13 条 Go 命令**、文档检查 **56 Markdown / 511 本地链接与锚点 / 20 RFC**及 diff 检查通过。独立非作者复审和定向复验确认上述名字捕获修复，无已知剩余阻断。

日志位于 `/workspace/goxide-restore-check/option-serde-final-race.json`、`option-serde-final-vet.log`、`option-serde-final-design.log`、`option-serde-final-docs.log`。按用户授权在 `feat/manual-default` 提交并推送，随后 fetch 核验；不更新 main/work 或冻结文档标签。


## JSON 底层 Go cause 保留

基准 `2c353e4282892b289bc202c0a22fae333d9348ca`。格式后端将实际 Token、strconv 数值/Unicode 转换错误保存为 serde.Error.Cause，维持原 Kind、Path、消息和检测顺序。errors.As 可取回 json.SyntaxError 的原始 Token Offset 与 strconv.NumError，errors.Is 保留底层 sentinel。自行检测的额外合法顶层值、Unicode 配对错误、无 strconv 错误的浮点下溢等不伪造 cause。已有路径包装继续保留内层错误链；不重算 offset、不添加统一位置 API。

[格式层回归](serdejson/cause_test.go)与[独立生成业务验收](tests/acceptance/serde_causes_test.go)覆盖正常输入、字段/索引/变体内错误、Token/容器结束/尾部失败、数值解析与自检边界。[订单示例](examples/serde_causes/main.hgo)显式用普通 Go errors.As/Is 分类错误。此轮仅补错误证据保留，不改变截断输入的既有 Kind/Path 分类。

ROADMAP 的 Default 待答记录已修正：仅选中 enum 默认变体要求 payload Default。格式 JSON Value/任意精度 Number、JSON native adapter、PATCH 字段 adapter 仍未实现；普通 Go 调用与手写 typed helper 可用。enum/Option 载荷及投影限制保持，不新增适配 API 或自动 MarshalJSON/UnmarshalJSON 调用。

最终验收：Go 1.27.1 linux/amd64、CGO=1、本地离线工具链。整树 `go test -race -count=1 -json ./...` 一次通过 **15 个有测试包、228 宿主顶层测试、406 子测试**，另 1 包无自身测试；新增生成模块 **5 项独立 race 测试**另计。真实安装 CLI、仓库外运行及独立生成二进制的 4 种输入通过；正常/范围/语法/类型输出分别为 `accepted 1`、`range`、`syntax 1`、`rejected`。独立错误链探针确认 SyntaxError Offset=1、NumError/ErrRange 可识别、自检 cause=nil。

`go vet ./...`、设计检查 **13 条 Go 命令**、文档检查 **56 Markdown / 517 本地链接与锚点 / 20 RFC**及 diff 检查通过。独立非作者复审无阻断；对基线与修复后 **10,095 组输入/reader** 的差分检查确认错误文本（含 Kind/Path）完全一致。差分脚本位于 `/workspace/goxide-cause-review/compare.py`；真实 Token.Offset 不推导为其他调用序列或 json.Unmarshal 的位置。

复现使用前述真实工具链与离线环境，执行 `go test -race -count=1 ./...`、`go vet ./...`、`python3 scripts/check_design.py`；独立验收可用 `go test -count=1 -v ./tests/acceptance -run '^TestSerdeCausesInstalledBusinessRace$'`，其生成模块明确启用 race。日志 `/workspace/goxide-restore-check/serde-causes-final-race.json`、`serde-causes-final-vet.log`、`serde-causes-final-design.log`、`serde-causes-final-docs.log`、`serde-cause-probe.log`。按授权提交推送 feat/manual-default 并 fetch 核验，不修改 main/work/冻结标签，不发布或合并。


## 普通 Option 缺失不依赖 Default

基准 `f5c07874ece5ff3130f5e8b1c8f96d40ffc95cd5`。普通 Option 字段缺失由 serde 派生 visitor 直接构造 None，跳过 Default 检查与表达式生成；JSON null 继续走现有 Deserializer.Option/VisitNone。显式 serde(default)/skip 仍复用已批准 Default 行为。此次是职责解耦，不改普通字段的返回值、调用顺序或错误策略，不新增 adapter。

[扩充验收](tests/acceptance/option_serde_test.go)让无 Default 的 InputPayload 处理缺失/null/Some，并验证对它派生外层 Default 仍拒绝；普通 Option 缺失/null 不调用带副作用的 Settings 默认构造。显式 Option default/skip 和普通字段 serde-defaults 业务同时回归。Option 生成模块现有 9 项独立 race 测试；既有 serde-defaults 模块 4 项另计。RFC 新增固定版本标准库、Serde missing_field/OptionVisitor、JSON null 与自定义字段缺失源码依据，明确普通 Option 不是依赖 Default 才得到 None。

最终验证：Go 1.27.1 linux/amd64、CGO=1、本地离线工具链。整树 `go test -race -count=1 -json ./...` 一次通过 **15 个有测试包、229 宿主顶层测试、406 子测试**，另 1 包无自身测试。定向验收真实安装 CLI、仓库外转译，并分别运行 Option **9 项**、serde-defaults **4 项**生成模块 race 测试，业务输出 `option 7 -1 4 0` 和 `demo 8080 8080 STSS`。生成模块数量与宿主统计分开。`go vet ./...`、完整设计检查 **13 条 Go 命令**、文档检查 **57 Markdown / 527 本地链接与锚点 / 20 RFC**及 diff 检查通过。

独立非作者检查控制流并重跑 Option 生成模块 race 与无 payload Default 负例；文档复审发现并修正四处固定版本源码行号和一处旧测试计数，最终复核无剩余阻断。日志位于 `/workspace/goxide-restore-check/option-protocol-targeted.log`、`option-protocol-final-race.json`、`option-protocol-final-vet.log`、`option-protocol-final-design.log`、`option-protocol-final-docs.log`。按授权在 `feat/manual-default` 阶段提交推送并 fetch 核验，不修改 main/work/冻结标签；adapter 仍未批准、未实现。

## T0 收口：if initializer 的问号传播（2026-10-02）

从 `feat/manual-default` 的 `fb74ba2380389b39afe92f35ebdb8500fab74adf` 建立 `feat/t0-completion`。现场未发现仓库/workspace AGENTS.md 或 .agents/skills 内容；工具链为 `/workspace/goxide-environment/go/bin/go`，Go 1.27.1 linux/amd64，CGO_ENABLED=1，缓存放 `/tmp/goxide-cache`。没有使用环境默认 work 作为实现基线。

实现 `if n := load()?; condition(n)? { ... } else { ... }`：initializer 与条件 prelude 留在包含两个分支的普通词法块中，声明不泄漏；失败退出原函数，保留条件短路和 defer，不制造闭包。支持普通 initializer、赋值 initializer、else-if 和 Option。独立审查另外发现裸 `load()?` 表达式语句的 payload 临时量不能作为 Go 独立语句；现显式丢弃成功值，保留传播与普通调用的求值。

[真实前端验收](tests/acceptance/propagate_if_test.go)新增四项生成模块 race 测试及四项拒绝子例，分别核对求值/失败/清理、作用域/遮蔽、Option/else-if、赋值/闭包和成功值丢弃。[业务源](examples/prototype/if_propagation.hgo)通过真实安装 CLI、仓库外转译、生成 Go `run -race` 和 CLI run，输出 `port 8080`、`disabled`、`invalid port`。这些属于真实 hgo→Go 执行；设计检查中的纯 Go 机制探针另计。

独立非作者审查已复核修复并重跑 `TestPropagationIfInitializer*`，无剩余阻断。完整范围与四类剩余清单已同步 [T0 收口核对](docs/t0-acceptance.md#2026-10-02-t0-收口核对) 和 RFC 0005 §6.2；循环等传播上下文仍未实现，Proposed adapters 未获批准/未实施，整项 MVP 尚未完成。

最终验证：全树 `go test -race -count=1 -json ./...` 通过 **15 个有测试包、232 宿主顶层测试、410 子测试**（另 1 包无自身测试）。新增生成模块四项 race 测试单列，不重复计入宿主数量；安装 CLI 业务产物另执行 `go run -race`。`go vet ./...`、完整设计检查 **13 条 Go 命令**、最终 docs-only **57 Markdown / 534 本地链接与锚点 / 20 RFC**、`git diff --check` 通过。日志为 `/tmp/t0-race.json`、`/tmp/t0-race.err`、`/tmp/t0-vet.log`、`/tmp/t0-design.log`、`/tmp/t0-docs.log`；它们是本环境证据，不作为另一机器的文件依赖。

## T0 收口第二组：多 RHS、取值与发送

基于 `ab02dc3a26860447a3eb6927013271725a0b3868` 继续 `feat/t0-completion`，本阶段实现标识符目标的多 RHS 赋值/短声明、单 var spec 多值初始化、指针解引用、类型断言（含单 RHS comma-ok）及 channel/value 中的问号。前面的运行时操作数先捕获，全部成功才执行原同步赋值；发送先捕获 channel，再计算值，成功才发送。失败保持最近函数/闭包早退与 defer，不增加公共 Try/Residual、From 链或新语法。

独立审查发现并修复非恒定位移丢失上下文类型的问题；现在具体标量临时量保留类型，局部名义类型别名在闭包降低前登记，覆盖 block/case/select 的语句列表和标签，跨文件包级生成名带 namespace。接受测试覆盖 63 位 uint64、局部类型遮蔽及嵌套闭包；原有调用参数/二元表达式捕获复用同一类型保留逻辑。单 RHS 元组不会拆成标量临时量，普通 Go panic 不改成业务 Err。

[新增真实前端验收](tests/acceptance/propagate_values_test.go)的一个生成模块含七项 race 测试，覆盖顺序/不部分写入、常量/nil/位移类型、多次传播、短路/闭包、解引用/断言、nil/关闭/rebind channel；六项拒绝核对间接目标、数量/类型错误、跨家族和只收 channel。上一阶段 if initializer 的多 RHS 拒绝现改为成功覆盖，其他三项拒绝保留。另一个[两文件任务投递业务](examples/propagate_values/main.hgo)经真实安装 CLI、仓库外转译、生成模块两项 race 测试及 CLI run，输出 `port 8080 priority 7`、`invalid port`；errors.As 验证 strconv.NumError 保留，失败没有发送。生成模块测试和宿主统计分开，纯 Go 机制探针不算前端交付证据。

独立非作者复审重跑新测试和其自行编写的真实 hgo 反例，修复后无剩余阻断。[RFC 0005 §6.3](docs/rfcs/research/0005-option-result-question.md#63-t0-收口多-rhs取值与发送)及[矩阵剩余例子](docs/t0-acceptance.md#t0-收口第二组多-rhs解引用断言与发送)已同步。索引/切片/字面量/间接目标、循环/switch/select、match 组合、权限跟踪限制、公开引用元数据及诊断仍未整体完成；select 候选传播组合与 Proposed adapter/原生桥接待决规则单列，不视作用户批准的延期，也不扩大到长期别名分析或完整 trait。

最终验证：`go test -race -count=1 -json ./...` 通过 **15 个有测试包、235 宿主顶层测试、415 子测试**（另 1 包无自身测试）。本组两个实际生成模块另有 **7+2 项 race 测试**，不重复计入宿主数量；真实安装 CLI 及业务执行通过。`go vet ./...`、完整设计检查 **13 条 Go 命令**、最终 docs-only **57 Markdown / 543 本地链接与锚点 / 20 RFC**、`git diff --check` 均通过。最终日志为 `/tmp/t0-values-final-race.json`、`/tmp/t0-values-final-race.err`、`/tmp/t0-values-vet.log`、`/tmp/t0-values-design.log`、`/tmp/t0-values-docs.log`；审查修复前中断的旧 race 日志不计作最终验证。

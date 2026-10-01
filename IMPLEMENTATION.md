# 可运行的宏与 serde 原型

此实现分支从 `v0.0.0-mvp-doc` 开始；冻结标签与研究工作区未改动。
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
- `Error`：`#[error("failed {Code}")]`、显式 `#[source]` 的 error 字段生成 Unwrap；单个 error 字段的 `#[from]` 生成 `FromType`。无动态错误变体推断、完整 enum 或问号实现。
- 日志示例调用 Go `log/slog`，验证重复字段参数和错误链；不是完整 tracing/span 运行时。

代表写法见 [订单](examples/prototype/order.hgo) 和 [日志与错误](examples/prototype/log_error.hgo)。源文件采用 Go package/import/function/field 语法，派生属性独占行；也支持有限 `struct Name` 声明简写。未实现 Rust/hgo 整套语法。

## 验证边界

`go test ./...` 包含运行生成 Go 的测试，覆盖宏卫生/求值/控制流、派生、serde 正负例与 CLI 输出保护。
`go test -race ./...` 检查测试进程和 runtime；测试中另启的生成模块普通 `go test` 不自动继承 race。
`python3 scripts/check_design.py` 仍检查旧设计夹具与文档，不作为编译器实现完成证明。
`tests/design` 和 `tests/research` 保留独立模块，防止各自隔离示例被根模块错误合并。

源码映射是原型：derive 记录 helper 范围及展开输入的类型来源；宏 token 保存格式化前的输出偏移及调用位置。
它不是最终 Go 文件逐 token、逐字段的完整映射，也不是 LSP。

未完成：泛型 derive、跨包宏、用户过程宏、完整 trait、泛型/跨包 enum、Option/Result/引用前端、YAML、格式 Value、借用解码和原生 JSON codec 自动适配。
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

这是候选 interface+值型case ABI的有限实现，未冻结ABI；仅match使用点验证，未实现全入口/初始化检查。case/helper当前未导出，外部Go构造尚未支持。泛型、复杂payload、表达式match、嵌套模式、Option/Result特殊规则仍未实现。枚举声明不是serde专用表示；下一阶段在这份元数据上补serde派生。

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

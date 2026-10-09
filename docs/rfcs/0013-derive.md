# RFC 0013：Default、Clone、Debug 内建派生

本轮 A06 的单文件外部已登记协议恢复，按
[RFC0001 实施前合同](0001-package-loading.md#a06-已知-go-module-的单文件外部恢复2026-10-06实施前合同)
实施。复用下文已有 canonical identity、签名和来源验证；手写 Clone/Debug/serde
登记不在本片新定，结果见[单文件外部恢复报告](../reviews/single-file-external-recovery-20261006.md)。

## 状态与范围

已确认目标：内建 `#[derive(Default, Clone, Debug)]`；用户可编程 derive 延期（D14）。
Default 的字段约束、enum 默认变体规则与无 static 的关联函数声明已确认；通用静态降低、泛型证据和 formatter 仍未定。
依据：[原设计](../design/macros.md)。derive 生成普通显式 impl，不能绕过 trait / 借用检查。类型关联函数/构造函数见 [RFC 0020](0020-associated-functions.md)；同名构造不会自动实现 Default。

## 源级规则

当前序列化派生由 serde core 拥有：先 `use serde;`，再写 `#[derive(serde::Serialize, serde::Deserialize)]`。显式导入某一派生或改名后按 canonical owner 解析；缺失/错误 owner、JSON owner、同名普通类型均拒绝。旧全局裸名不再保留。见[静态路径合同](../design/static-paths.md)。

最新 D28 补充确认：struct 自动 Default 必须每个字段有 Default，否则手动实现；enum 恰好标记一个默认变体，可以携带 payload，仅该变体的字段递归要求 Default，非默认变体不要求 Default。手动 Default 首期使用 `impl Default for T { func default() T { ... } }`，限同包本地非泛型 struct，具体边界见 [RFC 0020](0020-associated-functions.md#当前手动-default-实现子集)。

- 按规范 trait 身份选择固定内建规则；只读目标类型形状和固定辅助属性。
- Default 按声明顺序调用字段 Default；Clone 按同一顺序调用字段 Clone。
- Debug 按声明顺序格式化字段；enum 保留并格式化当前合法变体。
- 派生在目标类型包中生成实现；不读取外部类型私有布局、不修改字段、不新增导入。
- 字段缺少协议、同键重复实现或未知 derive 报错，不静默覆盖或降级为 Go 赋值。
- 普通赋值仍服从既有值复制规则；显式 Clone 不引入全局 move，也不承诺对象图深复制。

## 转译示例

**D01：非零业务默认值和具体类型 Clone。** goxide 声明片段。
依赖：`Label` 已有显式 Default（返回 `"untitled"`）、Clone（同值复制）和 Debug。

```goxide
#[derive(Default, Clone, Debug)]
pub struct Record {
    title Label
}
```

Go 对照：以下是具体类型的 Default / Clone 核心，省略与本例无关的字段导出策略。
函数名仅表示已选 impl；不冻结导出名和泛型 ABI。

```go
type Label string
type Record struct { title Label }

func defaultLabel() Label { return "untitled" }
func cloneLabel(value Label) Label { return value }

func defaultRecord() Record {
    title := defaultLabel()
    return Record{title: title}
}
func cloneRecord(value *Record) Record {
    title := cloneLabel(value.title)
    return Record{title: title}
}
```

`defaultRecord().title == "untitled"`；`var value Record` 的空字符串不是等价实现。
`cloneRecord` 的 Go 指针表示源级只读借用；Go 签名本身不提供该检查。
Debug 同样选择具体字段实现；其 formatter 签名尚未冻结，不能从上述函数自行推导 ABI。

**D02：共享字段的 Clone 仍共享。** goxide 声明片段；依赖 Arc 的显式句柄 Clone。

```goxide
#[derive(Clone)]
struct Handle {
    shared Arc[Counter]
}
```

Go 对照：核心片段依赖普通 `Counter` 类型；示例不读取或修改 Counter 内容。

```go
type Handle struct { shared *Counter }
func cloneArcCounter(value *Counter) *Counter { return value }
func cloneHandle(value *Handle) Handle {
    shared := cloneArcCounter(value.shared)
    return Handle{shared: shared}
}
```

约束：克隆前后 `shared` 指针相同；不因此获得可变权限或要求 `Counter: Clone`。
锁句柄也必须共享同一锁和状态，不能按值复制内部 Go mutex。

**D03：缺少字段协议必须拒绝。** goxide 反例，无 Go 输出。

```goxide
#[derive(Clone)]
struct Exclusive {
    value mut& int
}
```

依赖借用类型规则；若 `mut& int` 尚无明确 Clone 协议，派生按缺协议拒绝；mut& 不承诺独占，不能因复制别名本身拒绝（D19/D20）。
结构合法性若已在更早阶段拒绝，也不得由 derive 把它变成可接受程序。

## 拒绝与边界

- enum 的 Default 要求恰好一个 `#[default]` 变体；按字段声明顺序构造其 payload。缺少或重复标记、选中字段缺协议及非法属性位置均拒绝；不检查非选中变体的 Default。
- Result 不自动取得 Default；nil 不因 Default 或 Go 零值存在就成为合法 Result / 非空引用。
- 泛型 `var value T` 不能冒充 Default；容器需明确逐项调用选中的构造。
- 字段 serde(default/skip) 解码调用已登记 Default，缺协议拒绝；default 只处理缺失，skip 忽略输入并默认构造一次。本层读取及 required 检查后按声明顺序调用，显式值/错误不回退默认。Serialize-only 无解码 Default 约束。此项替代旧 Go 零值策略，详见 [serde RFC](research/0006-serde-core-and-formats.md#71-字段-default-接入与兼容变化)。
- 容器 Clone 必须说明存储、元素和别名行为；Go `slices.Clone` 不自动等于逐元素 Clone。
- 内建具体 Option serde 的缺失/null 为 None；default/skip 沿用 Option 自身 None 默认，不调用载荷 Default。未 skip 的载荷必须有对应方向 codec；错误值不回退。skip 仅消费格式/预算，不检查目标类型。具体限制见 [serde RFC](research/0006-serde-core-and-formats.md#72-内建具体-option-派生)。
- Debug 展示struct字段名和内容、enum源变体名和payload，包括私有业务数据；serde rename/value不改变显示。不自动脱敏，不绕过锁权限，不通过反射/Stringer/error兜底，也不是稳定序列化格式。
- Default 无接收者、Clone 返回具体 Self；不得抹成 `any` 塞进统一运行时接口。

## 双向互操作

goxide 调用生成实现与手写 impl 使用同一套选择、权限和唯一性规则。
Go 使用发布 `.go` 中公开的普通函数 / 类型；Default 不自动改变 Go 的零初始化行为。
原生 Go 调用方不受 goxide 借用检查；派生不能被描述为给整个 Go 程序增加所有权保证。
回到 goxide 时由 `.d.gox` 或 `.gox` 恢复协议信息，不从 Go 方法形状猜测派生语义。

## 验收与未定项

1. D01 验证业务默认值；多字段版本记录顺序，确保每个字段协议只调用一次。
2. D02 验证共享身份；独立容器复制必须另用已选定的容器 Clone 契约验收。
3. D03、缺少字段 Debug、重复 impl、多个 default 变体均不得生成成功产物。
4. 成本：逐字段协议调用；递归、分配、共享及副作用取决于字段实现，不承诺统一复杂度。
5. 未定：接收者类型与权限拼写、泛型字典或专门化、Debug formatter 和稳定符号编码。
6. 未确定的泛型形式不得假称已有可编译降低；Go 示例不证明通用派生器已实现。

当前手动入口与选中 payload 的实现证据见[独立业务验收](../../tests/acceptance/manual_default_test.go)；既有实现证据见[Default/打印验收](../../tests/acceptance/print_default_test.go)与[封闭映射验收](../../tests/acceptance/enum_wire_test.go)。泛型与完整 trait 后端仍后置；手动 Default 的具体子集不代表完整关联函数后端。

## 跨包已登记 Default 字段（2026-10-03）

目录/模块编译恢复生产包显式登记的具体 Default：消费 struct 的 `m.E` / `m.Settings` 字段可调用生产包 helper，包含透明 alias 和手动实现。没有登记的同名 `DefaultT` 函数、新 defined 类型不继承协议；不猜 helper 名。schema v5 记录目标、canonical 定义、helper 及 derive/manual 来源，核验真实 named 身份与精确非泛型 `func() T` 签名，alias 的来源必须与定义包一致。旧产物需重新生成，内容、依赖及 marker 绑定继续核验；Default-only 包也写出 artifact。

[真实 CLI 三模块验收](../../tests/acceptance/default_external_test.go)覆盖私有字段的业务默认值、字段调用次数、跨文件同名 import、透明 relay、宏 len 的 import 改名组合、serde skip 共用 Default，以及独立原生 Go 消费；三个生成/消费模块分别 race/vet，重复生成一致。[协议反例](../../internal/frontend/default_artifact_test.go)覆盖 helper 变量/泛型/参数/错误返回、假注册、new defined、alias 来源改写及陈旧 producer。原跨包 Default 字段负例已迁为[真实源包正例](../../tests/acceptance/manual_default_test.go)，外部目标 impl 仍拒绝。

struct 仍要求所有字段有协议；enum 仅选中默认变体要求 payload Default。此次不放开 enum 的外部载荷类型限制，不新增跨包 Clone/Debug/Serialize/Deserialize、完整 trait 或字段 adapters。internal-origin 不可直接导入的协议暂不可用，普通公开 Go API 保留。单文件外部协议恢复、容器默认语义及零值/公开入口契约不在本片修改；普通 Option 缺失/null 到 None 仍独立于 Default。

## 跨包已登记 Clone/Debug 字段（2026-10-03）

目录/模块模式支持消费生产包明确派生的具体 struct/enum Clone、Debug。qualified 字段可经生产者公开透明 alias 转发，跨文件相同 import 名按实际包路径区分。内部 metadata v6 在 v5 校验链上新增 Kind/目标/helper/derive 来源，检查 canonical named 身份及精确 `func(*T) T` / `func(*T) string` 签名；alias 不得凭同名函数或正确签名自行增加协议。内容、marker、依赖产物及 canonical 注册均须一致，旧生成模块需重新生成。

[真实 CLI 三模块验收](../../tests/acceptance/field_protocol_external_test.go)覆盖具体 enum/struct、透明 relay、跨文件/包名差异、array/slice/map 嵌套元素协议、nil/空区别、普通 goxide/Go 赋值仍共享容器、显式 Clone 的独立副本及不保留别名拓扑。当时以包级 type alias 保护容器类型；2026-10-08 改为统一分配无冲突局部名，保留源类型表达式及其文件 import 环境。Debug 保留生产类型/变体名、字段名、字符串和 map key 引号及内容（含私有字段），不自动脱敏；无需消费者反射或绕过字段可见性。三个生成/消费模块分别 race/vet，生成结果重复一致，原生 Go 可调用公开 helper。

[协议反例](../../internal/frontend/field_protocol_artifact_test.go)核验参数/返回类型、函数变量/泛型/variadic 拒绝、重复/未知/手写来源拒绝、relay 伪造另一 Kind 及陈旧 producer；CLI 对摘要篡改和源文件变化在消费者 import 位置报错。手写 Clone/Debug 尚无已实现登记入口，本片仅 derive 来源；消费者本地 `type Local = m.T` 字段的协议转发仍未接通，不能把该工程缺口说成类型身份不同。不可直接导入的 internal-origin 协议仍不可用，普通 facade API 保留。外部 enum 载荷类型范围、pointer/interface 自动协议、完整 trait、serde 和零值/公开入口验证不在本片扩展；borrow/Copy/指针拆分不作为本片前置条件。

## 跨包已登记 Serialize/Deserialize 字段（2026-10-03）

目录/模块模式按实际 import 路径恢复生产者显式 derive 的具体 struct/enum serde 协议，消费 struct 的 qualified 字段及既有 array/slice/map/pointer/Option codec 递归调用生产者 helper；公开透明 relay alias 可转发，多文件同名 import 不混淆。Serialize 与 Deserialize 独立登记/要求，不通过 helper 名、反射、Go 方法集或 encoding/json 自动实现兜底。

复用 metadata v6 的 FieldProtocols 记录，不新增 schema 字段或版本；Kind 扩展为 Serialize/Deserialize。核验 canonical named 类型、derive 来源、alias 定义包登记，以及精确非泛型 `func(serde.Serializer, *T) error` / `func(serde.Deserializer) (T, error)`，其中 core 类型必须与实际 serde 包身份一致，error 必须是内建 error。内容、依赖、marker 校验保持。旧 v6 产物若缺少对应协议记录必须重新生成；不能把旧 helper 的存在当作登记。

真实 CLI 三模块验收覆盖 producer struct/enum、透明 relay、单向协议、跨文件导入、容器和 Option、原生 Go helper 消费，三个生成/消费模块分别 race/vet。验证嵌套 field/variant path、Range/Syntax 的 Go cause、未知变体、生产者 deny_unknown_fields 与消费端忽略额外字段、忽略值深度预算、非法 enum nil/typed-nil/指针 case、nil 容器编码为空且解码为非 nil 空、普通 Option 缺失/null 为 None。没有把它们改为 Default 或其他新语义。重复生成一致；伪 helper、新 defined 类型、缺方向、篡改及陈旧产物拒绝，产物错误定位消费 import。

边界延续：消费者本地 `type Local = m.T` 字段转发尚未接通；手写 serde 登记、单文件外部协议恢复、不可直接导入的 internal-origin、普通 enum 外部 payload 类型范围均未借本片扩大。字段 adapters 的入口/缺失策略仍 Proposed，完整 trait/native bridge、Value/PATCH/YAML、公开函数 enum 参数入口验证不在本片实施。完整 T0 MVP 尚未据此宣告完成。

## 消费者本地透明 alias 的外部协议恢复（2026-10-03）

消费者包级 `type Local = producer.T`（包括多级 alias、native `.go` 中声明、生产者公开 relay）现统一恢复已登记 Default、Clone、Debug、Serialize、Deserialize 和 PartialEq/Eq。使用保留各文件 import 作用域的 Go 类型解析，只对真正 alias 的 Unalias 结果与已验证 canonical 类型作 Identical 匹配；新 defined 类型、alias 指向新 defined 类型、同名/相同底层结构不继承协议。非类型语句留给既有最终完整包检查，不从源码名称猜 helper。

沿用 metadata v6 及既有签名、来源、内容、依赖、marker 校验，无新 schema 或旧数据宽松恢复。协议绑定放在包级，goxide 文件没有源 import 时也能在 package 声明后插入生成 import。收集后的真实 TypeSpec/Imports 保留 `=` 和宏卫生改名；不新增宏模板 type selector 语法。

真实三模块验收 `tests/acceptance/local_protocol_alias_test.go` 覆盖各协议、生产者 manual Default、多级/重复导入/native-only alias、不同包相同导入名、同名局部类型拒绝、同名局部变量、容器与已支持的 Option serde/equality、独立 Go helper 消费、重复生成一致、新 defined/假 helper 拒绝及错误摘要定位原生 consumer import。生产/消费/Go消费模块分别 race/vet；独立审查另验证宏 import 改名与 alias 共存。此处 Option 不宣称 Clone/Debug 已实现。

边界：本片恢复外部 canonical 的已登记协议；同包原始类型经本地 alias 的 Default/Clone/Debug/serde 转发仍未接通，既有 Eq 已支持。单文件外部协议、不可直接导入 internal-origin、其他载荷/字段形状和未实现协议入口继续留账；不是透明 alias 身份发生变化，也不是获批延期。公开 enum 函数入口验证仍待用户决定，未混入指针阶段、完整 trait/manual serde 或字段 adapters。

## 同包已登记类型的透明 alias 协议恢复（2026-10-03）

同包具体原始类型的包级透明 alias 现与外部 alias 共用 `bindLocalProtocolAliases` 的 Go Unalias/Identical 身份解析。Default/Clone/Debug/Serialize/Deserialize 从既有 models/manual Default 显式登记恢复，不逐派生新增 alias 分支；既有 Eq 继续按真实类型身份工作。支持跨文件、多级 alias、native `.go` alias 和单文件编译。新 defined 类型（即使底层相同）、普通同名 helper 不产生登记；alias 上再 derive/impl 及重复实现按既有规则拒绝。

本地 helper 直接引用登记对应函数，不新增函数值全局初始化依赖；这不撤销现有 `checkDefaultCycles` 的静态递归拒绝策略，也不声称递归 Default 已支持。手写函数签名/返回、冲突、最终完整 Go 包仍照常检查。metadata 保持 v6，外包签名/来源/摘要校验不变。

真实证据 `tests/acceptance/same_package_alias_test.go`：独立目录生成模块与单文件生成模块分别 race/vet，执行 manual Default、enum 默认 payload、Clone、Debug、serde 往返、equality、容器独立副本、反射身份及局部同名 helper/变量；反例覆盖新 defined、无登记 helper、alias impl/derive、重复 Default、错误方向和错误手写返回类型。外部 alias 三模块继续回归。blank/embedded 字段没有悄悄新增跳过/展开/命名规则。

## 已登记具体 Option/Result 的 Clone/Debug 组合（2026-10-03）

最小旧失败例 `#[derive(Clone,Debug)] type S struct { V Option[[]int] }` 及对应 Result 字段现已接通。既有 Option serde/equality 与普通 enum Clone/Debug 不被当成此项证据；新实现经统一字段协议 resolver 分派，并用本地 lowering 或已验证跨包 family registry 中 validator 的精确参数类型核对真实 origin 和全部具体参数。类型名相似、同 family 不同具体实例、新 defined 类型均不能冒认协议；metadata 保持 v6，无新 schema。

Clone 在通过既有 validator 后只复制活跃分支：Option None 无载荷操作，Some 按载荷 Clone；Result 只处理 Ok 或 Err 实际载荷。静态资格仍要求所有可能载荷具备所需协议，不能因某个调用当前是 Ok 而放过无 Debug 的 Err 类型。递归复用既有字段/容器协议，普通 Go 赋值与别名拓扑规则不变。Debug 明确展示 `Option::None`、`Option::Some(nil)`、`Option::Some([])`、`Result::Ok(...)` / `Result::Err(...)`，保留字符串引号、nil/空区别及载荷内容。

Default 仅在实例身份确认是已登记 Option 后返回 canonical None；不要求载荷 Default，也不要求私有载荷可命名，不实例化无用 Some constructor。Result 继续没有隐式默认值；普通 enum 显式选中变体的既定 Default 规则不改。非法 nil Result、typed-nil/指针 case/伪造动态 case 仍按现有 validator 拒绝；没有扩大公开函数入口验证。

`tests/acceptance/sum_clone_debug_test.go` 提供真实单文件生成模块与独立 producer/consumer/Go-consumer 三模块 race/vet，覆盖深 clone、嵌套 sum、活跃分支、None/Some(nil)/Some(empty)、Err nil/空 map、跨文件/多级/native-only alias、反序 alias 参数、外部具体 Item 载荷、私有载荷 Option Default、不同实例拒绝、new defined/缺载荷协议/Result Default 反例，以及错误 artifact 定位消费 import、重复生成一致。跨文件收集保留必要 validator/constructor 函数签名；基本类型与容器优先按结构拼写，避免无关 slice alias 污染协议选择。独立审查对上述两问题修复后分别执行真实生成模块 race/vet。

边界：Clone/Debug 仍要求载荷的已有协议和可访问类型表达，未新增 pointer/interface 自动协议、手写 Clone/Debug、完整 trait 或递归对象图系统。blank/embedded 字段、公开 enum 入口契约不变。本片完成后先交回；命名返回 match 与动态已知类型函数参数 match 是下一独立工程片，未在此实施。

## 已有具体派生拒绝的原始位置（2026-10-05，实施合同）

对应 MVP 台账 A03。属性解析、生成 helper 冲突及其他已有行级派生诊断，在前置宏
展开改变行数之后仍须通过已保存的 source map 回到原始文件。原本只能给出行级
上下文的错误定位该行首个实际 token，不伪称已准确定位属性内部子表达式。已有
字段类型精确诊断继续保留；没有可证明来源的生成节点保持 generated/unmapped。

验收包括前置多行宏、多次展开、CRLF、目录编译不同文件、未知或
重复 derive/属性及 helper 冲突，断言结构化 origin 和显示行列。此片不新增手写
Clone/Debug/serde、typed Error source、blank/embedded 字段协议或泛型规则；合法
具体派生的生成与执行需继续回归。

enum 元数据坐标属于 enum lowering 之前，必须使用该阶段保存的 map 和 offset；
不得将其行号当作 refs/属性阶段行号。前一个 enum 的空行与降低行数不同、后面
存在同名声明的负例必须定位到实际 enum，不能误指随后声明。


## 无冲突容器类型拼写（2026-10-08）

Clone/Debug 的容器类型保护可以由生成局部绑定的统一 fresh-name 分配提供；不再
要求简单 `[]int` 必须额外生成 package type alias。生成的 `src`、`item`、`out` 等
绑定必须避开类型表达式真正使用的类型和 import 名，嵌套容器也遵守同一规则。
在原文件环境中保留类型表达式，不能通过新 defined type 或改用户类型名消除冲突。
若仍需 alias，只能在相同身份及文件解析环境下复用。

验收同时核对简单与嵌套容器、named type/import 遮蔽、nil/empty、普通赋值共享、
显式容器 Clone 的独立存储、元素协议以及类型身份/method set。Eq-only 不因此
新增 alias；不改变已有深浅复制、递归资格、手写协议或 blank/embedded 字段边界。

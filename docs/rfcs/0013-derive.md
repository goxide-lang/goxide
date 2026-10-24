# RFC 0013：普通库的 Default、Clone、Debug 与比较派生

## 状态与范围

std 与 serde 的业务派生是正式 proc-macro 库，输出普通 trait/impl；不保留编译器内建业务派生、Default 专用递归禁令或 derive 来源特权。当前发布与全树验收状态见[能力状态表](../mvp-gap-audit.md)。宏执行及卫生见 [RFC 0030](0030-procedural-macros.md)，条件泛型与实现选择见 [RFC 0031](0031-generic-protocol-impls.md)。

## 源级规则

显式导入库并选择宏路径：

```goxide
use std;
use serde;

struct Settings { Port int }
impl std::Default for Settings {
    func r#default() Self { return Self{Port: 8080} }
}
#[derive(std::derive::Default, std::derive::Clone, std::derive::Debug)]
struct Record { Count int }
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
struct Message { Text string }
```

宏只读取语法及已登记 helper 属性，生成普通声明；字段协议、类型身份、可见性、引用权限、归属与重叠实现由普通检查器处理。手写 impl 和宏产物使用同一路径，透明 alias 保留类型身份，新 defined 类型不继承协议。泛型派生输出满足字段业务需要的条件，不收紧原类型声明。

## 字段行为

| 协议 | 合同 |
| --- | --- |
| Default | struct 按字段声明顺序调用字段 Default；enum 恰好标记一个 `#[default]` 变体，只要求选中 payload 的协议；Option 返回 None 且不要求元素 Default，Result 不自动取得 Default |
| Clone | 按字段顺序调用明确的 Clone；enum 保留活动变体，容器逐元素处理并保持 nil/空区别；普通赋值仍为 Go 浅复制 |
| Debug | 按声明顺序显示类型、字段/变体名及内容，包括私有字段；complex64/complex128 与其容器/enum payload 均由普通 Debug impl 支持；不自动脱敏，也不是稳定序列化格式 |
| PartialEq | 调用字段的普通相等协议；slice/map 区分 nil 与非 nil 空值；enum 派生在比较前依次调用 `std::enum_assert!` 校验两侧活动 payload |
| Eq | 具有 PartialEq 前提的普通空 marker；显式 impl 是作者对等价关系的承诺，derive 按字段 Eq 条件生成普通 impl |
| NativeKey | 独立承诺 PartialEq 与稳定 Go 原生键相等一致，并要求 comparable；Eq 不自动授予此能力，enum 不自动派生 NativeKey |

Default 的方法体可以普通递归；协议前提循环和无限实例需求仍由通用机制诊断。字段缺协议、重复 impl 或无效 helper 均诊断，不以 Go 零值、反射或方法同名兜底。Debug 对 complex 的支持不自动提供 Clone、Default、Eq 或 Error 插值能力。

serde 两个方向独立。Deserialize 的 default 只用于缺失字段，skip 默认构造一次；完成本层读取和 required 检查后按字段顺序执行。Option 缺失/null 为 None，present 错误不回退默认，skip 只验证格式与预算。详见 [serde 合同](research/0006-serde-core-and-formats.md)。

## Go 互操作与验证

静态协议通过已选 impl 的普通入口调用，实例协议按 wrapper 与 carrier 合同生成；固有 Error 方法按 [RFC 0020](0020-associated-functions.md)直接落在 struct receiver 或 enum 接口/变体上。Go 调用者使用生成的普通公开入口，Goxide 下游从源码恢复协议关系，不读取旧 enum metadata，也不猜同名 helper。

正式 std/serde 宏的验证入口为 `internal/compile/formal_std_test.go`、`formal_serde_test.go`；真实 CLI 跨包与原生 Go 验收仍在 `tests/acceptance`。全树及发布验收状态单独见状态表。

## 旧管线实现记录

以下带日期的记录保留历史链接与当时实现背景；其中 metadata、helper 登记、特定 derive 来源及 Default 禁环均已由上述普通库合同取代，不是当前前端规则。

## 跨包已登记 Default 字段（2026-10-03）

目录/模块编译恢复生产包显式登记的具体 Default：消费 struct 的 `m.E` / `m.Settings` 字段可调用生产包 helper，包含透明 alias 和手动实现。没有登记的同名 `DefaultT` 函数、新 defined 类型不继承协议；不猜 helper 名。schema v5 记录目标、canonical 定义、helper 及 derive/manual 来源，核验真实 named 身份与精确非泛型 `func() T` 签名，alias 的来源必须与定义包一致。旧产物需重新生成，内容、依赖及 marker 绑定继续核验；Default-only 包也写出 artifact。

[真实 CLI 三模块验收](../../tests/acceptance/default_external_test.go)覆盖私有字段的业务默认值、字段调用次数、跨文件同名 import、透明 relay、宏 len 的 import 改名组合、serde skip 共用 Default，以及独立原生 Go 消费；三个生成/消费模块分别 race/vet，重复生成一致。[协议反例](https://github.com/goxide-lang/goxide/blob/bd90e606dcfa220b4054fe41c56bdc63a5687355/internal/frontend/default_artifact_test.go)覆盖 helper 变量/泛型/参数/错误返回、假注册、new defined、alias 来源改写及陈旧 producer。原跨包 Default 字段负例已迁为[真实源包正例](../../tests/acceptance/manual_default_test.go)，外部目标 impl 仍拒绝。

struct 仍要求所有字段有协议；enum 仅选中默认变体要求 payload Default。此次不放开 enum 的外部载荷类型限制，不新增跨包 Clone/Debug/Serialize/Deserialize、完整 trait 或字段 adapters。internal-origin 不可直接导入的协议暂不可用，普通公开 Go API 保留。单文件外部协议恢复、容器默认语义及零值/公开入口契约不在本片修改；普通 Option 缺失/null 到 None 仍独立于 Default。

## 跨包已登记 Clone/Debug 字段（2026-10-03）

目录/模块模式支持消费生产包明确派生的具体 struct/enum Clone、Debug。qualified 字段可经生产者公开透明 alias 转发，跨文件相同 import 名按实际包路径区分。内部 metadata v6 在 v5 校验链上新增 Kind/目标/helper/derive 来源，检查 canonical named 身份及精确 `func(*T) T` / `func(*T) string` 签名；alias 不得凭同名函数或正确签名自行增加协议。内容、marker、依赖产物及 canonical 注册均须一致，旧生成模块需重新生成。

[真实 CLI 三模块验收](../../tests/acceptance/field_protocol_external_test.go)覆盖具体 enum/struct、透明 relay、跨文件/包名差异、array/slice/map 嵌套元素协议、nil/空区别、普通 goxide/Go 赋值仍共享容器、显式 Clone 的独立副本及不保留别名拓扑。当时以包级 type alias 保护容器类型；2026-10-08 改为统一分配无冲突局部名，保留源类型表达式及其文件 import 环境。Debug 保留生产类型/变体名、字段名、字符串和 map key 引号及内容（含私有字段），不自动脱敏；无需消费者反射或绕过字段可见性。三个生成/消费模块分别 race/vet，生成结果重复一致，原生 Go 可调用公开 helper。

[协议反例](https://github.com/goxide-lang/goxide/blob/bd90e606dcfa220b4054fe41c56bdc63a5687355/internal/frontend/field_protocol_artifact_test.go)核验参数/返回类型、函数变量/泛型/variadic 拒绝、重复/未知/手写来源拒绝、relay 伪造另一 Kind 及陈旧 producer；CLI 对摘要篡改和源文件变化在消费者 import 位置报错。手写 Clone/Debug 尚无已实现登记入口，本片仅 derive 来源；消费者本地 `type Local = m.T` 字段的协议转发仍未接通，不能把该工程缺口说成类型身份不同。不可直接导入的 internal-origin 协议仍不可用，普通 facade API 保留。外部 enum 载荷类型范围、pointer/interface 自动协议、完整 trait、serde 和零值/公开入口验证不在本片扩展；borrow/Copy/指针拆分不作为本片前置条件。

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

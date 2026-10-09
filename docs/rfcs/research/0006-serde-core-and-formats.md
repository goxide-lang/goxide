# RFC 0006：按 serde-rs 分层的 serde 核心与格式适配

状态：用户已确定“仿照 serde-rs 的实现”，覆盖此前助手提出的统一 core Value、CoreValue-v1 和首版必须建完整树等候选。已确定 serde 优先、首版 JSON、未来 YAML 独立适配；下文 Go/goxide ABI、支持子集与本地策略仍是实施候选，不声称完整 Rust 兼容。初稿时尚无 serde 或宏前端实现；当前有限实现见末节，冻结标签 `v0.0.0-mvp-doc` 不变。

当前D27补充：语言只保留封闭变体enum。unit变体可用serde rename映射外部字符串，或完整显式int64 value映射数字；未知外部值返回UnknownVariant，业务validator延期。它不新增开放raw enum，不改变Clone/Debug身份；真实边界见[枚举专题](../../design/enums-and-match.md#01-封闭-enum-与外部映射)。

## 1. 官方设计与源码核对

本轮实际读取 serde-rs 官方文档及 serde/json 主分支源码。链接为移动分支，实施前应锁定参考版本；下表标明所核对的符号，不靠类名推断行为。

| 一手依据 | 核对事实 | goxide 采用方向 |
| --- | --- | --- |
| [数据模型](https://serde.rs/data-model.html) | 模型描述类型与格式交互的操作，含 struct/map、tuple/seq、enum、option、bytes 等区别 | 核心是协议，不是所有格式共用的 Value 树 |
| [serde_core/ser](https://github.com/serde-rs/serde/blob/master/serde_core/src/ser/mod.rs) 的 Serialize、Serializer、SerializeStruct/Seq/Map | 类型调用 serializer；成功/错误及复合状态由关联类型表达 | 派生调用核心协议，格式包实现具体状态 |
| [serde_core/de](https://github.com/serde-rs/serde/blob/master/serde_core/src/de/mod.rs) 的 Deserialize、Deserializer、Visitor、SeqAccess、MapAccess、EnumAccess、VariantAccess | 类型提供 visitor；格式解析后向 visitor 提交数据；集合按元素/键值访问 | 类型驱动读取，不先普遍解码成 any/树 |
| [derive Serialize](https://github.com/serde-rs/serde/blob/master/serde_derive/src/ser.rs) 的 serialize_struct | 生成 serialize_struct、逐字段 serialize_field 和 end 调用 | 派生不绑定 JSON，不通过反射扫最终 Go 方法集 |
| [derive struct Deserialize](https://github.com/serde-rs/serde/blob/master/serde_derive/src/de/struct_.rs) | 生成 visitor、字段槽、重复字段检查和缺失处理 | 缺失/default/未知字段是类型与派生契约 |
| [serde_json::Value](https://github.com/serde-rs/json/blob/master/src/value/mod.rs) | 仅 Null/Bool/Number/String/Array/Object；无 Struct/Enum/Bytes 节点 | Value 属于 serde-json；不是核心语义 IR |
| [value::Serializer](https://github.com/serde-rs/json/blob/master/src/value/ser.rs) | to_value 的后端产出 JSON Value，而文本 serializer 产出字节 | 同一 Serialize 可以走文本或可选 Value 后端 |
| [serde_json 文本 Serializer](https://github.com/serde-rs/json/blob/master/src/ser.rs) | unit variant 为字符串，带载荷外标记为 object；bytes 默认数字数组，None 为 null | 修正前稿“unit 也统一 object”“bytes 默认 Base64”的自创默认 |
| [serde_json Number](https://github.com/serde-rs/json/blob/master/src/number.rs) | 默认整数/有限浮点表示；arbitrary_precision 有独立路径和私有协议标记 | 精度选项是格式层问题，不自创核心 Decimal 节点 |

Serde 的 [Serialize 实现说明](https://serde.rs/impl-serialize.html)、[Deserialize/Visitor 说明](https://serde.rs/impl-deserialize.html)支持上述调用方向。协议支持 enum/struct，与动态值节点是否含它们完全不同。不得再把前稿 A/B 语义树选择当作用户需要重新批准的核心架构问题。

## 2. 修订后的模块依赖

```text
用户类型 + 内建 derive(Serialize, Deserialize)
                        |
      serde 核心：协议、Visitor、Access、基础类型实现
              /                         \
serde-json Serializer/Deserializer    未来 serde-yaml 等格式包
       |              |
     JSON I/O     可选 json.Value / to_value / from_value
```

- `serde`：格式无关协议、基础类型与容器的实现、错误构造约定；无通用公开 `serde.Value`，无公共 `serde.to_value`。
- 内建 derive：从 TypeShape 生成 Serialize/Deserialize 实现与 visitor，使用已选字段 codec，不产生 JSON tag/直接 encoding/json 调用。
- `serde-json`：JSON 解析/写出、Number、Value、格式错误和可选动态值 API；可复用 Go tokenizer，但不让 Go struct 反射默认行为替代派生规则。
- `serde-yaml`：后续独立格式包；是否有自己的 Value/文档 AST 由该格式需要决定，不能要求其复用 JSON Value。

`serde_json::to_value(x)` 明确表示按 JSON 语义投影；不再存在“格式无关 to_value 到底采用哪种 enum 编码”的歧义。JSON Value 编码回 JSON 不恢复源 enum/struct 身份；若要重新按其他格式编码，保留 typed 对象，或经明确目标类型 from_value 后再编码。任何 JSON/YAML 跨格式值转换都有可表达范围，不默认无损。

## 3. 核心协议保留什么

### 3.1 序列化

源对象实现 `Serialize`；对标量选择对应 serializer 操作，对复合值先取得状态、逐项写入、最后 end。最小能力族如下，最终 goxide 拼写与 Go 接口名称待实施冻结：

| 能力族 | 需要保留的区别 |
| --- | --- |
| bool、signed/unsigned、float、string、bytes | 不统一转 float64/string/any；整数宽度与范围按源类型保留 |
| none / some / unit | option 与 unit 是协议操作，不要求核心 Null/Option Value 节点 |
| seq / tuple | 长度可能未知的序列与已知形状元组分别表达 |
| map / struct | map 键可序列化，struct 字段是已知名称；JSON 常相同，不代表二进制格式必须相同 |
| unit/newtype/tuple/struct variant | enum 的名字、变体标识和载荷形状交给格式；不预先变成 JSON object |
| newtype/unit/tuple struct | 可分期实现，不能默默当成完全等同 map 的另一名字 |

核心与格式均可对尚不支持的能力返回明确 Unsupported；不得生成错误结果凑齐全部 Serde 方法。首版支持 bool、8–64 位整数、float32/64、string/bytes、Option、slice/固定数组、map、普通 struct 和外标记 enum 形状；128 位数、语言尚未存在的 tuple/newtype 语法可先保留能力设计，分期实现。

### 3.2 反序列化、Visitor 与 Access

`Deserialize[T]` 创建能构造 T 的 visitor，调用 `Deserializer.deserialize_struct/seq/u64/...` 给出期望形状；格式读取输入，调用匹配的 `visit_*`。visitor 默认对不支持的数据类型报 Type，不能由格式直接在用户 struct 上任意写字段。`deserialize_any` 只适合自描述格式/动态值，typed 派生优先具体方法，不要求未来二进制格式也能猜类型。

- SeqAccess：逐个元素提供子 deserializer；结束与“元素值为 null”是不同状态。
- MapAccess：先读键、再读对应值；已读键必须消费/跳过该值，不能调用 next_key 随意跨过它。
- EnumAccess：解析变体标识，交出 VariantAccess；后者按 unit/newtype/tuple/struct 读取载荷。目标 enum visitor 选择已知变体。
- 忽略未知字段使用递归消费单个值的 IgnoredAny 类 visitor；它仍检查语法/预算。不要先分配整个未知值树。
- 字段槽存 `seen` 与已解码值，seen 不得由“值非零/非空”推导。重复字段、缺失字段、default、skip 由生成 visitor 处理。

DeserializeSeed 的带状态初始化、原地更新与复杂恢复可后置；首版用明确的目标函数/visitor 传递子类型证据即可。Visitor 不是一个 `Value` 节点集合，也不要求所有输入都先转通用动态值。

## 4. Go/goxide 的最小实现偏差与接口草图

Rust 的泛型方法、关联 `Ok/Error/Value` 及 `'de` 不能直接照搬进当前 Go interface；本仓库 U04 的完整 trait 后端也未决定。推荐原型用 **普通 Go 核心接口 + 具体类型 helper/visitor + typed 子调用闭包**，不以实现全部关联类型求解为前提。下述为协议草图，不是已编译 API：

```text
WriteValue = func(Serializer) error
ReadValue  = func(Deserializer) error

Serializer:
  U64(value uint64) error
  String(value string) error
  Struct(name string, fields int) (StructState, error)
  Seq(length_hint) (SeqState, error)
  ...其余已支持的标量/option/map/variant 操作
StructState:
  Field(name string, write WriteValue) error
  End() error

Deserializer:
  U64(visitor Visitor) error
  Struct(name string, fields []string, visitor Visitor) error
  ...其余已支持的请求
Visitor:
  VisitU64(value uint64) error
  VisitMap(access MapAccess) error
  ...默认实现返回 Type 的方法
MapAccess:
  NextKey(read ReadValue) (present bool, err error)
  NextValue(read ReadValue) error
SeqAccess:
  NextElement(read ReadValue) (present bool, err error)
```

生成的具体 visitor 保存类型化目标/字段槽，例如 `OrderVisitor` 保存 `Order`；`decodeOrder(d) (Order,error)` 创建它，调用 d.Struct 后返回其结果。ReadValue 闭包只把具体字段解码结果写入该字段槽，不把所有字段塞进 any。SeqAccess 的 present 表示“有元素”，null 仍由子 visitor 处理。非泛型核心 interface 避开 Go interface 不支持独立泛型方法的限制；闭包/接口分派可能分配，不能称零成本。

| Rust 机制 | Go/goxide 首版候选 | 保留 / 代价 |
| --- | --- | --- |
| Serializer::Ok 由后端决定 | 所有写协议返回 error，结果由后端 Finish 取出；JSON Value builder 拥有结果 | 保留不同后端，放弃类型关联返回值的直接编码 |
| S::Error / D::Error | Go error + 核心分类/路径包装，保留 Unwrap cause | 统一签名易实现；不等于 Rust 静态格式错误类型 |
| Visitor::Value | 具体 visitor 内的 typed 结果，由生成 helper 返回 T | 不用动态 any；需要定义对象状态和成功后取值规则 |
| `Serialize<T>` 泛型调用 | 静态选出的 helper；泛型字段显式传 typed codec/evidence | 不冻结通用 trait 字典 ABI，不自动按 Go 方法集采用 |
| `'de` 与 borrowed/transient/owned 字符串 | 首版仅拥有型结果；保留数据复制到自身存储 | 不实现借用生命周期/零复制解码；不得返回复用 reader 缓冲 |
| by-value 状态/借用限制 | 顺序调用、子值恰好消费一次、结束后失效的运行时协议 | Go GC 不自动证明线性使用；误用返回 State 错误 |

Rust 的反序列化生命周期用于表达输入借用条件，参考[官方生命周期说明](https://serde.rs/lifetimes.html)。goxide D20 不做 MVP 借用分析，因此首版拥有型结果是明确实现范围，不凭同名 Visitor 宣称等价安全保证。原对象编码也不自动快照/加锁；读写权限来自既有规则，共享对象同步仍是调用方职责。

## 5. 最小端到端派生路径

```goxide
use serde;
#[derive(serde::Serialize, serde::Deserialize)]
struct Order {
    #[serde(rename="order_id")]
    id uint64
    paid bool
}

// 候选 API；首版 helper 可先采用 Go (T,error)，不等待 ? 前端。
order := serde_json::from_str[Order](../input)?
text := serde_json::to_string(order)?
view := serde_json::to_value(order)? // 可选 JSON Value，不是必经步骤
```

序列化 lowering 示意（协议顺序，不冻结真实 ABI）：

```text
encodeOrder(serializer, value):
  state = serializer.Struct("Order", 2)
  state.Field("order_id", child => child.U64(value.id))
  state.Field("paid", child => child.Bool(value.paid))
  state.End()
```

`serde_json.to_string → JSON Serializer → encodeOrder → StructState/Field → 子类型 serializer → writer`，无需 JSON Value。`to_value` 换成 JSON Value Serializer，同一个 encodeOrder 不变。

反向：`from_str → JSON Deserializer → decodeOrder → OrderVisitor → VisitMap → NextKey/NextValue → U64/Bool visitor → 检查 seen/default → 构造 Order`。字段名错误定位到 wire 名称，派生错误定位到源属性/字段。`from_value[Order]` 用 JSON Value Deserializer 驱动同一个 visitor；不是再调用 Go json.Unmarshal 反射回填。

JSON from_str 可先用 token reader 直接推进 Access，只有目标要求 Value 时才构树。若首个增量暂用 JSON 私有树来减少解析器工作，必须标为实现过渡：公共协议、派生及完成标准不能要求所有输入都建树，后续流式路径使用同一 visitor。自定义 Serialize/Deserialize 与 derive 共享核心，不提供仅能识别固定 Order 样例的生成器。

## 6. serde-json 的 Value、数字与线格式

### 6.1 格式层 Value

仿照 serde_json：`Null | Bool | Number | String | Array[Value] | Object[string,Value]`。没有 Missing、Struct、Enum、Bytes 或任意键 Map 节点。Object lookup 的 `(value,found)` 区分不存在与存在 Null；不得为方便 API 把两者只暴露成同一个 null。JSON Value 不保留名义类型、整数源位宽、指针身份或格式注释；按 enum JSON 表示写成 String/Object 后，这些就是普通动态数据。

Number 不统一成 float64。goxide 首版建议 JSON Number 保留经验证的有限数值文本（有长度预算）作为格式层精度策略；普通 int64/uint64 精确解析，目标浮点才舍入。serde_json 源码的 arbitrary_precision 是可选机制，不可声称其默认 Number 都任意精度。该 goxide 选择是工程建议，不是 serde-rs 核心必需节点。

精确 Number 的协议接入有真实额外工作：core 标准标量不足以表达任意精度十进制。参考 serde_json 的私有 Number 约定，可在 JSON 格式包内部以明确保留标记的 newtype/struct 协议传递验证后的数字文本；writer/Value builder/reader 同时识别，并测试不误吞普通业务 struct。标记不是安全边界，不复制 Rust 的私有字符串作为跨语言 ABI。其他格式若不支持该约定，应明确 Unsupported 或使用显式字符串/目标数值转换，不冒充格式通用 Decimal。初版未完成此桥接时，只能宣称 JSON parser/Value 保真，不能宣称任意 Number 字段已支持通用派生。

数字验收：2^53+1、uint64 最大值不先转 float；越界/负→unsigned 报 Range；`1.0`/`1e0` 到整数默认不截断；负零浮点保留符号；巨大指数只保存有界文本不展开零串。float 目标允许位宽内舍入，非零下溢到零与溢出应明确报告；精确金额使用格式 Number 或业务定点类型及明确 codec。JSON Number 与源浮点数值往返不等于文本字节保真。

### 6.2 对齐与显式偏差

| 行为 | serde-rs/json 核对结果 / goxide 处置 |
| --- | --- |
| struct/map | JSON object；struct 字段已知而 map 动态，核心仍区分 |
| unit enum | 对齐字符串，例如 `"Idle"`，撤回前稿 `{"Idle":null}` 默认 |
| 带载荷外标记 enum | 对齐 `{"Named":"Ada"}`、`{"Pair":[1,2]}`、`{"User":{"id":7}}`；首版仅外标记 |
| Option | 对齐 None→null、Some(x)→x；撤回默认 Some/None 标签封装；嵌套 Option 可能丢区别，见 §7 |
| bytes | 对齐默认数字数组，例如 `[1,2]`；Base64 是显式字段 adapter，不是核心默认；不照搬 Go []byte 默认行为 |
| 非有限 float | serde_json 文本 serializer 对 NaN/Inf 输出 null；goxide 建议首版明确报 UnsupportedNumber，属于待冻结的严格策略偏差，不能称完全兼容 |
| map 键 | serde_json 有独立 key serializer，支持部分可字符串化标量；goxide 首版可限定字符串键，其他明确 UnsupportedKey，而非声称 serde-rs 只允许源 string |
| 重复字段/对象键 | derive 对重复已知字段诊断；动态 JSON Object 通常无法保存重复。goxide 建议所有对象重复键（含未知/转义同名）在解析时拒绝，须单列为严格策略，不声称默认行为完全一致 |
| nil slice/map、nil pointer | Go 独有边界：普通容器建议空数组/object；nullable/Option 显式处理 nil；非空引用不能由 null 自动构造 |

内部/相邻标签及 untagged 在 Serde 中还涉及 derive 生成不同映射，不是永远由格式随意重选。未来加入属性时需同时实现派生与格式访问路径；不能期望 JSON Value 恢复已擦掉的 enum 身份。[Serde enum 表示](https://serde.rs/enum-representations.html)

### 6.3 Go JSON 实现细节与原生接口

本地 Go 1.27.1 的 jsontext 源码仍带实验构建条件，GOEXPERIMENT 为空，本轮未启用或下载依赖。可先用 encoding/json.Decoder 的 Token/UseNumber 做词法推进，配合独立重复键/尾部/预算和 Unicode 校验；不先 Decode 到 map[string]any/float64。v1 的非法 UTF-8/孤立 surrogate 替换、大小写匹配、[]byte Base64 和重复键策略不能悄悄成为 goxide 默认。[Go encoding/json](https://pkg.go.dev/encoding/json)

Serialize/Deserialize derive 不自动生成或调用 Go MarshalJSON/UnmarshalJSON；用户显式选择 JSON 专用 native adapter 才进入这条路径。native 输出验证为单个合法 JSON 值，错误保留 cause；输入交给 native 后，其字段/default/副作用由该实现负责，不宣称核心统一控制。JSON 专用 adapter 不自动适用于 YAML，也不因缺少它阻断普通 Go 调用。不得通过自动添加方法改变原类型的可选协议/方法集。

## 7. 字段属性、缺失与 null 的真实语义

当前有限实现支持 Serialize/Deserialize 独立派生及字段 rename、default、skip；未知字段默认消费并忽略，容器 deny_unknown_fields 可改为错误。skip_serializing_if、skip_deserializing/skip_serializing、容器级 default 和自定义默认工厂仍后置，不把不识别的属性静默忽略。设计参考[官方字段属性](https://serde.rs/field-attrs.html)，不承诺完整属性兼容。

- 非 Option 的 required 字段缺失报 MissingField；default 仅字段缺失时调用一次已登记的 Default。显式值始终按目标 codec 解码：普通 T 的 null 类型错误不触发 default，允许 null 的类型仍按原语义接收；false/0/空字符串不是 missing，未知 enum 变体和错误 payload 不回退为默认值。
- 内建具体 `Option[T]` 字段按 Serde 风格缺失为 None，显式 null 同样是 None，存在的非 null 值按 T 的 codec 解码为 Some；None 编码为 null，Some 编码其载荷。`Option[Option[T]]` 的普通派生并不能自动保存 PATCH 的三态；Some(None) 与 None 默认 JSON 都可能为 null，不宣称往返可逆。
- PATCH 要求缺失=不改、null=清空、值=设置时，应显式选择字段级 adapter/包装类型，跟踪字段存在性，例如库候选 `PatchField[T]`。它不是核心 Value 节点；默认值给 Missing，字段存在时的 custom deserializer 负责 Null/Value，序列化用 skip_if_missing。首版可先手写 DTO visitor，不为了它增加语言新类型。
- skip 编码时省略字段；解码时忽略输入内容，完整消费并验证其格式与预算，不调用该字段 codec；不论输入中是否存在该字段，成功构造时调用一次已登记的 Default。带 Deserialize 的 default/skip 字段缺少 Default 时在派生阶段诊断，Serialize-only 不要求解码 Default。
- rename 后冲突在派生阶段诊断；重复已知字段在 visitor 中检查，不以零值判断出现与否。私有字段会参与本包生成代码，敏感字段需显式 skip/自定义。

### 7.1 字段 Default 接入与兼容变化

这是从旧原型 Go 零值策略到协议调用的显式兼容变化：缺失的 `#[serde(default)]` 和解码 `#[serde(skip)]` 字段复用语言 Default 登记，不以同名普通函数认领协议，不在缺协议时退回 Go 零值。标量、同包派生 Default 和同包手动 `impl Default for T { func default() T { ... } }` 沿用当前规则；enum 仅选中默认变体的 payload 要求 Default。跨文件解析保留包与类型身份，跨包协议消费仍不支持。

每个 struct visitor 完成字段读取和全部 required 检查后，按字段声明顺序为本层缺失 default 字段和全部 skip 字段调用 Default。显式字段不调用 Default。读取或 required 检查失败时不开始本层默认构造；嵌套字段自己的解码和默认构造可能已经执行，其外部副作用不回滚。此顺序不承诺整个 JSON 文档后续失败时回滚。

旧代码中缺少 Default 的 skipped 字段现在会在带 Deserialize 派生时拒绝，包括原生指针、slice/map/数组等当前无 Default 的类型。原生 `*T` 的 codec 仍允许显式 null；这不授予 `*T` Default，故它不能标记解码 default/skip。需要业务默认构造时可使用具有已登记手动 Default 的本地 struct。该阶段未增加 Option serde；后续具体 Option 接入见下节。没有新增容器级 default、函数路径属性或完整 trait 后端。

### 7.2 内建具体 Option 派生

此增量复用核心 None/Some 与 Deserializer.Option/Visitor 协议，不引入通用泛型 trait。struct 字段可使用已支持具体载荷的 `Option[T]`，包括同包具有对应方向 codec 的 struct/enum；外层数组、slice、string-key map 可包含 Option。Serialize-only 只检查 Serialize 载荷，Deserialize-only 只检查 Deserialize 载荷。

- 未 skip 的 `Option[T]` 在编译期要求 T 有对应方向 codec，即使业务值始终是 None；不能用可空性掩盖不支持的载荷、外部 codec 或 Result。
- `Option[T]` 自身 Default 是 None，不要求或调用 T 的 Default。字段标记 `serde(default)` 也不改为 Some(default T)。手动 Default 的本地 struct 作为字段时仍使用已登记的业务构造，作为 Option 载荷时不能取代 Option 的 None 默认。
- 缺失和显式 null 均为 None；非 null 的错误值必须报告正常字段/索引/变体路径，不回退 None 或业务默认值。skipped Option 不调用载荷 codec，输入只受格式、重复键及预算检查，成功后为 None；因此其载荷可以没有 serde 或 Default，但仍须满足既有内建 Option 类型合法性限制。
- 普通 enum 的直接 Option payload、含 Option 的 struct payload 等仍受现有 payload 检查限制，不由 `Option[Enum]` 的支持反推可用。`Option[[]Option[int]]` 等穿过指针/容器的 enum 值投影继续拒绝；跨包 codec、Result serde 和任意泛型实例均未开放。
- 合法的 `Option[Option[int]]` 中 Some(None) 与 None 编码均可为 null，解码为外层 None；`Option[*int]` 的 Some(nil) 也会丢失与 None 的区别。JSON 往返只承诺当前表示可区分的值，不提供 PATCH 三态。

### 7.3 普通 Option 协议与 Default 相互独立

用户校正明确：标准库 Option 的 Default 返回 None，与 serde 将普通 Option 的缺失/null 映射为 None 是两条独立机制。普通字段不需标记 serde(default)，也不应经过 Default 登记、检查或表达式生成。T 仍需对应方向的 serde 能力，但不需 T.Default。显式 serde(default)/skip 才走已批准的 Default 路径；Option 的该显式 Default 恰好也是 None，不能据此混同来源。

固定版本依据：

| 路径 | 源码与行为 |
| --- | --- |
| 标准库 Default | [Rust 1.90.0 Option Default](https://github.com/rust-lang/rust/blob/1.90.0/library/core/src/option.rs#L2176-L2189)：独立返回 None，不要求 T.Default |
| 普通缺失字段 | [Serde 1.0.228 missing_field](https://github.com/serde-rs/serde/blob/v1.0.228/serde/src/private/de.rs#L22-L56)：MissingFieldDeserializer 的 deserialize_option 调用 visit_none，不调用 Default |
| Option 类型协议 | [Serde 1.0.228 OptionVisitor](https://github.com/serde-rs/serde/blob/v1.0.228/serde_core/src/de/impls.rs#L870-L927)：visit_none/visit_unit 返回 None，visit_some 解码 T 后包装 Some |
| JSON null | [serde_json 1.0.145 deserialize_option](https://github.com/serde-rs/json/blob/v1.0.145/src/de.rs#L1671-L1683)：消费 null 后调用 visit_none，否则 visit_some |
| 自定义字段缺失 | [Serde 1.0.228 expr_is_missing](https://github.com/serde-rs/serde/blob/v1.0.228/serde_derive/src/de.rs#L763-L803)：有 default 先处理；无自定义解码时调用 missing_field；有 deserialize_with 且无 default 时直接报缺失。这不是普通 Option 的规则 |

goxide 当前有限实现：普通 Option 缺失由生成的 struct visitor 直接设置 None，绕过 Default 检查及生成器；JSON null 沿现有 Deserializer.Option → VisitNone 路径。此职责分离不要求先移植通用 MissingFieldDeserializer 或完整 trait，也不改返回行为。标准库层的独立 Default 在当前原型由内建 Default 选择逻辑表示，不声称已实现通用 std trait 后端。

adapter 注解仍未批准、未实现；自定义 deserialize_with 的缺失行为不能从普通 Option 结论自动批准。本文确认普通 Option 规则，不把 Proposed 两项选择视为整体批准。

业务矩阵：配置 retries 缺失走 default，0 原样，null 报错；PATCH display_name 的 `{}`、`null`、`"Ada"` 仅通过明确三态 adapter 区分；事件 Idle 为字符串、Named 为外标记；JSON Number 保留大订单 ID。错误输入不吞成业务默认，不把 serde 错误自动选到某个 thiserror variant（仍按 D22 显式 From）。

## 8. 错误、预算与所有权边界

核心错误构造至少支持 Type、MissingField、DuplicateField、UnknownField/Variant、Range、Unsupported 和 Custom；格式附 Syntax/IO、byte offset，路径 wrapper 增加 Field/Index/MapKey/Variant 段并保留 Unwrap。结构化路径不能用点连接字符串代替，否则字段名里的点/斜杠有歧义。完整路径包装是 goxide 首版建议，不能声称 Rust serde 核心本身保证所有格式输出统一路径。默认不打印完整敏感载荷。

当前 JSON 后端保留实际 Token/数值转换错误为 cause；可用 `errors.As` 读取原始 `json.SyntaxError.Offset` / `strconv.NumError`，或 `errors.Is` 检查底层 sentinel。Kind、Path 与既有消息保持；位置来自实际 Token 调用，不重算为 json.Unmarshal 的 offset。仅由本后端检测的错误不伪造 Go cause，也不新增统一 offset API。

读入字节、嵌套深度、数字/字符串长度、容器项数设有限可配置预算；IgnoredAny、native adapter 和 Value builder 同样计数。Deserialize 构造新结果，成功后返回；自定义 default/codec 的外部副作用不能回滚。writer 出错前可已写部分输出，不宣称原子提交；需要原子缓冲由调用者选择。

Value 拥有其保存的字符串/字节，不引用解码器可复用缓冲。Serde bytes 协议的 borrowed/owned 全套优化后置，不把 Go GC 等同于缓冲不会变动。编码共享对象需要真实同步，递归对象循环须明确 Cycle/Limit；不通过未来借用分析替代这些运行时契约。

## 9. YAML 与其他格式后续边界

未来 serde-yaml 实现同一 Serializer/Deserializer/Visitor/Access，可能提供自身 Value 和文档 AST。非字符串键、tag、非有限数、anchors/aliases、schema 解析与 merge 策略由 YAML 格式包定义；二进制格式可以利用 struct/tuple schema，不能强制经过 JSON object/string。

JSON Value 不能保存 YAML 任意键、tag/锚点身份或注释；跨格式传递 typed 对象可保留更多业务语义，仍不等于 YAML presentation 无损。别名展开需预算且循环明确处理。不得因参照 Serde 就承诺任意 YAML 无损 roundtrip；本轮不实现 YAML。[YAML 1.2.2](https://yaml.org/spec/1.2.2/)

## 10. 最小交付与待实现验收

推荐顺序：核心协议/typed helper/visitor 状态 → JSON token reader/writer → 普通 struct 派生 → JSON Value 可选后端 → enum 外标记与明确字段 adapter。若先做 Value reader，可作为短期内部实现，但不能把“必须先建完整树”设为 API 或 MVP 固定架构。关键完成指标是真实输入→derive→核心协议→JSON→反向 visitor→typed 结果，而非仅手写固定 Go 示例。

| ID | 用例 | 验收 |
| --- | --- | --- |
| S01 | Order struct 派生、rename、字段顺序变化 | 文本/Value 两后端复用 Serialize，读取复用同一 typed visitor |
| S02 | 2^53+1、uint64 最大值、负→unsigned、超范围 | 保真数值路径；错误有字段路径，不经 float64 回转 |
| S03 | 任意精度 JSON Number 与普通同名业务 struct | 格式扩展正确区分，不注入原始任意 JSON；未支持字段桥接明确报告 |
| S04 | missing/null/default/0/false；Option 嵌套与 PATCH | 默认折叠明确；三态仅显式 adapter；不虚称无损 |
| S05 | bytes、Base64 显式 adapter、字符串/数组非法输入 | 默认数字数组；字段 adapter 可替换，不由 Go []byte 默认控制 |
| S06 | 全部外标记形状、未知 tag、重复 tag | unit 字符串与 payload object 对称处理，错误明确 |
| S07 | rename 冲突、缺 Default、skip、未知字段深层非法内容 | 编译期来源/运行时路径正确；skip 仍消费预算 |
| S08 | duplicate/转义同名键、非法 UTF、尾部第二值 | 严格策略与 Rust/Go 原生偏差单列验证 |
| S09 | MapAccess 跳过 value、End 后调用、回调多次消费 | 确定 State 错误，不隐藏重放或跳读 |
| S10 | 深度/长度超限、循环对象、IO 部分写、自定义错误 | 有界失败与 cause/路径；不声称回滚 |
| S11 | 泛型字段 codec、缺证据、只读字段、原生类型接口 | 不用反射 any 补洞；未支持语法明确拒绝；不自动拆 Arc/锁 |
| S12 | 改输入缓冲、并发共享源、巨大数组 | owned 结果不受复用影响；同步前提明确；实测分配/峰值内存而非零成本宣传 |

首版后置：完整泛型 trait ABI、借用式零复制/生命周期、公开 Seed 高级协议、untagged/flatten、YAML、所有格式的特殊节点与通用语义 IR。原因分别是当前前端能力、额外状态机/歧义、范围与真实需求；不是恢复风险分析准入。

初稿阶段只做官方文档/源码核对和规格修订，当时 S01–S12 均为待实现。当前有限实现和真实执行证据见末节及验收矩阵，不能将初稿检查当作运行证据。用户已决定仿照 serde-rs，不再询问是否采用该架构；后续只需逐项冻结 Go ABI/严格 JSON 策略及实际支持子集。

## 11. 被替代候选与一致性自审

前稿的公开 core Value、Record/Variant 节点、CoreValue-v1 固定映射、通用 serde.to_value、首版必建全树不再是推荐架构；统一 unit object、Option 标签封装、bytes 默认 Base64 同样撤回，除非未来作为用户显式 adapter 另提。保留的是数字精度、字段存在性、错误路径、预算和 Go 边界需求，并将其放回正确层次。

自审反例：JSON 动态单键 object 不恢复 enum；Option(null) 不保存 PATCH 三态；MapAccess 不消费 value 不能前进；精确 Number 不等于核心新增 Decimal；协议有 struct/enum 不意味着 Value 有这些节点；Go helper 有错误转换不等于 ? 已实现。当前选择不改变 D05/D06/D11，不移动基线标签；初稿未开始宏实现，后续有限实现另行验收。

## 实现分支进展（不冻结 ABI）

具体类型的核心协议、JSON 后端与 derive 已形成实际 `.gox → Go → 编译运行` 闭环。准确支持范围、未实现项和复现命令见[原型说明](../../../IMPLEMENTATION.md)。当前已接通有限封闭 enum 派生、字段 default/skip 的已登记 Default 调用及内建具体 Option serde；此结果不代表本 RFC 的泛型、全部 enum 表示、YAML、Value 或完整 trait ABI 已完成；机制夹具与生成代码测试分开计量。


## Proposed 研究增补：显式字段适配器

[字段适配器最小候选](serde-field-adapters-proposed.md)以 `4c25e1d` 为基线比较具体函数路径与 with 包约定、time.Time typed helper 与 JSON RawMessage 桥；核对 Serde 官方 v1.0.228 源码并记录仓库外机制探针。状态为 **Proposed，未批准、未实施**；尤其 Option 自定义解码缺失策略仍需选择，不改变本文已实现字段的规则，不扩 Value/PATCH。

## 跨包已登记 Serialize/Deserialize 字段（2026-10-03）

目录/模块模式按实际 import 路径恢复生产者显式 derive 的具体 struct/enum serde 协议，消费 struct 的 qualified 字段及既有 array/slice/map/pointer/Option codec 递归调用生产者 helper；公开透明 relay alias 可转发，多文件同名 import 不混淆。Serialize 与 Deserialize 独立登记/要求，不通过 helper 名、反射、Go 方法集或 encoding/json 自动实现兜底。

复用 metadata v6 的 FieldProtocols 记录，不新增 schema 字段或版本；Kind 扩展为 Serialize/Deserialize。核验 canonical named 类型、derive 来源、alias 定义包登记，以及精确非泛型 `func(serde.Serializer, *T) error` / `func(serde.Deserializer) (T, error)`，其中 core 类型必须与实际 serde 包身份一致，error 必须是内建 error。内容、依赖、marker 校验保持。旧 v6 产物若缺少对应协议记录必须重新生成；不能把旧 helper 的存在当作登记。

真实 CLI 三模块验收覆盖 producer struct/enum、透明 relay、单向协议、跨文件导入、容器和 Option、原生 Go helper 消费，三个生成/消费模块分别 race/vet。验证嵌套 field/variant path、Range/Syntax 的 Go cause、未知变体、生产者 deny_unknown_fields 与消费端忽略额外字段、忽略值深度预算、非法 enum nil/typed-nil/指针 case、nil 容器编码为空且解码为非 nil 空、普通 Option 缺失/null 为 None。没有把它们改为 Default 或其他新语义。重复生成一致；伪 helper、新 defined 类型、缺方向、篡改及陈旧产物拒绝，产物错误定位消费 import。

边界延续：消费者本地 `type Local = m.T` 字段转发尚未接通；手写 serde 登记、单文件外部协议恢复、不可直接导入的 internal-origin、普通 enum 外部 payload 类型范围均未借本片扩大。字段 adapters 的入口/缺失策略仍 Proposed，完整 trait/native bridge、Value/PATCH/YAML、公开函数 enum 参数入口验证不在本片实施。完整 T0 MVP 尚未据此宣告完成。

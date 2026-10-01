# RFC 0006：按 serde-rs 分层的 serde 核心与格式适配

状态：用户已确定“仿照 serde-rs 的实现”，覆盖此前助手提出的统一 core Value、CoreValue-v1 和首版必须建完整树等候选。已确定 serde 优先、首版 JSON、未来 YAML 独立适配；下文 Go/hgo ABI、支持子集与本地策略仍是实施候选，不声称完整 Rust 兼容。没有 serde 或宏前端实现，冻结标签 `v0.0.0-mvp-doc` 不变。

## 1. 官方设计与源码核对

本轮实际读取 serde-rs 官方文档及 serde/json 主分支源码。链接为移动分支，实施前应锁定参考版本；下表标明所核对的符号，不靠类名推断行为。

| 一手依据 | 核对事实 | hgo 采用方向 |
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

源对象实现 `Serialize`；对标量选择对应 serializer 操作，对复合值先取得状态、逐项写入、最后 end。最小能力族如下，最终 hgo 拼写与 Go 接口名称待实施冻结：

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

## 4. Go/hgo 的最小实现偏差与接口草图

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

| Rust 机制 | Go/hgo 首版候选 | 保留 / 代价 |
| --- | --- | --- |
| Serializer::Ok 由后端决定 | 所有写协议返回 error，结果由后端 Finish 取出；JSON Value builder 拥有结果 | 保留不同后端，放弃类型关联返回值的直接编码 |
| S::Error / D::Error | Go error + 核心分类/路径包装，保留 Unwrap cause | 统一签名易实现；不等于 Rust 静态格式错误类型 |
| Visitor::Value | 具体 visitor 内的 typed 结果，由生成 helper 返回 T | 不用动态 any；需要定义对象状态和成功后取值规则 |
| `Serialize<T>` 泛型调用 | 静态选出的 helper；泛型字段显式传 typed codec/evidence | 不冻结通用 trait 字典 ABI，不自动按 Go 方法集采用 |
| `'de` 与 borrowed/transient/owned 字符串 | 首版仅拥有型结果；保留数据复制到自身存储 | 不实现借用生命周期/零复制解码；不得返回复用 reader 缓冲 |
| by-value 状态/借用限制 | 顺序调用、子值恰好消费一次、结束后失效的运行时协议 | Go GC 不自动证明线性使用；误用返回 State 错误 |

Rust 的反序列化生命周期用于表达输入借用条件，参考[官方生命周期说明](https://serde.rs/lifetimes.html)。hgo D20 不做 MVP 借用分析，因此首版拥有型结果是明确实现范围，不凭同名 Visitor 宣称等价安全保证。原对象编码也不自动快照/加锁；读写权限来自既有规则，共享对象同步仍是调用方职责。

## 5. 最小端到端派生路径

```hgo
#[derive(Serialize, Deserialize)]
struct Order {
    #[serde(rename="order_id")]
    id uint64
    paid bool
}

// 候选 API；首版 helper 可先采用 Go (T,error)，不等待 ? 前端。
order := serde_json::from_str[Order](input)?
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

Number 不统一成 float64。hgo 首版建议 JSON Number 保留经验证的有限数值文本（有长度预算）作为格式层精度策略；普通 int64/uint64 精确解析，目标浮点才舍入。serde_json 源码的 arbitrary_precision 是可选机制，不可声称其默认 Number 都任意精度。该 hgo 选择是工程建议，不是 serde-rs 核心必需节点。

精确 Number 的协议接入有真实额外工作：core 标准标量不足以表达任意精度十进制。参考 serde_json 的私有 Number 约定，可在 JSON 格式包内部以明确保留标记的 newtype/struct 协议传递验证后的数字文本；writer/Value builder/reader 同时识别，并测试不误吞普通业务 struct。标记不是安全边界，不复制 Rust 的私有字符串作为跨语言 ABI。其他格式若不支持该约定，应明确 Unsupported 或使用显式字符串/目标数值转换，不冒充格式通用 Decimal。初版未完成此桥接时，只能宣称 JSON parser/Value 保真，不能宣称任意 Number 字段已支持通用派生。

数字验收：2^53+1、uint64 最大值不先转 float；越界/负→unsigned 报 Range；`1.0`/`1e0` 到整数默认不截断；负零浮点保留符号；巨大指数只保存有界文本不展开零串。float 目标允许位宽内舍入，非零下溢到零与溢出应明确报告；精确金额使用格式 Number 或业务定点类型及明确 codec。JSON Number 与源浮点数值往返不等于文本字节保真。

### 6.2 对齐与显式偏差

| 行为 | serde-rs/json 核对结果 / hgo 处置 |
| --- | --- |
| struct/map | JSON object；struct 字段已知而 map 动态，核心仍区分 |
| unit enum | 对齐字符串，例如 `"Idle"`，撤回前稿 `{"Idle":null}` 默认 |
| 带载荷外标记 enum | 对齐 `{"Named":"Ada"}`、`{"Pair":[1,2]}`、`{"User":{"id":7}}`；首版仅外标记 |
| Option | 对齐 None→null、Some(x)→x；撤回默认 Some/None 标签封装；嵌套 Option 可能丢区别，见 §7 |
| bytes | 对齐默认数字数组，例如 `[1,2]`；Base64 是显式字段 adapter，不是核心默认；不照搬 Go []byte 默认行为 |
| 非有限 float | serde_json 文本 serializer 对 NaN/Inf 输出 null；hgo 建议首版明确报 UnsupportedNumber，属于待冻结的严格策略偏差，不能称完全兼容 |
| map 键 | serde_json 有独立 key serializer，支持部分可字符串化标量；hgo 首版可限定字符串键，其他明确 UnsupportedKey，而非声称 serde-rs 只允许源 string |
| 重复字段/对象键 | derive 对重复已知字段诊断；动态 JSON Object 通常无法保存重复。hgo 建议所有对象重复键（含未知/转义同名）在解析时拒绝，须单列为严格策略，不声称默认行为完全一致 |
| nil slice/map、nil pointer | Go 独有边界：普通容器建议空数组/object；nullable/Option 显式处理 nil；非空引用不能由 null 自动构造 |

内部/相邻标签及 untagged 在 Serde 中还涉及 derive 生成不同映射，不是永远由格式随意重选。未来加入属性时需同时实现派生与格式访问路径；不能期望 JSON Value 恢复已擦掉的 enum 身份。[Serde enum 表示](https://serde.rs/enum-representations.html)

### 6.3 Go JSON 实现细节与原生接口

本地 Go 1.27.1 的 jsontext 源码仍带实验构建条件，GOEXPERIMENT 为空，本轮未启用或下载依赖。可先用 encoding/json.Decoder 的 Token/UseNumber 做词法推进，配合独立重复键/尾部/预算和 Unicode 校验；不先 Decode 到 map[string]any/float64。v1 的非法 UTF-8/孤立 surrogate 替换、大小写匹配、[]byte Base64 和重复键策略不能悄悄成为 hgo 默认。[Go encoding/json](https://pkg.go.dev/encoding/json)

Serialize/Deserialize derive 不自动生成或调用 Go MarshalJSON/UnmarshalJSON；用户显式选择 JSON 专用 native adapter 才进入这条路径。native 输出验证为单个合法 JSON 值，错误保留 cause；输入交给 native 后，其字段/default/副作用由该实现负责，不宣称核心统一控制。JSON 专用 adapter 不自动适用于 YAML，也不因缺少它阻断普通 Go 调用。不得通过自动添加方法改变原类型的可选协议/方法集。

## 7. 字段属性、缺失与 null 的真实语义

首版建议支持 Serialize/Deserialize 独立派生及 rename、default、skip、skip_serializing_if 的有限形式；未知字段默认消费并忽略，deny_unknown_fields 可改为错误。skip_deserializing/skip_serializing 可以后续分批，不把不识别的属性静默忽略。参照[官方字段属性](https://serde.rs/field-attrs.html)，精确拼写/支持子集仍待实现登记。

- required 字段缺失报 MissingField；default 仅字段缺失时调用一次已选择的工厂/Default。普通 T 的 null 类型错误不触发 default；false/0/空字符串不是 missing。
- Option 字段按 Serde 风格可以缺失为 None，显式 null 同样是 None。`Option[Option[T]]` 的普通派生并不能自动保存 PATCH 的三态；Some(None) 与 None 默认 JSON 都可能为 null，不宣称往返可逆。
- PATCH 要求缺失=不改、null=清空、值=设置时，应显式选择字段级 adapter/包装类型，跟踪字段存在性，例如库候选 `PatchField[T]`。它不是核心 Value 节点；默认值给 Missing，字段存在时的 custom deserializer 负责 Null/Value，序列化用 skip_if_missing。首版可先手写 DTO visitor，不为了它增加语言新类型。
- skip 双向省略，并为解码提供默认构造；没有合法构造或缺字段协议报派生错误。未知/skip 字段仍消费完整值且计入预算。
- rename 后冲突在派生阶段诊断；重复已知字段在 visitor 中检查，不以零值判断出现与否。私有字段会参与本包生成代码，敏感字段需显式 skip/自定义。

业务矩阵：配置 retries 缺失走 default，0 原样，null 报错；PATCH display_name 的 `{}`、`null`、`"Ada"` 仅通过明确三态 adapter 区分；事件 Idle 为字符串、Named 为外标记；JSON Number 保留大订单 ID。错误输入不吞成业务默认，不把 serde 错误自动选到某个 thiserror variant（仍按 D22 显式 From）。

## 8. 错误、预算与所有权边界

核心错误构造至少支持 Type、MissingField、DuplicateField、UnknownField/Variant、Range、Unsupported 和 Custom；格式附 Syntax/IO、byte offset，路径 wrapper 增加 Field/Index/MapKey/Variant 段并保留 Unwrap。结构化路径不能用点连接字符串代替，否则字段名里的点/斜杠有歧义。完整路径包装是 hgo 首版建议，不能声称 Rust serde 核心本身保证所有格式输出统一路径。默认不打印完整敏感载荷。

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

本轮只做官方文档/源码核对和规格修订，S01–S12 均为待实现。现有检查不验证 serde/derive/Visitor 已运行。用户已决定仿照 serde-rs，不再询问是否采用该架构；后续只需逐项冻结 Go ABI/严格 JSON 策略及实际支持子集。

## 11. 被替代候选与一致性自审

前稿的公开 core Value、Record/Variant 节点、CoreValue-v1 固定映射、通用 serde.to_value、首版必建全树不再是推荐架构；统一 unit object、Option 标签封装、bytes 默认 Base64 同样撤回，除非未来作为用户显式 adapter 另提。保留的是数字精度、字段存在性、错误路径、预算和 Go 边界需求，并将其放回正确层次。

自审反例：JSON 动态单键 object 不恢复 enum；Option(null) 不保存 PATCH 三态；MapAccess 不消费 value 不能前进；精确 Number 不等于核心新增 Decimal；协议有 struct/enum 不意味着 Value 有这些节点；Go helper 有错误转换不等于 ? 已实现。当前选择不改变 D05/D06/D11，不移动基线标签，不开始宏实现。

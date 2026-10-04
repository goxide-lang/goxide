# RFC 0006 研究增补：显式字段适配器的最小候选

状态：**Proposed，未批准、未实施**。研究基线 `feat/manual-default@4c25e1d65675e0b5f5e3a87c4e67f19e99a4c9d2`，核对日期 2026-10-02。本文不修改前端/运行时、不授予字段注解可用状态；推荐是供用户选择的方案。母 RFC 的已确定分层不重开，格式 Value、PATCH adapter、泛型 trait、enum payload 放宽不进入此轮范围。

## 1. 场景与当前事实

两个实际需求应分开：订单的 `Created time.Time` 想按业务指定格式编码为字符串；webhook 的 `Payload json.RawMessage` 想保留一个 JSON 子值，包含大整数或暂不认识的结构。前者只需核心 String 协议，后者要求 JSON 特有原始值能力，不能把原始 JSON 当成 String 或 Bytes。

| 基线能力 | 已执行/已实现 | 尚未实现 |
| --- | --- | --- |
| 具体 typed helper | 普通 `.hgo` 函数可接收 `serde.Serializer/Deserializer`，调用 `time.Format/Parse`，作为 `serdejson.Marshal/Unmarshal` 参数真实运行 | derive 字段的显式 helper 选择 |
| 错误证据 | `errors.As/Is` 保留真实 Go cause，Field/Index/MapKey/Variant 路径已有实现 | 全格式统一位置 API；不可伪称 Token 返回的 SyntaxError offset 等于 Unmarshal offset |
| 字段属性 | rename/default/skip/deny_unknown_fields；Option 缺失/null 为 None | with/serialize_with/deserialize_with 均拒绝 |
| Go JSON | 普通 Go 显式调用 encoding/json、原生方法可用 | 自动采用方法集；核心 raw JSON 子值读写能力 |
| 类型范围 | 具体字段、内建 Option 与既有有限 enum，真实 Go 包函数 | 用户泛型函数、通用 trait 字典、关联函数/Self 全套前端 |

实现证据：[字段属性解析与 codec](../../../internal/frontend/compile.go)、[现有核心协议](https://github.com/goxide-lang/serde/blob/a1b791e96d660c6be620e110c566a9a171631b7e/protocol.go)、[格式后端](../../serde-json.md)、[当前验收矩阵](../../t0-acceptance.md)。以下新注解均为候选伪源码，不能当作现有成功示例。

## 2. 一手资料与版本

已读取官方文档及所列源码，事实与本文建议分列。Serde 源码锁定 **v1.0.228**；文档站和 go.dev 是移动页面。Go 机制探针使用本地 **Go 1.27.1 linux/amd64**、CGO=1，无新依赖；本地 stdlib 与网络页面不是同一版本的保证。

| 来源与符号 | 核对事实 | 对本候选的影响 |
| --- | --- | --- |
| [Serde 字段属性](https://serde.rs/field-attrs.html)；[日期格式示例](https://serde.rs/custom-date-format.html) | serialize_with/deserialize_with 选择自定义函数；with 指向一对函数所在模块，字段不必拥有被替代方向的派生 | 保留用户熟悉的属性名，采用具体 Go 函数签名，不复制 Rust 泛型约束 |
| [v1.0.228 attr.rs](https://github.com/serde-rs/serde/blob/v1.0.228/serde_derive/src/internals/attr.rs)，Field::from_ast、WITH 分支 | with 展开为 serialize/deserialize 路径；重复设置会诊断 | 不采用“最后一个属性覆盖前一个”的规则 |
| [v1.0.228 de.rs](https://github.com/serde-rs/serde/blob/v1.0.228/serde_derive/src/de.rs)，expr_is_missing | 先处理 default；有 deserialize_with 且没有 default 时直接报 missing_field | 自定义 Option 缺失不是普通 Option 的隐式 None，必须明确 hgo 是否保留自身规则 |
| [v1.0.228 de/struct_.rs](https://github.com/serde-rs/serde/blob/v1.0.228/serde_derive/src/de/struct_.rs)，deserialize_map | 自定义 helper 读整个字段值；字段槽仍追踪出现与重复 | helper 不仅是 Some 的 payload 转换，外层字段管理仍由 derive 负责 |
| [v1.0.228 ser.rs](https://github.com/serde-rs/serde/blob/v1.0.228/serde_derive/src/ser.rs)，wrap_serialize_field_with | 用生成 wrapper 将已选字段函数接入 Serialize 调用 | hgo 可直接生成 typed 子闭包，不要求新增全局 trait 实现 |
| [Go JSON 文档](https://pkg.go.dev/encoding/json)；[encode.go](https://go.dev/src/encoding/json/encode.go)，marshalerEncoder、MarshalerError | 原生编码选择方法并检查/压紧返回 JSON；MarshalerError 可 Unwrap | 该过程不能替代 hgo 的重复键/Unicode/预算策略，也不能自动添加方法 |
| [stream.go](https://go.dev/src/encoding/json/stream.go)，RawMessage.MarshalJSON/UnmarshalJSON | 非 nil RawMessage 编码方法直接返回底层字节；解码方法复制数据，本身不校验 JSON | 桥需要显式校验和所有权边界，不能把调用原生方法等同安全 raw 通道 |
| [time.go](https://go.dev/src/time/time.go)，Time.MarshalJSON/UnmarshalJSON | 原生方法有自己的时间格式与 null/错误行为 | typed 日期 helper 是业务格式策略，不声称与原生方法等价 |
| [serde_json RawValue 文档](https://docs.rs/serde_json/latest/serde_json/value/struct.RawValue.html) | raw_value 特性提供一个 JSON 值的原文表示，拥有/借用形态分开 | 只说明格式专用 raw 能力可以独立于 Value 树；不移植其借用/零拷贝承诺 |

本地源码核对的 SHA-256：`encoding/json/stream.go` 为 `065501364e4954cf1c8f1887900248d4c0983593b8c058b2fe7997d9635b793d`，`encoding/json/encode.go` 为 `8ff45e82c60c6e29d11fe57ce0c59d79f40ef0d53be28ac36617482a7463b217`。网络事实只作设计依据，当前 hgo 能力以基线和探针为准。

## 3. 两种最小注解入口

### 候选 A：具体函数路径，推荐首个切片

```hgo
// Proposed：当前编译器拒绝这两个属性。
#[derive(Serialize, Deserialize)]
type Event struct {
    #[serde(serialize_with="WriteTime", deserialize_with="ReadTime")]
    Created time.Time
}
```

首版路径仅允许同包具体顶层函数名（可位于另一 `.hgo` 或 native `.go` 文件）；没有闭包、函数变量、接收者方法、泛型实参、任意表达式或字符串 eval。先不接受 imported `pkg.Func`；调用外部库可在同包普通 helper 中完成。这减少导入元数据/包别名卫生负担，但不妨碍 `time.Time` 作为具体 T：新工作是识别被显式选中 codec 的字段及签名，不能继续先套内建字段白名单。

候选签名固定为：

```go
func WriteTime(s serde.Serializer, value time.Time) error
func ReadTime(d serde.Deserializer) (time.Time, error)
```

编码采用 **值参数 T**，与当前字段 typed codec 的调用方向一致；与 Rust 的 `&T` 不同，不承诺零复制/不可变借用。大 struct 会复制，slice/map/pointer 是浅复制，helper 可以产生业务副作用；首例 time.Time 不据此授予锁/引用等类型复制资格。不要同时引入 `T`/`*T` 两套自动猜测签名。推荐首片复用现有 scalar、有限本地值 struct/enum、已支持容器/指针/Option 的字段类型范围，仅免除被显式替代方向的 serde 要求；新增导入类型限按真实包/类型身份识别的 time.Time 及具体 Option[time.Time] wrapper。不得将“有 helper”当成任意 imported struct、channel、func 或锁/读写引用的准入证据，也不承诺新增通用复制资格分析；额外类型需另列范围与验收。

两方向可分别指定：Serialize-only 只要求写函数；未覆盖的派生方向仍查既有 codec，不悄悄补另一半。出现在未派生方向的独立属性建议明确诊断，避免无效配置；这是 Proposed 规则，不声称与所有 Serde 边缘行为一致。

### 候选 B：with 导入包约定

```hgo
// Proposed：timecodec 是当前文件显式导入的普通 Go 包别名。
#[serde(with="timecodec")]
Created time.Time
```

约定 `timecodec.Serialize(s,T) error` 与 `timecodec.Deserialize(d)(T,error)`；使用 Go 导出名，而非 Rust 模块的 lowercase 函数。只解析包，不接受名为 timecodec 的 struct、变量或方法集。单向 derive 只要求该方向函数。每个包固定一个具体 T；不增加“泛型 adapter 包”或隐式实例化。与独立方向属性重复占用同一方向时拒绝。

| 比较 | A：具体函数对 | B：with 包 |
| --- | --- | --- |
| 最小前端增量 | 同包声明身份、具体签名、字段调用点 | 另需导入包导出对象、别名作用域及固定名字约定 |
| 用户成本 | 属性较长；可先写两个本地函数 | 使用短，需单独 adapter 包，每种具体格式/类型一包 |
| 单向覆盖 | 可只替换一个方向 | with 选择派生所需方向；混合覆盖需要明确冲突规则 |
| 后续关系 | 可扩 imported 函数路径 | 可作为相同 typed 调用机制的糖，不应另建 trait 系统 |

**推荐 A 先行，B 暂作后续糖**。不是一次批准三个注解的全部 Rust 语法；若用户更看重 with 的首日体验，B 可先行但实施范围相应增大。

## 4. 符号身份、签名与诊断

这些要求是实施验收建议，尚无新 resolver：

1. 在字段定义包/文件的静态上下文解析，不在生成的 visitor 局部作用域重新查找字符串。A 绑定唯一顶层函数对象；B 绑定实际 import path 与导出函数对象。包别名、helper、payload 类型名恰与 `s/d/v/x` 同名，不能捕获生成引用。
2. 检查规范化后的具体函数类型：参数个数、顺序、非 variadic、返回值个数、T 的 Go 名义身份与核心接口身份。Go alias 可按真实类型相同性处理，底层相同的 defined type 不等价；不自动取址/解引用、转换，或把任意 error-like 接口当成 builtin error。函数变量/方法/generic helper 均报未支持。
3. 验证所用方向；缺少函数、错误 T、可空返回指针、错签名、重复/conflict 属性分别诊断。以属性路径 span 为主，关联字段和函数声明位置；真实 Go 检查作为二次证据，不能只报生成行号。生成绑定使用编译器私有名和每文件命名空间，不能仅靠拼名字安全。
4. 推荐首片属性必须直接写在 struct 字段上。当前声明宏不会因此获得“字符串路径自动卫生”能力；item 宏生成带 adapter 属性的声明先明确拒绝或列为不支持，不把字符串自动按宏调用点捕获。函数体继续使用既有普通表达式/宏能力。
5. adapter 只替换该字段所选方向的 codec，不注册 T 的全局 Serialize/Deserialize，不授予 T.Default，不放宽普通 enum payload/投影/引用限制，不改变其他同类型字段。

候选生成调用形状：

```go
// 示意；真实实现需静态对象绑定和原始属性位置记录。
err = fields.Field("created", func(s serde.Serializer) error {
    return boundWriteTime(s, value.Created)
})
// 读取仍在 NextValue 子调用内：
err = fields.NextValue(func(d serde.Deserializer) error {
    item, err := boundReadTime(d)
    if err != nil { return err }
    result.Created = item
    return nil
})
```

运行时 core 检查回调恰好处理一个值、结束状态与错误污染沿用，不添加隐藏反射 fallback。

## 5. Option / default / skip 的 Proposed 组合

普通 Option 的规则已由用户校正明确：serde 自身处理缺失/null→None，与 std Default 独立，普通缺失不得调用 Default 检查或生成器。固定源码依据和当前实现见母 RFC [协议独立性](0006-serde-core-and-formats.md#73-普通-option-协议与-default-相互独立)。下表只有新 adapter 的组合仍属 Proposed；这一校正不批准 adapter 入口或自定义缺失策略。

明确区分“字段 codec”与“自动提升 payload codec”。两候选都选中**整个字段类型 T**；若字段是 `Option[time.Time]`，函数的 T 必须是该 Option，而非 time.Time。没有自动给任意函数包一层 Option、也没有自动递归应用到容器元素。用户可写具体同包 wrapper；首片不需要用户泛型声明。

| 情况 | 推荐候选行为 | 与既有规则/Serde 的关系 |
| --- | --- | --- |
| 无 adapter | 完全沿用基线 | Option 缺失/null→None 已批准，不重开 |
| 有 adapter，字段存在 | 调用一次字段 helper；包括显式 null | whole-field opt-in 接管 null，不在调用前先拆 Option；helper 若要 None 使用现有 Option/Visitor 协议 |
| 有 adapter，普通字段缺失 | 不调用 helper；无 default 报 MissingField，有 default 调用已登记 Default | time.Time 尚无可消费的 Default，不能偷偷用 time.Time{}；首例 required |
| 有 adapter，Option 字段缺失 | **推荐保留 hgo 的 None，不调用 helper**；default 仍为 Option.None | **有意偏离 Serde 1.0.228 自定义解码缺失规则，待用户选择** |
| default + 显式值/错误/null | 存在值由 helper 处理，任何错误不触发 default | 不能把默认工厂当解析失败兜底 |
| skip + adapter | 建议编译期拒绝冲突配置；要求移除无效 adapter | 已有 skip-alone 保持：格式级消费/预算，不匹配目标类型，成功后调用 Default |
| 调用时机 | 显式 helper 在字段读取时运行；本层读取和 required 检查通过后再按字段声明顺序回填 default/skip | helper/default 的副作用不回滚，后续重复键/尾部失败也不回滚 |

另一可选策略是自定义解码严格对齐 Serde：Option 也必须显式 `default` 才可缺失。推荐保留 None 是为了减少 hgo 既有可选字段的意外变化，但它必须作为明确选项批准，不能从“仿照 serde-rs”自动推导。whole-field helper 可自定义 null 表示同样属于新属性的 Proposed 契约，不改无属性字段；本文不赋予 PATCH missing/null/value 三态。

`Option[time.Time]` 也不自动获得跨包 sum codec 或新 enum 载荷位置。skip-alone 的旧 Default 要求不变，给导入类型加 adapter 并不能让 `serde(default/skip)` 合法。

## 6. 错误 cause、path 与位置

helper 原样返回 Go error。既有 Struct/Map/Seq/Enum Access 在字段/键/索引/变体边界加一段；helper 不再次添加自己的外层 wire 字段名，避免双重 Field。普通 `time.ParseError` 被包装为 Custom 并留在 cause 链；从核心读字符串得到的 Type/Syntax 等保留原 Kind/Path，不重新分类成解析失败。`errors.As/Is` 必须仍可用，不把错误转成 `NewError(..., err.Error())` 而丢 cause。

native bridge 若以后采用 `json.Marshal`，保留其真实 MarshalerError 与底层 cause；若显式直接调用方法，就只保留真实返回错误，不伪造一个“经过 json.Marshal”的 wrapper。建议固定选一种调用路线并写入协议，不能随类型自动切换。校验器自产的重复键/Unicode/预算错误没有原生 cause 时不制造 cause。

错误位置分别记录：hgo 属性/函数签名诊断回到源码；JSON 当前流由 Decoder.Token 返回的 `*json.SyntaxError.Offset` 保留真实值；native 子值解析器的 offset 是子值相对位置。**首片不把子值 offset 冒充整文档位置**，也不增统一 offset API；可先以字段路径和原 cause 区分来源。

## 7. 格式无关 helper 与 JSON 原生桥

| 维度 | typed time helper：建议先做 | JSON native/RawMessage 桥：后置研究 |
| --- | --- | --- |
| 数据流 | 时间 ↔ 核心 String，JSON 后端负责引号/转义 | 原生方法 ↔ 原始 JSON 子值，必须有 JSON 私有能力 |
| 格式范围 | 任何实现 String 的后端可复用；不代表 YAML 已实现 | 只在 serdejson 可用，其他后端应明确 Unsupported |
| 当前机制 | 已有 Marshal/Unmarshal helper 调用可运行 | 核心接口没有读/写 raw JSON；现有 API 不能直接把 RawMessage 嵌入任意字段 |
| 精度 | 时间格式由 helper 明定 | 不经 Any/float64/Value 中转，应保留原数字 token |
| 修改需求 | 前端属性解析、静态签名/调用；现有运行时可复用 | 另需格式扩展、字段 raw span/状态/预算/所有权设计，不是几个 frontend 属性即可完成 |

### 7.1 为什么不自动采用 Go 方法集

自动采用会让依赖新增方法、值/指针方法集、可寻址性等改变派生行为；还可能把 JSON 格式耦合进核心协议。Go 原生默认的重复键、Unicode、[]byte 等行为与 hgo 严格后端不同。明确字段选择使调用及副作用可审计，不自动为原类型注入 MarshalJSON/UnmarshalJSON。见母 RFC [原生接口边界](0006-serde-core-and-formats.md#63-go-json-实现细节与原生接口)。

### 7.2 未来桥的最低约束（不是已批准 API）

可研究由 serdejson concrete reader/writer 实现**可选的格式私有 raw 能力**，native Go helper 显式请求，其他后端返回 Unsupported；不向核心 Serializer/Deserializer 增加 JSON 方法。这里不冻结方法名、接口形状或新增属性。仅靠公开 InputOffset 不足以实现：字段分隔符、前导空白、Option 已缓存 token、嵌套状态和 value 起始位置都需跟踪；必须保证一次消费，不再次读取兄弟值。

编码建议流程：显式调用选定原生函数一次 → 若有错误立即保留 → 对返回字节取独立快照 → 严格验证恰好一个 JSON 值 → 通过父 writer 状态写入。验证必须包括转义后同名重复键、UTF-8/代理项、深度与大小；`json.Valid` 或 `json.Marshal` 自身不足以代替此策略。不把完整 raw token 变成 float64；不保证原生内部格式化/空白与原输入完全一致。

解码建议流程：在真实当前字段位置捕获一个完整 raw 子值 → 严格验证/计费 → 将独立拥有的字节交给显式 native 解码函数一次 → 成功后赋值。使用新建目标，避免默认复用旧对象；之后原生实现的字段/default/副作用由该实现负责。类型实现若有内部共享状态不能靠桥证明安全。

预算继承父调用：当前 writer 已输出的字节 + raw 输出不能超过总 MaxBytes；深度从字段当前位置继续计算，不能对每个 raw 子值重新从 0 开始。Option 委派和外层容器也消耗当前协议深度。校验扫描与原生调用可能重复工作，但不能把同一值当两个兄弟值或重复增加逻辑字节量；CPU 扫描成本另行测量。不得通过独立 `Decode(raw, Skip)` 的新顶层预算绕过父状态——本文探针只能证明独立 raw 校验机制，**没有证明嵌套桥正确**。

所有权：RawMessage.MarshalJSON 可返回原 slice，必须在验证/写出期间使用自己的快照；调用者仍须同步并发修改，复制不构成 race 屏障。RawMessage.UnmarshalJSON 复制输入，但一般原生 Unmarshaler 可能保留输入；给它独立拥有的缓冲，不复用池/decoder 临时区，亦不宣称阻止用户主动创建别名。方法可在返回前任意分配或运行，输出上限只约束已返回数据/后续 writer，不能给任意原生方法提供资源沙箱。

性能代价：typed helper 有接口调用、可能的闭包逃逸/分配、T 值复制和日期格式化；raw 桥至少有快照复制、严格扫描，解码还可能由原生解析器再次扫描/复制。峰值内存可能同时持有整输入、子值副本和业务对象。没有 benchmark，不声称零成本或比现有 DTO 更快。

## 8. 已执行机制探针与可复现步骤

本轮仓库外路径 `/workspace/goxide-field-adapter-research`，不提交探针为编译器实现：`typed/main.hgo` 经安装 CLI run 输出 `typed="2026-10-02T10:20:30.123Z" ParseError=true`；`mechanism/probe_test.go` 的 2 项 `go test -race -count=1 -v ./...` 通过。检查了重复键/第二值/孤立代理项/超深拒绝，raw 大数字（包括 1e999）可由 Skip 验证，RawMessage 别名/复制以及原生错误链。

以下短机制程序可脱离该目录重建，使用基线 runtime；它不是字段 adapter 实现：

```go
package main
import (
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "time"
    "github.com/hxsf/goxide/serde"
    "github.com/hxsf/goxide/serdejson"
)
func writeTime(s serde.Serializer, v time.Time) error {
    return s.String(v.Format(time.RFC3339Nano))
}
func readTime(d serde.Deserializer) (time.Time, error) {
    text, err := serde.ReadString(d)
    if err != nil { return time.Time{}, err }
    return time.Parse(time.RFC3339Nano, text)
}
func strict(raw []byte) error {
    return serdejson.Decode(raw, func(d serde.Deserializer) error { return d.Skip() })
}
func main() {
    value, err := serdejson.Unmarshal([]byte(`"2026-10-02T10:20:30.123Z"`), readTime)
    if err != nil { panic(err) }
    wire, err := serdejson.Marshal(value, writeTime)
    if err != nil { panic(err) }
    _, err = serdejson.Unmarshal([]byte(`"bad"`), readTime)
    var parsed *time.ParseError
    fmt.Printf("typed=%s ParseError=%v\n", wire, errors.As(err, &parsed))
    raw := json.RawMessage(`{"x":1,"x":2}`)
    native, err := json.Marshal(raw)
    fmt.Printf("native=%s err=%v strict=%v\n", native, err, strict(raw))
    source := []byte(`{"x":1}`)
    var owned json.RawMessage
    if err := owned.UnmarshalJSON(source); err != nil { panic(err) }
    source[5] = '2'
    alias, err := owned.MarshalJSON()
    if err != nil { panic(err) }
    alias[5] = '3'
    copied := bytes.Clone(alias)
    copied[5] = '4'
    fmt.Printf("source=%s owned=%s snapshot=%s\n", source, owned, copied)
}
```

在新目录将程序保存为 `main.go`，创建只含本仓库 runtime 的模块：

```sh
export PATH=/workspace/goxide-environment/go/bin:$PATH # 换成实际 Go 1.27+ 目录
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off
go mod init adapterprobe
# 换成实际 checkout；无需外部依赖。
go mod edit -require=github.com/hxsf/goxide@v0.0.0 -replace=github.com/hxsf/goxide=/workspace/goxide
go run -race .
```

预期：时间字符串往返，ParseError=true；原生接受重复键而 strict 返回 Duplicate；source/owned/snapshot 的 x 分别是 2/3/4。日期 helper 只是选定布局的应用样例，不宣称支持所有 time.Time 状态、单调时钟或保证任意年份可逆。还实际验证候选 A 在基线报 `candidate.hgo:5: unsupported serde attribute serialize_with="WriteTime"`；所以“helper 可以运行”不等于“字段注解已经实现”。

## 9. 实施前验收与需要选择的最少问题

若批准首片，验收必须有新的 `.hgo` 多文件日期业务，经 CLI 安装→仓库外转译→独立生成模块 race，而非只运行手写 Go：

- time.Time 的正常字符串、非法日期、null、缺失；ParseError cause 与恰好一个 Field 路径；同一类型不同字段可选不同格式，未标注字段仍明确拒绝。
- 单向派生、错误签名/defined type、同包跨文件、helper/类型/局部同名、原始属性 span；无函数变量或泛型反射 fallback。
- Option 缺失/null/Some/错误、default 不兜底、skip 冲突、普通字段缺 Default、嵌套失败副作用不回滚。具体 Option wrapper 明确校验有效表示，不让非法 native case 成为业务数据。
- 既有 enum payload/投影限制、语法拒绝与普通 Go 显式调用保持；全套 race/vet/设计文档检查仅在实施后执行，不用本轮 docs check 充当实现通过。

JSON raw 桥不是首片交付条件；若之后单独批准，另加嵌套父预算、cached Option token、字节快照、巨大精确数字、Unicode/转义重复键、错误污染/重复消费、其他格式 Unsupported 及 native 错误路径验收。完整 trait、Value/PATCH、borrowed raw、递归/循环任意对象图、自动方法采用、一般 adapter 泛型、任意 enum 位置仍排除。

需要用户选择仅两项，以下均未批准：

1. **入口/范围：推荐 A（具体函数对）+ time.Time 首例；B 的 with 包糖和 JSON raw 桥后置**，还是 with 必须首片可用？
2. **带自定义解码的 Option 缺失：推荐保留 hgo None**，还是严格按 Serde 要求显式 default？两者都不把缺失传给 helper，也不扩 PATCH；显式存在值（含 null）由 whole-field helper 接管。

## 10. 本轮研究审查记录

独立非作者核对了固定版本 Serde 的缺失处理与 Go RawMessage 源码，并独立重跑 2 项机制 race 测试，无阻断；据其建议精确化了 SyntaxError.Offset 表述和首片类型范围。正文完整 Go 机制程序另经提取后 `go run -race` 执行，与所列输出一致。最终 docs-only 检查通过 **57 Markdown、524 本地链接/锚点、20 篇逐特性 RFC 结构**及 diff 检查；外部链接已通过本轮官方浏览核对，不由 docs checker 检查。

这仅是研究文档与现有机制验收，没有运行“新字段 adapter”的端到端测试，也未因此批准候选。前端、runtime、源码测试文件未修改；只在当前工作分支同步本文及研究入口，不修改 main/work/tag，不发布/合并。

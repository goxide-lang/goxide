# serde 与 JSON

## serde 与 JSON 概览

serde 为类型声明格式无关的 `Serialize` 和 `Deserialize` 协议，`serde::json` 负责 JSON 格式。为 struct 或 enum 写上 `#[derive(serde::derive::Serialize, serde::derive::Deserialize)]`，过程宏会生成普通 `impl`；也可以只派生一个方向，或手写协议实现。

本章包括 [struct 与字段标记](#struct-与字段标记)、[enum 与分派格式](#enum-与分派格式)、[Option 与缺失值](#option-与缺失值)，以及[限制和改进方向](#限制和改进方向)。示例的字段类型和 enum payload 必须实现所用方向的协议。

### 字符串 sugar

新接口直接传入字符串或只读引用，省去 JSON 回调和字节转换：

```gox
pub func FromString[T serde::Deserialize](str string) (T, error)
pub func ToString[T serde::Serialize](t &T) (string, error)
```

使用方式：

```gox
package main
use serde;
use serde::json as json;

#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
struct Config { pub Port int }

func main() {
    config, err := json::FromString[Config](`{"Port":8080}`)
    if err != nil { panic(err) }
    text, err := json::ToString[Config](&config)
    if err != nil { panic(err) }
    println(text) // {"Port":8080}
}
```

serde v0.0.1 已提供这两个接口，Playground 示例直接使用它们。需要处理字节或显式传递协议回调时，也可以使用下面的等价调用：

```gox
config, err := json::Unmarshal([]byte(input), serde::Read[Config])
data, err := json::Marshal(&config, serde::Write[Config])
text := string(data)
```

两种写法使用相同的 serde 协议和 JSON 校验。`FromString` 的 `T` 必须实现 `Deserialize`，`ToString` 的 `T` 必须实现 `Serialize`；不会自动寻找 Go 的 `MarshalJSON` 或 `UnmarshalJSON` 方法。错误非空时，先处理错误，再使用返回值。

### 字节与底层协议入口

| 入口 | 用途 |
| --- | --- |
| `json::FromString[T](text)` | 从完整 JSON 字符串解析 `T` |
| `json::ToString[T](&value)` | 把 `T` 编码为完整 JSON 字符串 |
| `json::Unmarshal(data, serde::Read[T])` | 从 `[]byte` 解析 `T` |
| `json::Marshal(&value, serde::Write[T])` | 编码为 `[]byte` |
| `json::Decode(data, callback)` / `json::Encode(callback)` | 直接使用 `Deserializer` / `Serializer` 协议 |

在项目清单中声明 serde 与 std 依赖，版本沿用所选编译器的公开依赖；依赖配置见[依赖与命名空间](website-guide.md#依赖与命名空间)。`use serde;` 绑定协议与派生宏的根，`use serde::json as json;` 为格式包取别名。

## struct 与字段标记

struct 编码为 JSON 对象。默认使用字段名作为 JSON 键；解码时按键分派，与输入键顺序无关。普通必填字段缺失会报错；字段是否允许缺失由其 `Deserialize::missing` 实现决定，`Option[T]` 允许缺失。

[打开 struct 示例](https://goxide-lang.github.io/playground.html?example=serde_struct)，查看解析、编码、字段默认值和错误分支。

### rename 与 default

`#[serde(...)]` 是派生宏读取的辅助标记，不是单独执行的属性宏：

```gox
use std;
use serde;

#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
struct Settings { pub Port int }

impl std::Default for Settings {
    func r#default() Self { return Settings{Port:8080} }
}

#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    #[serde(rename="name")]
    pub Name string
    #[serde(default)]
    pub Settings Settings
    pub Limit Option[int]
    #[serde(skip)]
    pub Internal string
}
```

输入 `{"name":"demo"}` 会得到 `Name == "demo"`、`Settings.Port == 8080`、`Limit == None` 和空的 `Internal`。再次编码得到：

```json
{"name":"demo","Settings":{"Port":8080},"Limit":null}
```

`rename` 同时改变编码键和解码键。上例的键是 `name`，不是 `Name`；配合 `deny_unknown_fields`，输入 `Name` 会报未知字段错误。

`default` 只在字段缺失时调用字段类型的 `std::Default`。它不会忽略错误输入：`{"name":"demo","Settings":null}` 仍然失败。业务默认值来自显式 `impl std::Default`；普通 Go 零值和名为 `DefaultSettings` 的函数不会自动成为该协议。

### 字段策略

| 标记 | 位置 | 编码 | 解码 |
| --- | --- | --- | --- |
| `#[serde(rename="name")]` | struct 字段 | 使用指定键 | 按指定键匹配 |
| `#[serde(default)]` | struct 字段 | 正常编码 | 缺失时调用该字段类型的 `std::Default` |
| `#[serde(skip)]` | struct 字段 | 省略字段，不调用字段的 `Serialize` | 忽略输入值并调用字段类型的 `std::Default` |
| `#[serde(deny_unknown_fields)]` | struct 声明 | 正常编码 | 拒绝未声明的键 |

未标记 `deny_unknown_fields` 时，未知字段会被读取并跳过。跳过的字段依然必须是合法 JSON，重复键、无效 Unicode 和超出限制的嵌套仍会失败。

派生反序列化先检查当前对象和所有必填字段，再按字段声明顺序执行缺失字段和跳过字段的默认值。嵌套值在读取时完成自己的反序列化，已经执行的用户回调不会在外层失败时回滚。

如果只派生 `Serialize`，`default` 不要求额外的 `Default` 实现；只派生 `Deserialize` 也不要求字段实现 `Serialize`。字段的实际读写需求决定协议约束。

## enum 与分派格式

当前派生支持 unit 变体和单 payload 变体。默认使用外部标签：JSON 字符串或对象最外层的键决定变体，再调用该变体 payload 的协议。

[打开 enum 示例](https://goxide-lang.github.io/playground.html?example=serde_enum)，查看外部标签解析、编码和 unit 变体映射。

### 外部标签：当前支持

```gox
use serde;

#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
struct MessageBody { pub Text string }

#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
enum Event {
    Idle,
    Count(int),
    Message(MessageBody),
    Object { Value int },
}
```

| Goxide 值 | 编码结果；解码该 JSON 会选回同一变体 |
| --- | --- |
| `Event::Idle` | `"Idle"` |
| `Event::Count(7)` | `{"Count":7}` |
| `Event::Message(MessageBody{Text:"hello"})` | `{"Message":{"Text":"hello"}}` |
| `Event::Object{Value:3}` | `{"Object":3}` |

单命名字段变体同样作为一个 payload；`Value` 不是额外一层 JSON 字段。要编码一个含多个键的 payload，使用独立 struct，如上例的 `MessageBody`。

解码先读取外层变体名，随后按该变体读取 payload。未知变体、错误 payload 类型、`null`、空对象或含多个变体键的对象都会失败；unit 变体只接受字符串，例如 `"Idle"`，不接受 `{"Idle":null}`。无效 enum 值在序列化时返回错误。

### unit 变体的 rename 与数值映射

纯 unit enum 可以为变体指定字符串名称：

```gox
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
enum Status {
    #[serde(rename="ready")]
    Ready,
    Busy,
}
```

`Status::Ready` 编码为 `"ready"`，也从 `"ready"` 解码；`Status::Busy` 仍使用 `"Busy"`。输入 `"Ready"` 不再匹配。

也可以为每个 unit 变体显式指定 signed int64 数值：

```gox
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
enum Code {
    #[serde(value=-1)] Failed,
    #[serde(value=2)] Accepted,
}
```

`Code::Accepted` 编码为 JSON 数字 `2`，从 `2` 解码；未声明的数值报错。数值映射要求每个变体都有 `value`，值不能重复，也不能与 `rename` 混用。

目前只要 enum 使用了变体 `rename` 或 `value`，所有变体都必须是 unit。对带 payload 的 enum 重命名会得到 `explicit enum serde mapping requires unit variants`。

### 内部标签、相邻标签与 untagged

下面比较常见布局。后三种尚无自动派生配置；`untag` 通常称为 `untagged`。

| 布局 | JSON 形状示意 | 当前状态 |
| --- | --- | --- |
| 外部标签 externally tagged | `{"Message":{"Text":"hello"}}` | 默认派生支持解析与编码 |
| 内部标签 internally tagged | `{"kind":"Message","Text":"hello"}` | `#[serde(tag="kind")]` 未支持；可用 wire struct 与显式转换 |
| 相邻标签 adjacently tagged | `{"kind":"Message","data":{"Text":"hello"}}` | `#[serde(tag="kind", content="data")]` 未支持；固定形状可用 wire struct |
| 无标签 untagged | `{"Text":"hello"}` 或 `7` | `#[serde(untagged)]` 未支持；类型形状互斥时可手写分派 |

当前宏会明确拒绝这些 enum 容器标记，如 `serde tag is not supported at this position`、`serde untagged is not supported at this position`；它们不会被悄悄忽略。

[打开手写 untagged 示例](https://goxide-lang.github.io/playground.html?example=serde_untagged)：通过普通 `impl Serialize` / `impl Deserialize`，用 `Deserializer.Any` 的 `OnInt64` / `OnUint64` / `OnString` 按 JSON 类型分派，再直接编码对应 payload。非负整数进入 `OnUint64`，负整数进入 `OnInt64`；数字与字符串互不重叠，无需依次试解码。这种用法不提供任意对象形状之间的失败重试。

通用派生所需的协议改进见[限制和改进方向](#限制和改进方向)。

## Option 与缺失值

`Option[T]` 使用普通 serde 协议。JSON 的缺失字段和显式 `null` 都解码为 `None`；非 null 值通过 `T` 的协议解码为 `Some(value)`。

[打开 Option 示例](https://goxide-lang.github.io/playground.html?example=serde_option)，查看三种输入与再次编码的结果。

```gox
use serde;

#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
struct Request { pub Limit Option[int] }
```

| 输入 | `Limit` | 再次编码 |
| --- | --- | --- |
| `{}` | `None` | `{"Limit":null}` |
| `{"Limit":null}` | `None` | `{"Limit":null}` |
| `{"Limit":7}` | `Some(7)` | `{"Limit":7}` |
| `{"Limit":"seven"}` | 解码失败 | 无成功结果 |

`None` 编码为 `null`，不会自动省略字段。`Some` 编码其 payload。普通 Option 字段无需 `#[serde(default)]`；显式加上该标记仍然得到 `None`，不会构造 `Some(default T)`。

即使运行时只出现 `None`，派生仍要求 `T` 实现对应方向的协议。例如 `Option[chan int]` 缺少相应协议，不能靠空值绕过检查。

### 与指针、PATCH 三态的区别

`*T` 支持 JSON `null` 与非 null 值，但普通指针字段仍是必填字段；允许 `null` 不代表允许字段缺失。

Option 不保留 PATCH 请求中的“未提供 / 显式清空 / 设置值”三态。嵌套 Option 也不能恢复该区别：`Some(None)` 与 `None` 都可以编码为 `null`；可空指针 payload 的 `Some(nil)` 也会如此。需要三态时，应用应显式建模并手写读取逻辑，或等待专用字段适配器。

## 限制和改进方向

### enum 派生还缺什么

| 需求 | 具体阻碍 | 改进方向 |
| --- | --- | --- |
| 内部 / 相邻标签派生 | 宏不接受 `tag` / `content`；当前 JSON enum 入口只读取字符串或单键外部标签对象。对象中的标签也可能出现在 payload 后面 | 先确定布局、标签冲突和未知字段规则，再让派生生成对应对象读写；解码需支持标签晚出现时暂存并重放 payload |
| 通用 untagged 派生 | 宏不接受 `untagged`；解码器单向消费 token，候选失败后会保留错误，无法重置后再尝试下一个变体 | 明确候选顺序、重叠形状与失败诊断，再提供有大小 / 深度限制的缓冲重放；类型形状互斥时也可按一次 token 分派 |
| payload 变体 `rename` | 当前派生把显式 enum 映射限制在纯 unit enum | 分开字符串重命名与数值映射校验，让外部标签名称可以传给 payload 分支，并验证重复名称和错误路径 |
| 多字段 tuple / struct 变体 | 派生拒绝超过一个 payload；`VariantAccess` 只有 `Unit` / `Newtype` | 当前先用一个独立 struct 承载数据；直接支持多字段变体需确定 JSON 数组 / 对象布局，并增加对应协议与派生逻辑 |

这些能力没有被本次字符串 sugar 隐式启用。项目现在可以手写 `Serialize` / `Deserialize`：固定对象形状可使用 `Struct` / `Map`，互斥的标量形状可使用 `Any` 与 `VisitorFuncs` 分派。任意 untagged 候选的失败重试需要上述缓冲能力，不能直接重用同一个已消费的 `Deserializer`。

### JSON 的实际边界

JSON 后端支持布尔、精确 signed/unsigned 64 位整数、有限浮点数、字符串、数值字节数组、Option、指针、序列、字符串键 map、struct 和上述 enum 格式。`[]byte` 使用数字数组，不是 base64；map 编码按键排序。

输入与输出上限为 16 MiB，协议嵌套上限为 128 层；Option / 指针的委派也占用嵌套层。每次调用处理一个完整 JSON 值，尾随第二个值会失败。返回的字符串和字节拥有自己的存储。

后端拒绝重复对象键、无效 UTF-8、未配对 UTF-16 surrogate、非有限浮点数、浮点溢出及非零下溢为零。整数目标不接受小数或指数写法。跳过的值同样经过语法校验。

错误保留 `serde::Error` 的 kind、字段 / 索引 / 变体路径，并在存在底层错误时保留 cause，可通过 Go `errors.Is` / `errors.As` 检查。`Any` 会把数字转成机器整数或浮点数，因此不是任意精度 JSON 数值容器。

动态 `Value` / `Number` / `RawValue`、自动调用原生 JSON 方法、字段 adapter、流式 I/O 和 YAML 尚未实现。当前可以使用显式 Go 调用或手写 serde 协议连接这些需求；具体协议定义见 [serde 源码](https://github.com/goxide-lang/serde/blob/main/protocol.go)。

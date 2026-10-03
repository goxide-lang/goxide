# RFC 0013：Default、Clone、Debug 内建派生

## 状态与范围

已确认目标：内建 `#[derive(Default, Clone, Debug)]`；用户可编程 derive 延期（D14）。
Default 的字段约束、enum 默认变体规则与无 static 的关联函数声明已确认；通用静态降低、泛型证据和 formatter 仍未定。
依据：[原设计](../design/macros.md)。derive 生成普通显式 impl，不能绕过 trait / 借用检查。类型关联函数/构造函数见 [RFC 0020](0020-associated-functions.md)；同名构造不会自动实现 Default。

## 源级规则

最新 D28 补充确认：struct 自动 Default 必须每个字段有 Default，否则手动实现；enum 恰好标记一个默认变体，可以携带 payload，仅该变体的字段递归要求 Default，非默认变体不要求 Default。手动 Default 首期使用 `impl Default for T { func default() T { ... } }`，限同包本地非泛型 struct，具体边界见 [RFC 0020](0020-associated-functions.md#当前手动-default-实现子集)。

- 按规范 trait 身份选择固定内建规则；只读目标类型形状和固定辅助属性。
- Default 按声明顺序调用字段 Default；Clone 按同一顺序调用字段 Clone。
- Debug 按声明顺序格式化字段；enum 保留并格式化当前合法变体。
- 派生在目标类型包中生成实现；不读取外部类型私有布局、不修改字段、不新增导入。
- 字段缺少协议、同键重复实现或未知 derive 报错，不静默覆盖或降级为 Go 赋值。
- 普通赋值仍服从既有值复制规则；显式 Clone 不引入全局 move，也不承诺对象图深复制。

## 转译示例

**D01：非零业务默认值和具体类型 Clone。** hgo 声明片段。
依赖：`Label` 已有显式 Default（返回 `"untitled"`）、Clone（同值复制）和 Debug。

```hgo
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

**D02：共享字段的 Clone 仍共享。** hgo 声明片段；依赖 Arc 的显式句柄 Clone。

```hgo
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

**D03：缺少字段协议必须拒绝。** hgo 反例，无 Go 输出。

```hgo
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

hgo 调用生成实现与手写 impl 使用同一套选择、权限和唯一性规则。
Go 使用发布 `.go` 中公开的普通函数 / 类型；Default 不自动改变 Go 的零初始化行为。
原生 Go 调用方不受 hgo 借用检查；派生不能被描述为给整个 Go 程序增加所有权保证。
回到 hgo 时由 `.d.hgo` 或 `.hgo` 恢复协议信息，不从 Go 方法形状猜测派生语义。

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

# RFC 0013：Default、Clone、Debug 内建派生

## 状态与范围

已确认目标：内建 `#[derive(Default, Clone, Debug)]`；用户可编程 derive 延期（D14）。
具体派生规则与静态降低为候选方案（T11、T12）；静态成员语法、泛型证据和 formatter 未定。
依据：[原设计](../design/macros.md)。derive 生成普通显式 impl，不能绕过 trait / 借用检查。类型关联函数/构造函数见 [RFC 0020](0020-associated-functions.md)；同名构造不会自动实现 Default。

## 源级规则

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

依赖借用类型规则；`mut& int` 没有 Clone，不能生成普通指针赋值制造两个独占借用。
结构合法性若已在更早阶段拒绝，也不得由 derive 把它变成可接受程序。

## 拒绝与边界

- enum 的 Default 候选规则：恰好一个无 payload 的 `#[default]` 变体；其他情况手写实现。
- Result 不自动取得 Default；nil 不因 Default 或 Go 零值存在就成为合法 Result / 非空引用。
- 泛型 `var value T` 不能冒充 Default；容器需明确逐项调用选中的构造。
- 容器 Clone 必须说明存储、元素和别名行为；Go `slices.Clone` 不自动等于逐元素 Clone。
- Debug 不能绕过锁权限，不自动隐藏私有业务字段，也不承诺稳定序列化格式。
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
5. 未定：静态成员源码语法、泛型字典或专门化、Debug formatter 和稳定符号编码。
6. 未确定的泛型形式不得假称已有可编译降低；Go 示例不证明通用派生器已实现。

# RFC 0010：Vec 与 slice 视图

## 状态与范围

用户提出以 std 中的 Vec 提供高级序列能力；本篇采用此推荐方向继续设计。
具体名字、赋值语义、公共协议、迭代器与 Go ABI 均未冻结，也没有相应实现。
Vec 是库类型方向，原生 `[]T` 作为互操作形状保留；是否提供额外索引语法另定。
不同 Map 实现、公共查询协议与元素引用能力独立见 [RFC 0017](0017-maps.md)。
背景见[标准库专题](../design/standard-library.md)和[借用专题](../design/pointers-and-borrows.md)。

## 源级规则

- 容器负责合法初始化、边界检查、缺失查询和具体数据结构；语言负责泛型、借用、复制及调用效果检查。
- Vec 表达可增长序列；只读 / 可变范围视图不自动取得调整原容器形状的权限。
- 读取头部、读写元素、调整长度与容量分别检查权限；对门面的独占访问不自动证明底层数组没有别名。
- `get_copy` 可返回 Option；范围借用只能覆盖指定元素，不因持有 Go slice 就自动获得扩长能力。
- 容器支持库可利用 Go slices 算法，但必须覆盖算法真实写入范围，包括清零缩短后的尾部。
- 没有全局 move，因此普通赋值不能被解释为自动转移唯一所有权；共享和复制成本必须明示。

## 转译示例

以下 API 拼写都是提案，用例规定可观察行为，不代表已有 std 包可导入。

**V01：越界查询返回 Option，不暴露元素类型的非法零值。**

```hgo
values := Vec::of(7)
value := values.get_copy(3) // Option::None
```

```go
// 候选 slice 存储；Option / Some 使用 RFC 0008 候选 A。
data := []int{7}
index := 3
var value Option[int]
if index >= 0 && index < len(data) { value = Some[int]{Value: data[index]} }
```

元素为 Result 时也不能把越界结果解释成 nil Result；应返回外层 Option 的 None。
get_copy 要求元素可按约定复制，复制元素内部引用不会取得新的独占权限。

**V02：Vec 逐项构造，不能先公开未初始化的元素。**

```hgo
var values Vec[Result[int, string]] = Vec::from_fn(3, |i| Result::Ok(i))
```

```go
// 候选 slice 存储；构造完成前 data 不对外暴露。
data := make([]Result[int, string], 0, 3)
for i := 0; i < 3; i++ {
    data = append(data, Ok[int, string]{Value: i})
}
values := vecFromInitialized(data) // 支持库内部入口，名字未定。
```

成本：外层存储分配、逐项构造以及 payload 自身成本；不能以 Go make 的零填充冒充调用元素构造。

**V03：普通赋值是否共享，是需要选定的可观察语义。**

```hgo
a := Vec::of(1)
b := a
b.set(0, 9)
observed := a.get_copy(0) // 结果及是否允许写，取决于最终赋值/权限协议。
```

```go
// 候选一：复制 slice header；修改会被 a 观察到。
type VecHeader struct { data []int }
a := VecHeader{data: []int{1}}
b := a
b.data[0] = 9
observed := a.data[0] // 9
```

候选二是复制共享容器句柄，连 len / cap 状态也共享；候选三是复制独立外层存储，带来复制成本。
V03 用于迫使设计明确选择；不得把任何一种默认写成已确认规范。共享方案仍需权限检查，独立外层也不等于深复制元素。

**V04：元素写借用期间的 Vec 结构修改需要拒绝冲突。**

```hgo
values.with_mut(0, |element| {
    values.push(next) // 拒绝：与 element 的活跃借用冲突。
    use(element)
})
```

```go
element := &data[0]
data = append(data, next)
use(element) // Go 允许；可能指向旧数组，不能据此通过 hgo 检查。
```

Go GC 可以保留旧数组，但不保证 element 仍代表新 Vec 的同一槽位；无需等待实际重分配才拒绝冲突。

## 拒绝与边界

**V05：范围视图限制容量，但只读仍需 hgo 检查。**

```hgo
values.with_read_range(1, 3, |view| {
    inspect(view)
    view.set(0, 9) // 拒绝：只读视图不授予写权限。
})
```

```go
view := data[1:3:3]
inspect(view)
view[0] = 9 // Go 允许；限制容量没有限制写权限。
```

V05 的范围边界须先检查；view 及派生借用不能逃出允许范围，回调也不能经其他别名冲突访问相同存储。
公开未初始化容量、借用期间删除或移动元素、经 raw 视图绕过权限，均须拒绝或经明确的可信互操作入口处理。
临时元素、副本与原槽位引用必须在 API 上分清；不能为了产生引用悄悄复制一个元素。

## 双向互操作

Go `[]T` 与 Vec 并非只改名字就可互换。转换应区分复制外层存储与限定范围共享，两者都保留元素内部别名。
需要重装箱 Option / Result、初始化新元素或记录共享状态时，必须生成实际支持代码，不能靠 declare 擦除。
hgo 调原生 Go API 时，raw 导出仍遵守借用期限、留存及并发契约；函数返回后不能凭空认定所有 Go 别名已消失。
Go 调生成容器 API 时使用普通 Go 规则；生成库可以封装字段或检查输入，但不承诺第三方程序获得 hgo 静态权限保证。
具体实现与公共协议均应随 dist 交付；追加、删除、浅复制及逐元素 Clone 的复杂度和分配行为需分别说明。

## 验收与未定项

1. V01 正确区分越界和元素合法零值；V02 完成前不暴露未初始化元素。
2. 冻结赋值方案后，为 V03 固定结果或权限诊断，并覆盖 header、数组与元素内部三层别名。
3. V04、V05 是 hgo 拒绝而 Go 允许；不能以 Go 能编译作为权限检查通过的证据。
4. 覆盖追加是否重新分配、删除清零、范围重叠、借用逃逸及 raw 来源传播。
5. 未定：容器赋值、Clone 策略、索引语法、迭代协议、借用返回及公开 Go API。
6. 本篇没有选择唯一所有权模型；私有 data 字段不能代替对所有别名的权限检查。

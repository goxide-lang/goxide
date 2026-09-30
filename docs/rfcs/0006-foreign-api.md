# RFC 0006：原生 API、declare func、Ptr 与 unsafe

## 状态与范围

- 已确认：普通 `declare` 提供原生 Go 的 hgo 视图；使用 `Ptr` / `AsPtr` 与 `unsafe <expr>` 表达裸指针边界。
- 暂定：未声明 Go callable 统一 unsafe、unsafe 效果随函数值传播、`type Ptr[T any] *T` 表示。
- 未定：递归类型视图、回调桥接全规则、Ptr 来源恢复、最终语法及声明版本匹配。
- 本文约束 hgo 调用方及转译结果，不要求普通 Go 调用方遵守 hgo 静态权限规则。

背景：[原生 API](../design/foreign-api.md)。声明分发见 [RFC 0001](0001-package-loading.md)。

## 源级规则

原生视图保留真实 Go 符号和布局；声明视图补充 hgo 参数、结果和行为契约。
普通 `declare` 不执行危险操作，不使用 `unsafe declare`；被声明函数本身仍可具有 unsafe 效果。
编译器验证符号与降低后的签名；声明作者保证非留存、读写权限及重入等外部行为。
声明没有覆盖的函数不自动获得整个包的 safe 身份。

`Ptr[T]` 可空，不自带只读、独占或期限保证；单层转换保持地址及 nil。
`AsPtr` 暂定通过关联类型 `T` 和 `unsafe func as_ptr() Ptr[Self.T]` 表达后端指针导出。
`unsafe <expr>` 授权该表达式求值中的相关操作，不关闭类型检查，也不覆盖新闭包的函数体。
它不会在表达式结束时撤销外部已保留的指针。
unsafe callable 的赋值、返回、参数传递和泛型使用均应保留其效果。

## 转译示例

以下假定原生包 `native` 已有 `type T struct{ Value int32 }` 和 `func Fill(*T)`。
`native::` 是 hgo 中该包的示例限定名；Go 片段已导入 `native`。

**F01：declare 采用原函数。** 不生成第二份 Fill 实现。

```hgo
declare func native::Fill(p mut& native::T);

var value native::T
native::Fill(mut& value)
```

```go
var value native.T
native.Fill(&value)
```

约束：修改原 `value`。假定声明承诺短期不留存借用；签名匹配不证明这个行为承诺。
成本是原函数调用及所选边界检查，不要求 wrapper 分配。

**F02：raw 调用与指针导出。** 此例没有 F01 的声明，`p` 是现有写借用。

```hgo
// p: mut& native::T
unsafe native::Fill(p.as_ptr());
```

```go
// 支持库候选定义：type Ptr[T any] *T
// p: *native.T
raw := Ptr[native.T](p)
native.Fill((*native.T)(raw))
```

单层转换不复制 T，不需要 `unsafe.Pointer`；可以消去恒等转换。
unsafe 效果在 Go 中擦除，但保留在 hgo 声明元数据中。

**F03：函数值桥接。** 此例在 raw 视图取函数值；`raw` 已有 `Ptr[native.T]` 类型。

```hgo
f := native::Fill
unsafe f(raw);
```

```go
// 本片段选择 Ptr 出现在 hgo 函数值的 Go 参数中这一候选 ABI。
f := func(p Ptr[native.T]) {
    native.Fill((*native.T)(p))
}
f(raw)
```

`func(Ptr[T])` 与 `func(*T)` 不能靠直接赋值互换；该 ABI 下需要桥接。
选择保留原生函数值 ABI 的后端也可插入调用点转换，完整策略未定。
不得宣称所有函数值桥接均零成本；桥接调用和闭包是否消除由 Go 优化决定。

**F04：取得函数值不能清除 unsafe。** 拒绝发生在 hgo 前端。

```hgo
f := unsafe native::Fill
f(raw) // 拒绝：f 仍是 unsafe callable
```

```go
// 禁止据此放行：Go 的函数类型不携带 hgo unsafe 效果。
f := native.Fill
f((*native.T)(raw))
```

**F05：声明的指针层级不兼容。** `native.Fill` 的真实参数是 `*native.T`。

```hgo
declare func native::Fill(p native::T); // 拒绝：声明降低后参数为值
```

```go
// 禁止生成：验签应先报告声明与 func(*native.T) 不兼容。
var value native.T
native.Fill(value)
```

## 拒绝与边界

- Ptr 判空只证明非空；不能凭此安全恢复任意 `&T` / `mut& T`。
- `[]*T`、`**T`、含指针结构体和回调不得机械替换成含具名 Ptr 的不同布局。
- `Option[Ptr[T]]` 必须区分 None 与 Some(nil)，不能直接套用可空 Arc 优化。
- Go 不允许给具名指针接收者类型 `type Ptr[T any] *T` 定义方法；AsPtr 方法应使用 wrapper 或直接降低为转换。
- 相同布局不豁免借用来源检查；unsafe 导出指针后，未知外部行为由使用者承担。
- 原生包的 `init` 遵循 Go 规则；调用处的 unsafe 标记不构成初始化沙箱。

## 双向互操作

Go 模块可以同时携带 `.go`、`.d.hgo` 和 `.hgo`；Go 编译 `.go`，hgo 额外读取声明或源码。
声明也可来自调用方 `.hgo` 或独立声明包。兼容声明可合并，矛盾契约须诊断；选择不依赖扫描顺序。
hgo 导出的 unsafe API 在普通 Go 中仍是普通函数；Go 调用者不会获得 hgo 的效果检查。
改变实际行为需要真实包装，例如复制存储、约束回调或同步；声明本身不能把留存指针变成不留存。
`declare impl` 的方法采用及 marker 问题另见 [RFC 0003](0003-declare-impl.md)。

## 验收与未定项

1. F01–F03 验证原地址、原地修改与函数值可调用；F04–F05 必须在输出 Go 前诊断。
2. 对 Ptr 的 nil、双向单层转换和 AsPtr wrapper 分别验证，不将其推广成递归 ABI 证明。
3. 增加闭包体 unsafe 作用域、safe 回调位置拒绝 unsafe 函数值及元数据往返测试。
4. 声明需要记录行为契约；签名未变化的依赖升级仍可能需要重审留存与重入行为。
5. 最终 raw 操作分类、Ptr 恢复规则、回调 ABI、版本及构建条件匹配仍待定稿。

当前为待实现的规范约束；已有单层 Ptr Go 实验不代表 hgo 效果与来源检查已完成。

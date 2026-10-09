# RFC 0006：原生 API、declare func、Ptr 与 unsafe

## 状态与范围

- 已确认：普通 `declare` 提供原生 Go 的 goxide 视图；使用 `Ptr` / `AsPtr` 与 `unsafe <expr>` 表达裸指针边界。
- D19 已否决：未声明 Go callable 统一 unsafe。缺少摘要的普通调用正常保留，不自动宣称 safe，也不默认告警。
- 暂定：显式 unsafe 效果随函数值传播、`type Ptr[T any] *T` 表示。
- 未定：递归类型视图、回调桥接全规则、Ptr 来源恢复、最终语法及声明版本匹配。
- 本文约束 goxide 调用方及转译结果，不要求普通 Go 调用方遵守 goxide 静态权限规则。

D19/D20 边界：读写权限和已确认类型不变量照常检查；别名、留存、独占与重入风险分析独立长期推进，不属 MVP。未来仅有具体风险证据才 warning；未知不默认拒绝或告警。

背景：[原生 API](../design/foreign-api.md)。声明分发见 [RFC 0001](0001-package-loading.md)。

2026-10-02 更新：D31 已纠正普通写引用调用要求显式 AsPtr 的旧前提。具体标量 `&mut T` 到同型普通 Go `*T` 参数已直接接通，原生可留存/返回指针，readonly 不升级；[实现及验收范围](research/0002-native-pointer-bridge.md)。下文 Ptr/AsPtr 和函数适配示例保留历史候选，不是每个普通调用的必经步骤。

## 源级规则

原生视图保留真实 Go 符号和布局；声明视图补充 goxide 参数、结果和行为契约。
普通 `declare` 不执行危险操作，不使用 `unsafe declare`；被声明函数本身仍可具有 unsafe 效果。
编译器验证符号与降低后的签名；声明作者承担其明确写出的非留存、读写权限及重入等行为承诺。未知行为无需先补全效果注解才能调用。
声明没有覆盖的函数不自动获得整个包的 safe 身份，也不因此被标为 unsafe；D11 的显式 AsPtr 操作及真实类型桥接另行检查。

`Ptr[T]` 可空，不自带只读、独占或期限保证；单层转换保持地址及 nil。
`AsPtr` 暂定通过关联类型 `T` 和 `unsafe func as_ptr() Ptr[Self.T]` 表达后端指针导出。
`unsafe <expr>` 授权该表达式求值中的相关操作，不关闭类型检查，也不覆盖新闭包的函数体。
它不会在表达式结束时撤销外部已保留的指针。
unsafe callable 的赋值、返回、参数传递和泛型使用均应保留其效果。

## 转译示例

以下假定原生包 `native` 已有 `type T struct{ Value int32 }` 和 `func Fill(*T)`。
`native::` 是 goxide 中该包的示例限定名；Go 片段已导入 `native`。

**F01：declare 采用原函数。** 不生成第二份 Fill 实现。

```goxide
declare func native::Fill(p &mut native::T);

var value native::T
native::Fill(&mut value)
```

```go
var value native.T
native.Fill(&value)
```

约束：修改原 `value`。假定声明承诺短期不留存借用；签名匹配不证明这个行为承诺。
成本是原函数调用及所选边界检查，不要求 wrapper 分配。

**F02：raw 调用与指针导出。** 此例没有 F01 的声明，`p` 是现有写借用。

```goxide
// p: &mut native::T
unsafe native::Fill(p.as_ptr());
```

```go
// 支持库候选定义：type Ptr[T any] *T
// p: *native.T
raw := Ptr[native.T](p)
native.Fill((*native.T)(raw))
```

单层转换不复制 T，不需要 `unsafe.Pointer`；可以消去恒等转换。
此处 unsafe 授权的是 D11 的显式 as_ptr 操作，不是因为 Fill 缺少声明。显式 unsafe 效果在 Go 中擦除，但保留在 goxide 声明元数据中。

**F03：普通函数值桥接。** 此例在 raw 视图取函数值；`raw` 已有 `Ptr[native.T]` 类型。Fill 没有被声明为 unsafe，不能仅因效果未知给调用增加 unsafe。

```goxide
f := native::Fill
f(raw);
```

```go
// 本片段选择 Ptr 出现在 goxide 函数值的 Go 参数中这一候选 ABI。
f := func(p Ptr[native.T]) {
    native.Fill((*native.T)(p))
}
f(raw)
```

`func(Ptr[T])` 与 `func(*T)` 不能靠直接赋值互换；该 ABI 下需要桥接。
选择保留原生函数值 ABI 的后端也可插入调用点转换，完整策略未定。
不得宣称所有函数值桥接均零成本；桥接调用和闭包是否消除由 Go 优化决定。

**F04：取得函数值不能清除显式 unsafe 效果。** 此例另设已显式声明为 unsafe callable 的 `dangerous`，候选签名为 `unsafe func(Ptr[native::T])`，对应 Go 函数 `dangerous(*native.T)`；并非把普通 native::Fill 因缺摘要视作 unsafe。签名拼写尚待冻结，效果丢失应由 goxide 前端拒绝。

```goxide
f := unsafe dangerous
f(raw) // 拒绝：f 仍是 unsafe callable
```

```go
// 禁止据此放行：Go 的函数类型不携带 goxide unsafe 效果。
f := dangerous
f((*native.T)(raw))
```

**F05：声明的指针层级不兼容。** `native.Fill` 的真实参数是 `*native.T`。

```goxide
declare func native::Fill(p native::T); // 拒绝：声明降低后参数为值
```

```go
// 禁止生成：验签应先报告声明与 func(*native.T) 不兼容。
var value native.T
native.Fill(value)
```

## 拒绝与边界

- Ptr 判空只证明非空；不能凭此安全恢复任意 `&T` / `&mut T`。
- `[]*T`、`**T`、含指针结构体和回调不得机械替换成含具名 Ptr 的不同布局。
- `Option[Ptr[T]]` 必须区分 None 与 Some(nil)，不能直接套用可空 Arc 优化。
- Go 不允许给具名指针接收者类型 `type Ptr[T any] *T` 定义方法；AsPtr 方法应使用 wrapper 或直接降低为转换。
- 相同布局不豁免真实类型和读写资格检查；unsafe 导出指针后，未知外部行为仍未验证。不逃逸与别名风险分析按 D20 长期独立，不要求准入证明。
- 原生包的 `init` 遵循 Go 规则；调用处的 unsafe 标记不构成初始化沙箱。

## 双向互操作

Go 模块可以同时携带 `.go`、`.d.gox` 和 `.gox`；Go 编译 `.go`，goxide 额外读取声明或源码。
声明也可来自调用方 `.gox` 或独立声明包。兼容声明可合并，矛盾契约须诊断；选择不依赖扫描顺序。
goxide 导出的 unsafe API 在普通 Go 中仍是普通函数；Go 调用者不会获得 goxide 的效果检查。
改变实际行为需要真实包装，例如复制存储、约束回调或同步；声明本身不能把留存指针变成不留存。
`declare impl` 的方法采用及 marker 问题另见 [RFC 0003](0003-declare-impl.md)。

## 验收与未定项

1. F01–F03 验证原地址、原地修改与函数值可调用；F04–F05 必须在输出 Go 前诊断。
2. 对 Ptr 的 nil、双向单层转换和 AsPtr wrapper 分别验证，不将其推广成递归 ABI 证明。
3. 增加闭包体 unsafe 作用域、safe 回调位置拒绝显式 unsafe 函数值及元数据往返测试；另验收无摘要普通函数/函数值不自动 unsafe。
4. 声明需要记录行为契约；签名未变化的依赖升级仍可能需要重审留存与重入行为。
5. 最终 raw 操作分类、Ptr 恢复规则、回调 ABI、版本及构建条件匹配仍待定稿。

当前完整桥接与效果规则仍待实现；已有单层 Ptr Go 实验不代表 goxide 效果检查已完成，来源风险分析另属 D20。

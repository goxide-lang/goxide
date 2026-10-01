# RFC 0007：锁句柄、作用域回调与 Unit

## 状态与范围

- 已确认：Lock / RWLock 以指针句柄保存锁和数据；Do 的结果类型独立于数据类型；项目目标最低 Go 1.27。
- 暂定：Read / Write 的具体权限签名、Unit 降低及第三方锁适配；不逃逸证明不属 MVP。
- 未定：完整别名分析、外部共享字段的接入规则、第三方构造器；锁块语法延期。
- 示例使用拟议普通 Go 支持库 `hgosync`，不是 Go 标准库现有包。

D19/D20 边界：读写权限和已确认类型不变量照常检查；别名、留存、独占与重入风险分析独立长期推进，不属 MVP。未来仅有具体风险证据才 warning；未知不默认拒绝或告警。

设计背景：[锁与作用域回调](../design/synchronization.md)；权限依据 [RFC 0005](0005-borrows.md)。

## 源级规则

| hgo 类型 / 操作 | Go 表示 / 操作 | 候选约束 |
| --- | --- | --- |
| `Lock[T]` | `*hgosync.Lock[T]` | 复制句柄共享同一锁与数据 |
| `RWLock[T]` | `*hgosync.RWLock[T]` | 读写操作使用同一同步协议 |
| `do` / `write` | 持写锁调用 `func(*T) P` | 回调得到 `mut& T` |
| `read` | 持读锁调用 `func(*T) P` | 回调得到 `&T` |
| 无结果的回调 | 返回 `struct{}` 的回调 | Unit 精确名称与统一布局暂定 |

回调结果在持锁期间求值，随后解锁；panic 时解锁并继续传播 panic。
结果类型 P 独立于数据类型 T；返回其中引用或派生别名不因逃逸本身被拒绝，仍须保留读写资格。解锁后使用该别名不再受到这次锁操作的保护。
复制 Lock 句柄不能复制 mutex。T 的浅复制仍可能保留外部别名，不能因此宣称整个对象图已经独占。
同步依赖真实锁实现；hgo 前端检查读写资格，未来的逃逸风险 warning 另行研究，不能以任一层宣称全图无竞争。

## 转译示例

**L01：独立返回类型。** hgo 函数体片段对应已导入 `hgosync` 的 Go 函数体。

```hgo
var s Lock[i32] = Lock.new(0)
next := s.do(|inner| {
    *inner += 1
    return *inner
})
label := s.do(|inner| "ready")
```

```go
s := hgosync.NewLock[int32](0)
next := s.Do(func(inner *int32) int32 {
    *inner += 1
    return *inner
})
label := s.Do(func(inner *int32) string { return "ready" })
```

片段中的 `next` / `label` 由后续代码使用。P 分别推导为 int32 和 string，不装箱到 `any`。
拟议支持库实现如下；使用项目设定的 Go 1.27 泛型方法目标。

```go
package hgosync

import "sync"

type Lock[T any] struct {
    mu    sync.Mutex
    value T
}
func NewLock[T any](value T) *Lock[T] {
    return &Lock[T]{value: value}
}
func (s *Lock[T]) Do[P any](f func(*T) P) P {
    s.mu.Lock()
    defer s.mu.Unlock()
    return f(&s.value)
}
```

成本包括锁操作、回调调用和 Go 决定的对象分配；不增加所有权运行时。

**L02：Unit 回调。** `s` 沿用 L01 的类型，回调没有返回数据。

```hgo
s.do(|inner| {
    *inner += 1
})
```

```go
s.Do(func(inner *int32) struct{} {
    *inner += 1
    return struct{}{}
})
```

约束：转换器补全 Unit 返回；Go 不会自动把 `func(*T)` 适配成 `func(*T) struct{}`。
回调中的 return 只退出回调，不退出外层函数；不得借此实现尚未设计的锁块控制流。

**L03：返回锁内引用允许，但不承诺锁外访问安全。** 旧“逃出即拒绝”候选已被 D19/D20 覆盖。

```hgo
leaked := s.do(|inner| inner) // 允许保存；资格不变，锁已在回调结束后释放
```

```go
// 普通 GC 引用仍有效；解锁后通过 leaked 访问不自动受到锁保护。
leaked := s.Do(func(inner *int32) *int32 { return inner })
```

未来若存在具体的锁外冲突访问链，可报告风险 warning；单凭返回、保存或未知调用者不默认拒绝或刷警告。该分析不是 MVP 条件。

**L04：Read 不得写入。** 假定 `r` 为 `RWLock[i32]`，Go 中为 `*hgosync.RWLock[int32]`。

```hgo
r.read(|inner| {
    *inner = 1 // 拒绝：只有读借用
})
```

```go
// 禁止生成：Read 的 Go 签名本身不能阻止此写入。
r.Read(func(inner *int32) struct{} {
    *inner = 1
    return struct{}{}
})
```

RWLock 的支持库实现使用 `sync.RWMutex`：Read 持 RLock / RUnlock，Write 持 Lock / Unlock；具体完整 API 待定稿。

## 拒绝与边界

- 返回闭包、slice、trait wrapper 或容器同样须保留访问资格；不能把“保留资格”误作禁止所有引用离开回调。
- 向外部存储或 goroutine 传递引用仍按普通引用规则处理；未来有具体同步风险依据才 warning，不把来源分析设为准入条件。
- 原生 Go 仍可复制支持库结构体或修改 Read 参数；这些操作不受 hgo 前端保护。
- 对同一非重入锁重入可能死锁；Go 回调及第三方锁契约不能仅靠类型签名证明。
- Unit 返回插入不得吞掉 panic、改变 return 所属函数或提前解锁。

## 双向互操作

hgo 调用支持库时读取 `.d.hgo` 中的回调权限及已声明访问信息；Go 编译其中普通 `.go` 实现。
Go 调用 Do / Read / Write 使用普通函数值，不获得 hgo 静态只读检查；双方都不能仅凭回调签名宣称不逃逸或无竞争。
第三方锁暂定通过非泛型 Lock / Unlock、RLock / RUnlock 原语接入；泛型 Do 保留在具体包装类型上。
不把 `Do[P]` 放进 Go interface；项目 Go 1.27 目标仍需遵守接口方法的类型参数限制。

## 验收与未定项

1. L01–L03 验证多种 P、Unit、共享状态及返回引用的 GC 存活；L04 的只读写入在转译前拒绝。
2. 支持库验证正常返回、panic 后解锁、读写同步以及受保护存储的原地更新。
3. `go test -race` 提供动态竞争证据；长期别名/逃逸风险 warning 独立验收，不要求 MVP 先完成。
4. Unit 的通用类型规则、共享字段初始化契约、别名检查和第三方注入 API 尚未定稿。

仓库已有有限前端；本篇完整锁 API 仍未实现。Go 支持库机制验证不能代表源级回调权限或风险分析已实现。

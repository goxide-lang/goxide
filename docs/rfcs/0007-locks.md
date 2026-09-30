# RFC 0007：锁句柄、作用域回调与 Unit

## 状态与范围

- 已确认：Lock / RWLock 以指针句柄保存锁和数据；Do 的结果类型独立于数据类型；项目目标最低 Go 1.27。
- 暂定：Read / Write 的借用权限、不逃逸检查、Unit 降低及第三方锁适配。
- 未定：完整别名分析、外部共享字段的接入规则、第三方构造器；锁块语法延期。
- 示例使用拟议普通 Go 支持库 `hgosync`，不是 Go 标准库现有包。

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
结果类型 P 独立于数据类型 T，但不能含本次回调的借用及派生别名。
复制 Lock 句柄不能复制 mutex。T 的浅复制仍可能保留外部别名，不能因此宣称整个对象图已经独占。
同步依赖真实锁实现；hgo 前端检查读写权限和逃逸，二者不能互相替代。

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

**L03：借用逃出锁应被拒绝。** 这个候选错误不能留给 Go 编译器发现。

```hgo
leaked := s.do(|inner| inner) // 拒绝：结果携带本次写借用
```

```go
// 禁止生成：解锁后仍可经 leaked 访问受保护存储。
leaked := s.Do(func(inner *int32) *int32 { return inner })
```

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

- 返回闭包、slice、trait wrapper 或其他容器不能绕过 L03；需追踪派生别名。
- 回调向外部存储或 goroutine 传递借用，同样进入逃逸检查范围。
- 原生 Go 仍可复制支持库结构体或修改 Read 参数；这些操作不受 hgo 前端保护。
- 对同一非重入锁重入可能死锁；Go 回调及第三方锁契约不能仅靠类型签名证明。
- Unit 返回插入不得吞掉 panic、改变 return 所属函数或提前解锁。

## 双向互操作

hgo 调用支持库时读取 `.d.hgo` 中的回调权限及作用域信息；Go 编译其中普通 `.go` 实现。
Go 调用 Do / Read / Write 使用普通函数值，不获得 hgo 静态借用检查。
第三方锁暂定通过非泛型 Lock / Unlock、RLock / RUnlock 原语接入；泛型 Do 保留在具体包装类型上。
不把 `Do[P]` 放进 Go interface；项目 Go 1.27 目标仍需遵守接口方法的类型参数限制。

## 验收与未定项

1. L01–L02 验证多种 P、Unit、复制句柄共享状态；L03–L04 在转译前拒绝。
2. 支持库验证正常返回、panic 后解锁、读写同步以及受保护存储的原地更新。
3. `go test -race` 提供动态竞争证据；借用诊断另行验收。
4. Unit 的通用类型规则、共享字段初始化契约、别名检查和第三方注入 API 尚未定稿。

当前无 hgo 转译器；Go 支持库机制验证不能代表源级回调权限已实现。

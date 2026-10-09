# 锁与作用域回调

交付边界（D20）：本文涉及的别名/借用风险 warning 是未来独立长期分析，不属于 MVP 验收；当前语言语法、类型、明确操作与真实 Go 行为照常设计。不得为未实现分析器强制复杂效果注解。

状态：锁句柄采用 Go 指针表示，`Do` 支持独立泛型返回类型，最低 Go 版本为 1.27。下文的 Unit 形式、引用诊断和第三方适配接口属于设计草案；仓库尚无 goxide 转译器或 linter 实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D09：锁为指针句柄，Do 有独立返回类型参数，最低 Go 1.27。 |
| 暂定 | T06、T16：Read / Write 类型权限、风险 warning、Unit、第三方锁原语适配。 |
| 未定 | U02、U03：共享别名与引用检查算法；锁块语法延期。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 goxide 功能已实现。

[RFC 0001](../rfcs/research/0001-scoped-borrow-views.md)按 D19 采用风险 warning：锁内来源离开、绕锁访问、已知重入分别诊断，默认不阻断。旧严格准入方案已被用户否决。

## 1. 转译边界

goxide 负责源级语义检查和代码转换；同步由普通 Go 支持库完成，不引入另一套所有权运行时。

| goxide 概念 | Go 表示 |
| --- | --- |
| `Arc[T]` | `*T`，共享引用句柄 |
| `&T`、`&mut T` | `*T`；源级权限差异保留在转译前检查中 |
| `Lock[T]` | `*goxidesync.Lock[T]` |
| `RWLock[T]` | `*goxidesync.RWLock[T]` |

`goxidesync` 是拟议的普通 Go 支持库包名；标准库提供 `sync.Mutex` / `sync.RWMutex`，没有现成的泛型 `sync.Lock[T]`。

在 goxide 中复制 `Lock[T]` 只复制句柄，锁和受保护数据保持在同一对象里。不能转译成会按值复制 mutex 的外层结构体。原生 Go 手动解引用后复制整个支持库对象，属于 goxide 检查之外的互操作行为。

## 2. 泛型返回类型

```goxide
var s Lock[i32] = Lock.new(0)

next := s.do(|inner| {
    *inner += 1
    return *inner
})

label := s.do(|inner| format(*inner))
```

T 是锁保存的数据类型；P 是每次调用回调所产生的结果类型。两者独立，同一把锁可以先返回整数，再返回字符串，无需把结果装入 `any`。

Go 方法的完整签名是：

```go
func (s *Lock[T]) Do[P any](f func(*T) P) P
```

其中 `P any` 的约束不可省略。调用时可从回调类型推导 P，也可以显式指定。Go 1.27 新增了方法自身的类型参数，因此本设计要求工具链以及支持库、生成模块的语言版本至少为 1.27。[Go 1.27 发布说明](https://go.dev/doc/go1.27#language)

## 3. 支持库示意

以下是完整 Go 示例，所有构造入口返回指针：

```go
package goxidesync

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

type RWLock[T any] struct {
    mu    sync.RWMutex
    value T
}

func NewRWLock[T any](value T) *RWLock[T] {
    return &RWLock[T]{value: value}
}

func (s *RWLock[T]) Read[P any](f func(*T) P) P {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return f(&s.value)
}

func (s *RWLock[T]) Write[P any](f func(*T) P) P {
    s.mu.Lock()
    defer s.mu.Unlock()
    return f(&s.value)
}
```

返回值在持锁期间计算，随后 defer 解锁，再把结果交给调用者。回调 panic 时执行解锁，panic 继续传播，不伪造一个 P 的零值返回。

goxide 将 `Read` 的回调参数视为 `&T`，`Do` / `Write` 的回调参数视为 `&mut T`。上述 Go 签名均为 `*T`，本身不能表达只读、独占或不逃逸；源级只读类型错误仍由前端检查，独占/逃逸按 D19 仅对具体风险 warning。直接由 Go 调用 Read 并修改数据，不受 goxide 类型检查约束。

## 4. 没有结果的回调

对只执行副作用的 goxide 回调，建议使用 Unit 作为结果类型，降低为 `struct{}`，并补上 return：

```go
// 片段：s 为 *Lock[int32]。
s.Do(func(inner *int32) struct{} {
    *inner += 1
    return struct{}{}
})
```

Go 不会自动把 `func(*T)` 转成 `func(*T) struct{}`；这一补充由 goxide 转译完成。Unit 的源级名称和统一布局仍需与语言基础类型设计一并定稿。

回调中的 return 只退出回调，不退出调用 Do 的外层函数。若未来加入锁块语法，需要另行处理外层 return、break、continue 等控制流；不能只在闭括号后附加 Unlock。Go defer 以函数退出为作用点。[Go defer 规则](https://go.dev/ref/spec#Defer_statements)

## 5. linter 的检查范围

P 可以包含指向本次数据的引用；默认允许返回，未来 W01 仅提示有证据的协议违约，单纯返回不报。Go 类型系统允许回调返回内部指针、捕获该指针的闭包或共享底层数组的 slice，因而单凭此 API 签名不能宣称已经阻止借用逃逸。

长期分析可检查回调对借用及其派生别名的返回、外部存储、逃逸捕获和 goroutine 传递，MVP 不作此要求；允许返回不依赖该借用的结果，例如 i32 的副本。Read 还需要检查受保护存储上的写操作。

首版检查规则与不能证明的边界必须明确，不以完整跨包所有权证明为前提。Go 原生调用和既有共享别名不能被自动当成安全；`-race` 是运行时检查的补充，不替代这些源级诊断。

构造函数对 T 的复制仍遵循 Go 语义：T 的 slice、map、Arc 字段可能保留外部别名。锁绑定自己的存储，不自动获得整个可达对象图的独占权；相关初始化、访问与结果复制规则仍需由引用专题细化。

借用封装为 trait、闭包、error 或容器后仍可追踪来源；已知后续使用违反协议才给 W01，已知重入给 W04，但不阻断转译。未知效果本身不告警；源级只读类型直接写仍是类型错误。race 的动态冲突报告与这些静态 warning 分开。

## 6. 第三方锁适配

第三方实现接入非泛型锁原语：互斥锁提供 Lock/Unlock，读写锁另外提供 RLock/RUnlock。泛型 Do/Read/Write 留在具体的 `Lock[T]` / `RWLock[T]` 包装类型上。

Go 1.27 的接口方法仍不能声明自己的类型参数，因此不将 `Do[P]` 直接写进 Go Locker 接口。这个限制不影响通过普通锁原语适配第三方实现。[Go 1.27 接口限制](https://go.dev/doc/go1.27#language)

接口签名只能证明可调用；真正互斥以及所有相关访问遵循同一同步协议，仍是底层锁的行为契约。上面的完整示例使用标准库锁，第三方注入构造入口的最终拼写待定。

## 7. 验证要求

支持库的 Go 验证应覆盖：同一锁的多种 P 推导、显式 P、Unit 回调、正常返回、panic 后解锁、复制句柄共享状态，以及 RWLock 的读写路径。

`go test -race` 用于检查实际运行中的并发访问。goxide error/warning/无依据不报要在转译器和 linter 实现后分别验证，不能因为 Go 示例通过就标记完成。

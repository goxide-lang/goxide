# enum 表示与 match 降低

状态：已确认（2026-10-13）。本文是新前端的 enum 规范，取代 `enums-and-match.md` 中与本文冲突的表示与降低章节；`frontend-ast.md` §5.1 只定义语法节点。

## 1. 表示

用户声明的 enum 降低为一个**密封接口**加每个变体一个结构体：

```goxide
enum EE { AA, BB(Inner), CC{k1 int, k2 int} }
```

```go
type EE interface{ isEE() }

type AA struct{}
func (*AA) isEE() {}

type BB struct{ Value Inner }
func (*BB) isEE() {}

type CC struct{ K1, K2 int }
func (*CC) isEE() {}
```

- 变体降级为**指针类型**使用：`&BB{...}`。接口方法定义在指针接收者上。
- 标记方法名统一加 `is` 前缀加 enum 名（`isEE`）。加前缀是因为小写变体名会与结构体名相同（`enum ee { aa }` → `struct aa` 与 `func (*aa) aa()`，Go 报 field/method 同名）。
- 标记方法**未导出**，因此只有声明包能实现该接口。Go 在跨包、跨 module 下都强制这一点，实测确认（见 §7）。这就是"闭合"的实现方式。
- 命名：变体结构体名 = 变体名的 Go 名（按 `frontend-ast.md` §6.1 的可见性映射）。
- 空变体是零字段结构体；带载荷变体的字段按源码顺序命名（元组变体用 `Value`，多载荷用 `V0`、`V1`；结构变体用字段名转成的 Go 名）。
- 不生成 tag 常量、不生成 `Validate*`、不生成 `Tag*` / `Value*` / `Project*` 辅助函数。旧管线为元数据与跨包 ABI 生成的这些入口在新管线里都不存在。

### 1.1 P4 的类型锚与实例适配

trait 成员查找以源级 enum E 为目标，与 struct 使用同一规则；`Self` 始终为 E，不是某个变体类型。静态协议需要时，E 接口增加 `GoxideInternalTypeCarrierForSelf(E) EOps` 类型锚签名，各 `*Variant` 实现返回同一个零字段 EOps。`std/typeinfo.Of[E]()` 仅取得该 carrier 的零值；类型锚和关联类型 witness 只用于 Go 类型推导，不运行，不构造 enum 值，也不读取旧 enum 元数据。

carrier 的实例操作使用普通参数：按值接收 E，借用接收 `*E`，源级检查分别保存 `Self` / `&Self` / `&mut Self` 的资格。因此操作 enum owner 不需要为 `*E` 增加类型锚。

运行时 trait 的业务体只在对应 wrapper 上。值视图的 Go 便利入口可以在 E 接口声明 `AsNamed() Named`，由每个变体用一行方法转发同一个固定工厂，保留 `e.AsNamed().Name()`；不为 E 声明非法的 Go 接收者方法，也不把业务实现复制到变体。借用视图须保存真实枚举槽位，使用 `ForE(&e).AsNamed()` 或固定工厂；变体方法不能把自己临时装入 E 再取址，冒充原 owner。外部包拥有的适配同样使用 facade。

值视图保存的 E 副本继续共享原变体对象；引用视图保存 `*E`，可观察 owner 的整体替换。旧 payload 投影仍遵循 [P3 引用合同](enum-payload-borrows.md)，不会随 owner 替换而改指向。nil 规则仍由本规范规定，不因 trait 适配新增统一非空检查。

## 2. nil 策略

**nil 不是合法 enum 值；检查发生在实际分派处。**

1. **只检查正在分派的值。** 每次 enum 分派先检查当前值；nil 接口或装有 nil 变体指针的接口立即 panic，消息包含当前 enum 名。`match` 不递归检查载荷，不恢复 `Validate*` 入口，用户写的 `_` 分支也不兜 nil。
2. **嵌套模式也是一次分派。** `Outer::Wrap(Inner::A)` 先匹配外层变体，再分派其 `Inner` 载荷。内层值为 nil 时，在 guard 求值前 panic，消息包含内层 enum 名 `Inner`。
3. **只绑定不检查。** `Outer::Wrap(p)` 只绑定载荷；即使 `p` 为 nil，外层 arm 仍可正常执行。之后对 `p` 执行 `match` 时才检查并 panic。
4. **源码里的 nil 字面量在编译期拒绝。** Goxide 源码将 `nil` 字面量放在 enum 类型的赋值位置（如 `Outer::Wrap(nil)`、带类型的变量、赋值、返回、调用参数、复合字面量或显式转换）时，`check` 报 `enum-nil-literal`，诊断范围精确覆盖 `nil`。这条规则不禁止 nil 比较，也不禁止普通指针、`error` 或指针类型的 `Option` 载荷为 nil。它只检查字面量，不证明所有 enum 值非空；无初始化变量的零值、Go 调用方传入或返回的值仍在实际分派时检查。

每次分派的生成代码先检查接口 nil，并在选中具体变体后检查变体指针 nil。例如：

```go
if v == nil { panic("goxide: match on nil EE") }
```

`_` 只表示“其余变体”，不改变 nil 的行为。穷尽性由 `check` 保证（§4），不依赖 Go 编译器。检查点放在实际使用处，既保留嵌套解构在 guard 前失败的顺序，也避免每次外层 `match` 都遍历整个载荷。

## 3. match 降低

### 3.1 一次分派

一个 `match` 只做一次分派。外层按变体分支，同一变体内的多条 arm 在**内层按源码顺序**成为 `if / else if / else`：

```goxide
match r {
    Result::Ok(0)  => zero,
    Result::Ok(_)  => ok,
    Result::Err(e) => err(e),
    _              => other,
}
```

```go
if r == nil { panic("goxide: match on nil Result") }
switch v := r.(type) {
case *Ok[T, E]:
    if v.Value == 0 { /* zero */
    } else { /* ok */ }
case *Err[T, E]:
    e := v.Value
    /* err(e) */
default:
    /* other */
}
```

- arm 顺序只在变体内有意义。跨变体的 arm 顺序由外层 `case` 的排列决定，不影响结果。
- 不再生成"先取标签、再分支、再分别投影载荷"的多段代码。

### 3.2 arm 形状

| arm | 含义 | 生成 |
| --- | --- | --- |
| `E::V` | 该变体，不关心载荷 | `case *V:` |
| `E::V(p)` | 绑定载荷 | `case *V:` + 局部绑定 |
| `E::V(0)` | 载荷等于常量 | `case *V:` + 内层 `if v.Value == 0` |
| `E::V(p) if g` | guard | 内层 `if <条件>` |
| `E::V{p: 1, ..}` | 结构变体字段匹配 | 内层字段比较 |
| `_` | 其余变体 | `default:` |

- 常量载荷要求类型可比较；切片、map、函数等不可比较类型报诊断，提示改用绑定加 guard。
- guard 的求值顺序按源码顺序，短路、`defer`、`?` 不与普通 Go 语义不同。
- 绑定模式（`E::V(p)`）不要求可比较。

### 3.3 表达式的值

`match` 作为表达式时，各 arm 的值类型必须一致，由 `check` 校验。终结语句位置（各 arm 都终止）不产生值，也不需要补 `return`。

## 4. check 的职责

新增 `internal/check`，本阶段负责：

1. **穷尽性**：所有变体都被覆盖，或存在 `_`。缺哪个变体就报哪个。
2. **不可达 arm**：同一变体内，前一条 arm 已经覆盖后一条时（如 `V(_)` 在 `V(0)` 之前）报错。
3. **常量载荷可比较**：不可比较类型报错并提示 guard 写法。
4. **arm 类型一致**：表达式位置各 arm 的值类型一致。
5. **`?` 的返回类型**：外层函数返回值必须能容纳被传播的错误类型。
6. **泛型实参约束**：实例化满足声明处的约束。

nil **不**参与穷尽性判断（它一律 panic），也不因为出现 `_` 而被视为已覆盖。

## 5. 泛型

- 变体结构体带类型参数：`type Ok[T, E any] struct{ Value T }`，标记方法同样带参数。
- 类型 switch 断言的是**实例化后的**类型：`case *Ok[T, E]`，其中 `T`、`E` 取自被匹配值的类型。
- 同一个泛型 enum 的多个实例互不影响：`Option[int]` 与 `Option[string]` 各自有自己的变体类型实例。
- 不做"泛型 enum 退化为无参数类型"的特化。

## 6. Option 与 Result

本阶段 Option / Result 沿用 `std/sum` **现有形状**（带 tag 的结构体，变体各有独立内联槽），原因见 §6.2。

### 6.1 match 规则相同，分派对象不同

match 的规则（单次分派、同变体 arm 按源码顺序、内层 if 链、nil 语义）对两类 enum 完全一致，只有分派方式不同：

| 类型 | 分派 | 载荷读取 |
| --- | --- | --- |
| 用户声明的 enum | `switch v := x.(type)` | `v.Value`（变体结构体字段） |
| `Option` / `Result` | `switch sum.Tag(x)` | `sum.ValueSome(x)` 等既有取值函数 |

### 6.2 本阶段不改 std/sum 的原因

`std/sum` 当前保留 tagged struct 表示及 `Tag` / `Value` / `Project` / `Validate` API。表示统一在 [MVP-6](../pre-mvp-plan.md#后续阶段mvp) 与 safe std 一起设计和验收：`Some[T]`、`Err[T, E]` 等变体必须携带 enum 的全部类型参数，且变更会调整公开 Go API。

在该阶段完成前，编译器继续使用上述两类分派；P6 删除旧管线及 P6.5a 的 `gen` 入口均不改变 `std/sum` 的公开表示。

### 6.3 `Option[T]` 的指针特化已取消

曾考虑把 `Option[T]` 特化为 `*T`（空指针表示 None）以省分配。该方案在 `T` 本身可空时无法区分 `None` 与 `Some(nil)`，需要额外的别名变体与专门读法。**本阶段不做该特化**，`Option` 就是普通的泛型 enum。将来若要优化，那属于表示特化，不改变源级语义与 match 规则。

## 7. 实测依据

Go 1.27.1 / darwin-arm64，两个独立 module 的探针：

| 验证 | 结果 |
| --- | --- |
| 另一 module 对 `*BB` / `*CC` 做类型断言 | 编译通过，断言成立 |
| 外部包实现 `EE`（同 module 与跨 module 各一次） | 编译失败：`cannot use (*Fake)(nil) ... unexported method isEE` |
| `case nil` 分支 | 合法 |
| `v == nil` 判断 typed-nil 载荷 | 合法 |

结论：闭合性与变体身份由 Go 类型系统提供，**不需要旧 enum 元数据**。新管线不产生或读取 `internal/frontend/enum_artifact.go` 的 enum metadata；其删除排在 P6。P4 的 `std/typeinfo` 空 carrier 服务静态 trait 调用和泛型类型推导，与 enum 的闭合性、变体分派及旧元数据无关。

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

## 2. nil 策略

**nil 接口不是合法值，任何情况下都不是。**

- 用户写的 `_` 分支**不兜 nil**。
- 每个 `match` 的生成代码在分派前插入一次显式检查；命中即 panic，消息含 enum 名与位置：

```go
if v == nil { panic("goxide: match on nil EE") }
```

- `_` 只表示"其余**变体**"，所以它不能省掉穷尽性检查，也不改变 nil 的行为。
- 穷尽性由 `check` 保证（§4），不依赖 Go 编译器。

理由：`_` 兜 nil 会把"Go 调用方传了零值"和"确实落进某个分支"混为一谈，而这两者的处理完全不同。让 nil 一律 panic，语义只有一个。

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

`std/sum` 同时服务旧管线与新管线，而旧管线要到 P6 才删除。若现在把它改成密封接口形状，旧管线的生成代码与验收测试会一并失效，等于提前做 P6 的一部分。

因此分层推进：本阶段只让新管线支持"两类分派"，`std/sum` 的表示统一推迟到 P6（旧管线删除时）。届时 Option / Result 改为与用户 enum 相同的密封接口形状，`match` 的分派方式也随之统一，`TagOption` / `ValueSome` / `ProjectSome` / `ValidateOption` 等入口随之删除。

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

结论：闭合性与变体身份由 Go 类型系统提供，**跨包不需要任何元数据**。旧管线的 enum metadata（`internal/frontend/enum_artifact.go`、`enumMetadataVersion = 9`）与 `std/typeinfo` 的 carrier 机制在新管线里均不产生、不读取；其代码删除排在 P6。

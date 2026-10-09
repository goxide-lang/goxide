# 原生 Go API、安全声明与裸指针

交付边界（D20）：本文涉及的别名/借用风险 warning 是未来独立长期分析，不属于 MVP 验收；当前语言语法、类型、明确操作与真实 Go 行为照常设计。不得为未实现分析器强制复杂效果注解。

状态：D10/D11 的 declare、显式 unsafe 与 Ptr/AsPtr 方向保留。D19 否决“所有未描述 Go callable 自动 unsafe”；普通调用按真实类型允许，具体风险 warning。D31 已纠正普通同型写引用调用必须显式 AsPtr 的旧要求；具体标量参数直接桥接已实现，其他边界仍逐项处理。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D10、D11：普通 declare、raw / 声明视图、unsafe expr、Ptr / AsPtr、可选 .d.gox 和外置声明包。 |
| 暂定 | T01/T06 旧默认已否决；明确 unsafe 属性传播保留，具体 warning 规则暂定。 |
| 未定 | U02、U05、U07：裸指针恢复、递归 Go 视图 / 回调 ABI、版本 / 构建条件和导入选择。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 goxide 功能已实现。

[RFC 0001](../rfcs/research/0001-scoped-borrow-views.md) B04/B07 区分普通原生调用与明确 unsafe 操作；没有效果摘要本身不拒绝、不要求 unsafe、不报告风险。

## 1. 原生视图与声明视图

同一个 Go 包可以提供两种 goxide 视图，不要求真的复制或重命名 Go 包：

| 视图 | goxide 行为 | Go 实现 |
| --- | --- | --- |
| 原生视图，类似 `xx-sys` | 普通函数/方法不自动 unsafe；D31 具体标量 &mut →*T 参数已接通，其余边界继续 U15 | 原始 Go 符号与布局 |
| 声明视图，类似 `xx-lib` | `declare` 提供可信的参数、结果和调用约束；按声明进行普通调用或保留 unsafe 属性 | 原始 Go 符号，必要时增加边界检查；改变行为需要实际包装函数 |

`declare` 本身不加 unsafe。它是作者提供的类型与行为契约，不是执行危险操作。若某个声明后的函数仍需要调用者承担未被类型表达的前提，可以将函数本身声明为 unsafe；该属性与 declare 关键字分开。

旧 T01 的“所有未描述 Go 函数和方法调用统一要求 unsafe”已被用户否决。普通函数、方法及其函数值按真实签名使用；已知具体来源/回调/冲突链才 warning。不因指针、全局或未知行为的存在自动警告，更不把整包标成 safe。

采用声明视图后，未覆盖符号继续可按真实原生类型调用。只有明确 unsafe 的操作保持该属性，不能由缺少声明推断 unsafe。声明文件不把整个包变成已证明安全。

## 2. declare 补充可检查的调用接口

对于原生 Go 函数 `func Fill(p *T)`，可以在普通 `.gox`、包内 `.d.gox` 或外置声明包中写：

```goxide
declare func native::Fill(p &mut native::T);

var value native::T
native::Fill(&mut value)
```

生成的 Go 调用仍是：

```go
// 片段：native 指向原始 Go 包。
var value native.T
native.Fill(&value)
```

声明不生成另一个 Fill 业务实现。编译器检查真实 Go 符号存在，并检查降低后的参数、返回值及调用方式与真实签名兼容；不能随意改变指针层级、布局或将不兼容的函数类型当成等价类型。

`&mut T` 表达写访问，不自动证明独占。声明可另外记录不留存、回调和同步契约来改善诊断；无这些契约仍允许普通 Go 调用。只读类型上的直接写仍不合法，间接别名风险按 D19 warning。

这些外部行为不是编译器仅通过 ABI 校验就能证明的；正确性依赖声明作者。包升级后，即使签名没变，指针留存等行为也可能改变，应重新审核相关契约。源码分析未来可以辅助检查，但不作为首版必需的全程序证明。

借用返回、共享 slice 与回调默认允许；有具体风险链给 warning。需要真实布局转换时仍做实际桥接，不能通过修改声明伪造 ABI 相容；包装/复制是用户可选语义，不因效果未知强制引入。

## 3. Ptr 的表示与转换

源级 `Ptr[T]` 是统一的有类型裸指针，不提供非空、只读、独占、锁保护或借用期限保证。它可表示 nil。可采用透明新类型的形式，例如 `struct Ptr[T](*T)`；该拼写仍为草案，不据此规定所有单字段结构体的降低方式。

Go 支持库表示为：

```go
type Ptr[T any] *T
```

单层对象指针与原生 Go 的边界转换是普通类型转换：

```go
func ToPtr[T any](p *T) Ptr[T] { return Ptr[T](p) }
func ToGo[T any](p Ptr[T]) *T { return (*T)(p) }
```

它保留指针身份和 nil，不复制 T，也不要求 Go 的 unsafe.Pointer 或 uintptr。Go 对部分 `Ptr[T]` 与 `*T` 赋值本来就允许，因此 goxide 的显式转换和 unsafe 规则必须由自己的前端执行，不能依赖输出 Go 的类型检查替代。

原生包内已有的结构体字段、容器和函数签名保留实际 Go 布局。尤其不能把 `[]*T`、`**T` 等机械改成含具名 Ptr 的不同 Go 类型，再假设一切都可无成本赋值；递归视图、原位修改及适配策略须分别设计。当前实验只验证单层对象指针。

`Ptr[T]` 本身允许 nil，所以 `Option[Ptr[T]]` 必须区分 None 和 Some(nil)，不能直接复用 `Option[Arc[T]]` 的可空指针优化。

## 4. AsPtr 与 unsafe 表达式

以下为旧显式导出候选。D31 后，已有具体标量 `&mut T` 调用同型普通 Go `*T` 参数无需 AsPtr/unsafe；参见[实际规则与验收](../rfcs/research/0002-native-pointer-bridge.md)。这不批准 readonly 升级或真正内存重解释。

沿用普通关联类型，语法草案为：

```goxide
trait AsPtr {
    type T
    unsafe func as_ptr() Ptr[Self.T]
}
```

为已定义的 `Arc[T]`、`&T`、`&mut T` 等分别提供显式实现。可空句柄的适配必须明确 None 到 nil 的行为。其他句柄的关联类型应指向其真实后端对象，不能例如将锁句柄的地址自动当成锁内 T 的地址。

在没有 Fill 声明视图的场景：

```goxide
// p: &mut native::T
unsafe native::Fill(p.as_ptr());
```

`unsafe <expr>` 覆盖该表达式直接求值时的 unsafe 调用，包括此处明确要求 unsafe 的 as_ptr；Fill 本身不因无声明自动 unsafe。它不关闭一般类型检查，也不自动授权表达式中新定义的函数或闭包体；那些调用拥有自己的检查上下文。

对应 Go 可以先形成 `Ptr[native.T]`，再以 `(*native.T)(raw)` 调用 native.Fill；已知的恒等转换可以消去。unsafe 关键字及调用效果元数据在生成 Go 时擦除。

Go 不允许以具名指针类型作为接收者基类型。因此不能为 `type Ptr[T any] *T` 直接生成 `func (p Ptr[T]) AsPtr()`。goxide 的 trait 实现沿用既定 wrapper 机制，方法和 marker 定义在 wrapper 上；具体调用也可直接降低为转换。以下只展示 Go 实现机制，unsafe 约束由 goxide 元数据承担。[Go 方法接收者规则](https://go.dev/ref/spec#Method_declarations)

```go
type ptrView[T any] struct{ target Ptr[T] }

func (v ptrView[T]) AsPtr() Ptr[T] { return v.target }
func (v ptrView[T]) GoxideTraitAsPtr() {}
```

unsafe 表达式结束不是指针失效、释放或别名消失的时刻。借用派生的 Ptr 应保留已知来源；传给未知 Go 后不能自动恢复“外部不存在别名”的结论。局部检查、逃逸后的诊断与显式恢复规则仍需引用专题细化；未经证明的外部行为由 unsafe 使用者承担。

从 Ptr 反向构造高层引用是独立能力。判空只能建立非空性，不能证明独占、不逃逸或锁仍被持有；不提供能任意恢复 `&T` / `&mut T` 的安全通用 from_ptr。

## 5. unsafe 随函数值保留

仅明确要求的 unsafe 是 goxide callable 类型或调用效果的一部分，不能只记在原始 Go 符号上：

```goxide
f := explicitlyUnsafeOperation  // 已明确声明为 unsafe，不是因为缺少摘要
unsafe f(raw);
```

赋值、参数传递、返回、trait 方法与泛型调用都保留该属性。`unsafe explicitlyUnsafeOperation` 仅取得函数值，不能将其永久转换为 safe callable；普通函数回调位置也不能静默接收 unsafe callable。

将明确 unsafe 的函数包装成普通函数时，作者仍负责体内显式 unsafe 操作；普通原生函数不是因为缺少摘要就需要此包装。

Go 函数类型本身没有该 unsafe 属性，导出 goxide unsafe API 后，纯 Go 调用方不会受到 goxide 前端约束。取函数值或跨回调边界时，如果具名 Ptr 等导致两侧实际 Go 函数类型不同，应生成合法的桥接函数，不能只靠直接调用时的参数转换。

## 6. 声明分发与选择

声明可以由调用方普通 `.gox` 提供，也可以随原生 Go 包放在 `.d.gox` 中，或由类似 TypeScript `@types/xxx` 的独立声明包提供。`.d.gox` 是可选分发形式，原生 Go 作者无需配合，不要求已有 wrapper。

声明包必须绑定真实 Go import path、适用版本与构建条件。编译器和 LSP 读取同一份明确选定的声明集合；不同来源的冲突声明应诊断或要求显式选择，不按磁盘扫描顺序、导入顺序或“最后一个覆盖”决定语义。声明格式、包命名及依赖锁定机制待包系统设计。

`declare func` 只建立函数视图，通常不修改 Go 依赖源码；`declare impl` 遵守[显式实现适配规则](go-interop.md)，在 impl 包生成转发 wrapper、marker 和固定工厂，不给原生类型追加方法。若直接采用 safe trait 方法，声明作者也须担保原生实现符合该 trait 方法的借用和调用契约；编译器检查签名与声明契约兼容，不把 marker 当作行为证明。这不会顺带将其他原生方法或该方法的独立原生调用视图改为 safe。

类型文件本身没有需要执行的包初始化。原始 Go 包及依赖的 init 仍按 Go 规则运行，逐次调用的 unsafe 标记不覆盖初始化行为；这是引入原生依赖本身的信任边界，不能宣称 sys/lib 分层构成运行沙箱。

## 7. 验证边界

已在 Go 1.27.1 上以临时独立模块验证：具名泛型 Ptr 与原生单层指针的双向转换、nil、原地址与原地修改，以及 wrapper 实现 AsPtr。直接为具名 Ptr 定义方法的负例得到预期的 invalid receiver 错误。

未来 goxide 实现还需验证：普通未声明调用允许的对照、declare 签名校验与具体风险 warning、unsafe expr 范围、函数值传播、声明版本和冲突、Ptr 来源与反向转换，以及函数值/回调的 Go 桥接。上述 Go 实验不代表这些前端规则已经实现。

U15 的历史候选与 D31 后的实际实现范围见 [RFC 0002](../rfcs/research/0002-native-pointer-bridge.md)。普通同型写引用调用已确认，不再等待 D11 例外批准；容器、接口及引用回调不由此自动获批或完成。

写引用核对：goxide `native::Fill(&mut value)` 中 mut 不由自动桥接省略；已有 `p: &mut T` 可直接传 p。Go 输出 `native.Fill(&value)` 只是 Go 无 mut 语法。未知原生 *T 位置不能接受隐式升级的只读 &T；只读视图须有明确可信声明依据，见 [RFC 0002 权限核对](../rfcs/research/0002-native-pointer-bridge.md#11-写引用核对省-asptr不省-mut)。方案 A 的普通同型写引用调用方向已由 D31 确认；具体标量参数已实现，其余候选不能据此视为完成。

边界桥接不负责取得容器内部引用：只有已经通过值取址或容器自身方法/协议实际得到的 `&mut T` 才进入写引用桥接；`&mut Arc[T]` 等容器绑定引用不能自动展平成 `&mut T`。Boxed/Ref/Arc 的未定获取 API 不由 AsPtr 或方案 A 自动补齐。

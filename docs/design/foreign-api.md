# 原生 Go API、安全声明与裸指针

状态：按讨论采用普通 `declare` 补充 hgo 类型视图、原生调用使用 `unsafe`、`Ptr` 与 `AsPtr` 表达裸指针边界的方向。首版统一将未描述的 Go 函数和方法视为 unsafe、声明选择规则及具体语法仍为设计草案。仓库尚无相应转译器或 linter 实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D10、D11：普通 declare、raw / 声明视图、unsafe expr、Ptr / AsPtr、可选 .d.hgo 和外置声明包。 |
| 暂定 | T01、T06：所有未声明 callable 统一 unsafe、属性传播、具体语法与来源检查规则。 |
| 未定 | U02、U05、U07：裸指针恢复、递归 Go 视图 / 回调 ABI、版本 / 构建条件和导入选择。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 hgo 功能已实现。

## 1. 原生视图与声明视图

同一个 Go 包可以提供两种 hgo 视图，不要求真的复制或重命名 Go 包：

| 视图 | hgo 行为 | Go 实现 |
| --- | --- | --- |
| 原生视图，类似 `xx-sys` | 未描述的函数、方法按 unsafe callable 导入；普通对象指针以 `Ptr[T]` 表达 | 原始 Go 符号与布局 |
| 声明视图，类似 `xx-lib` | `declare` 提供可信的参数、结果和调用约束；按声明进行普通调用或保留 unsafe 属性 | 原始 Go 符号，必要时增加边界检查；改变行为需要实际包装函数 |

`declare` 本身不加 unsafe。它是作者提供的类型与行为契约，不是执行危险操作。若某个声明后的函数仍需要调用者承担未被类型表达的前提，可以将函数本身声明为 unsafe；该属性与 declare 关键字分开。

首版建议所有未描述的 Go 函数和方法调用统一要求 unsafe，而不只检查是否存在顶层 `*T`。slice、map、interface、回调、接收者及全局状态同样可能带来别名或外部行为；签名没有 `*T` 不能证明符合 hgo 借用规则。类型名称、常量引用不因此要求 unsafe；裸共享全局的访问、经 Ptr 访问存储也应纳入外部边界，不能只保护调用表达式。具体操作分类另行细化。

采用声明视图后，未覆盖的符号仍保留原生 unsafe 属性。提供一份声明文件不能将整个包自动提升为 safe。原生视图的显式选择方式待包系统设计，不使用隐式全局覆盖。

## 2. declare 补充可检查的调用接口

对于原生 Go 函数 `func Fill(p *T)`，可以在普通 `.hgo`、包内 `.d.hgo` 或外置声明包中写：

```hgo
declare func native::Fill(p mut& native::T);

var value native::T
native::Fill(mut& value)
```

生成的 Go 调用仍是：

```go
// 片段：native 指向原始 Go 包。
var value native.T
native.Fill(&value)
```

声明不生成另一个 Fill 业务实现。编译器检查真实 Go 符号存在，并检查降低后的参数、返回值及调用方式与真实签名兼容；不能随意改变指针层级、布局或将不兼容的函数类型当成等价类型。

`mut& T` 不只是将 `*T` 改一个名字。声明承诺本次借用范围内的合法读写、不让借用及派生引用逃出调用、不经其他别名或重入产生冲突访问。`&T` 还要求不修改借用所保护的存储。调用者仍须能合法创建对应借用，声明不能凭空使一个共享 Arc 变成独占对象。

这些外部行为不是编译器仅通过 ABI 校验就能证明的；正确性依赖声明作者。包升级后，即使签名没变，指针留存等行为也可能改变，应重新审核相关契约。源码分析未来可以辅助检查，但不作为首版必需的全程序证明。

借用返回、共享底层数组的 slice、逃逸回调等复杂关系不能仅靠改写指针参数消除。首版未表达的关系保留 unsafe，或由实际包装函数完成复制、检查、同步等适配，并明确成本。

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

它保留指针身份和 nil，不复制 T，也不要求 Go 的 unsafe.Pointer 或 uintptr。Go 对部分 `Ptr[T]` 与 `*T` 赋值本来就允许，因此 hgo 的显式转换和 unsafe 规则必须由自己的前端执行，不能依赖输出 Go 的类型检查替代。

原生包内已有的结构体字段、容器和函数签名保留实际 Go 布局。尤其不能把 `[]*T`、`**T` 等机械改成含具名 Ptr 的不同 Go 类型，再假设一切都可无成本赋值；递归视图、原位修改及适配策略须分别设计。当前实验只验证单层对象指针。

`Ptr[T]` 本身允许 nil，所以 `Option[Ptr[T]]` 必须区分 None 和 Some(nil)，不能直接复用 `Option[Arc[T]]` 的可空指针优化。

## 4. AsPtr 与 unsafe 表达式

沿用普通关联类型，语法草案为：

```hgo
trait AsPtr {
    type T
    unsafe func as_ptr() Ptr[Self.T]
}
```

为已定义的 `Arc[T]`、`&T`、`mut& T` 等分别提供显式实现。可空句柄的适配必须明确 None 到 nil 的行为。其他句柄的关联类型应指向其真实后端对象，不能例如将锁句柄的地址自动当成锁内 T 的地址。

在没有 Fill 声明视图的场景：

```hgo
// p: mut& native::T
unsafe native::Fill(p.as_ptr());
```

`unsafe <expr>` 覆盖该表达式直接求值时的 unsafe 调用，包括此处的 as_ptr 和 Fill。它不关闭一般类型检查，也不自动授权表达式中新定义的函数或闭包体；那些调用拥有自己的检查上下文。

对应 Go 可以先形成 `Ptr[native.T]`，再以 `(*native.T)(raw)` 调用 native.Fill；已知的恒等转换可以消去。unsafe 关键字及调用效果元数据在生成 Go 时擦除。

Go 不允许以具名指针类型作为接收者基类型。因此不能为 `type Ptr[T any] *T` 直接生成 `func (p Ptr[T]) AsPtr()`。hgo 的 trait 实现沿用既定 wrapper 机制，方法和 marker 定义在 wrapper 上；具体调用也可直接降低为转换。以下只展示 Go 实现机制，unsafe 约束由 hgo 元数据承担。[Go 方法接收者规则](https://go.dev/ref/spec#Method_declarations)

```go
type ptrView[T any] struct{ target Ptr[T] }

func (v ptrView[T]) AsPtr() Ptr[T] { return v.target }
func (v ptrView[T]) HyperGoTraitAsPtr() {}
```

unsafe 表达式结束不是指针失效、释放或别名消失的时刻。借用派生的 Ptr 应保留已知来源；传给未知 Go 后不能自动恢复“外部不存在别名”的结论。局部检查、逃逸后的诊断与显式恢复规则仍需引用专题细化；未经证明的外部行为由 unsafe 使用者承担。

从 Ptr 反向构造高层引用是独立能力。判空只能建立非空性，不能证明独占、不逃逸或锁仍被持有；不提供能任意恢复 `&T` / `mut& T` 的安全通用 from_ptr。

## 5. unsafe 随函数值保留

unsafe 是 hgo callable 类型或调用效果的一部分，不能只记在原始 Go 符号上：

```hgo
f := native::Fill  // 未声明的原生视图，仍为 unsafe callable
unsafe f(raw);
```

赋值、参数传递、返回、trait 方法与泛型调用都保留该属性。`unsafe native::Fill` 仅取得函数值，不能将其永久转换为 safe callable；普通函数回调位置也不能静默接收 unsafe callable。

将 raw 函数包装成普通 hgo 函数时，包装作者负责在体内满足对应约束并使用 unsafe。声明视图与实际包装函数是复用这种责任的两种方式。

Go 函数类型本身没有该 unsafe 属性，导出 hgo unsafe API 后，纯 Go 调用方不会受到 hgo 前端约束。取函数值或跨回调边界时，如果具名 Ptr 等导致两侧实际 Go 函数类型不同，应生成合法的桥接函数，不能只靠直接调用时的参数转换。

## 6. 声明分发与选择

声明可以由调用方普通 `.hgo` 提供，也可以随原生 Go 包放在 `.d.hgo` 中，或由类似 TypeScript `@types/xxx` 的独立声明包提供。`.d.hgo` 是可选分发形式，原生 Go 作者无需配合，不要求已有 wrapper。

声明包必须绑定真实 Go import path、适用版本与构建条件。编译器和 LSP 读取同一份明确选定的声明集合；不同来源的冲突声明应诊断或要求显式选择，不按磁盘扫描顺序、导入顺序或“最后一个覆盖”决定语义。声明格式、包命名及依赖锁定机制待包系统设计。

`declare func` 只建立函数视图，通常不修改 Go 依赖源码；`declare impl` 继续遵守[显式实现适配规则](go-interop.md)，需要时在隔离构建视图中补 marker。若直接采用 safe trait 方法，声明作者也须担保原生实现符合该 trait 方法的借用和调用契约；编译器检查签名与声明契约兼容，不把 marker 当作行为证明。这不会顺带将其他原生方法或该方法的独立原生调用视图改为 safe。

类型文件本身没有需要执行的包初始化。原始 Go 包及依赖的 init 仍按 Go 规则运行，逐次调用的 unsafe 标记不覆盖初始化行为；这是引入原生依赖本身的信任边界，不能宣称 sys/lib 分层构成运行沙箱。

## 7. 验证边界

已在 Go 1.27.1 上以临时独立模块验证：具名泛型 Ptr 与原生单层指针的双向转换、nil、原地址与原地修改，以及 wrapper 实现 AsPtr。直接为具名 Ptr 定义方法的负例得到预期的 invalid receiver 错误。

未来 hgo 实现还需验证：未声明调用诊断、declare 签名校验与借用检查、unsafe expr 范围、函数值传播、声明版本和冲突、Ptr 来源与反向转换，以及函数值/回调的 Go 桥接。上述 Go 实验不代表这些前端规则已经实现。

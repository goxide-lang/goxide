# goxide 基础包与 Go 标准库的安全视图

优先级（D21）：goxide/std 可承担共享/内部可变包装；不是批准全套 Box/Cell/RefCell 或自动线程安全。先服务 T0 的只读/可写访问与真实 Go 调用。

交付边界（D20）：本文涉及的别名/借用风险 warning 是未来独立长期分析，不属于 MVP 验收；当前语言语法、类型、明确操作与真实 Go 行为照常设计。不得为未实现分析器强制复杂效果注解。

状态：**首版设计草案**。本文的 safe 标准库以当前 Go 标准库的 API 和行为为基础，不以另一门语言的标准库作为规范。goxide 仍是转译器、必经的语义 linter 与普通 Go 支持库；本文不引入另一套运行时。目标语言及工具链版本为 Go 1.27，目前只编写设计，没有相应基础包或编译器实现。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D15：围绕现有 Go std 提供 safe 视图，并提供泛型 slice；不是参考 Rust std。 |
| 暂定 | T13：逐 API 优先顺序、ReadOutcome / FileHandle、名义 Slice、复制及 clone_with 等具体入口。 |
| 未定 | U10：包路径、容器 ABI、完整权限 / 克隆协议与首批覆盖范围。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 goxide 功能已实现。

## 1. 基础包、原生视图与安全视图

基础包承担 goxide 自身类型和协议；标准库视图负责把 Go std 的真实契约表达为 goxide 可检查的接口。两者共同提供日常编程入口，但不是把整个 Go std 标记为 safe。

| 逻辑层 | 职责 | 实现方式 |
| --- | --- | --- |
| goxide 基础能力 | Unit、Option、Result、引用与句柄、基础 trait、泛型容器入口 | 前端语义、元数据和普通 Go 支持类型 |
| Go std 原生视图 | 保留真实 Go 类型、函数、方法和行为；普通 callable 不因缺少描述要求 unsafe | 原始 Go 包和符号，裸对象指针用 Ptr 视图 |
| Go std 声明视图 | 为逐个审核的 API 描述参数权限、借用期限、结果及调用效果 | ABI 兼容的普通 declare；需要时生成边界检查 |
| Go std 包装视图 | 调整结果形状、复制数据、限定可变范围、验证不变量或管理同步 | 有业务逻辑的 goxide / Go 包装函数与支持类型 |

本文使用“基础包”“标准 io 视图”“标准 slice 视图”等逻辑名称。最终 module path、包拆分和导入语法尚未确定，不表示仓库已有可导入的 `goxide/std`。声明分发、版本选择和 raw / 声明视图选择沿用[原生 API 规则](foreign-api.md)，不通过目录名或 import 别名自动改变安全属性。

历史文档中的 safe 视图现在仅指有明确类型/行为契约的便利用法，不表示静态生命周期或并发安全。D19 默认允许普通原生调用，不因缺少声明或无法验证契约要求 unsafe。逐 API 审核用于准确描述行为、生成真实桥接及提高 warning 精度；不是调用 Go std 的门票。明确 unsafe 操作仍按 D11，未知边界不会因无警告自动变安全。

Option / Result 的表示与公开检查沿用[枚举设计](enums-and-match.md)；显式 impl、方法采用与 wrapper 沿用[trait 设计](traits.md)。Default、Clone、Debug 及格式化 / print 的基础契约由[宏与内建派生专题](macros.md)统一定义，本篇不另建同名协议。某个 Go 方法叫 Clone 或满足相同方法集，不自动成为这些 trait 的实现。

## 2. 逐 API 审核与 declare 的界限

每个 safe 入口应记录：对应 Go 符号和版本；接收者 / 参数读取或写入哪些存储；是否保留参数、派生别名或回调；返回值与输入是否共享；错误、panic、取消和并发契约。官方文档足以支持部分契约；没有说明的行为不能直接当成“不发生”，必要时检查当前实现或保守包装。

**签名和行为均可保留时使用 declare；需要改变行为时写包装。** 例如 `math.Sqrt(float64) float64` 可以保留真实签名并声明其可调用性；返回 NaN 的行为也保留。把一个多返回值函数改成 Result、把别名 slice 改成独立副本，或者把可空结果提升为非空句柄，都需要实际构造、检查或复制，不是仅删除 declare 关键字就能完成的转换。

声明采用标准接口的方法契约时，也须检查具体实现的可信来源。`io.Reader` 的结构式方法集只证明存在 Read，不证明任意第三方实现遵守“不保留缓冲区”之类的行为规则。`declare impl` 可以明确采用经审核实现的完整契约；marker 本身不提供行为证明。

跨边界时，参数与结果按目标 goxide 类型验证；枚举、非空句柄、回调和含引用字段的容器都不能跳过。unsafe 调用效果随函数值传播，普通回调可接收类型相容的 raw callable；明确 unsafe callable 的属性不擦除。合法 Go 的 nil、typed nil、共享对象和零值仍可能不满足目标 goxide 视图。

## 3. 优先适配的 Go std 能力

### 3.1 math、strings 与 bytes 按具体操作处理

`math.Sqrt` 的负数输入返回 NaN；safe 声明不会把它自动改成 Result。`strings.Repeat` 对负数次数或长度乘积溢出会 panic；若希望返回错误，应增加另一个明确检查参数的包装入口，保留原入口语义。[math.Sqrt](https://pkg.go.dev/math#Sqrt)、[strings.Repeat](https://pkg.go.dev/strings#Repeat)

不修改对象的标量 / 字符串函数适合优先建立精确 declare。含用户回调的函数还需描述回调效果和重入；不能因为同包中有纯计算函数，就为整个 strings 或 math 包开放未经审核的调用。

bytes 中既有复制数据的函数，也有暴露内部存储的 API。`bytes.Buffer.Bytes()` 返回与 buffer 共享的 slice，后续 buffer 修改影响其有效使用；因此 safe 视图应提供限定作用域的借用或显式复制入口。不能直接把这个结果声明为独立可变 Slice，或让它在原 buffer 继续读写时保持不受约束的借用。[bytes.Buffer.Bytes](https://pkg.go.dev/bytes#Buffer.Bytes)

构造方向也有留存行为：`bytes.NewBuffer(buf)` 接管传入存储，官方要求调用方之后不再使用 buf。goxide 没有全局 move，因此不能用“仅本次调用的 mut 借用”声明来冒充这一契约。首版 safe 构造可以先复制输入，付出复制与分配成本；若保留零复制入口，就必须显式承担所有原别名不再冲突使用的契约，缺少证明本身不要求 unsafe，能追踪到违约访问时 warning。[bytes.NewBuffer](https://pkg.go.dev/bytes#NewBuffer)

### 3.2 io 保留数据与错误同时发生的结果

`io.Reader.Read` 可以在 `n > 0` 时同时返回错误，包括 EOF；`(0, nil)` 表示本次没有进展，不能等同于 EOF。Read 可以使用传入 slice 的整个长度作为临时存储，并且契约要求不保留该 slice。因此借用写权限覆盖整个传入范围，不能在调用前只授权最终的前 n 个字节。[io.Reader](https://pkg.go.dev/io#Reader)

原生 `(n, err)` 可以通过 ABI 兼容的声明视图保留。更便利的包装建议返回同时持有两者的 `ReadOutcome`：计数 n 加可选错误状态；调用方始终能先处理前 n 字节，再处理状态。若提供直接返回数据的入口，也必须保留“数据 + 错误”的组合。不能简单把 `Read` 映射成 `Result[Slice[byte], error]`，在 Err 分支丢掉已经读到的数据。其他部分成功的 I/O 入口按各自文档单独处理。

safe Reader 还需要接收者状态与并发约束：普通 io 接口不默认承诺并行调用安全。借用缓冲区不逃逸，只是其中一个契约，不证明 reader 的内部状态或其所有其他别名都得到保护。普通原生 reader 允许直接使用；声称特定包装契约时须记录可信前提，不能通过包装一次 Read 调用证明整个实现守约。[io 包并发说明](https://pkg.go.dev/io#pkg-overview)

Go error 原值的身份与 nil 判断保持 Go 语义；不把 typed nil error 自动改成 nil，也不为显示错误而在边界校验中随意调用未知 Error 方法。若包装层使用 `Option[error]`，需要实际构造其 Some / None，而非把原始 error 接口当成已有的 Option ABI。

标准 fmt 视图同样保留普通输出函数的字节计数和错误；可恢复 I/O 继续通过这些显式结果处理。基础 print / println 便利宏采用的 I/O 失败策略由[宏专题](macros.md)定义，不改变 std io / fmt 的普通函数契约。fmt 还可能调用参数的 Format、Error 或 String 方法，这些隐式回调也须审核，不能把任意 `...any` 格式化入口直接声明为无额外效果的 safe 函数。[fmt.Fprint](https://pkg.go.dev/fmt#Fprint)、[fmt 格式化规则](https://pkg.go.dev/fmt#hdr-Printing)

### 3.3 os.Open 收窄成功结果，不承诺一次关闭

原生 `os.Open` 仍有 `(*os.File, error)` 签名，其 raw 指针视图为 `Ptr[os.File]`。safe 包装可提供 `Result[FileHandle, error]`：错误分支保留原错误；成功分支检查并建立非空句柄。不能把失败路径上的空指针包装成有效 FileHandle，也不能将任意 Ptr 单凭类型转换当成已打开的文件。[os.Open](https://pkg.go.dev/os#Open)

FileHandle 的共享表示与 Go 指针身份一致；复制句柄不会产生另一份文件。没有 move / 线性所有权时，类型本身不能保证 Close 恰好一次。首版保留显式 Close 及其错误，调用者可使用 defer 管理正常作用域；若另设幂等关闭或共享关闭状态，就必须由运行时包装实现并明确改变后的协议，仍不能管住持有原始 `*os.File` 的 Go 别名。

Go 的 File 方法允许并发调用，但 Close 会影响同一文件上的其他使用，重复 Close 返回错误。文件对象的并发契约不替调用者解决所传缓冲区的并发写入，也不证明关闭后操作可成功。[os.File](https://pkg.go.dev/os#File)、[os.File.Close](https://pkg.go.dev/os#File.Close)

### 3.4 context 保持 Go 的协作取消语义

safe context 视图继续使用 Go context 的期限、取消与 Done 信号。Done 可能为 nil；这表示没有可等待的取消信号，不能替换成已关闭 channel。CancelFunc 不等待工作停止，不能把调用 cancel 视为所有借用、goroutine 或资源都已经结束。Done 的关闭也可能晚于 cancel 返回。[context.Context](https://pkg.go.dev/context#Context)、[context.CancelFunc](https://pkg.go.dev/context#CancelFunc)

需要等待任务结束时，必须另有完成通知或同步协议。WithValue 保存的键值及 AfterFunc 等回调可以超出当前调用范围，引用可被装入并留存，有锁协议或具体冲突链时 warning。Context.Value 返回的任意 Go 值也不会因 context 自身可并发使用而成为只读或无别名的数据；这一部分应采用受约束的值视图或保留原生入口。

### 3.5 encoding/json 需要类型与修改策略

`encoding/json.Unmarshal` 会按目标类型修改存储：例如复用已有 slice / map、把 JSON null 写成某些 Go 类型的 nil，并可能调用用户定义的解码方法。类型不匹配等错误不保证目标完全没有变化。这些行为必须进入安全视图设计，不能只把 `any` 改成 `&mut T` 就宣布完成。[encoding/json.Unmarshal](https://pkg.go.dev/encoding/json#Unmarshal)

首版不为任意 T 提供一行 declare 的全能 safe Unmarshal。可以先支持明确审核的目标类型与解码协议，或提供“解码到独立临时对象、验证、成功后发布结果”的实际包装。临时对象必须有独立的可写存储；把旧对象浅复制过去仍可能通过 slice、map、指针字段修改原对象。用户解码方法的回调效果、引用留存和失败副作用也需采用可信契约，返回错误不自动回滚它们。

Option / Result 的 Go interface 表示没有自动的 JSON 编码协议。JSON null、缺失字段、Some(None)、错误变体的编码应显式定义；解码后的非空、合法 enum 和完整初始化约束须验证。Go JSON 包版本、选项或解码协议改变时，应重新审核，不能用反射写入绕过 goxide 不变量。

### 3.6 sync 使用已有锁专题

互斥与读写锁的 safe 数据访问沿用[锁与作用域回调](synchronization.md)：普通 Go 支持库保存同步原语与 T，goxide 检查回调中的读取、写入及派生借用。泛型 `Lock[T]` / `RWLock[T]` 是 goxide 支持类型，底层仍使用 Go sync。

按值复制已使用锁给 W06，锁内引用返回本身不报，未来有具体违约使用才 W01，也不把锁住一个 slice 头误认为锁住了所有外部底层数组别名。更多 sync 原语按其官方契约分别适配，不用“来自 sync”替代逐入口设计。

## 4. 泛型 slice 的首版方向

后续建议由 std 独立提供 Vec、借用视图与多种 Map 实现，不将原生 map 关键字作为高级容器的唯一入口。分别见 [Vec RFC](../rfcs/0010-collections.md) 和 [Map RFC](../rfcs/0017-maps.md)。以下名义 Slice 是既有候选方案，其赋值共享语义不自动等于新 Vec 的最终语义；Go 原生 slice/map 仍须在互操作层准确表示。

Go 已经有泛型 slice 及 `slices` 包。goxide 要补充的是权限、初始化、可选结果、错误结果和日常 API 视图，不是重新发明这些泛型算法。例如 Go `slices.Clone` 已按赋值复制元素，明确属于浅复制。[Go slices 包](https://pkg.go.dev/slices)、[slices.Clone](https://pkg.go.dev/slices#Clone)

### 4.1 Slice[T] 与原生 []T

首版建议基础包提供名义 `Slice[T]` 门面，内部保存 Go `[]T`，例如由普通支持 struct 的未导出 data 字段承载。`[]T` 保留为原生 Go 互操作形状；二者不是仅靠重命名就相等的类型。这个选择是草案，尚未冻结为语言内置类型或 Go ABI。

Slice 的普通值复制会复制其 slice 头，多个头仍可能指向同一底层数组；一个头的 len / cap 改变不自动更新其他头。多个共享句柄指向同一个 Slice 对象时，连头部状态也共享。`&mut Slice[T]` 授予对该门面的修改权限，不凭空证明底层数组或 T 可达对象没有其他别名。

raw `[]T` 到 Slice 有两条明确途径：复制元素形成新的外层存储，或在已知作用域和契约下借用原存储。后者必须保留外部来源和权限；从 Slice 暴露原始 `[]T` 也保留同样的边界，不能提供一个无限制的 safe raw 导出方法后继续声称可变别名受控。Go 下游只受普通 Go 规则约束，生成代码或支持类型的私有字段不构成完整安全证明。

### 4.2 操作按读取、元素写入和形状修改区分

| 操作方向 | 拟议 goxide 契约 | Go 行为 / 所需包装 |
| --- | --- | --- |
| 长度、检查为空 | 读取头部 | len，无元素借用 |
| 按下标读取副本 | `get_copy(i)` 返回 `Option[T]`；T 必须允许该种复制 | 检查 i 后读取元素，复制仍保留 T 内部别名 |
| 设置元素 | 合法可变访问；越界以明确错误返回 | 下标检查后写入，检查当前访问路径的写权限；不要求证明无重叠别名 |
| 范围只读访问 | 明确只读类型视图，离开协议的风险可诊断 | 只允许范围内读取；Go slice 本身不能表达只读 |
| 范围可变访问 | 明确范围及写权限的元素视图，不承诺独占 | 允许范围内写入，不授予调整原 slice 头的权限 |
| append / resize / delete | 对头部及涉及元素的可变访问，检查类型与初始化；活跃别名风险分析按 D20 延期 | 更新返回的 slice 头；可能分配、搬移或清空元素 |
| 查找 | 依据具体算法返回 Option 下标等 | 需要从 Go 的 sentinel / `(index, found)` 协议实际转换 |

范围视图使用明确的 `[lo, hi)` 边界；Go 桥接可用 `s[lo:hi:hi]` 限制可见容量。视图不提供调整原容器 len / cap、重新切片越界或无限追加的能力。限制容量不是只读证明，也不会消除已存在的别名；源级只读权限仍检查，别名与逃逸按证据 warning，不承诺不逃逸。[Go slice 表达式](https://go.dev/ref/spec#Slice_expressions)

旧的活跃借用期间形状修改默认拒绝已被 D19 否决。允许 append；若旧引用被当作新数组对应元素使用且有具体风险路径则解释性 warning，不能仅有借用就报错。Go append 在容量足够时可以继续使用原数组，容量不足时分配新数组；新 slice 头必须保存。原先的 slice 可能仍引用旧数组或共享原数组，不能假设全部别名自动跟随新头。[Go append](https://go.dev/ref/spec#Appending_and_copying_slices)

声明 std slices 算法时还应考虑其真实写入范围。例如 Compact 会改写元素并清零缩短后的尾部，其他旧视图可能仍能看到那些位置。若 T 不允许零值成为合法 goxide 值，就不能把缩短长度当成消除了全部问题：需明确旧视图读取的校验边界，或显式采用独立输出包装并说明复制成本；不能仅因未证明没有旧别名就普遍要求 unsafe/拒绝原生调用。扩长、make 和部分初始化约束也沿用[枚举与初始化规则](enums-and-match.md#6-初始化容器与-go-零值)。[slices.Compact](https://pkg.go.dev/slices#Compact)

回调式查找、排序或转换还须保留回调的 unsafe 属性、借用来源和重入约束。goxide 不为满足调用而隐式把 `[]T` 整体改成 `[]Trait`；元素适配沿用既有显式转换规则。

### 4.3 浅复制、逐元素克隆与别名

提供不同名称表达不同成本与结果：

- `copy_slice` 采用 Go 元素赋值，产生独立的外层元素存储；内部指针、slice、map 或共享句柄仍可能指向原对象。它要求 T 可以按该契约复制，不能复制不可复制的借用或锁状态。
- `clone_with` 对每个元素调用明确的克隆函数，把输出放入新 slice。回调的效果、失败、结果有效性与别名语义需要检查；逐元素调用不等于自动复制整个对象图。
- 如果 Slice 将来实现基础 `Clone` trait，必须单独规定其存储与逐元素策略，并显式定义 impl；不能把 Go `slices.Clone` 直接改名充当已经满足的协议。

复制产生的新外层数组可以解除外层元素槽位的共享，却不能清除元素内部已知的借用来源。clone_with 的作者若返回原引用，结果仍共享；深复制、保持图结构或拒绝循环均需具体类型自行定义，不由名字 Clone 推断。

## 5. 完整 Go 支持库示例

下面是一个独立 Go 包，演示保留 Read 的计数与错误、slice 外层复制以及逐元素处理。它是支持库机制示例，**不是**接收任意 io.Reader 或任意 T 后自动建立全部 goxide safe 契约的适配器。goxide 入口仍需完成 reader 契约采用、复制许可和回调检查。

```go
package stdsupport

import (
    "io"
    "slices"
)

// Err 保留原始 Go error，包括与非零 N 同时出现的 EOF。
// 若 goxide 公开视图使用 Option[error]，外层桥接另行构造 Some / None。
type ReadOutcome struct {
    N   int
    Err error
}

func ReadOnce(reader io.Reader, dst []byte) ReadOutcome {
    if reader == nil {
        panic("goxide: ReadOnce: nil Reader interface")
    }
    // 限制传入 slice 的容量，不改变调用方 dst 的头部。
    // 不证明 reader 实现不保留 slice，也不检测 typed nil 接收者。
    n, err := reader.Read(dst[:len(dst):len(dst)])
    if n < 0 || n > len(dst) {
        panic("goxide: ReadOnce: Reader returned invalid byte count")
    }
    return ReadOutcome{N: n, Err: err}
}

// CopySlice 独立复制外层元素槽位；T 内部引用仍按 Go 赋值语义共享。
// 返回值保留 nilness，并将可见 cap 限制为 len。
func CopySlice[T any](src []T) []T {
    out := slices.Clone(src)
    return out[:len(out):len(out)]
}

// CloneWith 依次调用 clone，不承诺深复制或回滚其副作用。
// goxide 必须检查 T 的读取方式与回调契约，而非直接照搬 any。
func CloneWith[T any](src []T, clone func(T) T) []T {
    if clone == nil {
        panic("goxide: CloneWith: nil callback")
    }
    if src == nil {
        return nil
    }
    out := make([]T, len(src))
    for i, value := range src {
        out[i] = clone(value)
    }
    return out
}
```

ReadOnce 只调用一次 reader，不把 `(0, nil)` 改成 EOF，不丢掉 `(n > 0, err != nil)` 的 n。它在调用后校验计数，只能防止后续代码相信非法长度，不能撤销恶意或错误 reader 已产生的副作用。nil interface 检查也不是对所有动态接收者的非空证明，具体接收者行为由采用契约负责。

CopySlice 按长度复制外层元素；CloneWith 建立输出并执行每个回调。具体堆分配及可消除的分配由 Go 编译器决定，应通过生成代码测量。示例没有竞态隔离：复制期间原数据也必须可合法读取。输出含引用时是否独立，取决于元素语义；Go 泛型 `any` 本身不表达 goxide 的权限或合法初始化约束。

本轮从本文提取完整 Go 包，在 Go 1.27.1 的独立临时模块中执行 `go test -count=1 -v ./...`，7 项测试通过。覆盖部分读取与错误同时返回、容量限制、零进展、非法计数及其已产生的副作用、nil / 非 nil 空 slice、外层复制与内部别名，以及克隆回调的顺序和共享策略。这只验证所列 Go 支持机制，不验证尚未实现的 goxide 安全视图或权限检查。

## 6. 首版交付与验证范围

暂定优先提供基础类型与少量有明确契约的便利入口：标量/字符串、显式字节复制、ReadOutcome、文件和 context、锁回调、slice。其他 Go API 按真实类型仍可正常调用；没有摘要不要求 unsafe，具体冲突链才 warning。反射和未知外部对象图单列未检查范围，不保证安全。

每项基础 / 标准库 API 的完成条件应包括：源级签名和效果、真实 Go 映射、Go 双向边界、必要包装与成本、正常路径与失败路径验证。至少检查：

1. ABI 不兼容的 Result / Option 包装有实际桥接，不能只靠 declare 编译通过。
2. Read 同时保留部分数据与错误；零进展、EOF、非法计数与具体 reader 的借用契约区分处理。
3. Open 的失败 / 成功分支建立正确句柄状态，Close 与复制别名语义一致，不宣称静态一次关闭。
4. context 的 nil Done、异步取消及完成通知没有混淆；存储值和逃逸回调有来源风险时给 warning，无依据不报。
5. JSON 部分修改、共享字段与自定义解码逻辑不能越过已声明的安全范围；enum / Option 协议显式定义。
6. slice 复制、重分配、可见容量、共享头与共享数据、尾部零值、逐元素克隆均有对应行为与负例检查。
7. Go 支持示例可独立编译；goxide 的权限、借用与 unsafe 诊断须由未来前端单独验证。Go 测试及 `-race` 只能验证实际覆盖路径，不代替这些诊断。

本文列出的是实现目标，尚未完成的源级检查、库实现与包发布不能因文档或 Go 示例存在而标记为已提供。


## 已批准独立仓库边界（2026-10-04）

稳定核心使用 `github.com/goxide-lang/std`，实验能力使用 `github.com/goxide-lang/next`，serde core 与 JSON 使用独立 `github.com/goxide-lang/serde`。共享 sum 的具体 API、schema 8/validator owner、统一重生成及首轮家族限定泛型省参计划见 [共享 std 迁移](shared-std-migration.md)。稳定分类不表示已发布 v1.0；新公开仓库尚未由本阶段推送。

# 卫生宏：声明宏与过程宏

声明宏提供结构化模式、模板、重复及定义点卫生；过程宏提供受限 WASI 中的语法变换。std 的派生、打印与 serde 派生均使用正式过程宏库，输出普通 Goxide 声明。过程宏只读取语法和调用 Context，不取得已解析 TypeShape 或类型证明。执行、预算、helper 与 RuntimeRoot 合同见 [RFC 0030](../rfcs/0030-procedural-macros.md)，普通协议见 [RFC 0031](../rfcs/0031-generic-protocol-impls.md)，当前验收状态见[能力状态表](../mvp-gap-audit.md)。

别名、逃逸和独占风险分析按 D19/D20 独立推进；宏输出仍接受正常语法、类型、可见性和读写权限检查。

## 1. 分类与首版范围

| 入口 | 当前机制 |
| --- | --- |
| `macro_rules!` | 语法类别捕获、模板插入与规定的重复展开，不执行用户程序 |
| function-like proc macro | 将 TokenStream 变为调用位置所需的表达式、类型、语句或 item |
| attribute proc macro | 接收并替换声明，需主动转交仍需保留的属性 |
| derive proc macro | 读取单个类型声明的语法快照，保留原项并追加普通声明 |
| `std::derive` / `serde::derive` | 普通过程宏库，生成 trait/inherent impl，字段协议由后续检查证明 |
| `std::print!` / `std::println!` | 库宏解析格式字面量，生成普通 Display/Debug 调用与 formatting 库调用 |
| cfg 与 Go 返回适配 | 编译器继续处理配置筛选与真实 Go 返回形状；不开放宏类型反射 |

显式依赖分别提供宏构建角色与目标运行时角色。宏在受限 WASI 环境执行，不读取宿主磁盘、网络、环境、时钟或启动进程；允许生成普通运行时 I/O 代码。展开结果继续经过 `resolve → check → lower`，并使用 Go 运行时。

trait、type、value 和 module 使用普通名字空间；声明宏与 function-like、attribute、derive 过程宏共用宏名字空间。同名普通项与宏可以共存，调用语法选择空间并继续校验宏入口种类。单个 `use` 引入目标两空间中的全部同名公开出口；别名、重导出和 glob 保留各自身份。每个空间独立按用户声明/命名 `use`、显式 glob、自动 prelude 的顺序选择，同空间歧义仍诊断。该规则适用于任意库名称，不依赖 std 特判，定义点卫生不变。

serde 核心采用独立的 Serialize/Deserialize、Serializer/Deserializer 与 Visitor/Access；Value 属于 JSON 等格式层，见 [serde RFC](../rfcs/research/0006-serde-core-and-formats.md)。Debug 不作为序列化，派生也不直接退回 encoding/json。公共语法工具位于独立 syntax module，见[语法工具设计](syntax-tooling.md)。

## 2. 模式与模板操作结构化语法

模式中的 `expr`、`type`、`ident` 等捕获类别按 goxide 语法解析。模板捕获值是保留来源与上下文的结构化语法片段，不是拼接后重新猜测绑定关系的字符串。

支持的捕获类别由声明宏语法规定；不同类别不能任意互换。将一个声明列表插入算术表达式位置应报错，不能借重新解析文本隐藏错误。

每个语法片段至少携带：

- 节点或 token tree 的结构及语法类别。
- 标识符的绑定身份或待解析名字，以及 scope / syntax context。
- 源码来源、当前展开实例和展开调用栈。
- 检查上下文，包括 unsafe 授权来源。

声明宏只使用语言规定的匹配和模板操作。需要分支、字段遍历或语法构造时使用过程宏及公共 syn/token 工具；它们观察语法，不取得类型检查结果。

编译器按显式依赖读取源文件与包元数据，但用户宏规则不能自行读取文件、环境变量、时钟或网络，也不能启动进程。这个限制不禁止宏生成运行时 I/O 代码：模板可以产生普通的日志、文件或网络调用，只要展开结果满足相应类型、依赖和 unsafe 规则。

## 3. 函数式声明宏示例

当前源码包采用 `pub macro_rules! name` 导出、`use root as alias;` 绑定、`alias::name!(...)` 调用。根及精确 Go 包映射由[依赖配置](dependency-roots.md)声明；宏与运行时项沿相同静态路径规则分别在宏空间和普通空间解析。运行时 helper 按 Go 导出约定写 `Emit` 等公开函数；私有 helper 跨包不可见，须由作者提供公开 helper。旧包点号调用与 `import macro` 不再支持；`pub func` 等声明使用当前普通语法。限定实现与来源/预算边界见[宏实现说明](https://github.com/goxide-lang/goxide/blob/bd90e606dcfa220b4054fe41c56bdc63a5687355/internal/macros/README.md)。

以下使用 `macro_rules!` 的模式与模板。假设 `macrokit` 包提供一个普通、公开的运行时 helper：

```goxide
pub func twice(value int) int {
    return value + value
}

pub macro_rules! twice_plus_one {
    ($x:expr) => {
        func() int {
            saved := 1
            value := $x
            return twice(value) + saved
        }()
    };
}
```

`$x:expr` 捕获一个表达式，模板中的 `$x` 插入该表达式的结构化语法。展开器不会在编译期执行 `$x` 或运行时函数 `twice`。模板中的立即调用函数是显式生成的普通代码，不依赖另一套尚未定义的块表达式规则。这个宏专门用于 `int`，实参类型在展开后的普通检查中确定。

对应调用：

```goxide
use macrokit as m; // dependency root maps to example.org/goxide/macrokit

saved := 41
twice := func(value int) int { return -1000 }
calls := 0
next := func() int {
    calls += 1
    return 1
}

result := m::twice_plus_one!(saved + next())
```

预期 `result == 85`、`calls == 1`，调用方的 `saved` 仍为 41。实参中的 `saved` 不被模板生成的同名局部变量捕获；模板中的自由名字 `twice` 指向定义包的 helper，不被调用者的局部 `twice` 替换。

这个示例只求值一次，是因为模板明确生成了 `value := $x` 这个局部绑定，并复用 `value`。**卫生本身不保证求值次数。** 模板如果写 `$x + $x`，就会执行实参两次；丢弃实参则可能不执行它。求值顺序、短路、闭包、`defer` 和 `return` 的行为都由实际生成的普通语法决定。

`use` 显式选择宏项或 namespace。模板使用的运行时 helper 也必须来自宏定义文件已声明的依赖；展开不能临时产生未知包路径。编译器根据绑定后的引用记录所需的普通 Go 依赖。

### 3.1 规则选择与重复

首版建议规则按定义顺序匹配，必须消费完整输入；第一条完整匹配的规则生效，不根据实参类型或当前 impl 集合选择分支。局部匹配歧义应诊断，不依赖无限回溯尝试解决。

重复语法可采用 `$($item:expr),*` 一类草案：模式保存每次捕获的结构，模板按对应层级重复插入。重复体不得在不消费输入的情况下无限匹配；捕获层级、重复长度或分隔符不一致时报错。

这是一组规定的声明式操作，不等于允许宏作者运行任意循环。具体捕获、重复和歧义规则见声明宏 RFC。

### 3.2 基础打印宏与格式检查

`std::print!` 和 `std::println!` 是正式过程宏。宏解析格式字面量并输出普通表达式；类型检查选择 Display/Debug，运行时调用公开 formatting 库。格式错误由宏诊断，缺少协议由普通 checker 诊断。`format!`、`eprint!`、`eprintln!`、`assert!` 等额外入口仍需各自确定合同。

当前格式子集：

- 第一个参数必须是格式字符串字面量；`{}` 按顺序请求 `Display`，`{:?}` 请求 `Debug`，`{{` / `}}` 表示字面花括号。
- 编译期检查花括号、占位符数量、实参数量及对应协议；不匹配、未使用参数或未实现 trait 均报错。内建标量也由基础包提供明确的格式化协议，不隐式把所有 Go 值变成可格式化值。
- 首版不支持运行时格式串、隐式捕获同名变量、位置重排、宽度精度参数或任意 Go `%v` 透传；如需动态模板，使用另有明确信任和错误契约的普通函数。
- 每个显式实参按从左到右顺序恰好求值一次，全部求值完后才按占位符顺序调用 formatter。临时量保留源级类型与来源；复制引用默认允许，先后参数存在具体冲突链时 warning，真实只读写入等类型错误仍拒绝。

`{}` / `{:?}` 的协议分工参考 Rust 的格式入口；具体 goxide 格式子集与求值顺序由本设计规定，不代表兼容全部 Rust 格式语法。[Rust format_args](https://doc.rust-lang.org/std/macro.format_args.html)

`Display` 和 `Debug` 是显式 trait 协议；这里的 safe 不承诺静态别名或并发安全。编译器不能用 Go `fmt.Stringer`、`error.Error()`、反射或 `any` 兜底调用未知方法；原生类型须有经过检查的显式适配。标量可降低为 `strconv` 等具体函数，复合值调用选定的实现。格式化实参可以在合法范围内被临时只读借用；格式化计划、formatter 和借用的留存不默认拒绝；有明确协议离开或冲突链时 warning。

打印发生在**生成程序运行时**。宏展开时不执行参数、formatter 或 stdout I/O。采用以下运行时契约：

- `print!("text")` 不追加换行；`print!("")` 不产生输出。`print!()` 缺少格式字面量，报错。
- `println!()` 与 `println!("")` 都输出一个 LF（`\n`）；非空形式在内容后再追加一个 LF，即内容已有换行时仍会追加。
- 正常完成返回 Unit；写入错误或短写转为带原始原因的 panic，不静默丢弃。错误前可能已有部分输出，不承诺回滚、自动重试或跨进程输出原子性。
- 调用者需要恢复和处理部分写入时，使用 [safe Go std 的普通 I/O 入口](standard-library.md)，保留写入计数和错误。缓冲与 flush 按所选输出适配契约处理，不因为使用 `println!` 就承诺持久化。

以上 I/O 错误策略参考 Rust 打印宏的失败行为；goxide 的具体支持库仍以 Go std 写入接口和明确包装实现，不引入 Rust 标准库运行时。

### 3.3 实现结果回流：限定打印协议

实现基准 `47af69f` 已交付 `print!` / `println!`：第 3.2 节的固定占位符、参数先按序各求值一次后格式化、空输出/LF 与写错/短写保留 cause 的 panic 契约均有真实运行证据。后端使用具体 typed formatter，不以 Stringer/error/反射/any 兜底；同包可信 Debug 派生可用于 `{:?}`，不自动成为 Display。

当前入口只接受直接语句位置，支持宏模板、显式参数闭包内的嵌套打印和参数中的 `?`；不支持 go/defer、值位置、initializer/post、导入 Debug 类型或用户格式协议。此范围不修改第 3.2 节的长期协议，也不声称完整 trait/格式生态。编译期仅使用可信来源，生成 I/O 代码仍可 unmapped，未实现运行时 panic 的 goxide 源码回映射。实现和独立生成模块 race 证据见 [实现记录](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/IMPLEMENTATION.md#print--default-最终验收)。

## 4. 卫生是符号身份，不是随机改名

Rust 的 `macro_rules!` 使用 mixed-site hygiene：局部变量和标签按定义点查找，其他符号通常按调用点查找；`$crate` 可明确指向定义包。goxide 本文选择模板自由名字按定义点绑定，是有意采用的更统一规则，不声称照搬 Rust 的名字解析结果。[Rust 宏卫生规则](https://doc.rust-lang.org/reference/macros-by-example.html#hygiene)

标识符解析依赖 SymbolID、ScopeID 和展开上下文。正式内部结构待前端设计，但必须保留以下区别：

| 名字来源 | 解析规则 |
| --- | --- |
| 模板新建的局部绑定 | 每次展开得到新的绑定身份；模板内引用连接到该身份 |
| 模式捕获后插入的调用者语法 | 保留调用者 scope 和已绑定符号，不被模板局部变量重新捕获 |
| 模板中的自由标识符 | 在宏定义点解析，保留对应包和符号身份 |
| derive 输出保留的目标类型引用 | 使用输入语法身份及卫生上下文，不按显示字符串猜类型 |
| 公开 item、trait 成员、字段选择器 | 按声明协议使用有意公开的名称或成员身份，不能全部 gensym |

第 3 节的两个 `saved` 是不同符号。Go 后端需要文本名字时，在绑定完成后为不同身份分配无碰撞的合法名称。临时变量后缀只是输出形式；随机字符串、时间戳或全进程计数不能充当卫生机制。

公开 API 名称是受控的有意接口。例如派生的 `Clone.clone` 必须对应该 trait 成员，而不是随机新方法。模板需要向调用方提供一个公开普通函数时，应由明确捕获的声明标识符或固定协议给出名称，并按目标包的正常规则检查冲突。首版不提供任意取消卫生、捕获调用者局部变量的通用操作。

宏展开身份、局部绑定身份和 Go 导出 ABI 名称分别管理。宏规则不自行拼接 marker 或 AsXXX 名称；这些仍由 [trait 降低](traits.md)与稳定符号规则决定。

### 4.1 定义点绑定不授予私有访问权

模板在定义点能找到私有 helper，不表示展开到另一个包后就能访问它。首版禁止跨包展开结果直接调用宏定义包的私有运行时成员；错误同时指向模板引用和宏调用。

宏作者可以提供普通公开 helper 或显式公开 shim。shim 在其定义包内访问私有实现，调用方只使用正常公开入口。展开器不自动突破 Go 的导出规则，也不赋予调用方额外权限。

宏包内部使用私有声明宏时，仍按显式登记的编译期依赖展开；它不能借此在最终代码中留下非法的私有运行时引用。

## 5. 普通库 Default、Clone 与 Debug 派生

```goxide
#[derive(Default, Clone, Debug)]
struct Point { X int; Y int }
```

所选 std 源库的 prelude 通过普通重导出提供这些派生宏及同名 trait；`#[derive(Debug)]` 选择宏，`T: Debug` 选择 trait。`std::derive::Debug` 和 `derive::Debug` 等限定路径继续可用。宏读取 Point 的语法并输出普通 impl。完整基础及比较协议见 [RFC 0013](../rfcs/0013-derive.md)，实现来源不影响协议资格或后续检查。

### 5.1 类型形状、约束与冲突

过程宏读取字段、变体、泛型参数与 helper 的语法。宏不查询已解析类型，不取得私有访问特权；输出位于目标包，按正常权限访问本包字段。泛型派生生成必要的 where 条件，不收紧原类型声明。手写、声明宏与过程宏产物统一接受 impl 归属、互斥、签名及引用检查；已有实现不被静默跳过或覆盖。

### 5.2 Default：显式构造合法值

Default 的普通静态成员是 `Default() Self`。struct 按字段顺序构造，enum 恰好选择一个 `#[default]` 变体，仅要求该变体的 payload 有 Default。Option 返回 None 且不要求载荷 Default；Result 不自动取得 Default。普通 `var`、`make`、map 缺失和 channel 零值不调用业务构造。

手写实现可给出非零业务默认值，也可像普通函数一样递归。编译器不保留 Default 专用调用图检查；协议前提循环和无限实例需求由通用机制诊断。

#### 5.2.1 实现结果回流：具体 Default 与 Go 零值

历史实现 `47af69f` 使用具体 helper，当前入口为普通 `std::Default` 与 `std::default_value[T]()`。serde default/skip 使用同一普通协议；default 只处理字段缺失，skip 默认构造一次，完成 required 检查后按字段顺序执行。Option 缺失/null 为 None，错误值不回退默认。历史背景见 [RFC 0013](../rfcs/0013-derive.md#旧管线实现记录)。

### 5.3 Clone：字段协议决定复制内容

Clone 从只读 self 产生同类型结果。struct 按字段顺序调用 Clone，enum 保留活动变体；容器按已选元素协议建立副本并保留 nil/空区别。普通赋值仍按 Go 浅复制，Clone 的共享、分配与别名行为取决于具体库实现；不使用反射或普通赋值替代缺失协议。

### 5.4 Debug：受检查的运行时格式化

Debug 返回 string，依字段声明顺序显示类型、字段/变体名与内容，包括私有数据。complex64/complex128、其容器元素及 enum payload 使用普通 Debug 实现；Debug 的资格独立于 Clone、Default、Eq 和 Error 插值。serde rename/value 不改变 Debug。输出不自动脱敏，也不作为稳定序列化格式。

### 5.5 静态 trait 操作与 Go 运行时接口

Default/Clone/Debug 与用户 trait 使用同一后端：静态操作按已选 impl 生成入口，符合运行时视图条件的实例操作使用 wrapper。源级 Self 保持实际目标类型，不擦成 any。普通 Go 泛型保留；有待定协议条件的实例按 D38 的实际请求展开。固有关联函数直接放在空 TypeOps carrier 上，详见 [RFC 0020](../rfcs/0020-associated-functions.md)。

## 6. 展开阶段

1. 按来源及构建角色处理 cfg，登记声明宏和过程宏入口。
2. 展开声明宏及函数式过程宏，按所在语法槽检查输出类别。
3. 按 [RFC 0030 的属性顺序](../rfcs/0030-procedural-macros.md#惰性属性的保留与消费)处理 attribute/derive/helper；attribute 替换目标并转交属性，所有 derive 读取共同语法快照、保留原项并追加产物。输出中的普通宏继续展开。
4. 对完整结果执行名字、类型、可见性和权限检查，选择普通 impl 并降低 Go。

输出合法语法不等于类型正确。缺协议、重复实现或只读写入仍在正常阶段诊断；库宏不能上传已检查证明，也不能根据 TypeShape 绕过此流程。配置变化通过构建 Context 与来源角色进入执行，不由宏读取宿主环境。

## 7. unsafe 与转译前检查

展开节点保留定义来源和调用来源，随后使用[原生 API 与裸指针](foreign-api.md)中的普通检查规则。宏不能免除生成代码的 unsafe、引用、锁回调或 trait 约束，也不能在展开时执行模板中的原生调用。

尤其不能由模板生成的 unsafe 替捕获参数里的代码授权。例如下列模板片段：

```goxide
unsafe raw::Consume($x)
```

模板作者引入的 `Consume` 可以按模板中的显式 unsafe 检查，但 `$x` 内调用者写下的操作保留调用者原有授权上下文。若实参为明确声明 unsafe 且未获授权的 `raw::Other()`，展开后仍应报错。调用点显式写出的 unsafe 可按普通求值规则授权其直接表达式，但不自动授权新建函数或闭包体。

实现不能只在展开文本中寻找最近的 unsafe 祖先节点；必须使用节点来源和授权范围。重复插入、再经另一宏转发或 derive 包装，都不能把调用者节点重新标成定义者代码，洗掉检查要求。

宏作者明确生成的 unsafe 操作承担与普通库作者相同的正确性责任；这不授权宏自动为用户代码新增可信外部契约。首版宏和派生结果不得生成外部 API 的 declare 契约声明，这些声明仍由显式源码或声明包提供。

借用派生的 Ptr 经宏包装后仍保留已知来源；不能因展开或 unsafe 表达式结束就认为外部别名消失。linter 的边界沿用总纲，不借卫生宏宣称完整跨包所有权证明。

## 8. Go 降低与双向互操作

### 8.1 卫生展开

Go 接收展开、检查后的普通代码，没有运行时宏对象或宏解释器。规则与捕获语法只存在于编译期。第 3 节可以降低为以下完整、可独立执行的 Go 示意：

```go
package main

import "fmt"

// 为独立验证而放在同一文件；实际跨包输出引用宏包的公开 helper。
func macroRuntimeTwice(value int) int {
    return value + value
}

func main() {
    saved := 41
    twice := func(value int) int { return -1000 }
    _ = twice
    calls := 0
    next := func() int {
        calls++
        return 1
    }

    result := func() int {
        goxideM1Saved := 1
        goxideM1Value := saved + next()
        return macroRuntimeTwice(goxideM1Value) + goxideM1Saved
    }()

    fmt.Println(result, calls, saved)
}
```

本轮已从本文提取示例，在 Go 1.27.1 的独立临时模块中执行，实际输出为 `85 1 41`。`goxideM1Saved` 等名字仅展示绑定后如何避开冲突，卫生检查发生在分配这些文本名字之前。运行这个 Go 示例只能验证特定展开代码的行为，不能证明尚不存在的 goxide 展开器已经正确。

### 8.2 Default 与 Clone 的静态调用

下面是另一段完整 Go 示意。`Record` 的派生结果调用三个字段各自选定的普通函数：Label 的默认值是非空字符串，示例分数列表的复制创建独立存储，共享计数器的复制只返回同一锁句柄。列表 helper 仅代表本例明确选定的字段实现，不承诺基础包已经确定泛型 Slice 的 Clone 策略。

```go
package main

import (
    "fmt"
    "sync"
)

type Label string

func defaultLabel() Label { return "untitled" }
func cloneLabel(value Label) Label { return value }

type Counter struct {
    mu sync.Mutex
    value int
}

func defaultCounter() *Counter { return &Counter{} }
func cloneCounter(value *Counter) *Counter { return value }

func defaultScores() []int { return []int{} }
func cloneScores(value []int) []int {
    result := make([]int, len(value))
    for i, item := range value {
        result[i] = item // 本例 int 的显式 Clone 可降低为同值复制。
    }
    return result
}

type Record struct {
    Title Label
    Scores []int
    Shared *Counter
}

func defaultRecord() Record {
    return Record{defaultLabel(), defaultScores(), defaultCounter()}
}

func cloneRecord(value *Record) Record {
    return Record{
        cloneLabel(value.Title),
        cloneScores(value.Scores),
        cloneCounter(value.Shared),
    }
}

func main() {
    original := defaultRecord()
    original.Scores = []int{1, 2}
    cloned := cloneRecord(&original)
    cloned.Scores[0] = 9
    cloned.Shared.mu.Lock()
    cloned.Shared.value = 7
    cloned.Shared.mu.Unlock()
    original.Shared.mu.Lock()
    sharedValue := original.Shared.value
    original.Shared.mu.Unlock()
    // fmt 仅用于观察这个独立示例，不代表 print! 的通用 lowering。
    fmt.Println(original.Title, original.Scores[0], cloned.Scores[0],
        original.Shared == cloned.Shared, sharedValue)
}
```

本轮从本文提取示例，在 Go 1.27.1 独立临时模块中执行，实际输出为 `untitled 1 9 true 7`：默认值由显式构造得到；分数列表的写入不影响原列表，锁句柄仍共享状态。此例只验证这些已选择的 Go 函数，不验证通用派生、借用诊断或泛型函数字典。生成函数的正式名字仍需按稳定 impl 身份编码。

### 8.3 Go 接口边界

过程宏生成的 impl 复用正常 trait 检查；静态操作按第 5.5 节生成入口，满足运行时视图要求的方法才生成对应 wrapper、业务方法和 marker。

goxide 调用原生 Go 时，宏可生成已经显式依赖的原生调用，但仍使用选中的 declare 视图或 unsafe 规则，不能因宏包受信就把调用方所有 Go API 提升为 safe。

Go 源码不能直接调用 goxide 宏，但可以使用宏生成并导出的普通类型、函数、方法和 trait 视图，遵守最终 Go ABI。若生成 API 涉及原生类型的 marker，发布与独立构建视图仍遵循 [Go 适配](go-interop.md)规则；宏不免除相关构建要求。

宏可决定使用方的公开 API，改变生成名称、签名或表示时需要正常兼容性审查，不能仅当作宏包内部实现变化。

## 9. 资源预算、确定性与缓存

递归宏、重复模板和过程宏程序均可能耗尽资源。当前预算、受限 WASI、CLI 截止及浏览器平台边界由 [RFC 0030](../rfcs/0030-procedural-macros.md)规定。

超限诊断应包含宏调用链、定义位置和超限类别，不能留下部分树继续当成成功。具体限额见 RFC 0030；墙钟超时与用户取消可以中断工具，但不应缓存为源码必然非法。

缓存至少包含：

- 编译器、宏语法、卫生协议、宏源码与 SDK 身份。
- 格式宏及其普通运行时依赖的源码身份。
- 宏规则内容、显式宏依赖和运行时 helper 的符号接口身份。
- 捕获语法结构、卫生上下文与授权来源，或 derive 输入语法与参数。
- 影响输入选择的构建配置及相关声明视图版本。

缓存不能把第一次调用的局部绑定身份、调用点 span 或 unsafe 授权原样贴到第二次调用。可缓存带待实例化身份的展开模板，命中时重新建立当前展开的 fresh bindings 与来源映射。

输出不依赖磁盘遍历顺序、map 随机迭代、进程地址、时间戳或并行调度；不要求内部对象地址相同。取消和资源耗尽产生的半成品不得进入成功缓存。

## 10. 诊断、LSP 与自举

每个展开节点同时保留 origin span 与 invocation span：前者指出模板或捕获参数的来源，后者指出用户为什么得到该节点。嵌套展开还要保留调用栈，不能只留下最内层生成文件的位置。

LSP 与构建共享宏依赖、展开器、缓存协议及语义数据库，应支持：

- 从调用跳到规则定义，查看当前展开结果与引入的普通依赖。
- 捕获参数错误定位调用者输入；模板错误同时显示定义点和调用点。
- 将 derive 产生的方法展示在目标类型下，标注宏定义与生成来源。
- 按 SymbolID 重命名，不因为显示名字相同就改到另一卫生作用域。
- 将生成 Go 的错误映射回 goxide 节点，同时保留生成位置用于调试。

展开预览是只读派生结果。修改规则或输入后重算；首版不支持编辑展开文本后反向猜测应修改哪一段模板。

编译器使用 Go 实现匹配、卫生与受限宿主；业务派生位于普通宏库。用户规则不因编译器用 Go 编写就获得宿主执行能力，也不要求先实现通用的编译期函数解释器。自举前后比较规范展开结构、绑定关系和生成程序行为，不只比较临时变量的文本名字。

## 11. 验收条件

以下列出行为要求；当前验证入口及未验收部分见能力状态表：

1. 第 3 节模板与调用者均使用 saved 时分别绑定；调用者的 twice 不能遮蔽模板定义点的函数。
2. 同一宏展开两次，其局部绑定不互相引用；缓存命中和未命中的绑定关系一致。
3. 特定示例输出 85，只执行一次 next；明确复制实参的模板可以执行两次，不把求值一次归因于卫生。
4. 模式完整匹配、语法类别、重复层级和歧义均有确定诊断，不依赖最终类型选择规则。
5. 模板生成的 unsafe 不能授权捕获参数中未获授权的调用，新函数体也不继承外围授权。
6. 跨包模板引用私有运行时 helper 被拒绝；公开 helper 或显式公开 shim 可用。
7. 打印格式字面量、实参数量、Display / Debug 实现均在编译期检查；实参从左到右各求值一次，临时引用按具体来源风险 warning；不调用未经适配的 Go Stringer 或反射。
8. `print!("")` 无输出，`println!()` 和 `println!("")` 各输出一个 LF；写入错误及短写遵守 panic 策略，不吞掉部分写入事实。
9. Default 派生逐字段构造；enum 接受单个 default 变体，仅其 payload 字段递归要求 Default；Result nil、无约束泛型零值不能借 Default 获得合法性。
10. Clone 派生调用字段协议，缺少显式 impl 仍错误；不能仅因字段含 `&mut` 就套用旧独占规则拒绝；共享 Arc 和锁仍共享状态，独立数据复制不被夸大为整个对象图深复制。Debug 不延长借用，也不绕过锁权限读取内部数据。
11. derive 只读取语法，输出显式 impl；缺失字段协议、重复 impl、关联类型错误正常诊断。静态 Self 操作保留具体类型，未知 derive 正常诊断。
12. 递归或输出爆炸在预算内终止并诊断；声明规则不能读取环境、文件或在宏扩展中执行用户 AST 程序。
13. 输入、规则、依赖、shape、格式协议、算法版本或授权来源变化时正确失效缓存；不同调用不复用旧 scope 和 span。
14. Go 可使用导出的生成 API；纯 Go 构建无需宏展开器，但须具备产物声明的支持库与按需增强构建视图。LSP 与构建给出相同展开和诊断。

声明宏、过程宏及正式库派生已接入；完整回归和发布状态见能力状态表，LSP 仍按 MVP 后续阶段推进。


问号传播候选见 [RFC 0005](../rfcs/research/0005-option-result-question.md)：`{:?}` 格式协议与传播运算符无关；展开中的 `?` 服从真实函数/闭包边界，卫生不改变返回目标。

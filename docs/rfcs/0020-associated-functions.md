# RFC 0020：类型关联函数与构造函数

## 状态与范围

已确认方向：类型关联函数定义在独立的 `impl T { ... }` 块中，用 `T::method(...)` 调用，转译为普通 Go 包级函数。impl 内支持 `Self` 和实例成员的 `self` 绑定及转译；直接书写的 Go 函数与方法保留原生 Go 语义。已确认成员均用 `func` 声明：首参数为 `self` 的是实例成员，否则是关联函数，零参数函数自然是关联函数，不引入 `static`。具体接收权限拼写、Go 导出名、重载与初始化细则仍为草案；当前实现子集见下文。

这里的“类函数”指无实例接收者的类型关联函数；构造函数是其中返回新值或句柄的一类。结构体声明只定义数据，增强成员写在 impl 块中。成员种类由首参数是否为 `self` 决定，不能根据函数体是否使用 self 来推断；接收者的类型及权限拼写仍按既有 self 规格另行收敛。

`impl T { ... }` 定义固有成员；`impl Trait for T { ... }` 定义显式 trait 实现，后者的实例 wrapper、marker、As/For helper 继续按 [RFC 0019](0019-uniform-trait-wrappers.md)。这一区分约束函数与方法声明，不将 enum、match 等其他 goxide 语法限制在 impl 内。

当前新增的[用户关联协议子集](../static-protocols.md)支持 `trait P { func Abc() Self }`、本包具体 struct/enum 的 `impl P for U`，以及 `U::Abc()` 和单协议约束的 `T::Abc()`。这组关联函数统一通过空 carrier 与 `std/typeinfo.Of` 降低；它不采用下面固有构造函数示例的候选 `NewT` 命名，也不代表通用 `impl T` 或实例 wrapper 已实现。

## 源级规则

1. 前端区分原生 Go 声明与 impl 成员。直接写 `func NewPoint(...) Point` 或 `func (p Point) M()` 时，保留 Go 的声明、接收者、名称和行为；不按函数名自动注册 `Point::new`，不自动改成 wrapper 方法。原生调用的权限边界仍见 [RFC 0006](0006-foreign-api.md)。
2. 编译期按类型身份与关联函数名解析 `T::method`。impl 中的 `Self` 绑定到完整的实现目标类型：`impl Point` 中是 Point，泛型 `impl ... Pair[T]` 中是 Pair[T]，trait impl 中也不是生成的 wrapper 类型。`Self::method` 继续做成员解析，不能仅把字符 Self 替换成一个 Go 标识符。
3. `self` 是实例成员的接收者绑定，其值/借用权限由成员声明决定；无接收者的关联函数没有隐式 self，也没有运行时“类对象”。Self/self 区分大小写，按语法树与符号身份处理；原生 Go 中显式声明的同名标识符仍是普通 Go 绑定。
4. 名称 new 不隐含分配、默认初始化、move 或析构；行为由函数体与签名定义。参数、返回值、泛型约束、可见性和 unsafe 仍使用普通函数规则。
5. 返回 `Self` 是值，返回 `Arc[Self]` 是共享句柄，返回 `Result[Self,E]` 是显式可失败结果。它们分别按既有类型规则降低，不因名称叫 new 就统一改成 Go `*T` 或 `(T,error)`。
6. 本篇的固有 impl 示例位于本地类型的定义包，生成函数也在该包；外部类型的关联声明另行设计。公开构造函数可用 Go 惯用名 `NewPoint`；其他关联函数可用 `PointOrigin`。这些是候选名称，编译器通过元数据查实际符号，不按名字猜测；发生导出碰撞必须诊断或显式命名，不能覆盖直接书写的 Go 声明。
7. 建议首版不按参数或返回类型重载；不同构造方式使用 new、origin、from_text、with_capacity 等不同名字。关联函数只允许从类型名调用，避免擦掉实例表达式的副作用；实例成员仍通过实例调用。
8. 构造函数显式执行。`var p Point`、数组零值或 `make` 不会隐式调用 new/Default；不允许零值的源类型继续接受初始化检查。

## 转译示例

### C01：类型上的构造函数，普通 Go 工厂

以下包含直接书写的 Go 类型声明与 goxide impl。已确定的是 impl 块、Self 支持、`Point::new` 调用与包级函数降低；关联函数用 `func` 且不声明首参数 self。

```goxide
type Point struct {
    x int
    y int
}

impl Point {
    pub func new(x int, y int) Self {
        return Self { x: x, y: y }
    }
    pub func origin() Self {
        return Self::new(0, 0)
    }
}

p := Point::new(1, 2)
o := Point::origin()
```

对应完整 Go 文件（调用片段不包含在文件中）：

```go
package geometry

type Point struct { x, y int }

func NewPoint(x, y int) Point {
    return Point{x: x, y: y}
}
func PointOrigin() Point {
    return NewPoint(0, 0)
}
```

Go 调用方使用 `geometry.NewPoint(1, 2)`、`geometry.PointOrigin()`。无额外 wrapper、分派表或实例分配；Go 仍按普通值与逃逸规则处理 Point。

### C02：返回共享句柄由签名显式表达

源级片段位于 `impl Point` 内；`Arc::new` 是待定义基础包构造入口的示意拼写，暂假定能将一个合法值放入共享存储：

```goxide
pub func shared(x int, y int) Arc[Self] {
    return Arc::new(Self::new(x, y))
}
```

对应 Go 片段，依赖 C01 的 Point/NewPoint：

```go
func PointShared(x, y int) *Point {
    value := NewPoint(x, y)
    return &value
}
```

每次调用新建该逻辑对象；复制返回句柄仍共享该对象。物理分配由 Go 编译器决定，不额外引入引用计数。返回 `&` 局部值成为 Arc 的合法性由明确构造操作规定，不能将任意短借用直接当 Arc 返回。

### C03：泛型构造保留类型实参

直接书写的 Go 泛型类型声明与泛型 impl；impl 的类型参数声明拼写为草案：

```goxide
type Pair[T any] struct {
    first T
    second T
}
impl[T any] Pair[T] {
    pub func new(first T, second T) Self {
        return Self { first: first, second: second }
    }
}
p := Pair[int]::new(1, 2)
```

对应 Go 定义与函数体调用片段（T 在此只需允许对应值复制）：

```go
type Pair[T any] struct { first, second T }
func NewPair[T any](first, second T) Pair[T] {
    return Pair[T]{first: first, second: second}
}
// 在调用函数内：
// p := NewPair[int](1, 2)
```

无参数且仅结果含 T 的构造，也应生成已解析的类型实参；不能指望 Go 从左侧变量类型推导泛型调用。具有 goxide trait 约束的构造还需携带相应实现证据，不能统一擦成 any。

### C04：取函数值仍是普通函数值

```goxide
make_point := Point::new
p := make_point(1, 2)
```

对应 Go 函数体片段，假定调用方已导入 geometry：

```go
makePoint := geometry.NewPoint
p := makePoint(1, 2)
_ = p
```

读取函数值不先构造 Point。unsafe 构造函数取值后仍是 unsafe callable；从类型名取值不擦除该效果。

### C05：可失败构造不会因名字改变 ABI

源级签名草图：

```goxide
// impl Point 内的关联函数；函数体应显式返回 Ok 或 Err。
pub func from_text(text string) Result[Self, string]
```

Go 签名片段，依赖最终选定的 Result 支持类型与 Point：

```go
func PointFromText(text string) Result[Point, string] // 仅签名示意
```

不得仅因为 Go 常用 `(Point,error)` 就将后者视为相同类型；如果公开另一种 Go 便利入口，需要实际桥接及完整成功/失败协议。enum ABI 仍由 [RFC 0008](0008-enums.md) 决定。

### C06：直接书写的 Go 函数和方法保持原样

源文件中的原生 Go 声明片段，依赖 C01 的 Point；这里没有 impl：

```goxide
func MakePoint(x, y int) Point { return Point{x: x, y: y} }
func (p Point) LegacySum() int { return p.x + p.y }
```

对应 Go 仍为相同声明：

```go
func MakePoint(x, y int) Point { return Point{x: x, y: y} }
func (p Point) LegacySum() int { return p.x + p.y }
```

调用仍是 `MakePoint(1, 2)` 和 `p.LegacySum()`；不从 MakePoint 的名称或返回类型推导 `Point::make`，也不为 LegacySum 自动创建 trait impl。若原生方法需要采用为 trait 实现，仍须显式 declare impl。原样保留指声明及执行语义，生成器可以正常格式化和组织文件。

### C07：impl 实例成员的 Self 与 self

以下为 `impl Point` 的实例成员概念片段，采用只含 int 字段的可复制 Point 与值接收者来展示绑定；首参数 self 表明实例成员，注释占位并非可编译的接收者声明，不由此冻结接收类型或权限语法：

```goxide
impl Point {
    pub func translated(self /* 接收类型及权限待定 */, dx int, dy int) Self {
        return Self { x: self.x + dx, y: self.y + dy }
    }
}
q := p.translated(3, 4)
```

对应 Go 声明片段，依赖 C01 的 Point：

```go
func (recv Point) Translated(dx, dy int) Point {
    return Point{x: recv.x + dx, y: recv.y + dy}
}
// 函数体调用：q := p.Translated(3, 4)
```

`Self` 在结果与字面量中都映射到 Point，self 的字段访问映射到实际接收者 recv。p 不被修改；q 的坐标分别增加 3、4。生成接收者名须避开用户已有绑定。借用或写接收者依已声明权限降低，不能为了方便统一复制成值接收者。

trait impl 的 Self 同样表示源实现目标；wrapper 内 `self.field` 访问保存的目标，当前 trait 方法调用则仍使用已选实现，不能全部改为调用目标的原生方法。详见 [trait 接收者规则](../design/traits.md#4-接收者共享状态与-self)。

### C08：原生 Go 中同名标识符不是 impl 绑定

以下原生 Go 片段单独作为一组示例，不在 impl 内：

```goxide
type Self int
func Echo(self Self) Self { return self }
```

生成 Go 保留普通类型名和参数名：

```go
type Self int
func Echo(self Self) Self { return self }
```

不得全文件搜索替换 Self/self。字符串、注释和未绑定到 impl 的同名符号也不参与该转换。

## 拒绝与边界

| 编号 | 输入或错误降低 | 预期 |
| --- | --- | --- |
| C-N1 | 无接收者关联函数读取未声明的 self，期望隐式获得实例 | 源级拒绝 |
| C-N2 | 同一类型定义两个同名 new，仅参数/返回类型不同 | 按首版不重载的候选规则拒绝 |
| C-N3 | `make_point().new()` 被直接降低为 NewPoint，漏执行 make_point | 禁止这种改写；建议源级拒绝实例式调用 |
| C-N4 | `var p Point` 自动执行用户 new 或 Default | 错误降低；初始化检查不隐式增加业务副作用 |
| C-N5 | 仅给普通 Go 构造函数换类型注解，就把可空返回变成非空 Arc | 必须有外部契约或真实检查，不能凭名字授权 |
| C-N6 | 生成的 NewPoint 与已有包级函数同名 | 在导出规划时诊断，不能覆盖或不稳定地改名 |
| C-N7 | 仅因原生 Go 函数返回 Point，就自动把它注册为 Point::new | 不建立这种隐式关联；按原生函数调用 |
| C-N8 | 根据方法体是否读取 self 决定它是关联函数还是实例成员 | 拒绝这种推断；成员类别由声明决定 |
| C-N9 | 将 impl 中的 Self 换成生成 wrapper 类型，或把原生 Go 的同名参数也改成目标字段 | 错误降低；按源类型与符号绑定分别处理 |
| C-N10 | 生成的固有方法与直接书写的 Go 方法重名 | 按成员及导出冲突规则诊断，不能覆盖原生方法 |

声明构造函数不会禁止普通 Go 写 `Point{}`，也不会自动禁止 goxide 的合法结构体字面量。若某类型必须只能通过检查后的构造入口取得，需要另行规定字段可见性、合法初始化和公开边界，不能从存在 new 函数直接推导。

## 双向互操作

Go → goxide：impl 中公开关联函数表现为普通 Go 包函数，如 `geometry.NewPoint`；本地类型的实例成员表现为按接收者规则生成的 Go 方法。直接书写的 Go 声明保留原入口。goxide 下游从声明元数据恢复“属于 Point 的 new”、真实符号、签名、泛型及 unsafe 效果，不反向猜测任意 NewPoint 都来自 impl。

goxide → Go：原生 `native.NewWidget` 可继续作为包函数调用；若希望在 goxide 视图中组织为 `Widget::new`，应显式绑定真实函数并校验契约。外部关联声明的精确语法、作用域和冲突规则暂不纳入已确认子集，不自动向外部 Go 类型增加成员。

trait 的 `Default.default() -> Self` 仍需选择显式 impl；具体实现可以同样降低为普通函数，但不凭同名 inherent `new/default` 自动建立 Default。泛型静态 trait 调用的证据传递见 [RFC 0013](0013-derive.md)；没有实例的方法不强塞进运行时 marker 接口。

## 验收与未定项

未来验收：C01 的两种构造、C02 的独立对象和共享句柄、C03 的明确类型实参、C04 的调用次数与效果、C05 的成功/失败保留、C06 的原生声明不变、C07 的 Self/self 绑定及接收者语义、C08 的普通同名符号不变，以及 C-N1–C-N10。这些通用固有关联函数与实例成员示例不作为当前实现验收。用户关联协议的真实源码与独立模块验收见[专项说明](../static-protocols.md)；下面保留既有手动 Default 入口的范围，不将其改写成新协议语法。

下一轮只需收敛：实例成员的接收类型及权限拼写；Go 公共名称是否采用 NewT/TMethod；固有关联函数与同名静态 trait 操作如何限定。impl 块承担增强成员定义、Self/self 按绑定转译、原生 Go 声明保留原语义，以及 `T::method` 与包级 Go 函数的直接对应均已确定。

## 当前手动 Default 实现子集

```goxide
struct Settings {
    Port int
}
impl Default for Settings {
    func default() Settings {
        return Settings{Port: 8080}
    }
}
#[derive(Default)]
struct Application {
    Config Settings
}
```

手动实现的目标限同包本地非泛型 struct，成员必须是唯一的零参数 `default`，返回目标具体类型；该入口没有 self，也不需要 static。嵌套 struct 派生调用登记的手动实现，保留业务默认值。登记按包与类型身份进行，同包多文件可共享，不能按普通函数 `DefaultSettings` 的名称认领协议。重复手写实现、与 derive 冲突、错误参数或返回类型及不受支持的静态可达函数引用的递归协议依赖均须诊断。

此子集不实现通用 trait 后端、实例 wrapper、泛型证据或跨包 Default 元数据消费；外部类型实现与跨包消费暂不支持。这里要求具体返回类型，不据此声称完整 `Self` 降低已经实现。

当前 Default 子集不做动态回调分析或一般终止性证明；宏可调用已登记 helper，但 item 宏生成 impl 尚不支持。

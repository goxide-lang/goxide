# RFC 0020：类型关联函数与构造函数

## 状态与范围

已确认：类型关联函数定义在独立的 `impl T { ... }` 块中，用 `T::method(...)` 调用，降低到类型的空 carrier。Self 是基础目标 T；成员用 func 声明，首参数为 `self Self`、`self &Self` 或 `self &mut Self` 时为实例成员，其余为关联函数，不引入 static。**是否带 self 优先于返回类型**：带 self 且返回 Self 仍为实例成员。

固有成员映射已实现：无 self 放空 carrier，enum 值和只读实例成员放接口与各指针变体，struct 实例成员沿普通 Go receiver；业务体直接放在对应方法内。enum 固有成员拒绝 `self &mut Self`，更新通过返回新 enum 并重绑定变量。条件成员遵循 D38 的按需扁平化，不为整个泛型家族添加不成立的方法，也不生成专用 view。全树及发布验收进度见[状态表](../mvp-gap-audit.md)。

这里的“类函数”指无实例接收者的类型关联函数；构造函数是其中返回新值或句柄的一类。struct/enum 声明定义数据，增强成员写在 impl 块中。成员种类由首参数是否为 self 决定，不能根据函数体是否使用 self 推断。

`impl T { ... }` 定义固有成员；`impl Trait for T { ... }` 定义显式 trait 实现，后者的实例 wrapper、marker、As/For helper 继续按 [RFC 0019](0019-uniform-trait-wrappers.md)。这一区分约束函数与方法声明，不将 enum、match 等其他 goxide 语法限制在 impl 内。

trait 的无接收者关联函数如 `trait P { func Abc() Self }` 支持本包具体 struct/enum 的显式实现、`U::Abc()` 和单 trait 约束的 `T::Abc()`，通过空 carrier 与 `std/typeinfo.Of` 降低。关联类型由 carrier witness 推导，不增加运行时字典参数；普通 Go 调用保留 `Make[User]()`。固有关联函数也使用 carrier，不凭同名成员建立 trait impl。

## 源级规则

1. 前端区分原生 Go 声明与 impl 成员。直接写 `func NewPoint(...) Point` 或 `func (p Point) M()` 时，保留 Go 的声明、接收者、名称和行为；不按函数名自动注册 `Point::new`，不自动改成 wrapper 方法。原生调用的权限边界仍见 [RFC 0006](0006-foreign-api.md)。
2. 编译期按类型身份与关联函数名解析 `T::method`。impl 中的 `Self` 绑定到完整的实现目标类型：`impl Point` 中是 Point，泛型 `impl ... Pair[T]` 中是 Pair[T]，trait impl 中也不是生成的 wrapper 类型。`Self::method` 继续做成员解析，不能仅把字符 Self 替换成一个 Go 标识符。
3. `self` 是实例成员的接收者绑定，值/读/写由 `self Self` / `self &Self` / `self &mut Self` 决定；无接收者的关联函数没有隐式 self，也没有运行时“类对象”。Self/self 区分大小写，按语法树与符号身份处理；原生 Go 中显式声明的同名标识符仍是普通 Go 绑定。
4. 名称 new 不隐含分配、默认初始化、move 或析构；行为由函数体与签名定义。参数、返回值、泛型约束、可见性和 unsafe 仍使用普通函数规则。
5. 返回 `Self` 是值，返回 `Arc[Self]` 是共享句柄，返回 `Result[Self,E]` 是显式可失败结果。它们分别按既有类型规则降低，不因名称叫 new 就统一改成 Go `*T` 或 `(T,error)`。
6. 固有 impl 位于本地类型定义包，生成成员也在该包；外部类型的固有关联声明另行设计。无 self 的成员放在空 carrier 上，公开方法按 pub 映射为大写名，例如 `PointOps{}.New`；私有方法保留私有名。struct 的实例成员生成普通 Go 方法；enum 的实例签名加入 E 接口，业务体直接位于各指针变体方法。Self 始终表示完整 E，返回 Self 不改变成员位置。发生生成名或字段/方法碰撞时诊断，不覆盖原生声明。
7. P4 不按参数或返回类型重载；不同构造方式使用 new、origin、from_text、with_capacity 等不同名字。关联函数只从类型名调用；实例成员通过实例调用。多个 trait 的同名静态成员不按返回期望或顺序猜选实现，无限定调用须诊断歧义。
8. 构造函数显式执行。`var p Point`、数组零值或 `make` 不会隐式调用 new/Default；不允许零值的源类型继续接受初始化检查。

## 转译示例

### C01：类型上的构造函数，空 carrier 方法

以下包含直接书写的 Go 类型声明与 goxide impl。关联函数用 `func` 且不声明首参数 self；读取或调用它不构造 Point 实例。

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
type PointOps struct{}

func (PointOps) New(x, y int) Point {
    return Point{x: x, y: y}
}
func (PointOps) Origin() Point {
    return (PointOps{}).New(0, 0)
}
```

Go 调用方使用 `(geometry.PointOps{}).New(1, 2)`、`(geometry.PointOps{}).Origin()`。carrier 不保存实例，Go 仍按普通值与逃逸规则处理 Point。

### C02：返回共享句柄由签名显式表达

源级片段位于 `impl Point` 内；`Arc::new` 是待定义基础包构造入口的示意拼写，暂假定能将一个合法值放入共享存储：

```goxide
pub func shared(x int, y int) Arc[Self] {
    return Arc::new(Self::new(x, y))
}
```

对应 Go 片段，依赖 C01 的 Point/PointOps：

```go
func (PointOps) Shared(x, y int) *Point {
    value := (PointOps{}).New(x, y)
    return &value
}
```

每次调用新建该逻辑对象；复制返回句柄仍共享该对象。物理分配由 Go 编译器决定，不额外引入引用计数。返回 `&` 局部值成为 Arc 的合法性由明确构造操作规定，不能将任意短借用直接当 Arc 返回。

### C03：泛型构造保留类型实参

直接书写的 Go 泛型类型声明与泛型 impl；impl 的类型参数声明遵循 RFC 0031：

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
type PairOps[T any] struct{}
func (PairOps[T]) New(first, second T) Pair[T] {
    return Pair[T]{first: first, second: second}
}
// 在调用函数内：
// p := (PairOps[int]{}).New(1, 2)
```

无参数且仅结果含 T 的构造，也应生成已解析的类型实参；不能指望 Go 从左侧变量类型推导泛型调用。具有 goxide trait 约束的构造还需携带相应实现证据，不能统一擦成 any。

### C04：取函数值仍是普通函数值

```goxide
make_point := Point::new
p := make_point(1, 2)
```

对应 Go 函数体片段，假定调用方已导入 geometry：

```go
makePoint := (geometry.PointOps{}).New
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
func (PointOps) FromText(text string) Result[Point, string] // 仅签名示意
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

调用仍是 `MakePoint(1, 2)` 和 `p.LegacySum()`；不从 MakePoint 的名称或返回类型推导 `Point::make`，也不为 LegacySum 自动创建 trait impl。需要 Goxide trait 时，须按归属规则写普通有体 impl，必要时使用本地包装类型。原样保留指声明及执行语义，生成器可以正常格式化和组织文件。

### C07：impl 实例成员的 Self 与 self

以下为 `impl Point` 的实例成员片段，采用只含 int 字段的 Point 与值接收者展示绑定：

```goxide
impl Point {
    pub func translated(self Self, dx int, dy int) Self {
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
type r#Self int
func Echo(self r#Self) r#Self { return self }
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
| C-N2 | 同一类型定义两个同名 new，仅参数/返回类型不同 | P4 不重载，拒绝 |
| C-N3 | `make_point().new()` 被直接降低为 carrier 调用，漏执行 make_point | 拒绝实例式关联函数调用 |
| C-N4 | `var p Point` 自动执行用户 new 或 Default | 错误降低；初始化检查不隐式增加业务副作用 |
| C-N5 | 仅给普通 Go 构造函数换类型注解，就把可空返回变成非空 Arc | 必须有外部契约或真实检查，不能凭名字授权 |
| C-N6 | 生成的 PointOps 或其成员与已有声明同名 | 在导出规划时诊断，不能覆盖或不稳定地改名 |
| C-N7 | 仅因原生 Go 函数返回 Point，就自动把它注册为 Point::new | 不建立这种隐式关联；按原生函数调用 |
| C-N8 | 根据方法体是否读取 self 决定它是关联函数还是实例成员 | 拒绝这种推断；成员类别由声明决定 |
| C-N9 | 将 impl 中的 Self 换成生成 wrapper 类型，或把原生 Go 的同名参数也改成目标字段 | 错误降低；按源类型与符号绑定分别处理 |
| C-N10 | 生成的固有方法与直接书写的 Go 方法重名 | 按成员及导出冲突规则诊断，不能覆盖原生方法 |

声明构造函数不会禁止普通 Go 写 `Point{}`，也不会自动禁止 goxide 的合法结构体字面量。若某类型必须只能通过检查后的构造入口取得，需要另行规定字段可见性、合法初始化和公开边界，不能从存在 new 函数直接推导。

## 双向互操作

Go → goxide：impl 中公开关联函数通过 carrier 调用，如 `(geometry.PointOps{}).New`；本地类型的实例成员表现为按接收者规则生成的 Go 方法。直接书写的 Go 声明保留原入口。Goxide 下游从源声明恢复“属于 Point 的 new”、真实符号、签名、泛型及 unsafe 效果，不反向猜测任意 NewPoint 都来自 impl。

goxide → Go：原生 `native.NewWidget` 可继续作为包函数调用；若希望在 goxide 视图中组织为 `Widget::new`，应显式绑定真实函数并校验契约。外部关联声明的精确语法、作用域和冲突规则暂不纳入已确认子集，不自动向外部 Go 类型增加成员。

trait 的 `Default.default() -> Self` 仍需选择显式 impl，通过已选实现的 carrier 调用，不凭同名 inherent `new/default` 自动建立 Default。泛型静态 trait 调用的证据传递见 [RFC 0013](0013-derive.md)；没有实例的方法不强塞进运行时 marker 接口。

## 验收与未定项

验收覆盖具体及泛型 struct/enum 的关联函数、函数值、原生声明边界、Self/self 绑定、读写权限、同名成员选择和冲突诊断。关联函数见 `internal/lower/inherent_carrier_test.go`，enum 实例方法见 `internal/lower/inherent_enum_test.go`；Arc 构造仍属后续基础库范围。

struct 与 enum 使用同一源级成员查询。接收者候选按已有引用规则构造，同一候选先查固有方法，再查唯一可见 trait；歧义用 `x.m::<Trait>` / `Trait::m(x)` 限定。trait 实例业务仍位于固定 wrapper，enum 值 As 由接口声明和各指针变体转发；借用视图用 ForE 保存真实 owner。

carrier 与 enum 变体方法直接承载原业务体，保持 `defer` / `recover` 调用帧。enum 值和只读成员均操作绑定时的完整 E 快照；保存的方法值继续使用旧变体对象，owner 重绑定不改换其对象。nil 接口的方法选择按 Go 规则失败；typed-nil 可以形成 Go 方法值，调用时先检查接收者，再进入业务体。

带待定协议条件的成员只按实际请求生成扁平入口；普通无条件 Go 泛型方法保留泛型。既不建立条件专用 view，也不把只对部分实例成立的方法加入整个 E[T] / Variant[T] 方法集。公开 Go 使用者通过 Goxide 明确导出的普通函数使用这些条件能力。

## 普通库 Default

Default 是 std 的普通 trait，无接收者成员使用已选 impl 的静态入口；手写、声明宏与过程宏生成的实现遵循同一选择规则。

```goxide
use std;

struct Settings { Port int }
impl std::Default for Settings {
    func r#default() Self { return Self{Port: 8080} }
}
#[derive(std::derive::Default)]
struct Application { Config Settings }
```

调用 `std::default_value[Application]()` 保留字段业务默认值；普通 `var` 初始化不调用 Default。条件泛型、跨包、透明 alias 和冲突按普通 trait 规则处理；普通同名函数不会取得协议身份。Default 方法可以像普通函数一样递归，不保留 Default 专用调用图禁环。协议前提循环和无法有限展开的实例需求仍由通用检查诊断。

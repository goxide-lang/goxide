# 研究 RFC 0007：以 pub use 为先的模块、目录与 Go facade 可行性

## 状态与范围

**结论：`pub use xxx::*;` 的公开 API 聚合可做，不需要先实现完整 Rust `mod`。** 建议先建立保留声明身份的 re-export 图，在现有 Go package 目录模型上交付有限集合；模块树、受限可见性和源目录重排另行分阶段。这里是研究建议，**不是已批准语言语法，也不是已实现能力**。

用户已确认的目标是 `pub` / `pub(scope)`、`use` / `pub use`、`mod`，允许考虑 TS 风格目录/路径导入导出；最新优先级是聚合公开 API，尤其 glob 重导出。一个项目 0 或 1 个 lib、多 bin、多 example 仍是工具链目标，精确入口格式未冻结。第一版 feature/manifest 草稿已暂停，不作为本 RFC 的前提。

验证基线：`3098a30e8e5fcb4696687246b2f39988218f4006`，真实工具链 `go1.27.1 linux/amd64`。本分支只新增研究文档及隔离试验；没有生产解析器、模块加载器或可见性修改。27 个手写 Go 机制试验与 16 个绑定图模型测试通过；它们不证明 hgo 源码已可编译。

本篇不决定所有权、Copy、enum ABI、借用检查、公开函数 enum 参数检查或 trait 新语义。兼容边界承接 [RFC 0001](../0001-package-loading.md) 与 [RFC 0015](../0015-public-api.md)：hgo 的额外静态保证不自动约束普通 Go 消费者。

## 先解决的核心案例

下面是**候选 hgo 片段**。先复用 Go import 绑定包身份，避免现在猜定相对路径字符串语法；这里的 `use` / `pub use` 尚无实现。

```hgo
// api/facade.hgo — Go import path example.org/app/api
package api
import netimpl "example.org/app/internal/netimpl"
import model "example.org/app/model"

pub use model::*;
pub use netimpl::Client as Client;
pub use netimpl::dial as connect;
```

此例描述目标语义，不代表首片覆盖所有成员：对 `internal/netimpl` 范围之外的消费者，`Client` 可经facade类型alias访问，`connect` 若没有可验证的函数访问路由，首片应报 unsupported route，不能偷偷包装。

消费者只经 `api::Client`、`api::connect` 及 model 的公开名字查找。前端查找结果必须是**原声明身份**，不是复制类型/变量、生成另一个业务定义。一个私有路径中的公开声明可以经授权 facade 暴露，但私有声明或 `pub(crate)` 不能借 `pub use` 被提升为全公开。

需要区分两个输出承诺：

| 名称类别 | hgo re-export 绑定 | 普通 Go facade |
| --- | --- | --- |
| 非泛型类型、泛型类型 | 同一 DefID、原 Go 类型；使用合法公开 alias 路由 | `type Alias = origin.T` / 参数化 alias 可保类型及方法身份 |
| 常量 | 原常量与精确类型/数值信息 | `const Alias = origin.C`；不能先落为运行时变量 |
| 函数、泛型函数 | 可合法访问原函数时直接绑定；否则需要经验证的可访问路由，不能盲目导入 internal | Go 没有通用函数声明 alias；默认不宣称 facade 有透明同名函数 |
| 可变全局变量 | 理论上可直接重写到同一原存储，但需保留 facade 的初始化边 | `var Alias = origin.X` 是复制，不能作为实现；首片拒绝发布变量别名 |
| 宏 | 必须带定义单元/模块、卫生与公开性信息，不能只换字符串路径 | Go 无宏命名空间；首片不发布宏 re-export，保留后续协议位置 |
| 模块/包命名空间 | 可作查找节点，不是运行时值 | Go 包不能作为变量或别名对象导出；不生成一个含函数值字段的“包对象”冒充 |

**推荐首片**：hgo 端显式、别名和 glob 重导出公开类型、常量，以及具有合法 Go 访问路由的函数；Go facade 精确保留类型 alias 和常量。类型的 canonical 来源即使位于 internal，也可以通过合法公开 facade alias 使用；函数没有同样的 Go alias 语法，首片对无合法路由的函数明确报 unsupported route。普通 Go 函数继续从可访问的原包调用，或用户另行选择明确标为 adapter 的入口。可变变量、宏、命名空间整体导出先拒绝。glob 遇到未支持的公开成员必须列出错误和改为显式列表的建议，不能悄悄丢成员。

这个边界有真实反例：`defer origin.Recover()` 能恢复 panic；若 facade 的 `Recover` 只是调用 `origin.Recover()`，`recover()` 已不在被 defer 的函数内直接执行，结果不同。一般 wrapper 即便能内联也不能承诺相同调用栈、函数 PC 或零成本。泛型函数也不能写成 `var F = origin.F`。因此“自动 wrapper 等同透明 re-export”不能作为默认语言保证。

若产品目标要求 **Go 消费者也只用统一入口调用全部函数**，需要用户明确接受 adapter 语义，或把函数定义原本放在公开入口包、内部实现按普通调用分层。后者不是任意内部函数透明重导出；不能把这个代价隐藏掉。

## 官方依据与本仓库事实

官方规则用于划边界；本篇具体 lowering 结论还由下述机制试验支撑。

- Rust 的 `use` 是名称绑定，`pub use` 可以提供新的公开路径；循环或歧义的重定向有约束。[Rust use declarations](https://doc.rust-lang.org/reference/items/use-declarations.html)
- Rust 私有项可由所在模块及后代访问；受限公开范围可指定当前模块、父模块、crate 或祖先路径。公开路径与声明本身的公开范围需要分别判断。[Rust visibility and privacy](https://doc.rust-lang.org/reference/visibility-and-privacy.html)
- Rust `mod` 可内联或从文件装载，源文件规则不等于 Go package 规则。[Rust modules](https://doc.rust-lang.org/reference/items/modules.html)
- Go 导出由真实标识符和包边界决定；Go import 图不能有环；包变量和 `init` 的初始化遵循 Go 规则。别名不能授权向外部类型增加方法。[Go specification](https://go.dev/ref/spec)
- 参数化类型 alias 是保留泛型类型身份的机制；alias 不是定义新的包装类型。[Go generic alias explanation](https://go.dev/blog/alias-names)
- Rust `$crate` 指向宏定义 crate，但不取消目标可见性检查；不能把它理解为访问任意私有项的权限。[Rust macro hygiene](https://doc.rust-lang.org/reference/macros-by-example.html#hygiene)
- Go `internal` 的限制是导入者目录树条件，不等同于 hgo 的编译单元/模块可见域。[Go internal directories](https://pkg.go.dev/cmd/go#hdr-Internal_Directories)

仓库实际路径（均针对上述基线）：

| 位置 | 当前机制 | 对新设计的影响 |
| --- | --- | --- |
| `cmd/hgo/main.go`, `directory.go` | 文件/目录命令；目录走 CompileModuleWithOptions | 尚无 manifest、mod 根或多 target 构建图 |
| `internal/frontend/module.go` | 按源目录收集包、复制目录、按 import 顺序编译所有包 | 不能把该目录等同关系提前固化为未来模块 ABI；选 target 时也需缩为依赖闭包 |
| `internal/frontend/package.go` | manual/default、外部声明恢复、宏收集、包级声明共享 | 条件选择/模块图需先于这些阶段；不能在类型检查后删除代码 |
| `internal/macros/package.go`, `hygiene.go` | source-built registry；包路径与文件级导入；定义点 helper 校验 | registry 不反序列化，不能宣称跨外部依赖宏 re-export 已就绪；需 ModuleID/DefID 上下文 |
| `internal/typecheck/importer.go` | 真实 Go 导出数据、源闭包指纹、环境/race/tags | re-export metadata 的别名图/布局版本也须入指纹，不能只看 facade.go 文本 |
| `internal/frontend/module.go`, `internal/sourcemap/map.go` | 每文件 Go、map.json、诊断回映 | 重排后需显式 generated-file → source span 索引，不能从相同相对路径猜 |

## 三种主方案与折中

### A：一个编译单元内所有 hgo mod 合成一个 Go package

一个 lib 是一个单元，每个 bin/example 各是独立单元；不同单元之间仍是 Go package 依赖图。单元内不把 `mod` 边变成 Go import。

收益：函数递归、经指针间接的类型递归可以跨 mod；父子私有访问和 pub(scope) 可由前端统一表达；lib 的类型不会为每个消费者复制一份。代码生成保留多文件而非简单文本拼接，避免文件局部 import 混淆。

代价：需要真正的绑定器、成员解析和权限检查，不能只改首字母。假设两个模块各有私有 `secret()`，Go 合包后会认为同名私有方法属于同一包；接口满足关系可能被错误扩大。因此需要分开维护：

1. **DefID**：具体声明身份，别名和重导出不产生新 DefID。
2. **MemberKey**：私有成员身份域＋原拼写。若选择 mod 作为私有身份域，同 mod 接口方法和实现方法应同 key，不能每个 DefID 各自乱改名；不同 mod 的同名私有方法不能合并。
3. **Go package/type identity**：外部 Go 来源身份不能被重新归属；真实类型及方法所属包仍须保留。

字段、匿名 struct 结构身份、嵌入提升、interface 隐式满足、方法表达式都要应用一致规则。物理同 Go 包使“给另一个 mod 的类型增加方法”在 Go 中可能合法，但是否允许仍是源语义问题，不能让 lowering 偶然放行。

私有或 `pub(crate)` 符号可降为未导出 Go 名，外包 Go 不能命名它；但同包原生 `.go` 能访问该名，前端不能控制普通 Go 编译器。公开嵌套路径可经显式 Go 名称映射/元数据处理；Go 不会自动获得 Rust 命名空间。reflection、栈名和包名可能显示生成命名，需列为 ABI 可观察项。

**结论：理论可做，适合以后确实需要 Rust 风格 mod 语义时；不应为了当前 pub use 目标先付出全部成本。**

### B：每个 mod 单独生成 Go package

收益：更接近当前目录包、Go 工具、独立编译、原生测试与包初始化。公开类型 alias 很直接；每包私有方法身份由 Go 自然区分。

硬约束：父包引用子包、子包再访问父包形成 Go import cycle；即使只有相互引用的指针类型也失败。子包不能访问父包未导出函数，所以 Rust 的后代访问父私有项不能直接实现。为受限公开项生成大写 bridge 可让 hgo 通过，但普通 Go 也能调用 bridge；`internal` 只缩小路径范围，不等于 pub(super)/pub(crate)。

**结论：可做“Go 包规则下的 mod 外观”，不能无条件承诺 Rust 模块行为。** 若采纳，应明确拒绝循环与跨包私有访问，而不是事后自动改表示。

### C：显式目录/路径 import-export（TS 风格方向）

保持现有目录对应 Go package；先给名称绑定、逐项/别名/glob 导出表，路径规则另行定案。可以首先使用正常 Go import path 与 package alias，不要求新建 Rust 文件模块树。

收益：与核心 facade 需求匹配，能够复用现有 importer、metadata、目录构建；不必先实现整 crate 重命名。多个包可以重导出同一类型而保身份。

限制：如果每目录仍是 Go package，环、父子私有访问、函数/变量 alias 的限制与 B 相同。**改变路径语法不能解除 Go 后端约束。** 不能照搬 TS 的运行时模块 namespace、live binding 或循环加载行为。

**建议先交付这一层的 re-export 图，保留未来 mod 的空间，但不要称已实现完整 TS/Rust 兼容。**

### D：按强连通分量 SCC 自动合并（不推荐默认）

可消除部分 Go import 环，但新增一条反向引用就可能使组件合并，改变 import path、命名类型包身份、私有成员身份、init 顺序、metadata 和缓存。固定分区可以稳定 ABI，却退化成显式编译单元边界。若未来只用于完全内部实现且有稳定 API 层，还需单独证明跨分区类型与初始化等价；不作为首片捷径。

| 维度 | A 单元合包 | B 每 mod 一包 | C 目录/路径导出 |
| --- | --- | --- | --- |
| pub use facade 核心 | 能做，但先搭大框架 | 能做 | 最小改动路径 |
| 内部函数/指针类型环 | 可表达；值递归/初始化环仍不合法 | Go 禁止包环 | Go 禁止包环 |
| pub(scope) | 前端检查；同包 Go 可绕过 | 前端＋必要 bridge；Go 端限制不精确 | 首片只承诺现有包公开性 |
| 原生 Go 类型身份 | 要避免搬迁/复制类型定义 | 自然保留包身份 | 保留原目录包身份 |
| 源文件到产物路径 | 必须新建布局映射 | 显式一对一或重排映射 | 首片可保持现有路径 |
| 相对实现成本 | 高：绑定、成员、卫生、布局一起改变 | 中高：循环及作用域限制需产品取舍 | 较低但仍须新 re-export/metadata 图 |

## 重导出解析规则草案

以下都是建议，精确规则需评审；不默认采用 Rust 的全部遮蔽规则。

- `use p::T as U` 只新增当前模块绑定，`pub use` 新增公开路径；DefID 和类型身份不变。
- glob 只枚举来源的有效公开表，不能仅依据源码首字母，也不能把内部 metadata 全部当公开。
- 同名来自同一 DefID 的多条路径可合并；来自不同 DefID 的多 glob、显式＋glob、局部声明＋导出一律报歧义，要求显式别名。第一版不设计“最后一个覆盖”或导入顺序优先。
- 显式项不存在、不可见或越过受限公开域时定位到 use 项，并显示原声明和访问链。private、pub(self/super/crate/in ancestor) 的确切规则在完整 mod 阶段落实；不能由别名升级。
- 私有 mod 内的 `pub T` 与私有 `T` 不同：授权的公开 re-export 可绕开前者的私有路径，但不能扩张后者的声明权限。应分别校验原路径访问权、目标声明允许的公开域、新绑定的公开域。
- 传递 re-export 用有限集合不动点求公开表；纯 alias 环没有声明落点时报 unresolved/cycle。原型演示有种子 glob 环可收敛、空 glob 环为空，但**首片生产建议对 package 导出依赖环直接拒绝**，因为 Go import/初始化仍要求 DAG。图模型的收敛不是 Go 可编译性证明。
- 构建顺序对图排序，不依赖文件遍历次序。名称冲突输出需稳定排序，显示两个候选来源。
- 导出的类型签名中所有所需外部类型必须可表示和验证；不能通过 alias 伪造对 Go 未导出成员的访问。公开签名携带私有类型是否允许需沿用/另评现有 API 规则，不在此偷偷新增检查策略。

模型试验 `reexports.py` 只接收已解析、已授权的图；它没有 parser、作用域树、成员检查、Go DAG、宏协议，也不会生成 Go。它验证固定点/歧义/DefID合并行为，不冒充生产权限验证器。

## 类型、函数与状态的 lowering 对照

### RE-01：泛型/非泛型类型、常量

候选 hgo 输入（声明已由来源包验证，片段）：

```hgo
pub use origin::Box;
pub use origin::Item as Record;
pub use origin::Small;
```

手写 Go 机制示意，完整 facade 文件；`origin` 是独立 Go package：

```go
package facade
import "example.org/probe/origin"
type Box[T any] = origin.Box[T]
type Record = origin.Item
const Small = origin.Small
```

目标模块语言版本也必须验证；不能只检查实际编译器是Go1.27。为稳定发布，建议泛型alias产物要求go.mod至少声明Go1.24（当前hgo自身基线已是1.27）。实际试验显示此Go1.27.1编译器接受go.mod 1.23的泛型alias，拒绝1.22并提示需要1.23；这是本工具链观察，不得伪造为1.23必定拒绝，也不代表旧Go1.23无需实验选项就支持跨包泛型alias。正式默认支持见 [Go1.24发布说明](https://go.dev/doc/go1.24#language)。

测试验证与原类型直接赋值、`reflect.TypeOf` 相等、原方法仍可调用，参数化 alias 约束仍由 Go 验证。`type Record origin.Item` 会创建新定义类型，不能替代 alias。向外部 alias 添加方法被 Go 拒绝；不能在 facade 复制原方法/结构来绕过。

### RE-02：函数、可变参数、多返回

hgo `pub use origin::join` 应仍绑定原函数。候选 Go adapter 的形状是：

```go
// 片段：此 adapter 是新的 Go 函数，不是透明 alias。
func Join(prefix string, values ...int) (string, int) {
    return origin.Join(prefix, values...)
}
```

试验覆盖可变参数、多返回、泛型转发的普通调用结果；同时覆盖 `recover` 反例与不同 PC。函数值导出不能用可赋值 `var Join = origin.Join` 偷换成函数声明；它还无法表示未实例化的泛型函数。hgo 直接绑定可避免额外 adapter，但只限调用者可以合法访问的 Go 路径；metadata 同时记录 canonical DefID 与可访问 Go 路由，不能要求一定存在 `facade.Join`，也不能无条件导入 canonical 包。

### RE-03：变量与常量不可混为一谈

```go
// 错误降低的反例；不是推荐输出。
var Counter = origin.Counter
```

试验修改 origin.Counter 后 facade 快照不更新，地址也不同。map/slice 即使暂时共享底层对象，重新赋值的变量槽仍不同。get/set 函数可提供新的状态 API，但不能叫变量 alias；不使用 unsafe/linkname 绕过。

### RE-04：canonical身份与可访问Go路由必须分开

`origin/api` 可导入 `origin/internal/detail` 并声明 `type T = detail.T`。外部消费者使用 `api.T` 合法且保留原类型身份；若hgo把这个名字一律改写为 `detail.T` 并直接import内部包，会被Go拒绝。绑定记录应包含 `canonical=(detail,T)` 和 `route=(api,T)` 两种信息，route由当前消费者位置、Go internal规则和真实导出数据验证。常量也可以经公开const route读取。

函数的一种**有限研究备选**是由合法位置的facade返回原函数值，而不是包装调用：

```go
// origin/api包中的片段；detail包是它可合法访问的internal包。
func CatchFunction() func() { return detail.Catch }
func IdentityFunction[T any]() func(T) T { return detail.Identity[T] }
// 消费者：defer api.CatchFunction()()，以及api.IdentityFunction[int]()(9)。
```

试验证明这保留原函数PC且直接defer仍可recover；getter本身是新增API和额外求值/调用，不承诺零成本。普通Go调用形状也不同于 `api.Catch()`。泛型参数必须先由hgo源调用推导，再显式传给getter；不能指望 `GetF()(argument)` 从第二次调用反向推导第一个调用的T。

它不是通用桥：origin可以有未导出 `hidden`，导出的New返回hidden、F接收hidden。`origin.F(origin.New())`可通过推断编译，但facade写不出 `func GetF() func(origin.hidden)`。泛型约束若含原包私有方法，也不能复制同拼写interface到facade，私有方法身份已经不同。若原包提供可命名的公开alias/constraint/function type，可复用它们；不能擅改第三方依赖补协议，更不能导出所有helper或用linkname越权。首片暂不默认启用getter桥，先保留该机制证据供评审。

## 未来完整 mod 的最小草案（不作为首片阻塞）

若以后采纳 A，候选输入可以是：

```hgo
// src/lib.hgo — 单个 lib 的根；语法示意，尚不能编译。
mod model;
mod engine;
pub use model::Record;
pub use engine::run;
```

候选布局：

```text
source/                         generated module (import path unchanged)
  go.mod                          go.mod
  src/lib.hgo                     hgo_lib_000_root.go
  src/model.hgo                   hgo_lib_010_model.go
  src/engine.hgo                  hgo_lib_020_engine.go
  cmd/web/main.hgo                cmd/web/hgo_root.go
  cmd/cli/main.hgo                cmd/cli/hgo_root.go
  examples/demo/main.hgo          examples/demo/hgo_root.go
                                  .hgo.modules.json (candidate, versioned)
                                  per-file .go.map.json
```

这是映射示意，不冻结文件命名。每个 target 只显式装载根及 `mod` 声明闭包，禁止默认递归扫描任意目录。建议第一版 `mod x;` 只解析相邻 `x.hgo`，嵌套来自 `x/child.hgo`；`x.hgo` 与 `x/mod.hgo` 二选一还是只允许前者须决定，不能静默择一。自定义路径、inline mod、symlink越界规则后续；根路径始终相对 manifest，不相对 cwd。

候选 scope 取值：private 与 `pub(self)` 对当前模块及后代；`pub(super)` 对父模块子树；`pub(crate)` 对当前编译单元；`pub(in crate::ancestor)` 只接受真正词法祖先，不接受 use alias；`pub` 对外。这里的 crate 是 lib 或单个 bin/example，**不是整个含多个 target 的 manifest 项目**。因此 bin 经普通依赖只能用 lib 的公开 API，不能因同一项目绕过 pub(crate)。这些精确定义仍待用户确认。

bin/example 引用 lib 应导入唯一 lib 产物，而非把 lib 源码重新合入 main；同一进程中多条依赖路径仍只初始化同一 Go 包一次。不同可执行程序是不同进程，不承诺跨进程共享全局变量。0 个 lib 合法；多个 lib 声明应在manifest层报错。example 默认不进入 lib 的发布 API或默认二进制清单；明确选择才构建。`web` 只是一个正常命名 bin，无需例外。

## 宏、初始化、测试与 metadata

### 定义点路径与宏

宏 definition context 应携带 `(UnitID, ModuleID, DefID, import bindings)`；callsite捕获保留调用点身份。需要 `$crate` 类似的定义单元锚点，但具体拼写不在此冻结。重导出和依赖重命名不会重置锚点，也不授予私有访问。扩展后路径和成员还要走可见性检查；不能只在展开前查一次文本。

当前宏只在 source-built package registry 中恢复，外部反序列化宏暂不支持。首片拒绝外部宏 re-export，比声称普通 JSON 导出表已足够更准确。机制试验仅手写定义点 selector 并验证调用点同名函数不遮蔽它，不证明宏解析器已支持 mod。

### 初始化

C/B 保持 Go package 依赖初始化；hgo将 `facade::F` 直接重写成 `origin.F` 时仍须保留用户导入 facade 的初始化边，必要时以 blank import 保留。否则 facade 自己的 `init` 可能被漏掉。不能因最终函数定义在origin，就无条件消除facade import。

A 合包需要定义逻辑模块的确定性源顺序并为副作用变量与 init 建立顺序；Go变量依赖排序仍先于init函数。试验观察固定生成文件名下 `parent-var, child-var, parent-init, child-init`，这是该输出布局的事实，**不是已确认 hgo 模块初始化语义**。纯值递归类型与全局初始化环仍被 Go 拒绝，不因合包而自动合法。

建议首片不改变现有初始化规则；完整 mod 阶段单独评审顺序。SCC改变物理包边界也会改变初始化，不能只比较最终类型检查是否通过。

### go test、source map、缓存与发布

- C先保留原生包级 `go test` 与外部 `_test` 访问规则；A以后将hgo模块测试归属于同一单元，但仍应由前端限制源码访问，不能把生成的Go同包测试权限作为hgo授权依据。
- 生成文件必须有独立源映射，标记逻辑模块/target/原文件范围。`//line` 可改善编译错误位置（机制试验已验证），但不能代替宏展开栈、精确 span、混合原生/生成文件映射，也要验证 runtime/debugger影响。
- metadata记录公开路径 → 原DefID、命名空间/种类/泛型参数、可见域、来源链、canonical Go身份以及一个或多个可访问Go路径/alias/bridge，另含来源closure及schema版本。enum/generic增强metadata须保留原声明归属，不能由facade重新定义身份。跨module恢复仍用真实Go类型核验；没有metadata的原生Go包只提供其真实公开API，不猜hgo私有语义。
- facade的源码可能未变，但来源新增公开项会改变glob结果；公开表摘要与来源closure均须入缓存键。未来key还包括单元/target、选中features并集、source layout版本、Go环境与toolchain、tags、test/race模式。不能只按生成目录或manifest名字缓存。
- 依赖Go包import path应稳定，不把本地绝对路径、Git缓存路径、feature临时目录或target hash写进公开类型身份。配置隔离在artifact存储路径和fingerprint实现，不改变同一声明的公开Go路径。
- 发布整个一致的Go源码＋必要metadata，不让依赖自己的replace成为下游必需条件。普通Go仍可直接构建无hgo metadata的实际Go源码；其增强检查边界不变。

## 可重复证据与反例

入口：[机制脚本](../../../tests/research/mod-feasibility/run.py)、[绑定图模型](../../../tests/research/mod-feasibility/reexports.py)、[本次结果](../../../tests/research/mod-feasibility/results.json)。脚本在临时独立module中使用真实Go，不下载依赖，不运行hgo生产编译器。

```sh
GOCACHE=/workspace/goxide-environment/cache \
GOMODCACHE=/workspace/goxide-environment/modcache \
python3 tests/research/mod-feasibility/run.py \
  --go /workspace/goxide-environment/go/bin/go \
  --report /tmp/mod-feasibility-results.json
python3 tests/research/mod-feasibility/reexports.py
python3 scripts/check_design.py --docs-only
```

| 试验名 | 观察 | 证明范围 |
| --- | --- | --- |
| merged_recursive_identity | 函数互递归、互指针类型通过race测试 | 合包后Go机制可承载；未证明mod解析 |
| split_import_cycle | Go拒绝import cycle | 每mod一包不能一般支持同样环 |
| merged_infinite_value_type | Go拒绝非法递归值类型 | 合包不是任意递归都合法 |
| split_parent_private | 子包不能调父包私有函数 | Rust后代私有访问不能直映Go包 |
| merged_private_interface_collision | 朴素同名误匹配；域编码后不匹配 | 需要MemberKey；未覆盖完整embedding算法 |
| reexport_alias_generics_methods | 参数化alias赋值/反射/方法身份保持 | 真实Go泛型alias可用 |
| alias_cannot_add_external_method | Go拒绝为非本地类型添加方法 | re-export不是方法所有权迁移 |
| reexport_function_and_variable_limits | 快照/函数var/wrapper行为区别 | 这些都不是通用透明alias |
| merged_native_same_package_bypass | 原生同包Go能访问mangled私有函数 | hgo作用域不能由Go同包强制 |
| external_go_private_rejection | 原生外包Go不能访问未导出函数 | Go包级私有仍有效 |
| internal_is_not_crate_privacy | allowed subtree内别的包可调用bridge | internal不是pub(crate) |
| initialization_and_shared_lib | lib初始化一次；2 bin＋1 example可构建 | 同import path共享单份lib身份/进程状态 |
| initialization_cycle_still_invalid | Go拒绝初始化环 | 不承诺循环加载live binding |
| definition_site_binding_vs_shadow | 定义点绑定不被调用点同名局部遮蔽 | 仅手写降低机制，不是宏实现 |
| facade_variadic_multireturn_constants | variadic、多返回、泛型转发、常量保真 | 普通调用可适配，不证明wrapper完全透明 |
| wrapper_changes_direct_recover | direct defer可recover，wrapper不能等价 | 自动wrapper有真实语义反例 |
| generic_function_not_variable_alias | 未实例化泛型函数不能绑定var | 不可用函数变量替代泛型alias |
| canonical_internal_path_is_not_accessible_route | 外部直接import internal被拒绝 | canonical身份不等于合法生成路径 |
| internal_alias_and_function_value_bridge | alias路由合法；getter保原函数PC与recover | getter仅适用于签名可表达的函数，并非默认生产方案 |
| facade_initialization_retained | blank import保留origin→facade初始化且各一次 | 正确路由也需保初始化边 |
| facade_initialization_lost_when_import_dropped | 只调origin会漏掉facade初始化 | 不能只改selector就删导入 |
| generic_alias_go123_observed_acceptance | 本Go1.27.1实际接受go.mod 1.23 | 不冒称此版本应被编译器拒绝 |
| generic_alias_go122_language_rejection | Go拒绝1.22的泛型alias | 工具链版本不等于模块语言版本 |
| hidden_signature_direct_call_inference | 可不命名hidden而直接调用 | 函数可用不等于其完整签名可书写 |
| hidden_signature_getter_rejected | getter不能命名origin.hidden | 函数值桥非通用 |
| private_constraint_cannot_be_textually_copied | 复制私有方法constraint不满足原约束 | 不能复制AST假冒原类型/成员身份 |
| source_line_diagnostic | 错误定位到原hgo路径/41行 | //line可桥接，完整source map仍需实现 |

绑定图模型16项覆盖显式alias、传递glob、同DefID合并、不同DefID歧义、显式不静默覆盖、private过滤/拒绝、局部冲突、无落点alias环、有种子glob环、空环、顺序无关及Go facade分类。模型没有源码解析、受限可见域或Go输出检查，不能据此宣称`pub use`已经实现。

## 分阶段建议与兼容路线

1. **现在（本分支）**：RFC＋机制反例＋独立review；不修改生产语义，保留已暂停工具链草稿供后续重新设计。
2. **最小re-export闭环**：现有目录/包边界不动；在早期收集阶段加入公开绑定图，支持类型/常量/函数的显式、别名、glob；hgo函数在合法路径上直接绑定，无合法路由明确拒绝；Go只发布已定义的精确alias/常量。首先测两个独立module、native Go importer、metadata stale与facade init。是否提供Go函数adapter先作产品决策。
3. **入口与依赖图**：建立与物理目录分离的TargetID/SourceUnit；0/1lib、多bin/example按需闭包。feature选择先于宏/声明收集；然后恢复manifest/依赖源/lock/offline设计，不能继续沿用“入口一定是目录”的草稿假设。
4. **完整mod（若仍需要）**：确认编译单元、private后代访问、pub(scope)、成员身份、初始化顺序后，选择A或受限B；新增真正resolver/布局映射，不能靠正则改名或目录复制伪装。

旧的无mod `.hgo` + 原生 `.go` 项目继续使用现有Go包模型，不因出现同名新关键字或manifest就自动重分包。新的模块布局应显式选择版本/模式。阶段2里现有Go包不能以后被自动吸收到mod合包单元，否则包身份和初始化会变；应通过显式迁移工具及API映射升级。稳定的hgo公开路径、DefID/Go来源映射可保大部分调用源码，但**不能承诺物理Go import path变化完全无破坏**。

需要决定的三件事：

1. **首片函数重导出边界**：先保证具有合法路由的hgo透明绑定＋Go原包调用，还是进一步评审函数值getter桥或有明确语义限制的Go adapter？这是当前最重要取舍。
2. **名字歧义策略**：接受保守的“不同DefID一律报冲突”吗，还是希望显式项覆盖glob？后者需额外优先级规则和变更诊断。
3. **完整mod是否下一阶段必要**：若只为聚合API，推荐先做C；若还需要跨mod类型/函数环与Rust式私有访问，再选择A并确认上述scope/单元定义。TS式路径语法本身不解决这些语义问题。

不需要现在一次确定完整Rust解析器、workspace resolver版本或高级target-specific feature；先把公开绑定和真实Go身份分离这一层做实，能减少后续返工。


## 本轮独立审查记录

独立只读review检查了RFC、基线代码与最终结果JSON；独立复跑16项绑定图测试、文档检查，先前独立Go复跑覆盖前21项，另单独复现hidden参数getter失败。最终27项完整Go记录由主研究运行生成；不冒称reviewer再次独立跑完27项。

修正项包括：MemberKey不能按每个DefID独立改名；canonical身份必须与合法Go路由分开；getter受完整签名可表达性和泛型推导约束；初始化边不能被alias重写删除；模型诊断稳定排序；同DefID不同kind视为metadata不一致；版本gate以真实结果记录。修正后无阻塞项。所有结论仍限于研究机制，生产前端尚未加入这些能力。

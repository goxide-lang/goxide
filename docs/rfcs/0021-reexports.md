# RFC 0021：目录包上的 use 与 pub use 重导出

## 状态与范围

用户已确认：优先 hgo 的透明重导出，不要求普通 Go 消费者具有完全相同的 facade 外观。变量不能通过复制实现；函数不能用会改变 `defer` / `recover` 的调用 wrapper 代替。

本片在已有 Go module / 目录包模型上实现 `use` 与 `pub use` 的逐项、别名、glob 绑定。没有新增完整 `mod`、`pub(scope)` 或普通声明的统一 `pub` 语义，也没有合入 Cargo/manifest 草稿。现有原生 Go 公开规则、`pub enum`、`pub macro_rules!` 保持各自既有语义。

研究依据与未选方案见 [研究分支 RFC 0007](https://github.com/hxsf/goxide/blob/29d81ecf06d7a9cfb383aa1e264a7bcd31d1fb6b/docs/rfcs/research/0007-modules-and-reexports.md)。本篇的实现范围优先于研究草案中的“尚未实现”状态。包发布协议承接 [RFC 0001](0001-package-loading.md)，Go 消费者不自动获得增强检查的边界承接 [RFC 0015](0015-public-api.md)。

实现位置：`internal/frontend/use.go`、`use_artifact.go`，接入包收集与 DSL 降低后的绑定阶段。宏模块仅增加公开名称查询，用于不支持项诊断；没有改宏展开/卫生语义。

## 源级规则

### 语法与绑定

```hgo
package api
import source "example.org/library/impl"

use source::Count;                  // 当前目录包内使用
use source::Add as sum;             // 当前目录包内别名
pub use source::Item as Record;     // 对 hgo 消费者公开
pub use source::*;                  // 枚举来源的有效公开表
```

- `source` 必须是同一源文件中显式 Go import 的包别名（省略alias时使用真实Go包名）。本片不猜目录相对路径，不装载 Rust 模块树。
- use声明位于包顶层。绑定属于该目录包，可由其他 `.hgo` 文件使用；import仍是文件局部。函数体内use、嵌套路径、分组导入和namespace整体别名不在本片范围。
- 使用绑定时沿用现有 Go/hgo 表达式：裸 `sum(...)` 或包选择 `api.Add(...)`。`::` 是本片 use 声明的路径分隔符；不新增通用 `api::Add` 表达式语法。enum 的 `E::Variant` 仍按既有规则处理。
- 同名来自同一声明且真实 Go 类型身份一致时去重；任何不同声明/类型冲突均诊断，显式项不覆盖glob，声明顺序不影响结果。别名必须避开现有包级声明与文件import名。
- `use` 不授权外部hgo包访问该绑定；`pub use` 才增加公开路径。原生Go仍只见实际导出符号。当前private类型/常量use会产生Go alias供包内使用；若alias首字母大写，普通Go能看到它，但hgo通过metadata拒绝外部访问，不能声称Go强制这种增强隐私。
- 带use metadata的包不支持hgo dot import，以免绕过绑定与私有检查；原生`.go`不受这项增强限制。
- 包循环（包括导出依赖环）仍拒绝，不能用re-export绕过Go DAG或初始化规则。

### 类别与首片限制

| 类别 | 当前实现 |
| --- | --- |
| 类型、参数化类型 | 真实 Go `type Alias = source.T`；保留方法、类型及实例化身份。公开alias名目前必须是Go导出名。泛型约束若有私有方法、第三包类型或引用类型参数（例如`S ~[]T`）的依赖约束，应使用显式原生Go alias；不复制私有成员身份。 |
| 常量 | 真实 `const Alias = source.C`，保持typed/untyped及精确常量值；公开alias名目前必须是Go导出名。 |
| 函数、泛型函数 | hgo引用直接绑定原Go函数selector，适用于直接调用、函数值、泛型实例化、可变参数、多返回及defer。不生成wrapper/可赋值函数变量。Go消费者从原包调用。 |
| 可变变量 | hgo读、写、`++`、取地址最终访问原变量；不复制变量槽，不额外提供并发安全。Go消费者从原包访问或使用用户显式提供的accessor。 |
| 具体enum | 公开类型alias接入既有enum metadata传播；验证了经facade的构造与match。已有internal-origin enum语法限制继续成立。 |
| 宏、未具可命名Go类型的内建泛型家族 | 本片不重导出。来源公开宏/家族已知时glob明确列名报错，不静默漏掉；使用显式支持列表。外部旧产物没有宏协议时，不猜测其不存在的宏导出。 |

普通Go包的公开表来自真实`go/types`，增强包的公开表再由验证后的metadata增补/限制。类型判等使用Go的真实类型身份，不用名称字符串模拟：`Box[int]`、`Box[string]`及实参中不同包的私有结构字段/接口方法必须区别处理。元数据保留真实identity route以恢复类型参数身份。

## 转译示例

### USE-01：变量是同一存储，函数仍是原函数

假定 `impl` 原生包提供 `var Count int`、`func Catch(){ _=recover() }`。

```hgo
// api/api.hgo
package api
import impl "example.org/demo/impl"
pub use impl::Count;
pub use impl::Catch;
```

```hgo
// client/client.hgo
package client
import api "example.org/demo/api"
use api::Count as Counter;
func Increment() { Counter++; api.Count = Counter + 1 }
func Handle() { defer api.Catch(); panic("example") }
```

Go输出的行为形状如下（片段；实际生成import名会避免所有源标识符冲突）：

```go
package client
import _ "example.org/demo/api" // 保留facade初始化
import origin "example.org/demo/impl"
func Increment() { origin.Count++; origin.Count = origin.Count + 1 }
func Handle() { defer origin.Catch(); panic("example") }
```

API包通过普通Go blank dependency保留impl初始化。每进程同一个Go包只初始化一次；hgo导出表不是第二份runtime实现。

### USE-02：类型、泛型、常量

```hgo
package api
import impl "example.org/demo/impl"
pub use impl::Box;
pub use impl::Small;
```

生成包中的独立 `hgo_use_aliases.go` 形状（完整片段，要求impl提供相应定义）：

```go
package api
import source "example.org/demo/impl"
type Box[T any] = source.Box[T]
const Small = source.Small
```

将alias放入独立Go文件，避免假装现有hgo enum语法降低器已支持用户泛型type声明。公开类型alias和常量可由普通Go直接使用；函数/变量只在hgo导出表中，不伪造同名Go入口。

### USE-03：internal合法路由

`producer` 可从 `producer/internal/impl` 重导出类型T，并生成 `type T = impl.T`。外部hgo消费者走 `producer.T` 的合法alias路径，原类型身份不变。

同一internal包的函数F或变量V没有Go原生透明alias。外部消费者如果无法直接访问其原包，本片报 `unsupported ... internal ... route`，不生成会改变recover的函数wrapper、变量副本或unsafe/linkname桥。用户可以提供公开的原生API；本片不偷偷改写第三方依赖。

## 拒绝与边界

| 编号 | 反例 | 判据 |
| --- | --- | --- |
| USE-N1 | 两个不同来源的Count经glob或显式导出占同名 | hgo报歧义及两个来源 |
| USE-N2 | 包级变量/类型/函数或文件import与use名称冲突 | hgo拒绝，不能由文件遍历顺序决定 |
| USE-N3 | 外部访问private use或以dot import绕过 | hgo拒绝；普通Go边界另述 |
| USE-N4 | 通过facade访问不可导入的internal函数/变量 | hgo明确不支持；不绕过Go限制 |
| USE-N5 | glob包含已知公开宏/未支持家族 | hgo列名拒绝，建议显式选择 |
| USE-N6 | alias环/包import环 | 包图拒绝，不声称支持循环模块加载 |
| USE-N7 | `Box[int]`与`Box[string]`合并 | 真实Go类型不同，拒绝歧义 |
| USE-N8 | 同拼写匿名结构私有字段或接口私有方法来自不同包 | 真实Go类型不同，拒绝歧义 |
| USE-N9 | 元数据缺失、marker不符、源或依赖变更、test/race/tags环境不符 | 拒绝恢复；不猜原声明 |
| USE-N10 | 单文件Compile、CLI文件模式或Playground使用use/导入use元数据包 | 明确要求目录/包编译；不降级为可绕过私有检查的Go视图 |

局部参数和局部变量可以正常遮蔽包内use绑定；新生成的原包import别名不会复用可能被局部变量遮蔽的源import名。属性键/selector成员不当成裸use绑定。当前裸use名出现在无法明确区分字段名与map key的具名composite literal中时会给出限制诊断，使用显式限定map key或不冲突的字段名；不能为了“通过”而重写错误的字段。

现有宏模板直接引用use引入的自由函数名、宏重导出、泛型enum家族别名等未建立独立卫生/家族协议；诊断不代表它们已可用。完整mod及pub(scope)仍另行设计。

## 双向互操作

hgo → Go：原生Go导出对象是声明身份基础；变量和函数始终沿合法原selector使用，因此可保持隐藏返回类型的Go推断能力，而不要求facade重新写出不可命名签名。源级alias不授予对Go私有对象的新权限。

Go → hgo：消费生成Go源即可使用实际类型/常量alias和普通业务函数；通过原包访问变量/函数。Go不理解`.hgo.uses.json`中的虚拟函数/变量入口，也不自动获得private use限制或其他增强检查。

metadata文件名`.hgo.uses.json`，Go marker为`HgoUseMetadata`，schema版本1。记录公开性、类别、canonical声明、合法Go route及类型identity route；不包含可执行宏代码。恢复校验marker、真实Go对象种类/类型/常量、精确selected source集合、source digest与Go importer的构建/依赖closure fingerprint。JSON必须仅含一个对象，读取有大小上限。普通模式产物不能被test/race模式默默复用。

source map在use移除及名称重写后继续回映原文件；生成alias文件是独立产物，不伪装原用户声明。重导出源目录与Go包仍同构；此片不预先固定未来mod/layout设计。已有生成Go+metadata再次进入目录流水线时保留use sidecar，并避免对已验证的相同enum marker重复声明。

## 验收与未定项

可运行例子：[examples/reexports](../../examples/reexports/README.md)。命令仍使用已有目录接口：

```sh
go run ./cmd/hgo run -go /path/to/go examples/reexports
go run ./cmd/hgo transpile -o /tmp/reexports-generated examples/reexports
```

使用真实Go1.27+，输出目录必须不存在且位于源module外。没有新增manifest或网络依赖命令。

`internal/frontend/use_test.go`覆盖多包/传递绑定、跨独立module local replace、可变变量/地址、defer/recover、泛型函数、类型alias、常量、init、private/unknown/conflict/cycle/macro诊断、internal正反例、真实泛型类型身份、局部import遮蔽以及具体enum/生成产物回读。生成工程实际运行Go test/race/vet，而不只对生成文本做快照断言。

本片不对任意泛型约束、宏、所有family或Rust/TS模块语义声称兼容。后续按明确metadata及合法Go路由增加支持；遇到无法保持语义的合法源需求，先给具体诊断和反例，不自动选wrapper或变量复制。

### 本片验证记录（2026-10-04）

执行环境为 Linux amd64、真实 Go 1.27.1（`/workspace/goxide-environment/go/bin/go`）。使用独立本地临时 module / replace fixture，`GOPROXY=off GOTOOLCHAIN=local GOWORK=off`，不需要网络依赖下载。

- `go test -race -tags hgoweb -count=1 ./...`：全树回归通过，包括 CLI、官网 embed 与验收集。
- 最后收紧依赖泛型约束诊断并补充独立 module 的泛型 alias / 隐藏返回类型案例后，另跑 `go test -race ./internal/frontend` 通过（27.607s）。
- `go vet -tags hgoweb ./...` 通过。
- `python3 scripts/check_design.py`：70 Markdown、904 本地链接/锚点、21 RFC；13 条真实 Go 机制命令（含预期失败）通过。
- `GO_BIN=/workspace/goxide-environment/go/bin/go python3 website/build.py` 通过，生成 WASM 和 5 个示例；单文件 Playground 明确不支持目录重导出，不宣称网页已支持该模式。
- 本文 CLI 示例实际输出 `shared 2 2 true`、`42 2 7`。
- 独立审查发现并推动修复局部 import 遮蔽、声明冲突、dot import 隐私绕过、生成产物 metadata 保留、泛型及私有结构类型身份、JSON 尾随对象校验问题；补充对应回归后审查结论无剩余阻断。

以上验证覆盖此目录包重导出切片；不代表尚未实现的 Cargo features、完整 mod/pub(scope) 或宏重导出已经通过验收。

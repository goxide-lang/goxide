# RFC 0028：crate 边界、库入口与标准库根

## 状态与范围

已确认方向：一个 `goxide.toml` 项目可含一个 lib、多个 bin/example；每个 target 有自己的 `crate::`，共享能力通过 lib 的公开 API 提供。保留依赖别名与 Go replace；只有未声明的根才查询标准库。删除所有 `namespaces` 映射字段，纯 Go 子包通过 `use alias::真实子路径` 解析；保留依赖 alias 根及原生 Go 模式的 roots 根声明。

实现状态：已实现。当前能力见[状态表](../mvp-gap-audit.md)，来源合同及实施流程位置见[依赖根设计](../design/dependency-roots.md)。本篇规定源级语义、Go 对照与回归要求。

本 RFC 补充 [RFC 0022](0022-project-toolchain.md) 的 target 合同、[RFC 0021](0021-reexports.md) 的路径根以及 [RFC 0025](0025-unified-project.md) 的配置边界；宏卫生沿用 [RFC 0011](0011-macros.md)。小项目无需为多个可执行入口建立 workspace，各 target 共享实际 Go 库包身份。

## 源级规则

### crate 根与库入口

| 源码所属 target | `crate::` 的根 | 引用本项目 lib |
| --- | --- | --- |
| lib | `[lib]` 选中的库根包 | 库内使用 `crate::…` |
| bin | 当前 `[[bin]]` 选中的入口根包 | `[lib].name::…` |
| example | 当前 `[[example]]` 选中的入口根包 | `[lib].name::…` |
| 没有 lib 的 bin/example | 仍为当前 target 的入口根包 | 没有自动库名入口 |

`crate::` 是源级静态路径根，不由依赖 alias、Go module path、包声明名或局部值重新绑定。增加或删除 `[lib]` 不改变已有 bin/example 内 `crate::` 的含义。本方案不提供 `project::`、字符串形式的 Go 路径 `use`、指向自身的 dependency 或任意目录挂载。

crate 由选中的 target 根、显式 `mod` 图及其中原生 Go 导入的同 target 包依赖闭包确定，不是整个 module 的目录树。原生 Go 导入可以纳入当前 target 子树内的辅助包，但不能吞入独立嵌套 target 或任意库外目录；跨 crate 的库访问及外部依赖仍遵守各自入口规则。仅由 Go import 纳入的包不会自动成为 Goxide 的公开 namespace。`mod child;` 仍定位当前包下的 `child/`，`pub mod child;` 再公开这条入口。`crate::` 从所属根沿模块声明和重导出解析，补充根寻址，不新增 `pub(crate)` 或放宽已有跨包私有成员规则；`self::`、`super::` 不作为这次方案的前提。

本项目各 bin/example 自动获得 `[lib].name` 对应的库根，只能沿库的公开 `pub mod` / `pub use` 使用公开成员。该根不是 self dependency，不增加来源节点，也不自动导入整个库。库名与任意已声明的 dependency alias 重名时诊断，包括未激活的 optional 依赖；库因所需 feature 未满足而不可用时，访问报错，不转而查标准库。外部消费者继续使用自己清单中的 dependency alias，不受库作者本地库名影响。

用于自动库入口的 `[lib].name` 必须是合法的源 namespace 标识符，且不能使用保留根 `crate`；不把 `app-core` 等名称静默改写为 `app_core`。bin/example 的产物名称继续遵循现有 target 命名规则。

没有 `[lib]` 时不推断隐式库，也不把 `src/` 自动作为库根；单 bin 按自身模块树组织，多 bin/example 的共享代码通过显式 lib 提供。Cargo 可以自动发现 `src/lib.rs`，本方案不附带这项自动发现功能。

### Go 混编与包身份

- 同包 `.gox` 与手写 `.go` 直接使用彼此的包级声明，无需 `use` 或清单映射。共享包级声明，不共享各文件的导入别名；手写 Go 只读参与解析和类型检查。
- `mod` / `pub mod` 可以绑定纯 Go、纯 Goxide 或混合子包。Go 包成员按 Go 的导出规则处理；Goxide 新语法按 `pub` 处理，保留既定 Go 风格声明的可见性规则。`pub mod` 公开包入口，不把小写 Go 成员或私有 Goxide 成员变成公开。
- 目录 target 下，子包的 Go 路径由 `go_mod.module` 和其 module 相对目录确定。CR-02 中 `crate::native` 在库内对应 `example.org/demo/src/native`，server 的 `crate::http` 对应 `example.org/demo/cmd/server/http`。源别名、实际 Go 包声明名不同时，生成普通 Go import alias。
- `crate::` 和库名路径最终绑定实际叶子包，不为经过的每一级模块额外生成 import。经不同公开别名到达同一包时保持同一 canonical 身份；多个 bin 使用 lib，不复制库类型或生成转发包装。
- Go 的 `internal`、循环依赖和不可导入 `package main` 等规则继续成立。bin 子包可以引用同 crate 的兄弟包，但不能通过 `crate::` 导入 main 根中的函数或类型。需要反向共享的声明放入普通子包。
- crate 归属与跨 crate 可达性在项目构建图中检查，涵盖参与构建的 `.gox` 和手写 `.go`。同一个 Go module 不自动授予访问另一个 target 私有树的权限，也不能通过读取其生成 Go 绕开源级检查。脱离 Goxide 项目构建的原生 Go 消费仍使用 Go 自身的可见性规则。
- 宏模板中的 `crate::` 绑定定义点所属 crate；库宏在 bin 内展开后仍指向库根。捕获路径及显式调用点路径沿用既有卫生规则，不因最终生成文件的位置重新绑定。

目录包含关系不能代替 target 归属：即使 bin/example 位于 lib 目录下，它仍是独立入口；lib 的 `mod` 图不能吞入该入口或其私有树。同一包不能被分配给两个具有不同 `crate::` 环境的 target；共享包归属 lib，经库入口复用。

文件型 target 沿用既有入口选取与 Go 投影，不能因新增 `crate::` 自动把同目录其他入口纳入同包。文件型 bin/example 根投影到 `goxide_targets/<kind>/<name>`，文件型 lib 根投影到 module 根。根文件中的 `mod child;` 仍从原始源目录定位 `child/`，子包保留 module 相对原路径；例如 `src/lib.gox` 的根生成在 module 根，其 `src/model/` 子包仍对应 `<module>/src/model`。两个文件型 bin 即使位于同一源目录，也有各自的 `crate::`；需要共享的包归入 lib，通过库入口消费，不能赋予同一包两个 crate 身份。

生成包与普通包使用相同边界：目标专用输出进入该 target 明确可达的 `mod` 树；多个 target 共用的生成包进入 lib 树或独立依赖。module 根的 `gen/` 不会因为删除映射表而自动成为每个 crate 的可见包。

### 依赖别名与标准库 fallback

`[dependencies.alias]` 继续统一声明来源、版本、feature 和可选性；Go replace 政策照常生效。源码使用稳定 alias，改变获取来源或替换实现不要求把源码改成 Go 包地址。各 target 按实际使用产生导入；清单共享依赖声明不等于把 bin/example 的代码或依赖调用写进 lib，也不引入一套 target 专用依赖配置。

根解析采用以下顺序，并保持已有显式绑定冲突诊断：

1. 当前源作用域的显式静态绑定，如 `mod`、`use`；`crate::` 按上文固定根处理。
2. 本 target 可用的自动库根和清单中的 dependency alias。
3. 根名字没有被显式声明或绑定时，查询所选 Go 工具链的标准库。

**全量依赖声明决定是否占名，活动依赖集合决定是否可用。** 已声明但未激活的 optional 根、未满足 feature 的库根、配置错误、加载失败或缺少成员，都报告原错误，不 fallback。先确定根来源，再在该来源中解析完整路径；不能在 `encoding::json` 的第二段失败时换成标准库。

普通标准库使用可以省略清单项：

```goxide
use fmt;
use encoding::json;
use encoding::BinaryMarshaler;
```

`encoding::json` 是子包，`encoding::BinaryMarshaler` 是包成员。包目录与导出成员由真实工具链信息区分，不把所有 `::` 机械替换为 `/`，也不靠不断缩短路径试探可导入前缀；若子包与成员存在同名歧义，给出诊断。最终核验 Go 返回的 `Standard` 身份和导入限制。不会仅根据末段自动提供裸 `json` 根；需要简称时使用 `use encoding::json as json;` 或显式清单别名。

显式配置仍然优先，例如：

```toml
[dependencies]
fmt = { path = "../my-formatting" }
json = { go = "encoding/json" }
output = { go = "fmt" }
```

`use fmt;` 选择配置的项目，`use json;` 和 `use output;` 选择对应标准库包。这些别名不能作为“冗余标准库声明”删除。fallback 只提供候选根，源码仍须 `use` 建立绑定；不增加自动 import prelude。

构建脚本的 host 上下文继续独立。默认标准库名字不会把 target 的库名、crate 树、module dependencies 或执行权限透传给 host；helper 依赖、冻结输入及授权闭包仍按[脚本合同](0023-build-scripts.md)处理。本方案不重启脚本 CLI 或扩大其权限。

### 依赖来源与纯 Go 子包

依赖来源的单位保持为项目或 module：

- 外部 Go 依赖仍写 `go = "module-path"` 与 `version`；`go` 不改成任意 package 入口。标准库已有的 `go = "fmt"`、`go = "encoding/json"` 无版本别名声明保留。
- `path` 选择有自身 `goxide.toml` 或 `go.mod` 的独立来源根。Git 的 `subdir` 选择仓库内这样的根，不能拿它选择同一 module 的普通子包。
- path/Git 的 module 身份从该根的清单读取，可选 `module` 只作一致性断言；版本、replace、feature 与锁继续由清单和 Go 解析器决定。

例如：

```toml
[dependencies.wire]
go = "github.com/goxide-lang/serde"
version = "v0.0.0-20261009021520-f342a31e20d0"
```

```goxide
use wire;
use wire::json as json;
```

`wire` 选择 serde module；`wire::json` 在该来源内选择真实 Go 包 `github.com/goxide-lang/serde/json`，无需额外配置。源码可用 `as` 取文件内别名，也可由库的 `pub use` 组织公开 API，不再提供清单中的逐包改名表。

纯 Go module 的子路径按需解析：路径段可能进入真实子包，也可能选择当前包的导出成员；只有 Go 包查询成功且 `Module.Path` 证实属于已选来源时，才建立包绑定。中间目录和 module 根可以没有可导入包，这时仅承载继续寻址的路径，不可直接访问包成员或通过 `as _` 请求初始化。目标子包必须真实存在、可导入，并遵守 Go `internal`、构建条件和导出规则；不得因为路径前缀相同而跨入嵌套 module。

同一段同时命中子包和导出成员时报歧义，不按尝试顺序选择，也不将失败路径逐段缩短后重试其他来源。这里根据真实 Go 包及成员查询解析源码路径，不把整个目录树机械地公开或提前导入。

Goxide 源码来源仍从自身 `[lib]` 入口沿 `pub mod` / `pub use` 解析，包括该库显式公开的纯 Go 或混合子包。源级路径不可达时不转而猜测底层 Go 目录，不得借自动 Go 子包解析绕过库的私有边界。

### 根选择模型

输入是源路径 `p = r::tail`、源文件及其宏上下文 `H`、所属 target `T`、显式静态绑定 `B`、完整依赖声明 `D`、活动依赖集合 `A` 和所选 Go 工具链的标准库包信息 `S`。输出为真实包/成员的 canonical 身份，或带源位置的诊断。

```text
Root(r, H, T, B, D, A, S) =
  定义点 crate 根                 若 r = crate
  B[r]                            若 r 属于显式静态绑定
  本项目 lib 根或不可用诊断        若 r 是 T 的自动库名
  D[r] 的入口或未激活诊断          若 r 属于 D；是否可用由 A 决定
  S[r]                            若 r 未占名且工具链确认标准库候选
  未知根诊断                      其余情况

Resolve(p) = 在 Root(r, ...) 选定的来源内解析 tail 并检查公开边界
GoImport(Resolve(p)) = 实际使用的叶子 Go package path 与必要的 import alias
```

显式名字冲突先按既有规则诊断，不由上式的顺序消除冲突。`H` 决定宏路径的定义点或显式调用点；路径来源一旦选定，后续失败终止解析，不重新选择来源。目录存在不等于公开入口，Go import-only 辅助包也不能据此被 Goxide 跨 crate 直接访问。

### 配置收敛合同

本 RFC 的目标配置合同如下：

| 配置 | 目标合同 | 语义 |
| --- | --- | --- |
| 项目顶层 `[namespaces]` | 删除整项能力 | 当前 crate 内用 `mod` / `crate::`；bin/example 使用 lib 公共 API 时用库名。任意跨 target、库外目录跳转不再保留为隐式入口 |
| 顶层映射中的构建生成包 | 随顶层表删除 | 先使输出进入所属 target 的模块树；共享输出归 lib。不能仅删映射而保留任意 `module/gen` 的全局访问 |
| `[dependencies.alias.namespaces]` | 删除 | 纯 Go module 子包由 `use alias::真实子路径` 解析；来源、版本、feature 与 alias 根保留 |
| `[workspace.dependencies.alias]` 中的 `namespaces` | 删除 | 模板只继承依赖来源、根别名及既有 feature 合同，不再继承逐包映射；workspace 按自身阶段开放 |
| 默认同名标准库依赖，如 `fmt = { go = "fmt" }` | fallback 实现后可省略 | 仅限没有改名、覆盖、optional 或其他配置意义的声明；保留显式声明仍合法 |
| 显式标准库 alias 根、覆盖或 optional 配置 | 保留 | 声明具有占名和选择来源的语义；不能删除后悄悄改用默认标准库 |
| 原生 roots 的 `[roots.alias.namespaces]` | 删除 | 只保留根声明，真实 Go 子路径采用同一来源核验和成员解析规则 |
| 原生 `go.mod` 模式的 `goxide.roots.toml`、`[roots.alias]` 和 `--roots` | 保留 | 没有作者清单的模式仍需要外部 alias 根；版本与 replace 留在 go.mod，标准库简单根可由 fallback 省略 |
| host 工具链根、build helper 依赖上下文 | 保留独立职责 | 与 target 的顶层本地映射不是同一权限或依赖入口 |

外部 Goxide 库继续从自身 `[lib]` 的公开路径访问。自动 Go 子包解析不能指向本项目的 bin 私有树、跨来源冒充包，或绕过被依赖库的公开边界。path/Git 仍选择有独立清单的来源根，不把普通本地目录变成 self dependency。顶层、依赖项、workspace 模板及原生 roots 的 `namespaces` 字段统一删除，不以其他字段名保留等价逐包映射。

## 转译示例

### CR-01：同包 Go/Goxide 混编

以下三个文件都属于 `example.org/demo/src/native`。手写 `native.go` 保留原样：

```go
package native
func NativeValue() int { return 7 }
```

`value.gox`：

```goxide
package native
pub func value() int { return NativeValue() }
```

生成 `value_gox.go` 的核心：

```go
package native
func Value() int { return NativeValue() }
```

预期：Goxide 同包直接调用 `NativeValue()`；原生 Go 消费方通过普通 import 调用 `native.Value()`，两者返回 7。无同包 namespace 配置、导入或转发包装。

### CR-02：一个 lib、两个 bin 与一个 example

下面的文件是路径与导入片段，业务声明省略；不作为可独立运行的完整程序。

```text
demo/
  goxide.toml
  src/
    lib.gox
    model/model.gox
    service/service.gox
    native/math.go
  cmd/
    server/main.gox
    server/http/http.gox
    worker/main.gox
  examples/
    demo/main.gox
```

```toml
[go_mod]
module = "example.org/demo"
go = "1.27"

[lib]
name = "appcore"
path = "src"

[[bin]]
name = "server"
path = "cmd/server"

[[bin]]
name = "worker"
path = "cmd/worker"

[[example]]
name = "demo"
path = "examples/demo"

[dependencies.wire]
go = "github.com/goxide-lang/serde"
version = "v0.0.0-20261009021520-f342a31e20d0"

```

```goxide
// src/lib.gox：声明库的公开入口。
package appcore
pub mod model;
pub mod service;
pub mod native;
```

```goxide
// src/service/service.gox：兄弟包经本库根访问。
package service
use crate::model;
use crate::native as helpers;
```

```goxide
// cmd/server/main.gox：HTTP 实现属于 server，服务能力来自 lib。
package main
mod http;
use crate::http;
use appcore::service;
use wire::json as wirejson;
use fmt;
```

worker 与 example 都可以写 `use appcore::service;`。它们不能用 `crate::http` 指向 server 的 HTTP 实现；库也不能因为 server 与自己同 module 而引用这个入口专用包。库目录外的业务代码若需要共享，应纳入 lib 模块树，或成为具有独立 `goxide.toml` / `go.mod` 来源根的显式依赖；普通目录不能冒充 path dependency。

没有 `[lib]` 的小程序可以保留 `mod http; use crate::http;`，但没有 `appcore` 根。若将 `service`、`model` 改为该 bin 的专用实现，放入其模块树；需要兄弟包经根访问的路径在根声明 `pub mod`，兄弟包再写 `use crate::model;`。这些声明不把 bin 变成可供其他 target 消费的库。多入口共享时提取到 lib，通过库名消费，不把同一目录反复挂载给不同 crate。

在相应成员确实被使用时，生成的 Go 导入片段为：

```go
// example.org/demo/src/service
import (
    "example.org/demo/src/model"
    helpers "example.org/demo/src/native"
)
```

```go
// example.org/demo/cmd/server
import (
    "example.org/demo/cmd/server/http"
    "example.org/demo/src/service"
    wirejson "github.com/goxide-lang/serde/json"
    "fmt"
)
```

预期：worker 和 example 引用同一个 `example.org/demo/src/service` 包；server 的 HTTP 包只属于 server。经过库根的源路径不会额外生成对 `example.org/demo/src` 的 Go import，也不复制 lib 的类型。

### CR-03：标准库 fallback 与显式别名

没有同名根声明时，以下完整 Goxide 文件使用标准库：

```goxide
package app
use encoding::json;
pub func valid(data []byte) bool { return json::Valid(data) }
```

生成 Go 的核心：

```go
package app
import "encoding/json"
func Valid(data []byte) bool { return json.Valid(data) }
```

预期：传入 `[]byte("{}")` 返回 true。若清单将 `json = { go = "encoding/json" }` 显式命名，则源码也可以写 `use json;`，绑定同一个 Go 包。`use encoding::BinaryMarshaler;` 则绑定 `encoding` 包的公开类型，不尝试导入 `encoding/BinaryMarshaler`。

## 拒绝与边界

| 编号 | 输入 | 判定 |
| --- | --- | --- |
| CR-N1 | worker 访问 server 私有包，或 lib 通过原生 Go import 引入 bin 树 | Goxide 包图检查拒绝；同 module 不豁免 |
| CR-N2 | 同一源包属于两个不同 target 根 | Goxide 拒绝歧义归属；共享代码须归 lib 或独立依赖 |
| CR-N3 | 沿私有 `mod` 路径跨包访问，或访问私有成员 | Goxide 按已有公开规则拒绝；`crate::` 不提高权限 |
| CR-N4 | 已声明的 optional 根未激活，或显式根缺成员/加载失败 | 保留对应诊断，不 fallback 标准库 |
| CR-N5 | `[lib].name` 与依赖 alias 重名、不是合法源标识符或为 `crate` | 清单校验拒绝；不静默改名 |
| CR-N6 | 库外普通目录冒充 path dependency，子路径跨入嵌套 module 或绕过 Goxide 公开边界 | 项目来源与包入口检查拒绝 |
| CR-N7 | bin 子包反向导入 `package main` 根、包循环或非法 Go `internal` 导入 | 保留 Go 结构限制；Goxide 可提前报告，最终 Go 构建亦拒绝 |
| CR-N8 | 标准库或外部 Go 路径同一段同时指向子包和公开成员 | 名字解析报歧义；不依赖尝试顺序 |

所有 `namespaces` 字段从 schema 删除后按普通未知字段处理，不设置兼容分支。未提供清单 target 的单文件、目录和原生 `go.mod` 模式共用标准库 fallback 与真实 Go 子包解析，不推断作者 lib 或套用虚构的 crate 树。浏览器使用其编译器配套的标准库快照提供同一根解析语义。

## 双向互操作

Goxide → Go：同包直接引用手写声明，跨包通过 `mod`、库公开入口、依赖 alias 或标准库根选择真实 Go 包；外部纯 Go 子包由 alias 下的真实子路径解析。生成代码遵守实际 package path、Go 导出名和 `internal` 规则。

Go → Goxide：CR-01 的 `native.Value` 与 CR-03 的 `app.Valid` 都是普通导出函数；多个 bin 和原生 Go 调用方共享 lib 的实际包与类型身份。脱离 Goxide 项目构建的原生 Go 消费使用 Go 自身的导出规则，`crate::` 和源 namespace 不成为额外 Go API。

## 验收要求与范围外事项

下列行为由对应回归与端到端检查覆盖：正例须生成 Go 并真实 build/run，项目测试须真实 test；核对运行结果、Go 可读性和同输入两次生成字节一致。

- 一个 lib、两个 bin、一个 example 使用同一公开类型及行为；bin 专用包不进入 lib，跨 target 私有访问被拒绝，lib 不因增加另一个 bin 改变产物。
- 无 lib 的单 bin 使用 `crate::`；补上 lib 后该 bin 的 `crate::` 仍指原根。兄弟包沿根访问，包级私有成员与非法 main 根反向导入按既有规则拒绝。
- 同包 Go/Goxide 双向调用，纯 Go 与混合子包经 `mod` 访问；原生 Go 导入和宏展开不能绕过 crate 归属或改变共享库类型身份。
- 默认 `fmt`、`encoding::json` 与标准库成员导入；显式重命名、同名覆盖、未激活 optional、错误来源和缺少子项分别覆盖，显式根失败时绝不 fallback。
- 全部映射字段删除后的生成包布局与锁文件重放；依赖 alias 下的真实 Go 子包、标准库别名、原生 roots 根声明和 host 隔离仍按各自合同工作。
- 外部 Go module 的根包、深层子包和导出成员分别验证；module 根无包、仅中间目录无包、path/Git 来源、Go replace、子包/成员歧义及嵌套 module 归属分别覆盖。Goxide 私有目录不能通过同形 Go 路径绕过。
- CR-01 至 CR-03 覆盖同包、跨包和标准库正例；CR-N1 至 CR-N8 覆盖归属、权限、占名、来源和 Go 结构边界。
- 文件型 target 保持根入口投影，子 `mod` 按原始源目录定位并保留子包的 module 相对路径；多个同目录文件入口独立，重复 crate 归属拒绝，共享 lib 的包身份与产物保持一致。
- 单文件、目录、原生 Go 项目及浏览器使用一致的 fallback 规则；原生 Go import 闭包与宏定义点 `crate::` 分别验证。

workspace、import-go、vendor、构建脚本及发布功能的开放仍按各自阶段执行。

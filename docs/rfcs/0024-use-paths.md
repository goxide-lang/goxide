# RFC 0024：use 路径、依赖名与分仓标准库

## 状态与范围

**提案，尚未实现或批准语法。** 基线 `ae61ec628b55e0fc66ce96d47261c0a6809c0280`；本提交仅文档，不改 parser、resolver、metadata 或仓库布局。用户已批准分仓方向：`github.com/goxide-lang/std` 为稳定核心、`github.com/goxide-lang/next` 为实验库、`github.com/goxide-lang/serde` 为独立序列化库；serde 不属于 core std。本篇不声称这三个仓库已有下文的包、API、版本或发布产物。

[0021](0021-reexports.md) 是已实现的逐项/alias/glob 重导出合同；本篇补它尚未定义的包定位。另参考[研究 RFC 0007](https://github.com/hxsf/goxide/blob/29d81ecf06d7a9cfb383aa1e264a7bcd31d1fb6b/docs/rfcs/research/0007-modules-and-reexports.md) 的 C 方案；它不是本目录的 [0007 锁回调](0007-locks.md)。继续采用 Go 目录包及 import DAG，不通过路径语法、TS 式 re-export 或自动合包消除包环。完整 `mod`、`pub(scope)`、所有权、enum ABI 和公共 enum 参数校验不在本篇范围。

## 源级规则

### 读得懂的入口

下列 manifest 使用**假设的本地 checkout**，不要求下载不存在的版本。字段全部来自现有 manifest；源码中的简写才是本提案新增项。

```json
{
  "version": 1,
  "dependencies": {
    "std": {"module": "github.com/goxide-lang/std", "path": "../std"},
    "next": {"module": "github.com/goxide-lang/next", "path": "../next"},
    "serde": {"module": "github.com/goxide-lang/serde", "path": "../serde"}
  }
}
```

```hgo
// 拟议；假定这些真实包/公开对象由对应库发布。
package client
use std::option::Option;     // 包 github.com/goxide-lang/std/option，项 Option
use next::queue::Queue;      // 独立实验依赖，不从 std 内暗转发
use serde::json::Decode;     // serde/json 包的项；并非 std::serde
use "fmt"::Println;          // 原生 Go 标准库
use "net/http"::Client as HTTPClient;
```

上例还要求这些子包已有可消费的 Go 产物，或其 hgo 源码已进入该依赖 lib 的选中构建闭包；仅存在目录不够。首片保持 0022 的依赖 lib 闭包限制，消费方新写一条 use 不会自动扩建依赖中未被 lib 引用的 hgo 子包。库可通过 lib 的显式引用/facade 纳入所需包；未来按消费方需求扩展跨 module 包构建图须另案设计。本篇不会把假设的 std/option 示例宣称为任意 source-only 目录都可直接消费。

这些声明只引入终端名称，不同时创建 `std`、`option`、`serde` 包对象；使用 `Println(...)`、`HTTPClient`，不增加通用 `std::option::Option` 表达式语法。上例中的 Option 只展示路径及类型绑定；当前裸 alias 的 enum constructor 限制仍适用（见下文）。`std` 无特殊解析权：它是显式依赖键，可改名为 `core`；不会因编译器安装而隐式下载、导入或 glob。现有内建 Option/Result 的行为不由本篇迁移或删除；让独立 std 的公开类型替代内建名称需要另行验证身份和兼容性。

### 三种根，确定性解析

拟议首片 grammar 仅扩展 use 路径：`[pub] use ROOT::segment...::ITEM [as NAME];` 或 `ROOT::segment...::*;`。所有形式仍只能在包顶层。`ITEM` 为一个标识符；glob 不允许 `as`。不支持分组、导入包 namespace、任意表达式根、函数内 use。

| ROOT | 解析规则 | 示例 |
| --- | --- | --- |
| 同文件显式 Go import 别名 | **优先保留现有语义**；后面必须恰好一个项或 `*`，不得当目录前缀 | `import p "example.org/lib"; use p::X;` |
| 当前构建节点的活动 manifest 依赖键 | 除最后项外的 segment 一律为 Go 包路径后缀；最后项在精确包公开表查找 | `use std::option::Option;` → `module + "/option"` 的 Option |
| 双引号字符串 | 完整、精确的 Go **package import path**；后面必须一个项或 `*`，不是 module URL 或磁盘路径 | `use "example.org/lib/v2/net-client"::Client;` |

对依赖根 `use dep::X;`，包路径就是依赖的 `module`，不是 `lib.name` 或 `lib.path`。查找的是生成后的 canonical module 根包；若无该包，精确报“根包不存在”，不由 resolver 猜 src/lib。现有文件型 lib 的 projectLayout 可以将入口映射到生成 module 根，这是工具链既定布局，不是 use 的搜索规则。`lib.path` 负责工具链选择源码，`lib.name` 是 target 名，两者都不是包路径别名。`use dep::a::b::X;` 总是包 `module/a/b` 的 X；不先试较短包再把后缀猜作类型、namespace 或 enum variant。不做最长存在前缀探测，不随新目录出现改变绑定。

同文件 import alias 与依赖键重名时，旧 import 始终优先，即使指向不同包；嵌套写法报“此根为文件 import，不能追加包路径”，不回退依赖。诊断同时显示实际 import 和被遮住的 manifest 键，并建议重命名 import 或使用字符串路径。另一个文件的 import 不参与根查找。包级类型/变量/use 绑定不是路径根；它们仍参与终端名称冲突检查。根的解析和终端绑定是两个阶段。

manifest 现允许数字开头或连字符依赖键；首片只有合法、非关键字源码标识符键可用裸路径，**不把 `my-lib` 自动变为 `my_lib`**。旧 manifest 不因此非法，使用精确字符串包路径或显式 Go import 即可。字符串遵循 Go import path 规则；只接受解释型字符串且解码后无路径逃逸，不把字符串当模板、shell 或网络 URL。

每个依赖源码按**自己的** manifest/稳定选版图解析别名，不继承消费方的依赖别名。A 的 `std` 和 B 的 `std` 即使映射不同 module，也不会彼此改写。不同键指向同一 module/version 时保留同一 Go 包身份；键不是并装两版本的机制，选版、来源冲突及 feature 并集继续由 [0022](0022-project-toolchain.md) 处理。

### 字符串、原生 Go 与依赖激活

`use "fmt"::Println;` 等同已存在的 `import f "fmt"; use f::Println;` 的语义组合，最终仍调用真实 Go 对象。第三方精确包路径可来自当前 Go build list 或当前 module；由真实 Go 定位其所属 module，不能用简单字符串前缀猜所有权（嵌套 module、v2 都交给 Go）。没有增强 metadata 的包只提供真实 Go 导出对象。

字符串路径**不增加 manifest 依赖、不启用 optional dependency、不请求 feature、不授权 build script**。选中 source-only hgo 依赖仍需活动 manifest 边并按稳定图构建；纯 Go 包沿用当前 Go 依赖策略。locked/offline 模式不得因路径便利写法自动 `go get` 或更新锁。未激活的可选别名给出“依赖未激活”；不退回同名本地目录或 Go 标准库。`use fmt::Println;` 若无 import fmt 或名为 fmt 的活动依赖就报未知根。

### 可见性、冲突、shadow 与 facade

保持 0021：use 终端绑定在当前目录包共享，import 则在文件内；局部参数/变量可以遮蔽 use 绑定。不同声明占相同终端名称报错（包括显式与 glob），显式不自动覆盖 glob；同一 canonical 声明且真实 Go 类型身份相同才去重。包级声明/import 与终端绑定冲突沿用现有诊断，不受路径简写绕过。

```hgo
// 拟议 facade；假设两个来源均含允许重导出的公开 API。
package api
pub use std::option::*;
pub use "example.org/app/model"::Record;
// 私有项不会由 glob 公开；显式选择私有项报错。
```

glob 只枚举**一个精确包**的有效公开表，不递归扫描子目录；有效公开表中有不可用类别则整体列名报错，不静默跳过。`pub use` 只增加公开路径，不改变 DefID、类型、变量槽或宏定义点。局部 shadow 不改变发布表。对 hgo 私有绑定的检查仍依赖 metadata，不能声称普通 Go 也强制了所有增强隐私。`pub(scope)` 不借本篇落地。

具体 enum 类型/家族沿用已有 metadata；**不新增** `use std::option::Option::Some;` 的 variant 引入协议：按路径规则这会定位包 `std-module/option/Option`，通常报精确包不存在；诊断可建议使用已有限定 constructor，不能偷偷解释为变体。如果该精确包确实存在，则严格按包解释。已有 `import opt ".../option"` 后 `opt.Option[int]::Some(...)` 的 constructor/match 路线保留；裸 `use ...::Option as Maybe` 只保证已有类型位置能力，不宣称 `Maybe[int]::Some` 已实现。

源图内宏重导出继续使用原 registry、定义点 imports 和卫生；新路径先解析成 canonical 包，再进入已有宏协议。跨独立 module 的宏发布仍不支持，仅有 `.hgo.uses.json` 不足以恢复宏；显式宏项、含外部宏的 glob 都需类别化诊断。不能为了 `std` 好用隐式读依赖全部源文件或放开私有 helper。未来宏源码/AST发布需要另案含来源、依赖闭包、大小/版本及配置验证。

### crate / self / super 与目录边界

推荐本轮不新增或保留全局关键字 `crate/self/super`。没有完整 mod 树时，不把它们定义为磁盘父目录、manifest 根或某个 bin/example 的根；现有合法同名 import/依赖仍按普通根解析。引用本项目包用精确 import path 或现有 import，足以表达本轮 Go 工程场景。未来若加入 crate 锚点，须由显式版本/语法区分，不能重解释既有根。

package path 的 `/` 是 Go 逻辑路径，不是 cwd 相对文件路径。拒绝 `"./x"`、`"../x"`、绝对磁盘路径、反斜杠逃逸、`file://`、`https://` 及 `@revision`；Git URL/修订只在依赖源声明。manifest 的本地 path 仍相对 manifest；取得源码后继续执行既有 canonical/symlink/checkout 边界检查。只装载精确包及必要 import 闭包，不默认递归任意目录；嵌套 module 由 Go 决定，不能扫穿边界。任何路径形式都保留 Go internal 授权、包环和 init 规则。

## 转译示例

下面两种 hgo 形式应产生相同绑定及可观察行为（第一种已存在，第二种待实现）：

```hgo
package client
import f "fmt"
use f::Println as log;
func Main() { log("hello") }
```

```hgo
package client
use "fmt"::Println as log;
func Main() { log("hello") }
```

```go
package client
import hgoImport "fmt" // 实际生成名必须避让全部相关作用域
func Main() { hgoImport.Println("hello") }
```

通过 facade 选函数/变量时仍走合法原 selector，保留 facade 初始化边；`defer Catch()` 不能变成 defer wrapper，变量读写/地址不能变成 `var Copy = origin.V`。internal 来源类型可用已验证公开 Go alias 路由，函数/变量无法合法直达时仍报 unsupported route，不生成 linkname/unsafe 桥。只有真实类型/常量 alias 承诺普通 Go facade 外观。以上沿用 0021，不另创运行时 namespace 对象。

实现顺序建议：

1. 建立只读 `ImportEnvironment`（建议内部概念，非已存在 API）：每构建节点的活动依赖键→逻辑 module、稳定计划/config 身份；先完成 cfg 删除和依赖选版。关闭文件沿用 0022 的词法、括号 token-tree 与 package/import 前缀合法性检查，不执行完整声明/表达式语法检查，也不做包/名称解析。
2. 用结构化路径节点收集依赖、位置和原拼写，严格执行根优先级及精确包规则；在包图/import 排序前解析路径，不能等 prepareUses 或宏展开后才发现漏掉的构建边。注入 hygienic imports 后复用既有 use binder，保持 source map；移除 cfg 的项不激活包。此步骤排序当前编译单元中的精确包边，不扩大已经按 lib 选定的跨 module hgo 源闭包；目标子包未生成时明确诊断该限制。
3. 先交付显式字符串单项/alias，再交付依赖根及 glob，全部走同一身份/可见性协议；宏产生的 use 在先行首片明确拒绝，直到可重新规划包图的专门协议建立，不偷偷晚加载。源图宏消费既有 use 与生成 use 声明是不同能力；前者也只限 0021 已支持的宏绑定，宏模板自由引用 use 引入函数名等既有限制不变。
4. 上述回归通过再开放 CLI 项目/目录接口。无 manifest 的目录模式仍支持字符串及旧 import 根；单文件/Playground 当前 use 限制不随语法便利自动取消。不先开放 enum variant use、完整 mod 或外部宏。

## 拒绝与边界

下面是**提案验收表，未运行的新语法测试**。假定相关公开项存在，所有错误定位应含源 use 位置、解析根及精确目标，不打印凭据或缓存绝对地址。

| Case | 应有结果 |
| --- | --- |
| `use std::option::Option`，活动 std 指向 canonical std | 精确绑定 std/option 的 Option |
| 依赖键 core 指向同一 std，`use core::option::Option` | 与 std 别名路径同身份，缓存语义包含别名映射 |
| `use "net/http"::Client as C` | 原生 Go 类型及方法身份不变 |
| `import std "example.org/other"; use std::X` | 旧 import 优先，无语义迁移；若 X 缺失不回退依赖 |
| 同 import 下 `use std::option::Option` | 报 import 根不允许嵌套，建议改名/字符串 |
| 未声明 std、未激活 optional std、别名误拼 | 分类诊断，不自动 fetch/激活/扫描 |
| 精确路径存在但 source-only hgo 子包不在依赖 lib 闭包 | 报依赖子包未构建，建议从 lib API 暴露或发布一致生成产物，不自动扫源 |
| `use dep::X` 但只有 dep/module/sub 包 | 报根包不存在，不猜 lib target 或子包 |
| `use "../private"::X` / URL / 逃逸符号链接 | 拒绝路径/来源边界 |
| 两个 glob 的 X 来自不同声明，或同名宏与 Go 项 | 报歧义；不由遍历或声明顺序选赢家 |
| 两个 facade 都转发同一变量/类型 | identity 去重，变量地址相同，初始化一次 |
| `use p::private` / private use 被外部访问 | metadata/Go 公开性检查拒绝 |
| `pub use "outside/internal/p"::F` | 保留 Go internal 拒绝；合法类型 alias 路线分别测试 |
| 依赖 A→facade B→A | Go package DAG 拒绝，不模拟 TS 循环加载 |
| 外部 module 只有生成 Go/metadata 的宏 | 分类拒绝；同包普通类型可显式选取 |
| 函数内 use / 分组 / package-as-object | 首片明确不支持；局部变量 shadow 仍合法 |

## 双向互操作

发布产物记录 canonical Go package/object identity、合法 route、公开表及增强信息，不把消费方的 `std` 别名或本地 checkout 目录当公共身份。下游可用任何依赖键；库内部别名解析不可依赖下游 manifest。native Go 无 metadata 时只看实际 Go 表；hgo metadata 存在但损坏/不匹配时不得静默退为 native view 绕过隐私。

0021 的 marker/schema、source closure、Go 类型验证继续有效；新增路径解析输入需进入配置/产物与外部恢复缓存键：解析协议版本、每节点活动 alias→module 映射、稳定选版/来源、features、target（bin/example）、Go 环境/tags/race/test。绝不把绝对临时缓存路径写作类型身份。facade 未变但来源公开表/glob变化也要失效。若必须扩展发布 schema，明确 bump 或兼容字段规则并测试旧读者拒绝；本篇没有宣称现有 schema 已支持新输入。

实际 source-to-Go 定位须保留原 use span 与展开信息。字符串/依赖名只降低输入冗余，不重写第三方 module path、改变 init 顺序、提升 Go 私有权限或要求普通 Go 安装 hgo。发布生成 Go 的版本必须与 metadata/config 一致；source-only 与固定 Go 产物边界继续承接 [版本来源设计](../design/version-source-plan.md)。

## 验收与未定项

### 兼容、依据与必要检查

现有 `import p "module/pkg"; use p::X;` 永久保留本轮语义，无自动迁移。可提供显式 codemod：同文件只有该 use 需要 p 时合并为字符串形式；若还有 selector、宏定义点引用或 blank-init 需求则保留 import。迁移到依赖别名须证实所选 logical module＋package suffix 一致；不从包名或仓库 basename 猜别名。改名 std/core、切换依赖版本或不同 target 应能 A/B/A 重建，无 metadata 串用。

现有实现定位（基线，不代表下面新增路径已实现）：

| 文件/符号 | 本轮设计所依赖的事实 |
| --- | --- |
| `internal/frontend/use.go`: scanUses / prepareUses / rewrite | 当前仅 import 根；类型身份、route、shadow 与位置处理入口 |
| `internal/frontend/package.go`: preparePackageContext | use 准备与宏 registry/enum metadata 接入；新图边须更早确定 |
| `internal/frontend/module.go` | 目录包收集与依赖排序；不能用晚期文本替换绕过 |
| `internal/frontend/use_artifact.go` | 发布表、marker、受限读取与恢复验证 |
| `internal/project/manifest.go`, `planner.go`; `cmd/hgo/project.go` | alias 与 module 字段不同，稳定选版/features、预检和配置隔离 |
| `internal/frontend/use_test.go`, `use_macro_test.go`, `use_hygiene_test.go` | 存储、recover、shadow、宏/metadata、生成名称卫生的现有回归 |

未来实现的最小隔离验收：临时本地多 module/replace 和 fixture proxy，使用真实 Go，不访问三个新仓库或私人依赖。比较旧写法/字符串/两种依赖别名的执行、类型/变量地址及 init；测真实 `defer recover` 正反例。加入所有上表黑白 case、Go internal/环、带连字符键和 package name 与目录不同、依赖 lib 闭包内/外子包、嵌套 module/v2、未知可选依赖、禁用 cfg 名称不解析、不同节点同名 alias、glob 上游新增项、metadata缺失/损坏/旧版本、feature/bin/example/test/race/config A/B/A。用生成 Go 运行 race/vet；全量适用回归必须保持。文档检查不证明这些拟议语法已可编译。

官方参考（2026-10-04 查阅）：[Go import 声明](https://go.dev/ref/spec#Import_declarations)说明包名/路径与导入绑定；[Go module reference](https://go.dev/ref/mod)定义 module/package、选版和 replacement 边界；[Rust use declarations](https://doc.rust-lang.org/reference/items/use-declarations.html)仅用于对照命名绑定与 re-export，**本提案不照搬 Rust namespace/解析优先级**。Go internal 与真实路由反例已见 0021 和研究 RFC0007。本篇规则是 hgo 的设计建议，非官方 Go/Rust 已提供的语法。

### 最多三个待决定点

1. **路径入口**：推荐同时接受 `use dep::pkg::Item` 和 `use "exact/go/package"::Item`，保留 import 根优先及精确包规则；是否采纳这一套新增语法？可以先只实施字符串形式，成本较小但 std 示例需保留 import。
2. **prelude**：推荐本轮不新增 prelude，也不赋予 std/next/serde 隐式依赖或名字；若以后要 `Option` 免 import，单独确定显式 opt-in、保留名与内建迁移。本轮无需为 next/serde 开例外。
3. **mod 锚点优先级**：推荐暂缓 crate/self/super 和 enum variant use，优先交付 Go 包路径体验；若必须同时引入，应先单独冻结逻辑模块树/身份与兼容版本，不能把它们暂时当磁盘路径。

以上推荐均未被文档作者当作用户已批准；其余身份、存储、recover、Go DAG/internal、安全与配置隔离边界沿用已确认合同，不需重新选择。

### 本轮文档审查记录

独立只读 review 核对基线代码与 0021/0022，复跑文档检查；已修正关闭 cfg 的语法检查表述、依赖 lib 子包闭包边界，并澄清生成 module 根与宏模板限制。`python3 scripts/check_design.py --docs-only` 通过（81 Markdown、951 本地链接/锚点、24 RFC），`git diff --check` 通过。本轮没有新增可执行语法或生产代码，不以既有测试冒称新语法验证通过。

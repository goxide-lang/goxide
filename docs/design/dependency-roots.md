# 显式依赖根与 Go 包映射

`.gox` 通过 `use` 绑定静态路径，使用 `::` 访问 namespace、类型和关联项。依赖清单负责声明可用根；声明不会自动建立源文件绑定。原生 `.go` 文件继续使用 Go 的 import 和 selector。

模块选版与源名字映射是两个输入。Go resolver 继续选择 module 版本、replace、校验和与工具链；根映射只将源代码中的 alias 绑定到精确 package 和显式子 namespace。module path 不自动成为可导入 package，目录也不自动成为导出 namespace。

依赖图按来源身份建立节点。alias 是声明它的项目内的引用名，不决定节点身份；指向同一来源节点的活动边合并 feature 请求，不同来源保留独立节点。`go_mod.module` 只描述生成 Go module 时使用的路径，不参与 Goxide 来源节点的合并。生成 Go 前检查最终投影：不同来源若占用相同 Go module 路径，给出冲突诊断，不因 alias 不同而生成重复的 Go 类型身份。

来源节点键 `SourceKey` 与描述已取得内容的 `SourceID` 分开：

| 来源 | 节点键 | 内容与锁记录 |
| --- | --- | --- |
| path、根项目、workspace 成员 | 规范化来源根目录 | 本地内容可变；写锁时目录相对锁的 owner 根表示，允许 `../` |
| Git | 规范仓库地址、已锁定 commit、subdir | rev/tag/branch 只用于取得 commit；不同 ref 指向同一 commit 和 subdir 时合并，键中不含缓存目录 |
| Go module 版本来源 | 逻辑 module path | Go MVS 的选中版本、实际替换来源和校验和进入内容身份；节点键不含版本或下载目录 |

已取得的 Git 或 Go 源码内部若有 path 依赖，该子项目仍属于取得来源，以相对取得根的目录区分，不把缓存中的绝对目录变成本地 path 身份；原有 checkout 边界检查仍适用。

Go 来源经实际生效的显式本地 `replace` 指向某目录时，按该有效 path 归一，可与同目录的 path 请求合并。来源收集允许不同节点声明相同 Go module 路径；随后构造 Go 投影索引时检查唯一性，冲突发生在写临时 `go.mod` 和调用 Go 选版之前。后续选版改变取得内容时重新求解请求，不能把旧版本的 feature 或依赖留在新内容上。

## goxide.toml 项目

依赖来源由 Cargo 风格字段声明，`package` 与 `namespaces` 只描述精确导入绑定。完整 schema 见 [清单说明](toml-manifest.md)：

```toml
[dependencies.wire]
go = "github.com/goxide-lang/serde"
version = "v0.0.0-20261009021520-f342a31e20d0"
package = "github.com/goxide-lang/serde"

[dependencies.wire.namespaces]
json = "github.com/goxide-lang/serde/json"

[dependencies.standard]
go = "github.com/goxide-lang/std"
version = "v0.0.0-20261009104839-c02a09d8c417"

[dependencies.standard.namespaces]
sum = "github.com/goxide-lang/std/sum"
formatting = "github.com/goxide-lang/std/formatting"
runtime = "github.com/goxide-lang/std/runtime"

[dependencies.output]
go = "fmt"
```

`wire`、Go 包的声明名和 package path 可以不同。`standard` 没有 `package`，因此是纯分组；只有显式映射的子 namespace 可用。较深路径使用带引号的键，例如 `"formats::binary" = "example.org/codec/binary"`。所有 alias 和 namespace 段必须是合法的源标识符。

```goxide
package main
use output;
use wire as serde;

#[derive(serde::Serialize)]
struct Message { Text string }

func main() { output::Println("explicit dependencies") }
```

`git`、`path`、`go` 是互斥获取字段，`version` 配合 Go module；单独 version 的 registry 来源尚未实现。`go = "fmt"` 表示真实 Go 标准库，无 module/version 或下载，不接收 features/default_features。optional 仍由正常 feature 规则控制；内部加载校验 Standard，不用包名猜归属。

path/Git 从所选项目根的 `goxide.toml` 中 `go_mod.module` 或 `go.mod` 中 `module` 读取 Go 投影路径，显式 `module` 可选且只作一致性断言。Git 的 `subdir` 仍选择含自身清单的项目/module 根，不选择普通子包。

可选 module 依赖同样先经过 feature 解析；只有活动 dependency 构成根上下文。访问未激活根报错，不由 `use` 启用依赖。工具链依赖保留活动请求以提供根，但不参与下载、MVS 或增强模块构建。

workspace 的 `[workspace.dependencies.alias]` 是模板。成员以 `workspace = true` 显式继承来源字段、可选 module 断言、package 与 namespaces，不能在继承项上改写这些字段；需要另一映射时声明完整 dependency。模板不自动导入，也不自动激活成员依赖。

原生 workspace 迁入模式保留已有 Go policy。对外部 Go 包增加源根时，dependency 的 `go`/`version` 必须精确匹配该成员的 `go.require`，包映射不能引入另一选版要求。实际 MVS 仍可选中更高版本；本地供应源继续由 `go.replace` 指定。此类根只增加活动 Requests，不进入增强成员的 Sources/Children，也不接受 feature 转发。optional 控制根可见性，不删除既有 Go require。原生 workspace 成员之间仍使用显式 member path。

## 本模块的精确包入口

库目标、二进制目标和生成包可能属于同一 module 的不同 Go packages。它们使用作者清单内的明确映射，不为自己建立 dependency：

```toml
[namespaces]
greeting = "example.org/app"
generated = "example.org/app/gen"
"formats::binary" = "example.org/app/formats/binary"
```

源码以 `use greeting;`、`use generated;` 或 `use formats::binary;` 建立绑定。映射的预期 ModulePath 固定为当前 `go_mod.module`，实际加载仍须证明包归属；不能借本地入口映射其他 module。键的首段与任意 dependency alias 冲突时拒绝，包括未激活的 optional dependency。此表只属于当前包，不从 workspace 根继承，不增加下载或 feature 边。

这是精确 Go package 互操作入口；本地模块的 `mod child;` 声明仍独立存在。没有自动扫描整个 module 并导出全部目录的 `crate` 树，也没有隐式 self dependency。

## 保留 go.mod 的混合源码项目

没有 `goxide.toml` 的 Go module 可在 module 根放置 `goxide.roots.toml`。文件只描述根，版本与 replace 仍来自原有 `go.mod`：

```toml
[roots.output]
source = "toolchain"
package = "fmt"

[roots.codec]
module = "example.org/codec"
package = "example.org/codec/core"

[roots.codec.namespaces]
json = "example.org/codec/json"
```

不允许在此文件写 version/path/git 等来源配置。键名区分大小写，未知字段与错误类型直接报错。只有 root 和 namespace 名是用户定义字段。

```sh
goxide run --go "$GO_BIN" ./app
goxide run --go "$GO_BIN" ./app/main.gox
goxide transpile --roots ./chosen-roots.toml ./app/main.gox
```

目录与 Go module 内单文件默认只读取所属 module 根的映射。`--roots FILE` 选择唯一映射文件，不与发现到的文件合并，也不在所选映射失败后回退。

独立 `.gox` 从源文件目录向上找最近的 `goxide.roots.toml`，遇到 `go.mod` 边界停止；也支持 `--roots`。这只是名字上下文，不让根文件成为模块解析器。没有匹配 context 的未知 root 给出诊断，不猜 import path。原有独立运行的支持库配置继续由 `--runtime` 指定的工具链清单提供。

已有 `goxide.toml` 的项目和 workspace 使用 dependencies 映射，拒绝 CLI `--roots` 覆盖或同根的 `goxide.roots.toml` 双重配置。迁入作者项目后，模块版本权威仍在 TOML 内。

## 包身份、加载与锁

实际使用某个包时，frontend 检查所加载 package 的精确 path，并通过 Go 返回的 `Module.Path` 或 `Standard` 核验其 module/toolchain 来源。用相同前缀的嵌套 module 冒充父 module、把普通模块伪装成 toolchain、错误 package owner 都不能通过。没有按最长可导入前缀搜索的路径回退。

映射本身不加载包。未用到的 namespace 可以指向当前未加载的包；group 不递归导入所有子包。根在源文件经过 `use` 才能访问；原生 Go 包的声明名不能改变 alias 的含义。

来源字段、可选 module 断言、package 与 namespaces 作为 manifest 的组成部分进入 project、workspace、lock 和来源摘要。`--locked` 拒绝改变映射后复用旧计划。原生 Go module 的显式根上下文进入 frontend 配置指纹；不同映射不能复用相同编译身份。

共享解析器是 `internal/rootconfig`。CLI、直接调用 module frontend 的库用户和 Playground 都构造同一 `paths.Roots`；Playground 的根清单是显式应用输入，不是源语言内置包名白名单。

## Host 构建脚本

`.gox` 构建脚本与 host helper 的 lowering 只接收作者当前活动的 toolchain roots，沿用 `use` / `::` 语法。目标的 module dependency 和 `namespaces` 不透传到 host；这不会授予 target 依赖作为 host helper 执行的权限。工具链根映射进入脚本冻结输入、缓存及精确授权 subject 的身份。

原生 `.go` host 脚本继续通过现有 `build.dependencies` 显式 helper 快照使用 Go import。`.gox` host 源对 module 根的 `use` 需要未来独立 host namespace 输入；当前给出缺失根诊断，不从 target dependency 或 module path 猜 alias。工具链根之外的 host 映射在 runner API 也明确拒绝。

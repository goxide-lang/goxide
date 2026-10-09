# 显式依赖根与 Go 包映射

`.gox` 通过 `use` 绑定静态路径，使用 `::` 访问 namespace、类型和关联项。依赖清单负责声明可用根；声明不会自动建立源文件绑定。原生 `.go` 文件继续使用 Go 的 import 和 selector。

模块选版与源名字映射是两个输入。Go resolver 继续选择 module 版本、replace、校验和与工具链；根映射只将源代码中的 alias 绑定到精确 package 和显式子 namespace。module path 不自动成为可导入 package，目录也不自动成为导出 namespace。

## goxide.toml 项目

在已有 dependency 声明上增加 `package`、`namespaces` 和可选的 `source`：

```toml
[dependencies.wire]
module = "github.com/goxide-lang/serde"
version = "v0.0.0-20261009021520-f342a31e20d0"
package = "github.com/goxide-lang/serde"

[dependencies.wire.namespaces]
json = "github.com/goxide-lang/serde/json"

[dependencies.standard]
module = "github.com/goxide-lang/std"
version = "v0.0.0-20261009053434-9ed27c85088a"

[dependencies.standard.namespaces]
sum = "github.com/goxide-lang/std/sum"
formatting = "github.com/goxide-lang/std/formatting"
runtime = "github.com/goxide-lang/std/runtime"

[dependencies.output]
source = "toolchain"
package = "fmt"
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

默认 `source = "module"`，保留 `version`、`path`、`git` 的既有来源合同。`source = "toolchain"` 表示 Go 标准库，不进入 module 图；不得声明 module、version、path、git、rev、subdir、features 或 default_features，显式空值也不能改变这一限制。它可以有 `optional`，仍由正常 feature 激活规则控制。

可选 module 依赖同样先经过 feature 解析；只有活动 dependency 构成根上下文。访问未激活根报错，不由 `use` 启用依赖。工具链依赖保留活动请求以提供根，但不参与下载、MVS 或增强模块构建。

workspace 的 `[workspace.dependencies.alias]` 是模板。成员以 `workspace = true` 显式继承 source/package/namespaces，不能在继承项上改写这些字段；需要另一映射时声明完整 dependency。模板不自动导入，也不自动激活成员依赖。

原生 workspace 迁入模式保留已有 Go policy。对外部 Go 包增加源根时，dependency 的 `module`/`version` 必须精确匹配该成员的 `go.require`，包映射不能引入另一选版要求。实际 MVS 仍可选中更高版本；本地供应源继续由 `go.replace` 指定。此类根只增加活动 Requests，不进入增强成员的 Sources/Children，也不接受 feature 转发。optional 控制根可见性，不删除既有 Go require。原生 workspace 成员之间仍使用显式 member path。

## 本模块的精确包入口

库目标、二进制目标和生成包可能属于同一 module 的不同 Go packages。它们使用作者清单内的明确映射，不为自己建立 dependency：

```toml
[package.namespaces]
greeting = "example.org/app"
generated = "example.org/app/gen"
"formats::binary" = "example.org/app/formats/binary"
```

源码以 `use greeting;`、`use generated;` 或 `use formats::binary;` 建立绑定。映射的预期 ModulePath 固定为当前 `package.module`，实际加载仍须证明包归属；不能借本地入口映射其他 module。键的首段与任意 dependency alias 冲突时拒绝，包括未激活的 optional dependency。此表只属于当前包，不从 workspace 根继承，不增加下载或 feature 边。

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

source/package/namespaces 作为 manifest 的组成部分进入 project、workspace、lock 和来源摘要。`--locked` 拒绝改变映射后复用旧计划。原生 Go module 的显式根上下文进入 frontend 配置指纹；不同映射不能复用相同编译身份。

共享解析器是 `internal/rootconfig`。CLI、直接调用 module frontend 的库用户和 Playground 都构造同一 `paths.Roots`；Playground 的根清单是显式应用输入，不是源语言内置包名白名单。

## Host 构建脚本

`.gox` 构建脚本与 host helper 的 lowering 只接收作者当前活动的 toolchain roots，沿用 `use` / `::` 语法。目标的 module dependency 和 `package.namespaces` 不透传到 host；这不会授予 target 依赖作为 host helper 执行的权限。工具链根映射进入脚本冻结输入、缓存及精确授权 subject 的身份。

原生 `.go` host 脚本继续通过现有 `build.dependencies` 显式 helper 快照使用 Go import。`.gox` host 源对 module 根的 `use` 需要未来独立 host namespace 输入；当前给出缺失根诊断，不从 target dependency 或 module path 猜 alias。工具链根之外的 host 映射在 runner API 也明确拒绝。

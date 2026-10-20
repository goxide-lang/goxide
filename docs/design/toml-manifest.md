# goxide.toml schema

当前作者清单与可提交的 `goxide.lock` 位于项目根；Go 元数据和生成源码只写入受管 target。依赖字段参考 [Cargo](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html)，Go module 选版仍用 [Go MVS](https://go.dev/ref/mod#minimal-version-selection)。不读取旧 source 标签或嵌套来源表，不实现 registry。

## P5 新管线入口

状态：已完成。`internal/compile.PrepareProject` 接受单项目目录，使用既有清单、Go 依赖图和锁，选择 bin 后把所需模块交给 `parser → expand → resolve → check → lower`。返回稳定生成模块目录与 Go 入口包；调用者在该目录执行 Go build、run 或 test。CLI 三种输入模式在 P6 统一切换。

本阶段使用项目 `[go_mod]`、精确 namespace 映射、Go/path/Git 依赖及 bin；依赖库沿用 lib 路径。多包源和所选 Goxide 依赖联合检查，保留 enum、trait、宏和引用权限，不使用旧前端导出 metadata。原生 Go 源码、测试与资源参与生成模块；当前互操作支持 Goxide 跨包导入原生 Go，以及原生 Go 调用生成的公开 API。同包 Goxide 引用手写 Go 声明的桥接留待管线集成。清单作者根不生成 go.mod/go.sum。

依赖版本和校验和由既有 Go planner 锁定，生成模块间使用 bundle 内的相对 replace；临时生成目录不进入锁或最终 Go 元数据。相同输入保持生成字节、产物路径和锁稳定，offline/locked 使用已有缓存与锁重放，清单依赖映射改变时 locked 拒绝更新。失败的源检查不发布新锁或覆盖已有 bundle。

feature 条件编译已接入：支持 cfg、cfg_attr 顶层条件属性及 cfg! 布尔查询；cfg! 不跳过 if 两支的检查。合同见 [RFC 0022](../rfcs/0022-project-toolchain.md)。workspace、vendor 迁入、import-go、构建脚本仍按后续 MVP 阶段接入；下文保留这些工具链实现的既有字段合同。

## 最小项目与真实依赖

新目录放两个文件即可运行；需 Go 1.27+，首次允许联网：

```toml
# goxide.toml
[go_mod]
module = "example.org/hello"
go = "1.27"

[[bin]]
name = "hello"
path = "src"

[dependencies]
fmt = { go = "fmt" }
errors = { go = "github.com/pkg/errors", version = "v0.9.1", package = "github.com/pkg/errors" }
```

```goxide
// src/main.gox
package main
use fmt;
use errors;
func main() { fmt::Println(errors::New("clean dependency download")) }
```

```sh
goxide build                    # 自动下载声明的依赖；打印 target 中的可执行文件路径
goxide build --offline --locked # 同一输出路径；不改写相同生成文件或锁
goxide run --offline --locked   # clean dependency download
```

也可在项目子目录执行。`--go /absolute/path/to/go` 选择 Go 工具，须与构建 CLI 的 importer 版本一致。作者根不补 go.mod、go.sum，不手工 go get；依赖写入 TOML。

## 字段

| 表 | 字段与含义 |
| --- | --- |
| go_mod | 生成 go.mod 的元数据：必填 module、go；可选 toolchain（建议版本，不自动下载） |
| namespaces | 本项目的精确包映射；名称到 Go package 路径，不写在 go_mod 内 |
| dependencies.alias | 下述来源加公共 module/package/namespaces、features、default_features、optional；alias 是 use 的根名 |
| features | 名称到字符串数组；default 为默认集合，dep:alias 激活 optional，alias/feature 转发 feature |
| lib | 至多一项 name、path，可加 required_features |
| bin / example | 数组表，各项 name、path、可选 required_features；path 为项目内目录或单个 .gox/.go 入口 |
| go.require | 数组表 module、version、可选 indirect；生成原生 Go require，供已有 Go 政策和编译器支持库依赖使用 |
| go.replace | 数组表 module、可选 version，以及 path 或 with+with_version；保留 Go replacement 语义 |
| go.exclude / go.godebug | 数组表 module+version，或 key+value；沿用当前受支持的 Go 政策 |
| build | entry、inputs、outputs、env、timeout_seconds、dependencies、tools，见 [脚本合同](../rfcs/0023-build-scripts.md)；下载不授权执行脚本 |
| workspace | members、default-members、exclude、go_mod、dependencies，以及已有原生迁入 go 政策，见 [workspace 合同](../rfcs/0026-workspaces.md) |

作者清单不声明格式版本，直接按当前字段校验。`[go_mod]` 仅描述生成 Go module 的元数据；语言中的 `use` 绑定来自依赖 alias 或顶层 `[namespaces]`，不以 `go_mod.module` 作为源码导入名。未知字段、错误类型、非法 TOML 和大小写错误有文件/行/列诊断。来源语义错误指出依赖名。

## Feature 配置

`[features]` 同时提供模块内查询的声明目录与展开图。根模块默认启用已声明的 default，再加入 CLI 的 `--features`；`--no-default-features` 只取消根默认请求。依赖通过 `features`、`default_features` 和 feature 边接收请求，同一 module/source 的请求取并集，任何一条边要求默认集合即保留。根 feature 不按同名隐式传播到依赖，各模块的包共享本模块最终集合。

`dep:alias` 激活 optional 依赖，`alias/feature` 激活并转发，本地名称展开本模块 feature；不隐式创建 optional 同名 feature。请求及实际处理到的完整 cfg/cfg_attr/cfg! 谓词检查本模块声明，未知 feature 报错；外层 cfg 删除的子树、未选中的 cfg_attr 载荷不继续检查，普通 if 两支仍检查。`required_features` 在图求解后检查，不自动启用 feature。纯 Go 或固定生成 Go 依赖不能被请求重配 Goxide feature。

源码条件选择不反向修改清单图；非 optional 依赖和 go.require 仍按声明处理。产物配置摘要纳入每个 module 排序后的实际 enabled 集，不能只用依赖计划身份区分生成物；同配置请求乱序或重复不影响输出。纯 cfg 变化且依赖计划不变时保持锁文件字节稳定，可 --locked 重放。目标/测试谓词使用未来编译上下文，不添加到 `[features]`；当前实现和验收进度仍见[状态表](../mvp-gap-audit.md)。

## 编译环境与目标

[RFC 0022 的统一条件配置](../rfcs/0022-project-toolchain.md#统一条件配置)仅让 feature 读取清单配置。target 由工作流与 --target 决定，env 直接查询一次捕获的原始进程环境；不增加 `[cfg]`、env 登记或 target 清单字段，也不提供 --cfg-env。实际 env 读取记录在编译结束后进入产物摘要，不进入依赖锁身份。

## 来源与包身份

| 来源 | 声明 |
| --- | --- |
| Go 标准库 | `fmt = { go = "fmt" }`；没有 version，不下载，不接收 features/default_features；真实加载时核验 Go 的 Standard 标志 |
| Go module | `errors = { go = "github.com/pkg/errors", version = "v0.9.1", package = "github.com/pkg/errors" }`；go 是无协议 module path，version 是 Go 规范版本/pseudo-version；MVS 可选更高版本，lock 记录结果 |
| Git | git 为 HTTPS 仓库 URL；可选零或一个 rev/tag/branch；缺省默认分支，完整 commit 写入 lock；可选 subdir |
| 本地 | path 直接选项目根，相对声明所在清单；module 校验项目身份；本地字节不冒充不可变远端 checksum |
| registry | 单独 version，或 version 加 registry；明确报未实现，不猜 registry/Git URL |

git、path、go 互斥；version 当前只配合 go。Git/path 加 version 的 Cargo 发布回退不在本片内。多个 Git 选择器报冲突，没有优先级；rev 支持 commit hash/远端公开 ref，tag 与 branch 分别解析自己的 ref。普通构建沿用锁定 commit，不随移动 ref 更新。

Git/path 的 module 是项目身份；Go 来源由 go 给出 module，不重复 module 字段。package 是精确 import package，不是下载地址或仓库子目录；namespaces 显式映射其他包，没有 package 的 module 根只是分组，不自动导入整个 module。原生 .go 仍使用 Go import。

公开 std module 有 formatting，没有 fmt：Go 的 fmt 是标准库，不能当作该仓库中的独立项目。

```toml
[dependencies.formatting]
git = "https://github.com/goxide-lang/std.git"
rev = "c02a09d8c417b35dc7e432fc484172fab6db6182"
module = "github.com/goxide-lang/std"
package = "github.com/goxide-lang/std/formatting"
```

## monorepo 与发现

Git 的 subdir 只选仓库内有 goxide.toml 或 go.mod 的项目/module 根；省略选仓库根。它不选择同一 module 下的普通 package，不递归搜索或按 alias 猜目录，也不得逃出 checkout。path 已直接选根，不配 subdir。

可重放的 monorepo 示例使用本地 Git 夹具；其独立 Go module 位于 packages/lib，package 绑定对应该 module 的根包。Git 获取的 subdir 与包绑定仍分开，默认分支解析后锁定 commit：

```sh
work="$(mktemp -d)"
mkdir -p "$work/repo/packages/lib" "$work/app"
cat > "$work/repo/packages/lib/go.mod" <<'MOD'
module example.org/mono/lib
go 1.27
MOD
cat > "$work/repo/packages/lib/lib.go" <<'GO'
package lib
func Value() string { return "monorepo" }
GO
git -C "$work/repo" init -q -b main
git -C "$work/repo" add .
git -C "$work/repo" -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm fixture
cat > "$work/app/goxide.toml" <<TOML
[go_mod]
module = "example.org/mono/app"
go = "1.27"
[[bin]]
name = "app"
path = "main.gox"
[dependencies]
fmt = { go = "fmt" }
lib = { git = "$work/repo", subdir = "packages/lib", module = "example.org/mono/lib", package = "example.org/mono/lib" }
TOML
cat > "$work/app/main.gox" <<'GOX'
package main
use fmt;
use lib;
func main() { fmt::Println(lib::Value()) }
GOX
(cd "$work/app" && goxide run && goxide run --offline --locked)
```

真实远端依赖使用 HTTPS；绝对本地 Git 路径用于上述夹具与定向验证。没有对旧 monorepo candidate 的历史 ABI 提供兼容。

build/run/test 及指定输出目录的 transpile 无 INPUT 时，从 cwd 向上发现最近 goxide.toml；go.mod 不阻断外层项目/workspace。显式目录采用同一规则；显式文件保留单文件语义。没有清单时需显式指定 Go 目录或源码，不扫描目录猜 main。

默认原位 `gen` / `transpile` 按最近模块边界选择入口：遇到当前或上层 goxide.toml 时进入项目模式，先遇到 go.mod 时只生成当前 Go 包；嵌套原生模块不会被更外层 TOML 接管。

成员子目录选择最近成员，workspace 根用 default-members，`-p MODULE` 与 `--workspace` 显式改变选择。未列出的项目报非成员，不因目录位置补依赖。成员用 `workspace = true` 继承模板，只可追加 features 或设置 optional/default_features；模板 path 相对 workspace 根。版本与锁全 workspace 统一，未选成员 features 不自动加入本次编译。

## 锁、缓存与输出

goxide.lock 在项目/workspace 根，应提交。Git 缓存位于受管 target/cache/git；Go 缓存继续用 GOMODCACHE/GOCACHE。项目根不生成 `.goxide`；缓存位置不写成可提交的 Git 来源身份。删除缓存后可联网重取已锁 commit；offline 缺缓存明确失败。

`--target-dir` > GOXIDE_TARGET_DIR > target；相对路径以作者根为准。同一配置复用稳定 bundle 与二进制路径，更新变化字节、移除失效文件，保留相同文件的 mtime。临时生成视图结束后删除，不永久新增 run 目录。用户修改或未知文件阻止覆盖/清理，构建失败不发布新二进制。见 [target 布局](target-layout.md)。

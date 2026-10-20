# #23 项目构建与依赖：项目构建实施记录

关联 [Issue #23](https://github.com/goxide-lang/goxide/issues/23)。此文保留先方案、后实施记录。用户已确认 Go 字段和 Git 默认分支规则；本 PR 接续实现，当前合同与可重放示例见 [schema 文档](toml-manifest.md)。原暂停草稿仍保留在另一工作区。

当前根因：CLI 强制 INPUT，项目检测没有向上找最近清单；依赖的获取来源和导入绑定混在 `source/module/package` 中；每次构建保留新的 `runs/build-*`；Git 缓存及构建锁无条件创建项目根 `.goxide`。现有下载逻辑已经存在，实施时先用干净项目复现漏项，再修实际缺失的依赖获取链路。

## 已确认来源字段

参考 [Cargo 官方依赖文档](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html)，采用普通依赖表及 `version/git/path/rev/tag/branch` 字段，不引入 `source` 标签或嵌套来源 DSL。新增 `go` 与 `subdir` 是 Goxide 扩展，不是 Cargo 字段。Cargo 支持 version 配合 git/path 的发布回退；本片没有 registry/发布流程，建议暂不实现这种组合。

| 获取来源 | 本片合同 |
| --- | --- |
| registry | 单独 `version`，或 `version` 加 `registry`；明确报未实现，不猜默认 registry |
| Git | `git = "https://…"`，零或一个 `rev/tag/branch`；缺省使用默认分支并锁定 commit；可加 `subdir`，不同时声明 path/go/version |
| 本地 | `path = "../lib"` 直接指向项目根，不同时声明 git/go/version |
| Go module | `go = "github.com/…"` 加 `version = "v…"`；路径不带协议，按 Go MVS 选版并锁定结果 |
| Go 标准库 | `go = "fmt"`，没有 version，不下载；用所选 Go 工具链的真实包信息核验标准库归属 |

当前入口规则以 [schema](toml-manifest.md#来源与包身份) 为准：依赖项不接受 `package`。Git/path 从所选根的 goxide.toml 或 go.mod 读取生成 Go 所需的 module 路径，可选 `module` 只作一致性断言；Go module 来源的路径由 `go` 给出，不重复填写。alias 默认绑定标准库指定包、纯 Go module 根路径或 Goxide 自己必需的 lib 入口。RFC 0028 已删除全部 `namespaces` 字段，纯 Go 子包按 alias 下的真实路径解析并核验来源，Goxide 仍沿公开模块树；features/default_features/optional 沿用 feature 规则，标准库不接受 feature 请求。

底层依赖图按来源身份建立节点，alias 只是各声明项目内的引用名。同来源节点接收的活动 feature 请求取并集，不同来源独立求解。path 使用规范来源目录，Git 使用规范仓库地址、锁定 commit 和 subdir，Go 版本来源使用逻辑 module path；实际生效的本地 replace 归一到 path。节点键与选中版本、校验和等内容身份分开，细节见[依赖根合同](dependency-roots.md)。`go_mod.module` 仅供 Go 投影使用，不作为 Goxide 节点去重键；来源收集之后、临时 Go module 和选版之前检查不同来源是否占用相同 module 路径，并明确报冲突。

Go module 是一起选版和分发的一组 packages，import package 是其中一个包，见 [Go 官方模块说明](https://go.dev/ref/mod#module-path)。Go module 版本只接受规范版本或 pseudo-version，本片不实现 Cargo SemVer 范围。Git 的 rev 建议支持 commit hash 或远端公开 ref，tag/branch 分别按对应 ref 解析；多个选择器报错，没有隐含优先级。没有选择器时按 Cargo 使用仓库默认分支。首次解析后把完整 commit 写入 lock，普通二次构建不重新解析移动的默认分支、branch 或 tag。

真实来源例：本仓库 main 的 go.mod 已固定下述 std 版本，该提交有 `formatting` 包，没有 `fmt`。源码路径与版本已核对；Go module 与 Git 来源均已完成真实首构建和锁定二次构建。

```toml
[dependencies]
fmt = { go = "fmt" }

[dependencies.standard]
go = "github.com/goxide-lang/std"
version = "v0.0.0-20261009104839-c02a09d8c417"
```

同一真实 formatting 来源也可以写成下面的 Git 声明，与上方 Go module 声明二选一：

```toml
[dependencies.standard]
git = "https://github.com/goxide-lang/std.git"
rev = "c02a09d8c417b35dc7e432fc484172fab6db6182"
```

两种声明都以 `use standard::formatting;` 选择该 Go 子包。

## monorepo 与工作区规则

`subdir` 只选择 Git 仓库内的项目/module 根，该目录必须有自己的 goxide.toml 或 go.mod；省略时选仓库根。不递归搜索，不按 alias 猜目录，不允许逃出 checkout。path 同样直接选根，不需要再加 subdir。Go 子包按 `use alias::真实子路径` 解析并核验实际 module owner，Goxide 公开子路径沿 pub mod/pub use；不能拿 subdir 选择同一 Go module 下的普通包目录。

实际 monorepo 验收使用本地 Git 仓库，packages/lib 内的独立 module 通过 subdir 选择；完整可运行 recipe 见 [schema 文档](toml-manifest.md#monorepo-与发现)。原 shared_std_candidate 夹具包含历史 ABI，已确认当前编译器正确拒绝，不能作为新用户的可运行依赖示例。

`goxide build/run/test/transpile` 无 INPUT 时，从 cwd 向上找最近 goxide.toml；显式目录采用同一规则，显式文件保留单文件入口。go.mod 不终止向上发现项目或 workspace；找不到清单则诊断，原生 Go 目录需显式输入。

找到成员清单后向上发现所属 workspace，沿用显式 members 与 `workspace = true` 继承；成员子目录选最近成员，根目录选 default-members，`-p`/`--workspace` 显式改变选择。未列出的项目不变成成员；成员资格不补依赖边。workspace 仍唯一根 lock/target；单项目使用自身根。源依赖自身的 workspace 继承只在其 checkout 内解析，不继承消费项目的 workspace。

## 获取、稳定输出与验收

单一缓存默认：Goxide 获取的 Git 源码放受管 `target/cache`，不增加第二套全局模式；Go module/build cache 继续使用 Go 的 GOMODCACHE/GOCACHE。根 goxide.lock 可提交，缓存可删除后按声明与锁重取；源码缓存位置不充当可提交的来源身份。项目根不再创建 `.goxide`，构建互斥锁须覆盖同根不同 target-dir 的锁文件写入。

复用稳定的 target 工作目录与最终输出路径，继续隔离目标、features、tags/race、编译器/工具链配置；每次不永久新增 run 目录。先生成完整新视图，再仅替换内容变化文件，并移除工具此前生成的失效文件；拒绝覆盖用户修改或删除陌生文件。保留现有必要所有权与互斥检查，不增加未来扩展注册表、wrapper 或通用缓存框架。

编译前自动获取已声明且受支持的依赖、选版并校验 sums；Go 标准库不进入下载图。缺声明、无效库入口、错误 module 身份、未实现 registry、离线缺缓存均给对应诊断，不让用户等 go build 提示手工 go get。构建不借下载执行依赖脚本；现有脚本授权不变。

实施顺序：确认下列取舍 → 在关联 Draft PR 中实现最小真实首构建 → 少量定向回归（正常链路、来源冲突、发现边界、旧文件删除/用户编辑保护）→ 干净缓存首构建及 offline/locked 二次构建，核对目录、内容/mtime、锁与无 `.goxide` → 独立正确性及删减审核 → CI 最终全量，同一版本不重复全测。不提供旧 schema/API 兼容，不扩到 serde 或其他任务。

## 决定记录

1. 已确认：采用 `go = "module-path"` 加 version，并用 `go = "fmt"` 表示免下载标准库；依赖 alias 选择来源根，纯 Go 子包自动按真实路径解析，Goxide 使用自身 lib 公开入口，不接受 dependency.package 或 namespaces。
2. 已确认 Git 使用零或一个 rev/tag/branch，缺省使用默认分支并锁定 commit；rev 支持 commit hash/远端公开 ref，并且本片不支持 version 与 git/path 的发布组合。
3. 本片按父任务明确范围采用显式项目根 subdir、不自动搜索；单一受管 target/cache 默认。

上方确认范围已恢复实施；registry、本片未覆盖的发布与依赖扩展留在 Issue #23 的后续范围，不在此添加机制。

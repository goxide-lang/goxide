# #23 项目构建与依赖：待确认的最小方案

关联 [Issue #23](https://github.com/goxide-lang/goxide/issues/23)。本 PR 仅提交方案，不包含实现；下列新 schema 尚未获批或完成构建验收。此前未确认的代码留在另一工作区，不进入本 PR。

当前根因：CLI 强制 INPUT，项目检测没有向上找最近清单；依赖的获取来源和导入绑定混在 `source/module/package` 中；每次构建保留新的 `runs/build-*`；Git 缓存及构建锁无条件创建项目根 `.goxide`。现有下载逻辑已经存在，实施时先用干净项目复现漏项，再修实际缺失的依赖获取链路。

## 来源字段建议（待确认）

参考 [Cargo 官方依赖文档](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html)，采用普通依赖表及 `version/git/path/rev/tag/branch` 字段，不引入 `source` 标签或嵌套来源 DSL。新增 `go` 与 `subdir` 是 Goxide 扩展，不是 Cargo 字段。Cargo 支持 version 配合 git/path 的发布回退；本片没有 registry/发布流程，建议暂不实现这种组合。

| 获取来源 | 本片建议 |
| --- | --- |
| registry | 单独 `version`，或 `version` 加 `registry`；明确报未实现，不猜默认 registry |
| Git | `git = "https://…"`，恰好一个 `rev/tag/branch`；可加 `subdir`，不同时声明 path/go/version |
| 本地 | `path = "../lib"` 直接指向项目根，不同时声明 git/go/version |
| Go module | `go = "github.com/…"` 加 `version = "v…"`；路径不带协议，按 Go MVS 选版并锁定结果 |
| Go 标准库 | `go = "fmt"`，没有 version，不下载；用所选 Go 工具链的真实包信息核验标准库归属 |

公共字段保留现有 `module`、`package`、`namespaces`、`features`、`default_features`、`optional`。Git/path 的 `module` 是选中项目的身份；Go 来源的身份由 `go` 给出，不重复填写 module。`package` 和 namespaces 是精确 import package 绑定；这里的 package 沿用 Goxide 包映射含义，不等于 Cargo 的发行 crate 名。未映射到 package 的依赖根只作显式 namespace 分组。标准库的 go 本身给出精确 package，无需再写 package；不接受 feature 请求。

Go module 是一起选版和分发的一组 packages，import package 是其中一个包，见 [Go 官方模块说明](https://go.dev/ref/mod#module-path)。Go 版本只接受规范版本或 pseudo-version，本片不实现 Cargo SemVer 范围。Git 的 rev 建议支持 commit hash 或远端公开 ref，tag/branch 分别按对应 ref 解析；多个选择器报错，没有隐含优先级。解析后把完整 commit 写入 lock，普通二次构建不重新解析移动 branch/tag。

真实来源例：本仓库 main 的 go.mod 已固定下述 std 版本，该提交有 `formatting` 包，没有 `fmt`。依赖语法是待确认提案，源码路径与版本已核对；新 schema 首构建留待实施验收。

```toml
[dependencies]
fmt = { go = "fmt" }
formatting = { go = "github.com/goxide-lang/std", version = "v0.0.0-20261009104839-c02a09d8c417", package = "github.com/goxide-lang/std/formatting" }
```

同一真实 formatting 来源也可以写成下面的 Git 声明，与上方 Go module 声明二选一：

```toml
[dependencies.formatting]
git = "https://github.com/goxide-lang/std.git"
rev = "c02a09d8c417b35dc7e432fc484172fab6db6182"
module = "github.com/goxide-lang/std"
package = "github.com/goxide-lang/std/formatting"
```

## monorepo 与工作区规则建议

`subdir` 只选择 Git 仓库内的项目/module 根，该目录必须有自己的 goxide.toml 或 go.mod；省略时选仓库根。不递归搜索，不按 alias 猜目录，不允许逃出 checkout。path 同样直接选根，不需要再加 subdir。项目内的包通过 package/namespaces 绑定，不能拿 subdir 选择同一 Go module 下的普通包目录。

真实嵌套根例：本仓库 `cf56a52c22acff7a1ab353f86515745b542d6abd` 的 `tests/research/shared_std_candidate/go.mod` 声明 module `github.com/goxide-lang/std`，其 `sum/` 是包目录。下面只用于展示 monorepo 根选择，来源是仓库内的测试夹具，不替代上面的公共 std 来源，也不与其同时声明：

```toml
[dependencies.sum]
git = "https://github.com/goxide-lang/goxide.git"
rev = "cf56a52c22acff7a1ab353f86515745b542d6abd"
subdir = "tests/research/shared_std_candidate"
module = "github.com/goxide-lang/std"
package = "github.com/goxide-lang/std/sum"
```

`goxide build/run/test/transpile` 无 INPUT 时，从 cwd 向上找最近 goxide.toml；显式目录采用同一规则，显式文件保留单文件入口。建议遇到原生 go.mod 边界停止项目查找，找不到清单则诊断；原生 Go 目录需显式输入，不借父项目切换其语义。

找到成员清单后向上发现所属 workspace，沿用显式 members 与 `workspace = true` 继承；成员子目录选最近成员，根目录选 default-members，`-p`/`--workspace` 显式改变选择。未列出的项目不变成成员；成员资格不补依赖边。workspace 仍唯一根 lock/target；单项目使用自身根。源依赖自身的 workspace 继承只在其 checkout 内解析，不继承消费项目的 workspace。

## 获取、稳定输出与验收

单一缓存默认建议：Goxide 获取的 Git 源码放受管 `target/cache`，不增加第二套全局模式；Go module/build cache 继续使用 Go 的 GOMODCACHE/GOCACHE。根 goxide.lock 可提交，缓存可删除后按声明与锁重取；源码缓存位置不充当可提交的来源身份。项目根不再创建 `.goxide`，构建互斥锁须覆盖同根不同 target-dir 的锁文件写入。

复用稳定的 target 工作目录与最终输出路径，继续隔离目标、features、tags/race、编译器/工具链配置；每次不永久新增 run 目录。先生成完整新视图，再仅替换内容变化文件，并移除工具此前生成的失效文件；拒绝覆盖用户修改或删除陌生文件。保留现有必要所有权与互斥检查，不增加未来扩展注册表、wrapper 或通用缓存框架。

编译前自动获取已声明且受支持的依赖、选版并校验 sums；Go 标准库不进入下载图。缺声明、缺 package、错误 module 身份、未实现 registry、离线缺缓存均给对应诊断，不让用户等 go build 提示手工 go get。构建不借下载执行依赖脚本；现有脚本授权不变。

实施顺序：确认下列取舍 → 在关联 Draft PR 中实现最小真实首构建 → 少量定向回归（正常链路、来源冲突、发现边界、旧文件删除/用户编辑保护）→ 干净缓存首构建及 offline/locked 二次构建，核对目录、内容/mtime、锁与无 `.goxide` → 独立正确性及删减审核 → CI 最终全量，同一版本不重复全测。不提供旧 schema/API 兼容，不扩到 serde 或其他任务。

## 请用户决定

1. 是否采用 `go = "module-path"` 加 version，并用 `go = "fmt"` 表示免下载标准库；保留现有精确 package/namespaces 绑定？
2. Git 是否要求恰好一个 rev/tag/branch，缺选择器报错；rev 接受 commit hash/远端公开 ref；本片不支持 version 与 git/path 的发布组合？
3. 是否采用显式项目根 subdir、不自动搜索；原生 go.mod 停止向上项目发现；单一受管 target/cache 默认？

这些仍是待确认建议，PR 建立不代表批准实现。registry、本片未覆盖的发布与依赖扩展留在 Issue #23 的后续范围，不在此添加机制。

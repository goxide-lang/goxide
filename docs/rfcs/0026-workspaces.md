# RFC 0026：Cargo 风格 workspace 与 Go 多模块边界

## 状态与范围

**作者 workspace 核心已实现，扩展协议仍 Proposed（2026-10-04）**。实现基于 `feat/cobra-posix-cli` 的 `09f1eb258fc98217e5d03506f34957c9ee9aec4f`，独立分支 `feat/workspace-core`；整合了 `594d3a3` 的已确认决策。显式 `workspace=true`、全 workspace 统一版本/根唯一锁、仅所选编译闭包的 feature 并集现已有生产代码与本地端到端测试。没有合并主线、冻结 tag 或发布网站。下文未纳入本节实现清单的语法仍为设计合同，不应视作已提供。

承接 [0022 项目工具链](0022-project-toolchain.md)、[0023 构建脚本](0023-build-scripts.md)、[0025 单一项目清单](0025-unified-project.md)。继续以 `goxide.toml` / `goxide.lock` 为作者配置权威，根不并存 go.mod/go.sum/go.work/go.work.sum。原生 Go 负责 module path、版本、MVS、checksum 和编译；生成 Go 配置只在受管 target。目标是组织多包开发与独立发布，不引入 Rust resolver、所有权、Copy、enum ABI 或新借用语义。

依赖来源字段的现行语法以 [TOML schema](../design/toml-manifest.md) 为准；下文的候选扩展不改变已实现的互斥来源规则。

### 本次实现范围

- 根 `[workspace]`，虚拟根/根包、显式 `members`、单段 glob、`exclude`、`default-members`；成员使用当前 goxide.toml 字段。拒绝重复 module、嵌套 workspace、成员锁、symlink/受管输出成员；外部成员须显式路径，由根 INPUT 选择。
- `[workspace.dependencies]` 仅模板；成员须 `{workspace=true}`，可加 feature 请求、设置 default_features/optional；不写该标记就不继承。`go_mod.go.workspace=true` / toolchain 同理。显式 Go module/Git/path 来源继续有效，不自动改为成员 path；显式来源冲突报错。原生 require 与成员 path 共存须显式未限定版本的本地 replace。
- `goxide build|test|transpile --workspace ROOT`；重复 `-p/--package MODULE`；run、显式 bin/example/lib 和 -o 必须只选择一个成员。默认多成员构建其 lib/bins，未满足 required_features 的默认目标跳过并说明；examples 仅显式选择。多成员 feature 使用 `MODULE/FEATURE`。CLI 保留 POSIX 风格，flags 在 INPUT 前。
- 根 schema 3 lock 保存统一 Go MVS 版本、有效政策、来源身份和校验和；schema 2 单项目继续支持，不自动迁移为 workspace。全体成员的声明 feature 闭包只用于版本求解；本次构建的 features 另按所选根/活动依赖固定点合并。根写锁串行化更新，不运行求解阶段脚本。Git 保持已锁 commit，已有 Unified 锁缺可达 pin 时即使未传 --locked 也拒绝移动分支解析。
- 拆出项目可直接读取复制的 schema 3 根锁：只沿实际清单及已校验 Go `.mod` 的可达关系求解，忽略无关记录，核对来源/输入/版本/checksum；不复用旧 workspace 的无关 replace 政策。远端/Git 可在新缓存位置重建；path 不自动物化或改源。`--offline` 使用已缓存的可达已锁版本，不要求被升级低版本的缓存。
- 根 target 所有权与成员 module/目标/feature/配置共同隔离；既有 normal/test metadata 拒绝机制与脚本 trust 保留。未选成员源码/脚本不编译或执行。host helper **沿用 RFC0023 的显式本地 Go 快照合同**；本片没有新增 workspace host 版本/feature resolver，也不声称跨脚本 helper 来源统一。

可运行清单与命令见 [workspace 示例](../../examples/workspace/README.md)，实现证据见 [核心验收](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/workspace-core-20261004.md)。原始研究记录仍是 19 项原生 Go 机制实验，不替代这些 goxide 测试。

**增强作者核心模式限制**：不支持 `[workspace.go]` / root override / workspace vendor / go.work import（纯原生迁入模式见下一节）、`publish_version`、dev 表、profile、registry、target-specific features、互斥 feature、Cargo resolver 数字版本、批量 examples/all-targets、manifest-path/workspace-manifest/no-workspace 参数。未知配置字段严格拒绝。现有 host helper 不支持依赖模板继承，不以 workspace 成员资格扩展 host 权限。全 workspace 搬家时绝对本地路径身份需重新锁定；拆出 path 或仍保留继承语法的成员必须先显式物化配置，不能声称复制锁即自足。

普通构建在成员清单/根政策改变时重新求解统一版本，保留已有 Git pin；--locked 明确拒绝变化。可达非成员 path 依赖政策变更、schema 2→3 转换、路径改源需要显式重建锁；当前没有细粒度 update 命令。需备份旧锁后移走并重新构建，新锁会重新解析 Git rev，必须审查新旧 commit；不能把该流程当作锁定构建。编译前沿现有脚本合同检查每个所选图，批量任务可能已有前序成员成功后才遇到后序构建失败；不是整个批次的事务性发布。

### 第二阶段迁入实施合同

基于 `2aa0d066` 的独立 `feat/workspace-import` 已实现纯原生 go.work / workspace vendor 迁入子集，接口、单配置分析边界、schema 4 锁与限制见 [迁入设计](../design/workspace-import.md)，实际测试见 [迁入验收](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/workspace-import-20261004.md)。本次采用受管原生 go.work，显式依赖图与 Go 传输政策分开。后续衔接已允许成员渐进使用 .gox/features/明确授权脚本。最新合同将 vendor 限定为迁入输入，验证后物化普通本地模块；成员独立时显式物化路径并复制根锁，没有 vendor 拆出转换，见 [供应源码迁入设计](../design/vendor-local-modules.md)。这仍不代表全 RFC 完成。

### 官方事实与本篇选择

查阅日期 **2026-10-04**；Cargo Book 为当天在线滚动版本，不声称固定 Cargo 版本。环境无 cargo/rustc，未执行 Rust 试验，也未安装。Go 机制实际使用 `go`，输出 **go1.27.1 linux/amd64**；版本相关结论以此测试为界，未来工具链需重验。

| 官方事实（资料） | 本篇提案，不能当作上游承诺 |
| --- | --- |
| Cargo 支持虚拟根/根包、成员选择、共享 lock/target 和显式字段继承；根 patch/profile 有特殊地位。[Workspaces](https://doc.rust-lang.org/cargo/reference/workspaces.html) | 采用显式模板继承；只有成员写 workspace=true 才继承，不自动引入依赖或把内部 path 依赖升级成成员 |
| Cargo 同次多包构建合并依赖 features；锁求解与本次编译 feature 选择不是同一过程。[Resolver](https://doc.rust-lang.org/cargo/reference/resolver.html#features) | 全 workspace 声明 feature 闭包统一锁版本；本次编译不因此启用未请求的 feature |
| Cargo feature 是加法；resolver 2 隔离 build/normal，dev 请求按需要生效。[Features](https://doc.rust-lang.org/cargo/reference/features.html#feature-resolver-version-2) | 采用闭包与角色隔离；不提供数字 resolver=2/3 假兼容 |
| Cargo path 与发布版本可以表达不同开发/发行来源。[Dependencies](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html#multiple-locations) | 本地来源必须显式声明，另保留发行版本；成员身份不能替换原version/path/git来源 |
| Go workspace 把 use 中模块视作主模块，MVS 统一同 module path；冲突 replace 可由 work 根裁决。[Go modules](https://go.dev/ref/mod#workspaces) | 构建仅显式选中成员及其依赖；全workspace统一版本解析，Go use 的隐式来源不照搬，迁入计划须物化声明 |
| Go workspace 使用 work 的 godebug，成员 go.mod 中的 godebug 被忽略。[GODEBUG](https://go.dev/doc/godebug) | 原文保留每层政策，生成等效上下文，不能取最高 go 版本后丢弃运行时默认值 |
| Go 的 go 最低版本与 toolchain 建议不同。[Toolchains](https://go.dev/doc/toolchain) | GOTOOLCHAIN=local；不自动下载，更高建议不变成硬性最低版本 |
| Go 提供 work vendor，和 module vendor 有不同作用范围。[go command](https://pkg.go.dev/cmd/go#hdr-Make_vendored_copy_of_dependencies) | 两种 vendor 身份分开，迁入不调用 vendor 重建缺失文件 |

## 源级规则

除上节列明的实现子集外，以下语法、CLI、诊断名仍为**候选合同**。go_mod 与 workspace 均不声明格式版本；清单直接按当前字段校验，未知表或字段报错。

### WS-01：根、成员与发现

```toml
# dev/goxide.toml：虚拟根；没有 [go_mod]
[workspace]
members = ["apps/*", "libs/shared"]
default-members = ["apps/service"]
exclude = ["apps/scratch"]
# 全workspace统一版本解析；没有selected/all-members模式开关

[workspace.go_mod]
go = "1.27"

[workspace.dependencies.shared]
module = "example.org/shared"
path = "libs/shared"     # 继承源的路径相对 workspace 根
publish_version = "v1.2.0" # 候选：发行时的 Go require 最低版本，不是当前 path 的锁版本
features = []
default_features = false
```

根同时有 `[go_mod]` 时是根包，自动纳入成员。虚拟根无 go_mod/namespaces/lib/bin/build/features/go/vendor 这些包级表；根包有自己独立的配置，不能把根包 dependencies 当成全部成员依赖。root 的 go 政策和 workspace.go 政策也必须分开。

成员是**一个作者包对应一个逻辑 Go module**，可含多个 Go packages、一个 lib、多 bin/examples。module path 是包身份；不把每个 bin 变成新 module。列表项指向清单目录，不是递归扫描源码。推荐首片即支持路径段内 `*`、`?`（不跨 `/`），拒绝 `**`；展开按规范相对路径排序，exclude 后必须非空。default-members 必须是最终成员的显式路径集合，不再展开 glob；缺省时有根包选根包，虚拟根选全部。

所有相对路径相对于**声明所在清单**。path 规范化、大小写/真实路径别名和重复 module 检查先于写入；同一目录多种拼写去重并报告来源，两个不同目录同一 module 身份报错。glob 不穿越符号链接、隐藏控制目录、`.git`、`target` 或声明的 target-dir，不扫描任意目录；显式 symlink/逃逸路径拒绝并展示规范目标。显式 `../repo` 外部成员可以支持，但必须完整列举，不能用父目录 glob；授权边界只扩到该成员，不到整个父目录。target 永远不得包含/成为任何成员或其祖先。

推荐首片支持外部显式成员，便于多个独立仓库共同开发；可先只实现同父目录 checkout 布局，但不能将多仓库模式标作永久不支持。工作区根不能成为自己的外部成员，单次命令只能绑定一个活动根；两个独立 workspace 可以并发只读使用同一外部成员，各自持有自己的锁/target，不争夺成员源码所有权。嵌套 `[workspace]` 是发现边界：外层 glob 不跨越，显式纳入报 WS-NESTED，须排除或重组。

从目录入口向上找最近 TOML 包/工作区；遇到最近 workspace 就停止，不跳过它找更远父根。只有该根列举该包才加入；未列入则报 WS-NOT-MEMBER，并给出加入/exclude/独立模式建议，不悄悄使用父锁。外部成员从根命令可选；从外部成员目录调用不会遍历兄弟目录找工作区，需 `--workspace-manifest /dev/goxide.toml`。显式 `--workspace-manifest` 必须验证成员资格。

推荐 `--no-workspace` 显式独立运行、关闭父发现；只用拆出后的独立清单/复制的根锁/新target，未展开的继承字段报错，不偷偷读父根。仓库移出后同样如此。发现阶段只核查全局路径/清单存在/可读取身份；未选成员的源码编译错误和脚本不进入本次构建；其清单身份、依赖/feature声明和Go最低要求仍参加全workspace图验证，坏清单/政策可以阻断锁解析，不能承诺只选A就忽略B的依赖政策。无法读取 TOML 或 module 身份、重复身份属全局目录损坏，仍阻断并注明不是该成员源码编译错误。提供后续 `workspace check` 全成员清单验证入口，不默认执行源码/脚本。

### WS-02：显式继承与本地成员依赖

```toml
# apps/service/goxide.toml：候选继承语法
[go_mod]
module = "example.org/service"
go.workspace = true

[dependencies.shared]
workspace = true
features = ["json"]
optional = false

[[bin]]
name = "service"
path = "cmd/service"

[[bin]]
name = "web"
path = "cmd/web"

[[example]]
name = "hello"
path = "examples/hello"
```

workspace.dependencies 是模板库，**不注入依赖边**。成员按 alias 显式 `workspace=true` 才继承。go/module/path/git/rev/tag/branch/subdir/version/publish_version 从模板一次性取得，成员不得同时重新指定来源；features 按集合相加；optional 只在成员声明，根模板禁止 optional；default_features 以成员显式值覆盖模板缺省，但任何其他活动依赖边仍可启用默认请求。本篇新增的键沿用现有 TOML `default_features` 拼写；CLI 使用 `--no-default-features`。`[workspace.go_mod]` 只提供 go、toolchain 默认值，成员在 `[go_mod]` 内显式写 `字段.workspace=true` 才继承；成员自己的 module 路径不继承。version/license/readme 等属于另外的发布元数据合同，不写入 go_mod，也不假定 Cargo 字段已被本仓库实现。

加入成员不等于依赖该成员，源码 import 也不能创建清单依赖或启用模板。只有显式继承模板中的 path、成员自己声明的本地 path，或明确的 override/replace 条目才能把来源切到本地成员；version、Git、path 和原生 go.require 不因另一个目录被列为 member 就自动改指该目录。`workspace=true` 若继承的是 version/Git，仍消费该远端来源；它不是“自动找同名本地包”开关。未来若需要 Cargo 式 workspace patch，必须另提明确条目与优先级，不能默认启用。

以下白/黑例采用用户要求的 inline TOML，分别是不同文件，不能拼接成一个清单：

```toml
# workspace 根：只定义模板，不引入依赖
[workspace.dependencies]
package1 = { module = "example.org/package1", path = "libs/package1" }
```

```toml
# 白例：成员显式继承，path 相对根模板声明所在目录
[dependencies]
package1 = { workspace = true }
```

```toml
# 黑例：没有 workspace=true；不能从根模板补 module/path
[dependencies]
package1 = {} # WS-INHERIT-EXPLICIT：不完整独立声明
```

完全省略成员的 package1 条目也不继承；若源码需要该库且显式活动依赖图没有提供它，报未声明依赖，不因 member/template 存在而搜索。独立完整声明 `package1 = { go = "example.org/package1", version = "v1.2.0" }` 则合法：即使根有同名 path 模板、同身份 member，也不得暗改版本来源；若全图同时要求本地同身份根，声明合法不代表图无冲突，须按来源冲突规则显式处理。显式依赖图包含已声明的传递依赖和原生 Go 解析出的依赖，并非要求每个 import 都新增直接增强依赖条目；关键是不能借 workspace 成员列表扩大来源图。

未激活 optional 边不会被 import、同名模板或成员搜索激活；没有其他有效活动边提供该来源时，保留源码 import 报 WS-INACTIVE-DEPENDENCY。关闭文件不装载 import。纯 path 成员依赖无需虚构版本；`publish_version` 如存在是发行最低版本，原生 go.require 保留最低要求。显式本地替换不声称未打 tag 的工作树满足发布版本；发行预检用真实版本验证路径/major 和可获取性，不能把工作树 HEAD 当已发布 tag。

path + publish_version 是**新语法**，区别于现有 version/path/git 互斥来源；不放宽 version 到 Rust 的 ^/~ 范围。Go `/vN`、pseudo-version 和 MVS 仍由真实 Go 校验。Git 锁定 commit+subdir；branch/tag 可移动但已有锁不重新漂移；URL 是 argv 数据，不经 shell，不保存凭据，不触发安装脚本。

来源冲突按“解析角色 + module path”识别。两个活动边若要求不同 path/Git commit 或成员路径不匹配，报 WS-SOURCE-CONFLICT，列出边与清单位置；两个版本要求交 MVS，不当成来源冲突。成员身份不能裁决来源冲突或覆盖普通版本边。若workspace全图同时包含本地成员B的根身份，而A仍显式依赖同module的远端版本，不能暗取本地B：统一域报WS-SOURCE-CONFLICT，须显式统一来源或拆成独立workspace；仅切换构建命令不能绕过全图冲突。workspace 根可用下述 Go replace 明确裁决，结果出现在 plan/lock；别名不能制造两份同 import path 的类型。

### 依赖来源：Git URL 优先，别名不是 registry 名字

**已确认方向**：目前优先显式Git URL引用Go包。示意清单（URL/rev是占位，不是已发布依赖）：

```toml
[workspace.dependencies]
package1 = { module = "example.org/package1", git = "https://example.org/team/package1.git", rev = "v1.2.0" }
# 成员文件另写：
# [dependencies]
# package1 = { workspace = true }
```

package1只是清单本地别名，不从它推导registry包名、module path或Git URL。rev经获取核验后锁成commit；重建用已锁commit，branch/tag移动不自动更新。类似crates.io的“名字+版本”注册中心发现留后续设计，`package1 = "1.2.0"` 当前不能解释为这类registry请求。

这不删除现有Go module互操作：显式 `go = "example.org/package1", version = "v1.2.0"` 及原生go.require仍沿现有Go机制处理；它们使用完整Go module身份，不是新的registry短名解析。显式path和workspace=true继承继续支持，Git优先是作者来源方向，不把所有Go互操作依赖强制改写为Git。

### WS-03：CLI 与目标选择

候选命令，需和独立 Cobra/POSIX 迁移统一；本分支未实现这些新 flag。保留现有 `--go`，不用 `-go`。`-h` 帮助、`-o` 输出符合 CLI 项目约定。为避免 module path 中 `/` 歧义，`--package` 使用完整 module 身份；将来可给唯一短名，歧义必须列候选。

```sh
goxide build --workspace-manifest ./goxide.toml --package example.org/service --bin web
goxide run --workspace-manifest ./goxide.toml --package example.org/service --bin web -- --listen=:8080
goxide build --workspace
goxide test --package example.org/a --package example.org/b --locked --offline
goxide run --package example.org/service --example hello
goxide build --package example.org/a --features example.org/a/json --no-default-features
goxide build --no-workspace --manifest-path ./apps/service/goxide.toml
goxide import-go --workspace --plan ./old/go.work
goxide import-go --workspace --out ./new --bin example.org/a=app:cmd/app ./old/go.work
```

工作区入口不选成员按 default-members；成员目录入口默认该成员，根包目录适用根的 default-members；`--workspace` 显式全部，和 `--package` 互斥。重复 package 去重、未知报错，不只选第一个。--manifest-path 定位包或根不等于关闭工作区发现。

单成员现有 bin/lib/example 规则不变；`run` 必须恰好一个成员、一个可执行目标。多成员批量 build/test 推荐默认每成员 lib（若有）和所有未缺 required_features 的 bins；缺 feature 的目标输出 skip 原因，显式选中的目标缺 feature 则失败，不自动开启。examples 默认不构建；批量 `--examples` 显式包含，`--all-targets` 包括 lib/bin/examples/tests，需单独计划 dev 闭包。多成员命令中的 `--bin/--example/--lib` 首片拒绝歧义并建议逐成员选择，不把同名 web 广播到多个模块。跨成员 bin 同名合法，成员内同类目标重复名拒绝。多产物 `-o FILE` 报错，使用布局输出/显式输出目录协议。

`--features module/feature` 以最后 `/` 分隔已知成员 module 和 feature；多成员时禁止未限定的 feature。只允许给选中根发请求（传递依赖经清单边请求），--no-default-features 关闭这些根的默认请求，不抹去传递边。单成员保留未限定 feature 简写。required_features 检查与 cfg 仍属包自己的 feature 集合。

### WS-04：解析范围、feature 和角色

“选中成员”只表示**本次命令要构建哪个项目**。例如workspace有app、server、common，app和server都显式依赖common：

| 命令（候选） | 本次构建 | 不因此构建 |
| --- | --- | --- |
| goxide build --package example.org/app | app及其显式活动依赖common | server及server专用脚本 |
| goxide build --package example.org/server | server及common | app |
| goxide build --workspace | app、server、common的选定目标 | 未声明的外部来源 |

这和“common或它的外部依赖选哪个版本”是两件事。**现已确认：整个workspace统一解析版本并使用根锁，命令仅构建选中成员及其显式活动依赖，未选中成员feature不自动带入。** 不再提供旧草稿selected/all-members两套模式，也不为go.work迁入另选版本策略。

例如app对D的原生Go最低要求为v1.0.0，server为v1.1.0：统一Go MVS结果为v1.1.0，只构建app也使用这个版本。但如果app请求common/json，server请求common/yaml，app单独构建只启用json；一起构建才合并json+yaml。Git同module不同commit是来源冲突，不按“更新commit”自动择一，不借全workspace模型恢复隐式本地覆盖。

锁解析图与实际编译图分开：前者覆盖全部成员声明的依赖/来源和版本政策；后者从命令选中根沿显式活动边形成。锁条目不产生依赖或feature请求。初次/显式更新全图锁可能需要其他成员的来源元数据；必须在plan列出，不能声称从不访问未构建成员信息。已有有效锁构建app只获取实际可达源码，不为server无关锁项下载字节、编译或执行脚本。声明的可选依赖参与锁定所需信息如何读取/存储须在锁协议中明确，但锁求解不能把其feature请求注入编译图；所选配置缺少完整锁记录时失败并要求显式补锁，不能静默改版本。单项目既有optional延迟规则不因本RFC自动改写。

官方对照：Cargo为锁文件考虑整个workspace成员及所有features，再计算实际编译feature集合；共同选中成员的依赖feature并集，见[Cargo resolver两个阶段](https://doc.rust-lang.org/cargo/reference/resolver.html#features)。这里采用“全图版本锁/本次编译feature分开”的原则，Go MVS仍在同一解析域为同module path选一个版本，不复制Cargo多版本crate实例。生成后端不能直接use所有member来绕过显式source声明。

全workspace版本解析并不混合host和target：分别在各角色内统一来源/版本，再按本次构建计算对应feature请求。未选成员的版本政策可以影响统一结果，其feature、脚本主体和编译目标不能沿这个版本影响自动传播。normal/test的编译配置与metadata继续分离。
先校验全workspace统一版本锁，再从选中根的显式活动依赖求解本次feature图，然后在既有图中按 Go 文件条件/cfg 和实际目标发现包 import。import 只选择图内包，不反向激活成员、feature 或来源；不存在旧草稿的“import→自动成员边→默认feature”固定点。feature 并集只沿显式活动边计算；MVS 改选版本导致清单/feature声明改变时仍须从根重算有效图，丢弃旧版本请求并检测不收敛，不能借这项正常求解工作恢复源码隐式边。

脚本执行在图冻结之后；生成文件或宏新增 import 也只能使用已声明活动图中的来源，缺边报 WS-UNDECLARED-GENERATED-EDGE，不能执行脚本后自动扩大依赖/信任闭包。模板未被继承、optional未启用和CLI未选无关成员都不构成额外来源。

对同一固定活动图，features 单调并集；默认请求有一条开启即保留。在已有完整锁的本次构建中，仅由未激活可选增强边关联、且没有其他活动边请求的来源不下载、不读源码或执行脚本；初次/显式更新全图锁可按plan读取必要来源元数据，不得因此启用编译feature或脚本。全workspace的原生require参与不能被optional声明抹掉。当前成员自己的未知 feature/feature cycle 仍按 RFC0022 明确诊断。MVS 升级后重新验证最终选中版本的 features；不存在请求不能换低版本逃过 MVS。条件编译统一遵循 [RFC 0022](0022-project-toolchain.md)：完整解析源码后由 cfg/cfg_attr 选择有效节点，再做宏、名称和类型检查；cfg! 仅产生布尔常量，if 两支仍检查。workspace 复用同一语义，其入口接入仍按独立阶段验收。

host 构建脚本有独立 host 版本/feature 域，与 target 不合并；同一 helper 若被同次多个脚本共用，host 内并集，其角色键不同于 target。脚本接收所属 target 包的已冻结 features，但不能返写图。Go 包 DAG 仍须合法；host 指向 target 不能造成“先生成自身才能编译生成器”的循环。

dev-dependencies 属候选后续能力：只加入所选成员 test/examples 所需闭包；普通 build 不激活，依赖库的 dev 边不递归传播。不能把全 workspace 测试 feature 泄漏给普通构建。首片可沿用现有显式 test 来源和 build.dependencies，不在 parser 接受后默默当 normal；新 dev 字段在未实现时必须明确拒绝。test 与 normal metadata 保持现有拒绝机制，还需记录 workspace 图/角色/计划身份。

### WS-05：Go 政策、锁与可复现

以下工作区级 override 仍是未批准的候选条目，不因用户要求显式继承就自动获准；若保留，必须实际写出条目才生效，不由成员列表推导。候选 workspace 根政策采用 `[[workspace.go.replace]]`（字段与 RFC0025 go.replace 相同），以及 `workspace.go.go`、`workspace.go.toolchain`、`workspace.go.godebug` 明确对应 go.work；不引入含糊 patch 竞争层。成员 go.require/replace/exclude/go 保留原文语义：全部成员作为版本/根政策参与者，其根政策按真实 Go 合并；外部版本依赖自己的 replace 不提升。冲突主模块 replace 由显式 workspace replace 裁决，不能按遍历先后选。exact-version replace 与 wildcard 优先次序委托 Go；成员替换自己/其他活动主模块的限制也交 Go，不用全局覆盖躲过检查。

生成策略不是定案：可合成受管的解析/构建视图，但不能将全部member目录直接写进Go use而重新引入隐式来源覆盖。版本/根政策参与者与编译来源映射分开；全workspace版本图也须仅采用显式图认可的source。生成go.work仅在其use行为与该显式图等价时可用，否则需分离视图或明确冲突，不能让后端Go“顺便找到成员”绕过前端缺边诊断。必须保留每模块 go 语言版本、root exclude/replace 合并、toolchain 建议及 GODEBUG 上下文；不能把全体 go 最高值写回每个包。生成的版本解析视图go至少满足全workspace政策参与成员和工具自身最低版本；若显式 workspace.go.go 太低报错，不静默升级。此 work go 会影响运行时默认值，plan 必须报告；独立发行用成员自身 policy，需在脱离 workspace 的发布验收中比较。未来 Cargo 式 profile 可以仅根定义，但与 Go -race/tags/opt 的映射尚无合同，首片不接受该表。

**已确认：workspace 只保留根 `goxide.lock`，成员不保留独立锁。** 旧草稿“成员锁保留但不采用”撤回。加入已有独立工程时，计划须显示其旧锁如何核验后并入根锁；不同Git pin/来源不能按顺序覆盖。正常工作区构建发现成员锁报 WS-MEMBER-LOCK，要求显式整理，不能静默忽略。迁入仍只写新目录，不因这一规则自动删除用户旧工程的锁。

根锁格式需支持不可变来源记录（Git URL+commit+subdir+module、Go version/checksum、vendor内容）和可达关系，工作区/构建计划仅是上下文索引，不能把所有记录绑死到原根绝对路径。schema 3仍为候选；schema 2须显式核验转换，不猜升级。根锁可记录未被当前命令使用的来源；**锁中有记录不等于清单有依赖**。本轮已另外确认全workspace统一选版；锁内可保留历史/角色记录，但不能以按命令存独立版本计划的方式恢复旧selected解析策略。

**已确认：拆出成员可以直接复制根锁到新独立项目。** 新清单先显式物化 `workspace=true` 继承，保留实际module身份和来源。加载复制锁时从新清单的显式活动边取可达记录；其余条目可留存并忽略，不激活依赖、不执行脚本、不下载或校验那些无关来源的字节。仍要验证锁文件自身格式/结构合法，不把可达缺项当无关项忽略。可达集合须完整，包括固定Git commit、subdir、module身份、版本/校验、必要传递关系及有效政策；来源冲突、缺项、校验不符明确失败，不偷偷重新解析branch/tag或升级版本。格式须容许验证并重绑定独立根上下文，而非仅因绝对路径/根成员列表不同便否定所有已锁来源；这是锁协议待实现的必需验收，不声称当前schema 2已经支持。

复制锁不复制源码：workspace本地path边必须显式保留可用路径、复制相应源码后改为新相对path，或明确转换为Git/原生Go版本来源；不能猜新来源。改来源若无对应已核验锁记录，报需显式更新锁；即使新Git碰巧是本地目录的远端，也不能假定内容等价。新清单和所用锁记录能保留一致来源时，拆出应支持首次 `--locked`；`--offline --locked` 另需所有可达源码字节已在本地。根策略变化需验证现有可达图是否满足新上下文，不能为了更低约束静默降版或重新MVS；不满足时给出需显式重锁的差异。

锁更新需保留未使用记录，不把每次构建写成“仅本次依赖”；锁项不能携带脚本授权。--offline仅禁fetch，不等于--locked；--locked可获取已锁内容但不能改变锁定结果。local path内容不伪装为不可变远端checksum；其源码/生成输入改变使缓存失效，锁只约束身份和有效依赖政策。feature、target、normal/test/host和编译配置进入artifact/metadata身份；锁内部索引格式仍可设计，但必须保持已确认的全workspace版本结果。
### WS-06：target、并发与信任

默认 target 在 workspace 根，以根所有权记录统筹；成员/角色/host-target/config/模式/目标名进入 unit 身份。外部 metadata 还校验选版计划、member source、feature 并集、编译器版本、Go 工具链、tags/race、本地源码摘要，保留 test/normal 隔离。普通与 workspace 两种根身份不共用裸缓存键，显式 --target-dir 也不能跨 owner 抢占。两个成员都叫 web 产物各在 module-id 下，不能都写 target/web。网站 web bin/嵌入资源可自然按现有目录目标工作，不修改独立官网分支。

锁更新采用跨进程 workspace 写锁、读取旧版本并 merge/CAS、临时文件+原子替换；A/B 并发不能丢合法记录更新或形成两个不一致的版本结果。每 unit 另有互斥及原子发布，失败留旧完整产物；脚本输出每主体独享，不让共享 OUT_DIR 多写。clean 必须取得相应 owner/unit 锁，默认仅删本项目受管目标；未知文件拒删，活跃任务拒绝或等待，不能 rm -rf 全父目录。不同 workspace 的同一外部成员允许各自独立 target，不共享源码写入。

成员资格、锁记录和依赖下载均不是脚本授权。先冻结选中图，再沿 RFC0023 按脚本主体/输入摘要检查 trust；根、成员、host helper 新主体分别授权，已有缓存不能绕过撤销。未被编译闭包触达的成员脚本不运行，全workspace版本参与也不改变此规则；CLI未选但经显式活动依赖边可达、确需构建的成员须照常授权，不能漏编；import本身不扩大图。并行任务不能从另一个脚本的输出目录读取未声明输入；fetch 不执行安装脚本；Git URL 不进入 shell。当前脚本无 OS 沙箱的边界继续成立。

### WS-07：go.work 迁入与 vendor 演进

默认只读 plan；显式 --out NEWDIR 才 staging 复制与验证。原 go.work/go.work.sum、每个成员 go.mod/go.sum、vendor 源字节和权限均不修改。不在任意目录递归找 main；命令需要每个成员 lib/bin/example 映射，批量目标映射语法尚待 CLI 统一。outside-use 成员在计划列出复制目的与路径改写，不保留隐形指向原源码的依赖；外部 replace 可选择复制闭包或显式保留规范路径，计划不得隐瞒不可搬移性。

| 输入项 | 候选无损规则 |
| --- | --- |
| go.work use | 显式members仅保留组织；原use产生的实际成员边/来源替换必须另在plan物化为依赖或明确override候选，不能只改members；版本政策按已确认全workspace统一解析 |
| go.work go/toolchain | 映射 workspace.go；Go 最低与建议分开，local 工具不满足最低则失败 |
| go.work replace | 根工作区 replace，保留 exact-version、wildcard、相对路径的原目标；优先于成员冲突 |
| go.work godebug | workspace.go.godebug，保留值/default 及与成员的覆盖关系；未实现等效生成前整项迁入拒绝 |
| 成员 go.mod require/replace/exclude/go/toolchain | 各自映射，不提升外部依赖的私有根政策；原生 Go 核查；不去重到损失版本限定 |
| 成员 godebug | 在成员清单保存；工作区构建按 work 政策屏蔽，独立构建恢复，不覆盖成 work 值 |
| 所有 go.sum/work.sum | provenance + 已知 checksum 输入，不把历史未选版本当锁；矛盾 checksum 拒绝，不联网重写 |
| retract/tool/ignore/Deprecated/未来指令 | 支持映射和发布语义前明确拒绝，给文件/指令；不能静默丢失以凑迁移成功 |

Go 原生允许没有 require 的成员间import，也会让use成员覆盖同module远端来源；这是与本提案显式合同的差异，不能在迁入后运行时模仿。迁入 plan 须逐条列“原来源/隐式边 → 显式依赖声明”，如根模板 path 加成员 `package1={workspace=true}`，或成员直接 path；如涉及冲突根政策，另列显式override候选供审阅，不默认批准新增patch机制。原有require对应版本保留作发行约束，不以成员身份推断已发布版本。

迁入分析必须声明覆盖的bin/lib/example/test、平台/tags/feature及生成源码范围，使用真实Go包归属结果而非最长module前缀猜测。若当前host分析不能无损覆盖条件import，plan列出未覆盖条件，要求明确选择迁入配置范围或补充显式边；不能声称全项目无损。未知生成器输出不能为探测而擅自执行；无法建立所请求范围完整显式图时拒绝out并给缺项。范围受限的迁入须记录允许配置，范围外构建报需补清单/重新审阅，不fallback自动member搜索。发布前在新目录用显式图验证；dry plan和失败均不改原工程。

Go 的 go.work sync 会改写成员 go.mod；本工具迁入不得调用它。也不能默认 tidy/get/vendor 修“坏输入”。计划先列每个政策和来源去向，生成与独立新目录验证全成功后发布；失败不留下半个新工程，不修改原来锁/权限。

原生 workspace vendor 仅为迁入输入；验证后转换为显式本地模块，合同见 RFC0025 与 [供应源码迁入设计](../design/vendor-local-modules.md)。Goxide 不保存 workspace vendor 模式，不用 modules.txt 控制构建或锁。成员独立时仅物化继承和本地路径、复制根锁；没有“vendor 拆出”协议或待批准转换。所有可达本地模块必须由显式路径提供，复制锁不能恢复缺失源码。原 work override/godebug 的独立政策恢复规则保持不变。

## 转译示例

### WS-08：两个 bin 与共享库

`libs/shared` 的 module 是 `example.org/shared`，lib 路径 `.`。service 的两个目录入口通过同一依赖身份引用它；work root/单命令仅改变图与目标，不更名 import path。

```goxide
// libs/shared/message.gox：片段，workspace 功能未实现
package shared
pub func Message() string { return "hello" }
```

```go
// 候选 target/<workspace>/<unit>/module/message.go
// 逻辑 module: example.org/shared，package: shared
package shared
func Message() string { return "hello" }
```

```go
// apps/service/cmd/web/main.go：原生 Go 文件，保留原包身份
package main
import (
    "fmt"
    "example.org/shared"
)
func main() { fmt.Println(shared.Message()) }
```

生成的 service Go 视图应使用稳定选版加受管 local replace 指向 shared 生成副本；作者根无 go.mod，不给每个调用点复制不同 Shared 类型。service/web 可独立构建；example hello 仅显式构建，不因 workspace 发布进入库 API。此例是目标合同，未运行 goxide workspace。

### WS-09：独立仓库开发与独立发布

```toml
# dev/goxide.toml；四个目录可各自是 Git checkout，无需改成单仓库
[workspace]
members = ["goxide", "std", "serde", "next"]
default-members = ["goxide"]
# 全workspace统一版本解析
```

这是未来示意：实际编译器目前还是普通 Go 作者工程，next 为空也不是合法成员；不能直接将此清单宣称现状可运行。需各仓库先选择迁入或保持工作区外原生来源；显式列出的空 next 要报 missing manifest，可以暂不列入。serde 必须在自身清单写 `std = { workspace = true }` 继承根的std path模板，或直接声明path；已有std版本依赖不会因std列为member就改用checkout。compiler没有声明serde依赖就不注入；import也不能补边。每仓库保留自身版本节奏/发行 module path，workspace 根不是发行模块，也不自动改变 tag 或仓库归属。

### WS-10：A/B 分开与一起（统一版本、分开编译feature）

下表所有A/B→D请求均来自显式清单边；D在根模板或成员列表中出现不会产生请求，也不将D版本来源改成本地。

| 调用 | D 最低版本与 feature 请求 | 预期 |
| --- | --- | --- |
| build A | A→D v1.0.0 + json | 锁定D v1.1.0，json；不编译B或运行B脚本 |
| build B | B→D v1.1.0 + yaml | 锁定D v1.1.0，yaml；同一根版本结果 |
| test A B | 两根同时活动 | D v1.1.0，json+yaml，以及所选测试活动 dev 请求；一次统一图 |
| 再 build A --locked | 同第一次输入 | 使用统一D v1.1.0及A的normal配置，不继承测试/yaml |
| 黑例：build A沿用旧v1.0.0或带入yaml | B已使全图要求v1.1.0，但B不是本次构建根 | 拒绝过期/不一致锁与错误配置缓存；不能以命令只选A为由降版或启用B的feature |

Go 试验确证上表**原生版本机制**，没有实现 feature 行为；没有 Cargo 测试。互斥 features、高级 target-specific dependency/多 resolver 版本仍不承诺。同一workspace同角色如果显式要求同module的不同Git commit，统一解析报冲突；只改变命令选中根不能消除冲突，需显式统一来源或拆成独立项目；Go 一个程序不能装两份同 import path 来绕过。

## 拒绝与边界

以下为待实现验收矩阵；WS-DIAG 名是候选稳定分类，不是已存在错误文本。每项白/黑必须一起落地，不能只跑成功样例。

| 编号 | 白例与预期 | 黑例与诊断/拒绝层 |
| --- | --- | --- |
| W01 身份 | 同一路径 ./lib 与 libs/../lib 规范去重 | 不同目录同 module：发现期 WS-DUPLICATE-MODULE，列两路径 |
| W02 根/锁 | 所有成员仅用根锁；拆出复制根锁后独立运行 | 存在成员锁：WS-MEMBER-LOCK；嵌套活动根冲突：WS-ROOT；继承未物化：WS-INHERIT-ROOT |
| W03 feature | A单独json，A+B并集，A/B/A可复用 | B的yaml泄漏A、normal读test metadata：配置验证拒绝，不能容忍缓存命中 |
| W04 optional | 已有完整锁时未激活Git依赖不fetch/执行；创建或更新锁可按plan读取必要元数据，不启用编译feature | 未知feature/环：图期带完整路径；活动缺离线源码明确 cache miss |
| W05 工具链 | 不同成员go按全图要求核验最低版本，建议更高仍local | B未来go要求参与全图政策，须拒绝不满足最低并指B；只选A不能绕过 |
| W06 replace | 相同政策合并，显式work replace裁决冲突 | 无根裁决两成员不同replace：Go政策预检失败；不得顺序胜出 |
| W07 未选成员 | B未被A构建触达且源码/脚本有错，A不编译或执行B；但全图feature声明/政策错误仍阻断锁解析 | B清单无法解析/无module、重复身份：发现期错误；--workspace显式选B则其语义错误生效 |
| W08 路径 | 段内glob排序，exclude scratch，显式../std | symlink、**、穿越target、嵌套root：WS-PATH，开始下载/写入前失败 |
| W09 脚本 | 活动A脚本独立授权，未触达B不运行 | 锁/成员隐式授权、撤销后缓存复用：trust拒绝；没有结果也不运行安装脚本 |
| W10 并发 | A/B并发维护同一全workspace版本结果，保留双方合法记录更新/配置索引；重名web独立输出 | 外owner target、同输出争抢、clean活跃unit：owner/lock拒绝，旧完整结果不丢 |
| W11 离开/发行 | 物化成员清单与真实发行依赖，隔离消费者成功 | 本地replace/../std/继承父字段泄入发行物：publish preflight失败，不发布 |
| W12 供应源码 | vendor 输入物化普通本地模块，冷离线与独立复制锁可用 | 缺包、错误归属/政策、旧 vendor 清单/锁：明确失败，不 fetch |
| W13 选择 | 两bin按需、example显式、跨成员同名合法 | 未限定多成员feature/多成员run/-o文件：CLI歧义失败 |
| W14 迁入 | dry plan源hash不变，新目录逐政策保留、验证后发布 | 未知指令/godebug尚未支持/已有输出/失败构建：整体不发布，不修改原工程 |
| W15 锁稳定 | 唯一根锁保留已锁来源，全workspace统一版本结果按角色验收 | 同source移动branch偷偷更新或复制锁后无声升级：--locked拒绝 |
| W16 Go身份 | 原生Go↔生成API同module/package类型，正常internal边界 | 通过复制包绕过import cycle/internal/同路径双版本：前端或Go拒绝 |
| W17 显式继承/缺边 | package1={workspace=true}继承path模板；直接path也可；同名完整version声明保留远端 | 空声明/未workspace=true不继承；源码或生成import缺显式活动来源报错；optional不靠成员旁路 |
| W18 迁入物化 | go.work隐式边在plan逐条物化，审阅后验证所声明配置范围 | 未知条件/生成import不能推断时列缺项、要求范围或拒绝；生成Go use不得恢复隐式边 |
| W19 拆出复制锁 | app复制根锁，仅取显式可达common/D记录；server专用条目留存但不下载/执行；身份校验成功可locked | 可达commit/sum缺失或身份不符：拒绝；path源码缺失不能靠复制锁恢复；不得重新解析移动tag |
| W20 Git与别名 | 显式Git URL+module锁commit；完整go+version原生互操作继续可用 | 仅别名+版本不得猜registry/Git；template未workspace=true不得继承 |

## 双向互操作

goxide 成员消费原生 Go module 仍通过 Go module 元数据/MVS；原生 Go 文件可调用选中生成库的导出函数。所有 lowering 的 package/import 身份、初始化次序、变量存储、internal/DAG、既有 sum metadata 合同不变。workspace 没有额外运行时，也不定义新 enum 参数校验。

组织不等于发布：候选 `package/publish --package MODULE` 必须在工作区外 staging 验证。先展开继承，映射活动成员覆盖到明确 publish_version 或原 require；迁入中已物化的本地成员边必须取得显式发行版本，生成require；缺版本则拒绝发行，移除本地 replace/work 政策，保留必要发行元数据，核验能从真实发行渠道取得依赖。若无法取得，说明缺哪一个版本，不能拿本地 checkout 冒充已发布；可本地 mock proxy 验证协议，但不证明公网已有该版本。独立源码发行给 goxide 和固定配置 Go 发行物是两种对象，后者含标准 go.mod/go.sum，不含父工作区清单/私有路径/target缓存/脚本信任。source发行需与传输.mod一致，遵守 RFC0025。

库发行默认不包含其他成员、workspace 根 lock、bin/examples 构建产物；examples 源码是否随包发布由显式 include/exclude 的未来发布合同决定，不改变 Go zip/import 规则。所有应用二进制独立命名交付，不能“发布workspace”顺便发布每个仓库。拆出后复制根锁可用于独立 CI；消费者的图由消费者主模块政策决定，发行库不能强迫消费者使用作者 workspace 锁。发布预检需要 standalone 计划而不是复制 workspace 图；CI 推荐两套：联合开发测试和每成员隔离发行测试。

## 验收与未定项

### 实际证据（本轮已完成）

[研究脚本](../../scripts/research/workspace_go_probe.py)仅生成临时原生 Go 模块，`GOPROXY=off GOSUMDB=off GOVCS=*:off GOTOOLCHAIN=local`，空 modulecache，所有依赖来自本地 fixture。没有私人第三方、安装脚本或 Rust工具链。记录及独立评审见 [研究验收](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/workspace-rfc-20261004.md)。执行：

```sh
python3 scripts/research/workspace_go_probe.py --go go
python3 scripts/check_design.py --docs-only
```

GO-01..04 验证 standalone / 全主模块 / 选中主模块选版；GO-05..06 replace冲突/根覆盖；GO-07..10 制造workspace vendor、删除原replace目录后冷构建、GOWORK=off不消费work vendor、缺包失败；GO-11真实Go解析go/toolchain/godebug；GO-12未选主模块的未来Go版本仍阻断原生全workspace。GO-13..15对比成员/work/成员的DefaultGODEBUG，验证policy覆盖与恢复；GO-16..17验证原生Go无require的成员import在workspace可用、standalone失败；它是与本提案的差异证据，已撤回把该行为用作goxide运行时自动成员发现的建议。GO-18..19验证最长module前缀不等于包归属，以及真实Go的多来源包歧义拒绝。脚本中的 go work vendor **仅制造研究fixture**，不是迁入行为。尚未验证 goxide workspace CLI/feature/lock/cache/trust/publish，也未测试完整所有Go指令或平台；表W01–W20是未来实现验收。

### 已确认与仍待决（按用户原编号核对）

| 用户回复编号 | 准确状态 |
| --- | --- |
| 1 “选中包是什么意思”及后续“对的” | 澄清后已确认：全workspace统一版本与根锁，命令仅构建选中成员及依赖，未选成员feature不自动带入 |
| 2 “成员不保留lock…拆分时直接复制根lock” | 已确认根唯一锁、复制与忽略无关项；替换旧草稿允许成员锁的建议，细则见WS05 |
| 3 “优先使用git url…后面…名字+版本” | 已确认当前Git URL优先和未来registry边界；保持显式workspace=true与原生Go互操作 |
| 4 “行” | 对应原四问的第4项“首片及迁移顺序”：先作者workspace核心，再go.work无损迁入及workspace vendor；这句本身不是批准第1项resolver；resolver随后由独立“对的”确认，本轮仍只修订文档 |

原四问中第三项的隐式成员覆盖已被此前显式继承指令撤回；本轮来源方向也明确，不复活隐式覆盖。最新全workspace统一版本锁及未选成员feature隔离已确认；optional锁记录组织等细节需保持这两个约束；schema/override/发布字段仍是候选实施合同。

已确认阶段顺序：阶段一作者workspace核心（根/成员、显式继承/来源、多目标、唯一根锁及拆出复制、layout/并发）；阶段二go.work逐政策迁入和workspace vendor。研究阶段未实现生产 workspace；当前实施子集以上方实现范围为准。W01–W11/W13/W15–W17/W19/W20为核心候选验收，W12/W14/W18为迁入候选验收；dev表、发布自动化、registry、profile另行设计。暂不支持Cargo数字resolver或自动工具链下载。

生产落点预估为 internal/project 清单/图/锁、cmd/goxide 选择、受管layout/metadata上下文；原则上不改宏语法基础设施，cfg使用既有配置入口。未来若需改变前端条件移除顺序，应先单列RFC0022变更并协调其owner。原研究阶段只有文档与机制研究脚本；当前实现验收另见核心验收记录，仍须跑选中图端到端、生成Go、race/vet、适用全量回归和独立对抗review。

来源选择的精确参数、交互/非交互确认、离线与正反例见[执行合同](../design/vendor-local-modules.md#来源选择执行合同2026-10-05)。

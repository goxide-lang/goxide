# RFC 0026：Cargo 风格 workspace 与 Go 多模块边界

## 状态与范围

**Proposed，整体未批准、未实现（2026-10-04）**。用户后续已明确一项约束：依赖继承必须像 `package1 = { workspace = true }` 一样显式；本篇据此撤回旧草稿的源码import自动建立成员边和按module身份自动本地覆盖。这不批准其余workspace策略或生产实施。用户要求参考 Cargo 编写 workspace RFC；本轮仅官方资料研究、原生 Go 隔离机制试验及文档评审。基线 `feat/vendor-import` / `d51117d36fa2d1ccfcf322b81195b0d753cbb8de` 的 hgo 不支持本篇 workspace 字段/命令。没有改生产代码、发布网站或合并主线；有 Go 实验不代表 hgo workspace 已通过验收。

承接 [0022 项目工具链](0022-project-toolchain.md)、[0023 构建脚本](0023-build-scripts.md)、[0025 单一项目清单](0025-unified-project.md)。继续以 `goxide.toml` / `goxide.lock` 为作者配置权威，根不并存 go.mod/go.sum/go.work/go.work.sum。原生 Go 负责 module path、版本、MVS、checksum 和编译；生成 Go 配置只在受管 target。目标是组织多包开发与独立发布，不引入 Rust resolver、所有权、Copy、enum ABI 或新借用语义。

### 官方事实与本篇选择

查阅日期 **2026-10-04**；Cargo Book 为当天在线滚动版本，不声称固定 Cargo 版本。环境无 cargo/rustc，未执行 Rust 试验，也未安装。Go 机制实际使用 `/workspace/goxide-environment/go/bin/go`，输出 **go1.27.1 linux/amd64**；版本相关结论以此测试为界，未来工具链需重验。

| 官方事实（资料） | 本篇提案，不能当作上游承诺 |
| --- | --- |
| Cargo 支持虚拟根/根包、成员选择、共享 lock/target 和显式字段继承；根 patch/profile 有特殊地位。[Workspaces](https://doc.rust-lang.org/cargo/reference/workspaces.html) | 采用显式模板继承；只有成员写 workspace=true 才继承，不自动引入依赖或把内部 path 依赖升级成成员 |
| Cargo 同次多包构建合并依赖 features；锁求解与本次编译 feature 选择不是同一过程。[Resolver](https://doc.rust-lang.org/cargo/reference/resolver.html#features) | 锁可容纳多个已求解计划；不为锁定而激活所有可选依赖 |
| Cargo feature 是加法；resolver 2 隔离 build/normal，dev 请求按需要生效。[Features](https://doc.rust-lang.org/cargo/reference/features.html#feature-resolver-version-2) | 采用闭包与角色隔离；不提供数字 resolver=2/3 假兼容 |
| Cargo path 与发布版本可以表达不同开发/发行来源。[Dependencies](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html#multiple-locations) | 本地来源必须显式声明，另保留发行版本；成员身份不能替换原version/path/git来源 |
| Go workspace 把 use 中模块视作主模块，MVS 统一同 module path；冲突 replace 可由 work 根裁决。[Go modules](https://go.dev/ref/mod#workspaces) | 新作者工程推荐显式依赖的选中闭包；Go use 的隐式来源行为不照搬，迁入计划须物化成可审阅声明 |
| Go workspace 使用 work 的 godebug，成员 go.mod 中的 godebug 被忽略。[GODEBUG](https://go.dev/doc/godebug) | 原文保留每层政策，生成等效上下文，不能取最高 go 版本后丢弃运行时默认值 |
| Go 的 go 最低版本与 toolchain 建议不同。[Toolchains](https://go.dev/doc/toolchain) | GOTOOLCHAIN=local；不自动下载，更高建议不变成硬性最低版本 |
| Go 提供 work vendor，和 module vendor 有不同作用范围。[go command](https://pkg.go.dev/cmd/go#hdr-Make_vendored_copy_of_dependencies) | 两种 vendor 身份分开，迁入不调用 vendor 重建缺失文件 |

## 源级规则

以下语法、CLI、诊断名都是**候选合同**。现有清单继续 format=2；新增 workspace 协议使用自身 format，旧工具遇到未知表必须拒绝，不能静默忽略。最终兼容门控需在实现时先通过黑例。

### WS-01：根、成员与发现

```toml
# dev/goxide.toml：虚拟根；没有 [package]
[workspace]
format = 1
members = ["apps/*", "libs/shared"]
default-members = ["apps/service"]
exclude = ["apps/scratch"]
resolution = "selected"  # 推荐；另一值 all-members 见 WS-04

[workspace.package]
go = "1.27"

[workspace.dependencies.shared]
module = "example.org/shared"
path = "libs/shared"     # 继承源的路径相对 workspace 根
publish_version = "v1.2.0" # 候选：发行时的 Go require 最低版本，不是当前 path 的锁版本
features = []
default_features = false
```

根同时有 `[package]` 时是根包，自动纳入成员。虚拟根无 package/lib/bin/build/features/go/vendor 这些包级表；根包有自己独立的包表，不能把根包 dependencies 当成全部成员依赖。root 的 go 政策和 workspace.go 政策也必须分开。

成员是**一个作者包对应一个逻辑 Go module**，可含多个 Go packages、一个 lib、多 bin/examples。module path 是包身份；不把每个 bin 变成新 module。列表项指向清单目录，不是递归扫描源码。推荐首片即支持路径段内 `*`、`?`（不跨 `/`），拒绝 `**`；展开按规范相对路径排序，exclude 后必须非空。default-members 必须是最终成员的显式路径集合，不再展开 glob；缺省时有根包选根包，虚拟根选全部。

所有相对路径相对于**声明所在清单**。path 规范化、大小写/真实路径别名和重复 module 检查先于写入；同一目录多种拼写去重并报告来源，两个不同目录同一 module 身份报错。glob 不穿越符号链接、隐藏控制目录、`.git`、`target` 或声明的 target-dir，不扫描任意目录；显式 symlink/逃逸路径拒绝并展示规范目标。显式 `../repo` 外部成员可以支持，但必须完整列举，不能用父目录 glob；授权边界只扩到该成员，不到整个父目录。target 永远不得包含/成为任何成员或其祖先。

推荐首片支持外部显式成员，便于多个独立仓库共同开发；可先只实现同父目录 checkout 布局，但不能将多仓库模式标作永久不支持。工作区根不能成为自己的外部成员，单次命令只能绑定一个活动根；两个独立 workspace 可以并发只读使用同一外部成员，各自持有自己的锁/target，不争夺成员源码所有权。嵌套 `[workspace]` 是发现边界：外层 glob 不跨越，显式纳入报 WS-NESTED，须排除或重组。

从目录入口向上找最近 TOML 包/工作区；遇到最近 workspace 就停止，不跳过它找更远父根。只有该根列举该包才加入；未列入则报 WS-NOT-MEMBER，并给出加入/exclude/独立模式建议，不悄悄使用父锁。外部成员从根命令可选；从外部成员目录调用不会遍历兄弟目录找工作区，需 `--workspace-manifest /dev/goxide.toml`，可选的 `package.workspace` 便捷指针留后续。显式 `--workspace-manifest` 必须验证成员资格。

推荐 `--no-workspace` 显式独立运行、关闭父发现；只用成员自身清单/锁/target，未展开的继承字段报错，不偷偷读父根。仓库移出后同样如此。发现阶段只核查全局路径/清单存在/可读取身份；可提取合法身份的未选成员，其坏 feature、不可满足 go 要求、源码错误、脚本不进入 selected 求解。无法读取 TOML 或 module 身份、重复身份属全局目录损坏，仍阻断并注明不是该成员源码编译错误。提供后续 `workspace check` 全成员清单验证入口，不默认执行源码/脚本。

### WS-02：显式继承与本地成员依赖

```toml
# apps/service/goxide.toml：候选继承语法
[package]
format = 2
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

workspace.dependencies 是模板库，**不注入依赖边**。成员按 alias 显式 `workspace=true` 才继承。module/path/git/rev/subdir/version/publish_version 从模板一次性取得，成员不得同时重新指定来源；features 按集合相加；optional 只在成员声明，根模板禁止 optional；default_features 以成员显式值覆盖模板缺省，但任何其他活动依赖边仍可启用默认请求。本篇新增的键沿用现有 TOML `default_features` 拼写；CLI 使用 `--no-default-features`。共享 package 字段首片只允许 go、toolchain 显式 `字段.workspace=true`；module 身份不继承。version/license/readme 等先有独立发布元数据合同再扩展，不假定 Cargo 字段已被本仓库实现。

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

完全省略成员的 package1 条目也不继承；若源码需要该库且显式活动依赖图没有提供它，报未声明依赖，不因 member/template 存在而搜索。独立完整声明 `package1 = { module = "example.org/package1", version = "v1.2.0" }` 则合法：即使根有同名 path 模板、同身份 member，仍使用版本来源。显式依赖图包含已声明的传递依赖和原生 Go 解析出的依赖，并非要求每个 import 都新增直接增强依赖条目；关键是不能借 workspace 成员列表扩大来源图。

未激活 optional 边不会被 import、同名模板或成员搜索激活；没有其他有效活动边提供该来源时，保留源码 import 报 WS-INACTIVE-DEPENDENCY。关闭文件不装载 import。纯 path 成员依赖无需虚构版本；`publish_version` 如存在是发行最低版本，原生 go.require 保留最低要求。显式本地替换不声称未打 tag 的工作树满足发布版本；发行预检用真实版本验证路径/major 和可获取性，不能把工作树 HEAD 当已发布 tag。

path + publish_version 是**新语法**，区别于现有 version/path/git 互斥来源；不放宽 version 到 Rust 的 ^/~ 范围。Go `/vN`、pseudo-version 和 MVS 仍由真实 Go 校验。Git 锁定 commit+subdir；branch/tag 可移动但已有锁不重新漂移；URL 是 argv 数据，不经 shell，不保存凭据，不触发安装脚本。

来源冲突按“解析角色 + module path”识别。两个活动边若要求不同 path/Git commit 或成员路径不匹配，报 WS-SOURCE-CONFLICT，列出边与清单位置；两个版本要求交 MVS，不当成来源冲突。成员身份不能裁决来源冲突或覆盖普通版本边。若同次选择本地成员B为构建根，而A仍显式依赖同module的远端版本，不能暗取本地B：统一域报WS-SOURCE-CONFLICT，建议分开构建或显式改变A的来源声明。workspace 根可用下述 Go replace 明确裁决，结果出现在 plan/lock；别名不能制造两份同 import path 的类型。

### WS-03：CLI 与目标选择

候选命令，需和独立 Cobra/POSIX 迁移统一；本分支未实现这些新 flag。保留现有 `--go`，不用 `-go`。`-h` 帮助、`-o` 输出符合 CLI 项目约定。为避免 module path 中 `/` 歧义，`--package` 使用完整 module 身份；将来可给唯一短名，歧义必须列候选。

```sh
hgo build --workspace-manifest ./goxide.toml --package example.org/service --bin web
hgo run --workspace-manifest ./goxide.toml --package example.org/service --bin web -- --listen=:8080
hgo build --workspace
hgo test --package example.org/a --package example.org/b --locked --offline
hgo run --package example.org/service --example hello
hgo build --package example.org/a --features example.org/a/json --no-default-features
hgo build --no-workspace --manifest-path ./apps/service/goxide.toml
hgo import-go --workspace --plan ./old/go.work
hgo import-go --workspace --out ./new --bin example.org/a=app:cmd/app ./old/go.work
```

工作区入口不选成员按 default-members；成员目录入口默认该成员，根包目录适用根的 default-members；`--workspace` 显式全部，和 `--package` 互斥。重复 package 去重、未知报错，不只选第一个。--manifest-path 定位包或根不等于关闭工作区发现。

单成员现有 bin/lib/example 规则不变；`run` 必须恰好一个成员、一个可执行目标。多成员批量 build/test 推荐默认每成员 lib（若有）和所有未缺 required_features 的 bins；缺 feature 的目标输出 skip 原因，显式选中的目标缺 feature 则失败，不自动开启。examples 默认不构建；批量 `--examples` 显式包含，`--all-targets` 包括 lib/bin/examples/tests，需单独计划 dev 闭包。多成员命令中的 `--bin/--example/--lib` 首片拒绝歧义并建议逐成员选择，不把同名 web 广播到多个模块。跨成员 bin 同名合法，成员内同类目标重复名拒绝。多产物 `-o FILE` 报错，使用布局输出/显式输出目录协议。

`--features module/feature` 以最后 `/` 分隔已知成员 module 和 feature；多成员时禁止未限定的 feature。只允许给选中根发请求（传递依赖经清单边请求），--no-default-features 关闭这些根的默认请求，不抹去传递边。单成员保留未限定 feature 简写。required_features 检查与 cfg 仍属包自己的 feature 集合。

### WS-04：解析范围、feature 和角色

**建议新建 workspace `resolution="selected"`**：一次命令选中的根及沿显式活动依赖边可达的成员/外部依赖形成一个 target 求解域。同一 module path 使用一次 Go MVS 选版和 feature 并集；同时选 A/B 会合并，分别调用则各自求解。缓存可有不同配置实例，但一次程序中不能混装不同配置的同一 Go 包。不是 Rust 同一 crate 多版本协议。即使不同 bin 最终分进不同进程，批量命令仍推荐统一一次 target 图，行为易解释；要互不影响分别运行命令。

`resolution="all-members"` 则所有成员作为版本/根政策参与者，即使只编译 A；**不因此启用未被编译闭包触达成员的 feature 或执行脚本**。这是 go.work 版本/根政策迁入的候选策略，不授予 use 的隐式来源覆盖；仍须显式依赖及迁入物化，并在 plan 标出 B 造成的升级/最低 Go 要求。原生 require 不受增强 optional feature 控制。两个模式的求解参与者、编译参与者、脚本主体必须分别列出，不能用一个“所有节点”集合混用。

先由清单显式活动依赖求解版本与 feature 图，再在既有图中按 Go 文件条件/cfg 和实际目标发现包 import。import 只选择图内包，不反向激活成员、feature 或来源；不存在旧草稿的“import→自动成员边→默认feature”固定点。feature 并集只沿显式活动边计算；MVS 改选版本导致清单/feature声明改变时仍须从根重算有效图，丢弃旧版本请求并检测不收敛，不能借这项正常求解工作恢复源码隐式边。

脚本执行在图冻结之后；生成文件或宏新增 import 也只能使用已声明活动图中的来源，缺边报 WS-UNDECLARED-GENERATED-EDGE，不能执行脚本后自动扩大依赖/信任闭包。模板未被继承、optional未启用和CLI未选无关成员都不构成额外来源。

对同一固定活动图，features 单调并集；默认请求有一条开启即保留。仅由未激活可选增强边可达、且没有原生require或其他活动边请求的依赖不下载、不读源码/执行脚本；all-members的原生require参与不能被optional声明抹掉。当前成员自己的未知 feature/feature cycle 仍按 RFC0022 明确诊断。MVS 升级后重新验证最终选中版本的 features；不存在请求不能换低版本逃过 MVS。cfg 时序沿用 RFC0022，workspace 不新增声明级标注、不把 cfg! 普通 if 变成死分支免检；关闭文件仅既有词法/括号及 package/import 前缀检查，不宣称完整语法验证。

host 构建脚本有独立 host 版本/feature 域，与 target 不合并；同一 helper 若被同次多个脚本共用，host 内并集，其角色键不同于 target。脚本接收所属 target 包的已冻结 features，但不能返写图。Go 包 DAG 仍须合法；host 指向 target 不能造成“先生成自身才能编译生成器”的循环。

dev-dependencies 属候选后续能力：只加入所选成员 test/examples 所需闭包；普通 build 不激活，依赖库的 dev 边不递归传播。不能把全 workspace 测试 feature 泄漏给普通构建。首片可沿用现有显式 test 来源和 build.dependencies，不在 parser 接受后默默当 normal；新 dev 字段在未实现时必须明确拒绝。test 与 normal metadata 保持现有拒绝机制，还需记录 workspace 图/角色/计划身份。

### WS-05：Go 政策、锁与可复现

以下工作区级 override 仍是未批准的候选条目，不因用户要求显式继承就自动获准；若保留，必须实际写出条目才生效，不由成员列表推导。候选 workspace 根政策采用 `[[workspace.go.replace]]`（字段与 RFC0025 go.replace 相同），以及 `workspace.go.go`、`workspace.go.toolchain`、`workspace.go.godebug` 明确对应 go.work；不引入含糊 patch 竞争层。成员 go.require/replace/exclude/go 保留原文语义：selected 的求解根和活动本地成员被声明为主模块参与者，其根政策按真实 Go 合并；外部版本依赖自己的 replace 不提升。冲突主模块 replace 由显式 workspace replace 裁决，不能按遍历先后选。exact-version replace 与 wildcard 优先次序委托 Go；成员替换自己/其他活动主模块的限制也交 Go，不用全局覆盖躲过检查。

生成策略不是定案：可合成受管的解析/构建视图，但不能将全部member目录直接写进Go use而重新引入隐式来源覆盖。版本/根政策参与者与编译来源映射分开；即使all-members，也须仅采用显式图认可的source。生成go.work仅在其use行为与该显式图等价时可用，否则需分离视图或明确冲突，不能让后端Go“顺便找到成员”绕过前端缺边诊断。必须保留每模块 go 语言版本、root exclude/replace 合并、toolchain 建议及 GODEBUG 上下文；不能把全体 go 最高值写回每个包。新工程 selected 模式生成 work go 至少满足活动成员和工具自身最低版本；若显式 workspace.go.go 太低报错，不静默升级。此 work go 会影响运行时默认值，plan 必须报告；独立发行用成员自身 policy，需在脱离 workspace 的发布验收中比较。未来 Cargo 式 profile 可以仅根定义，但与 Go -race/tags/opt 的映射尚无合同，首片不接受该表。

建议一份根 **goxide.lock schema 3（候选）**，包含工作区协议/根政策摘要、成员身份清单、source→immutable Git commit/Go checksum、vendor 内容，以及按求解输入索引的多个 plans。schema 2 不原地猜升级；显式只读迁移计划可将有效记录核验后写新目录。全局身份清单用于审计，不能作为每个 selected plan 的失效键：未选且不被 A 编译闭包触达的 B，其源码/feature/Go 政策变更不应破坏 A 的 --locked。成员身份重复/根选择政策改变仍须预检。

Plan key 至少包含协议、resolution 模式、排序后的求解主模块及各自有效依赖/政策、活动来源与约束、角色（normal/test/host）、根运行时政策；等价版本图可复用锁项，feature/config/目标仍进入 artifact key。锁不是“最后一次 build 的唯一图”：A→B→A 保留历史计划，第三次可 --locked；A+B 新图若无锁项，--locked 精确报缺计划，不能借 B 的锁改写 A。--offline 仅禁 fetch，不等于 --locked；--locked 可验证/获取已锁内容但不能更新解析结果，--offline --locked 还要求所有来源字节已在本地。local path 内容不伪装成不可变 checksum；源码/生成输入变化使缓存失效，锁仅约束其身份和有效依赖政策。

根锁是活动 workspace 唯一权威。独立仓库成员自己的锁可保留用于独立构建，workspace 模式明确输出“未采用成员锁”，不合并、不改写；两份锁不叫两份同时生效的配置。若成员锁的 Git pin 与根不同，plan 展示差异；--locked 使用根既有 pin，不意外切换。新根 lock 不存在时须显式 init/导入计划，不能把多个子锁互相覆盖。成员退出 workspace 后取自己的锁；若依赖继承未物化则 WS-INHERIT-ROOT，必须先生成独立清单；缺锁 --locked 失败。

### WS-06：target、并发与信任

默认 target 在 workspace 根，以根所有权记录统筹；成员/角色/host-target/config/模式/目标名进入 unit 身份。外部 metadata 还校验选版计划、member source、feature 并集、编译器版本、Go 工具链、tags/race、vendor 摘要，保留 test/normal 隔离。普通与 workspace 两种根身份不共用裸缓存键，显式 --target-dir 也不能跨 owner 抢占。两个成员都叫 web 产物各在 module-id 下，不能都写 target/web。网站 web bin/嵌入资源可自然按现有目录目标工作，不修改独立官网分支。

锁更新采用跨进程 workspace 写锁、读取旧版本并 merge/CAS、临时文件+原子替换；A/B 并发不能丢计划。每 unit 另有互斥及原子发布，失败留旧完整产物；脚本输出每主体独享，不让共享 OUT_DIR 多写。clean 必须取得相应 owner/unit 锁，默认仅删本项目受管目标；未知文件拒删，活跃任务拒绝或等待，不能 rm -rf 全父目录。不同 workspace 的同一外部成员允许各自独立 target，不共享源码写入。

成员资格、锁记录和依赖下载均不是脚本授权。先冻结选中图，再沿 RFC0023 按脚本主体/输入摘要检查 trust；根、成员、host helper 新主体分别授权，已有缓存不能绕过撤销。未被编译闭包触达的成员脚本不运行，all-members 的版本参与也不改变此规则；CLI未选但经显式活动依赖边可达、确需构建的成员须照常授权，不能漏编；import本身不扩大图。并行任务不能从另一个脚本的输出目录读取未声明输入；fetch 不执行安装脚本；Git URL 不进入 shell。当前脚本无 OS 沙箱的边界继续成立。

### WS-07：go.work 迁入与 vendor 演进

默认只读 plan；显式 --out NEWDIR 才 staging 复制与验证。原 go.work/go.work.sum、每个成员 go.mod/go.sum、vendor 源字节和权限均不修改。不在任意目录递归找 main；命令需要每个成员 lib/bin/example 映射，批量目标映射语法尚待 CLI 统一。outside-use 成员在计划列出复制目的与路径改写，不保留隐形指向原源码的依赖；外部 replace 可选择复制闭包或显式保留规范路径，计划不得隐瞒不可搬移性。

| 输入项 | 候选无损规则 |
| --- | --- |
| go.work use | 显式members仅保留组织；原use产生的实际成员边/来源替换必须另在plan物化为依赖或明确override候选，不能只改members；版本政策推荐all-members仍待决 |
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

现有 `[vendor] mode="go-mod"` 只支持 RFC0025 单模块，workspace 仍明确不支持。建议后续新增独立 `[workspace.vendor] mode="go-work"`：保留 workspace modules.txt 标记、根/成员政策、全部 vendor 原始字节、许可证、文件模式及独立内容锁，不冒充 proxy h1。采用真实 Go work vendor 视图，生成 Go workspace 必须与原 vendor 的主模块集合匹配；首片 vendor 迁入固定 all-members，即使只编译 A 也保留完整政策集合。vendor模式同样必须先核验已物化的显式依赖/来源图；原Go主模块集合只用于政策与vendor一致性，不授权源码从未声明成员取包。无法同时保持原vendor政策和显式source图时明确拒绝该迁入，不靠Go use补边。不要从部分 modules.txt 逆造完整 go.mod。

模块 vendor 与 workspace vendor 不能合并去重或任挑一个；module-vendor 工程独立构建保持现状，加入非vendor workspace 后若要使用新的统一来源，须显式模式切换/重锁。多个各自修改过 vendor 的成员不能未经选择合成一份。workspace vendor 无需原 replace 目录仍应可冷离线构建（本次 GO-08 已验证 native 机制），缺失一个实际 import 包就失败并给路径，不联网补全；不能宣称含依赖自己的 tests/任意新 import。vendor 中增强 .hgo、脚本或混合增强来源支持需另有合同，首片不自动执行/展开。工作区增删成员或改政策必须重新核验原 vendor，不能擦掉 ## workspace 伪装单模块。

## 转译示例

### WS-08：两个 bin 与共享库

`libs/shared` 的 module 是 `example.org/shared`，lib 路径 `.`。service 的两个目录入口通过同一依赖身份引用它；work root/单命令仅改变图与目标，不更名 import path。

```hgo
// libs/shared/message.hgo：片段，workspace 功能未实现
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

生成的 service Go 视图应使用稳定选版加受管 local replace 指向 shared 生成副本；作者根无 go.mod，不给每个调用点复制不同 Shared 类型。service/web 可独立构建；example hello 仅显式构建，不因 workspace 发布进入库 API。此例是目标合同，未运行 hgo workspace。

### WS-09：独立仓库开发与独立发布

```toml
# dev/goxide.toml；四个目录可各自是 Git checkout，无需改成单仓库
[workspace]
format = 1
members = ["goxide", "std", "serde", "next"]
default-members = ["goxide"]
resolution = "selected"
```

这是未来示意：实际编译器目前还是普通 Go 作者工程，next 为空也不是合法成员；不能直接将此清单宣称现状可运行。需各仓库先选择迁入或保持工作区外原生来源；显式列出的空 next 要报 missing manifest，可以暂不列入。serde 必须在自身清单写 `std = { workspace = true }` 继承根的std path模板，或直接声明path；已有std版本依赖不会因std列为member就改用checkout。compiler没有声明serde依赖就不注入；import也不能补边。每仓库保留自身版本节奏/发行 module path，workspace 根不是发行模块，也不自动改变 tag 或仓库归属。

### WS-10：A/B 分开与一起

下表所有A/B→D请求均来自显式清单边；D在根模板或成员列表中出现不会产生请求，也不将D版本来源改成本地。

| 调用（selected） | D 最低版本与 feature 请求 | 预期 |
| --- | --- | --- |
| build A | A→D v1.0.0 + json | D v1.0.0，json；不读 B 源码/脚本 |
| build B | B→D v1.1.0 + yaml | D v1.1.0，yaml；独立计划 |
| test A B | 两根同时活动 | D v1.1.0，json+yaml，以及所选测试活动 dev 请求；一次统一图 |
| 再 build A --locked | 同第一次输入 | 用 A 原计划和 normal 配置，不继承测试/yaml |
| build A（all-members） | B 原生 require 参与但 B feature 不活动 | D v1.1.0；json，不能因版本图里有 B 就运行其脚本 |

Go 试验确证上表**原生版本机制**，没有实现 feature 行为；没有 Cargo 测试。互斥 features、高级 target-specific dependency/多 resolver 版本仍不承诺。两个根如果要求同 module 的不同 Git commit 同次报冲突，分开调用可各自锁定；Go 一个程序不能装两份同 import path 来绕过。

## 拒绝与边界

以下为待实现验收矩阵；WS-DIAG 名是候选稳定分类，不是已存在错误文本。每项白/黑必须一起落地，不能只跑成功样例。

| 编号 | 白例与预期 | 黑例与诊断/拒绝层 |
| --- | --- | --- |
| W01 身份 | 同一路径 ./lib 与 libs/../lib 规范去重 | 不同目录同 module：发现期 WS-DUPLICATE-MODULE，列两路径 |
| W02 根/锁 | 外部成员从显式根用根锁；--no-workspace 用自身锁 | 嵌套根被当成员/同时活动双根：WS-NESTED/WS-ROOT；继承脱根 WS-INHERIT-ROOT |
| W03 feature | A单独json，A+B并集，A/B/A可复用 | B的yaml泄漏A、normal读test metadata：配置验证拒绝，不能容忍缓存命中 |
| W04 optional | 未激活Git依赖不fetch、不执行；活动正常锁commit | 未知feature/环：图期带完整路径；活动缺离线源码明确 cache miss |
| W05 工具链 | 不同成员go取活动最低要求，建议更高仍local | selected只A不受B未来go影响；all-members则必须拒绝不满足最低，并指B |
| W06 replace | 相同政策合并，显式work replace裁决冲突 | 无根裁决两成员不同replace：Go政策预检失败；不得顺序胜出 |
| W07 未选成员 | B未被A触达且源码/feature/脚本无效但身份可读，selected A成功 | B清单无法解析/无module、重复身份：发现期错误；--workspace显式选B则其语义错误生效 |
| W08 路径 | 段内glob排序，exclude scratch，显式../std | symlink、**、穿越target、嵌套root：WS-PATH，开始下载/写入前失败 |
| W09 脚本 | 活动A脚本独立授权，未触达B不运行 | 锁/成员隐式授权、撤销后缓存复用：trust拒绝；没有结果也不运行安装脚本 |
| W10 并发 | A/B并发增补lock保留两计划，重名web独立输出 | 外owner target、同输出争抢、clean活跃unit：owner/lock拒绝，旧完整结果不丢 |
| W11 离开/发行 | 物化成员清单与真实发行依赖，隔离消费者成功 | 本地replace/../std/继承父字段泄入发行物：publish preflight失败，不发布 |
| W12 vendor | workspace原字节+policy冷离线；module独立vendor继续成功 | 缺包、## workspace误作go-mod、多个vendor冲突：预检/Go vendor失败，不fetch |
| W13 选择 | 两bin按需、example显式、跨成员同名合法 | 未限定多成员feature/多成员run/-o文件：CLI歧义失败 |
| W14 迁入 | dry plan源hash不变，新目录逐政策保留、验证后发布 | 未知指令/godebug尚未支持/已有输出/失败构建：整体不发布，不修改原工程 |
| W15 锁稳定 | B内容变化不使selected A计划消失，all-members变化如实使图变 | A+B无计划却借单A锁；同source移动branch偷偷更新：--locked拒绝 |
| W16 Go身份 | 原生Go↔生成API同module/package类型，正常internal边界 | 通过复制包绕过import cycle/internal/同路径双版本：前端或Go拒绝 |
| W17 显式继承/缺边 | package1={workspace=true}继承path模板；直接path也可；同名完整version声明保留远端 | 空声明/未workspace=true不继承；源码或生成import缺显式活动来源报错；optional不靠成员旁路 |
| W18 迁入物化 | go.work隐式边在plan逐条物化，审阅后验证所声明配置范围 | 未知条件/生成import不能推断时列缺项、要求范围或拒绝；生成Go use不得恢复隐式边 |

## 双向互操作

hgo 成员消费原生 Go module 仍通过 Go module 元数据/MVS；原生 Go 文件可调用选中生成库的导出函数。所有 lowering 的 package/import 身份、初始化次序、变量存储、internal/DAG、既有 sum metadata 合同不变。workspace 没有额外运行时，也不定义新 enum 参数校验。

组织不等于发布：候选 `package/publish --package MODULE` 必须在工作区外 staging 验证。先展开继承，映射活动成员覆盖到明确 publish_version 或原 require；迁入中已物化的本地成员边必须取得显式发行版本，生成require；缺版本则拒绝发行，移除本地 replace/work 政策，保留必要发行元数据，核验能从真实发行渠道取得依赖。若无法取得，说明缺哪一个版本，不能拿本地 checkout 冒充已发布；可本地 mock proxy 验证协议，但不证明公网已有该版本。独立源码发行给 hgo 和固定配置 Go 发行物是两种对象，后者含标准 go.mod/go.sum，不含父工作区清单/私有路径/target缓存/脚本信任。source发行需与传输.mod一致，遵守 RFC0025。

库发行默认不包含其他成员、workspace 根 lock、bin/examples 构建产物；examples 源码是否随包发布由显式 include/exclude 的未来发布合同决定，不改变 Go zip/import 规则。所有应用二进制独立命名交付，不能“发布workspace”顺便发布每个仓库。成员自身锁可用于独立 CI；消费者的图由消费者主模块政策决定，发行库不能强迫消费者使用作者 workspace 锁。发布预检需要 standalone 计划而不是复制 workspace 图；CI 推荐两套：联合开发测试和每成员隔离发行测试。

## 验收与未定项

### 实际证据（本轮已完成）

[研究脚本](../../scripts/research/workspace_go_probe.py)仅生成临时原生 Go 模块，`GOPROXY=off GOSUMDB=off GOVCS=*:off GOTOOLCHAIN=local`，空 modulecache，所有依赖来自本地 fixture。没有私人第三方、安装脚本或 Rust工具链。记录及独立评审见 [研究验收](../reviews/workspace-rfc-20261004.md)。执行：

```sh
python3 scripts/research/workspace_go_probe.py --go /workspace/goxide-environment/go/bin/go
python3 scripts/check_design.py --docs-only
```

GO-01..04 验证 standalone / 全主模块 / 选中主模块选版；GO-05..06 replace冲突/根覆盖；GO-07..10 制造workspace vendor、删除原replace目录后冷构建、GOWORK=off不消费work vendor、缺包失败；GO-11真实Go解析go/toolchain/godebug；GO-12未选主模块的未来Go版本仍阻断原生全workspace。GO-13..15对比成员/work/成员的DefaultGODEBUG，验证policy覆盖与恢复；GO-16..17验证原生Go无require的成员import在workspace可用、standalone失败；它是与本提案的差异证据，已撤回把该行为用作goxide运行时自动成员发现的建议。GO-18..19验证最长module前缀不等于包归属，以及真实Go的多来源包歧义拒绝。脚本中的 go work vendor **仅制造研究fixture**，不是迁入行为。尚未验证 hgo workspace CLI/feature/lock/cache/trust/publish，也未测试完整所有Go指令或平台；表W01–W18是未来实现验收。

### 已确认约束与三个待决问题

**已确认（本轮用户修正）**：成员依赖必须显式，只有 `workspace=true` 继承模板；成员身份和import不添加依赖，不替换来源。旧草稿“成员覆盖与发布”项中的“自动成员覆盖/无require开发边”建议撤回，不能作为备选继续默认推进。发行元数据字段与override具体协议仍是提案，不因显式原则而获批。其余原三项保持待决：

1. **默认解析范围**：推荐新建 selected、迁入 go.work 明示 all-members；两者同次多根 feature并集、host分离。备选全局all-members默认更贴Go，但无关模块会影响单包开发。批准前不实现默认。
2. **锁与独立仓库**：推荐根lock多计划，允许成员独立lock保留且活动时明确不用；备选强制删除成员lock不利独立仓库。需要确认本提案的“唯一权威是每次作用域唯一”，不是整棵目录只能有一个锁文件。

3. **首片及迁移顺序**：推荐先作者workspace核心，再无损go.work迁入及go-work vendor；不是将常见Go输入永远拒绝。完整政策(godebug等)未映射前迁入要阻断并说明，不可宣传完整迁入。

推荐阶段一：根/成员/显式继承、路径段glob与外部显式成员、选择/多目标、selected与all-members图分层、MVS/feature/host隔离、多计划锁、target所有权/并发、W01–W11/W13/W15–W17。阶段二：go.work逐指令无损迁入、workspace vendor冷离线、W12/W14/W18及Go消费者隔离预检。dev表/发布自动化/更广glob/共享发布元数据/profile另逐项实现；暂不支持target-specific features、互斥feature求解、Cargo数字resolver或自动工具链下载。

生产落点预估为 internal/project 清单/图/锁、cmd/hgo 选择、受管layout/metadata上下文；原则上不改宏语法基础设施，cfg使用既有配置入口。未来若需改变前端条件移除顺序，应先单列RFC0022变更并协调其owner。本轮只有文档与机制研究脚本，不要求全量生产race/vet重新跑来冒充workspace已实现；实现阶段须跑选中图端到端、生成Go、race/vet、适用全量回归和独立对抗review。

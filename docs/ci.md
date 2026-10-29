# 最小 CI / PR 流程

工作在从最新 `main` 开出的短期分支，普通 push 后创建 Draft PR，正文关联 issue、
准确源码/验证 SHA 和已知缺口。CI 配置的变更独立提交，不混入已验收功能分支。
普通 CI 不自动合并、发布、关闭 issue 或修改分支保护；发布由下述语义版本 tag 工作流单独触发。

| 触发 / 层 | 检查范围 |
| --- | --- |
| 所有 PR、push main、手动触发 | 全树 race 一次、普通模式差异与生产预算检查、独立 vet/gofmt、文档检查 |
| 所有 CI | 六个平台的 CLI 构建及解压运行验收，插件 Node 测试及通用 VSIX 打包；产物保留 7 天 |
| 网站及依赖相关变更 | 网站工具与 Node/WASI 回归、离线 CLI 示例/导出数据/WASM 构建、sandbox 浏览器及路由验收、带 `goxideweb` 标签的 HTTP race/vet |
| CI summary | 无路径过滤，始终汇总；失败、取消或应运行却跳过都不能报告成功 |

PR 在 `opened`、`synchronize`、`reopened`、`ready_for_review` 和 `converted_to_draft` 时运行；同一 PR
的新运行取消旧运行。普通 feature push 不另触发，避免与 PR 双跑。手动触发是完整门禁。
读取精确 head 对应 run 的状态/汇总，失败才读取对应 job 日志。最终 SHA 已绿后停止
重复本地或远端验证；本地仅做实现所需的定向检查。

网站选择包含 `website/`、`cmd/`、`internal/`、`go.mod/go.sum`、`scripts/build.go`、`examples/`、
`docs/rfcs/` 和 CI 本身；同时包含实际发布的五份指南/状态文档，以及阅读指南、文法、翻译语义、
前端 AST、enum 表示和已确认决策六份设计文档。文件清单与 `website/docs_content.py` 对齐。
生成网站保持被忽略，不上传或部署；浏览器诊断单独保留为 CI 产物。
网站使用 `python3 -B website/build.py --worktree` 构建当前 checkout；CLI 示例、
导出数据与 WASM 记录同一源码摘要，并在构建前后校验一致。网站 job 另执行托管审计、浏览器验收和 HTTP 包的 race/vet。

准备任务从 `go.mod` 选择配套 Go，固定具体补丁版本和 checkout 提交，供全部检查和构建使用。
Python 3.12，Linux runner 提供 C 编译器；网站和插件各自使用 Node 22 与对应的 `package-lock.json` 执行 `npm ci`。
完整 Git 历史用于 catalog 快照。缓存按锁文件选取；先用仓外清单副本
预热完整固定模块图并 `verify`，逐字节确认 `go.mod/go.sum` 未变。网站 job 随后设置 `GOPROXY=off`；
原生检查保留 Go proxy 配置。P5 普通模式使用独立空模块缓存首次获取固定依赖；race 模式复用
共享模块缓存，缺少固定依赖时仍可独立获取。两种模式都显式执行后续 offline/locked 重放。
Go 构建缓存和宏机器码缓存分别恢复；冷 cache、离线、校验和、权限/安全夹具仍验证各自的边界。
全树测试移除外层 `GOWORK` 以保留 workspace 发现合同；其余脚本保留自身隔离规则。
原生 `GOMAXPROCS=4`；Node/WASI 和浏览器验收步骤将其设为 1。

宏宿主默认将机器码缓存写入 `os.UserCacheDir()/goxide/wazero`，Linux 为 `~/.cache/goxide/wazero`；
`GOXIDE_WASM_CACHE` 可指定私有缓存根。磁盘目录按 wazero 依赖的版本和校验和隔离，依赖身份未知或
使用 replace 时只保留宿主内存缓存。每个宿主独占并关闭自己的运行时与缓存对象，磁盘文件可由后续
宿主或进程复用；每次宏调用仍创建新实例，各编译请求的取消和执行预算保持独立。
目录初始化失败时继续使用内存缓存；模块加载失败时，在原截止时间内禁用磁盘缓存并重试一次。

race 各片在独立 runner 上恢复宏缓存，按主机平台、`go.sum` 和分组隔离，以提交更新缓存快照，
并用同组前缀恢复此前的快照。各 runner 的目录互相独立；同一 runner 上首次并发请求同一模块仍可能
重复编译。native 的每条测试命令使用独立空缓存根，宏宿主的冷启动、取消和预算自测也使用各自的
临时目录，保证机器码缓存不会掩盖冷启动行为。

## 测试分工与并发

全树行为测试只在 race 模式执行一次。包集合来自 `go list -mod=readonly -race ./...`，
分组校验互斥且并集等于完整集合；新增包自动进入 core。

| 分组 | 保留的覆盖 | 执行方式 |
| --- | --- | --- |
| core | 词法/解析、展开、解析名字、检查、项目工具、宏宿主等其余包 | 独立 runner，`-p=2` |
| compile-1…2 | 项目编译、正式库、跨模块协议、锁文件与依赖准备 | 顶层用例分两片，独立 runner，片内串行 |
| lower、cli、prototype | 生成 Go 行为、CLI 和跨模块序列化 | 每组独立 runner，组内 `-p=1` |
| lsp | LSP、gopls 协议、未保存编辑、取消/进程回收及真实宏 | 两包共用独立 runner，`-p=1` |
| acceptance-1…4 | 所有语言、正式库、跨包与安装后 CLI 验收 | 同一包的顶层用例分四片，独立 runner，片内串行 |
| native | 下表中的普通模式专属与生产预算验收 | 独立 runner，顺序执行 |
| compiler、web、extension | 六平台归档实跑、WASM/HTTP、编辑器行为与 VSIX | 各自独立作业，保留构建目标差异 |

LSP runner 安装与扩展相同的 `gopls v0.24.0` 并加入 PATH，真实后端协议和导航回归必须执行。
CLI runner 安装 `Delve v1.27.2`，以绝对路径设置 `GOXIDE_TEST_DLV`，执行真实断点和无调试运行的 DAP 合同；
这两个检查不依赖编辑器 UI。工具安装失败直接使作业失败。
固定主机的性能及资源观察仍由显式环境变量单独启用，不在共享 runner 上断言。
这两个作业还检查原始测试 JSON：CLI 不允许跳过，LSP 仅允许上述三项固定主机观察跳过；
子用例跳过同样会被检查，父用例通过不能掩盖后端合同未执行。

CLI 与 acceptance 测试在各自测试进程内惰性构建默认 CLI，沿用 `scripts/build.go` 的 SDK 印记。
构建环境取自测试进程启动时；用例继续使用各自的工作目录、环境和输入，进程结束后清理共享二进制。
`go test -list` 不触发构建；特殊版本参数与未盖 SDK 印记的构建合同独立执行。

compile 和 acceptance 通过当前 race 构建的 `go test -list '^(Test|Example|Fuzz)'` 取得用例，
排序后轮转分片。每个顶层用例及其全部子用例只属于一片；校验片非空、互斥且完整，
不维护容易漏新增用例的手写名单。分片包含 example 与 fuzz 的 seed corpus，未启用持续 fuzz。
各片按数量分配；长子用例仍会影响均衡，依据产物中的真实时间调整，分片数不等于提速倍数。

普通模式只运行以下已有检查：

| 测试 | 原因 |
| --- | --- |
| `lower.TestProcSnapshotPreparedRoleLifetime` | `!race`；真实 WASI 角色准备、快照与生命周期 |
| `playground.TestPackageCheckedProceduralSnapshot` | `!race`；完整 SDK 快照、传输及消费边界 |
| `procmacro.TestWASIHostSDKCommand` | 完整 SDK 冷启动、响应、输出限额和失败后恢复 |
| `procmacro.TestWASIHostGoPackageInitIsBounded` | Go guest 初始化也受截止控制 |
| `procmacro.TestWASIHostTimeoutCancellationAndCumulativeBudget` | 单次/累计耗尽、取消及单活动实例 |
| `compile.TestFormalCFGDerivedProtocols/true` | 固定公开 std/serde 的四种派生、业务输出及 locked 重放 |
| `format.TestFormattedTraitsMatchMacrosAndGenericsBuildAndRun` | 格式化后真实生成、稳定重放与运行输出 |
| `prototype.TestDefaultOrdinaryHelperCoexists` | formal 公共夹具的普通预算、正式 Default 派生与 Go 断言 |
| `lsp.TestCandidateUsesFreshMacroSession` | 关闭分析 session 后，候选修改使用独立真实宏 session |
| `lsp.TestLanguageServerUnsavedProceduralMacro` | 真实 LSP 传输及宏、传递依赖、调用方的未保存修改 |
| `cli.TestGenerateMissingRuntimeDependency` | CLI 原位生成的正式 std 宏、缺依赖诊断及补齐后的生成 |
| `compile.TestP5ProjectBuildRunTestAndStableLock` | 外部测试包的真实宏宿主、空模块缓存、生成代码及 offline/locked 重放 |

这些是生产预算的代表性验收，不重复全部语言输入。宏生产预算仍是单次 5 秒、
一个编译请求累计 60 秒；WASM 准备、编译、初始化与执行都受相应截止控制。
race 测试宿主（含进程内 CLI 与 LSP）的默认预算乘 20，以容纳插桩开销；显式毫秒级耗尽、循环、取消断言不变。
普通测试、CLI、发布二进制和 LSP 的真实宏宿主仍使用生产预算，外层 `-race` 不会自动传播到子进程。
生成代码的 `go test -race` 必须保留，因为它检测的是生成程序，而非编译器测试进程。
P5 的生成程序 build/run/race/vet 只在普通模式执行；项目准备、导入检查、锁与产物稳定性、
作者文件保护和失败原子性仍在普通与 race 两种模式执行。

可删的是重复执行同一断言的流程和无调用者的测试辅助函数；不能仅按文件主题删除测试。
例如语义诊断、生成代码运行、跨包/原生 Go 消费、CLI 交付分别验证不同边界；
同输入两次生成用于确定性校验，冷缓存、offline、locked 与发布归档实跑也各有独立作用。
只调用 `main`/`Exercise` 的合成测试若已有同一次生成的精确程序输出断言，则不再重复构建执行。
已摘除 CLI 入口的 workspace/import-go/vendor/scripts 不保留无条件 Skip 的旧流程占位，
未实现范围继续由[状态表](mvp-gap-audit.md)记录。已支持的 feature/lib/bin/example 使用当前
语法和布局恢复有效断言；环境或平台条件导致的 Skip 必须按未执行覆盖报告。

### 并发边界

- `-p` 调度构建命令和不同包的测试进程，默认值等于 `GOMAXPROCS`；`-parallel` 只影响主动调用 `t.Parallel()` 的用例。
  当前重型套件保持包内串行。外层 `-p=1` 不限制用例自行启动的 Go 子进程。
- formal helper 临时修改 `procmacro.CompilerSDKIdentity`；CLI 多处使用 `t.Setenv`/`t.Chdir`，
  一些子用例还共享宿主、计数器、锁文件或缓存测试夹具，不能直接加 `t.Parallel()`。
  不同测试进程隔离这些状态；标准 Go 构建缓存支持并发，本身不是必须串行的理由。
- 固定墙钟预算要求 CPU 余量。LSP 还验证 500ms、1 秒、5 秒级完成/取消行为，
  与编译和大量 Go 子进程争抢 CPU 会产生环境性失败，所以放到独立 runner。
- 网站构建清空并重写 exports/dist；同 checkout 的两个构建不能并行，HTTP embed 编译要等构建完成。
  Node 网站测试和扩展测试各有进程全局替换或 PATH 修改，文件内不盲目开启并发。

继续使用 Go 原生调度和 GitHub Actions matrix，不增加 make/CMake/xmake。
make 可以包装命令，但不会自动发现上述共享状态或协调嵌套 Go 子进程；增加一层调度不能替代隔离。
本地按改动运行对应包/用例；需要全树时使用 `go test -race -p=1 -vet=off -count=1 ./...`，
再补上述 native 差异检查，避免在同一机器同时运行多套重型门禁。

### 时间和结果

race 每组单包上限 120 分钟、步骤上限 150 分钟、作业上限 180 分钟，留出日志上传时间；native 每步单包上限 15 分钟、
作业上限 30 分钟。这些上限不改变宏的生产时限。
`ci_go_test.py` 启动时打印日志目录，逐包完成和失败即时输出，顶层用例超过 10 秒时打印耗时；
最终列出最慢的包与顶层用例、未完成项及 pass/fail/skip；没有任何顶层测试通过的空筛选或全跳过门禁直接失败。包、父用例和子用例的时间有包含或并行关系，
不相加冒充墙钟。公共端到端 helper 另记录项目准备、宏宿主累计时间及子进程时间；宏宿主时间包含
WASM 准备/编译/执行，不等于纯 guest 执行时间。

测试包装器将 `GOXIDE_WASM_TRACE_DIR` 设为本次原始日志目录，宿主关闭时写入独立 JSON 统计并由
包装器汇总：`calls` 为宏调用数，`memory_hits` 为宿主内已编译模块命中数；`module_loads` 为
`CompileModule` API 调用数，包含磁盘加载、完整编译和失败后的重试，不能当作后端冷编译次数。
`module_load_ns` 记录这些 API 调用的累计时间，`execute_ns` 记录实例化、初始化和宏执行的累计时间；
`disk_cache`、`cache_fallback` 记录磁盘缓存启用及回退状态。计数只覆盖已关闭且成功写出统计的宿主，
进程被强制终止时可能缺失。

完整 `go-test.jsonl` 与 stderr 逐行落盘。native、各 race 分组与 HTTP 通过 `always()` 上传
`go-test-*` 诊断产物，保留 7 天；取消或强制终止时上传仍是尽力尝试。
发布只选择 `goxide-*` 交付产物。CI summary 要求 native、整个 race 矩阵及其它必需作业成功，
失败、取消或应运行却跳过均不得报告通过。

## 浏览器验收

web 作业使用 runner 已安装的 Google Chrome，记录可执行路径和版本，先以
`chromiumSandbox: true` 启动浏览器和页面，确认实际 runner 支持 sandbox。
通过 `CHROMIUM` 将同一可执行文件传给所有浏览器脚本；不修改内核或关闭浏览器 sandbox。

构建前执行 `npm run test:proc-macro --prefix website`，核对真实 Go SDK 的 native/WASI 响应、
Worker 终止与恢复。构建和托管审计通过后，启动仅监听 loopback 的
`website/hosting/preview.py --port 8084 --headers`，等待清单可访问，再依次执行
`npm test --prefix website` 和 `website/hosting/qa-routes.mjs`。
两个页面套件覆盖真实 Worker/CLI 一致性、编辑与导航、诊断及宏恢复；路由套件检查资源和真实 404。
服务随步骤退出清理。截图、Worker 观察和预览日志通过 `always()` 上传，失败时仍保留诊断。

A03 宏/Eq 组合的来源丢失由 [PR #10](https://github.com/goxide-lang/goxide/pull/10)
修复，具体验收见 [issue #6](https://github.com/goxide-lang/goxide/issues/6)；这不代表全部
A03 lowering 路径完成。普通 CI 工作流只有 `contents: read`，checkout 不保留凭据，
不使用 `pull_request_target`、secrets 或发布服务。

## Tag Release

推送 `v` 前缀的 SemVer tag（如 `v0.0.1`）自动触发 `Release`；也可手动输入已存在的
tag 重试未完成的发布。工作流始终解析 `refs/tags/<tag>` 并固定对应提交，所有平台使用
准备阶段解析的同一 Go 补丁版本。`checks` 复用上述完整 CI 门禁和产物；发布直接下载同次运行的
CLI 与 VSIX，不重复构建。CLI 矩阵在六个原生 runner 上分别生成 macOS / Linux / Windows 的
amd64、arm64 包；插件只有一个通用 VSIX，不内置 `goxide`、`gopls` 或 `dlv`。

归档包含可执行文件、许可证与安装说明；版本来自 tag，可用 `goxide --version` 查看。
每个平台都解压归档后，真实执行快速开始多包项目的生成、运行和构建。Windows arm64 执行生成
模块的普通 Go 测试，并核对 `TestDescribe` 通过；其他平台执行 `goxide test` 的 race 测试。
CLI 要求本地 Go 版本与构建版本完全一致（包括补丁号）；`goxide test` 另需 C 编译器，且当前 Go
race detector 不支持 Windows arm64，因此该平台暂用生成后的 `go test`。

普通 CI 的 CLI 版本和文件名使用 `dev-<12位提交SHA>`；tag 构建使用完整 tag。VSIX 文件名也保留
完整 tag 或开发标识，内部版本采用数字 `major.minor.patch`，开发构建为 `0.0.0`；预发布包设置
VS Code 的 pre-release 标记。打包不修改 `package.json` 或锁文件。这些 VSIX 通过 GitHub
下载交付，多个 rc 共用数字内部版本；Marketplace 发布及其升级版本策略另行安排。

安装 VSIX 后，Goxide 从配置或 PATH 获取。默认 gopls 先查 PATH，缺少时由插件通过 `go install`
准备固定版本，保存在扩展宿主的私有目录，并显示进度与取消入口；显式配置的命令或路径按原值使用。

只有全部检查和平台任务成功后，发布任务才使用 `contents: write` 创建草稿、上传六个 CLI 归档、
一个 VSIX 与覆盖全部七项的 `checksums.txt`，核对远端资产集合及大小后公开 Release。
已公开的资产不覆盖；失败时可重试尚未公开的草稿。
`v0.*` 和带预发布标识的 tag 均标为 GitHub prerelease，且不设为 latest。
pre-1.0 是实验阶段，语言、CLI、配置及生成代码仍可在 patch / minor 版本中发生破坏性变更。

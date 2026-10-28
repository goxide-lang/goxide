# 最小 CI / PR 流程

工作在从最新 `main` 开出的短期分支，普通 push 后创建 Draft PR，正文关联 issue、
准确源码/验证 SHA 和已知缺口。CI 配置的变更独立提交，不混入已验收功能分支。
普通 CI 不自动合并、发布、关闭 issue 或修改分支保护；发布由下述语义版本 tag 工作流单独触发。

| 触发 / 层 | 检查范围 |
| --- | --- |
| 所有 PR、push main、手动触发 | 全树普通测试和 race、独立 vet、gofmt、Markdown 链接/锚点/围栏门禁、文档 Go 示例运行 |
| 所有 CI | 六个平台的 CLI 构建及解压运行验收，插件 Node 测试及通用 VSIX 打包；产物保留 7 天 |
| 网站及依赖相关变更 | 网站工具回归、离线 CLI 示例/导出数据/WASM 构建、带 `goxideweb` 标签的 HTTP race/vet |
| CI summary | 无路径过滤，始终汇总；失败、取消或应运行却跳过都不能报告成功 |

PR 在 `opened`、`synchronize`、`reopened`、`ready_for_review` 和 `converted_to_draft` 时运行；同一 PR
的新运行取消旧运行。普通 feature push 不另触发，避免与 PR 双跑。手动触发是完整门禁。
读取精确 head 对应 run 的状态/汇总，失败才读取对应 job 日志。最终 SHA 已绿后停止
重复本地或远端验证；本地仅做实现所需的定向检查。

网站选择包含 `website/`、`cmd/`、`internal/`、`go.mod/go.sum`、`examples/`、
`docs/rfcs/` 和 CI 本身。生成产物保持被忽略，不上传或部署。
网站使用 `python3 -B website/build.py --worktree` 构建当前 checkout；CLI 示例、
导出数据与 WASM 记录同一源码摘要，并在构建前后校验一致。网站 job 仅追加 HTTP 包的 race/vet。

准备任务从 `go.mod` 选择配套 Go，固定具体补丁版本和 checkout 提交，供全部检查和构建使用。
Python 3.12，Linux runner 提供 C 编译器；网站和插件各自使用 Node 22 与对应的 `package-lock.json` 执行 `npm ci`。
完整 Git 历史用于 catalog 快照。缓存按锁文件选取；先用仓外清单副本
预热完整固定模块图并 `verify`，逐字节确认 `go.mod/go.sum` 未变。网站 job 随后设置 `GOPROXY=off`；
原生检查保留 Go proxy 配置，供 P5 独立空模块缓存测试首次获取固定依赖，后续离线重跑由测试显式控制。
缓存仅加速，冷 cache、离线、校验和、权限/安全夹具仍由原测试自行构造，不改夹具环境。
全树测试移除外层 `GOWORK` 以保留 workspace 发现合同；其余脚本保留自身隔离规则。
原生 `GOMAXPROCS=4`；若增加 Node 执行 Go WASM 的入口，仅该进程设为 1。

普通全树测试验证过程宏宿主的生产预算：单次 5 秒、单次编译累计 60 秒，并运行仅普通模式的
完整 SDK 快照验收。普通与 race 任务均使用 `-p=1` 串行执行测试包，避免不同包争用 CPU
导致生产预算超时，包括 race 测试启动的普通 CLI 子进程。两项任务运行在独立 runner 上。
正式过程宏验收会构建并执行 WASI 产物；套件时限为完整验收、串行包执行和冷缓存预留时间。
普通任务的单包上限为 90 分钟、流程上限为 120 分钟；race 任务分别为 120 和 180 分钟。
这些时限只控制 CI 测试套件，不改变过程宏的单次或累计生产预算。
race 全树保留完整插桩；原生宏测试宿主将默认预算乘以 20，
容纳插桩开销。测试中显式设置的毫秒级超时、循环和取消断言保持原值；CLI 与发布产物始终
使用生产预算。race 检查验证并发与行为，生产时限由普通检查验证。

`ci_go_test.py` 分开汇报测试包/无测试包、顶层测试/嵌套子测试及 pass/fail/skip；
只记录实际墙钟，不把并行包的 `Elapsed` 相加。成功仅输出摘要，失败输出失败包记录；
完整 JSON 按行刷新并保留在 runner 临时目录，便于检查进行中的测试；不入 Git，
也不传入日常 agent 上下文。

## 浏览器 CI 跟踪缺口

现有 `website/redesign-qa.mjs`、`website/catalog-qa.mjs` 均显式设置 `chromiumSandbox: true`。
CI 尚未调用浏览器验收；接入时先在实际 runner 确认保留 sandbox 的 Chromium 可以启动，
再运行 HTTP/真实 Worker/UI 的应用验收。
不得修改内核或浏览器安全设置来使测试通过。这里的 WASM 构建和 HTTP 通过不代表
浏览器 QA 通过，也不替代该验收；汇总和 PR 持续显示此缺口。

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

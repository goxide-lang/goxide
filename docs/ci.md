# 最小 CI / PR 流程

工作在从最新 `main` 开出的短期分支，普通 push 后创建 Draft PR，正文关联 issue、
准确源码/验证 SHA 和已知缺口。CI 配置的变更独立提交，不混入已验收功能分支。
普通 CI 不自动合并、发布、关闭 issue 或修改分支保护；发布由下述语义版本 tag 工作流单独触发。

| 触发 / 层 | 检查范围 |
| --- | --- |
| 所有 PR、push main、手动触发 | 全树普通测试和 race、独立 vet、gofmt、Markdown 链接/锚点/围栏门禁、文档 Go 示例运行 |
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

Go 从 `go.mod` 选择配套 1.27.x；Python 3.12，Linux runner 提供 C 编译器；web job显式安装Node 22，并用website/package-lock.json执行npm ci，供Preact/Vite/Monaco构建与worker检查使用。
完整 Git 历史用于 catalog 快照。缓存按锁文件选取；先用仓外清单副本
预热完整固定模块图并 `verify`，逐字节确认 `go.mod/go.sum` 未变。网站 job 随后设置 `GOPROXY=off`；
原生检查保留 Go proxy 配置，供 P5 独立空模块缓存测试首次获取固定依赖，后续离线重跑由测试显式控制。
缓存仅加速，冷 cache、离线、校验和、权限/安全夹具仍由原测试自行构造，不改夹具环境。
全树测试移除外层 `GOWORK` 以保留 workspace 发现合同；其余脚本保留自身隔离规则。
原生 `GOMAXPROCS=4`；若增加 Node 执行 Go WASM 的入口，仅该进程设为 1。

普通全树测试验证过程宏宿主的生产预算：单次 5 秒、单次编译累计 60 秒，并运行仅普通模式的
完整 SDK 快照验收。普通与 race 任务均使用 `-p=1` 串行执行测试包，避免不同包争用 CPU
导致生产预算超时，包括 race 测试启动的普通 CLI 子进程。两项任务运行在独立 runner 上。
race 任务的流程上限为 60 分钟，单个测试包的上限仍为 30 分钟。
race 全树保留完整插桩；两个原生宏测试宿主将默认预算乘以 20，
容纳插桩开销。测试中显式设置的毫秒级超时、循环和取消断言保持原值；CLI 与发布产物始终
使用生产预算。race 检查验证并发与行为，生产时限由普通检查验证。

`ci_go_test.py` 分开汇报测试包/无测试包、顶层测试/嵌套子测试及 pass/fail/skip；
只记录实际墙钟，不把并行包的 `Elapsed` 相加。成功仅输出摘要，失败输出失败包记录；
完整 JSON 保留在 runner 临时目录，不入 Git，也不传入日常 agent 上下文。

## 浏览器 CI 跟踪缺口

现有 `website/qa.mjs`、`website/catalog-qa.mjs` 均显式设置 `chromiumSandbox: true`。
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
准备阶段解析的同一 Go 补丁版本。`checks` 复用上述完整 CI 门禁，构建任务在五个原生 runner
上分别生成 macOS / Linux amd64、arm64，以及 Windows amd64 的 CLI 包。

归档包含可执行文件、许可证与安装说明；版本来自 tag，可用 `goxide --version` 查看。
每个平台都解压归档后，真实执行快速开始多包项目的生成、运行、构建和 race 测试。
CLI 要求本地 Go 版本与构建版本完全一致（包括补丁号）；`test` 另需 C 编译器。
Windows arm64 暂未纳入发布矩阵，因为当前 Go race detector 不支持该目标。

只有全部检查和平台任务成功后，发布任务才使用 `contents: write` 创建草稿、上传完整归档
与 `checksums.txt`，最后公开 Release。已公开的资产不覆盖；失败时可重试尚未公开的草稿。
`v0.*` 和带预发布标识的 tag 均标为 GitHub prerelease，且不设为 latest。
pre-1.0 是实验阶段，语言、CLI、配置及生成代码仍可在 patch / minor 版本中发生破坏性变更。

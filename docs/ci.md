# 最小 CI / PR 流程

工作在从最新 `main` 开出的短期分支，普通 push 后创建 Draft PR，正文关联 issue、
准确源码/验证 SHA 和已知缺口。CI 配置的变更独立提交，不混入已验收功能分支。
本流程不自动合并、发布、关闭 issue 或修改分支保护。

| 触发 / 层 | 检查范围 |
| --- | --- |
| Draft PR | 文档与 review 产物合同；源码相关变更跑指定编译器、CLI、AST/验证器 smoke |
| Ready PR、push main、手动触发 | 一次完整全树 race、独立 vet、完整设计门禁；不再先跑 Draft smoke/文档门禁 |
| 网站及依赖相关变更 | 网站工具回归、离线 CLI 示例/导出数据/WASM 构建、带 `goxideweb` 标签的 HTTP race/vet |
| CI summary | 无路径过滤，始终汇总；失败、取消或应运行却跳过都不能报告成功 |

PR 在 `opened`、`synchronize`、`reopened`、`ready_for_review` 和
`converted_to_draft` 时运行；同一 PR 的新运行取消旧运行。普通 feature push 不另触发，
避免与 PR 双跑。手动触发是完整门禁；首次 bootstrap 时可能要工作流先进入默认分支，
本次不以自动合并绕过。新 CI Draft PR 的 PR 事件可先验证 Draft 与网站层；完整原生门禁
仍需 ready/main/manual 事件。读取精确 head 对应 run 的状态/汇总，失败才读取对应 job 日志。
最终 SHA 已绿后停止重复本地或远端验证；本地仅做实现所需的定向检查。

网站选择包含 `website/`、`cmd/`、`internal/`、`runtime/`、`formatting/`、
`go.mod/go.sum`、`examples/`、`docs/rfcs/` 和 CI 本身。不只看页面目录。
Draft 网站构建只在相关变化时运行，不执行全树原生测试。网站 job 仅追加 HTTP 包的
race/vet，不重复全树已经覆盖的 `internal/typecheck`。生成产物保持被忽略，不上传或部署。
编译器固定对象/生产源码摘要不匹配必须失败，由功能改动同步审阅对应 pin，不能在 CI
自动重写摘要使之通过。

Go 从 `go.mod` 选择配套 1.27.x；Python 3.12，Linux runner 提供 C 编译器和 Node >=18。
完整 Git 历史用于 catalog 与 review 产物合同。缓存按锁文件选取；先用仓外清单副本
预热完整固定模块图并 `verify`，逐字节确认 `go.mod/go.sum` 未变，然后 `GOPROXY=off`。
缓存仅加速，冷 cache、离线、校验和、权限/安全夹具仍由原测试自行构造，不改夹具环境。
全树测试移除外层 `GOWORK` 以保留 workspace 发现合同；其余脚本保留自身隔离规则。
原生 `GOMAXPROCS=4`；若增加 Node 执行 Go WASM 的入口，仅该进程设为 1。

`ci_go_test.py` 分开汇报测试包/无测试包、顶层测试/嵌套子测试及 pass/fail/skip；
只记录实际墙钟，不把并行包的 `Elapsed` 相加。成功仅输出摘要，失败输出失败包记录；
完整 JSON 保留在 runner 临时目录，不入 Git，也不传入日常 agent 上下文。

## 浏览器 CI 跟踪缺口

现有 `website/qa.mjs`、`website/catalog-qa.mjs` 使用 `--no-sandbox`，本 CI 不调用。
后续浏览器接入必须去掉该参数，显式 `chromiumSandbox: true`，先在实际 runner 证明
保留 sandbox 的 Chromium 可以启动，再运行 HTTP/真实 Worker/UI 的应用验收。
不得修改内核或浏览器安全设置来使测试通过。这里的 WASM 构建和 HTTP 通过不代表
浏览器 QA 通过，也不替代该验收；汇总和 PR 持续显示此缺口。

A03 既有诊断来源缺陷继续由 [issue #6](https://github.com/goxide-lang/goxide/issues/6)
跟踪，不因 CI 接入变成已修复。新增工作流只有 `contents: read`，checkout 不保留凭据，
不使用 `pull_request_target`、secrets 或发布服务。

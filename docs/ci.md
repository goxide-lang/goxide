# 最小 CI / PR 流程

工作在从最新 `main` 开出的短期分支，普通 push 后创建 Draft PR，正文关联 issue、
准确源码/验证 SHA 和已知缺口。CI 配置的变更独立提交，不混入已验收功能分支。
本流程不自动合并、关闭 issue 或修改分支保护；已合并 main 通过全部必要门禁后，
才尝试把同一 run 的网站产物发布到既有 Pages 分支。

| 触发 / 层 | 检查范围 |
| --- | --- |
| Draft PR | 文档与 review 产物合同；源码相关变更跑指定编译器、CLI、AST/验证器 smoke |
| Ready PR、push main、手动触发 | 一次完整全树 race、独立 vet、完整设计门禁；不再先跑 Draft smoke/文档门禁 |
| 网站及依赖相关变更；每次 push main | 网站工具回归、离线 CLI 示例/导出数据/WASM 构建、HTTP race/vet、保留 sandbox 的真实浏览器 UI/Worker 验收 |
| CI summary | 无路径过滤，始终汇总；失败、取消或应运行却跳过都不能报告成功 |
| main 网站发布 | summary 与 web 都成功后复用同一 run 的产物，普通 push 到既有发布分支；核对线上资产与真实 Worker |

PR 在 `opened`、`synchronize`、`reopened`、`ready_for_review` 和
`converted_to_draft` 时运行；同一 PR 的新运行取消旧运行。普通 feature push 不另触发，
避免与 PR 双跑。手动触发是完整门禁；首次 bootstrap 时可能要工作流先进入默认分支，
本次不以自动合并绕过。新 CI Draft PR 的 PR 事件可先验证 Draft 与网站层；完整原生门禁
仍需 ready/main/manual 事件。读取精确 head 对应 run 的状态/汇总，失败才读取对应 job 日志。
最终 SHA 已绿后停止重复本地或远端验证；本地仅做实现所需的定向检查。

网站选择包含 `website/`、`cmd/`、`internal/`、`runtime/`、`formatting/`、
`go.mod/go.sum`、`examples/`、`docs/rfcs/` 和 CI 本身。不只看页面目录。
Draft 网站构建只在相关变化时运行，不执行全树原生测试。网站 job 仅追加 HTTP 包的
race/vet，不重复全树已经覆盖的 `internal/typecheck`。生成产物保持被忽略；main 的产物
通过网站门禁后上传到当前 run，PR 不上传发布产物、不取得发布凭据。
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

## 浏览器与发布门禁

`scripts/ci_website_browser.py acceptance` 在 loopback 服务已构建的 dist，复用
`website/validation/package-files/run.py`。它验证全部 11 个旧例子的真实 Worker 输出，
同包 LF/CRLF、错误恢复、文件增删/切换、输出 helper、剪贴板、390px UI、陈旧 Worker
消息以及实际 Go/std/serde 身份。CLI 和 dist 都来自前面的唯一一次构建，不重跑全树。
Python Playwright 与传递依赖使用 Linux x86_64 / Python 3.12 固定版本及 SHA-256，
浏览器使用 ubuntu-24.04 runner 自带的 `google-chrome`，显式 `chromium_sandbox=True`。
浏览器缺失或 sandbox 拒绝即失败；不安装备用浏览器、不修改系统安全设置、不降级。
历史 `website/qa.mjs`、`website/catalog-qa.mjs` 的无 sandbox 入口不用于 CI。

发布只由本仓 `push main` 触发，`needs: [summary, web]` 阻止失败、取消或跳过门禁的
run 发布。workflow_dispatch 与 PR 只验证，不发布；不能拿另一 run 的 artifact 补齐。
`website/dist/` 先经过现有公开资产审计，再上传为 `goxide-site-<完整SHA>`，保留 7 天，
保留 `.nojekyll`。发布 job 只下载当前 run 的同名产物，核对 website SHA、reviewed
compiler pin/digest、WASM hash；不构建、不重跑原生测试、不自动重写 pin。

发布 job 使用固定 concurrency group，禁止取消正在发布的 job。GitHub 不保证待运行
队列顺序，因此脚本在准备前和 push 前均读取实时 main；不再是最新 main 的 run 显式
记为 superseded。已有部署的 website SHA 必须是待发布 SHA 的祖先，防止旧 run 或
非主线历史倒退。推送只更新 `goxide-lang/goxide-lang.github.io` 的现有
`deploy/goxide-site-ae61ec6` 分支根目录，保留提交历史，以普通 fast-forward push
拒绝并发写入，不自动 force/rebase/retry。两个仓库不能跨仓原子锁定 main：若 main
恰在最终检查后前进，本次可能短暂发布刚验收的前一版本，但不会覆盖已发布的新版本。

只复制 11 个允许的静态资产，README 从本次真实 manifest 生成。未知根文件失败并
要求审阅，不删除未知内容。Pages 配置在写入前只读确认仍为 legacy branch、准确分支、
根目录与正式 URL；配置改变或读取 403 立即失败，不切换 source、不改保护规则。
推送成功仅表示分支已更新。随后最多等待 10 分钟，核对正式 URL 的完整 manifest、
所有公开资产字节、页面加载及真实单/多文件 Worker 输出；保留 Chromium sandbox。
失败需查看目标仓对应 commit 的 Pages 部署状态，不自动回滚或重复发布旧资产。

## 现有授权的配置边界

工作流 `GITHUB_TOKEN` 始终只有 `contents: read`，checkout 不持久保存凭据，不使用
`pull_request_target`。它不能跨仓写入，而且其 push 不会触发 branch Pages 构建，见
[GitHub token 范围](https://docs.github.com/en/actions/concepts/security/github_token)与
[Pages 发布源规则](https://docs.github.com/en/pages/getting-started-with-github-pages/configuring-a-publishing-source-for-your-github-pages-site)。

维护者仅在确认**已有**合适授权时，把 repository variable `GOXIDE_SITE_PUBLISH_SECRET`
设为该现有 Actions secret 的名字；工作流通过 `secrets[vars.GOXIDE_SITE_PUBLISH_SECRET]`
读取。该凭据须能对 `goxide-lang/goxide-lang.github.io` 执行 **Contents: write** 与
**Pages: read**（后者仅核对 source），且其 push 能触发现有 Pages 部署。没有假定任何
secret 已存在；当前集成读取 Actions secrets 被 403 拒绝，未枚举或创建凭据。变量为空、
secret 不存在或 API/Git 拒绝均 fail closed；只有 main 发布 job 报明确阻塞，PR 不受影响。
脚本用临时 askpass 从环境读取凭据，不写进 Git URL/config、资产或日志。

最小后续操作是确认现有授权 secret 的可用性并设置上述变量，然后合入经审阅的配置
PR；首次 main run 会重新完成应有 CI。若只重试发布失败的 job，会复用同一 run 的现有
资产，不重复完整测试；资产过期则不可跳过必要门禁。若不存在可复用的授权凭据，
当前授权内自动跨仓发布仍被阻塞，不能靠新建 token 或扩大权限解决。改为同仓 Pages
artifact 需要改变当前 Pages source/站点归属，属于另一个需明确授权的方案，本 PR
不迁移。该 Draft PR 自身不能证明新的 Actions 浏览器门禁或自动发布已经上线。

A03 宏/Eq 组合的来源丢失由 [PR #10](https://github.com/goxide-lang/goxide/pull/10)
修复，具体验收见 [issue #6](https://github.com/goxide-lang/goxide/issues/6)；这不代表全部
A03 lowering 路径完成。

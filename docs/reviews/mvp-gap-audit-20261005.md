# MVP 差距审计验证（2026-10-05）

审计结论及 Issue 候选见 [台账](../mvp-gap-audit.md)。基线为 `5d777820c5a617db57bcedb50b4d31ad6f2d3f15`，网站源码为 `6dc779a651af3eb34121f53637a55bfb6feee387`。只修改文档及审计证据，不改生产、测试、网站构建或依赖代码。

## 版本与范围

实际 fetch 核对编译器 `feat/vendor-local-modules`、std `feat/goxide-naming`、serde `migration/core-json`、网站源码 `feat/site-source-selection-5d77782`；std/serde SHA 与根 go.mod 的公开 pseudo-version 一致。额外 fetch 0024 和 mod 研究分支，避免漏掉主目录未合入的设计。

源码核对覆盖根编号 RFC（26篇）、分支 RFC0024、11篇本地 research 和分支 mod 研究。`TEMPLATE.md` 只作格式说明。设计示例、手写 Go 机制、真实前端负例、生成 Go 执行与历史报告分开使用。没有将全 RFC 文本都当作已批准实现范围，也没有把“本片未做”当作获批延期。

网站线上公开清单的只读观察摘要存于 [identity JSON](mvp-gap-audit-evidence/live-identity.json)。线上 catalog 有26个能力卡片，不是RFC完成率。本轮未执行浏览器 UI/WASM 差分；历史浏览器证据指向网站固定提交的 source-selection 验收。未部署或修改静态仓库。

## 本轮实际重跑

环境：Linux amd64，真实 Go 1.27.1，CGO 可用；固定 `GOTOOLCHAIN=local`，可写且隔离的 Go module/build cache，`GOMAXPROCS=4`。先用公开根 go.mod `go mod download` 预热；没有把 std/serde 换成本地 replace。命令在编译器根运行，库测试分别在固定 std/serde checkout 运行。

| 检查 | 命令 / 结果 |
| --- | --- |
| 实现层专项 | `go test -race -count=1 ./internal/macros ./internal/frontend ./internal/project ./internal/propagate`：通过，4.237s / 74.537s / 1.693s / 7.039s；[日志](mvp-gap-audit-evidence/targeted-race.log) |
| 接受测试专项 | `go test -race -count=1 ./tests/acceptance -run 'Test(MacroBuiltin\|MacroAndCloneExplicit\|MatchExpressionUnsupported\|MatchCallSignatures\|MatchNamedDynamic\|GoResultInference\|ExternalSerde\|SerdeBlackBox\|SumCloneDebug)'`：通过，80.643s；表内反斜线只为 Markdown 表格转义，实际正则为普通 `|`；[日志](mvp-gap-audit-evidence/acceptance-race.log) |
| std | `go test -race ./...` 与 `go vet ./...`：退出0；sum 1.053s |
| serde | `go test -race ./...` 与 `go vet ./...`：退出0；core 1.016s，json 1.028s |
| 文档检查 | `python3 scripts/check_design.py --docs-only`：通过；115 Markdown、26 RFC 结构，全部本地链接/锚点和围栏通过（最终日志见下） |

CLI 专项命令：

```sh
go test -race -count=1 ./cmd/goxide -run 'Test(ProjectTargetsAndFeatures|ProjectLocalFeatureUnion|ProjectGitRevisionLock|ProjectConditionalGoxideAcrossTargets|WorkspaceUnifiedVersionsSeparateFeatures|WorkspaceCopiedGitLockIgnoresUnreachable|NativeWorkspaceProgressiveFeatures|NativeWorkspaceProgressiveScriptPermissions|NativeWorkspaceCopiedLockStandalone|ImportSourceTerminalProcess|ImportSourcesVerifiedAndOffline|BuildScriptGenerationAndIncrementalCLI)$'
```

首次该组退出1（193.083s），唯一报告失败为 `TestProjectConditionalGoxideAcrossTargets`：离线 Go module planning 的 `module lookup disabled by GOPROXY=off`。保留[初次失败日志](mvp-gap-audit-evidence/cli-race-initial.log)。初始 `go mod download` 没有预热整个依赖图；随后仅补 `go mod download all`，保持源码及测试不变，单独以 `go test -race -count=1 ./cmd/goxide -run '^TestProjectConditionalGoxideAcrossTargets$'` 重跑：退出0，20.534s，见[重跑日志](mvp-gap-audit-evidence/cli-cache-rerun.log)。这支持该次失败源于离线依赖缓存未完整预热；没有修改生产代码、放宽offline或跳过该场景。不能把首次组执行写成全绿。测试调用本仓库自编 fixture，不执行用户或第三方未授权脚本。接受测试自身包含生成模块的 race/vet；并非对每个未来 RFC 示例都执行。未重跑整个编译器全树，不把 source-selection 的历史22包/516顶层/1086子测试通过当作本次结果。

最终文档检查原始输出见[docs.log](mvp-gap-audit-evidence/docs.log)，`git diff --check` 通过。

## 已修正文档检查问题

初次文档检查发现审计稿中一个私有目标测试文件名误写、以及验证报告尚未创建。测试链接已改为实际 `propagate_private_place_test.go`，报告随后建立；没有改动生产代码或删掉失败用例。

特别复核多结果 match：既有负例的函数返回两个值、单个 arm 只返回一个值，不能直接作为合法多结果需求的复现；台账要求先区分多 RHS 表达式位置与新增多值 arm 语法，保留 Go 原生 arity 错误。其他正确限制如 typed-nil Err、readonly 非全局不可变、offline 需要缓存、浏览器不执行脚本也不列作待修缺陷。

## 交付边界

仅 `docs/mvp-gap-audit` 普通 commit/push，并核对远端 SHA；不合 main、不移动标签、不建 release、不创建 Issues/Project。台账保留仓库已有设计与证据，不发布私人对话、消息标识或完整内部源码到网站。剩余 blocker 是各 D 项的具体设计/首版范围决定，不是对本次文档提交的重新许可。

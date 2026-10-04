# Workspace RFC 0026 研究验收（2026-10-04）

状态：研究完成，设计待评审；workspace 生产功能未实现、未批准，未部署网站。基于 `d51117d36fa2d1ccfcf322b81195b0d753cbb8de`，文档分支 `docs/workspace-rfc`。正式合同候选见 [RFC 0026](../rfcs/0026-workspaces.md)。

## 资料与环境

已查阅官方 Cargo Book workspaces/resolver/features/specifying-dependencies，Go modules/toolchains/GODEBUG/cmd/go，链接与事实/提案区分见 RFC。Cargo 为 2026-10-04 滚动文档，无本地 cargo/rustc，未声称 Rust 实测。真实 Go 为 `/workspace/goxide-environment/go/bin/go`：`go version go1.27.1 linux/amd64`。

## 已通过机制试验

运行 [研究脚本](../../scripts/research/workspace_go_probe.py)，全部 12 项通过；[完整结构化结果](evidence/workspace-go-20261004.json)已保存，fixture 路径归一为 `<fixture>`。每次新建临时目录/空GOMODCACHE，只用本地replace；网络解析禁用，GOTOOLCHAIN=local，无Go下载/私有来源/脚本安装。研究用 `go work vendor` 只制造fixture，不提出在迁入中重建vendor。

| 证据 | 结果 |
| --- | --- |
| GO-01 / GO-02 / GO-03 | A独立输出one；A/B共同主模块时即使只run A也输出two，选择D v1.1.0 |
| GO-04 | 生成仅A的work视图恢复one，证明选择解析范围是可观察差异 |
| GO-05 / GO-06 | 两成员replace冲突，work根显式override后输出two |
| GO-07 / GO-08 | modules.txt含workspace标记；移走两个replace目录后，work vendor冷modulecache仍输出two |
| GO-09 / GO-10 | GOWORK=off不能消费workspace vendor；删vendor实际import包明确失败，不联网 |
| GO-11 | Go edit JSON保留Go/Toolchain/Godebug；此项只验证解析，不证明hgo映射或运行时GODEBUG等效 |
| GO-12 | 原生全workspace含未来go1.99成员时，list A也拒绝，未选择编译不等于未参与Go政策 |

## 文档与实现边界

变更限于 RFC、索引、ROADMAP、研究脚本/结果。检查生产源文件diff为空；不新增接受workspace的parser或CLI路径。RFC六章节、链接与代码围栏需通过 `python3 scripts/check_design.py --docs-only`。W01–W16是未来实现矩阵，不是已通过测试；无hgo workspace feature/cache/lock/trust/publish实测。本轮不重复生产全量race/vet，基线已有覆盖不算本RFC功能验收。

## 独立评审

待独立 reviewer 对设计矛盾、Cargo/Go区别、迁入无损性及锁/脚本边界做对抗检查；完成后记录实际结论。

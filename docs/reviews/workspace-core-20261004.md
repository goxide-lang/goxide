# Workspace 作者核心验收（2026-10-04）

实现分支 `feat/workspace-core`，基线 `09f1eb258fc98217e5d03506f34957c9ee9aec4f`，已整合 RFC 决策 `594d3a3564b48f6b39420d68dab971ad993ffce2`。范围为 [RFC0026 实现子集](../rfcs/0026-workspaces.md#本次实现范围)，不是所有候选协议。未合并主线、发布站点或冻结 tag。

## 实际交付

`internal/project/workspace.go` 处理成员、边界、严格 TOML 和显式模板继承；`workspace_graph.go` 负责全体成员版本解析与选中图 feature 固定点；现有 Go planner 负责 MVS/校验和，schema 3 仅包装来源、全局版本与政策。`cmd/hgo/workspace.go` 提供 `-p/--package` 和 `--workspace`，沿用既有命名 bin/example、生成 Go、脚本 trust 与 metadata 机制。没有修改宏前端、Result 或 enum/所有权语义。

全局求解的内部合成根只用于版本约束，从不作为编译/import 来源。明确拒绝把成员 path 隐式覆盖同 module 的原生 require/version 来源。增强 TOML 依赖仍须显式 `[dependencies]` 才进入增强编译；原生 require/replace 不是自动启用 hgo feature 的入口。

根锁写入由 workspace lease 串行保护，成员锁拒绝。复制根锁的独立构建不加载旧 workspace root policy；可达 Git pin 即使普通构建也必须完整、一致。已锁间接版本预处理只沿真实清单与 Go 校验的 `.mod`，不信任锁中的自报依赖关系。输出 owner 使用 workspace 根，module/target/config 仍进入 unit 身份；切回单独构建不会复用批量 feature 结果。

## 测试与证据

工具：真实 `/workspace/goxide-environment/go/bin/go`，`go1.27.1 linux/amd64`，`GOTOOLCHAIN=local`、`GOWORK=off`。依赖 fixture 使用本地临时 Git、file proxy 和空临时 modulecache；没有私人第三方或安装脚本。全量回归所需的既有 go.mod 公开依赖已从官方 Go proxy 预热，回归运行时 `GOPROXY=off`。

| 验证 | 结果/边界 |
| --- | --- |
| 显式继承与不隐式继承 | `TestWorkspaceExplicitInheritance` / `NoImplicitInheritance`；根相对 path、features 加法、default_features 保留 |
| 严格发现 | 大小写错字、features 错类型、重复 module、nested 祖先、symlink、非成员 INPUT、成员锁明确拒绝 |
| 全局版本与选择 feature | A 要 D1.0/B 要 D1.1，单独 A 得 D1.1 + json；单独 B 为 yaml；批量 feature 并集，随后单独 A 恢复 json |
| 复制 Git 锁 | branch 移动后仍重建旧 commit；无关 repo 不可用且未选脚本是非法 Go，选中构建仍成功；缺 pin/缺 module 拒绝；政策刷新也不得补解析已锁丢失 pin |
| Native 来源边界 | member 与原生 require 冲突拒绝；显式 replace 加增强依赖可成功；import 不补成员边 |
| 已锁缓存离线 | 直接和间接依赖的未锁低版本缓存删除后，`--offline --locked` 成功 |
| 旧 Go 库 | zip 不含 go.mod，使用 proxy synthetic `.mod` 成功，离线重复成功 |
| 对抗锁/元数据 | 原始 JSON 注入虚构依赖关系不能授权 import；真实缓存 `.mod` 加边被校验拒绝 |
| 根包 workspace | 根同时是成员时受管 target 可重复构建 |
| 命令实例 | [运行记录](evidence/workspace-core-example-20261004.json)：双 web + admin、显式 example、批量 build/locked repeat/test、单独 feature 恢复均成功 |

主要持久测试在 `cmd/hgo/workspace_test.go` 与 `internal/project/workspace_test.go`；沿用 CLI POSIX、宏 cfg、target/cache/test 模式、路径/Git/version 及脚本回归。

独立 `/root/review` 对抗 review 先发现并复核修复：复制锁误读旧 replace、根包 output 归属、严格键/类型、直接低版本缓存、legacy synthetic `.mod`、缺 Git pin，以及一度用于预选版的未认证 `native_requires` 索引。最后一项已从 schema 删除，改为读取 Go 校验后的真实 `.mod`；独立 race 红测全部转绿，核心测试已持久化。独立 reviewer 没有修改生产代码。

全量 `go test -race ./... -count=1` 全部通过（CLI 388.319s、acceptance 337.111s；[完整结果](evidence/workspace-core-race-20261004.txt)）。首次离线全量运行因既有公开 CLI 依赖的传递缓存不完整失败，按现有 go.mod 从官方 proxy 执行 `go mod download all` 后重跑通过；没有修改依赖版本或使用未知源。最终 Git pin/CLI 增量 race 也通过（CLI 39.976s、project 1.028s）；workspace 成员共享 lease 的独立 race 通过。owner 使用不能与合法 Go module 重名的 `<workspace>`，并有普通 module 名 `workspace` 的碰撞回归。

`go vet ./...` 通过；设计检查通过：100 Markdown、1030 本地链接/锚点、25 RFC 结构、13 条真实 Go 命令（含预期失败）。原生 Go 研究的 19 项实验另见 [历史研究](workspace-rfc-20261004.md)，不混作 hgo workspace 端到端覆盖。所有最终生产增量的 CLI/workspace/layout/scripts race 子集通过（CLI 244.395s、project 1.041s），覆盖最终 owner 标识变更。

## 明确限制

- 暂不提供 go.work/workspace vendor 迁入、workspace.go/override、发布/registry、dev/profile/target-specific features、高级 CLI 范围选择。
- host helper 保持 RFC0023 显式本地 Go 快照/独立 trust；本片没有跨脚本统一 host 版本/feature resolver，不支持 helper 的 workspace 继承。normal/test/host 缓存和授权不混用。
- 复制锁不打包 path 源码；继承字段与本地路径需要显式物化。整棵 workspace 移动时本地绝对来源身份需重锁。
- 没有细粒度 lock update；成员/根政策变化可在普通构建刷新并保留 Git pin，非成员 path 政策改变需明确重建锁；移走旧锁重建可能解析新的 Git branch commit，必须审查差异。`--locked` 不做这些更新。
- 批量逐成员构建，不是整个 workspace 的事务性发布；后序编译或 trust 失败可能发生在前序成员已经构建之后。没有新增 OS 脚本沙箱。

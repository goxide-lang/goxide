# 供应模块来源选择验收

分支 `feat/vendor-local-modules`；本地核心基线 `f1474551cb7f18c5610880b91b54e7367a2a6a2e`。先提交合同 `062c59d`，来源选择实现 `3a8f204a4eacc62ba11ac15fce7a29c91f0bd5d7`。本次接续上一阶段，不重新引入 vendor 运行模式。

## 实现与边界

- stdin/stderr 都是真实终端且未给 source 参数时，一次展示全部供应模块及 direct/indirect、local 路径、remote 实际版本和 unknown 状态；空行全部 local，单行模块覆盖，cancel/EOF/缺换行取消。非 TTY 默认 local，不消费管道输入。重复 `--source MODULE=local|remote` 提供确定性选择。
- remote 只支持原清单证实的精确 Go module path/version，包括版本化 replacement；local replacement 的远端不可确认，明确拒绝。没有 Git 推断、任意 URL、新版本搜索或 registry；没有从 go.sum 猜选版。
- Go 下载使用隔离目录并固定本地工具链。对模块 zip 重算 h1，与 Go 返回的身份/校验和核对，再比较全部供应文件；不信任可编辑的展开缓存。verified subset 仅证明供应文件集合相同，不证明完整发行物等价；无法映射的祖先许可或无可比较文件为 unknown。权限差异另列，原权限仍保留。
- 差异逐文件列出，终端要求完整 `remote MODULE` 确认；非终端要求对应显式 remote 的 `--accept-source-diff MODULE`。未知/重复模块、未知来源、确认 local 等拒绝。所有原供应文件仍物化并记审计，即使运行来源改为 remote。
- remote 使用普通 Go replacement/锁，保留逻辑 module/import identity 与直接/间接关系；上游完整依赖图由正常 Go 求解与构建验证。支持同一工程 remote D + 修改后的 local E；不伪造裁剪快照的原始完整图。原工程不变，失败不发布目标。
- `--offline` 禁止工具控制的 proxy/sumdb/Git/工具链下载；暖缓存可验证 remote，冷缓存明确失败。plan 不读取终端、不执行 remote 比对；常规 build 不询问。不声称隔离任意用户脚本的网络。

新增 `golang.org/x/term v0.30.0` 与间接 `x/sys v0.31.0` 仅用于可靠跨平台 TTY 检测。std/serde 的既定公开版本及 API 未变。云端 Linux 额外交叉构建 Windows amd64、Darwin arm64 CLI 均通过；这是编译检查，未在 Windows/macOS 实机运行。

## 实际验证

专项 race：`go test -race ./cmd/goxide ./internal/project -run 'TestImportSource|TestSupplied|TestVendor' -count=1` 通过，CLI 93.282 秒，project 1.183 秒。之后补充混合来源搬移和离线交互缺缓存/取消分支，普通来源测试与混合测试通过；最终全树回归包含这些新增用例。

真实 PTY 子进程覆盖默认 local、cancel、EOF、拒绝/确认远端差异和不询问的 plan；真实非 TTY 子进程覆盖忽略管道选择输入、缺参数与 `-source` 拒绝。服务用例补充缺确认行、重复/未知选择、无效确认。zip 与展开缓存篡改均拒绝发布。单模块、版本化 fork、workspace remote、原字节保留及搬移后混合来源 `--offline --locked` 均通过。HTTP 计数 stub 覆盖冷/暖缓存、plan、非 TTY local/remote、交互 remote 错误与取消，记录零请求。

首次 PTY 矩阵发现 `-source` 已被拒绝但缺少规范 `--source` 迁移提示，已加入长选项诊断表后重新验证；没有通过放松选项语法绕过测试。

独立 reviewer 只读检查并运行全部来源/PTY/缓存完整性相关测试，8.864 秒通过，没有阻断项。随后对最终提交 `3a8f204` 再核查零比较文件 unknown 分支，并独立运行混合来源搬移用例（1.945 秒）通过。其文案建议已修正：默认 local 不 download，显式 remote 可按确认身份下载；两者均受 offline 约束。

vet、13 条设计检查（含预期失败）和 web tag 包 race 通过。网站生产源绑定 `3a8f204`，digest `27323c6bf7a3066bab1d6546355e681e2b2d548e10c07389ffe2793f6541aa42`；重建 WASM 16,138,905 bytes，sha256 `a25799bf67a5ed2e9e8ebff195cbf9b18edcb8fc93741830429687867c060772`。只在云端 loopback 验证，无网站部署、main 合并、tag/release、仓库权限更改或 Mac 操作。

## 最终证据

浏览器 10 个示例与 CLI 逐字一致、9 项 Go-result 检查、26 张目录卡片/10 个拒绝例、工程快照、移动端/剪贴板、同源请求、损坏及陈旧 WASM 拒绝均通过。见 [浏览器对照](source-selection-evidence/source-selection-wasm-qa.log) 与 [目录 QA](source-selection-evidence/source-selection-catalog-qa.log)。最终实现提交的完整 `go test -race -timeout 30m -json ./...` 退出 0：22 包（含无测试包），516 个顶层测试、1086 个子测试，无失败；CLI 103 项、acceptance 148 项。详见 [全树汇总](source-selection-evidence/full-race-summary.json)，包含各包耗时和原始日志 SHA256。原始阶段日志见 [专项 race](source-selection-evidence/source-targeted-race.log)、[设计检查](source-selection-evidence/source-selection-design.log)、[网站构建](source-selection-evidence/source-selection-website-build.log)。网站 owner 接入范围见 [接入说明](../../website/LATEST-INTEGRATION.md)。

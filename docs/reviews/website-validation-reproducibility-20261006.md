# 网站验证仓内复现交付（2026-10-06）

本批关闭已报告的网站构建/快照/输出 AST 对照依赖任务目录 overlay 的交付欠账。
在独立、干净、没有共享 Git object alternates 的单分支 clone 所建 worktree，
实际运行仓内入口：七阶段网站验证、完整 Mac race、全树 vet 和完整设计检查均通过。
原有 **97 个 fixture、10 个示例、qa.mjs 与 package-lock.json** 保持原名和原字节。
A04 unary/composite match 留在下一批；本批没有改编译器生产源码或公开库 ABI。

## 准确身份与复现范围

| 身份 | 准确值及含义 |
| --- | --- |
| 原交付基线 | `cbd872470d72ca7c3d6ac08acbd89f0799b6c8cc` |
| 实施前合同 | `d7d585031b4573fe04d4586d1e593c9a26873d43`；[合同](website-reproducibility-contract-20261006.md)先于工具实现提交 |
| 被验证编译器生产源码 | `e36d23dacee64d19b34aac639900c3f5c29e55c2` |
| compiler inventory digest | `41f51dffc2aa68535a7617824231065d268cab190adeb7e164e239e3b7022848` |
| 正式验证的网站/工具提交 | `c93461c27e67fce30fa6ace726d0c85c54fc4700`；tree `a901c7c42fcc34865f76cd85711f2cb4ba38b773` |
| 旧编译器对照 | `fd8380c39876c96441857a737be8e32c322a0467` |
| 交付分支 | `fix/website-validation-reproducibility` |

本报告及证据、验证 README 的环境说明在上述准确工具提交之后追加。
这是文档/证据提交，不冒称对其新提交身份重跑了整套构建；已执行的源码与工具明确绑定
`c93461c`。catalog 的 compiler pin 不跟着报告或网站工具提交滚动，避免把包含自身的
新 hash 写回自身。构建 manifest 同时记录 compiler commit、digest 与 website commit。

原编译器工作区仍在 `fix/generated-ast-provenance` 的 `cbd8724`，本片使用独立工作树。
std、serde、next 和静态网站仓库没有改动。本批不部署，不新增或触发 GitHub workflow；
Mac 的成功不表示 Linux 或托管 CI 已通过。

## 修复内容

- 构建、project/workspace/source-selection 快照和命名 audit 均回到仓内。catalog
  更新到已验证编译器 pin/digest，并同步五张来源选择/项目卡片及生成的 FEATURES。
- inventory 递归检查整个生产源码集合，工作树与 pin 的新增、缺失、修改都失败；
  即使同时重算 catalog digest，也不能掩盖源码不一致。保持原排序与已审定 digest。
- LICENSE 从实际选用 GOROOT 定位，兼容该工具链的 Homebrew `libexec` 父目录布局；
  不再硬编码本机用户/版本，缺失则失败。
- 将非祖先分支的提案原文固定为仓内证据副本。19,926 字节副本与原对象
  `5e4b118f25262d8022d1b8f72575d89d69f096aa:docs/rfcs/0024-use-paths.md` 完全一致，
  SHA-256 为 `9d3a71b5d773fb3c7b721c7c61bc9e2ec20e6a9773be53b8ea1b66cfbf2a3af9`。
  commit/digest/blob/path 校验继续严格执行；证据内容不导出到公开网站 assets。
- 新增[仓内验证入口](../../website/validation/README.md)，只接受干净已提交 checkout
  及不存在的新输出目录。明确选用已安装工具和缓存，独立物化 current/baseline，
  重新编译二者，保存命令、日志、输入清单、二进制身份、差异及失败状态。
- 完整 parsed AST oracle 比较结构、子字段顺序、语义标量和 token presence；只忽略
  数值位置、注释及 parser caches。不会用 AST 相同替代字节相同，也不以粗略 token
  或标识符集合代替结构对照。
- 只有七阶段与最后的 source/current/baseline HEAD、状态、全部 tracked bytes
  都核对通过，才能写 `passed: true`。任何失败保留日志和部分摘要，并停止自有服务器。

## 实际复验结果

环境：Go 1.27.1 darwin/arm64、Python 3.14.7、Node 26.9.0、Playwright 1.58.2、
Chromium 145.0.7632.6；使用现有 C/race 工具链。工具和模块均未安装/下载。
正式入口退出 0，七阶段全部通过：WASM build、web build、native comparison、
web race、web vet、真实 browser QA、catalog QA。

| 验证 | 实际结果 |
| --- | --- |
| 原生来源诊断 | 13 例 × LF/CRLF × 各两次，共 52 次观察 |
| 旧/新生成 Go | 33 对：27 对字节相同，6 对排版差异保留；33 对完整 AST 相同 |
| 旧/新原生行为 | 5 对程序、10 次运行成功，stdout/stderr 相同；不声称运行了全部 33 对 |
| CLI/WASM 当前版本差分 | 97 fixture / 10 示例保留，按既有矩阵逐字节核对 |
| 浏览器诊断及恢复 | 52 CLI、26 UI LF、52 raw Worker LF/CRLF 观察；每次错误后恢复 |
| 浏览器功能 | 编辑、复制、XSS、32 KiB、390px、超时、中断、重置、无外部请求通过 |
| catalog | 26 张卡片、10 个负例、过滤/链接/剪贴板通过，console errors 为空 |
| 全树 Mac race | **600 顶层 / 1302 子测试通过，0 失败、0 测试跳过**；18 个测试包通过、另 5 包无测试文件 |
| 全树 vet | 退出 0 |
| 完整设计检查 | 123 Markdown、1352 本地链接、26 RFC、13 个 Go 机制命令通过 |
| naming audit | ABI enum/reference/use/sum = 8/3/2/2；新命名与显式旧名拒绝边界通过 |

完整 race 命令在 `c93461c` 干净 checkout 运行，`GOWORK` **unset**，而网站 runner
自行设置其隔离环境。真实 workspace 用例 `TestImportSourcesWorkspace` 通过。
来源与结果见[完整门槛摘要](website-validation-reproducibility-evidence/full-gates-summary.json)、
[网站摘要](website-validation-reproducibility-evidence/web-summary.json)、
[原生摘要](website-validation-reproducibility-evidence/native-summary.json)、
[浏览器结果](website-validation-reproducibility-evidence/qa-result.json)。
UI textarea 会规范化换行，因此 UI 只计 LF；CRLF 由真实 raw Worker 验证。
native-only 最终 Go 类型拒绝不冒称 WASM 执行了最终 Go 类型检查。

生成 WASM 为 16,298,510 字节，SHA-256
`b96b5373ef081372565f8f3442707fa47f61e2641ee7e29658f3352f07ee9276`；export digest
`64cabea20fc79e847825506245fcc0c4d9649762d873908804c29fbad8a89582`。
std 固定 `ce5bccb4ff9c7fb96b7f192a3a263051590d9288`，serde 固定
`a1b791e96d660c6be620e110c566a9a171631b7e`，ABI 8/3/2/2 不变。
详见[验证身份与 manifest](website-validation-reproducibility-evidence/validation-identity.json)。

## 独立审查与失败记录

独立审查最终无阻断，精确绑定 `c93461c` 的全部 20 个工具/网站新增或修改文件。
分别完成 22 个源码/身份/LICENSE 观察、13 个 AST 输入对、1 个真实 Git pin 场景的
6 个断言、11 个阶段失败/旧输出/矩阵断言、10 个真实 Git 身份变化与清理场景。
这些单位和重复红绿运行不相加成测试总数；轻量阶段探针不算完整浏览器/编译器运行。
见[独立审查](website-validation-reproducibility-evidence/independent-review.json)及
[准确文件绑定](website-validation-reproducibility-evidence/exact-source-binding.json)。

两项实际独立红测已修复并保留：同时把提案引用改为可变 `HEAD` 曾被接受；给 current
snapshot 创建空提交而不改变 tree/bytes/status 曾被误报成功。最终分别校验完整 immutable
身份及 snapshot HEAD，原反例均拒绝。红测、修后绿测均在本报告证据目录。

根代理第一次小型 Go 检查因默认 build cache 沙箱不可写退出，换到获准的新临时缓存后通过。
另两次全树 race 误把 `GOWORK=off` 设置为全局环境，影响 workspace fixture；首次有真实
`go work vendor: no go.work file found` 失败。两次均已中断，不计通过；无源码或测试变更，
改为 `env -u GOWORK` 后完整重跑获得上表结果。失败日志 hash 与
[环境纠正记录](website-validation-reproducibility-evidence/race-environment-correction.json)保留。
README 同步修正全树命令，未通过弱化校验、删除 fixture 或跳过测试取得绿灯。

## 无任务目录源码依赖的证据

正式根目录为 `/private/tmp/goxide-website-repro-20261006-jy8y93vh`，准确源码在其
`final-checkout`，输出在 `final-run`。独立单分支 clone 无 alternates，确实没有无关提案
commit `5e4b118`，仍能通过提案证据校验；新旧 CLI 都从该次独立快照重新编译。
source/current/baseline 的 HEAD、状态和全部 tracked bytes 在最后再次核对，均干净。

206 条记录命令的 argv/cwd 和 Go 环境均未引用 Codex 任务目录；源码、比较器、快照与
输出来自该独立根目录。Go build cache 初始为空。模块源码与 Playwright packages 从
现有固定依赖缓存独立复制，复制后没有逃出副本的符号链接，安装工具仍使用声明的现有
系统路径。缓存准备来源如实记录，不把已有依赖称为从零下载或声称完全无外部环境前提。

这是命令、身份和输入 inventory 的证明，**不是操作系统文件访问追踪**。
见[干净 checkout](website-validation-reproducibility-evidence/clean-checkout-proof.json)、
[无任务目录输入检查](website-validation-reproducibility-evidence/no-task-dependency-proof.json)、
[依赖准备](website-validation-reproducibility-evidence/environment-preparation.json)、
[命令](website-validation-reproducibility-evidence/validation-commands.json)、
[输入清单证明](website-validation-reproducibility-evidence/input-inventory-proof.json)。
source/current 清单逐字节一致，仅保存一份 source 清单；baseline/Playwright 清单分别保存。
所有发布的原始证据副本未规范化，来源、字节数、SHA-256 见
[发布清单](website-validation-reproducibility-evidence/publication-provenance.json)。
较大的全树 race 原始日志保留在上述临时根目录，仓内摘要记录其 hash、时间与计数。

## 短复现命令

先选干净、已提交的本分支完整 checkout；需要上述历史祖先对象、已安装工具、完整固定
Go 模块缓存及匹配 lock 的 Playwright。`--out` 必须是不存在的绝对路径，其父目录已存在。
以下环境路径按机器实际现有位置填写；没有任何任务目录路径要求。

```sh
export GOTOOLCHAIN=local GOPROXY=off
python3 -B website/validation/run.py \
  --out /absolute/new-validation-directory \
  --chromium /absolute/existing/chromium \
  --playwright-node-modules /absolute/existing/node_modules
env -u GOWORK go test -race -count=1 -timeout 30m ./...
go vet ./...
python3 -B scripts/check_design.py
```

可用 `--go`、`--node`、`--go-cache`、`--mod-cache` 与 `--port` 指定工具、缓存与空闲
loopback 端口。全树测试另用实际可写的 `GOCACHE`、`GOMODCACHE`、解析后的 `TMPDIR`。
入口不会安装工具、下载模块或 fetch 历史对象，缺少依赖会明确失败。
使用 `summary.json` 的整体结果，不以单个成功日志或旧输出目录推断通过。

## A04 下一批实施边界

下一批只接既有单值 match 到常见 unary 与 composite literal 上下文，例如
`return -(match …)`、`!(match …)`、`S{N: match …}`、`[]T{match …}`。
沿现有 atom→prelude/value 模型，从真实函数/赋值/字段/元素取得可命名 expected type，
保留 alias/nominal 身份、溢出、shift 和 nil 边界，不以 `any` 擦除类型。

验收应证明 scrutinee 一次、guard 顺序、只执行所选 arm、composite 内调用顺序与短路，
无效 enum 在业务副作用前拒绝；`?` 指向最近用户函数，失败跳过后续字段/元素。
不引入改变 direct recover 的 IIFE/frame，defer、goto、权限及可寻址性保持。
既有 index assignment 仍修改原存储、目标操作数一次求值，保持 RHS 与 bounds/nil 写入
检查时序；本批不顺带扩展复杂 LHS。

端到端业务验收：JSON → closed enum → unary/composite match 装配业务对象 →
含 slice 的派生 struct → Clone 并修改副本 → Debug → JSON；核对原对象不变、nil/empty、
错误 cause/path、失败后零后续副作用，生成模块 race/vet 与输出确定性。
相关起点为 [matchexpr](../../internal/matchexpr/README.md)、
[现有组合验收](../../tests/acceptance/match_composition_test.go)及
[差距清单 A04](../mvp-gap-audit.md)。

控制头、package initializer、多结果、不可命名类型、reference/send-receive unary 与
复杂 LHS 分成后续工程切片，仍在账上。block arm、generic、原位 mut&、blank/embedded、
serde adapters 及公开 enum 入口规则不借本片擅自定约。本交付只关闭 A26/A28 中的
本地验证复现欠账，不关闭 A04、A01/A03 或 MVP 整项，也不改变实际网站发布状态。

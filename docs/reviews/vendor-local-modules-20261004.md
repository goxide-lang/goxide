# 原生 vendor 输入物化为本地模块验收

分支 `feat/vendor-local-modules`，基线 `d23064760e1fb5d4bb77cae17e5d7818fb73139d`；规范先行 `f08c50c`，本地核心实现 `56fab9834a7a37acaba42d954b34ff47aafca7d3`。std 仍使用 ce5bccb，未改变 std/serde API 或 module import 路径。

## 已交付行为

- vendor 仅在 import-go 读取原生输入时解释；build/run/test/transpile 不使用 `-mod=vendor`、PrepareVendor 或 vendor 计划。旧 TOML vendor 字段和旧 lock vendor 字段（含 null）明确要求重新迁入，不能静默改变来源。
- 供应源码物化为 `local-modules/<module-id>/` 普通 Go 模块，保持逻辑 module identity，以明确 local replace 绑定。保留直接/间接关系；require 的实际标签匹配 Go 确认的供应选择，原政策留审计，不把全部模块提升为直接依赖。
- `goxide-import.json` 记录原 go.mod/go.work/sum/modules.txt、全部 header、文件内容/权限摘要、输出映射及合成 metadata。原 go.sum 不认证本地修改；未与远端比对时为 unknown。裁剪源码 import 图不宣称完整上游图。
- 所有供应 Go 源（含平台/tag）参与依赖识别，资源/cgo/header/asm/embed 与许可证保留。模块级 require 环可用；缺包、未知归属、许可证输出碰撞、旧锁、特殊文件等明确拒绝，无网络补包。
- 本地源码正常可编辑。锁中的可搬移本地身份和磁盘位置分开：从当前显式路径绑定并校验身份/metadata，不读取旧 staging 路径。workspace 成员独立只需物化继承/本地路径并复制根锁，没有 vendor 拆出模式。
- plan 列出 local 默认选项与有证据的 remote module/版本及 unknown 状态；不从 Go module path 猜 Git URL。`--offline` 复用既有 POSIX 双横线长选项，禁止探测/下载，候选可显示本地证据但不宣称已核验。

## 独立复审

独立 reviewer 只读审查并实际复现：宿主标准库列表遗漏 syscall/js；父模块根包抢占嵌套模块 LICENSE；合成许可证路径覆盖供应资源；合法 workspace 同模块多 explicit 版本 header 被误拒；普通 local wildcard/exact-version 替换优先级。

均已修复。标准包身份从当前工具链 GOROOT 源目录读取而不按宿主裁剪；模块与文件归属核验及输出碰撞拒绝；原生 Go 选择正确版本并保留完整原 headers；exact-version 优先。reviewer 再次运行多版本、双 explicit 无包且移除原替换目录、跨平台、许可证、旧锁和复制锁测试，确认当前 local core 无剩余阻断。

## 验收范围与阶段边界

真实 CLI 覆盖修改字节、原文件不变、冷 module cache、离线 locked、输出搬移、删除原 workspace、复制根锁、正常本地编辑、混合 Goxide/Go、features/宏/脚本授权、原生 Go/work 无 vendor 迁入，以及合法模块环。请求记录 HTTP stub 验证 local plan、非交互迁入与缺包错误的零代理请求；这不声称任意用户脚本的 OS 网络隔离。

本阶段交付本地核心及只读来源候选。**TTY 一次选择清单、显式 remote 选择参数、远端差异核验/修改确认尚未实现**，作为用户允许的下一阶段；相应 TTY/远端离线错误分支也尚未验收，不能宣称已具备。当前所有迁入使用本地快照，不进行远端选择或探测。

网站构建和浏览器验证仅在云端 loopback；不部署。owner 同步此分支代码、文案、快照脚本及 catalog 后重新构建。历史研究/冻结文档记录保留当时行为，当前 RFC0025/26 与供应源码设计已撤回旧 vendor 模式和拆出提案。

## 已完成的辅助检查

`go vet -tags goxideweb ./...` 退出 0；设计检查通过 13 条 Go 命令（含预期失败），Markdown 链接/规范结构检查通过。真实 CLI 独立进程确认搬移后 workspace `--offline --locked` 构建退出 0；`import-go -offline` 退出 1 且提示 `--offline`。

网站构建通过，WASM 15,898,810 bytes，sha256 `838a567da027420f4b7a6d665c18d001434257d7119ba1e90c30e52bb5e1f2c7`。浏览器 10 示例与 CLI 逐字一致，26 卡片、10 目录拒绝例、工程快照、移动端/剪贴板、同源请求及陈旧/损坏 WASM 拒绝均通过；web tag 包 race 通过。原始输出见 [设计检查](vendor-local-evidence/vendor-design.log)、[浏览器对照](vendor-local-evidence/vendor-wasm-qa.log)、[目录检查](vendor-local-evidence/vendor-catalog-qa.log)、[进程结果](vendor-local-evidence/vendor-process-evidence.json)。

## 最终 race 与兼容收尾

完整 `go test -race -timeout 30m -json ./...` 退出 0：22 包（含无测试包）、508 个宿主顶层测试、1061 个子测试，无失败；CLI 95、acceptance 148。这是实施期间的全树回归。随后对核心提交 `56fab98` 再次完整运行受影响的 `internal/project` 与 `cmd/goxide` 两包 race，118 个顶层测试、169 个子测试全部通过（CLI 增至 96，含最后补充的 workspace 多版本/空模块用例）。详见 [全树汇总](vendor-local-evidence/vendor-local-race-summary.json) 与 [核心包汇总](vendor-local-evidence/vendor-local-final-race-summary.json)，包含完整原日志 SHA256。

收尾额外扩展普通 Go 复制锁测试，发现未使用成员 replace 被纳入新 local 身份归一化。`fb4fba3ef229a00e12c800088a12f0f0c377167c` 将其限定为实际记录的普通本地依赖；显式成员原有规则保持。独立 reviewer 复跑普通复制锁、供应模块搬移、丢失 work override 拒绝三项通过；补丁的 project/CLI 专项 race 通过（CLI 64.278 秒），最终 vet 通过。见 [补丁 race](vendor-local-evidence/vendor-unused-replace-fixed.log)。此处保留既有合同：显式 local replace 路径即使未使用也需要物化；不引入缺失路径的惰性发现。测试中的该路径已明确物化，不能把这个结果解读为允许任意缺失 replacement。

最终原生输入补丁 `fceeb8669e738ebffc968b8bd948b2334fe627cb` 将供应清单中未选的 replacement-only 尾记录限定为 provenance，避免合法原始 vendor 的未使用旧路径成为迁入后的运行时依赖；Go workspace 主成员的原替换政策仍保留。单模块/workspace 都加入不存在的旧目标回归。独立 reviewer 实跑四项相关测试通过，确认没有扩大普通作者工程的路径规则。最终专项 race 覆盖 TestSupplied、TestVendor 与 vendor/supplied/复制锁 workspace 用例，project 1.172 秒、CLI 138.122 秒，退出 0，见 [最终专项 race](vendor-local-evidence/vendor-provenance-final-race.log)。

最终生产编译器源绑定 `fceeb8669e738ebffc968b8bd948b2334fe627cb`，digest `c03e0b656ede234d0d11f2a8fbeb0a1204534594d70a69737b7d5781fb66b472`。后续证据提交不修改生产源码。全程没有 Mac 操作、main 合并、tag/release、仓库重命名/权限修改或网站部署。

最终绑定后的 vet、网站重构建、浏览器 CLI/WASM 对照与目录 QA 均退出 0。收尾 QA 首次启动漏载可写 Go 缓存环境，CLI 子进程尝试建立只读 `/home/agent/go` 而失败；加载既定环境后完整重跑通过，未修改产品代码。见 [最终构建](vendor-local-evidence/vendor-website-delivery-build.log)、[最终浏览器对照](vendor-local-evidence/vendor-delivery-wasm-qa-fixed.log)、[最终目录 QA](vendor-local-evidence/vendor-delivery-catalog-qa.log)。文档检查在新证据文件复制前曾报告链接缺失，复制后重新验证。

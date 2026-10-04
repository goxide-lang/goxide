# 供应源码迁入为本地模块

状态：本地核心已实现于独立分支 `feat/vendor-local-modules`；来源候选显示已实现，以下来源选择合同进入实现。取代旧 Goxide vendor 模式和 workspace vendor 拆出提案，不改冻结历史或原生 Go 自己的 vendoring 行为。

## 输入与输出

`import-go` 读取原 go.mod/go.work 与 vendor/modules.txt，使用真实 Go 只读一致性检查。供应内容分配至 `local-modules/<module-id>/`，每个目录有合成 go.mod；TOML 显式本地 replace 指向这些目录。目录身份取 module path 的稳定摘要，不取 fork 名或临时路径。原始源码、资源和许可证保留内容与文件权限；合成 go.mod 单独记录为新增元数据。祖先许可证复制到相关模块的许可证记录目录，不覆盖原模块自身文件。输出 provenance 记录每个输入文件的摘要及一个或多个输出位置，以及原版本、replace、explicit 和 Go 版本。原 modules.txt 只进入来源记录，不放入输出 vendor 树。

合成模块保留逻辑 import identity。供应源码扫描全部 Go 文件的 imports（含 tests、平台/tag 文件）；精确包声明决定归属，声明模块前缀用于诊断缺失包，不能以字符串前缀抢占嵌套模块。已知标准库和 `C` 不形成第三方边；未知包明确拒绝。供应模块 go.mod 表达实际供应 import 边与 modules.txt 版本标签；这不是恢复上游完整依赖图。无包 explicit 模块仍有空 go.mod；其 Go 版本缺失时沿用输入根最低版本并在 provenance 明示。替换尾记录没有选中版本时仅保存来源信息，不把未使用的旧 replace 目录变成运行时依赖；它不同于 explicit requirement 对应的无包模块，后者仍保留空模块声明。workspace 主模块政策继续保留。

为 Go 的裁剪图求解提供 modules.txt 已选版本约束及显式 local replace；原 require 的直接/间接关系保留，低于供应选版的标签提升到供应版本，原完整 go.mod/go.work/sum 与 modules.txt 另存 provenance；不从原始 go.sum 认证修改源码，不调用 tidy/get/download/vendor。所有供应模块均本地绑定，模块间循环 require 可以存在，包环交 Go 拒绝。目录中缺失未来新 import 必须失败，不以在线代理补包。补全供应内容必须由用户显式提供源码和依赖政策。

## 普通本地模块语义

输出只有普通模块、显式依赖路径和普通锁。build/run/test/transpile 与后续增强功能均不解释 modules.txt，也不设置 `-mod=vendor`。本地编辑是正常作者行为；缓存随源码变化，锁约束身份/依赖政策而非供应文件不可变。provenance 是迁入时审计，不是后续构建锁。

workspace 中供应模块不是隐式 use 成员；它们由普通 require/local replace 提供，不伪造增强依赖边。原作者成员间依赖仍按真实 Go 包图物化并验证。拆出成员时物化继承及可达本地路径、复制根锁；不需要 vendor 转换，且不能靠原 workspace 路径偷偷取源码。原 work override 和成员独立政策不一致时明确失败。

旧 TOML vendor 字段或 lock vendor 记录均拒绝并要求从原生输入重新迁入；不静默丢字段/忽略旧锁。普通无 vendor 的 Go/work 迁入行为和增强模块 API 保持兼容。

## 白黑例

| 白例 | 对应黑例 |
| --- | --- |
| 本地修改 D 源码，原 replace 目录删除；物化后冷离线 build/run 相同结果 | 使用原 go.sum 当 D 内容认证，或向 proxy 恢复 D：拒绝 |
| D/p→E/q，E/r→D/s，包图无环：本地 module require 环可编译 | D/p→E/q→D/p：Go 报包环 |
| D 与 D/sub 为不同 module，准确保留各自 packages | 仅最长字符串前缀错分声明包：拒绝 |
| 平台/tag/cgo/embed/祖先 LICENSE 全复制并记录权限 | 只复制当前 host 包文件、丢许可证、未知归属：拒绝 |
| explicit 无包模块保留空 module 声明 | 自动下载补包或伪造空 Go package：拒绝 |
| 移动整个输出目录并删除原工程，离线 locked 构建 | 锁或 replace 指向已删除 staging/original：验收失败 |
| workspace 成员物化 path 并复制根锁，离线独立构建 | 缺本地模块或可达锁身份冲突：拒绝，不恢复隐藏 workspace |
| 迁入后编辑本地依赖，按正常本地模块规则重建 | 旧 vendor 模式/锁继续运行：要求显式重新迁入 |

## 验收

真实 CLI 覆盖单模块/workspace、冷缓存、输出移动、原始 hash 不变、所有资源/权限、混合 .go/.gox、features/宏/脚本、缺包在线也拒绝、普通无 vendor 迁入、旧配置/锁拒绝、合法模块环与非法包环、成员拆出复制根锁。独立复审后运行适用全树 race/vet/design 和 WASM 回归。网站只同步文案与快照源，不部署。

## 已确认的来源选择交互（后续交付）

用户已确认模块级多来源候选，默认供应快照本地模块。TTY 在迁入时展示一次选择清单；非 TTY 默认确定性 local，也可用显式参数覆盖。正常 build 不询问。清单逐模块列逻辑身份、直接/间接关系、local 输出路径、remote module path/实际版本，以及 modified/verified/unknown 状态。当前本地物化交付先显示候选；交互和远端执行另一步验收后启用。

远端候选只来自 go.mod 与 modules.txt 选中版本/替换证据；go.sum 是校验集合，不是 lock，不能从其未选历史行猜版本。Go module path 不等于 Git URL；没有确认的 URL+revision 就不列 Git 候选。原 local replace 没有可证实的远端身份时，显示远端不可确认，不能假定原逻辑 module@version 就是供应源码的上游。

默认 local 不联网比对；unknown 不等于未修改。选择 remote 才可按明确身份获取并核验，比较所有供应文件，说明裁剪集合不能证明完整发行物等价。切换可能丢失本地修改，发现修改必须明确确认；无法核验则不将其标 verified，也不能静默丢供应字节。plan 仅输出候选，不执行来源切换。

| 白例 | 黑例 |
| --- | --- |
| D@v1.2.0，无 replacement：列 local（默认）和 Go module D@v1.2.0（unknown） | 从 go.sum 的 D@v1.3.0 推测当前版本 |
| D@v1.2.0 => F@v1.4.0：列实际 F@v1.4.0，保持逻辑 D 身份 | 将 D 的路径猜作 Git URL |
| D=>../fork：local 默认，远端未知 | 无证据自动选择 D 的远端发行物 |
| TTY 逐模块选择，非 TTY 省略参数保持全部 local | CI 阻塞等待交互，或将每个间接模块升为直接依赖 |
| remote 比对发现修改，明确确认后才切换 | 未确认覆盖修改，或把 unknown 显示为 verified |

### 离线来源选择

现有 `--offline` 同时约束迁入分析、来源候选核验和构建验证：禁止远端探测、Go proxy/sumdb、Git fetch 和工具链自动下载，仅用本地文件与缓存。由本地元数据推导的远端 module path/版本仍可显示，但明确标未联网核验；缓存不足必须失败，不自动恢复网络。

白例：隔离环境显示 local（默认）及 D@v1.2.0（未联网核验），迁入供应快照成功。黑例：选择远端但本地缓存不足，明确错误且请求计数为零；不得为了比较差异联网。验收覆盖 plan、非 TTY、交互选择及错误分支，以请求记录 stub 验证。此开关不宣称为任意用户脚本提供 OS 网络沙箱；迁入不运行安装/生成脚本。

### 来源选择执行合同（2026-10-05）

本轮候选限定为供应清单证实的精确 Go module 版本，包括版本化 replacement；不提供 registry、任意 URL、Git 推断或新版本搜索。`--source MODULE=local|remote` 可重复，MODULE 是逻辑模块身份；重复、未知模块或其他值报错。省略时非 TTY 全部 local；stdin 与 stderr 均为 TTY、且没有显式 source 参数时展示一次模块清单，用一行 `MODULE=remote`（空格分隔）覆盖默认 local，空行接受 local，`cancel`/EOF 取消且不发布。显式 source 参数关闭自动来源选择；远端差异确认仍按下述规则。`--plan` 永远不读取终端、不联网比对，显示候选及请求；不得以 plan 作为差异确认。

remote 只按证据里的精确 path@version 调用 Go 下载/缓存核验，在隔离临时目录中进行；遵循 Go proxy/sumdb 政策，强制 GOTOOLCHAIN=local。用下载 zip 的内容核验供应文件，避免把可编辑解压缓存作为认证依据；不将原 go.sum 当锁。审计记录下载返回的校验和及逐文件差异。verified 仅指供应集合的字节相等，不代表裁剪快照与完整上游发行物等价；权限差异单列，不把 Go zip 的规范权限冒称供应原权限。缺少同名文件也是 modified；模块目录外祖先许可证不能可靠映射时明确 unknown，按差异处理。下载/校验失败直接中止。

有 modified/unknown 差异时逐模块展示清单，TTY 要求输入完整 `remote MODULE` 确认，其他输入或 EOF 中止。非 TTY 必须显式 `--accept-source-diff MODULE`，只接受已经显式选择 remote 的模块，不支持全局 yes。所有供应字节仍保留于输出 local-modules 与 provenance，即使该模块已选择 remote；它们不再作为该模块运行来源。正常构建不读取审计，也不再次询问。

remote 绑定为普通精确 Go module replacement，保持逻辑 module/import 身份和 direct/indirect 角色。远端完整 go.mod 可能引入新依赖或使 Go MVS 选版变化；迁入后的真实构建/锁按正常 Go 规则验证，不伪装还原裁剪供应图。所有候选先完成来源及差异确认，再验证新目录；任何失败不发布目标、不改变原工程。

| 正例 | 反例 |
| --- | --- |
| `--source example.org/d=remote` 使用清单证实的 v1.2.0 | `--source example.org/d=git:https://...`、未选模块、重复 source：拒绝 |
| 无参数非 TTY 全部 local；TTY 空行 local | 管道缺输入或 EOF 被默认为确认 remote：拒绝 |
| 非 TTY 修改模块显式附 `--accept-source-diff example.org/d` | 没有精确确认、确认 local 或未知模块：拒绝 |
| remote 供应集合相等，报告 verified subset | 根据缺失的测试文件断言完整上游相同：禁止 |
| `--offline` 缓存 zip 完整可核验并使用 | 缓存不足联系代理/sumdb/VCS/工具链：禁止，零请求失败 |
| 取消、输入错误、远端不可确认、构建失败不发布目标 | 先覆盖原源码或留下部分正式输出：禁止 |

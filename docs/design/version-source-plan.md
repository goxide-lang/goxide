# Go module 渠道的 goxide 项目与统一选版

作者根与锁格式以 [RFC 0025](../rfcs/0025-unified-project.md) 为准；本文的 Go 原生指令经 TOML 映射后交真实 Go 执行。

状态：已按“先文档、再实现”实施；初稿经独立 review 后先提交为 25e3fa6，实施基线为 f097b1951dde4f8653a0629a7da8a0bb5646ec79。最终检查结果以本次交付记录为准。接续 [RFC 0022](../rfcs/0022-project-toolchain.md) 和 [RFC 0023](../rfcs/0023-build-scripts.md)，不更改 Result/enum/宏推导语义。

## 格式和发布合同

无需新的 source 字段。`go = "模块路径"` 加 `version`、`git`、`path` 是获取渠道；单独 version 的 registry 不实现，goxide.toml 则表明是否使用 goxide 的 feature/lib/build 项目规则。例如：

```toml
[dependencies.lib]
go = "example.org/lib"
version = "v1.2.0"
features = ["fast"]
default_features = false
```

version 仍使用 Go 可接受的规范版本/伪版本。Go 自行处理 module path、MVS、proxy、zip 与 sums。goxide 不实现第二套版本比较器，不把版本请求当作精确最终版本。不同到达边的版本要求共同参加 Go MVS；对同一实际源码身份的活动 manifest features/default 请求取并集。根 TOML go.require 已要求 同库时，manifest 的 default_features:false 仍有效；Go require 不构成第二条默认 feature 请求。

Go module zip 可以携带 goxide.toml/.gox；嵌套模块、vendor、链接等受 Go zip 规则限制，不能假设发布目录等于工作树。[Go module zip 规则](https://go.dev/ref/mod#zip-files) 与 [go mod download 的 JSON 输出](https://go.dev/ref/mod#go-mod-download) 为机制来源。实现使用实际返回的 Dir、Version、Sum、GoModSum，不按缓存路径命名猜测身份。

- 有 goxide.toml 的源码发行物：按 goxide 项目处理，可为 .gox、手写 .go 或显式 build 生成源码；作为依赖必须有 lib。默认 feature 与 git/path 相同。
- 无 goxide.toml 的纯 Go 或已生成 Go 发行物：保持原生 Go/既有 metadata 消费路径；显式请求 goxide feature 必须报错，不能假装重配已有二进制/Go 产物。
- 源码 goxide 项目不能混入 goxide 生成物：拒绝 goxide_project_config.go、已知增强 metadata、.go.map.json，或同路径同名 .goxide/.go 双源，避免重复声明和旧配置污染。手写不同名 Go 文件正常保留。不按普通 `Code generated` 注释拒绝其他 Go 生成器产物。
- 本轮没有 source/generated 文件所有权 manifest 或双视图发布协议。直接 Go 消费者不会执行 goxide feature 解析：source-only goxide 库需先转译并发布固定配置 Go 发行物；纯 Go 已发布默认产物可直接消费，但不可通过 features 重新配置。不能宣称一份混装 zip 自动同时满足两种消费方式。

## 统一规划与稳定性

1. 根项目身份、锁、Go module 文件读取后，在独立短期规划目录工作；不修改用户 go.mod/go.sum 或下载后的 module Dir。
2. 每轮从根重建当前可达的 goxide/Go 依赖边与 feature 请求。只激活本轮 manifest 的 optional 边；原始 go.mod require 仍属于 Go 图。上轮消失的 goxide manifest、边、feature 请求不得残留。此处只重建 goxide 额外边；Go 原生图中的非最终版本 requirements 也可能影响 MVS，完整 Go 图必须交由 Go 保留和解析，不能自行按最终版本裁剪。
3. 原生 Go require 与激活 goxide manifest 的版本要求进入同一个 Go main-module 规划图。活动约束先作为 main module 根要求；只含 go.mod 的私有桥接模块保留多个最低版本请求，让真实 Go MVS 合并。桥继承根的 Go directive，不用旧 go 1.16 强迫新项目展开未裁剪图；桥接模块不会进入输出 Go 模块或用户锁身份；goxide-planner.invalid/ 是内部保留 module namespace，真实根/传递依赖引用会明确拒绝以防碰撞。path/git 用规范化源码替换，根 TOML 原生 Go 政策 replace/exclude 为全局权威。冲突的显式 goxide path/git 与根 replace 必须报错，不能静默覆盖；依赖自己的 replace/exclude 不提升为根规则。
4. 用 `go list -m -json all` 得到实际选版，再用 Go 下载选中源码。对于活动 goxide.toml 依赖边读取当前版本的 goxide.toml 并重新计算请求。明确分开 SelectedModules（Go 选版/锁）与 BuildNodes（增强编译/脚本许可）：只有活动 manifest 边提升为 goxide 构建节点，沿用 path/git 的显式规则；仅原生 go.mod require（包括 indirect）不产生 goxide defaults 请求，也不会自动准备 lib 或脚本。消费 source-only goxide 库必须加显式 goxide.toml 依赖边；仅原生图的固定配置 Go 产物正常消费。桥接和计划注入的 require 同样不产生 feature 请求。
5. 选中版本/来源、可达边、features 都不再变化才稳定。版本变化可删掉旧依赖，因此不能只做不断加边的闭包。重复的非相邻状态诊断振荡；设有限轮数上限，超限拒绝。中间版本的未知 feature、缺少 lib 等错误在最终稳定视图确认；不得先编译临时版本。最终增强构建依赖环明确拒绝。
6. 稳定后生成依赖顺序，并把同一个选版/替换计划应用到每个独立生成 module。Go 重新列出的实际 build list 必须与计划一致，不一致拒绝；不能继续依赖原先“各节点独立选版后恰好相等”的做法。CLI 对全图只含 go.mod/go.sum 的影子模块先应用政策并核对；全部通过后才运行脚本、类型检查、宏展开和输出，实际生成副本再核对一次。bundle/main-anchor 只含根 TOML 原生 Go 政策，允许原生模块级回边；不提供根包源码，实际包循环不会因此成为支持能力。

规划只处理显式依赖闭包，不扫描任意相邻目录；Go lazy graph 的完整模块列表不等于所有包都需要转译，库包选择仍遵守 RFC 0022 的 lib 闭包边界。纯 Go 模块由 Go 编译；goxide 导入不能被 `go mod tidy` 的原生 .go 扫描静默遗漏。

## 身份、边界与锁

逻辑身份是 module path，来源身份包含选中版本、替换后实际 path/version、Go sums 或显式本地/git 身份。同一 module 只能有一种最终来源；经根不带版本限定的本地 replace 到同一规范目录的 version 与 path 可统一；没有该显式根规则时不能用 path 静默覆盖 version 来源。goxide 源码的 go.mod 声明必须仍等于消费逻辑 module path；替换为改名 fork 时不自动改写源码自导入或增强身份，声明不一致明确拒绝。纯 Go 保持 Go 原有替换规则。不同目录或替换来源冲突拒绝。

version 下载 Dir 只读：转译、config、metadata、生成脚本输出和 go.mod 编辑一律在 target bundle 副本。下载内容的路径依赖/host helper/tool 不得逃出所取得模块边界；zip 不含的嵌套模块不能通过缓存相邻目录补齐。拒绝 symlink/伪造 manifest/go.mod，读取 manifest/编译/执行前运行 go mod verify，覆盖已取得的解压源码；goxide zip 中的 go.mod 还必须逐字节等于代理 .mod 元数据，防止规划和编译读取不同依赖政策。download JSON 的 Sum 不能证明现存 Dir 未被修改，也不以只读 mode 位代替内容验证。

goxide.lock 增量保存每个准确根配置的稳定模块计划和下载身份/sums；稳定状态与锁身份不包含随机规划目录、桥接模块或生成 bundle 路径。已有 Git/sums 项继续保留。`--locked` 比较最终实际计划，缺项或选版/sums 变化拒绝；`--offline` 只用 Go/Git 暖缓存，冷缓存清楚失败。锁不冻结用户 path 内容。未激活 optional 来源不为它单独请求、下载或授权；若同模块被 go.mod 引入则仍是 Go 显式依赖。

target config、前端配置标记/metadata、脚本 SourceID/缓存/非根执行闭包许可都包含稳定选版来源身份。升级版本即使脚本字节不变也需新的依赖执行许可。全图稳定并检查所有许可后才执行；不能通过解析临时版本或旧 cache hit 绕过 RFC 0023。脚本不能反向改动 feature 或选版图。

## 黑白例与真实验收

| 编号 | 白例 | 黑例/明确拒绝 |
| --- | --- | --- |
| VS-01 | file proxy 的 goxide v1 默认、关闭默认、显式 feature 与 path/git 输出相同 | 无 manifest 的 Go 发行物接收 feature 请求 |
| VS-02 | 两条活动 manifest 边请求同库 a/b，最终并集且任一边要求 defaults 即生效；go.mod 同库 require 不撤销 false | 未知 feature、feature 环、最终 goxide 构建环 |
| VS-03 | 根请求 v1.0，原生 Go 依赖要求 v1.1，所有 goxide 节点统一使用 v1.1 | 生成节点实际 build list 与规划不同 |
| VS-04 | v1.1 manifest 更换依赖，只保留当前边并重新选版 | A@1→B，B原生require A@2，A@2不再依赖B导致反复切换：振荡诊断 |
| VS-05 | 根 replace 到本地 goxide，version/path 同源请求并集 | 同 module 不同 path/git、下载模块 path/host helper 越界 |
| VS-06 | 暖缓存 offline+locked 重复、A/B/A target/config 切换 | 冷缓存、缺计划锁、sum 或版本变化；Go cache 污染 |
| VS-07 | .gox 与不同名手写 .go 库、lib 闭包保持 | 同名双源/增强 metadata 混装；仅生成 Go 不能重配 |
| VS-08 | 已许可本地 fixture 脚本在稳定版本生成源码；repeat 可命中 | 未许可/升级版本/撤销，先拒绝再执行；临时选版脚本绝不运行 |
| VS-09 | 生成普通 Go 可直接 build/run/race/vet；原始 cache 递归内容 hash 不变；仅 Go 间接 unused 模块不触发 goxide 脚本许可 | 直接 Go 或仅 go.mod 消费 source-only .gox 无显式 goxide 边/预生成产物，不作支持承诺 |

## 实施顺序与检查

1. 本文与 RFC 补充先独立 review、修订并提交；此时不修改生产代码。
2. internal/project 增加共享 Go 规划、来源识别、稳定图、计划锁；先做本地 proxy 多版本/变化/拒绝单测。Go 解析器与版本排序继续外包真实 Go。
3. cmd/goxide 应用最终计划到生成模块，传递来源指纹到 target/config/scripts；不进入另一个 Result 推导任务的前端语义区域。
4. localproxy E2E、真实 Go cache 不改写、离线/锁定、多目标、脚本许可测试；独立 review 后修复。
5. 全量 race（含 goxideweb 资产准备）、vet、文档校验；提交实现并推送独立分支，核验远端，不 merge/tag。

上表为语义验收矩阵，具体测试入口见下节；最终执行结果以交付记录为准。若统一图的必要拒绝超出这些合同，先说明具体反例和选择，不以沉默放宽语义完成任务。

## 已实现的验证入口

生产改动集中于 internal/project/planner.go、graph.go 和 cmd/goxide/project.go/scripts.go，没有修改 Result 或前端表达式语义。新 cmd/goxide/version_test.go 提供 VS-01～09 的 localproxy 回归，包括缓存污染、manifest 切换后旧边消失、振荡、后置高版本桥的新增原生闭包、仅原生 unused 模块不激活脚本、纯 Go fork、原生 module 回边、A/B/A 和真实生成 Go race/vet。脚本只使用本仓库自编 fixture，经现有 plan/allow 流程执行。

独立 review 发现并修复：内部桥覆盖真实 module namespace、纯 Go fork 被 goxide 身份检查误拒、旧桥 Go directive 改变 pruning、脚本执行早于全图政策预检，以及较晚 Git 边收紧来源边界后未重新检查子依赖（独立红绿回归覆盖）。goxide 改名 fork 保留明确拒绝；不是遗漏修复的纯 Go 兼容问题。源码/生成物混装仍没有自动所有权分配，不能冒称双视图发布协议已完成。

构建顺序仍是增强 module 级 DAG，不实现跨 module 的包级交错排序。policy-anchors 只保证原生 module 图回边的政策解析；依赖不能通过它导入尚未生成的根包，即使某个包级图本身无环也不在本阶段支持范围内。

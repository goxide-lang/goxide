# goxide.toml 项目清单迁移

> 后续用户决定已取代本文长期双清单兼容建议：见 [RFC0025](../rfcs/0025-unified-project.md)。本文保留格式切换的历史验证记录；统一作者模型以新RFC为准。

用户已决定正式清单从 hgo.json 改为 goxide.toml；本设计与实现同步，不兼容读取旧 JSON，也不改变 go.mod、feature 解析、选版、脚本授权或 target 语义。hgo.lock 名称及其 JSON 内容保持不变。

```toml
[package]
format = 1 # 清单格式版本，不是包发布版本；module 身份仍来自 go.mod

[features]
default = ["pretty"]
pretty = []

[dependencies.codec]
module = "example.org/codec"
path = "../codec" # 或 version / git，三选一；路径相对清单
features = ["json"]
default_features = false

[lib]
name = "app"
path = "src/lib.hgo"

[[bin]]
name = "cli"
path = "src/main.hgo"

[[example]]
name = "demo"
path = "examples/demo.hgo"
required_features = ["pretty"]

[build]
entry = "tools/build/main.go"
inputs = ["schema.txt"]
outputs = ["generated.go"]
env = []
timeout_seconds = 30

[build.dependencies]
"example.org/helper" = "../helper"
```

对应关系：旧顶层 version → package.format；bins → bin 数组表；examples → example 数组表；其余字段名/值原样保留（包括 default_features、required_features、timeout_seconds）。不新增 package.name/version/module、workspace、继承或版本简写。依赖 module 字段仍必填，git 的 rev/subdir、optional 等原有字段不变。build.tools 是别名到工具路径的表，沿用旧执行/信任合同。使用正规 github.com/pelletier/go-toml/v2 v2.4.3（TOML 1.1，包含 1.0 语法）解析，固定 go.sum；不是自写 TOML 子集。

识别接口：project.ManifestName = "goxide.toml"；project.Detect(root) (bool, error) 统一检测。无清单返回 false；只有 hgo.json 给出迁移错误；两者存在给出双配置错误；清单必须为普通文件。project.Read(root) 通过 Detect 后严格 TOML 解码。CLI、path/git/version 来源与 host helper 均接入，不能把旧 JSON 误判为纯 Go 并跳过脚本/features。原生图中非活动 hgo 依赖仍不因检测获得增强构建/脚本授权。

迁移需要按上表改写内容，不能只重命名扩展名。先生成/手写 goxide.toml，核对字段后将旧 hgo.json 移出项目根保存；两文件同时存在会明确失败。本轮不提供自动删除或覆盖文件的迁移命令。旧来源需发布新的源码修订；锁定旧修订时给出迁移诊断，不暗中更换 revision。现有 hgo.lock 可保留，若源码/清单输入变化，按既有 --locked 规则拒绝变更，解锁重建再审阅锁差异。

新格式身份进入构建配置、依赖计划和脚本来源指纹，旧配置产物不得直接当新配置命中。内部 Manifest JSON 序列化仍可用于确定性摘要/锁内部数据，它不是用户清单协议。脚本输出禁止生成两种清单；host helper 也检查旧/双清单及 bootstrap script，避免改名绕过授权。

未知字段、重复 key/table、错误类型和非法 TOML 语法必须有文件/行/列诊断；动态 feature/依赖表名及业务字段继续既有校验。注释、引号键、多行数组和字符串等由 TOML 库处理。测试包括严格解码与格式等价、旧/双配置在根和依赖/host helper的拒绝、feature A/B/A、mock proxy 多版本、local git、targets/examples、脚本授权/cache、生成 Go 和全量 race/vet。文档示例/feature catalog/官网源文案同步；不声称线上官网已部署。

## 后续纯 goxide 根目录：待确认架构，不随格式切换生效

用户询问根是否只保留 goxide.toml 与 goxide.lock。**可行，推荐作为显式纯项目模式；当前实现仍要求根 go.mod，本轮不删除它。** Go 没有单独 lock 文件：go.sum 是完整性校验记录，不等于锁定所有选版/feature/来源；hgo.lock 目前承担增强锁。本轮保留该名称，改为 goxide.lock 是待确认迁移建议。

建议后续模式由显式字段区分，而不是见到/缺少 go.mod 就猜：

- **纯项目模式（提案）**：goxide.toml 中增加经过批准的 module、Go语言版本/工具链策略和依赖声明。根只需 TOML＋增强锁及源码；先在受管 target 的规划 module 统一 MVS，再为各 unit 生成 go.mod/go.sum。不同 bin/example/config 隔离存储，但同一进程的相同逻辑 package 不按feature复制身份；feature并集依然统一。增量及离线构建依赖锁和缓存，不要求根有隐形生成文件。
- **Go 混用模式（当前能力）**：根保留用户 go.mod/go.sum，TOML只增加features/来源/targets/scripts。module身份和Go语言版本来自go.mod；原生require/replace/exclude与hgo约束按既有规划共同处理。未来纯模式字段若在混用模式出现，建议报冲突而非猜优先级。绝不覆盖用户go.mod/go.sum。
- **通过 Go proxy 发布 hgo source（当前渠道）**：发布 module 必须符合 Go module/proxy 的 .mod、版本、路径/zip/checksum合同，含适配的go.mod及goxide.toml/hgo源码，消费方显式增强依赖边才编译。开发根可以未来无go.mod，但不能据此声称原生Go发布渠道也无需module元数据。可以由发布步骤从纯清单生成一致source发行目录；需要单独发布协议和验证，不偷偷写回根。

例子（路径是设计形状，非新增CLI）：

| 场景 | 根/发行物 | target产物与限制 |
| --- | --- | --- |
| 纯 app（未来） | goxide.toml、建议goxide.lock、src/；TOML显式module | target/.../plan/go.mod及go.sum；target/.../units/.../module/go.mod及go.sum；统一选版后构建 |
| 现有 Go app（现在） | go.mod、go.sum、goxide.toml、hgo.lock | 现有受管unit生成副本；根Go文件保持用户所有权 |
| proxy上的 hgo source（现在） | zip中go.mod＋goxide.toml＋源码，proxy的.mod与源码一致 | hgo消费方在target生成Go；原生Go不自动转译.hgo，不承诺直接可用 |
| 原生Go消费发行物 | 某固定feature配置的生成Go＋go.mod/go.sum及一致metadata | 普通Go可消费；不能再靠features重新配置已生成源码，不混装重复所有权源码 |
| 只有TOML、无go.mod的远端仓库 | 未来可经显式Git checkout或本地path取得后合成module元数据 | 不能直接当Go proxy可发现module；本轮现有path/git同样仍要求go.mod |

最小实施次序建议：先稳定本轮TOML schema与识别接口；后续确认显式模式及纯模式module/Go版本字段、锁名迁移；再做纯根到统一规划main module的适配，最后设计source/固定Go两种发布产物。不要把每unit各自选版作为简化方案，也不新增自制Go版本解析器。需新增测试：纯/混用同图等价、冲突诊断、根文件未写、锁迁移拒绝双锁、graph/feature A-B-A、Git/path纯来源与proxy需.mod的正反例。

待用户决定仅两组：是否推进显式纯项目模式并采用上述module/Go版本权威规则；是否将增强锁正式更名goxide.lock并采用旧/双锁明确迁移诊断。本轮不冻结这些新增字段，只保持内部Manifest与人类文档结构分离，为后续扩展留出[package]位置。

## 实现与独立审查

本分支已接入严格 TOML 解码、统一 Detect、所有活动来源/helper 的旧/双清单拒绝，并加入 ManifestProtocol 到计划、配置和脚本来源身份。独立审查发现库的 struct 解码大小写折叠；现用同一 TOML 库 AST 和结构体精确 tag 校验补齐，包含内联表/数组，不另写 TOML parser。独立反例与解析/迁移专项 race 已通过；生成脚本禁止输出新旧两种清单、helper bootstrap 脚本仍拒绝。完整回归结果以交付记录为准，不把文档检查等同于执行验证。

### 本轮验证记录

真实 Go 1.27.1；本地临时模块、fixture proxy 和本地 Git，无私人依赖/安装脚本。`go test -race -tags hgoweb -count=1 ./...` 完成全树：CLI 316.756s、acceptance 344.744s，其余包通过；其中 project 包唯一失败为 TOML 库升级后的错误文案断言（旧期待 cannot decode，实际 array table），修正测试后完整 `go test -race ./internal/project -count=1` 通过（1.030s），生产代码未因该断言调整。不把有此断言失败的原始全树命令描述为零退出。

额外版本来源/feature/script专项race通过（114.436s）；解析/迁移专项race通过，独立大小写反例复核通过；脚本保留清单名和helper bootstrap专项通过。全树vet、文档检查、网站/WASM构建通过。公开project示例临时副本真实运行两个bin、example、关闭默认feature的locked离线切换，输出符合README。官网源文案已同步，未部署线上站点。

格式迁移会改变精确脚本来源身份；保留项目信任并不让旧第三方脚本授权自动匹配新来源。迁移后先查看 scripts plan，按既有授权流程审阅新 subject；不由工具自动授予权限。

# A01 宏模板直接 append：有限子项交付

本片从恢复分支 `restore/backup-771d464` 的 `771d4641bf052cb676362c830179f033c6843f46`
创建独立 `feat/macro-append-builtin`，关闭 [MVP 台账](../mvp-gap-audit.md) A01 的直接
`append` builtin 子项。此前合法 `append($s,$v)` 模板会报 unsupported free template name；
现在复用既有 `len`/`cap` 卫生绑定与 marker 恢复，最终生成原生 `append` 调用。
生产改动只在 [hygiene.go](../../internal/macros/hygiene.go) 两处列表加入 `append`，
没有引入 wrapper、额外用户函数帧或新的存储操作。

先写入 [RFC0011](../rfcs/0011-macros.md) 的白例/黑例合同，再实施及验证。
代码、测试与合同提交为 `a2c7300966f3bf2c0a446ff13818bd4a8ce06c68`；
之后的交付提交仅含台账、验收记录及证据。编译器生产文件摘要为
`7f0b5bb5b5cbab65fb2db7f71ca899a3b5ced46eacebd1a70f5f1eb03c4009cc`。

## 完成范围与可复现验收

| 子项 | 行为与证据 |
| --- | --- |
| 原生 append 求值与存储 | [8 项宏测试](../../internal/macros/append_test.go) 和 [真实 CLI 验收](../../tests/acceptance/macro_append_test.go) 验证命名 slice、容量复用/扩容、重叠源、nil/空、string spread、零次/多次 repetition、嵌套捕获次数与从左到右求值 |
| 宏卫生 | 调用点局部变量/参数/常量及 import alias 不截获 builtin；模板局部函数和定义点普通函数仍按原身份绑定；跨包/跨文件真实执行，保留 field/key、range、named result 等上下文 |
| 控制流和日志组合 | `?` 仍退出最近用户函数；首实参失败跳过后续实参，后续失败不执行 append 写入；slice header 变化与原生操作数时序一致；关闭的条件 slog 不求值消息、slice 或元素 |
| 明确拒绝 | builtin 当函数值、定义点 var/const/type/import 冒认 builtin、定义点指向 builtin 时调用包级同名声明，以及覆盖调用点的同名局部 type 名称；错误 slice/element/arity/spread 在实际 Go build 拒绝 |
| 来源与确定性 | LF/CRLF、重复 Go 与 map 字节相同；builtin token 保留定义/调用栈；真实无关 cwd 的 CLI 验收覆盖生成模块 test-race/vet/run-race，输出 `[1 2 3] ab` |
| 独立审查 | [review.json](macro-append-builtin-evidence/review.json) 无阻断；额外执行 slice header/None/string spread、首实参传播、named result/range shadow、跨文件 import+晚期 marker 五类探针，并实际拒绝 nil base、溢出、string→rune、混合 spread、go append 丢弃结果 |

支持零个追加值的 `append(s)`，不额外分配或改 nil 身份；Go 1.27 的 vet appends analyzer
会提示该调用没有追加值，单元运行测试仍覆盖该语义。要求 clean vet 的验收例使用零长度 spread。
不会把 Go vet 的提示改成新的语言规则。

## 原生 Mac 验证

环境为 Mac mini、`go1.27.1 darwin/arm64`，`GOTOOLCHAIN=local`、`GOMAXPROCS=4`。
固定依赖下载至任务独立 module cache，`go mod verify` 通过，未修改 go.mod/go.sum。
std 工作分支同名并位于 `ce5bccb4ff9c7fb96b7f192a3a263051590d9288`；
serde 保持 `migration/core-json@a1b791e96d660c6be620e110c566a9a171631b7e`，两库均无源码变化。

整树 `go test -race -count=1 -timeout 30m -json ./...` 完成 535 项顶层测试：
534 项通过，唯一失败为 `TestProjectConditionalGoxideAcrossTargets`，退出码 1。
它的离线规划执行 `go mod download -json all`，初次普通 `go mod download` 未缓存完整图，
缺少四个已在 go.sum 固定的依赖源码 zip；错误 JSON 在 stdout，现有错误包装只显示空 stderr。
这与历史 [审计记录](mvp-gap-audit-20261005.md) 的同一用例/原因一致。
补齐任务 module cache 的 `go mod download all` 并通过 `go mod verify` 后，
该用例以 `-race -count=3` 连续三次通过，未改任何生产或测试代码。

因此 535 项不同顶层测试（CLI 103、acceptance 150）、17 个有测试包的覆盖均已闭环，
5 个包没有测试，没有跳过的测试或未解决失败；**这不是一次退出 0 的整树命令**。
已通过的 534 项未重复运行。失败、缓存诊断与补验分别保存在
[整树摘要](macro-append-builtin-evidence/full-race-summary.json)、
[原整树结果](macro-append-builtin-evidence/canonical-attempt-summary.json)、
[离线 JSON 诊断](macro-append-builtin-evidence/offline-dependency-probe.log)、
[修复前连续三次失败](macro-append-builtin-evidence/conditional-targets-retry.log) 及
[补缓存后三次通过](macro-append-builtin-evidence/conditional-targets-fixed.jsonl)。
原始整树 JSON 保留在本机任务 validation 目录，提交摘要记录其字节数和 SHA256。
两份人读日志仅展开 tab/去行尾空白以满足仓库检查，原始及展示文件哈希见
[日志展示 provenance](macro-append-builtin-evidence/log-display-provenance.json)；原始日志保留。


完整 `go vet ./...` 通过，见 [vet.log](macro-append-builtin-evidence/vet.log)。
[完整设计检查](macro-append-builtin-evidence/design.log) 的文档结构及 13 个 Go 机制命令通过；
这些机制探针不代替 `.gox → Go → 执行`。
[最终文档检查](macro-append-builtin-evidence/docs.log) 单独检查交付文档。
[宏包 race](macro-append-builtin-evidence/macros-race.log) 与
[CLI acceptance race](macro-append-builtin-evidence/acceptance-cli-race.log) 均通过；
[改动前红例](macro-append-builtin-evidence/macros-before.log) 保留真实缺口。

首次整树回归使用 macOS 默认 TMPDIR，`/var` 指向 `/private/var`，触发既有真实目录
安全检查及 canonical 来源路径不匹配；该次也包含上述缓存缺失，未计作通过。
[首轮摘要](macro-append-builtin-evidence/initial-full-race-summary.json) 保留结果和原始日志哈希。
改用任务内真实目录 TMPDIR 后，相关 scripttrust/buildscript/project/use 定向回归通过，
没有修改测试、跳过用例或放宽路径安全检查。

## CLI/WASM 与网站本地组合

独立验证 checkout 使用本片代码，website 来源 overlay 为
`6dc779a651af3eb34121f53637a55bfb6feee387`，QA/fixtures 保留本片提交。
catalog compilerCommit/digest 仅在验证 checkout 绑定；这是本地组合，不能据 manifest
中的 checkout HEAD 推断 website 全部源码与该 HEAD 相同。
[源码身份](macro-append-builtin-evidence/web-source-identity.json)、
[WASM 身份](macro-append-builtin-evidence/local-wasm-identity.json) 和
[完整 web 摘要](macro-append-builtin-evidence/web-validation-summary.json) 可核对。

| 检查 | 结果 |
| --- | --- |
| 构建与 web race/vet | Go WASM 构建通过；`goxideweb` 两包 race/vet 通过；WASM SHA256 `8c698061e8d48eb0247df23e52efee4df7d34e040ed473d59fb2b701c4087dfe` |
| 既有差分 | 10 个示例、9 个 Go-result/legacy 检查、5 个诊断/importer 检查，CLI/WASM 逐字节一致 |
| 新增 append 矩阵 | 5 个正例生成 Go 一致；6 个绑定错误诊断一致且清空输出/禁用复制/后续恢复；4 个普通 Go 类型错误先比较生成 Go 一致，再由原生 go build 实际拒绝 |
| UI 与 catalog | 桌面/390px、超时/取消/恢复/复制、无外部请求；26 卡片、10 负例、过滤/链接/快照、损坏/陈旧 WASM 与 manifest 失败态均通过；JS/console 错误为零 |

单文件 CLI transpile 与 WASM 保持 lowering 边界，**浏览器没有执行最终 Go 类型检查**。
上述四个类型黑例不是浏览器编译拒绝证据；QA 明确分类避免混淆。

Mac 的 Homebrew Go 在 GOROOT 下缺 LICENSE，原构建脚本第一次失败，
[日志](macro-append-builtin-evidence/wasm-build-before-homebrew.log) 保留。
仅在独立验证 checkout 改一行，从同版本 Cellar 根目录复制真实 LICENSE，
[适配 patch](macro-append-builtin-evidence/homebrew-license-validation.patch) 与文件哈希随证据保存。
不能宣称原脚本未经改动就在该 Mac 上构建通过。生产 build.py/catalog 未修改，未部署。
使用已有 Node 26.9.0、固定 Playwright 1.58.2 本地 npm 依赖及现有 Chromium 145.0.7632.6；
没有安装浏览器或更改系统工具。验证服务已关闭。

## 仍开放的工作

A01 只完成直接 append 子项；make/new/copy、调用包级同名绑定、覆盖调用点的局部 type 名称、
imported type/var/generic helper 的剩余合法需求继续为工程账，不能把拒绝测试当作永久范围。
A02 的完整模式和 tracing API 没有借本片扩张。RFC0011、A01 整项和 MVP 均未关闭。

A03 仍开放：有些捕获实参的 Go 类型错误指向生成的 `(`，保持 `generated/unmapped`。
[15 个旧/新对照](macro-append-builtin-evidence/diagnostic-controls.json) 验证旧基线和当前
len/cap/普通函数捕获均有同一边界，而直接原生 append 的错误两版本均有来源，故不是
本片新增退化。测试要求真实错误类别和诚实 unmapped；append() 与宏绑定错误仍严格验证
原始位置/定义/调用栈，不伪造精度。

本轮只报告 Mac 原生执行，Linux 构建/执行没有重跑。未实施完整 trait/borrow checker、
serde adapters、blank/embedded 字段或公开 enum 入口的待定规则；恢复分支、main 和旧
hyper-go 工作目录保持原状。普通 feature 分支推送不等于合并、打 tag、发布或部署。

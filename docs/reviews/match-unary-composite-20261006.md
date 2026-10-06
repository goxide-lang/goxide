# A04 unary/composite 单值 match（2026-10-06）

本批接通 `return -(match ...)`、`S{N: match ...}` 以及普通 slice/array/map
字面量内的单值 match。真实 CLI 验收串联 JSON → closed enum → match 装配业务对象
→ Clone 并修改副本 → Debug → JSON，检查原对象不变、nil/empty 与错误 cause/path。
这只关闭 A04 的标量 unary/普通 composite 子集，未宣布 A04 或 MVP 整项完成。

## 提交身份

| 身份 | 准确提交 / 值 |
| --- | --- |
| 工作基线 | `2cd3b71e4cdeff0fe8ea9ae0a2f460ac52ee416d` |
| 实施前 RFC/验收合同 | `f1da21c21fc9456e257120fefd253f65a5e35d4d` |
| 首次实现，独立审查发现函数类型缺陷 | `ee5949d80c11b513a40700f8ec03dd7050a77876` |
| 最终编译器生产 pin | `f2d7a39c4b81a00fff191c89ac847b09af6a62e9` |
| 123 个 compiler inputs 的 digest | `aeaaa2ca69563c064b47ecb757415e1a8478b771248e79144cecf9d9b79bc1e8` |
| 正式验证的网站/工具提交 | `0d814dda56009af89ab60e77fcf77ef32d3a63bd` |
| 正式验证 tree | `f9fc7584e7fa35af250f38160a6083ae8a95b546` |
| 网站旧编译器对照 | `fd8380c39876c96441857a737be8e32c322a0467` |
| 分支 | `feat/match-unary-composite` |

本报告、证据与交付导航在上述网站/工具提交之后追加，是文档/证据提交。
最终报告提交 SHA 由 Git 和交付回执记录；不将它冒称为运行整套验证时的 HEAD，
也不将它回填 compiler pin 形成自引用。网站 manifest 分别记录 compiler 与 website commit。
完整门槛在 `0d814dd` 的干净独立检出实际执行，生产 inventory 与 `f2d7a39` 一致。

## 实现和语义

支持标量 `+ - ! ^`、显式 struct/slice/array/map 字面量及 Go 允许的嵌套类型省略、
隐式指针元素。它们可进入已有的局部 initializer、单目标赋值、单结果 return、
调用参数和表达式 arm；struct 字段名、array/slice 常量 index 与 map key/value 保留各自角色。

类型从真实 `go/types` 字段、元素、key/value 或调用签名取得。名义类型、alias、
局部同名遮蔽、uint64 shift 与 typed nil 保留精确身份；不以 any 或数值 widening 接受错误。
先安装已证明的外层类型，再解析内部 pending match，最终仍检查所有 arm，包括未选中的 arm。
pending 标记按实际编译器类型身份识别，合法用户类型名中的同名子串不会被误拒。

表达式继续降为原用户函数中的 prelude/value。scrutinee、guard、选中 arm 和调用/receive
按既定规则求值；短路右侧留在原条件分支，`?` 返回最近的真实用户函数并跳过后续元素。
真实闭包先规范化，再组合到外层，分别保留函数归属和来源；不留下改变控制流的运行时 IIFE。
命名返回槽、用户 defer、panic/direct recover、goto、break/continue 与原有索引 place
规则均有实际运行断言。不额外承诺 Go 未规定的普通读取与非调用 panic 的全序。

函数类型与真实闭包使用完整类型/签名 AST 区分。`[]*func() int`、`[](func() int)`、
`map[string]*func() int`、`map[*func() int]int`、`[1]*func() int` 都有原红/修后绿记录；
完整 literal 在二元拆分前识别，类型里的 `*` 不再被误当乘法。表达式中的真实 FuncLit
仍保留函数边界，包括返回函数、closure-first/last 和内外不同 `?` 返回类型。

## 验证结果

环境为 Go 1.27.1 darwin/arm64、Python 3.14.7、Node 26.9.0、Playwright 1.58.2、
Chromium 145.0.7632.6。使用已安装工具和固定依赖副本，没有安装或下载。

| 门槛 | 结果 |
| --- | --- |
| 完整 Mac race | 605 顶层/1338 子测试通过，0 失败、0 测试跳过；18 个测试包通过，另 5 包无测试文件 |
| 全树 vet | 退出 0 |
| 完整设计检查 | 124 Markdown、1375 本地链接、26 RFC、13 个 Go 机制命令通过 |
| 仓内网站入口 | 七阶段全部通过，退出 0，`passed: true` |
| 旧网站输入 | 97 fixture、10 示例原字节保持；追加后共 105 fixture、11 示例 |
| 旧/新 Go 输出 | 33 对：27 字节相同、6 排版差异保留；33 对完整 AST 相同 |
| 旧/新原生行为 | 5 对程序实际运行，stdout/stderr 相同；不宣称运行了全部 33 对 |
| 旧来源诊断 | 13 例，52 CLI / 26 UI LF / 52 raw Worker LF/CRLF 观察保留 |
| 新 A04 差分 | 8 fixture：4 正例、4前端拒绝；32 CLI / 16 UI LF / 32 raw Worker 观察，每次后恢复 |
| 新原生运行 | 4 个正例及业务示例共 5 个 current-only 程序，退出/输出逐项符合独立预期 |
| catalog / 浏览器 | 26 卡片、10 负例、过滤/链接/复制、390px、XSS、超时/中断/重置、无外部请求通过；errors 为空 |

完整 race 内的 `TestMatchUnaryCompositeCLI` 包含 21 个子测试：生成模块、16 个真实类型/
权限负例和 4 个明确的一元实现边界。生成模块实际执行 8 个 runtime tests、race/vet/run
及重复生成字节比较；旧 literal 拒绝场景迁为正向运行，没有删除场景。
此前候选的 34 个定向子测试保留为工作树阶段历史；随后核对了与 `ee5949d` 的文件
快照，但冻结后没有按该提交重跑，不能视为准确提交执行证据。最终完整结果以
`0d814dd` 的上述门槛为准。

见[完整门槛](match-unary-composite-evidence/testdata/full-gates-summary.json)、
[网站摘要](match-unary-composite-evidence/testdata/website/summary.json)、
[浏览器结果](match-unary-composite-evidence/testdata/website/qa-result.json)、
[新原生运行](match-unary-composite-evidence/testdata/website/native/a04/runtime.json)。
CLI/WASM 比较同一当前编译器的输出字节；旧/新 AST 对照是独立证据。
UI textarea 规范化换行，因此 UI 只计 LF；CRLF 由真实 Worker 覆盖。
最终 Go 类型错误在 native 检查，不宣称 WASM 运行了最终 Go 编译或业务程序。

WASM 为 16,349,088 字节，SHA-256 `6ca9c3c7a36788fac832082bc034afb1f5193d9b1d2aabd3f52e178f7808abfb`；
export digest `64cabea20fc79e847825506245fcc0c4d9649762d873908804c29fbad8a89582`。
公开库版本与 ABI enum/reference/use/sum = 8/3/2/2 保持；std `ce5bccb4`、serde `a1b791e`。
准确完整值见[身份](match-unary-composite-evidence/testdata/website/identity.json)和
[命名 audit](match-unary-composite-evidence/testdata/naming-audit.json)。

## 独立审查和失败历史

编译器黑盒审查、来源/atom 审查及网站工具审查均无剩余阻断。
编译器审查绑定全部 123 个 canonical inputs，另单独绑定网站服务 `cmd/goxide-web/main.go`，
不把这个辅助文件算入 compiler digest。最终增量实际执行 6 个函数类型模块、2 个闭包
模块和一个含 7 项 flow tests 的模块，生成模块 race 全通过。初始类型/顺序探针仍按其
真实候选版本标注；不同单位和重复运行不相加成一个夸大的测试总数。

来源审查从准确 `f2d7a39` archive 独立构建，30 项断言通过。LF/CRLF 各检查 105 个有宏栈
token 的用户函数归属，8 个精确原 token 的字节与 26 个合成标识符的 nil Origin。
未知来源仍标 intermediate/unmapped；不声称所有后端节点恢复了精确原坐标。
网站独立审查绑定准确提交的 19 个文件，核对旧输入、10 项 runner 测试、16 项 support
测试、JS 语法及另外 5 个实际原生程序；没有把小型审查当完整网站运行。

基线 `2cd3b71` 在新业务 literal/unary 入口实际失败。开发及独立审查发现并修复：真实
内层闭包与外层 match 组合丢失、已知外层类型对 pending 内层解析过早、合法用户类型名
子串被误判，以及函数类型/真实函数边界和类型星号的两处识别缺陷。`ee5949d` 和中间 v3
函数类型红记录保留；未将它们记成通过。

夹具修正另列：既有属性语法、tuple return 前先算局部值、局部值再登记 defer、
避免源程序自带 unreachable vet 错误、正确的不可寻址 receiver 与 readonly 类型控制。
这些修正不算功能缺陷修复证据。初始 missing module/GOROOT 等 harness 错误也不计 feature 红测。
详见[验收记录](match-unary-composite-evidence/testdata/acceptance/evidence.json)、
[编译器审查](match-unary-composite-evidence/testdata/review-core/review.json)、
[来源审查](match-unary-composite-evidence/testdata/review-origin/review.json)和
[网站审查](match-unary-composite-evidence/testdata/website-independent-review.json)。

## 干净检出与复现

正式根目录 `/private/tmp/goxide-a04-20261006-ukafjzho`；从独立 clone 建立 `final-checkout`，
无 Git object alternates。新建 Go build cache 初始为空，Go modules 和 Playwright 是明确的
已有固定依赖副本，没有逃出副本的符号链接，`go mod verify` 通过。
source/current/baseline 分别核对全部 1206/1206/1042 个 tracked inputs，状态和字节均未改变。
243 条仓内 runner 命令的 argv/cwd 没有任务目录引用。此为身份、命令和输入清单证明，
不是操作系统文件访问追踪；现有工具及缓存仍是声明的环境前提。

短入口见 [website/validation/README](../../website/validation/README.md)：

```sh
env -u GOWORK GOTOOLCHAIN=local GOPROXY=off go test -race -count=1 -timeout 30m ./...
env -u GOWORK GOTOOLCHAIN=local GOPROXY=off go vet ./...
python3 -B scripts/check_design.py
python3 -B website/validation/run.py --out /absolute/new-output \
  --chromium /absolute/existing/chromium \
  --playwright-node-modules /absolute/existing/node_modules
```

须从干净已提交检出运行，具备 README 列出的工具、完整固定模块缓存和可写的真实临时路径。
全树检查不导出 `GOWORK=off`；网站 runner 自行隔离其工作区环境。原始命令、时间、退出码、
完整 race 日志 hash 与输入证明见[门槛摘要](match-unary-composite-evidence/testdata/full-gates-summary.json)、
[输入证明](match-unary-composite-evidence/testdata/input-integrity-proof.json)与
[原样发布清单](match-unary-composite-evidence/testdata/publication-provenance.json)。

## 仍开放的范围

`* & <-` 等引用/收发 unary、`&S{N:match ...}`、新 postfix/index/receiver 组合、
go/defer/control 头、package initializer、多结果、复杂 LHS、block arm、完全不可命名类型、
builtin/type-conversion call 仍是工程欠账，不是语言禁令或自动获准延期。
实际 defer 验收先求局部 literal 再登记 defer；direct recover 在用户 deferred closure 的
函数体内使用局部值，没有宣称接通头内 literal 或新的 postfix 入口。
tuple/struct variant、原位 mut& ABI、blank/embedded、手写协议登记及 Proposed adapters
没有在本片新定语义。无需新的用户语义决策即可完成本片修复。

没有部署网站、修改公开库 ABI、合并 main 或创建 tag。原编译器 checkout 及其他四库保持原状。
只执行 Mac 本地验证；未运行 Linux 或托管 CI，不将不存在的 CI 记录称为通过。

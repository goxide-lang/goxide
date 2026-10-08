# 官网 alias 与显式 std 校验交付（2026-10-08）

本批从实际远端 main `eb6ddf0ce026251b4e9b791d0271ee05b9438749` 创建
`fix/website-semantic-cleanups-20261008`，仅在 goxide-dev Linux 云环境工作。
两项改进不引入新语法、公开 API 删除或 schema 变更。

- **无冲突 alias 精简**：宏依据真实 Universe 对象和词法作用域恢复原类型拼写；局部遮蔽、未知绑定继续保留定义点 alias，捕获和包级/import 冲突拒绝不变。派生器统一分配 fresh 局部名，保留原文件中的容器类型表达式；named/import shadow、嵌套容器、nil/empty、普通赋值共享和显式 Clone 独立存储均覆盖。类型身份及 method set 不变。
- **显式 std 具体登记**：限定 Option/Result 构造、match、`?` 和 Go constructor 推导复用 canonical 身份，生成完整的有限载荷 validator。包括原生 companion、传递包装包、跨独立 module、use/现有透明 alias。恢复依旧核验 artifact/源码/依赖/签名，并用当前 importer 的真实类型对象授权；绑定使用 canonical origin，避免意外执行无关 facade 的 init。

审查中确实发现并修复 native/transitive enum 载荷漏检、跨 importer 身份失败和 facade init 副作用。
私有字段、internal 来源、篡改 artifact、pointer/embedded/blank/递归图边界继续明确拒绝。
已有 Option/Result/Goxide* Go 兼容 API 和 std/serde 锁定依赖未变。

## 准确源码与资产

- 最终生产源码 pin：`dd6031e7572a557b9d01305bacb64fd81eb719b2`。
- 生产摘要：`15de732b6cebac4fba64b65ff2138c40670a06e4e6210d099ffc732d67625c86`。
- 最终网站/fixture 验证提交：`25894641372ff44e67aad86f27492469769de33f`。
- 合同断言修正提交：`357950768cff3872dd234f888715cffb4073974a`，仅测试变化；之后本报告与验收账为文档提交。
- 后续完整整树 race 的源码/测试提交：`1cf209b8b646bfa44c3cca0c8c913a95054e0592`；本次报告补记是其仅文档后继，源码与测试相同，运行验证仍归属该准确 SHA。
- std `ce5bccb4ff9c7fb96b7f192a3a263051590d9288`；serde `a1b791e96d660c6be620e110c566a9a171631b7e`；无本机 replace。
- ABI 保持 enum 8、reference 3、use 2、sum 2。每个 Go shell 按实际 start.md 激活 Go 1.27.1；原生 GOMAXPROCS=4，只有 Node GoWASM 子进程为 1。

## 本次验证

| 检查 | 结果与范围 |
| --- | --- |
| Focused compiler / generated modules | 宏、derive、显式 std 与相关回归 race/vet 通过；独立审查的 native/transitive、artifact 和 init 探针通过 |
| 首轮整树 race（历史） | exit 1：645 顶层及 1638 子测试通过，1 项过期合同断言失败；17 个包通过、1 个包失败、5 个包无测试。修复后的 5 项 qualified acceptance race 与包 vet 通过，详情见下文 |
| 后续最终整树 race | 准确 `1cf209b` 一次完整运行 exit 0，耗时 729 秒（729.199 秒）：18 个有测试包、646 顶层及 1638 子测试全部 PASS，FAIL=0、测试 SKIP=0；另 5 个包无测试文件 |
| vet / web | `go vet -mod=readonly -tags goxideweb ./...` 通过；最终构建后 `go test -mod=readonly -race -tags goxideweb -count=1 ./cmd/goxide-web` 通过 |
| 设计门禁 | `python3 -B scripts/check_design.py` 通过：生产源码门禁为 131 Markdown、26 RFC、13 条 Go 机制命令；最终报告另跑 docs-only，机制证据与编译器测试分开 |
| 网站构建 | `website/build.py` 通过，11 原网站例子逐字节保留并真实原生执行；共享 std/serde、export data、WASM、manifest 源码摘要核验通过 |
| 当前 CLI/WASM/Worker | 11 原例 + 7 新输入，36 组 LF/CRLF CLI/Node GoWASM 字节或诊断完全一致；真实 HTTP Worker 36 组一致；16 个成功输入执行原生程序并核对独立 stdout |
| Chromium | 151.0.7922.173 / Python Playwright 1.62.0，`chromium_sandbox=True`；获准上下文运行，仅回环 HTTP 请求 |

首轮整树命令为 `env -u GOWORK go test -mod=readonly -race -count=1 -timeout 30m -json ./...`，
在 `2c1df02` 启动，生产源码与最终 pin 相同。
唯一失败 `TestQualifiedPackagesRequireConcreteValidation` 仍要求 producer 没有
`Result[string,error]` 登记就拒绝；它与本次消费端可信 canonical 实例补齐合同冲突。
修正为 `TestQualifiedPackagesGenerateConcreteValidation`，验证新 string 实例的成功/错误传播，
以及 nil、typed-nil、case pointer、伪造 embedding 在任何 match 后副作用前按准确 panic 拒绝。
错误类型身份和未登记家族的邻接负例保留。独立只读审查认可此合同迁移。
随后 `go test -mod=readonly -race -count=1 ./tests/acceptance -run '^TestQualifiedPackages'`
通过（5 项，含生成模块 race/vet），`go vet -mod=readonly ./tests/acceptance` 通过。
当时尚未再次运行整树，未把这一组合证据描述为一次整树全绿。

随后按主线合入前要求，在干净的 `1cf209b8b646bfa44c3cca0c8c913a95054e0592`
上实际完成一次完整整树 race，2026-10-08 07:37:15–07:49:24 UTC，结果见表。
实际命令为 `env -u GOWORK "$GO_BIN" test -mod=readonly -race -count=1 -timeout 30m -json ./...`，
其中 `GO_BIN=/workspace/.cloud-setup/toolchains/go1.27.1/bin/go`。
运行前后 HEAD 相同、工作树均干净，修正后的 5 项 qualified acceptance 也在该次整树中全部通过。
复核确认合同更新仍要求真实 canonical 身份与完整 payload validator，保留非法值及副作用次序检查。
此后仅补记本报告，未改源码或测试、未重跑测试或已通过的网页矩阵；此前失败与修复历史保留。

当前验收入口和失败保留规则见 [验证说明](../../website/validation/README.md)，
用例为 [semantic-cleanups-cases.json](../../website/validation/semantic-cleanups-cases.json)。
原 macro、macro_repeat、equality、match_composition 生成 Go 的 primitive/derive 多余 alias 均为 0，stdout 保持预期。

首轮 focused-v1 真实失败：新增 map Debug 用例要求未打包的 `sort`，CLI 成功而 WASM 拒绝；HTTP 层未运行。
本批不扩浏览器包集合，改用 nested slice 检查同一 fresh-binding 问题；原生 map 回归保留。
focused-v2 所有层通过。两轮原始输入、输出、失败和 manifest/hash 记录分别保留，未覆盖旧证据。
WASM SHA-256 为 `66aebdc85c10ab86c738f8e07c69fb6f9562687fc4a7b1cf84563db5d0ef2283`。
在激活上述 Go 后，本片网站检查可在干净检出重放：

```sh
python3 -B website/build.py
python3 -B website/validation/focused.py \
  --out /absolute/new-focused-output \
  --go "$GO_BIN" --node node --chromium /usr/bin/chromium \
  --cases website/validation/semantic-cleanups-cases.json
```

云端证据根目录为 `/workspace/.cloud-setup/semantic-cleanups-20261008/`：
`focused-v1/summary.json`、`focused-v2/summary.json`、`logs/full-race.jsonl`、
`logs/qualified-repair-race.txt`、`logs/qualified-repair-vet.txt`、`logs/vet-tagged.txt`、
`logs/web-race-v2.txt`、`logs/design-final.txt` 和 `logs/design-report.txt`。
后续最终整树证据为该根目录 `final-race-1cf209b/` 下的 `summary.json`、`identity.json`、
`events.jsonl`、`stderr.txt` 和 `remote-refs.txt`；summary 记录命令、准确身份、结果及证据摘要。
独立语义探针见仓外 `/workspace/.cloud-setup/semantic-cleanups-review/REVIEW.md`；
这些运行证据未加入 Git。

## 边界与整合

- 宏 + Eq 派生组合存在基线已有 A03 展开栈缺失，本片未修复。后续在准确 main `eb6ddf0` 与最终 `1cf209b` 用同一 overlay 探针复验：`PartialEq/Eq` 与 `zero!() → int(0)` 同文件时，两边均编译成功，但生成调用位置的宏定义/调用栈均为空；这证明该场景不是本批引入。探针 PASS 表示取证成功，不表示缺陷修复，影响仍是诊断追溯。证据见仓外 `/workspace/.cloud-setup/a03-exact-provenance-20261008/report.md`。普通 struct DSL 后置路径和 LF/CRLF 模板 token/栈回归通过。
- 未运行完整网站 UI/历史七阶段 AST 等价矩阵；alias 精简有意改变生成 AST，旧 oracle 和冻结矩阵原样保留，可在旧准确提交复现。此次不宣称 Mac 或托管 CI 通过。
- 无自动 family reexport、新手写 generic alias、新派生协议、runtime 迁移、多文件浏览器 UI、限定 derive 或部署。
- 原始日志/资产/探针均在忽略目录或 `/workspace/.cloud-setup/semantic-cleanups-20261008/` 等仓外目录；仓内只有源码、测试和本短报告。
- 建议父协调合入整个已验证功能分支，保留源码 pin 对象与网站绑定提交的关系；main 合入仍待批准，本批仅推送功能分支，没有更新 main、force push、tag 或发布网站。

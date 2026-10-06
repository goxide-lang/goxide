# A01 宏模板直接 copy：有限子项交付

基线 `1cf4b58f628bca028cda12ba9b7cde651b644060` 在 Mac mini 上重新执行完整
`go test -race -count=1 -timeout 30m -json ./...`，退出 0：535 项顶层、1117 项子测试，
17 个有测试包通过，5 个无测试包，没有失败或跳过测试。上轮唯一离线缓存失败
`TestProjectConditionalGoxideAcrossTargets` 本次在整树内通过（13.4 秒）。
[基线摘要](macro-copy-builtin-evidence/baseline-full-race-summary.json) 保存准确提交、
逐包结果及原始日志哈希；原来的失败记录不删除、不改写为成功。

随后从此基线创建 `feat/macro-copy-builtin`，先在 [RFC0011](../rfcs/0011-macros.md)
写白例/黑例合同，再接通 A01 的直接 `copy($dst,$src)`。之前该合法模板报
unsupported free template name；现在复用既有 len/cap/append 的卫生绑定与 marker，
生成原生 Go copy。只关闭 copy 子项，不关闭 A01、A03、RFC0011 或 MVP。

代码提交：`49c428767de35259ce66d4e50887725f2fc0b85f`。生产摘要：`45aec417fcd3766749633def2fa92c8ad69ea660a224086f1c70afb8eb6c56e8`。
交付记录后续提交只改文档和证据。std 位于同名工作分支 `ce5bccb`，serde 保持
`migration/core-json@a1b791e`；没有改变公开依赖版本、main、恢复分支或旧 hyper-go。

## 原生行为与权限验收

| 合同 | 验证内容 |
| --- | --- |
| 复制计数与存储 | 目标短/长、nil/空、命名 slice、重叠与 string 来源；返回实际复制数，长度/容量和底层存储不变；普通指针元素保持浅复制的共享对象身份 |
| 求值与控制流 | 两侧函数操作数按原生次序求值，模板重复捕获保次数；`?` 返回最近用户函数，失败无复制并跳过后续操作数；条件关闭不求值；defer 参数先求值、复制在返回时执行 |
| 绑定与来源 | 模板局部/定义点真实函数优先，调用点局部/import 不截获 builtin；跨文件/包、LF/CRLF、重复 Go/map，黑例保定义/调用诊断 |
| 既有权限 | 只读标量来源可复制到独立目标；拒绝只读目标及只读指针/容器元素权限逃逸；没有新增借用分析或把复制改为 Clone |
| 原生类型黑例 | 非 slice 目标、不同元素类型、string→rune、错误 arity/ellipsis 均经真实 Go build 拒绝；transpile/WASM 的 lowering 边界保持 |

[宏单元](../../internal/macros/copy_test.go)新增 8 项，先红后绿，宏包 48 项顶层 race 通过。
[真实 CLI 验收](../../tests/acceptance/macro_copy_test.go)从无关 cwd 执行，包含 6 组生成模块
运行断言、三类只读拒绝、15 个类型/卫生黑例和 LF/CRLF；生成模块分别 test-race/vet/run-race，
输出 `2 [4 5 0] 2 ab`。原有 unsupported copy 断言迁到仍未开放的 clear，拒绝类别没有删除。
[红例](macro-copy-builtin-evidence/copy-macros-before.log)、
[宏 race](macro-copy-builtin-evidence/copy-macros-race.log)、
[CLI race](macro-copy-builtin-evidence/copy-acceptance-cli-race.log)保留。

## 验证与独立审查

对上述准确代码提交执行一次完整整树 race，退出 0：**544 项顶层测试、
1141 项子测试，17 个有测试包全部通过**，5 个无测试包，无失败或跳过测试。
[最终整树摘要](macro-copy-builtin-evidence/copy-full-race-summary.json)保存逐包结果、完整命令、
环境和原始 JSON 的 SHA256；原始大日志保留在本机任务 validation 目录。
本轮两次整树均使用真实任务 TMPDIR 及已经补齐的固定依赖缓存，没有修改安全检查或测试条件。

[全树 vet](macro-copy-builtin-evidence/copy-vet.log)退出 0；
[完整设计检查](macro-copy-builtin-evidence/copy-design.log)文档及 13 个 Go 机制命令通过，
机制探针不替代 Goxide 真实执行；[最终文档检查](macro-copy-builtin-evidence/docs.log)单独保存。
人读日志仅规范 tab/行尾空白，原始和展示哈希见
[日志 provenance](macro-copy-builtin-evidence/log-display-provenance.json)。

[独立审查](macro-copy-builtin-evidence/independent-review.json)绑定同一代码提交，无阻断。
额外运行 8 对普通 copy/宏调用的权限对照（16 个探针），五对拒绝、三对接受，结果逐对一致；
额外真实 CLI→Go race 执行验证只读标量/值数组、浅指针/嵌套 slice 共享、defer 描述符与内容
时序、dst?/src? 四组合及局部 copy 遮蔽。审查未重复父任务整树或浏览器检查。
A03 合成括号和 refs 中间坐标边界按现状明确保留。

## CLI/WASM 本地组合与 CI

[WASM/browser 汇总](macro-copy-builtin-evidence/web-validation-summary.json)：
新增 19 项 copy 差分（6 正例、8 前端绑定/权限拒绝、5 原生类型拒绝）通过；
原有 10 示例、14 诊断/适配检查及 15 append 检查继续通过。
前端拒绝逐字节比较诊断、清空旧输出、禁用复制并验证后续恢复；类型黑例先比较生成 Go，
再由真实 native go build 拒绝。web race/vet、桌面/390px、超时/取消/恢复、26 卡片及
10 catalog 负例、损坏/陈旧 WASM 和 manifest 失败态均通过。

网站使用已有源码 overlay `6dc779a651af3eb34121f53637a55bfb6feee387` 与本片 QA/fixtures
在独立验证 checkout 组合，编译器身份绑定本片提交。Homebrew Go 的 LICENSE 来源适配
只在该 checkout 进行并保存 patch/哈希；不能声称生产构建脚本未经改动在本机通过。
生产 build.py/catalog 不改，未部署。浏览器执行前端降低，普通类型黑例的最终拒绝由原生
Go 编译器完成，不能混称浏览器完整类型检查。

[基线 CI 记录](macro-copy-builtin-evidence/baseline-ci-status.json)绑定准确 `1cf4b58`：
独立只读查询未找到 Actions runs、check runs 或 commit-status contexts。
本机现有 gh 的 Actions 请求为 404，connector 的 status 列表为空；没有改认证。
零 context 的聚合 pending 不能证明 CI 正在运行，故未等待假定任务。
本批最终交付提交在普通 push 后另行做准确 SHA 查询，结果随交付消息报告，不能从本地
测试通过推导 CI 通过。

本轮所有执行证据是 `go1.27.1 darwin/arm64`；Linux 未运行，本地成功不代表托管 CI 成功。
没有安装系统工具、浏览器或修改全局 Git/认证设置。普通 SSH feature 分支推送单独核验
精确 SHA，没有 merge main、tag、release 或部署。

## 仍开放

- A01：make/new、调用包级同名/覆盖展开点 local type 名称、imported type/var/generic helper；合法需求仍是工程账，拒绝测试不等于永久排除。
- A03：捕获合成括号、helper alias、合成传播节点及部分后端错误仍 generated/unmapped；本片不伪造来源精度。
- A02 完整模式/tracing API、具体派生尚未冻结入口，以及 blank/embedded、serde adapters 等设计待决项保持原账。

后续可独立评估 A03 的原子调用实参来源子片；不得全局删除捕获括号从而意外放开 expr
进入类型位置，也不把本轮扩为完整 trait 或借用检查。

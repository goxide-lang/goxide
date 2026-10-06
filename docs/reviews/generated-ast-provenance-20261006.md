# A03 AST 证据快照与生成来源（2026-10-06）

本片从 `f5b6ce658e976d4d5e951e1bb7d3ce4aaea14be1` 新建普通分支
`fix/generated-ast-provenance`。前片完整验证生产源码是
`fd8380c39876c96441857a737be8e32c322a0467`。本片准确验证源码为 `e36d23dacee64d19b34aac639900c3f5c29e55c2`；
后续交付记录提交只包含文档和证据。没有更改 main、tag 或其他仓库。

## 问题与结果

`macro_rules! m {($x:expr)=>{len($x?)};}` 中 int 不能传给 len，仍应报同一原生类型错误。
基线却在两处丢失来源：frontend 改写 builtin alias 后继续拿旧 token 串做格式化映射，
清空整份 map；lowering 只认识输入指针，忽略共享 questionFlow 创建的 payload 临时引用。
本片按 [RFC0018](../rfcs/0018-lowering-contract.md#a03-ast-变换的证据快照与生成节点2026-10-06实施合同)
先冻结证据，再通过显式单原因登记生成 token，合成点仍 `Origin=nil`。

共享 `CaptureAST` 在变换前冻结具体自有 token 的身份、kind/text、原始点及完整栈；
`Context` 返回 registry 内的明确原因，`Generated/GeneratedTree` 只登记确实新建的语法。
原输入、未知墓碑和冲突原因不会被外层填补。模板在插入旧 operand 前登记，
共享顶层 helper 不获得第一次调用的栈。旧 `Nodes/Map.AST` 入口已删除。

frontend 的 parse→ResolveBuiltins→首次打印和三个 propagation 打印入口使用同一模型。
questionFlow 的 expr/effect/frame 三种消费者共用显式 marker 原因；原 operand AST 保留。
输出按完整子字段角色与语义标量配对，原节点改名只保已证明 context，不冒领旧精确点。
连续括号链长度变化时，两侧链的括号均不映射，只有完全配对的内部节点保留证据。

输入 token 需被同一有效优先片段完整覆盖。宽片段只可保完整栈，精确点还要求 token 对齐；
局部未知或异源覆盖会使整 token 未知，空/反向区间与 Lookup 一样忽略。源点需有非空合法
结构，原文件长度与跨阶段 owner 仍由上游证据和同一次 parse 配套前提保证。
没有新增语言语法、对外来源 schema 或部分宏栈拼接。

## 验证

新增真实 CLI 验收在基线为 **13 项预期红测、8 项控制绿**；准确源码的
`TestMacroASTProvenanceCLI` 在独立 archive 以 race 执行，**21 项全部通过**。
覆盖 LF/CRLF、single/nested/sibling、多文件、跨包同 basename、enum、多 `?`、
改名后仍精确的原 token、非宏邻近控制及仍未映射的后续索引临时量。
最终 JSON map 直接检查 expr、effect 的两个 payload 和真实 guard frame 的 payload，
均保持 `Origin=nil` 与各自完整栈；有 Catch/Fail 及 private-place 的实际路由证据。

生成模块的 race/vet/run 通过，观察到 `AB 5`，并检查 Some/None、短路、嵌套/最近帧、
求值次数、失败后跳过赋值、defer 与原生除零 panic。两次生成的 Go 与 JSON map 确定。
较大跨包用例的 Go 字节和严格 token 流与基线不同：validator 调用的换行及可选尾逗号改变；
完整解析 AST 的类型、字段角色和语义标量一致，双方结构摘要相同，行为证据独立。
另有 Option/Result 两个小型程序的生成 Go 逐字节相同，双方原生 race 运行输出 `ok`。
具体源码身份、日志与哈希见 [CLI 验收证据](generated-ast-provenance-evidence/acceptance-evidence.json)、
[输出结构对照](generated-ast-provenance-evidence/ast-comparison-final.json)及
[实际输出差异](generated-ast-provenance-evidence/runtime-final-diff.txt)。

框架与接入的最终重点 race 记录 93 个测试/子测试运行，三个相关包通过，相关 vet 通过。
这只是重点检查；下面的完整 Mac 回归单独记录准确源码、测试计数和结果。

准确源码完整 `go test -race -count=1 -timeout 30m -json ./...` 退出 0：
**597 个顶层测试、1295 个子测试全部通过**；
17 个有测试包通过，5 个无测试包，无失败或跳过测试。
[完整回归摘要](generated-ast-provenance-evidence/ast-full-race-summary.json)保存准确提交、
时间、逐包结果和完整原始日志/标准错误 SHA256。测试使用正常授权的本地监听器权限。
全树 vet 与完整设计检查退出 0，后者含 13 条 Go 机制命令；设计检查不替代真实编译器验收。
完整原始日志位于任务 validation/ast-provenance-model，发布的可读日志只规范空白，
[发布哈希](generated-ast-provenance-evidence/publication-provenance.json)区分 raw 与展示文件。

准确源码的 [独立审查](generated-ast-provenance-evidence/independent-review.json)结论为无阻断。
13 个框架子项分两轮 race 验证，覆盖冻结证据、重复文字不同 owner、冲突不复活、
混合树未知、foreign handle、语义标量不匹配、括号边界及空区间；最终提交逐文件 hash 对齐。
首版有 3 项完整 token 证据反例失败，修复后全部通过，原红日志保留。

14 组旧/新黑盒对照含 4 组已修 single/nested question 与 10 组明确余项。
初次 helper-cycle 检查的两项失败来自测试要求 frontend 的 `source span` 文本，实际 build
一直采用 `[source]` 补充格式；保留初始 12 通过/2 失败结果，再以 JSON 中精确 Value 点和
原生 build 格式复核为 14 组已核实，未重写失败历史。
括号两例及 helper-cycle 的 Go 和 stderr 逐字节相同；refs 拒绝文本相同；index 仍未知，
生成错误行从 17 移至 18，不能称所有 stderr 字节不变。

AST 对照 oracle 另经 4 个语义负控（运算符、ellipsis、channel direction、type alias）及
1 个尾逗号正控核验，独立于编译器运输实现。最终字节、AST 与原生行为分别记账。

[网站验证汇总](generated-ast-provenance-evidence/web-validation-summary.json)的 7 个阶段均退出 0：
WASM/web 构建、native 对照、web race/vet、真实 browser 和 catalog。保留全部旧 83 项、
上一批 8 项合成点，加本片 5 个诊断和 1 个成功控制，共 **97 个 fixture + 10 个示例**。
13 个诊断 fixture 有 52 次原生来源预检；browser 阶段另有 52 次 CLI、26 次 UI LF、
52 次真实 Worker LF/CRLF 对照及逐次恢复。新 AST 子集为 CLI 20/UI 10/Worker 20。
浏览器/catalog 错误数组为空；26 卡片、10 负例、390px、键盘、XSS、超时/中断和
错误后恢复均通过，本地服务已关闭。

UI 会规范化换行，因此 CRLF 只归 raw Worker；单文件网站不冒称跨包或 effect/frame 覆盖。
原有 17 项 native-only 类型错误由真实 Go 编译拒绝，WASM 只做相同转译结果对照。
当前版本 CLI/WASM 输出逐字节一致，与旧/新编译器比较是两种不同证据。

旧/新 33 项生成 Go：**27 项字节相同、6 项排版不同，33 项完整 AST 相同**。
首次严格字节检查在 result 示例失败，原日志和两侧原始 Go 保留；随后逐项核验结构，
未把失败重写为字节通过。差异限于 result/go_result 和四个 question 用例的换行及尾逗号。
5 组双版本原生运行均退出 0 且输出相同，包括首次差异的 result 示例；不声称 33 项都运行。
见[逐项 Go 对照](generated-ast-provenance-evidence/successful-go-parity.json)、
[原生行为对照](generated-ast-provenance-evidence/native-runtime-parity.json)和
[首次严格检查日志](generated-ast-provenance-evidence/web-native-initial.log)。

编译器源摘要 `41f51dffc2aa68535a7617824231065d268cab190adeb7e164e239e3b7022848`；
WASM 为 16,298,510 bytes，SHA256 `b96b5373ef081372565f8f3442707fa47f61e2641ee7e29658f3352f07ee9276`。
隔离基础 website overlay 为 `6dc779a651af3eb34121f53637a55bfb6feee387`，QA/fixture来自本片源码。
既有 Playwright 1.58.2、Chromium 145.0.7632.6；未安装工具。
构建仅在隔离副本把 Homebrew Go LICENSE 路径改为 `/opt/homebrew/Cellar/go/1.27.1/LICENSE`，
原/后 build.py 和 LICENSE 哈希均记录于[构建身份](generated-ast-provenance-evidence/web-source-identity.json)；
不宣称生产 build.py 原样通过。旧 CLI 的 site-only dirty overlay 同样披露，
编译器源码与准确 git 对象单独核验；生产 build/catalog/static 未改，网站未部署。

## 可证明边界与剩余工作

以下是具体后续验收场景，不是永久拒绝清单。E 为已有合同下的工程补齐；D→E
需要先确定新的语法/语义。已完成的固定类型 make/new 不再列为未做。

| 项目 | 可复现/可执行的收尾场景 | 后续完成要求与证据边界 |
| --- | --- | --- |
| A01 定义点 named type（E） | 定义 `type Count int` 和 `macro_rules! zero {()=>{new(Count)};}`，调用函数另有 `type Count string`；跨包再换成 `new(model.Count)` | 当前固定类型门禁拒绝；以后必须返回定义点 `*Count`，保持可见性、类型身份及用户反射名称，不能按同名字符串绑定。固定 Go 泛型实例身份另列，不能顺带开放新 `.gox` 泛型声明 |
| A01 builtin 名称被调用点 type 遮蔽（E） | 模板 `len($x)`，调用处有 `type len int`；或调用包定义同名 var/func/type | 当前明确边界；后续须区分定义点 builtin 和调用点绑定，不改名用户 named type、不引入改变控制帧的 wrapper。调用处 `type int string` 下模板 `new(int)` 已有 alias 验证，不能混作未做 |
| A01 其他类型槽的 expr capture（E） | `macro_rules! zero {($T:expr)=>{func()$T{var v $T;return v}()};}` 再调用 `zero!(int)` | 既有类别泄漏，未来应按既有拒绝合同准确拒绝。不能误拒完整表达式捕获 `id!(make([]Local,2))`，或数组长度表达式中的类型。参数、结果、var、复合字面量、断言等槽需逐项补齐，尚未穷尽 |
| A01 原生 `new(expr)`（E） | `boxed!(next())` 展开为 `new($v)`，对照 `boxed!(int)` | 原生 Go 1.27 值形式合法，本片宏类型形式仍拒绝。后续需证明值/类型真实绑定、一次求值、named 类型、浅共享、typed nil 和 ? 最近用户帧；不能把 expr 捕获顺带赋予类型资格 |
| A01 一般 type fragment（D→E） | 候选 `$T:type` 后在 `new($T)` 使用 | 先定义语法、绑定点、泛型/限定名与 repetition 规则；不能拿 `$T:expr/ident/tt` 或现有漏洞绕过设计 |
| A03 下游传播 AST：共享 questionFlow（本片有界交付，其他 temp 仍 E） | `len($x?)`、binary/call 参数的 payload；expr、effect、guard frame 三类调用者 | frontend alias 改名与三个 propagation 打印边界统一快照；questionFlow 的明确新 token 保完整链、Origin=nil，operand 保自身身份。`($x?)[0]` 错误落在后续 freezeValue temp，仍无点/无栈；不为全部临时量按父 Pos 归属 |
| A03 格式化删除括号（E，本片保守保留） | `m!([]int{},"bad")` 经外层转发到 `inner!` 再展开 `append($s,$v)` | 单层捕获括号在前片已保留栈，本片继续保持；该嵌套形式的 Go 格式化删除冗余括号后，错误所落的剩余合成括号仍无可运输的映射。后续必须证明括号与展开上下文对应，不按邻近真实字符串 token 补栈 |
| A03 helper alias 因果链（E，具体 cycle 控制） | `read!()` 调用 helper，helper 返回 `Value`，而 `var Value=read!()` | 首条 Value 错误已有真实来源；Go initialization cycle 后续含 appended alias 的链只保留原始 stderr。若补充须证明共享 alias 与一次/多次展开的关系，不能任挑最近调用作为唯一来源 |
| A03 refs 中间阶段（E） | `macro_rules! bad {($r:ident)=>{$r.N++};}` 用在只读 `r &S` 上 | 权限拒绝不变；已有回归明确显示 `refs.Finish [generated/unmapped; intermediate coordinates]`。后续传递真实 token 身份，不为定位放宽 readonly |
| A03 其余直接错误路径（E，静态待穷尽） | builtin finalization 某分支直接创建 sourcemap.Error，绕过本次 Diagnostic mapper | 先找有 ?/跨文件偏移的稳定触发例，再核对原始 point 与 frames；目前是代码路径边界，不虚称已复现新退化。enum/derive 等阶段也保留各自准确坐标合同 |

本片不会把 context-only 合成 token 变成原始位置，也不把任一未知 frame 拼接成完整链。
A01 的具体边界沿用 [allocation 报告](macro-allocation-builtin-20261006.md) 和 RFC0011，
本诊断片未实施新语义。A01、A03、RFC0011 与 MVP 整项继续开放。

## 发布与环境

实际环境：Mac mini，darwin/arm64，Go 1.27.1；任务内真实 TMPDIR、固定完整源码模块缓存、
GOTOOLCHAIN=local，GOMAXPROCS=4。未安装工具、修改全局 Git/auth 或旧 hyper-go checkout。
网站验证使用隔离源码 overlay，没有部署；Linux 未运行。托管 CI 仅在最终普通分支 push 后
按精确 SHA 查询；无 Actions/check/status 记录不算通过，也不把聚合 pending 当作运行中。
最终分支 SHA、fetch、ls-remote 与 CI 查询写入本轮交付结果，避免报告自引用 SHA。

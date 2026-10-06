# A03：合成错误点与可证明宏栈分离

基线 `c32b94f4375aa5b2750107edffe422315168d5b4`，独立分支
`fix/macro-synthetic-origin-stack`，准确源码及验收提交 `fd8380c39876c96441857a737be8e32c322a0467`。
本片修复宏诊断及现存 map 在错误点未知时无条件丢弃已证明展开栈的问题，
覆盖 `?` 预处理合成节点；同时修复包编译将同类节点的零长度范围误报为真实源码。
没有新增语言语义或扩大宏类型槽支持；下游 AST 重建和格式化的具体余项另列。

## 来源合同和修复边界

原始文件的 Identity map 经 `propagate.PrepareMapped` 传递到宏展开；宏 registry
同时保存定义文件的原始坐标，调用方 token 仍可能处于 prepared 坐标。
这两种坐标必须按显式标记处理，不能因文件名或数字偏移相同就混用。
宏输出的逐 token map 再经各 lowering pass、打印和格式化到最终 Go 文件。
不同降低阶段的坐标也不能直接套用 prepared map。

本片将错误点和完整展开栈分别证明。真实 token 只接受有效、完整的显式映射；
`?` 的合成 marker 没有原始等长 token，继续显示 `generated/unmapped`。
已知宏定义/调用的包围范围可由真实端点恢复，因为该准备阶段顺序复制原 token
并插入 marker；这不是其内部每一个字节都有来源的承诺。
完整 outer→inner 栈只有在每一帧的定义和调用均可证明时才保留。
其中任一帧未知仍保守省略整链，不拿相邻位置补齐、不拼接部分帧冒充完整历史。

原始 registry 坐标先检查非空文件、正的行列及非空正向范围，再按来源 owner
使用；同名文件可能属于另一个包，不能用调用文件的长度或内容重解释它。
未标 original 的外部文件坐标不能借用当前文件 map。
结构化错误、成功展开的 generated/unmapped segment 和最终 Go 补充诊断
均允许 `Origin=nil` 与已证明的 `Stack` 同时存在。Go 原始 stderr 保留，
补充诊断只匹配具体生成文件，不因 basename 相同串文件。

详见 [RFC0018](../rfcs/0018-lowering-contract.md#a03-合成错误点与宏展开栈2026-10-06实施合同)
和 [验收合同](../t0-acceptance.md#a03-合成错误点保留已知宏栈2026-10-06实施验收合同)。

## 验证

新增 CLI 验收在 c32 基线有 12 项预期缺陷失败，6 项真实 token 控制及一个合法运行通过；
应用修复后 19 个 leaf case 全部通过 race。实际 Go build 失败的原始 stderr、文本补充
及导出的 JSON map 在同一错误坐标对照，包含 LF/CRLF、单文件、目录、多文件、
跨包相同 basename、nested、enum、多个 ?、make/new 类型槽和确定性运行。
红绿历史与源码文件哈希见 [验收证据](macro-synthetic-origin-evidence/acceptance-evidence.json)。
主仓库目标 frontend/sourcemap race 也通过，直接反例涵盖部分 token、零宽/逆向范围、
错误文件、越界端点、逆序端点和未知帧；相同文件名的另一个 owner 的定义位置
特意超过调用文件长度，验证不会借用调用文件内容。

完整 `go test -race -count=1 -timeout 30m -json ./...` 退出 0：
**564 个顶层测试、1226 个子测试通过**，
17 个有测试包、5 个无测试包；没有失败或跳过测试。
[整树摘要](macro-synthetic-origin-evidence/synthetic-full-race-summary.json)保存准确提交、
逐包结果及完整日志 SHA256；使用任务真实 TMPDIR 和完整固定依赖源码缓存。
全树 vet 与完整设计检查退出 0，后者包含 13 条 Go 机制命令，不替代真实 CLI 验收。

[独立审查](macro-synthetic-origin-evidence/independent-review.json)绑定准确源码，结论无阻断：
44 项矩阵中，28 个合成点保持 unmapped 并显示准确完整栈，14 个普通 token 诊断
与基线逐字相同，2 个 len($x?) 控制仍是明确余项。另有 16 组 LF/CRLF 新旧对照：
8 组合成栈改善、2 组真实点不变、6 组明确剩余边界；显示出的 10 条补充宏栈逐帧
核对真实源码 anchor。合法分配控制生成 Go 字节不变，真实 `go run -race` 输出 `1 2 3`。

独立审查的最初宽泛预期在嵌套 append 括号上失败两项，原始
[结果](macro-synthetic-origin-evidence/independent-results.json)保留失败状态。
[分阶段探针](macro-synthetic-origin-evidence/nested-append-format-probe.json)证明原始
宏输出和本片 mapper 都有完整 `m → inner` 栈；既有格式化回退在删除冗余括号后
只恢复非括号 token，报错括号的上下文随之丢失。这是经定位的现有 formatter 边界，
不是新 mapper 错拒已证明帧；不修改历史记录以假装第一次全绿。

第一次整树 race 在沙箱中执行，`TestImportSourcesVerifiedAndOffline` 的本地 HTTP
fixture 因 `bind: operation not permitted` 中断 cmd/goxide 包，整树退出 1；
当次 486 顶层通过、1 顶层失败，其余未执行部分不能计作通过。
[首轮摘要](macro-synthetic-origin-evidence/sandbox-attempt-race-summary.json)保留日志 hash
及结果。经正常权限审批，同一用例原样 race 复核通过，然后重跑完整整树。
没有更改测试、依赖或源码，没有跳过端口测试；最终完整结果见上一节。

[CLI/WASM 汇总](macro-synthetic-origin-evidence/web-validation-summary.json)绑定同一源码，
WASM/build、web race/vet、native parity、browser QA、catalog 共七阶段退出 0。
旧 83 项 checks 和 10 示例全部保留通过；新增 8 个诊断 fixture 形成 32 次原生 CLI
观察、16 次 UI LF 观察和 32 次真实持久 WASM worker 的 LF/CRLF 观察，均有逐次恢复。
textarea 会规范化换行，因此 UI 只声明 LF；CRLF 字节通过 raw worker 真实编译验证。
每例独立检查原始 byte span/定义/调用 anchor，不只依赖 CLI 与 WASM 相互一致。

10 示例和 22 个既有正例共 32 个成功源码，在相同输入路径下与嵌入 `c32b94f` 且
`vcs.modified=false` 的基线 CLI 对照，生成 Go 逐字节相同。
[输出对照](macro-synthetic-origin-evidence/successful-go-parity.json)保存每个源及输出 hash。
catalog 26 张卡片、10 个负例、桌面/390px、取消/超时/恢复及零外部请求检查通过。
服务已停止。

使用既有网站 overlay `6dc779a651af3eb34121f53637a55bfb6feee387` 的隔离 checkout；
编译器与 QA/fixtures 从准确源码提交恢复。compiler digest 为
`fb807e5029c92645606fd7d3536f984e325793ae9f30b254e8448b357e6c1519`；
WASM 16,183,976 字节，SHA256 为
`64b40e660a3a0655be2c639b19810cf46b4813daf662abb3be1f990ebdc4d382`。
[构建身份](macro-synthetic-origin-evidence/local-wasm-identity.json)和
[来源身份](macro-synthetic-origin-evidence/web-source-identity.json)保留完整环境及哈希。
Homebrew Go LICENSE 路径适配只发生在该隔离树，patch 单独留证；不声称生产脚本
未经适配就在本机通过。生产网站行为/build/catalog 未改，未安装依赖、未部署。

## A01/A03 具体收尾场景

以下是具体后续验收场景，不是永久拒绝清单。E 为已有合同下的工程补齐；D→E
需要先确定新的语法/语义。已完成的固定类型 make/new 不再列为未做。

| 项目 | 可复现/可执行的收尾场景 | 后续完成要求与证据边界 |
| --- | --- | --- |
| A01 定义点 named type（E） | 定义 `type Count int` 和 `macro_rules! zero {()=>{new(Count)};}`，调用函数另有 `type Count string`；跨包再换成 `new(model.Count)` | 当前固定类型门禁拒绝；以后必须返回定义点 `*Count`，保持可见性、类型身份及用户反射名称，不能按同名字符串绑定。固定 Go 泛型实例身份另列，不能顺带开放新 `.gox` 泛型声明 |
| A01 builtin 名称被调用点 type 遮蔽（E） | 模板 `len($x)`，调用处有 `type len int`；或调用包定义同名 var/func/type | 当前明确边界；后续须区分定义点 builtin 和调用点绑定，不改名用户 named type、不引入改变控制帧的 wrapper。调用处 `type int string` 下模板 `new(int)` 已有 alias 验证，不能混作未做 |
| A01 其他类型槽的 expr capture（E） | `macro_rules! zero {($T:expr)=>{func()$T{var v $T;return v}()};}` 再调用 `zero!(int)` | 既有类别泄漏，未来应按既有拒绝合同准确拒绝。不能误拒完整表达式捕获 `id!(make([]Local,2))`，或数组长度表达式中的类型。参数、结果、var、复合字面量、断言等槽需逐项补齐，尚未穷尽 |
| A01 原生 `new(expr)`（E） | `boxed!(next())` 展开为 `new($v)`，对照 `boxed!(int)` | 原生 Go 1.27 值形式合法，本片宏类型形式仍拒绝。后续需证明值/类型真实绑定、一次求值、named 类型、浅共享、typed nil 和 ? 最近用户帧；不能把 expr 捕获顺带赋予类型资格 |
| A01 一般 type fragment（D→E） | 候选 `$T:type` 后在 `new($T)` 使用 | 先定义语法、绑定点、泛型/限定名与 repetition 规则；不能拿 `$T:expr/ident/tt` 或现有漏洞绕过设计 |
| A03 下游传播 AST（E，最终提交实测仍缺栈） | `macro_rules! m {($x:expr)=>{len($x?)};}`，捕获返回 `Option[int]` 的调用 | int 传给 len 的类型错误继续拒绝；重建的临时节点仍 unmapped/无栈。后续沿节点身份证明上下文，不能猜附近 len 或相同文字 |
| A03 格式化删除括号（E，最终提交实测仍有边界） | `m!([]int{},"bad")` 经外层转发到 `inner!` 再展开 `append($s,$v)` | 单层捕获括号本轮已保留栈；该嵌套形式的 Go 格式化删除冗余括号后，错误所落的剩余合成括号仍无可运输的映射。后续必须证明括号与展开上下文对应，不按邻近真实字符串 token 补栈 |
| A03 helper alias 因果链（E，具体 cycle 控制） | `read!()` 调用 helper，helper 返回 `Value`，而 `var Value=read!()` | 首条 Value 错误已有真实来源；Go initialization cycle 后续含 appended alias 的链只保留原始 stderr。若补充须证明共享 alias 与一次/多次展开的关系，不能任挑最近调用作为唯一来源 |
| A03 refs 中间阶段（E） | `macro_rules! bad {($r:ident)=>{$r.N++};}` 用在只读 `r &S` 上 | 权限拒绝不变；已有回归明确显示 `refs.Finish [generated/unmapped; intermediate coordinates]`。后续传递真实 token 身份，不为定位放宽 readonly |
| A03 其余直接错误路径（E，静态待穷尽） | builtin finalization 某分支直接创建 sourcemap.Error，绕过本次 Diagnostic mapper | 先找有 ?/跨文件偏移的稳定触发例，再核对原始 point 与 frames；目前是代码路径边界，不虚称已复现新退化。enum/derive 等阶段也保留各自准确坐标合同 |

上述 A03 实测余项及已经改善的单层 append/copy/make，见
[独立结果](macro-synthetic-origin-evidence/independent-results.json)。A01 的当前边界与
分类沿用 [allocation 报告](macro-allocation-builtin-20261006.md)和 RFC0011，未在本诊断片
实施新语义。A01、A03、RFC0011 与 MVP 整项继续开放。

## 执行范围

本轮是 Mac mini、Go 1.27.1 darwin/arm64 的本地证据。Linux 未运行。
托管 CI 在普通 push 后按最终精确 SHA 查询；查不到的 runs/checks/contexts
不能记为通过，也不能把空 contexts 的聚合 pending 解释成正在执行 CI。
网站仅本地构建验证，未部署；main、恢复分支及 tags 不变。

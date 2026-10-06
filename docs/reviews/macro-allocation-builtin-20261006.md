# A01 固定类型 make/new：有限子项交付

从已推送的 `9ca38dc283ebd615498535de100057585b2492ac` 创建
`feat/macro-allocation-builtins`。先研究既有 RFC 的捕获类别与定义点绑定，再写
[RFC0011 白黑例合同](../rfcs/0011-macros.md)，最后实现固定预声明类型的分配子集。
代码提交：`ec9dcbfba578c8c0553e4b1bfa32d0909463ae02`；交付文档后续提交不修改已验证源码。

## 研究结果与实现边界

仅在 builtin 名单加入 make/new 会令 expr/ident/tt 捕获进入类型位置，甚至从普通
`.go` 文件捕获泛型类型。这违反既有类别合同。另一方面，定义点 `func int` 或
模板局部 `int := 7` 会令 `new(int)` 变成合法的值初始化；只按名称检查也不够。
研究证据与独立探针保留在本批 evidence 中，未以白名单实验作为可交付实现。

类型门禁只在调用确实绑定为 builtin 后生效。第一实参接受真正预声明类型及既有
pointer/array/slice/map/channel/括号组合；类型标识符必须无模板局部绑定，并通过
既有 primitive 定义/调用包及 import 检查。数组长度仍是表达式位置，可使用合法
捕获和模板局部常量。模板局部或定义点真实函数名为 make/new 时保持普通函数行为。
分配操作最终仍为原生调用，不引入 wrapper 或改变最近用户函数的问号返回目标。

| 已实现 | 继续开放 |
| --- | --- |
| 固定类型的 make/new，长度/容量与零值，独立分配、map/channel 使用，绑定、求值与传播验收 | 同包/导入 named type、类型参数、泛型实例、匿名 struct/interface/func 类型 |
| 对新增 allocation 类型槽显式拒绝任意深度 expr/ident/tt 捕获 | 一般 type fragment 及完整捕获类别规则；基线其他模板返回类型位置已有捕获泄漏单独记账 |
| 复用原有预声明类型 alias，调用端局部 primitive 类型不截获模板类型 | 调用包同名 builtin、覆盖调用点的局部 named type 等现有工程边界 |

原生 Go 1.27 支持 `new(expr)`，本片明确尚未交付它；不能将合法需求列成 Go 类型
黑例，也不把此子片完成写成 A01、RFC0011 或 MVP 全部完成。

## 验证

[宏目标测试](macro-allocation-builtin-evidence/allocation-macros-final.log)的 10 组 race
通过；包含深度 40 的确定性工作量回归，防止数组长度中的类型子树被指数重复检查。
嵌套宏仅信任内部准确记录的 primitive alias，并通过不同定义包的同名类型对照。
[旧实现红例](macro-allocation-builtin-evidence/allocation-macros-before.log)保留；开发中
slice 空长度保护、native 顺序测试假设和 refs 混合 fixture 的失败记录留在任务目录，
未将它们冒称旧基线或一次最终整树结果。

[真实 CLI 验收](macro-allocation-builtin-evidence/acceptance-cli-race.log)从无关 cwd
执行，重复生成 Go/map；生成模块 4 组运行断言、race/vet/run 通过，输出 `2 4 3 0`。
覆盖 21 个预声明类型、复合类型、独立存储、原生 make 对照与每个参数一次执行、
问号成功/早退的 8 种组合、跨包绑定及 readonly 正反例。21 个类型槽拒绝包含 native
`.go` 提供的泛型声明，避免由另一个阶段的泛型限制掩盖泄漏；9 个原生类型错误由
真实 CLI build/Go 拒绝。其后注释和嵌套 alias 修复均在下列最终完整整树中重新覆盖。

[原生 make 对照](macro-allocation-builtin-evidence/allocation-native-order.log)记录
本机 Go 1.27.1 实测原生与生成代码均先求容量再求长度（`CN`）；验收对照同工具链
原生行为且要求各参数恰好一次，不将此观察扩写为新的 Goxide 求值规则。

对准确提交执行完整 `go test -race -count=1 -timeout 30m -json ./...`，退出 0：
**555 个顶层测试、1200 个子测试通过**，
17 个有测试包通过，5 个无测试包；没有失败或跳过测试。
[整树摘要](macro-allocation-builtin-evidence/allocation-full-race-summary.json)记录逐包结果、
准确提交、环境、完整日志字节数与 SHA256。使用任务真实 TMPDIR 和完整的固定依赖缓存，
不改测试条件。[全树 vet](macro-allocation-builtin-evidence/allocation-vet.log)及
[完整设计检查](macro-allocation-builtin-evidence/allocation-design.log)退出 0，包含 13 个
Go 机制命令；机制检查不替代真实 Goxide/CLI 执行。

[独立审查](macro-allocation-builtin-evidence/independent-review.json)绑定同一源码提交，
无阻断。37 个单文件用例及一个跨包模块形成 56 个最终观察；补充 16 个基线/当前
来源控制，并在规范 TMPDIR 下重跑 20 个原生命令。额外黑盒覆盖数组长度常量与不求值规则、深层类型捕获、24 层嵌套长度、
跨定义包 primitive alias、普通/捕获调用目标和 LF/CRLF；结果及适用范围见
[逐例记录](macro-allocation-builtin-evidence/independent-results.json)。

有一个明确保留的来源边界：`new($x?)` 正确拒绝未交付的值形式，但拒绝点是预处理
生成的 `__goxide_question` 节点，因此显示 generated/unmapped 且无来源栈。
基线在同类合成节点语法错误和 len 传播错误中有相同表现；旧版 new 本身未支持，
先在原始 new token 报错，新版的更深检查触发了既有边界。不能声称所有新增门禁
错误均有完整栈；没有用 new 的位置冒充实参位置。最初对完整栈的失败预期保留，
审查结合基线控制将它归入 A03 剩余，而非改写历史为初次全绿。

[CLI/WASM 汇总](macro-allocation-builtin-evidence/web-validation-summary.json)确认
新增 35 个 allocation 差分通过：5 个白例、22 个前端拒绝、8 个原生类型拒绝。
Go 合法 `new(7)` 原生对照执行成功，宏类型形式子集明确拒绝。旧的 19 copy、15 append、
14 诊断/适配检查与 10 示例继续通过；web race/vet、桌面/390px、恢复/超时/中断及
完整 catalog/integrity 检查通过，loopback 服务已停止。

网站在隔离 checkout 使用已有 overlay `6dc779a651af3eb34121f53637a55bfb6feee387`，
编译器及 QA/fixtures 精确绑定本片提交。编译器 digest：`e3524637a087e15003d37d170c5aa544c6fa5a5e78ac9d900934e80e39c1d6c7`。
WASM SHA256：`2e92ef9c70ffa521bcc18cd042037047b7281b45d5e45d9a12b913f042e62b9f`。
Homebrew Go LICENSE 路径适配仅在该 checkout 中进行，
[patch](macro-allocation-builtin-evidence/homebrew-license-validation.patch)及
[源码身份](macro-allocation-builtin-evidence/web-source-identity.json)保留；不能声称生产
构建脚本未经改动就在本机通过。生产 build.py/catalog 不变，未部署，未安装浏览器。
人读日志仅做空白规范，原始与展示 hash 见[provenance](macro-allocation-builtin-evidence/log-display-provenance.json)。

单文件转译和 WASM 是现有前端降低边界；普通 arity、make 类型、容量及数组常量等
错误需真实 Go build 拒绝。模板类型门禁拒绝发生在前端；普通 token 保留宏来源，
合成传播节点的例外如上所述。
A03 合成括号、helper alias、传播节点及部分后端错误的 generated/unmapped 边界
保留，不按相邻文字伪造来源位置。

本轮是 Mac mini、Go 1.27.1 darwin/arm64 的执行证据；Linux 未运行。托管 CI 只在
普通 push 后按最终精确 SHA 查询，零 runs/checks/contexts 不能记为通过或等待中的任务。
未安装系统工具、改变全局 Git/认证设置、推送 main/tag、发布 release 或部署网站。
main 与恢复分支保持原提交，std/serde/next/site 没有本批修改。

仍可按既有合同继续的 T0 工作包括：A01 定义点非泛型 named type 身份和可见性、
明确类型位置的既有捕获类别泄漏，以及 A03 原子调用实参的来源精度。一般 type
fragment/完整重复语法等仍需设计确认；不能用这些未定项掩盖其余已确认工程需求。

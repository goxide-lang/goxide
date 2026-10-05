# 宏与具体派生诊断：A03 首片交付

本片接续 [MVP 差距台账](../mvp-gap-audit.md) 的 A03，以及 WASM 差分发现的 A01 单文件 importer 工程缺口，基于已批准的 T0 宏、具体
派生及 lowering 来源保持要求。先在 RFC0011/0013 写验收合同，再实现既有错误的
定位修复；没有新增语义决策。诊断提交 `b89a09b5ac7bbafe92a13db320eaa87d0db3059e`，importer 提交
`50490e113ea668333cdb78e8b36df4850f8543c4`，
保留审计提交 `0b543ddd5321b38b9c792b823874bebde65dc813` 在祖先链中。

## 交付边界与可复现证据

| 问题 | 修复后行为 | 实现与验收 |
| --- | --- | --- |
| 模板前有合法语句，import selector/key/type/label 错误总报模板首 token；后续错误覆盖首个错误 | 通过 AST offset 找到触发规则的 token；按既有两阶段 AST 遍历顺序保留首个已识别问题 | [hygiene](../../internal/macros/hygiene.go)、[专项](../../internal/macros/hygiene_diagnostics_test.go)；LF/CRLF、重复 selector、package/nested |
| `_=$v?;_=fmt.Println` 前处理增长18字节，原第43列报61列 | 结构化诊断保留坐标阶段；本文件 prepared token 映射，package original token 不重复映射；保留定义/调用栈 | [macro Diagnostic](../../internal/macros/macros.go)、[frontend](../../internal/frontend/compile.go)、[Prepared/NestedPrepared 测试](../../internal/frontend/derive_diagnostics_test.go) |
| 前置多行宏改变行数，原第8行 derive 错误报第15行 | 属性与 helper 冲突使用 refs 后、属性擦除前保存的 map；已有行级错误指到该行首个实际 token | [derive tests](../../internal/frontend/derive_diagnostics_test.go)；未知/重复 derive、未知属性、helper 冲突、不同文件、CRLF |
| enum 元数据行号误用后续降低阶段 map，可把第26行 enum 错指第38行无关 var | enum 错误使用 enum 降低前 map + offset；model 错误按 enum/普通类型分流 | `TestDeriveDiagnosticEnumStage`；前一个 enum 有20空行、后面有40个同名声明 |
| 修诊断不能改合法模板或具体派生行为 | 真实 CLI 从无关 cwd 转译生成模块，再 race/test/vet/run；两次捕获求值、Clone 独立 slice、Debug、Default 保持原语义 | [CLI acceptance](../../tests/acceptance/macro_derive_diagnostics_test.go)；stdout `2 42 43 0 Item { Values: [42] }` |
| 单文件宏忽略前端 importer，在 WASM 访问不存在的宿主 GOROOT | 单文件复用既有前端离线/浏览器 importer，传入的 importer 不再受 package registry 非空条件限制；浏览器包集合不变 | `TestSingleFileMacroImporter` 先失败后通过；[浏览器合法 import helper](../../website/checks/macro_imported_helper.gox) |
| 浏览器与 CLI 不可出现不同诊断 | 新增4个负例及1个合法 import helper 例，逐字节比较规范化文件名后的真实 CLI/WASM 错误，失败清空旧输出 | [QA](../../website/qa.mjs)、[selector](../../website/checks/reject_macro_selector.gox)、[prepared](../../website/checks/reject_macro_prepared.gox)、[derive](../../website/checks/reject_derive_origin.gox)、[enum](../../website/checks/reject_enum_derive_origin.gox) |

原始新增测试先失败，证实实际缺口；修复后专项通过。独立对抗审查首轮发现 `?`
坐标和 enum 阶段两个阻断，均修复；第二轮原探针及跨文件双方含 `?` 的嵌套探针通过，
两项阻断已闭环。后者已纳入永久回归，并强化为按 span 切回原始定义/调用文本和行列断言。

## 验证与本地 WASM 组合

环境为 Linux amd64、Go 1.27.1；固定 `GOTOOLCHAIN=local` 和可写 Go cache/module cache，公开 std/serde 版本未改或换成本地 replace。首轮全树 race 在追加 importer 修复时主动停止；最终全树从最终生产代码重新运行，不把中断结果当作全树通过。网站验证使用源码 `6dc779a651af3eb34121f53637a55bfb6feee387`
覆盖独立 detached 验证 worktree 的 website 目录，保留本片 QA/fixtures；catalog 的 compilerCommit/digest
只在该 worktree 绑定本片编译器。没有改变发布目录、生产 catalog、线上网站或部署状态。
此组合不能宣称为已发布版本；编译器生产文件摘要为
`c83f62e5228c1312314d9adf15f28c9853fd6385b7e0c6ab6d1b9869f1e6ee93`。

追加 importer 的独立审查通过：注入在展开之前，不改变定义点 scope/alias/function-only
拒绝；原生继续离线，WASM 只读既有 browserexports。离线专项通过。

## 验证结果

| 检查 | 结果与证据 |
| --- | --- |
| 红绿回归 | [原始诊断先红](macro-derive-diagnostics-evidence/before.log)、[importer 先红](macro-derive-diagnostics-evidence/importer-before.log)、[最终专项 race 通过](macro-derive-diagnostics-evidence/targeted-race.log) |
| 原生全树 | 最终 `go test -race -count=1 -timeout 30m -json ./...` 退出0：17个有测试包、526项顶层测试通过，5个无测试包，没有跳过测试；CLI 103项、acceptance 149项。[完整摘要及原始日志哈希](macro-derive-diagnostics-evidence/full-race-summary.json)；`go vet ./...` 退出0，[空输出日志](macro-derive-diagnostics-evidence/vet.log) |
| docs | [最终文档检查](macro-derive-diagnostics-evidence/docs.log)：本地链接/锚点、围栏及26个 RFC 结构通过 |
| design | [完整设计检查](macro-derive-diagnostics-evidence/design.log)：13 个 Go 命令，包括预期失败；这不是 Goxide 语义测试的替代品 |
| WASM 构建 | [构建日志](macro-derive-diagnostics-evidence/wasm-build.log)，10例；[本地组合身份](macro-derive-diagnostics-evidence/local-wasm-identity.json) |
| web race/vet | `go test -race -tags goxideweb ./cmd/goxide-web ./internal/typecheck` 与同包 `go vet` 通过；[race](macro-derive-diagnostics-evidence/web-race.log)、[vet](macro-derive-diagnostics-evidence/web-vet.log) |
| CLI/WASM 差分与 UI | [browser QA](macro-derive-diagnostics-evidence/browser-qa.log)：10例生成 Go、9个 Go-result 检查、4个新负例与1个合法 imported helper 全部逐字节一致；桌面/390px、超时/取消、错误恢复、无外部请求通过 |
| catalog | [catalog QA](macro-derive-diagnostics-evidence/catalog-qa.log)：26卡片、10负例、过滤/链接/工程快照、损坏/过期 WASM 拒绝及 manifest 失败态通过 |

首次 npm 安装误用不可写 home cache，指定 workspace npm cache 后通过；首次浏览器
CLI 子进程未继承 Go cache 环境，修正环境后暴露真实 importer 缺口，
[原始差分失败](macro-derive-diagnostics-evidence/browser-importer-before.log)保留。
修复 importer 后重新构建并完成最终 browser/catalog QA；未绕开失败 fixture。

## 剩余工作与下一片

A03 仍开放：appended helper alias、合成传播节点及其他后端诊断完整来源链尚未完成。
无法证明的生成节点继续 `generated/unmapped`；本片没有把原来只知行级上下文的
属性错误包装成属性子表达式级精度，也没有扩充允许的宏语法。

下一片建议 A01 已批准 log/tracing 常用 builtin/绑定用例，以最小合法/拒绝矩阵选一组。
A07/A29 先分出已有入口的工程部分和需要确认的协议选择；手写 Clone/Debug/serde、
typed Error source、blank/embedded、泛型新规则仍先给最小用例确认。独立的 A04 match
表达式与 A10 问号控制流可继续，不等待长期借用分析。vendor/local、命名不重开；full borrow 非 MVP 的结论不变。

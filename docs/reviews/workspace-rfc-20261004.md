# Workspace RFC 0026 研究验收（2026-10-04）

状态（历史研究快照；后续实现见 [核心验收](workspace-core-20261004.md)）：研究与独立对抗评审完成，显式依赖、根唯一锁/拆出复制、Git URL优先及阶段顺序已确认，统一版本解析与构建feature隔离随后获确认；workspace 生产功能未实现、未批准，未部署网站。基于 `d51117d36fa2d1ccfcf322b81195b0d753cbb8de`，文档分支 `docs/workspace-rfc`。正式合同候选见 [RFC 0026](../rfcs/0026-workspaces.md)。

## 资料与环境

已查阅官方 Cargo Book workspaces/resolver/features/specifying-dependencies，Go modules/toolchains/GODEBUG/cmd/go，链接与事实/提案区分见 RFC。Cargo 为 2026-10-04 滚动文档，无本地 cargo/rustc，未声称 Rust 实测。真实 Go 为 `/workspace/goxide-environment/go/bin/go`：`go version go1.27.1 linux/amd64`。

## 已通过机制试验

运行 [研究脚本](../../scripts/research/workspace_go_probe.py)，全部 19 项通过；[完整结构化结果](evidence/workspace-go-20261004.json)已保存，fixture 路径归一为 `<fixture>`。每次新建临时目录/空GOMODCACHE，只用本地replace；网络解析禁用，GOTOOLCHAIN=local，无Go下载/私有来源/脚本安装。研究用 `go work vendor` 只制造fixture，不提出在迁入中重建vendor。

| 证据 | 结果 |
| --- | --- |
| GO-01 / GO-02 / GO-03 | A独立输出one；A/B共同主模块时即使只run A也输出two，选择D v1.1.0 |
| GO-04 | 生成仅A的work视图恢复one，证明选择解析范围是可观察差异 |
| GO-05 / GO-06 | 两成员replace冲突，work根显式override后输出two |
| GO-07 / GO-08 | modules.txt含workspace标记；移走两个replace目录后，work vendor冷modulecache仍输出two |
| GO-09 / GO-10 | GOWORK=off不能消费workspace vendor；删vendor实际import包明确失败，不联网 |
| GO-11 | Go edit JSON保留Go/Toolchain/Godebug；此项只验证解析，不证明hgo映射或运行时GODEBUG等效 |
| GO-12 | 原生全workspace含未来go1.99成员时，list A也拒绝，未选择编译不等于未参与Go政策 |
| GO-13 / GO-14 / GO-15 | 成员panicnil=1、工作区panicnil=0覆盖、退出后成员值恢复；DefaultGODEBUG由真实Go报告 |
| GO-16 / GO-17 | A没有require B仍可在workspace import成员B；standalone readonly明确缺模块失败 |

Go list默认VCS stamping在此隔离环境报git状态错误，研究脚本显式关闭buildvcs，避免环境VCS影响政策探针；不改变作者工程。GODEBUG探针显式-mod=readonly避免前项workspace vendor被自动采用，vendor试验本身仍用-mod=vendor。

GO-18 / GO-19 验证最长前缀不等于包归属：内层module无目标包时外层可提供；双方都有时真实Go报ambiguous import。

## 文档与实现边界

变更限于 RFC、索引、ROADMAP、研究脚本/结果。检查生产源文件diff为空；不新增接受workspace的parser或CLI路径。RFC六章节、链接与代码围栏需通过 `python3 scripts/check_design.py --docs-only`。W01–W18是未来实现矩阵，不是已通过测试；无hgo workspace feature/cache/lock/trust/publish实测。本轮不重复生产全量race/vet，基线已有覆盖不算本RFC功能验收。

## 初版独立评审（历史记录，自动成员边建议已撤回）

独立 reviewer 初轮检查全部RFC/验收表、脚本及JSON，独立运行原12项Go机制试验，归一化结果与候选记录相等；另行验证无require的成员import。发现并已修订：单次命令只能绑定一个根不等于禁止多workspace只读同一成员；CLI未选不等于源码未触达；optional边不抹去原生require/其他活动边。补充固定点重算/振荡诊断、关闭文件不激活、脚本/宏生成跨成员import须清单预声明和独立发行补齐require。第二轮独立重跑17项通过，指出最长module前缀不等于包归属；修正为Go实际包映射并新增GO18/19正反例，总19项通过。最终文字复核确认其余无阻断；依其最后建议将optional诊断限定为没有其他有效显式/原生require边的情形，避免错误拒绝合法并集。没有运行Cargo或声称hgo功能完成。

最终文档检查通过96个Markdown、1004个本地链接/锚点、25个正式RFC；git diff --check通过。生产目录没有变更。


## 用户修正：仅显式依赖继承

用户明确要求 `package1 = { workspace = true }`。已同步RFC WS02/04/05/07/09/10、W17/W18、来源冲突和待决表：根模板不注入，成员身份不覆盖version/path/git，import不创建边；生成Go use不能绕过此约束。原生GO16–19仍是Go差异/包归属证据，不再为goxide自动成员发现背书，旧import/feature固定点建议撤回。Go.work迁入须在plan物化实际边/覆盖并声明配置覆盖范围，无法无损推断时列缺项/拒绝，不运行时fallback。

本次仅改文档解释，19项原生Go机制结果未改变、不重复充当新hgo功能测试；显式原则不代表其他workspace策略获批。新增inline TOML白/黑例；独立文档一致性复审确认无阻断，覆盖模板、来源保持、显式图先于import、生成Go use及vendor不得绕过、迁入范围和历史撤回。按review建议将W18迁入物化验收归入阶段二，避免阶段一范围含糊。文档检查及diff空白检查再次通过。


## 用户后续确认：唯一根锁、Git优先与阶段顺序

核对原四问：第4项是先作者workspace核心、再go.work/vendor迁入，不是解析策略批准。已替换成员保留lock建议，新增复制根锁的可达性/完整性/来源校验、无关条目不下载不执行、不自动升级以及本地path源码边界。Git URL优先，别名不猜registry；原生完整Go module+version继续互操作。app/server/common表明确命令选择只决定构建对象，统一版本图和feature范围另定；全workspace统一版本锁是最新建议，未批准。

重新查阅官方Cargo workspaces/resolver及Go module reference；Cargo全workspace锁求解和实际编译feature集合有区别，不能照搬成Go多版本类型身份。再次检查仓库相关AGENTS/.agents/skills，无适用新增指令。本轮只更新文档及黑白例，不重跑未变化的原生Go探针、不声称生产实现；文档独立一致性复审完成：指出并已修复旧selected范围、分开命令解决来源冲突、optional读取承诺和并发两计划措辞；统一为全workspace版本结果/完整锁构建与锁更新分层，并修复表格连接。文档检查96文件/1004链接/25RFC及diff空白检查通过。


后续“对的”确认已同步：全workspace统一版本解析与根lock，仅构建选中成员及依赖、未选成员feature不带入。删除selected/all-members模式建议，WS10改为共同依赖被server抬升但app不启用yaml的正反例；host/target独立角色内统一，根锁拆出规则不变。前段“仍待决”是这次后续确认之前的过程记录，不是当前状态。

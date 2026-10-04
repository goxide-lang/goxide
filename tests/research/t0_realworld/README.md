# T0 独立实用性、回归与性能审查

2026-10-04；已执行 `git fetch origin feat/t0-completion`，实际基线为 `3098a30e8e5fcb4696687246b2f39988218f4006`。独立分支 `review/t0-realworld-20261004`。工具链 `/workspace/goxide-environment/go/bin/go`（Go 1.27.1 linux/amd64）。没有采用陈旧 main，没有修改生产编译器、推送或 merge，没有介入 Go `(v,error)` 适配宏实现。

## 优先处理的发现

| 优先级 | 发现与用户影响 | 证据 / 最小建议 |
|---|---|---|
| P1 正确性 | 跨包宏的 builtin 常量/类型被调用包顶层名称静默捕获。`true/false` 反转、`int(0)` 变字符串。不是文档列出的 len/cap 拒绝边界。 | [真实复现和期望/实际](macro/README.md)。先检测冲突并定位诊断，完整方案需保留定义点绑定，不能重命名用户 named type。 |
| P2 运行性能 | imported Result `?` 的 validator 两层函数变量使每项值逃逸；1024 项 1025 alloc / 8200 B。 | [双模块案例](integration/README.md)。仅临时生成代码改具名类型转发函数后 1 alloc / 8 B，校验和 ABI 不变；建议修复登记 validator 的静态绑定，不移除校验。 |
| P2 编译性能 | 模板内每个 imported selector 重新扫描整棵 AST，重复 helper 呈 O(N²) 扫描。 | [宏 benchmark / 来源](macro/README.md)。提前收集 CallExpr.Fun 集合并查表；未修改生产代码，未宣称已验证修复收益。 |

两个 macro primitive 表现按同一根因统计，不重复当作两个独立缺陷。优化 benchmark 三次中位数见 [完整表](integration/results/summary.md)：1024 项原始 hgo 14.663µs，手写 Go 0.782µs；静态 validator 实验 hgo 2.054µs，8 B/1 alloc。共享机器时延有波动，分配差异和编译器 escape 证据更强，不宣称通用 7× 加速。

## 可运行交付

```bash
export GO_BIN=/workspace/goxide-environment/go/bin/go
bash tests/research/t0_realworld/integration/run.sh /tmp/review-integration-new
bash tests/research/t0_realworld/integration/compare_direct.sh /tmp/review-integration-new/app /tmp/review-direct-new
bash tests/research/t0_realworld/macro/run.sh /tmp/review-macro-new
```

最后一个脚本在基线上**预期退出 1**，报告两个卫生行为失败及类型案例的 vet 失败；其它正常检查继续执行。不能把此脚本非零退出描述成全部通过。

- [integration](integration/)：真实两模块、多包业务，源宏→JSON→enum/match→Option/Result/?→Error/From，跨模块显式 Default/Clone/Debug/PartialEq/Eq，新增性质、差分与优化 benchmark。
- [macro](macro/)：最小卫生复现、4096 输入控制流差分、限时 fuzz、运行期与编译期基准。
- [官网交接](PUBLICATION_HANDOFF.md)：可宣传能力、必须标明的边界、真实源与生成 Go 示例。
- [独立复核](INDEPENDENT_REVIEW.md)：另一审查者的验证与反证意见。

## 已执行与边界

双模块生成 Go race/vet、优化 build 与运行通过；781 × 4 × 2 的业务差分、351 组 JSON业务、nil/typed-nil/sentinel cause、Clone/equals 状态性质通过。最终版本 5 秒 fuzz 133,920 次；宏控制流 4096 输入及 3 秒 fuzz 145,905 次通过。生成代码对照实验再次 race/vet 通过。完整日志分别在两个 results 目录。

六个生产包 macros/enums/deriveextras/serdejson/serde/formatting 的针对性本地 race 回归通过。未运行全树 acceptance、CI、长时间 fuzz、非宿主平台或新 Go 适配宏验收。未把已有源码映射、宏语法、预编译宏 ABI 等已知边界重报为新缺陷。

公开 enum 参数提前验证、blank/embedded derive、serde adapters 均保持未决；未引入 Copy/move、借用检查或 enum ABI 改变。普通 Go 浅复制与显式 Clone、nil/非 nil 空容器不等的既有语义保持。

环境记录：工作区和当前仓库未发现 `AGENTS.md` 或 `.agents/skills`；已阅读实际功能 README 与验收边界。首次构建默认 Go cache `/home/agent/.cache/go-build` 因只读失败；随后仅将缓存设到允许写入的 `/tmp`。一个独立 serde 委派被平台内容风险检查终止，未重试该委派或绕过；其独立专项结论缺失。主任务已完成上述组合 serde 流程和本地 serde 包回归，不将其等同于独立 serde 专项审查。无外部依赖下载。

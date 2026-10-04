# 审查后的生产修复（2026-10-04）

用户继续授权在独立分支修复并推送。最初审查快照已先推送并核验为 [`09a524a6bae0f9f013376c22cbadfb14f94facd2`](https://github.com/hxsf/goxide/commit/09a524a6bae0f9f013376c22cbadfb14f94facd2)，供网站任务引用。后续仍只在 `review/t0-realworld-20261004` 工作，没有向 `feat/t0-completion` 写入或 merge。

## 实际修复

独立可复用提交：

- [122caf3：宏 primitive 冲突诊断](https://github.com/hxsf/goxide/commit/122caf301259db102da8dc8791b691b277347840)。
- [69cdd28：跨模块 Result 静态 validator](https://github.com/hxsf/goxide/commit/69cdd28d8d5f17574604889165dcff913c5d3d43)。

**宏 primitive 捕获：阻止静默错误，使用明确诊断。**

`internal/macros/primitive_bindings.go` 检查定义点声明/import、调用包声明和当前调用文件 import 是否会重绑原始 primitive。普通 alias 生成前以及固定 Option/Result 参数被替换前均检查。冲突报告定义与调用位置；不输出可继续使用的展开结果。caller-local shadow、模板局部、显式 capture、已有非泛型 helper 函数仍按原路径处理。没有重命名用户 named type。

这是防错修复，不是完整支持所有 primitive 名称冲突。当前包级/文件 import 冲突的工程边界仍需官网诚实说明；后续完整绑定可单独设计。本组也检查实际定义点是否为预声明对象，避免仅依据 `int/true` 字符串误认身份。

**跨模块 Result `?`：保留可信 validator 的静态目标。**

`internal/frontend/imported_family.go` 不再把各 instance validator 预先装进函数变量，而是登记原定义包的 qualified 目标。`internal/propagate/lower.go` 在具体 nominal 类型已经匹配且可命名时，先创建包级类型 alias，再生成具名 typed forwarder。这样既隔离调用者局部 import/type shadow，又让 Go 分析 validator 和成功载荷的逃逸。

producer 的具体校验函数、调用位置、nominal instance 选择、非法/typed-nil/forged case 的拒绝以及嵌套 payload 检查均保留。公开 enum ABI、构造器和用户语义未变。该改动同时沿用既有 Option 的 imported-family 路径，但不宣称所有 enum/derive helpers 都已经静态化；独立 enum 的 `imported.go` 未改。

新增真实双模块 allocation gate：1、32、1024 个非小整数输入，允许至多一个最终 Result 分配，禁止逐项 Result 逃逸。原始 1024 项 8200B/1025alloc，生产修复输出 8B/1alloc；这不是对任意载荷/工具链的零分配承诺。

独立 reviewer 发现并关闭了删除旧声明时带来的 EOF 拼接回归：prelude 现在显式以换行开始，所有 consumer 正反例移除文件末尾换行，既检查合法源编译也保持原约束诊断。未仅放宽失败期望。

**AST 二次扫描未优化。** 本轮没有取得隔离扫描计数/profile 与修复前后等价性证据，不扩大变更；原审查证据继续作为后续候选。

## 验证与独立 review

本地完整 `go test -race -count=1 ./...` 已退出 0（acceptance 339.951s），`go vet ./...` 通过，设计检查 13 条 Go 命令通过。见 [全树 race](results/fixes/full-race.log)、[vet 输出](results/fixes/full-vet.log)及 [受测源文件哈希/命令清单](results/fixes/manifest.json)。这是本地完整检查，不称为 CI。

实际生产修复的单 P 优化 benchmark（200ms × 3，仍有共享机器负载）1024 项为 2648–3045ns/op、8B/1alloc；手写 Go 对照 902.5–1115ns/op、0B/0alloc。见 [原始数据](results/fixes/production-bench.log)和 [内联/逃逸摘录](results/fixes/production-escape-excerpt.log)。这些数字来自正式编译器重新生成的代码，不是旧的 direct_calls.py 实验。

- [FIX_REVIEW.md](FIX_REVIEW.md)：不同审查者复核两个实际修复，发现/关闭 EOF 回归，重新执行 Result 与宏聚焦 acceptance、生成 Go race/vet、allocation gate；当前无剩余阻塞。
- [宏修复日志](macro/results/fix-validation.log)：最小反例现在准确拒绝、正常控制流 race/vet 与 3 秒 fuzz（135854 次）通过，脚本退出 0。
- [双模块修复日志](results/fixes/integration.log)：实际生产编译器重新生成 model/app，race/vet、781×4×2 差分、351 JSON 组合、nil/typed-nil cause、5 秒 fuzz（67379 次）、优化 build/CLI 通过。
- [独立聚焦验收完整日志](results/fixes/independent-acceptance.log)、[主任务跨模块检查](results/fixes/crossmodule.log)、[设计检查](results/fixes/design.log)保留可复核输出。

原始研究日志仍保留，不用后续通过结果覆盖初始错误。测试脚本打印 checkout 的 HEAD 为 `09a524a` 时，生产修复尚在工作树；这不是声称初始审查提交本身包含修复。后续正式提交含这套受测补丁。

## 适配宏与网站任务交接

更新：已 fetch 并验证新宏提交 `ffc9b9b`；实际重叠为 hygiene.go、propagate/lower.go 和宏 README，三方自动合并无冲突，组合验收通过。`sums.go` 未出现在该新宏 diff 中。详见 [准确提交、合成树与安全集成步骤](INTEGRATION_FFC9B9B.md)。

本修复在 `internal/macros/sums.go` 的准确位置：在 `prepareSums` 的 `fixedType(arg)` 检查成功后、`args = append(args, arg)` 前新增 7 行，对 `ts[start:j]` 中 primitive token 调用 `checkPrimitiveBinding`。若适配宏增加新的类型处理入口，应复用这个检查，避免绕过；本任务已对准确的 `ffc9b9b` 合成树执行聚焦验证，但未覆盖对方以后新增或未提交改动，亦未在合成树重跑全树 race。

其它生产文件：`internal/macros/hygiene.go`、新增 `primitive_bindings.go`、`internal/frontend/imported_family.go`、`internal/propagate/lower.go`。它们的 diff 和回归可独立审查/复用。没有实现 Go `(v,error)` 适配宏，没有改网站。

官网请读取更新的 [PUBLICATION_HANDOFF.md](PUBLICATION_HANDOFF.md)：将 primitive 碰撞表述为明确诊断的限制，将 Result 优化表述为本例消除逐项分配，不宣传完整宏卫生或零成本抽象。原始 baseline `.hgo`→Go 摘录保留原版本标识。历史 `compare_direct.sh` 只适用于 `3098a30` 的生成产物；当前生产修复用 `integration/run.sh` 直接验证。

独立 serde 委派原被平台内容风险检查中止，原因是该委派内容被标记为潜在 cybersecurity risk；没有重新路由、重试或绕过，也没有将该专项称为通过。既有主任务组合 serde 流程与 Go serde 包回归是分别执行的覆盖。后续 git push 未遇权限拒绝。

# T0 宏独立实用性审查（2026-10-04）

基线 `3098a30e8e5fcb4696687246b2f39988218f4006`；Go 1.27.1 linux/amd64。
最初独立审查仅新增研究案例；其后获得修复授权，现已补充 primitive 绑定诊断与回归测试，未在此子任务提交或推送。

修复后的 `run.sh` 接受带定义/调用位置的明确拒绝，也接受未来真正保留定义点语义的成功执行；不再接受静默结果变化。历史原始失败日志保持在 `results/review.log`，修复验证另存 `results/fix-validation.log`。

## 首要发现：跨包 primitive aliases 丢失定义点绑定

`shadow_constants/ops/macros.hgo` 导出的宏分别返回 `true` 和 `false`。
调用包合法声明反向同名常量后：

- 预期：`enabled=true disabled=false`。
- 实际：`enabled=false disabled=true`；生成 Go 的 race 执行成功，vet 也成功，结果静默改变。

`shadow_types/ops/macros.hgo` 的宏返回 `int(0)`。
调用包声明 `type int string` 后：

- 预期：`value=0 type=int`。
- 实际：`value="\x00" type=main.int`；race 执行成功。
- 生成 Go 的 vet 报出 integer-to-string conversion，但 hgo 编译本身成功且输出类型已改变。

两个案例是同一缺陷的常量和类型表现，不重复计为两个独立漏洞。
`internal/macros/README.md` 明确承诺 primitive types/constants 定义点 aliases，
这里也没有使用已知不支持的 free names、generic helper、预编译宏或任意 tt 注入。
文档列出的 caller package shadow 限制仅对 `len/cap` 做了显式诊断，不能解释这里的静默重绑定。

**来源**：`internal/macros/hygiene.go:193-198` 将原始名转成 alias key；
`internal/macros/macros.go:206-209` 在调用者源文件追加 `type alias = int` / `const alias = true`，
RHS 因而重新解析到调用包作用域。

**最小建议**：先检测 primitive alias 与调用包顶层同名声明冲突并发出包含定义/调用位置的诊断，
避免静默错编译；完整支持再将此类 alias 绑定到定义包的可信生成 helper/exported alias。
不能简单重命名用户 named type，因为会改变其反射身份。
常量可在确认定义点确实是预声明 true/false 后生成不依赖名字的布尔常量表达式。
现已实现上述保守诊断，并验证定义点该名字确实未被声明/import 遮蔽；同样保护固定 Option/Result 类型参数。没有擅自决定永久不支持此场景。

## 次要发现：重复 imported helper 的卫生检查反复扫描整棵 AST

真实 `.hgo`：`perf/repeated_imported.hgo` 和语义等价的 `perf/repeated_captured.hgo`，
每个宏展开含 512 次 `strconv.Itoa`；两个案例均已成功 transpile。
`expansion_bench_test.go` 进一步隔离 `macros.Expand`，比较 imported helper 和同包 helper。

**来源**：`internal/macros/hygiene.go:63-68` 为每个 imported selector 再调用一次
`ast.Inspect(root, ...)` 来判断它是否是直接调用；即使找到也继续遍历。
N 个选择器、O(N) 大小的模板因此产生 O(N²) 的遍历工作。

首次本地两次 `-benchtime=1x` 的 2048 项宏展开：

| 形式 | 时间 | 分配字节 |
| --- | --- | --- |
| 同包 helper | 96–110 ms | 84.8–84.9 MB |
| imported helper | 505–592 ms | 104.1 MB |

完整复跑日志在 `results/review.log`，复跑与其它审查测试并行，存在明显时间噪声，
不能把单次数值当稳定性能门槛；O(N²) 来源可直接从实现确认。
这些数字只涉及编译期宏展开，不是生成 Go 的运行期开销。对照还包含 Go export-data/import resolution 成本，不能把整个 Expand 的时间差全归因于 AST 扫描；源码确认的是其中的 O(N²) 子过程。
完整 CLI 的 2048 项样例也测得 imported 3.0–3.2s、explicit capture 2.3s，
其包含下游各编译步骤，不能全部归因于此单点。

**最小建议**：第一次 AST 遍历建立 `map[ast.Expr]bool` 的 `CallExpr.Fun` 集合，
selector 只查集合；可复用后续 calls 收集的思路，但其当前键为 `*ast.Ident`，需扩展为 `ast.Expr` 或另建集合。保留现有 type/export/generic 检查。
未做生产修复，不能宣称已测得修复后收益。

## 运行方式与已执行检查

```bash
GO_BIN=/workspace/goxide-environment/go/bin/go \
  bash tests/research/t0_realworld/macro/run.sh /tmp/t0-macro-review-new
```

脚本构建 CLI，执行两个卫生复现、生成 Go 的 race/vet、差分/fuzz 和 benchmark。
原始基线执行最终**返回 1**：2 个预期行为断言失败，外加类型复现的 vet 失败，共 3 项。更新后的脚本把明确且带位置的拒绝视为修复成功。
原始完整日志：`results/review.log`；生成 Go 和映射：日志中的 `/tmp/t0-macro-final`。

`differential/flow.hgo` 组合显式 once closure、disabled 分支、break、return、defer、
调用者/模板同名局部，与手写 Go 对照：

- 4096 种输入的有界差分，生成 Go `test -race` 通过；`go vet` 通过。
- `FuzzFlow` 3 秒、2 workers 完成 145905 次执行，无差异（前一次为 139886 次，也通过）。
- 默认优化生成 Go 与手写 Go 均为 `120 B/op, 4 allocs/op`；时间随并行负载波动，
  未观察到可靠的宏额外运行成本。最终日志约 450–601ns/op，较空闲首跑约 142–167ns/op。
- 两个卫生案例确实用 `go run -race` 执行；其中无测试文件，未把无测试的 `go test` 当行为验证。
- 未执行仓库全套 CI、非宿主平台、长时间 fuzz，也未审查正在并行开发的 Go `(v,error)` 适配宏。

宏 README 中的已知限制（nil/free names、复杂 tt、宏内任意 enum 构造等）未当作新增漏洞。

## 授权后修复验证

新增生产检查位于 `internal/macros/primitive_bindings.go`，由 `hygiene.go` 和
`sums.go` 在创建 primitive alias / 固定类型 helper 前调用。未修改 AST selector 性能路径。

已执行并通过：

- `go test ./internal/macros ./tests/acceptance -run 'Test(MacroPrimitive|Primitive)' -count=1`
- `go test ./internal/macros -race -count=1`（`results/fix-macros-race.log`）
- `go test ./tests/acceptance -run 'TestMacro' -race -count=1`（`results/fix-acceptance-race.log`）
- `go vet ./internal/macros ./tests/acceptance`
- 更新后的 `run.sh`，退出 0；4096 差分、生成 Go race/vet、3 秒 fuzz 135854 次通过（`results/fix-validation.log`）。
- `git diff --check`

单元回归覆盖定义点和调用点的跨文件 primitive 常量/类型声明、固定 sum 类型参数、
显式/隐式 import 名字与文件作用域；验收覆盖真实 .hgo + 原生 .go 模块组合，以及局部
同名类型保持反射名称、显式 capture、既有 definition-site 函数 helper。
这里只报告本地执行；完整仓库回归与独立复核由主任务统一负责。

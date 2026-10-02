# 私有赋值 guard 与正常控制流研究

历史状态：`a3f064fbcd0e75bb06bdba406f007e5abd68bd71` 功能路径已接通，**性能验收未通过，私有目标缺口未收口**。这里没有实现新的编译器 IR，也没有批准延期。修正后的 guard 识别只遍历真正存储路径；只读 index/key/调用参数不需要 guard。不将该机制扩到循环。

## 重现

从任意 checkout 执行（需要 Go 1.27+、Python 3.10+、race 支持）：

```sh
python3 tests/research/assignment_guard/run.py --go /path/to/go --out /path/to/new-output
```

输出目录的 source/generated 必须尚不存在。脚本从当前 checkout 构建 hgo，转译仓库内 source，复制测试、分别执行生成模块 race、legacy panicnil 配置、vet、escape analysis 和非 race 基准，记录命令、版本、工作树状态及统计。GOPROXY=off，不下载第三方模块；Go 路径可显式指定，避免误用同名游戏。生成代码私有标识改变时需要同步手写探针的具体 enum 名字，编译失败不会被跳过。

- [source/benchmark.hgo](source/benchmark.hgo)：真实 hgo Guard1/Guard8、单条八问号 PackedGuard，和相同 Result/validator/Err 重建的显式 match 基线。
- [source/index_probe.hgo](source/index_probe.hgo)：真实 hgo 只读索引与显式基线。
- [guard_bench_test.go.fixture](guard_bench_test.go.fixture)、[extra_test.go.fixture](extra_test.go.fixture)：输入在计时前构造、结果写全局 sink；panic identity、用户 defer panic、旧 Goexit/问号组合回归。
- [alternatives_test.go.fixture](alternatives_test.go.fixture)：**纯 Go 机制探针**，不是 hgo 编译器实现。观测预期 nil panic 使用 recover，不用于候选正常控制流。

另可在输出目录的 generated 内重做汇编/分配归因（以下使用同一真实 Go 工具链；profile 与常规计时分开）：

```sh
go test -c -o ../audit.test
go tool objdump -s 'places.example/update.Guard1' ../audit.test > ../guard1-asm.txt
go test -run '^$' -bench '^BenchmarkGuard8FailLast$' -benchtime=10000x -memprofile=../alloc.pprof -memprofilerate=1 -o ../profile.test
go tool pprof -top -alloc_objects ../profile.test ../alloc.pprof
go tool pprof -list=runtime.recovery -alloc_space ../profile.test ../alloc.pprof
```

结果文本入库时仅去除行尾空白，数值和诊断内容保持原样。

## 测量与成本归因

Go 1.27.1 linux/amd64，AMD EPYC 9V74 云 VM，GOMAXPROCS=1。数字为非 race ns/op 中位数；同机微基准不代表所有应用。原审计 200ms × 5，见 [原始结果目录](results/a3f064f/summary.json)和[原始 bench](results/a3f064f/bench.txt)。以下结果不把不同 workload 的原生 `(int,error)` 基线用于计算 guard 开销。

| 路径 | guard ns/op | 显式 match ns/op | guard B/alloc | 显式 B/alloc |
| --- | ---: | ---: | --- | --- |
| 单条成功 | 22.43 | 2.85 | 32/1 | 0/0 |
| 单条失败 | 280.1 | 25.17 | 48/2 | 16/1 |
| 八条成功 | 158.4 | 13.26 | 256/8 | 0/0 |
| 八条末条失败 | 491.3 | 32.1 | 296/10 | 16/1 |
| 单条八问号成功 | 31.97 | 13.21 | 32/1 | 0/0 |
| 单条八问号末问号失败 | 287.4 | 31.28 | 48/2 | 16/1 |

[escape 摘录](results/a3f064f/escape-excerpt.txt)显示 token 地址进入 panic 接口令 guard 逃逸，即使成功也有 heap allocation。源函数含 defer 不能内联，Catch 含 recover 也不能内联，但显式基线部分函数也因复杂度不能内联，不能把时间差全部归因于内联。八条末次失败额外 24 B/1 alloc 的 [runtime recovery 证据](results/a3f064f/recovery-allocation.txt)来自 alloc profile 的 runtime.recovery/savedOpenDeferState；它不是第九个 guard。调试器与 profiler 可观察内部 panic。现有探针没有穷尽所有 From panic/Goexit/用户 defer 组合。

后续路由修复的[结果](results/routing-fix/summary.json)与 [manifest](results/routing-fix/manifest.json)为 100ms × 3 快速复核；与整树检查并行，存在系统负载干扰，不用于精确耗时比较。真实 hgo 索引成功 0 B/0 alloc；同次重测真正私有目标的 guard 仍有上述分配。手写 ContextIR 成功 0 B/0 alloc、失败 16 B/1 alloc，仅表明该子集无需 guard；它与 Guard1 workload 不同，不用于加速倍数宣传。GenericCapture 微基准可高度优化且语义不合格，不能作为通用方案性能依据。

## 候选及反例

1. **提前地址捕获**：`p := &tree.Wide; if failed { return }; *p = v` 对 nil leaf 在传播失败前 panic；原失败应跳过最终写入。`capture[T](&tree.Wide)` 可以推断私有 mask，却仍先求值地址，泛型不会解决时机。
2. **RHS 后统一取址**：即使 RHS 成功，`p := &tree.Count; first, *p = 5, 7` 在 nil 时 first 仍是 0；原 `first, tree.Count = 5, 7` 则先写 first=5。它违反部分写入，不是完整两阶段 lowering。
3. **延迟 setter/getter**：闭包重新走根链，会在前一写项替换隐藏 leaf 后写新 leaf；原多赋值应写第一阶段确定的旧 leaf。把隐藏指针作为泛型参数仍需先取到它；调用方无法拼写私有链，Go 函数字面量也不能省略返回类型来推断通用私有指针 getter。没有以反射/unsafe/导出私有字段/生产包接口规避限制。
4. **保留完整赋值的副作用 IR**：先执行明确的传播/短路控制流，最终保留 `first, tree.Wide, tree.Enabled = 5, 1<<n, cond==true`。纯 Go 探针证明 63 位 shift、未类型化 bool、失败不写及最终 nil 前项写入可同时保持。此路线尚未实现为通用 compiler pass，不能宣称收口。

第四项需保留源表达式的原始 typed/untyped 属性：`box(tree.Enabled && other.Enabled)` 推断私有命名 bool，而替换成 `box(cond==true)` 推断 builtin bool；接口断言反例已测试。仅使用上下文处理后的 go/types.TypeOf 不足。还需处理 callee/实参顺序、tuple/comma-ok、源直接 recover、len/cap 去调用后的求值属性，以及副作用子表达式与隐藏目标的相互作用。继续这项工程研究不需要用户批准新语义；没有证明该路线不可行，也没有承诺通用实现已经完成。


## questionFlow 基础重构复核

在 `ec437f2a12dc40671cb474f7cd2a0efc40cf5db1` 上提取传播节点后，仍未替换真正私有目标的 guard。[同机 200ms × 5 结果](results/question-flow/summary.json)与 [manifest](results/question-flow/manifest.json)记录本次工作树。整树测试结束后执行基准；单条 guard 成功 22.17 ns、32 B/1 alloc，对照显式 match 2.922 ns、0 B/0 alloc；失败 280.0 ns、48 B/2 alloc，对照 27.05 ns、16 B/1 alloc。八条末次失败仍 296 B/10 alloc。不以波动宣称性能改善。

从上述基线 commit 的 git archive 构建旧 CLI（归档无 git metadata，使用 go build -buildvcs=false），与新 CLI 对相同 source 分别转译。[研究模块](results/question-flow/research-token-comparison.json)及 [private 业务模块](results/question-flow/private-token-comparison.json)的 Go token 序列均一致，只有排版/源位置差别；比较包含 Go 扫描器插入的分号，不是简单删除空格。相同 token 不能证明源位置诊断不变，诊断仍由既有 hgo 回归验证。比较工具可重用：

```sh
go run tests/research/assignment_guard/tools/compare_tokens.go BASELINE_GENERATED CURRENT_GENERATED
```

它双向检查非 `_test.go` 的 Go 文件集合并比较 token，忽略注释和位置；测试夹具由 runner 另行安装，不纳入编译器产物比较。空基线、文件集合差异、扫描错误或 token 差异均失败。忽略的注释也包含 Go 编译指令，因此这只是本次产物的结构证据，不能单独证明任意程序等价。新增 questionFlow 出口机制测试属于单独的生成 Go 研究，不把它当作本目录 Guard 基准或新增 hgo 功能。


## effects planner：首批真实无 guard 结果

基于 `249567c6055f3f7d3ab6d506d9ea39100843c671` 的后续 effects 工作树，[原始结果](results/effects/bench.txt)、[统计](results/effects/summary.json)和 [manifest](results/effects/manifest.json)保存本次 Go 1.27.1 同机 GOMAXPROCS=1、200ms × 5 测量。全树回归结束后单独执行。Guard1/Guard8/PackedGuard 保留历史函数名，但当前由真实 hgo planner 生成普通 return，不含 Assignment/Catch/Fail；六项手写 Go 机制探针仍仅为机制证据。

```sh
python3 tests/research/assignment_guard/run.py --go /path/to/go --out /path/to/new-output --expect-no-guard
```

新增开关同时验证 benchmark.go 的无 guard 形状，以及上述三种成功基准的全部重复样本均为 0 B/0 alloc，并核对样本数。开关默认关闭，以便仍可使用脚本研究旧 compiler 的成本。

| 真实 hgo 路径 | planner ns/op | 显式 match ns/op | planner B/alloc | 显式 B/alloc |
| --- | ---: | ---: | --- | --- |
| 单条成功 | 2.608 | 2.437 | 0/0 | 0/0 |
| 单条失败 | 25.4 | 24.43 | 16/1 | 16/1 |
| 八条成功 | 9.399 | 12.18 | 0/0 | 0/0 |
| 八条末次失败 | 28.32 | 30.99 | 16/1 | 16/1 |
| 单条八问号成功 | 9.726 | 13.86 | 0/0 | 0/0 |
| 单条八问号末次失败 | 30.78 | 32.31 | 16/1 | 16/1 |

成功路径此前的每条 32 B guard 分配已从这些路径消除；失败仍有与显式 match 一致的 16 B/1 次 Result 重建。计时受工具链/虚拟机及内联差异影响，不用单次表格推广所有应用性能。此结论不覆盖仍走 guard 的形状：完整清单和真实 unsafe/checkptr 反例见 [RFC §6.9](../../../docs/rfcs/research/0005-option-result-question.md#69-首批无-panic-完整赋值-lowering)。基准源/model 是固定研究快照，与扩充的业务 acceptance 原生 model 各自独立。


## context 第二组：私有 bool、signed shift、len/cap

基于 `693282c64f0868cb38beaacd98c113e2aba029b1`，新增 [真实 hgo context 源](source/context.hgo)与 [分组基准](context_bench_test.go.fixture)。Bool 的显式基线在左条件失败时跳过右臂；Shift 基线保留同一原生负数检查；三组都用相同 Result/validator/Err 重建，输入在计时前构造。没有用原生 `(int,error)` 或手写 IR 探针替代此对照。

[原始数据](results/context/bench.txt)、[统计](results/context/summary.json)及 [manifest](results/context/manifest.json)记录 Go 1.27.1 同机 GOMAXPROCS=1、200ms × 5；全树测试结束后单独运行。`--expect-no-guard` 现在检查 benchmark.go 和 context.go，两文件无 Assignment/Catch/Fail，原三组与新三组共六组成功路径的全部样本均为 0 B/0 alloc；同时检查样本数。

| 真实 hgo 路径 | planner ns/op | 显式 match ns/op | planner B/alloc | 显式 B/alloc |
| --- | ---: | ---: | --- | --- |
| 私有 bool 成功 | 2.666 | 2.581 | 0/0 | 0/0 |
| 私有 bool 失败 | 22.64 | 24.26 | 16/1 | 16/1 |
| signed shift 成功 | 3.109 | 3.57 | 0/0 | 0/0 |
| signed shift 失败 | 25.51 | 22.34 | 16/1 | 16/1 |
| len 成功 | 2.457 | 2.595 | 0/0 | 0/0 |
| len 失败 | 26.26 | 26.67 | 16/1 | 16/1 |

这些有限微基准的时间差不能推广所有应用；已证明的是所测路径没有额外成功分配，失败分配与显式 match 相同。研究生成模块共 11 项 race 测试，其中 6 项仍是手写 Go 机制探针；新增 TestContextBenchmarkEquivalence 比较真实 hgo 两路径的结果和目标状态。业务 acceptance 另覆盖 witness 未调用、nil、负 shift 原生 runtime.Error、private 参数及 cap/keyed literal。tuple/unsafe 与其余未规划形状仍有 guard；精确范围见 [RFC §6.10](../../../docs/rfcs/research/0005-option-result-question.md#610-私有-bool上下文-shift-与-lencap-第二组)。

## Third group: assignment multi-values

The [multivalue source](source/multivalue.hgo) is compiled by the real hgo CLI.
Its static tuple and map comma-ok benchmarks compare the same preconstructed
Result inputs with explicit match, including the same error reconstruction.
The [raw results](results/multivalue/bench.txt), [manifest](results/multivalue/manifest.json)
and [summary](results/multivalue/summary.json) record the parent revision plus
uncommitted stage changes, Go version and exact commands. Reproduce with the
existing `run.py --count 5 --benchtime 200ms --expect-no-guard` command.

| Case | Planner median ns/op | Explicit match median ns/op | Both B/op, allocs/op |
| --- | ---: | ---: | --- |
| Tuple success | 2.506 | 2.993 | 0, 0 |
| Tuple failure | 23.69 | 28.51 | 16, 1 |
| Map success | 6.030 | 6.725 | 0, 0 |
| Map failure | 29.76 | 24.88 | 16, 1 |

Every success sample in all eight checked groups allocates zero. These short
benchmarks establish the allocation gate for the covered shapes, not a general
speedup claim. All three generated implementation files omit guards. The research
module has 12 tests: six hand-written Go mechanism probes remain separate from
actual hgo tests. Race, legacy panicnil, and vet pass; the unsafe chain regression
remains in the installed business module and still uses the guard path.

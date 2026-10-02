# 私有赋值 guard 与正常控制流研究

状态：`a3f064fbcd0e75bb06bdba406f007e5abd68bd71` 功能路径已接通，**性能验收未通过，私有目标缺口未收口**。这里没有实现新的编译器 IR，也没有批准延期。修正后的 guard 识别只遍历真正存储路径；只读 index/key/调用参数不需要 guard。不将该机制扩到循环。

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

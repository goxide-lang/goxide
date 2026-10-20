# Enum 表示实验

这里保留转译器 enum 表示的对照实验及三轮历史测量。它是独立 Go module，依赖并排 checkout 的 `syntax/token`；编译器主 module 与 SDK 的生产表示不受影响。

- [enumbench](enumbench)：七种空变体的 `uint8+iota`、`int+iota`、Goxide 生成的指针密封接口，以及手写值变体与 `Type()` 分派实验。
- [tokenbench](tokenbench)：与 `token.Token` 字段布局一致的平铺结构，和“公共元数据 + Goxide enum payload”的对照。
- `.gox` 与生成的 `_gox.go` 一起保留。空值变体、`Type()` 是手写实验，当前 Goxide 仍按[已确认规范](../../docs/design/enum-representation.md)生成指针变体。

## 生成与验证

要求 Go 1.27，`syntax/` 与 `goxide/` 位于同一父目录；[go.mod](go.mod) 的本地替换只用于此实验。从 `goxide/` 开始：

```sh
export GOTOOLCHAIN=local GOWORK=off
go build -o bin/goxide ./cmd/goxide
export GOXIDE="$PWD/bin/goxide"
cd benchmarks/enum
go generate ./...
go build ./...
go test -race -count=1 ./...
go vet ./...
gofmt -l enumbench tokenbench
```

`go:generate` 直接使用 `GOXIDE` 指定的可执行文件；比较指定编译器时，把它设为对应构建的绝对路径。Go 会展开该变量，不经 shell。测试覆盖有效值结果、nil/typed-nil/非法值、输入分布、Token 字段与递归布局、构造所有权。检查重复生成：

```sh
snapshot=$(mktemp -d)
cp enumbench/unit_gox.go "$snapshot/unit_gox.go"
cp tokenbench/payload_gox.go "$snapshot/payload_gox.go"
go generate ./...
cmp enumbench/unit_gox.go "$snapshot/unit_gox.go"
cmp tokenbench/payload_gox.go "$snapshot/payload_gox.go"
```

## 重现测量

在本 module 运行。历史数据来自 Apple M2、Go 1.27.1、darwin/arm64，7 次采样，每次至少 300ms，单核、正常优化构建。第一、二轮使用默认 PGO 设置，第三轮显式关闭 PGO；性能采样不加 race。

```sh
export GOTOOLCHAIN=local GOWORK=off
export GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off TMPDIR=/private/tmp

# 第一轮：数值表示、生成的密封接口、Token 布局
go test -p=1 -cpu=1 -run '^$' \
  -bench '^Benchmark(Unit(Construct|Dispatch|BatchBuild|Traversal)|Build|Traverse|CopyShallow)$/(Numeric8|NumericInt|Sealed|Program|ProgramX32)$' \
  -benchmem -benchtime=300ms -count=7 ./enumbench ./tokenbench

# 第二轮：增加手写空值变体
go test -p=1 -cpu=1 -run '^$' \
  -bench '^BenchmarkUnit(Construct|Dispatch|BatchBuild|Traversal)$/(Numeric8|Sealed|SealedValue)$' \
  -benchmem -benchtime=300ms -count=7 ./enumbench

# 第三轮：类型 switch 与 Type() 方法，分别测乱序、分组和单一变体
go test -pgo=off -p=1 -cpu=1 -run '^$' -bench '^BenchmarkUnitDispatchStrategy$' \
  -benchmem -benchtime=300ms -count=7 ./enumbench
```

以上选择当前源码中与各轮对应的 workload；历史原始命令保留在结果目录。迁移改变了 Go 包路径和生成 panic 文本，Go 类型信息及分派布局也可能随包路径、工具链变化，复跑结果应单独比较。

`enumbench` 的乱序输入固定为 7168 个值，每种变体 1024 个。Construct 包含运行时类别转换及工厂分支；Dispatch 每次处理一个值，BatchBuild/Traversal 每次处理整批，比较时用 `ns/value`。输入准备均不计时，表示的构造只在 Construct/BatchBuild 中计时。`B/slot` 是切片元素宽度，`B/op` 是实际分配，分配器取整会使两者比例不同。

`tokenbench` 的 Program 来自编译器 `clone_debug` 示例：42 个根节点、递归共 576 个节点；ProgramX32 重复该输入，1344 个根节点、18432 个节点。Build/Traverse 的分母是 `nodes/op`，CopyShallow 的分母是 `roots/op`。计时排除词法分析、校验、字符串和共享引用准备；Build 包含全部节点数组及候选 payload 分配，CopyShallow 共享子树和 payload。该工作负载覆盖 token 布局操作，整条解析、JSON 编解码和真实宏吞吐需要另行测量。

## 历史结果

下表都取各轮 7 次采样的中位数。每个结果目录原样保留 `benchmarks.txt`、`medians.json` 和环境文件；原有 `syntax/internal/...` 路径是当时的测量位置。迁移只重新生成和验证代码，没有重跑这些性能采样。

| 轮次 | 实验源码提交（syntax） | 原始输出 | 汇总 | 环境 |
| --- | --- | --- | --- | --- |
| 基线与 Token | `9aa3c44f84f4fcfa28366c15cd363ff70dbd0c19` | [文本](results/2026-10-20-baseline/benchmarks.txt) | [JSON](results/2026-10-20-baseline/medians.json) | [文本](results/2026-10-20-baseline/environment.txt) |
| 空值变体 | `fc4408f25f23e199fed09f6f20e805dbd64dce86` | [文本](results/2026-10-20-empty-value/benchmarks.txt) | [JSON](results/2026-10-20-empty-value/medians.json) | [JSON](results/2026-10-20-empty-value/environment.json) |
| Type() 分派 | `253a349330a255eeb3ba4cb6c45172e46ae89c14` | [文本](results/2026-10-20-type-method/benchmarks.txt) | [JSON](results/2026-10-20-type-method/medians.json) | [JSON](results/2026-10-20-type-method/environment.json) |

生成编译器基线是 `a18780ef9cb82a7a6248db605ba6a54bc54f2016`，SDK 初始基线是 `1b7ffb08c84163d091c0f247566e3ab5f0f87337`。三轮独立采样应各自比较，跨轮差值不作为优化收益。

### 空变体基线

| 表示 | Construct ns/value | Dispatch ns/value | BatchBuild ns/value | Traversal ns/value | B/slot | 批量 B/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| uint8+iota | 1.971 | 4.862 | 0.5149 | 1.627 | 1 | 8192 |
| int+iota | 1.999 | 5.318 | 0.7882 | 1.559 | 8 | 57344 |
| 指针密封接口 | 5.257 | 10.32 | 6.897 | 8.148 | 16 | 114688 |

空结构体指针的单值构造为 0 allocs；批量构造三者均只有切片的一次分配。这个实验中的主要成本差异包括接口元素宽度、构造分支和分派代码。零大小变体没有每元素堆分配，不能把携带 payload 的结果套用到这里。

### Token 布局

| 工作负载 | 平铺 ns/op | enum payload ns/op | 平铺 B/op | enum payload B/op | 平铺 / enum allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| 构造 Program | 36981 | 27095 | 130112 | 58169 | 89 / 665 |
| 构造 ProgramX32 | 1281311 | 1062651 | 4155392 | 1853218 | 2817 / 21249 |
| 遍历 Program | 2244 | 1615 | 0 | 0 | 0 / 0 |
| 遍历 ProgramX32 | 76011 | 54413 | 0 | 0 | 0 / 0 |
| 浅拷贝 Program | 1942 | 681.2 | 9472 | 3072 | 1 / 1 |
| 浅拷贝 ProgramX32 | 65555 | 26444 | 294912 | 90112 | 1 / 1 |

本机平铺 Token 为 216 字节，候选头为 64 字节，后者还需单独 payload。此样本的候选表示减少字节量，并降低构造、遍历和浅拷贝耗时，同时明显增加分配次数；收益取决于 payload 大小、token 分布及访问方式。

### 手写空值变体

| 表示 | Construct ns/value | Dispatch ns/value | BatchBuild ns/value | Traversal ns/value | B/slot |
| --- | ---: | ---: | ---: | ---: | ---: |
| uint8+iota | 1.964 | 4.785 | 0.5106 | 1.502 | 1 |
| 指针密封接口 | 4.822 | 10.22 | 6.633 | 8.206 | 16 |
| 值密封接口 | 4.391 | 9.613 | 5.622 | 7.065 | 16 |

值变体保留 16 字节接口槽位；两种接口的批量构造均为 114688 B、1 alloc。本轮值变体有所改善，仍高于数值表示；单值构造的样本范围有重叠，详见汇总 JSON。值接收者的 Go 方法集与指针接收者不同，生产表示的调整还需要语义设计。

### Type() 分派

| 输入顺序 | uint8 | 指针 type switch | 值 type switch | 指针 Type() | 值 Type() |
| --- | ---: | ---: | ---: | ---: | ---: |
| Shuffled | 1.493 | 8.419 | 7.040 | 8.294 | 8.333 |
| Grouped | 0.7865 | 1.982 | 0.8479 | 2.240 | 2.241 |
| Single | 0.5884 | 1.963 | 0.8485 | 2.073 | 2.068 |

单位为 ns/value；各项均 0 allocs。分组和单一变体明显降低分派成本。`Type()` 返回标签后仍要调用方法并分支，本轮没有稳定优势；携带 payload 时还需要类型断言。

当时的 Go 内联诊断显示，生成的 `UnitKindScore` 成本 106，超过预算 80；手写 `UnitValueKindScore` 成本 35，能内联；两个 `Type()` 消费函数成本均为 104。汇编中 `Type()` 通过接口间接调用。这些观察解释了为什么当前源码形式、nil 检查与内联机会都要一起考虑，单看类型表示不能归因全部差值。可用下列命令检查当前工具链：

```sh
go test -pgo=off -c -gcflags=-m=2 -o /tmp/goxide-enumbench.test ./enumbench \
  2>/tmp/goxide-enumbench-inlining.txt
go tool objdump -s 'UnitTagged(Pointer|Value)Score' /tmp/goxide-enumbench.test
```

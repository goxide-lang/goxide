# RFC 0004：trait 二进制/源码路径与最小适配成本

状态：2026-10-01，研究候选，未批准替换 D05/D06。D21 已确认宏、enum/match、只读/可写分离为 T0；本 RFC 只研究 trait 实现取舍。风险分析仍按 D20 独立长期推进。没有 hgo 编译器实现，所有实验是手写 Go lowering 模型。

## 1. 先给结论：可以优化表示，不能按模式重选语义

用户提出“编译二进制时无冲突直接适配、有冲突最小 wrapper；导出 Go 源码固定 wrapper”。**对内部静态调用消除包装可行；仅凭没有方法名冲突就把动态视图替换为原对象，不保证等价。** 同一个 Go 编译器本来就可以内联薄 wrapper、消除局部装箱；生成源码路径也能获得这些优化，收益不是二进制模式独有。

建议保持共同的 impl 选择、访问权限、视图能力及外部 ABI。二进制模式只在表示不可观察、目标已知时降低成直接调用；向未知 Go 函数、interface/any、回调、反射、持久容器或公开导出边界传值时，仍构造规范视图。仅导出“Go 源码固定 wrapper”若包括原有无体 declare，则改变 D05 的返回原对象与动态身份，需用户另选；本研究不视为已批准。

这里二进制模式仍是 hgo→Go→Go 工具链，不加入自建机器码后端。可见应用源码不等于能分析或改写所有原生库反射、可选接口和回调；不能以“全程序构建”自动证明可观察行为不存在。

## 2. 冲突必须按语义分类

| 冲突/约束 | 局部 wrapper 能解决什么 | 不能解决什么 / 最小策略 |
| --- | --- | --- |
| 同一规范 trait/type/参数的重复 impl | 可分别保存两个已明确命名的实现 | 不能决定未限定调用应该选谁；当前 T08 仍报错。等价无体声明合并是单独候选，不靠 wrap 自动解决 |
| 两个不同 trait 的同名异签名方法 | 不同 wrapper 各自拥有方法；静态调用可直接到已选实现 | 不能在一个 Go 接收者上放两个同名方法；不能因没有名字冲突忽略自定义方法体 |
| 同名同签名但不同业务行为 | wrapper 或普通实现函数可保存行为 | 不能直接采用原方法；“方法集相符”不等于行为相同 |
| marker 与嵌入方法提升 | 在应用包定义独立视图可避免给原对象注入新方法 | 不会修好已经注入后的整个增强图；必须明确选哪种 ABI，不能先污染后局部补丁 |
| 外部包/接口/定义指针/泛型接收者限制 | 普通 struct wrapper 可持有外部指针或接口；包级函数可作静态实现 | Go 不允许随处给原类型加方法；任意 `type View[T] T` 也不可作为通用方案。元数据/字典、包作用域、泛型实例化仍需设计 |
| 只读能力 | 受限视图可只暴露已批准的方法 | 不能把受限 `&T` 直接变成可恢复全部方法的原生 *T；不能转发所有方法来“透明” |
| 动态接口、可选协议、反射、相等性 | 固定视图可提供稳定能力集合 | 动态类型和可选协议会变化；包装尺寸为零不消除此事实 |
| 构建/发布 | 无注入的本地视图可随普通 Go 模块发布 | 现行 marker 注入需要根构建选择增强依赖；子模块 replace 不向下游根传递 |

真正的判定顺序应是：选择唯一 impl → 校验类型/权限 → 确定这次需要的视图及外部能力 → 再优化表示。不能先试编译 Go，哪个版本成功就选择哪个业务实现。

## 3. 可对照的 lowering，不把名字当成本保证

共同工作负载是同一 `*Source` 的方法调用、读取和 io.Copy；方法体及底层对象不因方案改变。完整可执行源码见 [研究夹具](../../tests/research/trait_paths/probe/paths.go)。以下是缩略的 Go 示意：

```go
// 直接采用原方法：只有已选 impl 的语义确实如此才可以。
s.Step(n)

// 静态自定义实现：不需要先形成动态 trait 值。
CustomStep(s, n) // 示例是 Step(2*n)，不能换回原 Step(n)

// 一个指针字段的值 wrapper；接口装箱不必然额外堆分配。
type Value struct { Target *Source }
func (v Value) Step(n int) int { return v.Target.Step(n) }

// 指针 wrapper：*Value。逃逸时可能增加一个对象。
v := &Value{Target: s}

// 无额外对象布局的新定义类型；普通指针转换，不使用 unsafe.Pointer。
type Defined Source
func (v *Defined) Step(n int) int { return (*Source)(v).Step(n) }
v2 := (*Defined)(s)

// 类型化字典：静态调用不需要 Go 方法 marker。
type Dict[T any] struct { Step func(T, int) int }
func Apply[T any](d Dict[T], value T, n int) int { return d.Step(value, n) }
```

| 表示 | 可用范围与性能机会 | 语义/实现成本 |
| --- | --- | --- |
| 现行原包 marker + 原对象 | 采用已有方法，原动态身份/可选接口保留；marker 自身不增加对象字段 | 原包方法集变化、嵌入影响、增强构建/发布；不能用于任意接口接收者 |
| 单指针值 wrapper | 通用具体指针适配；小方法可内联，单指针值装入接口在本环境可不额外分配 | 新动态类型；值视图相等可按 target，需规定 nil；不能自动恢复额外协议 |
| 指针 wrapper | 表示稳定视图对象；可持有额外状态/字典 | 每次逃逸构造可能多分配，wrapper 身份也可观察；不能随便换成值视图 |
| 无额外布局 Defined 视图 | 已知具体结构布局时通过普通 Go 指针转换复用原对象，无额外包装对象 | 新类型/方法集仍可观察；不是 type alias；不能泛化为任意 T、接口或定义指针；额外视图状态无处存 |
| 包级直接函数 | 静态 impl、静态 Self、具体关联类型操作，可内联 | 必须调用所选方法体；不能作为原生接口方法值直接传递，动态边界仍需适配 |
| 类型化字典 | 同一个泛型函数共享实现，携带已选 trait 证据；不要求改原包 | 函数字段可能间接调用，捕获/接口化可分配；公共 Go 签名增加证据参数，不直接是 io.Reader |

“零布局”仅表示未新增 wrapper 存储，不表示动态类型相同、零 dispatch 或零性能代价。Go type alias 保留原类型但不能在外包别名上随意添加方法；新 Defined 类型不同于 alias。[Go 方法与接收者规则](https://go.dev/ref/spec#Method_declarations)

## 4. 同一批 Go API 的两种模式会不会不一样

### 4.1 没有名字冲突也有可观察差异

`errorView` 只提供 Error，原错误还提供 Unwrap。二进制直接传原值时 `errors.Is` 为 true；源码固定包一层时为 false。HTTP ResponseWriter 同理：核心三方法匹配不保证 Flush。Reader 核心方法匹配不保证 WriterTo；io.Copy 会选择不同路径。这些都不是 trait 方法名冲突。

实验同时检验直接原值、值 wrapper、指针 wrapper；读取还增加 Defined/接口字段视图。三个协议各自显式补齐的 `Full` 适配能恢复被测行为。这里 Full 只适用于已知具有对应能力、纯采用原行为的对象；不能把任意 ResponseWriter 强转为 Flusher，不能默认每种 error 都有 Unwrap。

官方分派依据：[io.Copy](https://pkg.go.dev/io#Copy)、[errors.Is](https://pkg.go.dev/errors#Is)、[ResponseController](https://pkg.go.dev/net/http#ResponseController)。hgo 应优先保留原生对象→原生接口的直接结构式路径；进入自定义 trait 视图是明确的能力/行为选择，不是每次调用 Go 都要做的手续。

### 4.2 自动转发并不是通用修复

实验中自定义 Read 将 `abc` 转成 `ABC`。只传自定义 Reader 时 io.Copy 得到 `ABC`；自动转发 target.WriteTo 后，io.Copy 优先走 WriteTo，得到原始 `abc`。因此“恢复快速路径”可能跳过过滤、审计、解密或限流逻辑。

对于希望保留 WriteTo 的自定义 impl，必须实现与自定义 Read 等价的 WriteTo，而不是机械调用 target.WriteTo。同理，自动 Unwrap 可能泄漏目标能力；是否公开是 impl 作者的语义决定。优化不能仅以输出字节恰好相同为依据忽略调用次数、错误、日志或其他副作用。

### 4.3 marker 嵌入及动态身份

同一 build-tag 夹具里，`Combined{*Native; Helper}` 原先从 Helper 提升 Marker；给 Native 注入同一 Marker 后，同深度歧义使动态接口断言 true→false。不注入、改成本地 wrapper 可避开这个注入副作用，但会改变当前 D05 模式，不是无需确认的小优化。仅扫描原类型直接方法名不足以发现所有下游嵌入/反射影响。

Value/Defined 与原 *Source 的 any 动态类型不同；两个指向同一 target 的值视图可以相等，两次新建指针 wrapper 则不相等。nil target 的非 nil wrapper 也不是 nil 接口。若源码输出固定指针 wrapper，二进制不能把它无条件改成原值或值 wrapper；这些都是可观察行为，两个模式需一致。

### 4.4 共同语义基线（推荐候选）

1. 两模式选择同一 impl；自定义方法体不因“无冲突”消失。只读/写权限和显式 unsafe 不变。
2. 为每种显式视图固定外部协议、动态类型/身份、nil 行为、可见反射与相等性；导出入口不随构建模式变化。
3. 静态局部调用可不物化视图；经过只读资格擦除、未知原生函数、动态接口或可保存回调前，必须保持其应观察到的规范表示。可内联只意味着身体可展平，不授权漏掉校验/副作用。
4. 若希望两模式都使用“原对象直接采用”，其对 Go 的动态身份也必须两边一致；不能一边原对象、一边固定 wrapper 后声称只是性能模式。原生 facade 与自定义视图可作为两个显式 API，两个模式都提供，而非自动选不同含义。
5. 动态视图分配/地址若已暴露，则不能自由消除；只在结果不可观察的路径做优化，不要求 MVP 构建一般全程序逃逸证明。复用 Go 编译器现成优化是第一步。

## 5. 实验设计与复现

执行 [run.py](../../tests/research/trait_paths/run.py) 会复制 fixture 到临时模块，不修改依赖或仓库根 go.mod，不下载软件。Go 可执行文件必须明确指定；本环境默认 /usr/bin/go 不是 Go 编译器。

```sh
python3 tests/research/trait_paths/run.py \
  --go /workspace/goxide-environment/go/bin/go \
  --out /tmp/trait-paths-reproduction --count 5 --benchtime 200ms
```

源码中无 `go:noinline`，无 unsafe 内存转换；仅用 unsafe.Sizeof 记录表示尺寸。所有路径同一目标类型/方法体/数据，Go 正常优化打开，GOMAXPROCS=1，五次短基准；没有用人为禁内联制造某条路径劣势。测试不是 hgo 生成产物，不等于前端优化实现完成。

- Local：目标/视图构造在计时循环外，比较静态原方法、值/指针/Defined、普通函数、已知接口和字典调用。
- Retain：同一个预先存在的 target，每次把适配结果存入全局，体现实际逃逸；不夹带 target 分配。字典例子存到具体类型全局，不是接口装箱，单列成本边界。
- Polymorphic：多个真实动态类型交替调用；不人为禁止内联，观察编译器能否确定目标。不是与单态路径完全同一 workload 的速度排名。
- Copy：相同 4096 字节和 io.Discard，复用 target、每轮重置；每轮包括适配。原生 WriterTo 路径可避免读取/复制，因此不能把全部时间差归因于 wrapper 分配。吞吐数字不是网络/磁盘吞吐。
- Plain/enhanced：同一个 marker 夹具开关伴生方法；并复测错误链、HTTP、io.Copy、自定义 Read、身份/存储。

原始结果及版本/命令/源码摘要保存在 [results](../../tests/research/trait_paths/results/environment.json)，包括 [测试](../../tests/research/trait_paths/results/plain.log)、[增强方法集](../../tests/research/trait_paths/results/enhanced.log)、[基准](../../tests/research/trait_paths/results/bench.log)、[编译器诊断](../../tests/research/trait_paths/results/compiler.log)、[动态分派反汇编](../../tests/research/trait_paths/results/dispatch.log)。单次机器的一组微基准只回答这些表示在这些形状下的成本，不推算真实业务百分比或宣称跨版本零成本。


### 5.1 本轮结果（五次中位数）

环境：Go 1.27.1、linux/amd64、Python 3.12.14，普通优化开启。完整 min/max 与全部 21 项见 summary.json。以下不是跨工作负载的性能排名。

| 基准 | ns/op 中位数 | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Retain/raw | 0.7479 | 0 | 0 |
| Retain/value | 0.7296 | 0 | 0 |
| Retain/pointer | 20.48 | 8 | 1 |
| Retain/defined | 0.6953 | 0 | 0 |
| Retain/interface-holder | 31.39 | 16 | 1 |
| Local/direct-function | 0.7214 | 0 | 0 |
| Local/typed-dictionary | 2.62 | 0 | 0 |
| Local/static-dictionary | 2.638 | 0 | 0 |
| Local/known-interface | 0.4297 | 0 | 0 |
| Polymorphic | 6.616 | 0 | 0 |
| Copy/raw | 7.755 | 0 | 0 |
| Copy/value | 69.29 | 0 | 0 |
| Copy/explicit-full | 8.914 | 0 | 0 |

最有用的结果是分配条件：保留同一 target 的单指针值 wrapper 为 0 alloc，指针 wrapper 为 8 B/1 alloc，接口字段 wrapper 为 16 B/1 alloc。局部 wrapper 全部 0 alloc；`-m=2` 确认 Step/Value.Step/Defined.Step/StepImpl 被内联，已知接口 `v.Step` 被去虚拟化。多态循环反汇编保留 `CALL CX`；循环前的 malloc 是一次性准备，不能记成每次调用分配。

补充字典反汇编时首次未指定可写 Go 缓存而失败，重设 GOCACHE 后成功；失败日志及修正命令均保留，不算语义失败。

本例中 Apply 自身内联并未消除字典函数字段调用，局部固定字典和可替换全局字典约 2.6 ns；[字典反汇编](../../tests/research/trait_paths/results/dictionary-dispatch.log)保留间接 CALL。不能从“类型化静态字典”名字推断与直接函数完全等价。亚纳秒局部小方法的排序在两轮中变化，受寄存器分配/噪声影响，不据此宣称某种表示普遍更快。

Copy 原生约 7.8 ns、Read-only 核心值视图约 69.3 ns、显式保留 WriterTo 的 Full 约 8.9 ns；这主要展示可选协议分派导致工作量不同。io.Discard 的快速路径甚至不需要复制 4 KiB，不能解释为真实 I/O 加速倍数。Full 与原生的微小差值也不能推广到业务。

源码发布实验复现：原包 marker 模式配原依赖预期编译失败，改根配置为增强依赖后成功；本地值 wrapper 配原依赖成功。对应日志 publish-marker-original/enhanced、publish-wrapper-original 保留在 results。固定本地 wrapper 可以减轻发布依赖，但改变 D05 身份保证；这个产品取舍不能只按 ns/op 决定。没有测完整 hgo 构建时间或产物代码体积，不能宣称改善了多少。

## 6. 决策建议与待选边界

优先候选是“**稳定视图语义 + 静态直接 lowering + 优先单指针值 wrapper**”；仅在需要可观察 wrapper 身份或状态时用指针 wrapper。Defined 可作为具体类型的优化对照，但不能作为通用 ABI。字典适合泛型证据，动态 Go 接口仍需具体 adapter；不把字典当所有场景更快的答案。

这能保留用户双路径想法里有益的部分：二进制内部可比公开源码 ABI 更积极地消除表示；源码固定语义便于发布、检查和调试。但两者可以共用优化后的 Go lowering，固定 wrapper 也不等于每次堆分配。普通 Go 调用不应被迫套 trait；实际需要多个语义实现时才生成所需视图。

仍需用户选择的是：是否将双模式统一为上述稳定视图语义，只在内部不可观察处消除包装？这不会自动批准取消 D05 marker 或将所有 declare 改 wrapper。若希望彻底摆脱增强依赖，可另选“impl 关系只在 hgo 元数据，静态调用直接函数，动态 trait 统一本地视图”的后端；它改善普通模块发布但改变 D05 和 Go 可见 API，需要独立兼容性设计。

现阶段不应仅为等待 trait 后端决定而推迟 T0 宏/enum/基本读写类型。可先做具体类型和有限宏，保留未来 evidence/源码映射；不开展完整编译器实现。

# RFC 0001 场景：正常 Go 用法与有依据的 warning

更新：2026-10-01。用户已否决前两轮默认保守拒绝；本页替换原来的“必须证明独占才能使用”场景。示例为 goxide 语义草案，最终语法未冻结，也尚不能由仓库执行。诊断依据见 [B01–B08 与 W01–W06](0001-scoped-borrow-views.md)，不是静态安全保证。

## 1. 缓存读取与更新

默认允许现有容器装入锁，不要求唯一性证明，不自动复制：

```goxide
func new_cache(values map[string]int) Lock[map[string]int] {
    return Lock.new(values)
}
func lookup(c Lock[map[string]int], key string) int {
    return c.do(|p| { return (*p)[key] })
}
func put(c Lock[map[string]int], key string, value int) {
    c.do(|p| { (*p)[key] = value })
}
```

三个函数本身无已知外部冲突线索，不因 map/借用参数/未知调用者而逐个告警。普通 Go 的 map 缺失键读取、nil map 写入等行为仍按实际语义处理，以上构造不承诺输入非 nil；显式需要非空类型时另做类型/边界检查。

```goxide
values := make(map[string]int)
c := new_cache(values)
put(c, "quota", 10)
values["quota"] = 20 // W03：已知同一 map 经 c 保护，这次写绕开 c
```

该串行片段并未发生数据竞争，诊断只能说绕过协议。可改用 `put`，也可保留有意的串行约定并抑制该条警告；不强制复制。若同时观察到 goroutine 通过 c 读与外部写可能并发，再增加 W02 风险链；只知道外部“可能存在别名”不报。

## 2. 指针返回、方法值、error 与 panic

```goxide
func fresh() mut& int {
    value := 1
    return mut& value  // 允许、不报：普通 Go GC 存活，不是词法悬空
}

escaped := c.do(|p| { return p }) // 仅返回不报；记录来源供未来诊断
```

返回局部指针所需的具体源级拼写仍待 U02；规则目标是允许这一普通 Go 机制，不能借“未设计生命周期”永久拒绝。真实类型不匹配仍 error。

```goxide
method := counter.do(|p| { return p.increment })
err := counter.do(|p| { return ViewError{target: p} })
counter.do(|p| { panic(ViewError{target: p}) })
```

若 increment 方法值/ViewError 确实保留 p，三处本身仅记录来源；只有具体后续访问/不留存契约违约才给 W01：“返回值/异常载荷仍引用锁内数据，离开回调后锁已释放”。类型合法就继续生成 Go；普通对象上的同样三种操作无保护协议来源则不报。可返回独立值、锁句柄包装的操作，或在已知共同同步下使用。不要强制调用 `Error()` 转字符串作为修复：它可能回调用户代码；没有具体风险链时也不因此给所有 error 格式化报警。

## 3. 集合遍历与快照

```goxide
snapshot := c.do(|p| { return *p }) // map 浅复制：仅返回不报，后续绕锁访问才提示
// 用户可选择独立副本，非编译前提：
snapshot := c.do(|p| {
    out := make(map[string]int)
    for key, value := range *p { out[key] = value }
    return out
}) // 独立外层存储，元素为标量；不报该锁逃逸警告
```

map 值若含 slice/指针，复制外层不会清除内部来源；能追踪到锁内对象时才告警，未知元素关系不猜测。快照具有分配和复制成本；保留共享视图可能是用户正确的性能选择。

```goxide
c.do(|p| { put(c, "quota", 10) }) // W04：持有 c → put → 再获取 c
c.do(|p| { (*p)["quota"] = 10 })  // 同一次回调内更新，无重入警告
```

W04 不阻断，即使风险很明确。建议合并操作或调用不重新加锁的 helper。未知第三方函数可能重入但无具体证据时不报；可选测试补充。锁外快照再写回可能覆盖其他更新，不能将它当作保持事务语义的自动修复。

## 4. 异步交接

```goxide
values := []int{1, 2, 3}
go func() { total(values) }()
values[0] = 100
// W02：同一底层数组 → 后台读取 / 前台写入，未见同步排序
```

建议通过 channel 等待后台完成后再写、使用同一把锁，或传独立副本。检查器未实现通用 happens-before 证明；自定义同步可能误报，隐藏别名可能漏报。

```goxide
values := [3]int{1, 2, 3}
go func(copy [3]int) { total_array(copy) }(values)
values[0] = 100 // 数组值已复制，无同存储冲突，不报 W02
```

只读共享也允许；goroutine/闭包本身不触发警告。若输入是已经复制出的 slice，父任务再写这个新 slice 仍与子任务共享，不能仅凭“它是快照”取消 W02。并发函数体未知、只有调用但未见写冲突链，则不报，同时不宣称无竞争。

## 5. 普通 Go API 与显式 unsafe

```goxide
n := strconv::Itoa(10)  // 普通 Go 符号与签名匹配，不因缺摘要要求 unsafe
fmt::Println(n)        // 同样允许；没有具体风险链不报
f := native::Update
f(nativeValue)         // ABI/类型合法即可；函数值不自动带 unsafe
```

这里 nativeValue 是 ABI 相容的原生值，不暗含 Ptr→非空引用等转换。若已有明确类型桥接要求，仍需合法桥接，不能靠警告跳过类型检查。D11 的显式 `unsafe p.as_ptr()` 仍保留；普通 Go 指针参数与该边界如何做到顺手的 U15 尚待协调，不能为了缺摘要重新强迫上述普通调用写 unsafe。

锁内 `fmt::Println(n)` 只有标量参数且无已知回调当前锁，不报。若实际参数的 String 方法已知调用 `c.do`，并且 fmt 当前持有 c 时调用它，则 W04 展示 `c.do → fmt → String → c.do`；动态类型未知不进行全包推测告警。declare 可帮助关联调用链，也可描述不留存，但并非调用门票。

明确标为 unsafe 的函数或 AsPtr 操作仍须显式上下文，遗漏为 error；这与普通 Go API 缺少摘要是两回事。没有 warning 不表示标准库整体 safe，未知原生行为仍未检查。

## 6. 原三项决策的更新

| 前轮选择 | 当前处置 | 仍有意义的实现取舍 |
| --- | --- | --- |
| 词法期限 vs 最后使用作为准入规则 | 默认保守拒绝已被用户否决；两者均不能成为使用指针的普遍门槛 | 可用局部最后使用/分支分析减少 warning 误报；不证明则不报安全结论，不拒绝 |
| 按接收权限拆 impl vs 每方法权限 | U03 仍未定；不因 warning 方向废除类型/trait 规则，也不强迫拆 API | 前者延续身份模型，后者减少拆分；都须区分真正类型错误与别名风险，未冻结 ABI |
| 证明唯一再构造 Lock vs 可信采用 | 默认允许正常构造；不要求额外 unsafe 或人工承诺才能 Lock.new | 显式复制构造是按业务选用的隔离/性能取舍，非默认安全门票；能看到绕锁访问才 W03 |

严格模式如需研究，应单独获得用户批准；这轮不实现或设成推荐默认。按 D20，风险分析整体是长期工作线，MVP 不要求其中任何规则。未来可研究直接协议违约、重入、锁复制及 Close 后使用，再考虑有限摘要；每条诊断必须具备正例、反例及不应报告的例子。

## 7. 验证与不报告对照

现有 Go 测试保留 6 个边界/快照夹具，本轮另加普通局部指针返回、共同只读指针并发两项机制正例。它们证明这些 Go 行为可用，不是假装已测试 goxide warning 文本。文档警告目录人工自审以下负对照：标量复制、只读共享、无摘要调用、未知动态方法、独立数组任务、正常指针返回均不应仅因无法证明安全而警告。

`go test -race` 检测实际运行中的数据竞争；`go build -race` 只构建插桩程序，运行后才可能报告。静态 warning 在执行前依据代码风险链产生；race 未覆盖路径和未知外部行为单独说明。当前仓库没有完整前端，不能声称已实现上述诊断级别或静态保证。

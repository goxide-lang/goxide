# 官网与 playground 的证据交接（不包含网站实现）

> 后续修复状态见 [FIXES.md](FIXES.md)：primitive 包级/文件 import 冲突改为带位置诊断，尚未全面支持这些命名；跨模块 Result `?` 的静态 validator 修复已落生产代码。本页其它基线证据和范围限制仍适用，不应把修复外推到所有 enum/derive 校验。

依据：2026-10-04 实测 `feat/t0-completion@3098a30`，Go 1.27.1 linux/amd64。网站由另一任务负责。本文提供保守宣传口径与可复用源，不代表发布验收或稳定语言规范。

后续适配宏 `ffc9b9b` 与本修复的组合聚焦验收已通过，具体能力与限制仍以适配宏任务交付为准；本交接不改写原始 baseline 示例。见 [集成检查](INTEGRATION_FFC9B9B.md)。

## 可作主展示的已实证能力

建议总述：**“把有限、显式的语言增强编译成可检查、可用 Go 工具测试的普通 Go 源码。”**

1. 源码包 `pub macro_rules!` 与 qualified 调用；示例选择 `logkit.event_if!`，展示 disabled 分支不求值、模板明确控制一次或多次求值。不要宣称宏无条件 once-only 或任意标识符完全卫生。
2. 闭合 enum / 穷尽 match 与 payload；`Mode::Immediate / Mode::Batch(int)` 实例正常，未知 JSON 变体拒绝。enum 生成的是闭合接口和 case 类型，不是零成本整数 tag 的承诺。
3. 显式 `Result` / `Option` 与 `?`，展示跨模块 `Step` 的成功/错误路径；在本组循环、defer、From 转换内与手写 Go trace 相同，cause 身份保留。不要说原生 Go 多返回值已自动适配：后续 `ffc9b9b` 提供显式 `go_result!(T,call)` / `go_result_keep!(T,call)`，后者 Err 载荷为 `struct{Value T;Err error}`，error-only 调用有单参数形式。T 当前需显式且包作用域可命名，多非 error 值仍先组织为业务 struct；预编译宏导入仍不支持。与本修复的组合聚焦验收已通过，详见集成记录。
4. 显式 Default/Clone/Debug/PartialEq/Eq；独立 model 的协议可供消费者派生。展示 None / Some(nil) / Some(empty) 的 Debug 与 equals 结果，Clone 后变更 slice 不影响源。不要说普通赋值变成深复制或所有 Go 类型自动具备协议。
5. serde JSON 的具体派生、default/skip、未知变体拒绝及 Error/From cause 链。展示默认值、正确 JSON、非法枚举、`strconv.NumError`。不要说是 encoding/json 的无差异替代。
6. Go 互操作用事实表达：“本样例的两个生成模块通过 Go race/vet，并可普通 go build”。不能升格为全平台、任意依赖、源码不可得时全部支持或 CI 全绿。

## 可直接复用的输入与输出

- 较小完整订单：仓库 `examples/enum_trial`；本审查复用了其代码并增加 [integration/app/pipeline.hgo](integration/app/pipeline.hgo) 和 [integration/model/config/pipeline.hgo](integration/model/config/pipeline.hgo)。
- 新的高价值业务回归：[integration/app/pipeline_test.go](integration/app/pipeline_test.go)，能反映真实组合，而不是只重复单元测试。
- 可供网页错误页展示的真实错误：`{"Name":"bad"}` 保留 `*strconv.NumError`，`{"Name":"1","Mode":{"Unknown":0}}` 拒绝；输出行为见已有 enum_trial README，不编造源码位置精度。
- 宏卫生已知失败暂不可当正例：调用包遮蔽 primitive 时，原始基线会静默改变结果，当前修复对这些冲突明确拒绝，见 [macro/shadow_constants](macro/shadow_constants/) 和 [macro/shadow_types](macro/shadow_types/)。应修复后再扩大卫生宣传。

`.hgo` 生产者的真实源：

```go
func Step(n int) Result[int,error] {
    if n < 0 { return Result[int,error]::Err(Negative) }
    return Result[int,error]::Ok(n + 1)
}
```

基线实际生成 Go 的函数摘录（未做美化改名）：

```go
func Step(n int) Result[int, error] {
    if n < 0 {
        return __hgo_enum_ctor_Result_Err[int, error](Negative)
    }
    return __hgo_enum_ctor_Result_Ok[int, error](n + 1)
}
```

这只是函数摘录；完整 Go 包还含 Result interface、case、constructor、validator 和 metadata。网站必须明确“摘录”，不能让用户误以为单独复制该函数即可编译。要展示完整可运行输出，用 `integration/run.sh` 产生的 `model/config/pipeline.go` 和对应模块文件；不要使用 `compare_direct.sh` 的修改后实验产物冒充编译器输出。消费者 `?` 展示应保留校验、type switch 和提前返回，不把它画成无校验的 Go 二元组伪代码。

## 必须标实验或具体限制

- 当前是有限原型/可执行子集，完整 trait、用户泛型定义、任意表达式组合与完整借用系统未交付。
- 宏重复/捕获片段有限，某些 builtin/free names 不支持；源包宏和跨独立模块的预编译宏 ABI 不能混为一谈。本审查确认原始 primitive shadow 静默错误；当前修复后仍需标明包级/文件 import 冲突是显式诊断的工程边界。
- Result/enum 为 interface/case 表示。本审查发现并修复跨包 Result `?` validator 间接绑定导致的逐项分配，最终 Result 返回仍可分配；不得宣称零成本抽象、与手写 Go 性能一致或所有路径零分配。
- Option JSON null/缺失→None；嵌套 Option 不能完整表达 PATCH missing/null/value 三态。nil 与非 nil 空容器不相等；序列化不能普遍承诺所有内存状态一一往返。
- serde JSON 拒绝重复键、非法 UTF-8/surrogates、整数目标的指数/小数语法；bytes 是整数数组；输入大小/嵌套有边界。Native JSON adapters/PATCH 不是既有能力。
- metadata 对源码闭包及 race/tags/平台视图敏感；普通模式生产者不能直接被当成 race 视图依赖。单文件 playground 成功不证明目录、多模块或任意 imports 模式可用。
- 源映射覆盖有限；诊断可能指向 generated/unmapped 位置。不要展示“所有错误均精确原文件行列”式承诺。
- 公开 enum 参数入口校验、blank/embedded derive、serde adapters 仍待决。页面不能通过文案擅自解决这些设计议题。

## 建议交给修复任务的最高价值回归

1. 调用包 `const true=false-expression` / `type int string` 的跨包宏最小反例；断言输出或正确 located rejection，不能只断言 Go 能 build。
2. >255 的成功整数、真实独立生成 producer、每项 `?` 的 allocation gate；同时覆盖错误和 producer validator，防止以删校验换性能。
3. 包宏 + 跨模块 ? + enum match + loop/defer 的完整 trace 差分，与 nil/typed-nil error cause 身份。
4. source 与 clone、None/Some(nil)/Some(empty) 的跨模块协议组合。
5. N=128/512/2048 imported macro helper 的扩展工作量，按规模验证，避免仅测一次小模板。

公开部署/playground 的构建、隔离、托管、访问权限和发布检查不在本任务范围，未执行，也不由此文宣告完成。

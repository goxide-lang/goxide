# RFC 0011：声明宏与卫生展开

## 状态与范围

已确认目标：提供模式 / 模板声明宏；用户过程宏延期（D14）。
本文是候选规范；宏语法、定义点卫生、阶段和预算未冻结（T10、U09），尚无展开器实现。
依据：[原设计](../design/macros.md)。打印、派生和语法数据分别见 RFC 0012、0013、0014。

## 源级规则

- 捕获的是带语法类别、来源和上下文的结构化片段，不是重拼后的字符串。
- 候选匹配规则：按定义顺序选择第一条完整匹配；重复层级或类别不符报错。
- 模板局部绑定每次展开取得新身份；捕获语法保留调用者绑定；自由名字按定义点解析。
- 卫生只决定绑定。实参执行次数由模板决定，不能把“一次求值”作为所有宏的隐含性质。
- 展开后仍执行普通名字、类型、借用和 unsafe 检查，再生成 Go；宏本身没有运行时对象。
- 用户宏规则不执行文件、环境、网络或任意用户程序；可以生成合法的运行时 I/O 调用。

## 转译示例

**M01：显式保存实参，保留两侧同名变量。** hgo 片段；`macro_rules!` 为候选语法。

```hgo
func twice(value int) int { return value + value }
macro_rules! twice_plus_one {
    ($x:expr) => {
        func() int {
            saved := 1
            value := $x
            return twice(value) + saved
        }()
    };
}
// 以下片段位于调用方函数体。
saved := 41
twice := func(value int) int { return -1000 }
calls := 0
next := func() int { calls += 1; return 1 }
result := twice_plus_one!(saved + next())
```

Go 对照；片段置于函数体，依赖定义点 helper 的普通函数 `macroRuntimeTwice`。
该 helper 的函数体为 `return value + value`；输出名字只是示意。

```go
saved := 41
twice := func(value int) int { return -1000 }
_ = twice
calls := 0
next := func() int { calls++; return 1 }
result := func() int {
    hgoM1Saved := 1
    hgoM1Value := saved + next()
    return macroRuntimeTwice(hgoM1Value) + hgoM1Saved
}()
```

约束：`result == 85`、`calls == 1`、`saved == 41`；调用者的同名 `twice` 不能替换定义点绑定。
不得先把捕获表达式按文本粘贴进模板，再按生成后的局部名字重新解析。

**M02：重复捕获表达式会重复执行。** hgo 片段沿用 M01 的 `next`。

```hgo
macro_rules! duplicate {
    ($x:expr) => { $x + $x };
}
result := duplicate!(next())
```

Go 对照；依赖 M01 的 `next`，从 `calls == 0` 开始观察。

```go
result := next() + next()
```

约束：`result == 2`、`calls == 2`。不得将其擅自改为保存一次再相加。
短路、闭包、defer 和 return 同样遵循实际展开语法，不由卫生额外改写控制流。

**M03：unsafe 来源不随模板移动。** 下列 hgo 候选模板不能替调用者授权。

```hgo
macro_rules! consume {
    ($x:expr) => { unsafe raw::Consume($x) };
}
consume!(raw::Other()) // Other 在调用点没有 unsafe 授权
```

预期拒绝，不生成 Go；依赖 `raw` 为未声明 safe 视图的原生 Go 包。
模板中的显式 unsafe 只按其来源授权模板引入的操作。

## 拒绝与边界

- 表达式捕获插入类型位置、重复长度或层级冲突：拒绝，不通过字符串重解析兜底。
- 模板展开到外包后调用定义包私有 helper：拒绝；应显式提供公开 helper / shim。
- 模板不得生成未知 import、宏定义或外部 API 的 declare 契约；相关限制仍属候选阶段协议。
- 展开结果缺少 impl、权限冲突或重复定义：按普通源程序拒绝。
- 递归或输出超预算：诊断包含定义、调用链和预算类别，不把半成品当成成功缓存。

## 双向互操作

hgo 宏生成的原生 Go 调用仍遵守所选 `.d.hgo` 契约或 unsafe 规则。
发布模块中的 `.go` 已是展开并检查后的普通代码；Go 消费者不需要宏展开器。
Go 可调用公开生成函数，不能调用 `m!(...)`；宏规则供 hgo 工具链加载，具体承载格式未定。
增强检查只覆盖 hgo 工具链管理的代码，不给原生 Go 调用者追加源级权限保证。

## 验收与未定项

1. M01/M02 分别验证一次与两次执行；不要只比较生成临时名。
2. 同一宏重复展开、缓存命中和并行构建必须保留相同绑定关系，不复用旧调用点身份。
3. M03、私有 helper 和类别错配产生源级错误；生成 Go 错误可追溯到模板与调用点。
4. 成本：运行时成本来自展开代码；编译期成本受匹配、递归和输出节点预算限制。
5. 未定：完整捕获类别 / 重复语法、导入拼写、阶段细节、预算默认值及缓存身份协议。
6. 这些是未来端到端验收；独立执行 Go 片段只能检验指定展开的行为。

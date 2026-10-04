# RFC 0015：pub 与公开 Go API

## 状态与范围

已确认：使用 `pub` 表达公开边界，生成 API 能被 Go 调用。暂定：导出名称、输入检查与失败策略。未定：公开字段、方法、回调、泛型入口的完整规则。尚未实现。

背景见[边界设计](../design/overview.md#34-go-边界与非法值)，包分发见 [RFC 0001](0001-package-loading.md)。

## 源级规则

1. goxide 可见性来自声明与元数据；Go 可见性来自实际生成符号。两者需要显式映射。
2. 为讲解，本文把 `pub add` 映射为 Go `Add`；这只是候选编码。不同源符号不能经大小写转换碰撞后覆盖。
3. 普通 Go 调用者遵守生成 API 的调用契约，不获得 goxide 静态借用检查。值合法性检查不能变成独占性或不逃逸证明。
4. 若公开签名采用非空 Arc、enum 等源级约束，所需检查及其失败行为必须公开。检查不能改写业务结果形状而不改变 API。

## 转译示例

### PUB-01：公开函数和包内函数

源级片段：

```goxide
func twice(x int) int { return x * 2 }
pub func add_twice(a int, b int) int { return twice(a + b) }
```

候选完整 Go 文件，名称只是示意：

```go
package calc
func twice(x int) int { return x * 2 }
func AddTwice(a, b int) int { return twice(a + b) }
```

Go 调用方使用 `calc.AddTwice(1, 2)`，结果为 6；不能从别包直接调用 `calc.twice`。没有额外运行时包装。

### PUB-02：非空输入检查不证明借用独占

采用非空 Arc 的候选契约，源级片段：

```goxide
pub func value(p Arc[int]) int { return *p }
```

候选完整 Go 文件：

```go
package numbers
func Value(p *int) int {
    if p == nil { panic("goxide: Value.p: nil Arc") }
    return *p
}
```

合法输入保留地址且不复制目标对象；nil 在解引用前 panic。一个空值分支有检查成本。Go 持有其他别名或并发写入时，非空检查不授予数据竞争保证。

## 拒绝与边界

| 编号 | 情形 | 预期 |
| --- | --- | --- |
| PUB-N1 | 源符号经候选命名规则产生相同 Go 名称 | 转译诊断，不能任选其一 |
| PUB-N2 | goxide 从别包访问未公开符号 | 源级诊断 |
| PUB-N3 | Go 向 PUB-02 传 nil | 按选定入口策略拒绝，不静默创建对象 |
| PUB-N4 | Go 获得读回调参数 `*T` 后写入 | 不宣称由 goxide 静态检查阻止；列为调用方契约 |

enum 的精确变体检查见 [RFC 0008](0008-enums.md)；回调返回及可变共享字段也是输入，不能只用第一次构造检查替代后续所需检查。

## 双向互操作

Go → goxide：如 PUB-01/PUB-02，使用导出的真实符号。调用者无需认识 `pub` 或 Arc，但必须遵守 API 的非空与同步约定。

goxide → Go：真实 Go 导出名和签名是导入依据；明确选择的声明映射恢复 goxide 名称与类型视图。`.d.gox` 不能给不可访问的 Go 私有成员额外授权。

## 验收与未定项

未来验证 PUB-01 的跨包可见性、PUB-02 的合法/非法输入及命名碰撞。Go 入口检查通过与 goxide 权限检查通过分别报告。

导出名称编码、公开字段写入、泛型验证能力和检查插入策略仍未冻结。本文不以两个标量示例宣称任意共享对象可被安全验证。

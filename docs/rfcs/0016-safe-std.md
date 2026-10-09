# RFC 0016：Go 标准库的声明与实际包装

## 状态与范围

已确认：围绕真实 Go std 提供逐 API 的 safe 视图。暂定：首批 API、包装结果类型与名称。没有 safe std 实现。背景见[标准库专题](../design/standard-library.md)；容器分别见 [Vec](0010-collections.md) 与 [Map](0017-maps.md)。

## 源级规则

1. ABI 与行为保持时可用 declare；改变结果形状、复制存储、判空或验证计数，必须生成真实代码。
2. 声明作者保证原生实现遵守契约；goxide 检查本侧参数权限和调用效果，不要求证明任意 Go 函数体。
3. safe 不表示无副作用、无 panic 或无 I/O 错误。Go 的“部分数据 + 错误”不能被包装丢掉。

## 转译示例

### STD-01：不改变数值行为的声明

源级片段，假设 `math` 是 Go 包的导入别名：

```goxide
declare func math::Sqrt(x float64) float64;
y := math::Sqrt(9.0)
```

Go 调用片段（位于函数内，文件导入 `math`）：

```go
y := math.Sqrt(9.0)
_ = y
```

结果为 3。负数输入仍返回 NaN，声明没有把它改成 Result，也没有增加业务包装。

### STD-02：读取同时保留字节数与错误

源级结果草图；`GoError` 表示保留原生 nil/typed nil 身份的错误视图，最终名称未定。`ReadOutcome` 与输入权限的完整 API 也仍为候选：

```goxide
struct ReadOutcome { count int; error GoError }
// reader 已有可信 Read 契约，dst 为调用期间的可写字节视图。
let (n, err) = reader.read(dst)
return ReadOutcome { count: n, error: err }
```

对应完整 Go 机制示例，仅验证结果保留与计数，不证明任意 io.Reader 守约：

```go
package stdio
import "io"
type ReadOutcome struct { Count int; Error error }
func ReadOnce(r io.Reader, dst []byte) ReadOutcome {
    n, err := r.Read(dst)
    if n < 0 || n > len(dst) { panic("goxide: invalid Read count") }
    return ReadOutcome{Count: n, Error: err}
}
```

当 Read 返回 `(3, io.EOF)` 时，调用者仍能处理 `dst[:3]` 并观察 EOF。包装增加范围检查和小结果值；不复制缓冲区，不修复违反契约的 reader。

## 拒绝与边界

| 编号 | 错误适配 | 预期 |
| --- | --- | --- |
| STD-N1 | 将 `(3, io.EOF)` 只映射为 `Err(io.EOF)` 并丢弃已读数据 | 违反结果保留契约 |
| STD-N2 | 通过一条 declare 把 `bytes.Buffer.Bytes()` 宣称为独立拥有的 Vec | 拒绝这一契约；需实际复制或有期限的借用视图 |
| STD-N3 | 将可能留存回调的 API 声明为普通同步短借用且无行为依据 | 不纳入已审核 safe std |
| STD-N4 | 任意 `Unmarshal(any)` 只改写成 `&mut T` 即宣称失败不修改输入 | 不成立；需定义独立临时对象与验证/发布步骤 |

## 双向互操作

goxide → Go：STD-01 直接调用，STD-02 保留真实读取行为；每个 safe 入口记录原生符号、借用/别名、回调、错误和版本依据。

Go → goxide：可直接调用生成 `ReadOnce` 并读取 `Count`/`Error`；普通 Go reader 的正确性由调用契约承担，不享有 goxide 的静态缓冲区检查。

## 验收与未定项

未来验证 STD-01 的普通数值与 NaN；STD-02 的正数+EOF、零进展 `(0,nil)`、非法计数、typed nil error。既有支持库机制检查见[测试说明](../../tests/README.md)，不等于本 RFC 已有 goxide 实现。

每增加一个 safe API，必须补充本篇同样的调用/包装对照，或另立小 RFC；不会因为包内已有 safe 入口就默认整个包 safe。

# RFC 0001：同模块包加载、dist 与 Go 发布

## 状态与范围

已确认：类似 TS/JS，同一个 Go 模块可以携带 `.go`、`.d.hgo` 和可选 `.hgo`；发布整个 dist；Go 工具链编译 `.go`，hgo 工具链额外加载声明或源码。增强语义约束 hgo 工具链管理的代码，不要求普通 Go 调用者获得 hgo 静态检查。

已确认：运行时 trait 统一使用 wrapper；trait 的 Go 接口包含业务方法与导出 marker，wrapper 实现 marker。包级工厂及 Go 便利入口见 [RFC 0019](0019-uniform-trait-wrappers.md)。

暂定：同包源码与生成文件的选择规则。未定：具体命名、版本指纹及完整公开 ABI。没有 hgo 加载器实现。背景见[Go 适配](../design/go-interop.md)。

## 源级规则

1. `go mod` 负责正常模块分发，不因有 `.d.hgo` 就另建包下载系统。发布内容须实际包含声明文件；`dist/` 是产物目录概念，不强制成为 import path 的一部分。
2. hgo 使用真实 `.go` 类型信息校验声明；`.d.hgo` 不会给 Go 编译器增加方法，也不是运行时实现。
3. 同一实现选择 `.go + .d.hgo` 或 `.hgo` 源码路径；不能把源码及其生成 `.go` 当成两份独立业务定义。包内独立手写 `.go` 仍正常参与构建；识别生成文件的协议待定。
4. 只有 `.hgo` 而没有生成 `.go` 的包，可由 hgo 处理，不能宣称可被普通 Go 直接使用。
5. trait wrapper、固定工厂和便利入口随 impl 声明包 C 发布。C 正常依赖 trait 包 A 和原类型包 B，不向 A/B 注入方法；A、B、C 可以重合。
6. 本方案的 trait 适配不需要依赖副本、GOROOT overlay 或下游继承 replace；dist 提供正常 Go 源码及依赖声明。

## 转译示例

### PKG-01：不涉及外部方法注入的完整发布

源文件 `add.hgo`，名称导出采用 [RFC 0015](0015-public-api.md) 的候选规则：

```hgo
package calc
pub func add(a int, b int) int { return a + b }
```

发布文件 `dist/add.go`（完整 Go 文件）：

```go
package calc
func Add(a, b int) int { return a + b }
```

`dist/add.d.hgo` 描述同一个 `add` 对应 Go `Add`，不是第二份实现。下面是声明形状示意，外部符号绑定格式未冻结：

```hgo
package calc
pub declare func add(a int, b int) int;
```

```text
dist/                       # 发布模块根，示例 module example.org/calc
  go.mod                    # go 1.27
  add.go
  add.d.hgo
  add.hgo                   # 可选
```

Go 使用 `calc.Add(1, 2)`；hgo 读取声明后使用 `calc::add(1, 2)`。两者结果都是 3；调用时无额外包装成本。

### PKG-02：外部类型通过本地 wrapper 完整发布

用户包 `example.org/adapters` 中的 hgo 片段。此例 trait 与 impl 同包，原类型来自标准库。`*bytes::Buffer` 仅表示 Go 接收者身份，完整借用权限见 [RFC 0005](0005-borrows.md)：

```hgo
pub trait Sized { len() int }
declare impl Sized for *bytes::Buffer;
pub func size(b *bytes::Buffer) int { return b.len::<Sized>() }
```

发布文件 `dist/adapters.go`（完整 Go 文件，名称编码仅为示意）：

```go
package adapters
import "bytes"

type Sized interface {
    Len() int
    HyperGoTraitSized()
}
type bufferSized struct { target *bytes.Buffer }
func (v bufferSized) Len() int { return v.target.Len() }
func (v bufferSized) HyperGoTraitSized() {}
func AsSizedForBuffer(b *bytes.Buffer) Sized { return bufferSized{target: b} }

type bufferFacade struct { target *bytes.Buffer }
func ForBuffer(b *bytes.Buffer) bufferFacade { return bufferFacade{target: b} }
func (v bufferFacade) AsSized() Sized { return AsSizedForBuffer(v.target) }

func Size(b *bytes.Buffer) int {
    s := AsSizedForBuffer(b)
    return s.Len()
}
```

普通 Go 可调用 `adapters.AsSizedForBuffer(b)` 或 `adapters.ForBuffer(b).AsSized()`。
两者使用同一工厂和 impl；接口动态类型是 wrapper，其 target 保留原指针。
facade 只提供便利入口，不匿名嵌入原对象，也不再次选择实现。
依赖下载和编译遵循普通 Go 规则；bytes 包无须增加 marker 或 As 方法。

### PKG-03：旧外部 marker 注入方案的错误对照

以下片段依赖 PKG-02 的 Sized，展示已替换方案的问题，不是当前输出：

```go
var s Sized = b // 错误：原始 *bytes.Buffer 不实现 marker。
// 在 adapters 包为 *bytes.Buffer 声明方法也不合法：接收者属于外部包。
```

将 `package bytes` 文件放入 `dist/bytes/` 也不会并入标准库包；相同包名不等于相同 import path。
当前使用 PKG-02 的 wrapper 解决这个问题，marker 保留在 trait 接口及 wrapper 上。
静态已选定的具体调用可优化为 `b.Len()`；生成或导出运行时视图时仍遵守固定 wrapper 表示。

## 拒绝与边界

| 编号 | 输入 | 预期 |
| --- | --- | --- |
| PKG-N1 | `.d.hgo` 声明存在而实际 Go 函数缺失/签名不符 | hgo 导入时诊断 |
| PKG-N2 | 同一源码和生成文件被当成两份实现 | 加载器拒绝或按明确来源选择，不由目录顺序决定 |
| PKG-N3 | PKG-03 将原始 bytes.Buffer 直接充当携带 marker 的接口 | Go 编译拒绝；当前输出必须使用 wrapper |
| PKG-N4 | 将依赖模块的 replace 当成下游主模块自动继承的配置 | 发布验证失败；不归因于 `.d.hgo` 下载 |

## 双向互操作

hgo → Go：原生模块仅有 `.go` 也能加载；未声明入口按 [RFC 0006](0006-foreign-api.md) 处理。包自带或显式选定的 `.d.hgo` 恢复可信 hgo 视图。

Go → hgo：下载发布模块后直接调用生成 `.go` API，如 PKG-01、PKG-02。不要求 Go 理解 trait、借用或宏；生成工厂和 facade 均为普通 Go 代码。源码权限保证与 Go 构建可行性分别验收。

## 验收与未定项

未来须在新主模块中分别运行普通 Go 导入与 hgo 导入，验证 PKG-01、PKG-02、声明缺失/冲突及源码模式。PKG-02 不提供 overlay 或增强标准库，工厂与 facade 应返回相同 wrapper 类型并共享原指针；PKG-03 作为 Go 拒绝的历史反例。当前无 hgo 端到端通过记录。

待定：生成文件来源识别、声明格式/指纹、构建条件一致性、公开工厂命名与权限契约。统一 wrapper 的选择已确认，不再将外部 marker 注入视为发布前提。

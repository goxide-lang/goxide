# RFC 0001：同模块包加载、dist 与 Go 发布

## 状态与范围

已确认：类似 TS/JS，同一个 Go 模块可以携带 `.go`、`.d.gox` 和可选 `.gox`；发布整个 dist；Go 工具链编译 `.go`，goxide 工具链额外加载声明或源码。增强语义约束 goxide 工具链管理的代码，不要求普通 Go 调用者获得 goxide 静态检查。

已确认：运行时 trait 统一使用 wrapper；trait 的 Go 接口包含业务方法与导出 marker，wrapper 实现 marker。包级工厂及 Go 便利入口见 [RFC 0019](0019-uniform-trait-wrappers.md)。

当前已有目录/模块入口的版本化 enum、引用及具体协议 metadata 恢复，见下文增量；
这不等于通用 `.d.gox`/trait 加载器或完整公开 ABI 已完成。同包来源选择与单文件恢复
按各自已实现合同验收，不能用本篇早期“没有加载器”的状态概括当前实现。
背景见[Go 适配](../design/go-interop.md)。

## 源级规则

1. `go mod` 负责正常模块分发，不因有 `.d.gox` 就另建包下载系统。发布内容须实际包含声明文件；`dist/` 是产物目录概念，不强制成为 import path 的一部分。
2. goxide 使用真实 `.go` 类型信息校验声明；`.d.gox` 不会给 Go 编译器增加方法，也不是运行时实现。
3. 同一实现选择 `.go + .d.gox` 或 `.gox` 源码路径；不能把源码及其生成 `.go` 当成两份独立业务定义。包内独立手写 `.go` 仍正常参与构建；识别生成文件的协议待定。
4. 只有 `.gox` 而没有生成 `.go` 的包，可由 goxide 处理，不能宣称可被普通 Go 直接使用。
5. trait wrapper、固定工厂和便利入口随 impl 声明包 C 发布。C 正常依赖 trait 包 A 和原类型包 B，不向 A/B 注入方法；A、B、C 可以重合。
6. 本方案的 trait 适配不需要依赖副本、GOROOT overlay 或下游继承 replace；dist 提供正常 Go 源码及依赖声明。

## 转译示例

### PKG-01：不涉及外部方法注入的完整发布

源文件 `add.gox`，名称导出采用 [RFC 0015](0015-public-api.md) 的候选规则：

```goxide
package calc
pub func add(a int, b int) int { return a + b }
```

发布文件 `dist/add.go`（完整 Go 文件）：

```go
package calc
func Add(a, b int) int { return a + b }
```

`dist/add.d.gox` 描述同一个 `add` 对应 Go `Add`，不是第二份实现。下面是声明形状示意，外部符号绑定格式未冻结：

```goxide
package calc
pub declare func add(a int, b int) int;
```

```text
dist/                       # 发布模块根，示例 module example.org/calc
  go.mod                    # go 1.27
  add.go
  add.d.gox
  add.gox                   # 可选
```

Go 使用 `calc.Add(1, 2)`；goxide 读取声明后使用 `calc::add(1, 2)`。两者结果都是 3；调用时无额外包装成本。

### PKG-02：外部类型通过本地 wrapper 完整发布

用户包 `example.org/adapters` 中的 goxide 片段。此例 trait 与 impl 同包，原类型来自标准库。`*bytes::Buffer` 仅表示 Go 接收者身份，完整借用权限见 [RFC 0005](0005-borrows.md)：

```goxide
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
    GoxideTraitSized()
}
type bufferSized struct { target *bytes.Buffer }
func (v bufferSized) Len() int { return v.target.Len() }
func (v bufferSized) GoxideTraitSized() {}
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
| PKG-N1 | `.d.gox` 声明存在而实际 Go 函数缺失/签名不符 | goxide 导入时诊断 |
| PKG-N2 | 同一源码和生成文件被当成两份实现 | 加载器拒绝或按明确来源选择，不由目录顺序决定 |
| PKG-N3 | PKG-03 将原始 bytes.Buffer 直接充当携带 marker 的接口 | Go 编译拒绝；当前输出必须使用 wrapper |
| PKG-N4 | 将依赖模块的 replace 当成下游主模块自动继承的配置 | 发布验证失败；不归因于 `.d.gox` 下载 |

## 双向互操作

goxide → Go：原生模块仅有 `.go` 也能加载；未声明入口按 [RFC 0006](0006-foreign-api.md) 处理。包自带或显式选定的 `.d.gox` 恢复可信 goxide 视图。

Go → goxide：下载发布模块后直接调用生成 `.go` API，如 PKG-01、PKG-02。不要求 Go 理解 trait、借用或宏；生成工厂和 facade 均为普通 Go 代码。源码权限保证与 Go 构建可行性分别验收。

## 验收与未定项

未来须在新主模块中分别运行普通 Go 导入与 goxide 导入，验证 PKG-01、PKG-02、声明缺失/冲突及源码模式。PKG-02 不提供 overlay 或增强标准库，工厂与 facade 应返回相同 wrapper 类型并共享原指针；PKG-03 作为 Go 拒绝的历史反例。当前无 goxide 端到端通过记录。

待定：生成文件来源识别、声明格式/指纹、构建条件一致性、公开工厂命名与权限契约。统一 wrapper 的选择已确认，不再将外部 marker 注入视为发布前提。

### 跨模块引用恢复首片（2026-10-02）

独立模块 local replace 的生成 Go + `.goxide.refs.json` 已能恢复现有引用签名，详见 [RFC 0005 §6.21](0005-borrows.md#621-跨-module-引用产物恢复2026-10-02)。源定位与 build selection 复用离线 Go importer，缺失/陈旧产物不猜测权限；普通 Go 消费无需 metadata。此证据仅覆盖具体引用签名，不代表 trait、宏、enum 等跨模块增强协议全部完成，亦不代表远程 registry 或外部 cgo 输入验证完成。

### 跨模块具体 enum 元数据（2026-10-03）

独立模块 local replace 现可恢复公开具体 enum 的闭合变体与真实类型身份，见 [RFC 0008](0008-enums.md#跨-module-公开具体-enum-恢复2026-10-03)。该产物与 refs 协议分别版本化并复用内容/构建指纹；同 module registry 和原生 Go API 消费不替代外部 goxide 模式验收。通用 Option/Result 家族及跨包 derive 协议未由此片恢复；Go internal 包访问规则仍需尊重，普通 facade API 不因其内部表示不可导入而被拒绝。

## A06 已知 Go module 的单文件外部恢复（2026-10-06，实施前合同）

基线为 A04 最终报告提交 `e7c10e9fe7bf3854e7dc119b916b620868638bff`；其中实际
编译器为 `f2d7a39c4b81a00fff191c89ac847b09af6a62e9`，网站/工具为
`0d814dda56009af89ab60e77fcf77ef32d3a63bd`。本合同先于 A06 实现提交，
独立分支 `feat/single-file-external-recovery`；本节是验收要求，不是通过声明。

本片接通单文件 consumer 对已有、源可用、已生成 Go 和合法 metadata 的外部公开
enum、具体 Option/Result 实例，以及已登记 Default、Clone、Debug、Serialize、
Deserialize、PartialEq/Eq 的恢复。派生/manual 来源和签名沿用现有目录协议，
手写 Default/Eq 的已有入口不等于新批准手写 Clone/Debug/serde 登记。
不复制 std/serde 类型、不新增公开库 ABI 或 metadata schema，不依赖 helper 名猜测协议。

### F01：一个输入 owner、同一依赖图、真正的包身份

规范化输入文件路径后，从其自身祖先选择最近的 `go.mod`；嵌套 module 优先，
module directive 加输入目录相对路径决定 consumer 的真实 package path。
文件经 symlink 到达时，配置归属和 Go 执行路径须一致，不能借逻辑路径绕过 internal。
配置已知时，metadata 查询、类型检查和最终 build/run 使用同一 module 图、Go 配置、
go.sum 与 relative replace 含义。无关 cwd、ambient GOWORK 或 `--runtime` 不得
替换 owner，也不能从 compiler runtime 偷带 producer/std/serde 依赖。
缺少依赖须要求 consumer 配置，不隐式合并 runtime 的 require/replace 改写选版。

这与目录模式的配置责任一致。单文件 build/run 应真正可运行，不能只验证 transpile
后由测试手动补依赖。输入 module、依赖、metadata 和配置文件保持不变；Go 只能修改
明确拥有的临时产物/缓存。若使用临时 modfile 或 Go source overlay，它们只提供原
配置与指定生成文件的受管视图，不改 module identity、不替换依赖代码或真实源文件。

已知 module 模式的 `-o`、`--source-map`、`--expanded` 输出须是源 module 外的
全新路径；所有路径在写入任何产物前一起检查，已有文件（包括 symlink/hardlink）
不覆盖。默认 build 输出若位于源 module 内，须显式给外部 `-o`；transpile/expand
不指定 `-o` 时仍输出 stdout。临时目录也不得落入源 module；standalone 旧模式不变。

没有输入祖先 Go module 的 standalone 旧模式保留；它不凭 cwd 声称取得一个真实
consumer package identity。TOML/workspace 的单文件 target/features 选择、单文件
`--tags`/`test` 及 use/pub use 不在此新定，沿用明确的目录入口边界，不隐选目标。
这些工程余项继续列账，不代表永久语言限制或已批准延期。

### F02：同一文件在两种入口恢复同一已登记能力

producer 通过目录/模块入口生成普通 Go 和 metadata，公开 API 例如：

```goxide
package model
pub enum Event { Idle, Count(int), }
#[derive(Default, Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
type Item struct { N int }
```

consumer 的 `go.mod` 明确依赖该生成模块；同一个指定 `.gox` 中写：

```goxide
package main
import p "a06.example/producer/model"
type Local = p.Item
type Level = Local
#[derive(Default, Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
type Envelope struct { Item Level }
func count(e p.Event) int {
    return match e { p.Event::Idle => 0, p.Event::Count(n) => n }
}
```

联合验收实际运行 producer、单文件 consumer、目录 consumer 和第三个原生 Go
consumer。单文件与目录应得到相同业务结果：qualified 构造/match、已登记 sum 的
`?`、Default 调用、Clone 后修改副本而原值不变、Debug、equals、JSON 往返与错误
cause/path。保留 nil/empty、选中 arm、非法 enum 在 guard/业务前拒绝的既有规则。
原生 Go 只调用公开 Go API/helper；删除 metadata 后其构建仍不要求增强加载器。

### F03：canonical identity 和文件选择

直接 import、producer 的公开透明 relay alias、consumer 包级多级 `=` alias
恢复同一真实类型的登记。整个单文件编译共享一个 importer 和已验证 registry，
恢复、alias、sum/serde/equality/refs 和最终类型检查使用相同的 Go 类型对象。
不能用路径/类型名字符串把不同 `types.Package` 实例或不同 nominal 类型强行判等。

单文件只编译指定源，不扫描或拼接同目录 `.go`/`.gox` sibling。单文件 build 中
未声明的 sibling helper 仍失败，目录对照可成功；坏 sibling 不影响指定文件。
被实际 import 的已有 native/generated Go 包按原 module 图加载，不自动编译依赖中
任意 `.gox` 源码。保留旧单文件无 `pub` 的本地 enum 和条件性类型检查行为；
不能直接改走完整 CompilePackage 而悄悄增加目录声明规则或输出发布 artifact。

### F04：黑例与可见性

| 输入 | 必须保持的结果 |
| --- | --- |
| 缺失、陈旧、错 schema/artifact version/包路径/marker/file set/hash/依赖闭包 | 在 consumer import 处拒绝；普通 API 调用也不能让带 marker 的包静默降级为未验证 Go 视图 |
| 正确拼写甚至正确签名的未登记 helper | 不恢复协议；各协议分别有反例 |
| 重算摘要后的伪 relay 登记、替换 helper owner、伪 validator、伪造 derive/manual 来源 | 仍由真实 Go identity 与 canonical 定义包登记拒绝，不能只靠摘要完整接受 |
| `type New p.Item`、同名异包、alias 指向新 defined、错误 serde 方向或 Eq 提升 | 不继承原类型协议；不 widening 或擦除为 any |
| consumer 合法访问本 module 的 internal native/generated 包 | 保持 Go 允许的访问，frontend 与最终执行路径一致 |
| 源码直接 import 另一个 module 的 internal/private 对象 | 拒绝；importer 能载入不是可见性授权 |
| 公开 facade 的普通 Go API | 保持可用；不因内部表示不可命名而误禁普通调用 |
| facade 的增强操作需要消费者不可导入的 internal validator/payload/helper | 保留明确工程边界；不强行生成不可访问 import，不复制原类型或制造新 relay 协议 |
| readonly/reference 擦除、single-file use/pub use 或 imported-use metadata 守卫 | 原拒绝保持；不借 A06 打开其他协议入口 |

配置验收包含 unrelated cwd、cwd 冲突依赖、移动完整 fixture 后的相对 replace、
嵌套 module、symlink 和 source/config 前后字节核对。没有新语义决策才能接受的输入
继续独立报告具体例子和候选行为；不能弱化验证或禁用测试取得成功。

### 交付门槛

保留基线真实失败，先合同、后实现；独立审查准确源码。producer/各 consumer 的实际
CLI、生成 Go race/vet/run、重复输出确定性与拒绝定位分别报告。随后在独立干净检出
运行完整 Mac race/vet/design 和仓内 CLI/WASM 七阶段；原 105 fixture/11 示例不削弱。
跨包能力由原生 CLI 证明，浏览器仍限已有单文件可用包，不宣称任意 module 加载。
compiler pin、网站/工具、最终报告 SHA 分开记录；普通分支提交/push 后核对本地、
fetch 与远端。无 Linux/托管 CI 证据不称通过，不 merge main/tag、不部署网站。

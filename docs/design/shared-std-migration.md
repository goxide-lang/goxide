# 共享 std、独立 serde 与 sum 推导迁移计划

状态：**独立库已公开提交，compiler 整合进入最终验收**（2026-10-04）。编译器基线 `ae61ec628b55e0fc66ce96d47261c0a6809c0280`，独立工作分支 `feat/shared-std-sums`。本文替代[此前研究](../rfcs/research/shared-sum-abi-and-inference.md)中“先同仓库”的建议；其已验证的 Go 机制与 ABI 风险仍适用。先提交本文与 API 合同，再实现。std 的“稳定核心”是分类，不构成 v1.0、长期 ABI 冻结或发布承诺。

## 1. 仓库、模块与职责

| 仓库 / module path | 首轮布局 | 边界 |
| --- | --- | --- |
| `github.com/goxide-lang/std` | `sum` 普通 Go 包 | 共享 Option/Result 表示、cases、构造、形状检查；不容纳格式/serde 协议、编译器或一般 runtime lowering helper |
| `github.com/goxide-lang/serde` | 根包 `serde` 为 core 协议，`json` 子包为 JSON 实现 | 保留 core/format 分层；JSON 导入 core，core 不导入 JSON；不把 serde core 塞进 std |
| `github.com/goxide-lang/next` | 本轮为空 | 实验性能力，未定 API 不为填仓库而实现 |
| 现有 compiler 私有仓库 | frontend、metadata、派生与 lowering、RFC/验收 | 本轮不把整个 compiler、RFC、历史、凭据或现场日志搬到公开库 |

serde 的明确坐标：core `github.com/goxide-lang/serde`；格式 `github.com/goxide-lang/serde/json`，源码可使用 `serdejson "github.com/goxide-lang/serde/json"` 避免与 Go encoding/json 混淆。唯一整合 owner 已统一迁移包路径、compiler import 与 field protocol identity。core 使用格式无关协议；现有 Option 缺失/null→None 独立于 Default，default/skip 才走 Default，保持不变。

新环境已通过正常 Git 发布并核验 std `3f2204770fd3b09cf9f5a1d9228464b7f7c1bdcd`、serde `a1b791e96d660c6be620e110c566a9a171631b7e`。精确 pseudo-version 和 checksum 由 Go 实际解析；compiler 无 std/serde 本地 replace。next 保持空仓库。未创建 token、改变可见性、tag 或 release。详情见[整合记录](../reviews/public-library-migration-20261004.md)。

现有 compiler 根 [LICENSE](../../LICENSE) 是 Mozilla Public License 2.0。迁出的已有代码保留现有许可文本及已有通知，不擅自改为 MIT/Apache 或添加另一许可证；候选库保留来源清单。公开交付仅包含用户要求的库源码、库测试、go.mod、必要说明和原许可，不包括私有 compiler 内容。若后续发现单独文件许可/来源不明，列出该文件再处理，不自行宣称已完成法律审查。

## 2. std/sum 首轮 API 合同

canonical origin 为 `github.com/goxide-lang/std/sum`，包名 `sum`，最低 Go 版本沿本项目当前 1.27。当前形状 ABI 标识 `GoxideSumABI = 2`（schema version 另算）。标识不能替代 go/types 签名和类型身份校验。

```go
package sum
const GoxideSumABI = 2

type Option[T any] interface { isOption(T) }
type Some[T any] struct { Value T }
func NewSome[T any](value T) Option[T]
func None[T any]() Option[T]
func ValidateOption[T any](value Option[T])

type Result[T,E any] interface { isResult(T,E) }
type Ok[T,E any] struct { Value T }
type Err[T,E any] struct { Value E }
func NewOk[T,E any](value T) Result[T,E]
func NewErr[T,E any](value E) Result[T,E]
func ValidateResult[T,E any](value Result[T,E])
```

上面是合同签名摘要，不是含函数体的完整 Go 文件。值 cases 在 std 内实现私有 marker；marker 包含所有泛型参数，Err 不丢 T，Ok 不丢 E。Option 的 nil 为 None；Some 的载荷即使是 nil 也不等于 None。Result 的 nil 非法；NewErr 不根据 error 是否 nil 改成 Ok。普通赋值遵循 Go 浅复制，Clone 仍是显式协议，不自动复制指针/slice/map 指向的数据。

shape validator 必须 exact-case switch。外部 embedding 可以提升 marker，case 的指针也可能实现接口，因此 marker 不是完整有效值检查；这些值（包括 typed-nil case pointer）必须拒绝。普通用户 enum 的 ABI、复制和检查规则不因本轮整体改写。

用户 Go API 兼容层保留：

```go
// 生成在用户包，类型别名共用真正的 std 身份。
type Result[T,E any] = sum.Result[T,E]
type Option[T any] = sum.Option[T]
type GoxideResultOk[T,E any] = sum.Ok[T,E]
type GoxideResultErr[T,E any] = sum.Err[T,E]
type GoxideOptionSome[T any] = sum.Some[T]
// 旧 GoxideNew* 名称保留薄函数；新内部降低可直调 sum.New*。
```

共享 core 不导入 compiler 或用户包。具体 payload validator、derive/free helper、From 及需要用户类型可见性的代码继续由适当的源包生成；不承诺零 helper。std 不反射猜测用户 enum 或协议。

## 3. metadata 7 与实例 owner

共享 std 阶段将 enum metadata schema 从 6 升到 7；正式命名阶段再升至 8，旧 artifact 明确要求重生成，不静默接受旧身份。普通用户 enum 的表示不变，但其与 sum 混合的 artifact 也按版本重建。

生产接入已将 family 记录增加 `ABI`（共享 sum 为 1）；`DefinitionPath/DefinitionName` 是 std canonical origin；case/constructor 从同一 origin 解析。具体 `enumFamilyInstanceExport` 改为 `ValidatorPath, Validator`，把实例检查归属与家族定义分离。泛型 std 家族声明不要求穷举 concrete instances；实际使用的具体实例仍需可信检查记录或本地静态生成检查。

必须同步：

1. `enum_exports.go` 导出 alias 的真实 origin；不把每个用户包重新宣称为家族定义者。调用方生成的 concrete validator 登记自己的 owner。
2. `enum_artifact.go` 分别验证 family origin 与 instance owner 的真实签名、phantom 参数、case、constructor、共享 ABI。通过现有 artifact/依赖指纹绑定 owner 来源，不能仅依据同型函数名信任其载荷契约。
3. `imported_family.go` 在正确 owner 引用 validator；不能一律从 std 查找。若 owner 位于不可达 internal 包，必须由可达 facade 提供已登记并可核查的转发，或给出明确限制诊断；不能削弱成 shape-only 检查。
4. typed registry 对本地透明 alias 使用 std origin，不再要求 named origin 属于本地包。部分/重排泛型 alias、跨 module 及普通 Go consumer 必须保留真实身份。
5. 本地具体 payload 检查沿用现有共享递归投影规则，保留非法 enum、具名容器、跨文件 import scope 的回归；canonical family 核验只比较 ABI 描述，不要求各 producer 的实例集合等于 std。多个同型 validator 必须先核对相同 payload 身份与已登记契约，冲突即拒绝；等价候选按 owner path/helper 名稳定排序，本地合法静态生成实例优先。不得仅凭函数签名把未登记 helper 加入候选。验收包含篡改 owner path、同签名未登记 helper 与候选顺序变化。
6. `?` 的单次求值/函数帧、失败转换和目标构造保持原逻辑。From 仍可赋值优先，然后精确唯一一步；不搜转换链，不自动丢弃 partial.Value。`go_result!` 的原 E nil 判断不受 expected Result 类型改写。

## 4. 版本与迁移兼容

首轮用精确提交或对应 Go pseudo-version 固定 std/serde；本地隔离测试使用 v0.0.0 + 明确 replace，仅是 fixture，不将未发布版本写成可下载依赖。新环境实际有提交后，再把真实版本写入 compiler go.mod/生成项目依赖。不得伪造 tag、pseudo-version 或 Go checksum。

现有项目锁与 dependency fingerprint 应包含所选 module version/replace 内容和共享 ABI；Go MVS 只选择模块版本，不证明 ABI 兼容。旧生成 Go/.a 的私有 marker 属于旧用户包，无法用新增 alias 追溯修复，生产者、消费者、metadata 和浏览器 export archives 统一重生成。源码公开类型/构造函数名尽量兼容，反射包路径会变化；不承诺旧二进制或旧 artifact 可混用。

serde 迁移需把编译器生成 import、协议 identity 校验、原生手写实现、fixtures 与 JSON 全部切换为同一新 core path。若保留旧 path 的过渡 shim，只能透明转发/alias 同一 core identity，不能复制出另一套协议；是否保留 shim 由同一迁移阶段统一记录，不让 std 任务先创建旧/新双协议。

## 5. 家族限定构造与 pattern 推导

首轮采用 `Option::Some/None`、`Result::Ok/Err`，不引入裸 variant 名称解析。显式完整参数继续支持；已实现 `go_result!(call)` 不改规则。

可用上下文：词法最近函数/闭包的 return 类型、有类型变量初始化、已声明赋值目标、已解析函数签名的调用参数，以及 match scrutinee 的真实类型。Some 只有 T，可从明确载荷类型（包括常量通常默认类型）推得；None 或 Result 未给出的另一侧必须有唯一约束。多 RHS/多返回/variadic 等组合不得错配 expected type，不支持的组合须明确诊断而非猜测。

```goxide
func absent() Option[string] { return Option::None }
func ok(n int) Result[int,error] { return Result::Ok(n) }
func fail(e *Failure) Result[int,*Failure] { return Result::Err(e) }
var r Result[int,error] = Result::Ok(1)
match r {
    Result::Ok(n) => { consume(n) },
    Result::Err(e) => { report(e) },
}
```

不可接受：无上下文 `Option::None`、独立 `Result::Ok(1)`、独立 `Result::Err(e)`、只有 nil 的未知 E、互相冲突的明确参数。不得默认 any/error，不把错误动态类型用于推导，不隐式 From/装箱来解约束、不丢 partial.Value。明确目标 E=error 时，载荷仍按普通 Go 可赋值规则检查，不改变 go_result! 对原调用静态 E 的适配。

实现应把缺参节点保留到 typed 阶段，用真实 expected type 补齐后登记 concrete instance；不能先登记 Result[any,error]。pattern 降低需要延迟到 scrutinee 类型确定后，不能任取第一臂决定家族。现有 match 分支结果推导保留；首轮不顺带实现无 expected type 时跨分支补齐 Result 两侧的全局联合推导，也不实现完整用户泛型/全程序推导。

## 6. 分工与改动接口

| 工作 | 主要文件/接口 | 协调方式 |
| --- | --- | --- |
| std 共享表示与别名 | `internal/enums/builtins.go` 的 Builtin/builtinDeclarations/builtinInstances | std 任务拥有 ABI；可读性任务先不改这些函数的家族/constructor身份 |
| schema/origin/owner | `internal/frontend/enum_artifact.go`, `enum_exports.go`, `imported_family.go` | std 任务拥有 family 段；serde 任务只在约定阶段改 field protocol identity 段，避免并写 |
| typed 注册/推导 | `internal/propagate/lower.go`, `go_result.go`；拟新增独立 sum 推导模块；`frontend/compile.go`, `package.go` 接入 | std/推导任务；可读性私有名改写随后适配生成接口 |
| serde 独立迁移 | `serde/`, `serdejson/`, serde derive/import/验证及相关 fixtures | 由接续的唯一迁移 owner 完成：公开 core/json 已发布，私有旧库实现移除 |
| 可读性 | 空 payload switch、私有短名、无 guard match if/else；AST格式/映射 | 独立任务；保留 exported GoxideInst ABI名，不能删除验证或改 break 目标 |
| 网站 | `website/build.py` export 闭包/digest、示例及 QA | 合流后统一重建，避免混用旧 compiler/新 std；暂不部署 |
| use | RFC 0021 现状与后续文档 | 另任务负责，不新增 use 语法 |

本轮共享库会减少通用生成声明，但实例检查与控制流 helper 仍合理存在；配套可读性研究没有证明性能加速，不能宣传为已测加速。

## 7. 分阶段验收与交接

- [x] 用户确认仓库边界；本计划记录明确 std API、serde 坐标、schema/兼容与任务文件所有权。
- [x] 文档独立审查通过；按阶段提交 push/fetch，随后隔离准备 std 候选库及 provenance，不推新公开库。
- [x] 新环境核实三个 repo；std/serde 真实提交与可下载依赖已核验，next 不填充。
- [ ] 实施共享 std/schema/registry；真实 goxide 多包和独立模块测试直接互赋、Go consumer、generic alias、问号/From及保值结构。
- [ ] 迁移 Eq/Clone/Default/Debug/serde 注册与 Option 缺失/null；测试 std/serde 核心 identity 同一，不能只跑纯 Go 原型。
- [ ] 家族限定缺参构造及 pattern：return/assignment/callarg/scrutinee 正例，信息不足/冲突/typed nil/partialvalue负例。
- [ ] 全树 race/vet、每个生成模块独立 race/vet、设计检查；网站真实 CLI/WASM 差分与 export data，独立审查后报告完整 SHA。不以机制探针代替生产验收，不合 main、不 tag/release、不部署网站。


当前实施证据：`internal/frontend/shared_std_test.go` 已改用真正发布的 std 依赖执行 goxide→Go 独立模块，并拒绝旧 schema 7。`shared_std_artifact_test.go` 核验 owner 路径、同签名未登记 helper 与候选顺序。serde、网站 catalog、可读性和首轮 TOML 格式迁移已整合；最终整树结果以整合记录为准，不把重点测试冒充完整交付。

用户随后批准统一项目模型：从 Go 迁入时由 go.mod/go.sum 迁成 goxide.toml+goxide.lock，混合指 .go/.gox 源码。该后续接口由独立工具链任务拥有；当前测试使用现有 manifest 模式，不把根 go.mod authority 固化为长期架构。公开 std/serde 是普通 Go 发行模块，继续保留其 go.mod。


## 显式 std 具体实例登记（2026-10-08）

直接导入 `github.com/goxide-lang/std/sum` 后，`s.Option[T]` / `s.Result[T,E]`
与兼容的裸家族使用同一 canonical 身份。已支持的限定构造、match 和 `?` 应为
实际使用的具体参数登记本地实例及完整 payload validator，不要求用户先用裸
Option/Result 触发登记。不以名称、相同底层结构或 shape-only validator 代替
canonical 类型、全部实参和载荷契约核验。

保留现有 use/透明 alias 路径、生成 Go 的 Option/Result/Goxide* 兼容 API、
exact-case 检查、递归载荷检查及 `?` 单次求值/最近函数帧。手写 generic alias
和新 defined 类型的既有边界不放宽。验收包括直接构造/Go 构造函数、match/传播、
跨文件 import 环境、嵌套非法 enum、typed-nil/指针/伪 case 及重复生成一致性。

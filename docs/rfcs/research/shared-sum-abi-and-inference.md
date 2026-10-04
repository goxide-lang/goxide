# 共享 std Option/Result ABI 与上下文推导研究

状态：**研究方案，未实施生产 ABI 迁移**。用户已明确 Option/Result 应随 std 共享发布；本文的包路径、ABI 版本和推导语法细节是建议，不把它们当作已批准。基线 `ae61ec628b55e0fc66ce96d47261c0a6809c0280`；站点部署提交 `9e938712` 不作为旧代码回退理由。普通 enum 保持 Go 浅复制和显式 Clone，不涉及 full borrow。

后续批准：用户已指定独立 `goxide-lang/std`、`goxide-lang/serde`、`goxide-lang/next` 仓库；本文同仓库建议被[迁移计划](../../design/shared-std-migration.md)替代。生产实现与可复现研究仍分开记录。

## 1. 已证实现状

Option/Result 目前是前端内建家族，但其 Go 定义**每个使用包生成一套**，不是从共享 std/runtime 导入。目录编译会处理包内重复定义，不等于跨包共享。现有 [runtime](../../../runtime/constraints.go) 是 lowering 支持，不含 Option/Result。

关键源码：

- [builtins.go](../../../internal/enums/builtins.go)：`Builtin` 指定本包 marker/case/constructor；`builtinDeclarations` 生成泛型接口、case、构造函数、形状检查与 Go 公开 aliases/wrappers；`builtinInstances` 另生成具体载荷 validator。
- [enum_exports.go](../../../internal/frontend/enum_exports.go)：本包生成家族的 DefinitionPath 是生产者包路径。公开 Go case 如 `HgoResultOk` 是本包 case 的透明 alias。
- [enum_artifact.go](../../../internal/frontend/enum_artifact.go)：当前 schema 6 验证 canonical origin、私有 marker、phantom 类型参数、case/constructor 签名和具体实例 validator；validator 仍从家族定义包 `origin` 查找。
- [imported_family.go](../../../internal/frontend/imported_family.go) 与 [lower.go](../../../internal/propagate/lower.go)：消费者按真实 Go 类型身份恢复家族，要求生产者匹配的具体 validator；不根据短名称认协议。
- [question.go](../../../internal/propagate/question.go)：`?` 校验源实例、提取成功值，Err 使用目标家族 constructor 重建；可赋值优先，再唯一精确 From，不搜转换链。

真实 hgo fixture：a、b 两包都返回 `Result[int,error]`。a 的 2 行源码生成 50 行 Go；b 经 `a.Make()?` 重建本包 Result，生成 98 行。数字只描述本 fixture，不是性能结论。生成片段为：

```go
// a 与 b 各自产生以下接口，marker 的包身份不同。
type Result[T any,E any] interface{ __hgo_enum_marker_Result(T,E) }
// 每包的动态 case 也不同。
type HgoResultOk[T any,E any] = __hgo_enum_case_Result_Ok[T,E]
```

实际生成 Go 的 race/vet 通过；直接 `var _ b.Result[int,error] = a.Make()` 编译失败，诊断包含不同包的 unexported method。因此 `?` 能工作，不证明两个包的 Result 已共享或可直接赋值。

**原因分层**：Go 泛型允许共享定义，并不要求每个用户包重新声明；当前包级 sealed marker/名义身份、前端 registry 和 metadata 约定共同维持旧表示。Go 不会自动完成 hgo 的跨包重建或 `?` 语义。

## 2. 推荐最小共享 std ABI

建议先在现有 module 内提供一个普通 Go 包（暂称 `github.com/hxsf/goxide/std/sum`），随匹配编译器版本交付；不是新建或发布独立仓库。逻辑上属于 std 基础类型，具体目录名与公开版本策略仍待确定。不把当前 compiler-private runtime 直接当作稳定公共 std。

保留当前表示：

- `Option[T]` 为共享包私有 marker 的接口；None 仍为 nil，Some 是持有 `Value T` 的值 case。
- `Result[T,E]` 为共享包私有 marker 的接口；Ok/Err 各一个字段，marker 携带 T、E，避免 phantom 参数丢失；nil Result 非法。
- std 提供共享 case、构造函数和**仅形状** validator。用户包生成 `type Result[T,E any] = sum.Result[T,E]` 等薄 alias 以保留现有源码和 Go 导出名字；现有 `HgoNew...` 可保留短转发函数（Go 没有泛型函数声明 alias）。新降低直接引用共享构造函数，不必层层转发。
- 私有 marker 不能单独保证闭合值集合：外部 embedding 可提升 marker，case 指针也有值 receiver 的方法集。必须保留 exact-case switch，拒绝冒充值、typed-nil case pointer 和 nil Result。隔离探针覆盖这些值。
- **具体载荷检查继续由知道 T/E 的包静态生成**，std 不能仅凭 `T any` 得知某个用户 enum 的已登记检查。用户 enum 的非法 nil、具名容器递归投影、Default/Clone/Debug/equality/serde 所需 helper 不得被通用 shape check 替代。优先保留静态直接调用，不为“代码变短”引入反射/全局注册表/闭包分配。

机制原型：

```go
// 共享包 sum；仅示意，完整可运行版本在探针脚本中。
type Result[T,E any] interface{ result(T,E) }
type Ok[T,E any] struct{Value T}
func (Ok[T,E]) result(T,E) {}
// 用户包/Go API 的兼容 alias：
// type Result[T,E any] = sum.Result[T,E]
// type HgoResultOk[T,E any] = sum.Ok[T,E]
```

隔离三个独立 go.mod（std、producer、consumer）已验证 generic alias 后同身份赋值与 Go type assertion、None/Some 区别、slice 浅复制、非法形状拒绝；race/vet 通过。phantom 参数不同、伪造同名 marker、向外部 alias 加方法均按预期编译失败。js/wasm 构建与 export data 生成通过，**尚不是 hgo/WASM 前端迁移通过**。

Go 的透明 alias 保持目标类型身份，非本地类型不能通过 alias 获得新本地方法；普通泛型调用不会仅因外围 return 类型而补出未见参数 E。依据 [Go specification：alias](https://go.dev/ref/spec#Alias_declarations)、[methods](https://go.dev/ref/spec#Method_declarations)、[inference](https://go.dev/ref/spec#Type_inference)，并由本地 Go 1.27.1 探针验证。

## 3. 不能遗漏的迁移工作

1. **拆分家族 origin 与实例检查 owner**：当前 `enumFamilyInstanceExport` 只有 Validator 名称，`validateEnumFamilies`/`importedFamily` 都从 origin 找函数。共享 std 不可能定义含消费者私有 T/E 的所有实例。新 schema 应记录可验证的实例 owner path、helper identity、签名及 canonical family ABI；不得把任意同名函数当作协议。家族描述与具体实例列表解耦，shared std 的家族无需穷举用户实例。实例 owner 可能位于消费者不可导入的 internal 包；std 可达不代表 validator 可达。需设计经过来源验证的公开 facade 转发，或明确诊断该限制；owner artifact 与依赖指纹须绑定，函数签名同型本身不能证明载荷契约。
2. **typed registry**：本包 alias 的 `types.Unalias(...).Obj().Pkg()` 会变成 std；`family` 的“本地 named 属于本包”分支必须改为可信 canonical std origin。支持部分/重排 generic alias；保留旧类型参数可见性边界，不降级 any。不同生产者相同实例不能靠随机枚举 helper 选取未经核对的载荷契约。
3. **constructor/match/问号**：所有 CaseName、Constructor、Validator 来自共享家族描述；`?` 仍保持 operand 求值/函数帧、显式 From、保值结构、不改变 E 的 nil 判断。不能只换生成字符串而遗漏 metadata 和类型分析。
4. **methods/derive**：共享类型的方法只能定义于 std 所属包。当前具体 derive/free helper 与用户声明的协议登记仍归正确 owner；审查 `GenerateSum`、Default、Clone、Debug、equality 与 serde Option 特判。普通 Option 缺失/null→None 仍独立于 Default。不得靠新“通用方法”绕开既有字段协议。
5. **Go consumer/兼容**：保留公开 aliases/constructor 名可降低源码迁移成本，但这是 ABI 改变，旧包的 marker 与新 std marker 不兼容。建议首轮要求生产者、消费者及 `.hgo` metadata **统一重生成**，旧 artifact 给出明确版本错误。已有 `.a` 不能用 type alias 修复；反射显示的包路径也会改变。若未来需混用旧 ABI，应另做显式 adapter 方案，不默默转换。
6. **模块与 WASM**：把 std 加入生成 go.mod/选版图、编译器信任描述、浏览器 export archives 依赖闭包与 compiler/export digest。当前 [website/build.py](../../../website/build.py) 只列现有 runtime/formatting/serde 等；offline export data 必须与同版本 std 一起构建。多模块 Go 版本选择不等于 hgo ABI 协商；版本线需和刚完成的 source-feature 图集成验收。

建议迁移顺序：定 canonical 路径/ABI → std 机制库及 schema → frontend 同步切换与完整重生成 → 独立模块/alias/Go consumer/derive/非法嵌套值/问号回归 → 全树 race/vet 和网站 CLI/WASM 差分。无需先实现完整 trait、full borrow 或通用 Rust std。

## 4. 官网泛型重复：能推导与尚未实现

[官网 result.hgo](../../../website/examples/result.hgo) 的重复参数来自当前语法/登记要求，**不是所有位置都缺信息**。

| 位置 | 实际现状 | 推荐规则/信息来源 |
| --- | --- | --- |
| `go_result!(call)` | 已实现 T/E 推导 | 原生函数实际签名；不是一般构造推导 |
| `return Result[int,error]::Ok(n)` | 参数目前须显式 | 返回类型已给 T/E，可写 `Result::Ok(n)` |
| `return Result[string,PortError]::Ok(text)` | 同上 | 以 expected return 类型检查载荷 |
| `return Option[string]::None` / Some | 同上 | 返回类型给 T，None 不需要猜 |
| 有类型的初始化、已声明变量赋值、固定签名调用参数 | 构造缩写均未实现 | 从唯一 expected type 向构造表达式传递 |
| `match nextPort(text)` 的两个 Result patterns | scrutinee 的真实类型已知，但 patterns 仍须参数 | 两臂均可用 `Result::Ok/Err`；验证同家族，不由第一臂任意决定 |
| `match nameLength(names)` 的 Some/None patterns | 同上 | 可省 `[int]`，由 Option[int] scrutinee 给出 |
| wildcard-only match | 已实现真实 scrutinee 家族识别 | 不能由此声称所有 shorthand patterns 已支持 |
| `n := match x { explicit Some(v)=>v, None=>0 }` | 分支结果推导已有实现，探针通过 | 与 pattern 的泛型省略是两件事 |
| `x := Option::Some(1)` | 当前拒绝，但信息足够 | 可按通常无类型常量默认规则推得 Option[int] |
| `x := Option::None` | 无 T 信息 | 明确报错，要求目标类型/显式 T |
| `x := Result::Ok(1)` / `Result::Err(err)` | 只知道一侧 | 无 E / T 信息，不能默认 error/any |

建议用户可见前后例子（**右侧为拟议语法，尚未实现**）：

```hgo
// 当前
func firstName(names []string) Option[string] {
    if len(names)==0 {return Option[string]::None}
    return Option[string]::Some(names[0])
}
// 建议
func firstName(names []string) Option[string] {
    if len(names)==0 {return Option::None}
    return Option::Some(names[0])
}
// 当前 match 重复 Result[string,PortError]；建议从 nextPort 的签名取得：
match nextPort(text) {
    Result::Ok(port) => { fmt.Println(port) },
    Result::Err(err) => { fmt.Println(err) },
}
```

优先推荐**保留家族限定、省略泛型参数**，先不引入裸 `Ok`/`Some` 的全局名称解析/导入竞争。裸 variant 也可设计为只在唯一已知家族上下文中识别，但属于下一层语法选择，不应偷带进 ABI 迁移。

## 5. 推荐推导约束与黑白例

采用局部双向约束：先使用明确 expected type，再从有效载荷获取已知参数；跨嵌套 match/constructor 以 typed 固定点传播，未决不能伪装为 any/error。普通未定类型常量最后按 Go 默认规则处理。显式参数与 expected type 不一致报错，不能改写用户明确类型。

- 白例（拟议）：返回/变量声明/赋值目标/call 参数为 `Result[int,*Failure]`，`Result::Err(e)` 可得 T/E；`Option::Some(1)` 无上下文可得 int；`Option::None` 有 Option[string] 目标可得 string。完整上下文确定 E=error 时，载荷按普通 Go 可赋值规则检查——这是用户声明的目标类型，不是把未知 E 自动选成 error。
- 白例（后续可做）：`r := match flag { true=>Result::Ok(1), false=>Result::Err(e) }` 若两臂及 e 的**静态**类型唯一约束 T/E，可联合推导；不能沿用“任取一臂类型”实现。现有分支结果推导尚不能构造缺参节点，这是实现缺口。
- 黑例：无上下文的 None、只有 Ok 或只有 Err；两臂给出不一致的具体载荷类型且无明确公共目标；错误侧仅 `nil` 无具体 E。即使 enclosing 函数返回 Result，也不把它的类型任意灌给函数中所有独立局部变量。
- 推导不搜索隐式 From，不自动提升兄弟错误为 error，不把 `go_result_keep!` 的整个失败结构投影成 `.Err`，不把 E 换成其运行时动态类型。`?` 保持既有可赋值与唯一一步显式 From 规则。
- 用户泛型函数、一般多态局部绑定、全程序推导不应捆绑进这次便利语法。默认参数、裸 variant 以及跨分支联合推导的首轮范围应明确记录。

## 6. 实证与项目待办

复现脚本：[shared_sum_probe.py](../../../scripts/research/shared_sum_probe.py)。使用真实 Go 1.27.1、可写 GOCACHE/TMPDIR，并执行：

```sh
python3 scripts/research/shared_sum_probe.py /workspace/shared-sum-evidence
```

本轮 31 项命令结果符合预期（包含应失败负例）。脚本保存每个编译/运行日志、生成 Go、输入 fixture 和 results.json；提交的真实生成快照见 [a.go](../../../scripts/research/testdata/shared-sum/current-a.go.txt) 与 [b.go](../../../scripts/research/testdata/shared-sum/current-b.go.txt)。hgo 部分真实构建本基线 CLI、转译、生成模块 race/vet；共享 std 部分是隔离 Go 机制试验，**不作为 hgo 迁移完成的证据**。此轮不改生产代码，不发布 std。独立审查通过，并据审查补测仅改变 E 的 phantom 负例、typed-nil case pointer；设计检查通过 81 个 Markdown、23 个 RFC 与 13 条 Go 机制命令（后者不作为 hgo 迁移证据）。

- [x] 确认每包生成与跨包不同身份，真实 `?` 重建和直接赋值负例。
- [x] 检查官网具体重复位置；分别验证 adapter、分支结果、wildcard 推导，以及现构造/pattern 缩写拒绝。
- [x] 共享包+generic alias+独立模块+普通 Go consumer 的最小机制；形状检查、浅复制、phantom 身份、方法限制、WASM 构建/export data。
- [ ] 实施共享 std 与 metadata schema/registry 迁移；全链 hgo/WASM 验收尚未开始。
- [ ] 实施构造上下文与 pattern 泛型省略；不必等待 std 迁移，但应避免同时改 ABI 和推导造成定位困难。
- [ ] 需确定：canonical std 包路径/版本约束，旧生成 artifact 是否统一重建（推荐是），首轮语法是否只含家族限定省参（推荐是）。用户已确定共享 std 方向，无需再次问“是否共享”。

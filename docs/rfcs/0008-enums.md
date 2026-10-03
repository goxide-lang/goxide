# RFC 0008：变体枚举、Option 与 Result

## 状态与范围

变体枚举、Option / Result、名义类型身份是已确认目标；本分支已实现有限值型case子集；完整RFC尚未实现。
源码拼写、泛型验证能力和 Go ABI 仍是草案。下文 interface 加值型 case 只是一种候选，不能据此排除借用模式。
本篇约束枚举值的表示、初始化和转换；分支执行与借用绑定见 [RFC 0009](0009-match.md)。
背景与已有 Go 机制实验见[枚举专题](../design/enums-and-match.md)。

## 源级规则

最新D27覆盖旧开放raw/C enum：只保留封闭变体。数值/string属于serde外部映射，未知输入返回UnknownVariant而非合法未知值；业务validator延期，不取消结构边界检查。具体属性与反例见[当前enum规则](../design/enums-and-match.md#01-封闭-enum-与外部映射)。

- 一个合法 enum 值恰好属于一个声明的变体，payload 满足对应类型约束。
- 类型身份包括定义包、枚举声明及类型实参；同名变体不使不同 enum 互换。
- `Option::None`、`Option::Some(nil)`、`Option::Some(Option::None)` 表达不同状态，不得展平。
- 普通 enum / Result 不自动取得首个变体作为默认值；Option 默认值拟定为 None。
- 普通赋值延续 Go 值复制方向，不自动执行 move 或递归深复制；含借用值仍受权限检查。
- 首版候选以无 payload 或单 payload 变体为基础；复合数据可放入 struct，多字段语法另定。

## 转译示例

**E01：构造保留变体、payload 和全部类型参数。**

```hgo
pub enum Result[T, E] { Ok(T), Err(E) }
var r Result[int, string] = Result::Ok(7)
```

```go
// 候选 A：interface + 值型 case；符号名仅为示意。
type Result[T, E any] interface { hgoResult(T, E) }
type Ok[T, E any] struct { Value T }
type Err[T, E any] struct { Error E }
func (Ok[T, E]) hgoResult(T, E) {}
func (Err[T, E]) hgoResult(T, E) {}
var r Result[int, string] = Ok[int, string]{Value: 7}
```

对应约束：marker 包含 E，即使 Ok 不存储 E。不能使 `Result[int, string]` 与 `Result[int, bool]` 意外兼容。
成本：接口装箱可能产生分配；不能承诺所有实例零分配，最终以选定 ABI 和 Go 编译结果为准。

**E02：嵌套 Option 保留层级。**

```hgo
var a Option[Option[int]] = Option::None
var b Option[Option[int]] = Option::Some(Option::None)
var c Option[Option[int]] = Option::Some(Option::Some(3))
```

```go
// 仍采用候选 A；只用 nil 表示外层 None。
type Option[T any] interface { hgoOption(T) }
type Some[T any] struct { Value T }
func (Some[T]) hgoOption(T) {}
var a Option[Option[int]] = nil
var b Option[Option[int]] = Some[Option[int]]{Value: nil}
var c Option[Option[int]] = Some[Option[int]]{Value: Some[int]{Value: 3}}
```

对应约束：a、b、c 必须能被模式匹配区分；不能以“底层都是 nil”消除 b 的 Some 包装。 此为内存中的名义值语义；普通 JSON Option 表示中 a 与 b 均可编码为 null，解码为外层 None，不承诺 JSON 无损往返或 PATCH 三态，见 [serde RFC](research/0006-serde-core-and-formats.md#72-内建具体-option-派生)。

**E03：没有合法初始化的 Result 不能成为源级可读值。**

```hgo
var r Result[int, string]
copy := r // 拒绝：r 尚未取得合法值。
```

```go
// Go 允许；此代码展示不能直接照搬的降低。
var r Result[int, string]
copy := r // nil interface，不是任何 Result 变体。
_ = copy
```

拒绝来自 hgo 初始化检查，不依赖 Go 编译器。生成代码内部可有临时零值，但不能让业务代码观察它。

**E04：外部值必须按所声明的枚举契约取得合法视图。**

```hgo
// 签名示意；输入位置的验证策略以公开 API RFC 为准。
pub func value(r Result[int, string]) int {
    return match r { Result::Ok(v) => v, Result::Err(_) => 0 }
}
```

```go
func Value(r Result[int, string]) int {
    switch c := r.(type) {
    case Ok[int, string]:
        return c.Value
    case Err[int, string]:
        return 0
    default:
        panic("invalid Result[int,string]")
    }
}
```

这是候选 A 的标量验证示例，不能直接推广到任意泛型 T。校验不调用 marker 或其他用户方法。

## 拒绝与边界

- 候选 A 中，Go 允许 `*Ok[int, string]` 满足接口；它仍不是规定的值型 case，应拒绝，包括 typed nil。
- 嵌入合法 case 的其他 Go struct 不因继承 marker 而成为新变体；白名单核对实际动态类型。
- `Result::Err(nil)` 是否合法由 E 决定；它与整个 Result 为 nil 分开判断。
- 容器扩长、缺失查询、关闭 channel 及泛型零值不能暴露非法 Result；具体容器入口见 [RFC 0010](0010-collections.md)。
- 验证共享容器只说明当次读取的值；共享写入、同步和借用仍需独立契约，不能靠一次扫描永久证明。

## 双向互操作

hgo 导入带元数据的生成包，应恢复 enum 身份、变体与约束；普通 Go interface 不自动推断为封闭 enum。
原生 `(T, error)` 改成 Result 需要实际桥接，并保留原 API 允许的“值和错误同时存在”信息；declare 擦除不能改变结果形状。
Go 调用生成包时仍使用普通 Go 类型与工具链；hgo 不向第三方 Go 程序追加静态穷尽性或借用规则。
需要公开的运行时校验随 dist 交付，其位置、失败方式及成本必须记录；不能承诺原始 Go 任意输入都满足 hgo 契约。
最终表示须兼容 [RFC 0009](0009-match.md) 的三种绑定，导出 ABI 和内部布局是否分离尚未选择。

## 验收与未定项

当前真实验收：[enum_wire业务](../../examples/enum_wire/main.hgo)与[独立测试](../../tests/acceptance/enum_wire_test.go)，包含安装CLI、Go双向调用、known往返、unknown错误先于业务。旧raw语法是前端拒绝例；数值映射不是任意Go强转。下列未实现的完整范围仍需独立验证。

1. 正例 E01、E02 保留类型参数、变体及嵌套层级；E03 在 hgo 阶段拒绝。
2. E04 对规范标量 case 通过，对 nil、指针 case 和嵌入伪造值拒绝。
3. 增补带共享字段、递归数据、含借用 payload 及跨包元数据往返样例。
4. Go 机制测试与 hgo 端到端验证分开记录；当前没有后者的实现。
5. 未定：最终 ABI、递归验证、泛型验证证据、完整初始化算法及稳定导出符号。
6. 不得先冻结值型 case，再把无法实现的原位借用默默降低成副本修改。

### 独立 enum 稳定阶段与声明分隔符（2026-10-03）

最新用户顺序为宏 → enum → 指针拆分。当前独立 enum 阶段保持 interface＋值型 case，不引入 Copy/move 或新借用模型；未来原位 payload 可写借用仍是演进目标，不再作为本阶段前置。既有引用功能保留。此调整不承诺 case 布局永久兼容，也不自动决定比较/map key 资格、非法零值或所有 Go 入口的校验政策。

已修正 `enum E { A, }; func f...` 中显式声明分号残留、导致生成 Go 顶层空声明报错的问题。仅移除 enum 后紧随的显式分号，保留间隔注释和自动换行分隔；不改变 match 语义。原来把合法表达式 match 当负例的测试迁为实际执行正例，多文件/pub enum、混合普通声明、同线多 enum、注释、EOF 与真正语义错误见[回归验收](../../tests/acceptance/enum_separator_test.go)。

### 跨 module 公开具体 enum 恢复（2026-10-03）

目录构建输出内部 `.hgo.enums.json`（协议/产物 v1）和 `HgoEnumMetadata` 常量。消费者在 enum 语法 lowering 之前恢复闭合变体集合，并检查真实 Go enum 类型、私有 marker、值型 case、构造器参数/返回和 validator 签名。元数据不保存 AST 或生产者私有载荷名；载荷类型由真实 case.Value 与构造器签名提供，所以私有类型的公开字段读取、转发和重构可继续推断。

产物绑定实际包路径/名称、逻辑元数据摘要、选中 Go 文件、源闭包与目标构建配置，递归绑定增强依赖 artifact；复用引用阶段的离线 importer 指纹机制。enum/ref 两种 marker 均先写入最终 Go，再计算各自产物，不使已有引用文件摘要失效。缺失/陈旧/错包/版本/构建变化在消费 import 处诊断；这不是生产者认证，外部 cgo 输入限制沿用现有边界。

[真实双模块验收](../../tests/acceptance/enum_external_test.go)覆盖文件级 import alias、公开透明 type alias 再导出、函数转发、闭合构造/穷尽 match、guard 前非法动态值拒绝、私有载荷推断、同名不同包隔离及定义新类型不冒充 alias。生产者已生成的 serde/Clone/Default 辅助函数可以直接 Go 调用；原生 Go 消费者不依赖 JSON。库与应用两次生成一致，独立模块分别 race/vet，引用权限恢复继续成立。

首片限定公开具体 enum 与源可用 local replace。消费者跨包 derive 协议、外部通用 Option/Result 家族元数据、单文件外部 enum 语法恢复未由本片完成；已有同 module builtin 能力保留。透明 alias 若指向消费者不可导入的 `internal` 定义包，普通公开 Go API 仍可使用，实际 enum 模式/构造语法明确诊断未支持；是否增加公开 relay helper 另需收敛，不冻结为永久延期。

本片不迁移 interface＋值型 case，不引入 Copy/move，不改变比较/map key、非法零值或全公开入口的验证策略。元数据与具体 case 导出仍为内部/版本耦合协议，不承诺未来 payload 原位引用或 ABI 迁移无破坏。

# match payload 原位引用：表示与替换合同提案

> **2026-10-09 pre-MVP 决定更新**：用户已明确“pre-mvp先让match enum 能用。不做检查。”
> 实施采用固定类型的独立原位槽，保持普通 enum 浅值复制；交付显式 `ref` / `ref mut`
> 及共用 lowering 的基础 `match &e` / `match &mut e` 自动绑定。保留已有类型正确性与
> 只读/可写权限检查，不新增 lifetime、escape、跨 variant 失效或借用风险检查；不加
> generation、受检引用 wrapper 或运行时 borrow 机制。保存、返回、捕获不被禁止，
> 跨 owner 整值替换使用旧 payload 引用不受保护，不声称 Rust 独占或借用安全。
> 下文“未批准”及长期候选推荐记录此前设计讨论，不再构成 pre-MVP 前置或重新确认要求；
> 代际受检方案仅为后续候选。设计和纯 Go 探针本身仍不代表生产实现完成。

状态：**设计提案，尚未批准 enum 布局或实现原位引用**。关联
[#14](https://github.com/goxide-lang/goxide/issues/14) 和
[#13](https://github.com/goxide-lang/goxide/issues/13)。研究基线为冻结的
[#15](https://github.com/goxide-lang/goxide/pull/15)
`906c10f302f651bb25a5630dda0489c5cab38a3a`；std 为
`9ed27c85088abd91ac7b32c98cfa532f559ad4d7`，serde 为
`f342a31e20d0be3a4c02b5c750fb609dc25a7dc3`。
本提案和机制探针不修改编译器、std ABI、网站或 #15。

## 已确认的方向与尚待决定的部分

已确认按语法位置区分：类型 `&T` / `&mut T`、表达式 `&value` /
`&mut value`，payload 模式 `V(ref p)` / `V(ref mut p)` 创建引用绑定。
引用模式 `&p` 解构已有引用，不是 `ref p` 的另一种写法。
直接删除旧 `mut&` 的引用解释，不保留兼容层；普通二元 `mut & x`
仍按表达式文法解释，不能根据空白、注释或名字的偶然绑定改变规则。
绑定本身的重赋值与目标的写权限分开处理，`ref mut p` 不另授予或剥夺
绑定重赋值能力。

用户认可显式 `ref mut` 在 arm 中修改原 payload 的目标行为：

```goxide
// 目标语法，当前前端尚未支持。
match e {
    Counter::Value(ref mut n) => { *n += 1 }
}
```

普通引用继续可复制、保存、返回和捕获；可写引用非独占。已有只读、
投影、权限擦除和跨包检查必须保留。不引入完整 Rust 所有权规则，
不以“仅 arm 内有效”或禁止其它别名重赋 enum 回避表示问题。

本阶段继续评估 `match &e` 默认只读、`match &mut e` 默认可写的
基础自动绑定。它们应与显式模式共用一个原位引擎；**这仍是建议，
不是已经实现或批准完整 match ergonomics**。详细修改面和目标测试见
[实施计划](match-payload-borrows-implementation-plan.md)。

仍需决定：enum 存储布局、variant 替换后的旧引用行为、guard 重入时
匹配哪个判别值，以及相应 Go API/零值迁移。下面给出一个完整推荐，
避免只批准拼写却暗中选择运行时语义。

## 为什么当前表示不够

[enum engine](../../../internal/enums/lower.go) 使用 interface 加值型 case；
match 保存 scrutinee、断言 case 后，通过 `binding := payload.Value`
复制 payload。取断言副本的地址不是原位引用，退出 arm 时写回也不能
提供其它别名在 arm 内观察到的立即更新。

语言目标是原位访问及普通 enum/父结构的浅值复制；复制前后取得引用
都不能改变复制规则。若另外要求引用透明表示为原生 `*Payload`，下面
几种直接表示存在不同后果。受检句柄可提供原位访问而不透明等于 `*T`，
其长期取舍另列在后文，不能把当前后端表示当成语言前提。

| 方案 | 标量 `copy := e; *p = 2` 的结果 | 代价或反例 |
| --- | --- | --- |
| 每个 variant 独立 inline 槽 | 原 e 为 2，copy 保持 1 | 旧引用跟随固定槽；布局与 Go API 必须迁移 |
| interface 加 pointer case | e 和 copy 都为 2 | 普通值复制静默变共享，不符合已要求的合同 |
| 第一次借用时 lazy boxing | 借用前的 copy 为 1，借用后的 copy 为 2 | 复制语义取决于是否借用过 |
| 编译器在赋值处插复制钩子 | 覆盖到的源级赋值可以复制 | 原生 Go 赋值、聚合值复制绕过钩子；已逸出的裸指针写入也不触发 COW |
| 带代际检查的引用包装器 | 可以检测旧引用失效 | 改变引用 ABI、自然 Go pointer 桥及替换后的访问行为；可另实现复制/保存/返回，但不是本片的透明替代 |

这些是机制反例，不是证明所有可能表示均不成立。若把透明的原生 `*T`
桥和普通 Go 浅值赋值都作为硬要求，工程上最小的候选是
**私有 tag 加每个 variant 独立、具体类型的 inline 槽**。
即使两个 variant 的 payload 类型相同也不合槽，否则 A 的旧引用会
直接改到 B 的活跃 payload。禁止偷偷装箱或使用 unsafe union。

它不是无需讨论的语言默认：下面的旧引用清零、写不活跃槽及再次活跃
确实容易令人意外。长期设计还应比较受检投影引用，不能因为当前 refs
恰好擦为 `*T` 就排除不同 ABI。

## 固定槽候选的精确合同

下面只是说明布局的 Go 模型，不是最终公开 API：

```go
type Counter struct {
    tag uint8
    value int
    other int
}
func NewValue(n int) Counter { return Counter{tag: 1, value: n} }
func NewOther(n int) Counter { return Counter{tag: 2, other: n} }
```

对 `e := NewValue(1); p := &e.value`，这个候选有以下精确行为：

| 后续操作 | `*p` | 活跃 variant 与 payload |
| --- | --- | --- |
| `before := e; *p = 2` | 2 | Value(2)，before 保持 Value(1) |
| `after := e; *p = 3` | 3 | Value(3)，after 保持 Value(2) |
| `e = NewValue(4)` | 4 | Value(4) |
| `e = NewOther(6)` | 0 | Other(6)，整值赋值清零 value 槽 |
| `*p = 9` | 9 | 仍为 Other(6)，9 留在不活跃的 value 槽 |
| `e = NewValue(5)` | 5 | Value(5)，同一槽再次活跃 |

这是普通字段指针的语义：不是引用失效，也不是引用永久保留旧 payload
对象。重新赋整个父结构复用其字段存储；把父指针或 slice header 改绑到
另一分配则不会让旧引用自动追随。array 元素及嵌套的非递归 inline
字段适用同一原则，slice/map/显式指针中的共享继续是 Go 浅复制。

需要公开说明的成本：

- 引用可能读写不活跃槽。validator、Equal、Clone、Debug、Error、serde
  和 match 的语言级行为只能按活跃 tag 选择 payload；不能要求所有
  不活跃槽永远为零。
- 不活跃槽被旧引用写入 slice/map/pointer 后可能保留对象；interior
  pointer 也可能保留整个 enum 分配。GC 存活不证明活跃 variant 或无竞争。
- 空间约为各槽尺寸之和加 tag/对齐，不是最大 payload 的尺寸。
  本提案没有零分配、性能优势或无泄漏的结论。
- 外层 variant 替换可把嵌套 enum 槽变成零值。保存的内层引用仍指向
  该存储；后续需要有效 enum 的操作继续执行已有验证，不能保证旧引用
  永远观察有效 enum。
- 当前前端不支持递归 enum 值投影。直接递归 inline 布局不可成立；
  探针中的显式 Go 间接化只解释机制，不能冒称已有前端支持，也不能
  把当前边界升级成永久语言禁令。

## 长期候选：失效检查与阻止失效

需要先选语言的可观察合同，再确定 Go 表示。这里不把新的引用包装器
当作兼容层，也不因为实现成本而自动选择固定槽。

| 候选 | A→B 后旧 p | A→B→A 后旧 p | 普通引用复制/保存/返回 | 原生 Go 与存储代价 |
| --- | --- | --- | --- | --- |
| 固定 inline 槽 | 可读零值并写不活跃槽 | 观察新的 A 值 | 可直接用 `*T`，GC 保活 | 浅值复制自然；所有槽占空间，旧引用可能保留整个 enum |
| 只检查活跃 tag 的投影句柄 | 访问明确失败 | 又允许访问新 A | 可以复制句柄 | 需要读写 lowering；仍有 ABA 重新活跃，不能宣称永久失效 |
| owner/路径/代际投影句柄 | 访问明确失败 | 旧代际仍失败 | 可以复制、保存、返回、捕获句柄 | 需要稳定 owner 和不随值复制重置的代际，以及受控的替换入口；裸 Go `*T` 不执行检查 |
| 运行时 pin 已借用槽，阻止使其失效 | 替换操作失败，e 保持 A | 第一步已失败 | 需要跟踪所有仍存活的引用 | 需要精确释放或有 GC 时机依赖的保守 pin；原生 Go 赋值/留存指针绕过拦截 |

**长期优先建议研究代际投影句柄**：它能提供“旧引用不会悄悄指向另一
次构造值”的合同，消除闲置槽写入和 ABA 的源级意外。它不必引入
Rust 独占或禁止普通引用逃逸。但是应先明确接受下面的表示和 Go 桥
变化；在这一步决定前，不能承诺完整失效检测已经可行或选定它实施。

投影句柄可以记录稳定 owner、payload 路径、权限与取得时的代际，
每次 `*p` 读写执行活跃/代际检查。普通 payload 写入不必使自身失效；
整值替换（是否包括同 variant 重构）、父对象替换和嵌套路径的代际
规则必须明确。严格失效合同应在 A→B→A 后仍拒旧 p，不能只看 tag。

代际不能简单作为 enum 中另一个随值复制的整数：普通 Go 整值赋值
可以覆盖它，把早先保存的 enum 复制回来也能恢复旧数值。要保证所有
替换都被记录，必须使用独立的 owner 存储/管理层，或限制替换为可拦截
的操作。编译器只能改写它看到的代码，原生 Go `*owner = oldValue`
没有自动钩子。可设计“Go enum 值快照 + 受检可投影 owner”两种接口，
但这涉及赋值、参数、返回、聚合存储及跨包 ABI；不能悄悄把普通复制
变成共享 owner，也不能宣称换个字段就解决。

可继续提供普通 Go 互操作，但必须决定桥的保证范围：语言侧句柄访问
可检查，交给 Go 的原始 `*T` 无法撤销。即使进入 Go 前检查一次，callee
仍可能保存该指针、经回调替换 enum，或在返回后写旧位置。现有机制
探针明确显示：句柄检查已失败，先前得到的裸指针仍能写旧对象。
因此以下承诺不能同时默认成立：任意跨函数保存裸 Go 指针、任意原生
Go 整值替换，以及所有旧引用访问都被强制拒绝。

可选的桥合同是显式列出未受检查的原生指针出口，或提供句柄/受检
accessor API；若要求 Go 留存后仍强制检测，就必须让它也通过句柄访问，
而不是透明接收 `*T`。这不是无差别破坏 Go 互操作，但确实是用户可见
的 API 决策；不能在本片暗加“所有 Go callee 不得留存”规则。

pin 方案把错误提前到替换处，但逃逸引用可任意复制/保存/捕获时，
“最后一个引用已经不再使用”的时刻并非词法 arm 结束。依赖 GC 回收
释放 pin 会让替换成功与否取决于回收时机；显式 close、引用计数或
scope guard 各有新的使用/复制合同。raw Go alias 同样可绕过替换入口。
这不是既有非独占规则自然推出的限制，当前不优先推荐。

空间与保留成本也不能只归因于一种方案：inline 布局对多 variant 大
payload 支付槽尺寸总和；tag/代际句柄若仍投影 inline owner，也继承
该成本。仅保存活跃 payload 的间接布局可以减少槽空间，但必须另付
分配、复制策略和 Go value ABI 代价。retained 句柄或裸指针都可能保留
owner/旧对象；“失效后访问失败”本身不会让所有旧存储立即回收。
这里没有性能排名，不需要更多微基准才能做上述语义选择。

## 固定槽候选的 guard 合同：入口快照判别

place 只求值一次还不足以定义 guard 回调重赋 enum 后的行为。
推荐保持当前按值 match 的**入口判别和浅值快照**：选 arm 时使用入口
tag，值绑定读入口活跃值快照；引用绑定指向入口确定的原槽。
共享 slice/map 内容仍然共享，快照不是深复制。

例如入口为 A(1)，按源码顺序尝试：

```text
B(_) => return "B"
A(ref p) if { replace_e_with_B_20(); false } => ...
A(ref p) => return *p
```

推荐结果是最后一个 A arm 返回 0：A 判别来自入口，引用观察被整值
替换清零的 A 槽。若最后 arm 用值绑定 `A(v)`，则 v 为入口的 1。
若第一条 A guard 返回 true，则 A body 仍执行；经其旧引用写 A 槽
不会修改活跃 B(20)。这两种观察必须在文档和测试中明确。

独立反例表明“每个 arm 重新读当前 tag”不是无代价替代：上例的 B arm
已被跳过，A guard 改为 B 并失败后，剩余 A arm 不再匹配，原本穷尽
的 match 可能没有结果。重启扫描又会改变 guard 次数、顺序甚至终止性。
不推荐把这些行为暗中引入。

实现需在任何 guard 前，基于入口验证取得当前 variant 所需的固定槽
handle，后续同 variant arm 复用它。编译期验证跨包 helper 的实际 owner、
类型身份和读写权限，helper 在首次投影运行时检查 tag；普通 Go helper
本身不执行 goxide 只读检查。**不能在后续 arm 再按当前 tag 投影**，否则上述
旧引用合同会被 helper 的重新校验意外拒绝。读取引用所指值时依然遵循
对应类型的已有验证规则。这是明确的旧槽行为，不是借用安全证明。

此 guard 合同只对应固定槽候选，与布局一并提请决定，尚未写入生产
lowering。若选择代际句柄，仍可保留入口判别/值快照，但 guard 替换后
访问旧引用应明确失败，不能沿用本节的 A 槽返回 0；新 guard 合同必须
相应验收。阻止失效的 pin 方案则在 guard 的替换操作处失败。

## Go 互操作、标准库及元数据迁移

建议在实现原位借用时统一迁移普通 enum 与 std Option/Result，避免
同一模式对内建 family 暗中改成副本或拥有另一套存储引擎。若要缩小
首片，必须公开其支持矩阵并另获范围决定，不能在实现中悄悄略过。

| 边界 | 同批工作与明确行为 |
| --- | --- |
| 普通 enum | 保留名义身份；零 tag 非法；构造、验证、match、派生一同切换 |
| Option/Result | Option 零 tag 为 None；Some(nil) 仍不同于 None；Result 零值非法；保持共享 std 名义类型与全部类型参数 |
| Go consumer | 原 interface case 断言、公开 case 字面量、Option 的 nil 表示会破坏性改变；发布新 canonical API 并重生成，不提供旧布局转发层 |
| enum metadata | 当前 v8 强校验 interface/case/Value/constructor；布局变化须升级，验证真实 Go 类型和投影 helper，不凭名字接受伪造 owner |
| sum ABI | 当前 v2，须与 std 一同升级；std 不能反向依赖 compiler 或 serde |
| ref metadata | 当前 v3，权限与符号绑定独立；现具体元素校验拒绝泛型命名类型、interface 及包含 interface 的结构，新 inline enum 的 `error` 槽也不会自动获准。按真实新结构升级或扩验证，不无理由改 wire version |
| serde | 格式无关 core 与 JSON 协议原则上无需改变；主要改生成的 enum adapter，维持 wire 格式并实际回归，不能因此称 serde 已验收 |
| 网站 | 重新绑定 export data、producer metadata、WASM 与 compiler pin，不能沿用旧布局产物 |

跨包私有槽需要公开且受检的投影入口及元数据，包括实际 owner、variant、
payload 类型身份、签名、权限和依赖指纹。普通 Go 调用者能形成编译器
未检查的可写别名；Go build 不执行 goxide 只读检查。原生 Go 桥应继续
支持其已支持的形状，不把尚未实现的复合 payload 桥说成借用安全。

比较尤其需要分层记录。当前 `.gox` 已要求 enum 间比较走明确的
PartialEq / `.equals(...)`，不能照抄旧研究中的“unit == unit 已支持”。
现存 enum 与 nil 的比较仍需迁移决定；建议随布局切换直接拒绝 enum/nil
比较并给出 Option 模式匹配等诊断，不保留 nil 布局兼容。原生 Go 中
可比较的 inline struct 的 `==` / map key 会包含不活跃槽；含任一
slice/map 槽时整个 struct 不可比较。原生表示比较不能被宣称为语言级
活跃 variant 相等，公开 Go API 必须提供明确的语义操作。

已有值型 match 及派生应先复制活跃值再调用外部协议，保留 serde/
格式化回调重入时的既有观察顺序。改成直接循环读取活跃槽也可能
改变非借用行为，不能作为无关优化混入。

## 验证、交付顺序与决定请求

[纯 Go 研究套件](../../../tests/research/match_payload_borrows/README.md)
重放复制、固定槽、替换、间接化、boxing/COW 反例及 guard 分歧。
它不调用 goxide 编译器，不证明源语法、权限、跨包或 WASM 已支持。
[实施计划](match-payload-borrows-implementation-plan.md) 将相应行为列为
尚未执行的前端验收矩阵。已有 #15 的全量证据仅覆盖冻结的 #3/#4。

建议优先决定：旧引用应保持固定槽，还是在替换后明确失效。长期优先
考虑代际投影的明确失败合同，同时接受其受检 owner/Go 桥边界需要
设计；若必须保持透明原生 `*T` 和任意 Go 浅值赋值，则固定槽是更直接
的方案，但需明确接受上表的意外行为、空间及保留成本。两者都不能
以兼容回退并存来逃避语言决定，也不采用普通赋值共享的 pointer case。
在用户选择前，只交付设计与证据，不改生产表示。

实施顺序为：

1. 决定上述布局、旧引用、guard 和零值/Go API 合同；同步 #14/#13。
2. std 新 sum ABI 候选及独立测试，保持无 compiler 依赖。
3. compiler 同步 lowering、权限/元数据与全部派生，固定确切 std SHA。
4. producer/consumer 重生成，CLI 与网站绑定同版产物。
5. 独立模块和原生 Go consumer、serde、权限正反例及 CLI/WASM 差分；
   独立对抗审查修复后复审，最终全量交 GitHub Actions。

本片只有设计与机制证据，不能关闭 #14，也不包含合并或部署授权。

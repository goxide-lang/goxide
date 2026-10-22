# RFC 0031：普通库协议的条件 impl 与按需实例化

## 状态与范围

**参数化 trait、通用互斥、定义点证据分区、按需实例化与 Go 普通入口已实现；declare impl 已移除。** 宏输出普通 Goxide 声明，经同一条解析、名字、类型及权限检查管线处理。当前状态、验收入口及标准库发布边界见[状态表](../mvp-gap-audit.md)。业务派生宏的正式迁移仍逐项验收，不由通用协议能力自动完成。

[RFC 0030](0030-procedural-macros.md) 提供语法变换与受限执行，宏不查询类型、不登记 impl。Default、Clone、Debug、PartialEq、Eq、Serialize、Deserialize 的迁移使用普通库协议；每个业务协议仍须保持原有行为并分别验收。

| 当前基础能力 | 规则 |
| --- | --- |
| 泛型与条件 impl | 类型参数、同一类型的多个 trait 约束、具体或复合类型的 where 条件由普通 checker 处理；条件不收紧目标类型本身 |
| impl 归属与唯一性 | 按 Self 与位置输入组成的完整有序头检查 owner；只有共享约束证明交集不可能的声明才可并存，不按特化或源码顺序选择 |
| 普通协议前提 | 普通 trait 可声明无环的 `where Self: Other[Input]` 前提；输入与源权限参与身份，关联类型按完整 application 区分 |
| 具体 impl | 没有待定参数且前提可证明的 impl 直接生成，Go 使用者可调用其正常公开入口 |
| 按需实例化 | 含待定源级协议条件的 impl/函数保留为模板；从本次转译范围内的实际请求递归生成已确定的实例 |
| 普通 Go 泛型 | Go 能直接表达、不依赖待定 Goxide 实现选择的类型和函数保留泛型；`Box[T any]` 不因条件 Default/Clone 收紧 T |
| Go 使用边界 | 条件能力通过 Goxide 明确导出的普通函数使用；Go 调用者不传 Ops，也不引用实例哈希名 |
| sugar | 按[分发模块配置](../design/toml-manifest.md#go-成员-sugar-配置)，所有 impl 来源一致；只对 Go 允许追加方法的目标生成短入口 |
| 数组长度参数 | `const N int` 仅匹配固定数组长度、接受显式非负整数常量和原样转传；不开放长度表达式求解或 const 泛型具名类型 |
| enum 外层检查 | `enum_valid(value)` 单次求值并返回外层变体是否合法；payload 与普通 match 的检查规则不变 |

最小数组长度参数与显式 enum 可失败检查已实现。Default 的函数调用图递归限制和 Eq 派生的现有资格规则保持不变；下文关于放宽这两项的候选暂不实施。`no_mangle` 不属于本篇或本次实现。

[已确认决策](../design/decisions.md) D05 已改为移除 `declare impl`；D19/D20 的权限边界、普通有体 impl 的运行时 wrapper 和 enum 表示继续适用。此稿不增加 move、独占分析、动态字典、反射兜底或每种 derive 的编译器 hook。当前部分业务宏仍拒绝泛型声明；通用迁移完成时须支持其业务合同适用的泛型类型，不能把该拒绝作为完成边界。库的泛型 impl 同时须为业务字段提供协议。

Error 派生、`go_result!` / `go_result_keep!`、`cfg` 属于不同边界：Error 的 struct 方法已有普通 inherent impl 迁移证明，enum 固有方法的新映射仍未实现；From/Into 基础按 RFC 0032 实现。Error 的 `#[from]`、消息和上下文等人体工程库后续开发；现有内建 Error helper 仍可显式调用，但不登记为 `?` 转换证据。go_result 涉及调用结果形状和错误转换，cfg 决定进入展开的源码。print/println 的库化另验格式检查与调用行为；本篇提供其可使用的普通协议能力。

## 源级规则

### 1. 声明、归属与约束（当前基础合同）

已确认语法沿用 `impl[T ...]`、`where` 形状和 `+` 组合；数组常量参数按第 4 节限制使用：

```goxide
impl[T Clone] Clone for []T { /* 必需成员 */ }
impl[T any] Clone for Box[T] where T: Clone { /* 必需成员 */ }
impl[T any] Decode for Box[T] where T: Deserialize + Default { /* 必需成员 */ }
impl[T Clone, const N int] Clone for [N]T { /* 必需成员 */ }
```

第一个和第四个 impl 位于 Clone 的定义包；第二个也可位于 Box 的定义包。`Box[T]` 自身可保持 `T any`，协议条件不回写成类型声明的约束。where 左侧允许实际字段类型，如 `[]Label`，从而让过程宏只依据语法输出条件，再交给普通类型检查证明。泛型 impl 的每个参数必须能从 Self 与位置输入的完整头绑定；满足 RFC 0032 有序 owner 与本篇互斥规则的裸参数 blanket impl 可以声明。

归属以 canonical 包与类型身份判断，透明 alias 不创建新 owner，新 defined 类型是独立目标。`[]Local`、`map[string]Local` 和 `[3]Local` 的元素属于本包，不使这些未命名容器成为本包所有。trait 位置类型参数是输入；方法不引入独立泛型参数，关联类型仍是 impl 的输出。

这不引入 underlying-type 自动采用：`type Score = int` 可使用 int 的实现，`type Score int` 不因此取得任何协议。当前 Default/Clone/Debug 的字段检查也先要求 defined 类型有显式实现，serde 只展开 alias、不展开 defined 类型的 underlying；内部生成函数对已登记根类型的解包不是普通字段兜底。迁移须保留这一差别，不按派生名称补证据；普通 blanket 必须通过统一的归属、互斥与前提检查。

同一目标可同时要求多个 trait；关联类型绑定仍按所属 trait 区分。成员查询保持现有权限候选和固有成员优先规则，同名 trait 成员须限定，不按约束书写次序选择。每个约束都须有显式 impl，不能凭 Go 方法集补足。

普通 trait 前提可表达 Eq 对 PartialEq 的关系；将现有 Eq 派生迁为下列库协议时仍须核对其独立行为合同：

```goxide
pub trait PartialEq { func equals(self Self, other Self) bool }
pub trait Eq where Self: PartialEq {}
```

证明 `T: Eq` 时先证明其前提；在 `T: Eq` 的函数内可使用 PartialEq。只开放无环的普通 trait 前提；带输入的前提按完整类型与权限代入，不增加默认方法、运行时自动视图转换或任意继承机制。PartialEq 继续使用两个值参数，不借迁移暗中改成只读借用。

**暂缓的 Eq 迁移候选。** 若保留“派生 Eq 只接受结构派生 PartialEq”的区别，可由普通库 marker `StructuralPartialEq` 表达：PartialEq 宏同时生成该 marker 的 impl，Eq 宏要求 `Self: StructuralPartialEq`。手写 PartialEq 本身不产生 marker。marker 与手写 Eq 一样是作者承诺，用户也可显式实现；它不是编译器签发的不可伪造证明。接受此规则包含接受这种显式承诺入口；不能为隐藏它重新增加 derive 名称判断。

Default 成员统一写作 `r#default`，使用普通 raw identifier；不再以 trait 名称触发关键字解析例外。静态函数返回真实 Self；Clone、Debug、Serialize 的 `self &Self` 保持既有只读权限；Deserialize 返回 `(Self, error)`，不抹成 `any`。

trait 与派生宏沿 RFC 0030 的单一名字空间：运行库可导出 `Clone` trait，并将宏包重导出为 `derive`，调用写作 `#[derive(protocols::derive::Clone)]`；也可显式导入宏并改名。现有裸内建派生入口在迁移时删除，不用编译器固定映射模拟普通库。宏输出的运行库路径通过既定定义点绑定与 RuntimeRoot 解析，消费者按 RFC 0030 声明运行库依赖；宏执行依赖不会自动成为目标依赖。这里的模块及导出名是示例，最终库布局须在迁移前确定。

### 2. 通用互斥与实现选择（设计提案）

本节的通用求解与选择合同已实现，替代“只比较目标头”和此前只覆盖具体类型的有限否定方案。定义中保留未知类型/长度参数，按共享约束及包级开放环境证明；真实过程宏输出与手写声明共用此路径。业务宏的正式发布仍独立推进，不能把有限具体实例或宏主动拒绝泛型当作通用能力验收。`declare impl` 已完整移除。

已确认：完整移除 `declare impl`，保留普通 Go 双向互调用；trait 输入可由实参和已知返回上下文推导；采用包级开放世界。完整头、owner 扩展和转换调用见 [RFC 0032](0032-from-into-conversions.md#源级规则)；本节统一规定所有普通协议的求解与选择，不按宏或 trait 名称特判。不引入特化、用户负 impl 或运行时协议搜索。

#### 2.1 身份、量词与结果

输入为 cfg 选择和宏展开后的声明 D、canonical 类型/包环境 Γ、定义点约束 ParamEnv 与开放环境 Horizon。收齐当前包及其可达源码依赖的声明后再认证；声明顺序、use 别名、重导出、依赖遍历次序不参与选择。

```text
ImplDecl = (DeclID, owner, parameters, Head, Predicates, body)
Head = (canonical TraitID, norm(Self), norm(trait inputs...))
Overlap(I,J) = ∃ X,Y. Head(I,X)=Head(J,Y) ∧ Predicates(I,X) ∧ Predicates(J,Y)
```

透明 alias 归一，defined 类型保留名义身份；源级 &T、&mut T、原生 *T 保留不同权限合同。关联类型是输出，不是 Head 的额外输入。允许同 Head、互斥 where 的两个声明后，证据和生成符号必须使用各自稳定 DeclID；不能再用 owner/target/trait 三元组去重。DeclID 可用规范化声明头及条件区分，同一 canonical 声明经多条导入路径加载仍只有一份。重复但无关的声明不能因此去重；完全重复的有体 impl 仍冲突。

互斥是存在量查询：分别 freshen 两边参数，将头合一的代换、两边全部前提放进同一个约束状态 Σ。不能用不同 T 分别满足 A、B，就声称同一个 T 同时满足二者。函数/impl 体检查是定义点的全称保证：泛型参数 rigid，在 ParamEnv 下证明每次使用所需能力，不能靠某次具体调用碰巧成立放过模板。

```text
Intersect(I,J,Horizon) → Impossible(certificate)
                       | Possible(witness)
                       | Unknown(reason, residual constraints)
```

Impossible 要证明所有允许代入与预留扩展都无交点；Possible 给出一致的具体或符号见证；其余为 Unknown。两个有体实现只有 Impossible 才能并存。Possible 诊断展示交点，Unknown 诊断说“无法证明互斥”及剩余义务，不能冒充已找到重叠实例。相同文本的重复有体 impl 也不合并。

#### 2.2 共享约束求解

```text
SolveExists(Σ, goals):
    饱和类型、const、Go type-set、原生签名及可用关联输出等式
    有矛盾 → Impossible
    无 goals 且 Σ 已证可满足 → Possible；无法判定可满足性 → Unknown
    取一个 goal，枚举当前 impl 分支及 Horizon 的未来分支
    每个分支把头等式、前提、输出和其余 goals 放回同一 Σ
    任一分支 Possible → Possible
    枚举完备且全部分支 Impossible → Impossible
    否则 → Unknown
```

Goxide trait 义务的 OR 枚举只包含已知有体 impl 和未来有体 MayBody。原生 Go interface/type-set 义务使用原生约束规则，仍进入同一个 Σ。AND 中任一条件失败即可排除该分支；每个答案保留代换和剩余约束，不能只缓存布尔值。未来分支也继续参与约束归约，例如普通 Go 约束中的方法签名产生 T=int、另一必要条件产生 T=string 时，仍可符号化反证。原生方法集满足 Go interface，不创建 Goxide impl 或对应协议证据。

完成范围必须包含以下普通证明规则：

- 名义类型和构造类型合一、重复参数、occurs check；T=Box[T] 等无限类型无解。不能按 underlying 混同 defined 类型。
- Go 约束的有限 union/intersection，精确类型项和 ~underlying 项，以及已允许的 comparable、方法签名合同。~int 与 ~string 可直接反证，不能只枚举当前已知具名类型来代表 ~int。comparable 按当前 Go 约束满足规则处理，不把接口类型与运行时值可比较性混为一谈。
- 同名必需方法的精确参数、返回、variadic 和接收者约束；确定类型交给当前 Go 类型环境核验，符号类型保留准确条件。无法精确处理的方法集合返回 Unknown，不能猜空集。
- 现有 const N int 的数组长度等式、非负合法长度域及常量矛盾；N=2 且 N=3 无解，未知 N 不妨碍证明。现有源语言禁止的 const 算术仍按第 4 节诊断，求解预算耗尽另报 Unknown。
- 同一协议 application 的关联输出等式，但须先取得独立的 coherence 依据，规则如下。

对于已认证的 Q，T:Q[Item=int] 与 T:Q[Item=string] 可利用输出唯一性证明互斥。两个待比较 impl 自己声明不同输出，则不能据此断言互斥：输出差异可能恰是冲突。记录每张互斥证书依赖的 trait coherence；在同一个待证依赖 SCC 内，先禁止彼此借用输出唯一性，只能展开具体分支或使用头、Go 约束等独立事实。某节点独立认证后才解锁它的输出等式；剩余循环担保返回 Unknown(CircularCoherenceDependency)。

#### 2.3 包级开放世界与负证明

Horizon 不是可切换的编译选项。它记录定义包 K、选定源码/原生依赖版本、cfg、完整声明集、owner 规则及以下扩展类别；所有否定证书绑定这些依据。对 impl 对 I/J，只能分别使用 I 或 J 的定义包作为 K，任一合法定义环境取得的互斥证书即可成立；不能任选无关包关闭世界。全图认证保留证书的 K 和依赖快照，消费者验证其依据仍有效，不能改用消费者包为 K 重判既有声明对。实际新增声明仍与既有实现逐对检查，证书依赖变化则重新认证。

1. **定义包当前源码固定。** 对 K 中 impl 进行认证时，K 的声明已完整收集。修改 K 的 impl、类型或方法后重检。不能为方便证明把可达外包当作 K 所有。
2. **未来下游包开放。** 枚举新包可定义的新具名类型，按 RFC 0032 的有序 owner 规则判断是否能满足义务；裸变量可取新类型，固定外部名义构造及其指针链不会因内部参数来自新包而转移 owner。已有但未加载的包按同样的 owner 与依赖方向判断；可新增保持无环的依赖边，不能只看当前可达性。没有加载到声明本身不能作为不存在证明。
3. **兼容外包有体扩展按句法形状预留。** 在保持 import DAG 与既有类型/trait 合同的前提下，预留两类新增声明：(a) OwnerOK 且完整头每次参数出现都被非 * 构造覆盖；(b) 某个头位置的具名根是该版本新定义的 owner 类型，首个这种新本地根之前没有未覆盖参数，之后可有。新根可属于既有上游或未来下游，alias/重导出不创建新根。对既有根新增的其他 blanket、删改 impl、放宽 where 或改变输出属于需重新验证的变更，不作为兼容新增预留。covered 按每次出现判断：Q[P] for Box[P] 仍有未覆盖 P；Q[P] for NewForeign 可用新的 Self 根预留；Q[P,NewForeign] for OldForeign 不能用后面的新根掩盖前面的裸 P。这是 coherence 的扩展分类，不宣称任意库 API 变化都兼容。
4. **原生 Go 约束的未来变化仍须预留。** 普通有体条件 impl 可要求 T 满足原生 Go interface。上游类型新增方法或外包嵌入类型改变提升路径，可能使此前不满足的 Go 前提成立；不能因删除 declare 而按“当前缺方法”否定它。直接稳定同名方法的签名矛盾不能由新增另一个同名方法修复。K 的完整方法集只在没有外部提升影响时可视作固定。

MayBody 对每个已有非 K 包及 FreshDownstream 包类构造上述两类存在分支，并保留头合一、OwnerOK、普通类型约束和 trait 必要前提。已有根分支中，外包无法通过无环依赖命名的名义子树只能由参数代替，而参数的每次出现须满足对应覆盖条件。新根分支要求头位置本身（只穿过 * 链）可与 FreshNamed 合一；不能从 []T 或 KnownBox[T] 内部取 owner，固定名义根也不会因 T 未定变成新根。所有可能分支均被反证才关闭 MayBody；形状/投影分析不完备则 Unknown。

负证明不依赖外包当前的 private 可见性：其 owner 可能新增公开 alias，泛型参数也可覆盖私有实参。实际声明仍执行普通可见性检查；只在未来扩展分析中保守预留可经无环依赖取得的名称。未来有体分支与原生约束 residual 继续和其他条件共享 Σ，避免分别找出的未来实例被错误拼接。

关键验收是在 Wrap 定义包证明泛型 `impl[T] Into[[]T] for Wrap[T]` 与 std blanket 互斥：交点要求 `[]T:From[Wrap[T]]`。当前完整声明无可满足分支；未来下游无法拥有 slice 或 Wrap 外层，上游要接收无法命名的 Wrap[T] 必须使用未覆盖来源参数的 blanket。因此在整个 T 域排除该义务，而非先实例化 int/string。反之，裸 `T:Q` 可被新下游具名类型的普通有体 impl 满足，不能因当前无实现就否定。这套 owner 证明同样适用于实例协议，不再要求 trait 含静态成员。

该规则保证库检查考虑允许的未来扩展；新增本包实现、依赖版本/cfg 变化及不在兼容预留类别内的 blanket 扩展仍触发相关包与下游重检。缓存绑定完整环境和依赖证书，不能复用旧锁文件或另一 cfg 下的负结论。新增实际交点应报冲突，不能改成更具体实现优先。运行时插件、反射发现和纯 Go marker 不参与源码实现集合。

#### 2.4 移除原生采用，保留普通互调用

[移除合同](0003-declare-impl.md) 完整删除 `declare impl`，不区分 trait/type 所有者或第三方；不增加替代采用属性、签名自动登记或按 Go 方法集补 trait 证据。普通有体 impl 继续按 owner 规则检查；trait 定义包仍可为外部类型提供完整业务体。完整头不满足普通有体 impl 的 owner 规则时，定义本地包装类型再写普通 impl，或使用普通函数；拥有 trait 输入的包仍按完整头规则处理。透明 alias 不取得 owner，下例无输入 Reader 使用本地包装类型：

```goxide
use bytes;
use protocols;

pub struct BufferReader { raw *bytes::Buffer }
impl protocols::Reader for BufferReader {
    func read(self &mut Self, p []byte) (int, error) {
        return self.raw.Read(p)
    }
}
```

包装类型及方法体是作者明确写出的业务，不按原生同名方法自动委托。它可保存同一个 Go 指针、游标或缓存，避免每次调用重建底层对象；普通权限与原生调用合同照常检查。仅需 Go interface 时直接使用 io::Reader 等原生接口，继续按 Go 方法集结构匹配。`declare func`、`declare type`、Go 函数/方法调用、Go 调用生成的公开 API 均保留。

删除采用后，solver 不再需要 MayDeclare、无体声明合并、原生绑定等价证明或多个采用工厂的选择规则。普通有体 impl 的 runtime wrapper、marker、固定工厂、As/For 和 Go 成员 sugar 保留；它们服务已选业务实现，不给外部原生类型添加方法，也不从 Go marker 反推 impl。每个完整 application 的业务实现由互斥认证保证唯一，不按包路径或具体程度消歧。

编译器已删除采用语法、checker/lower 路径和采用专用测试，保留承载其他外部声明的 DeclareDecl。源码及宏输出使用同一声明语法；普通有体适配和双向互调用由跨模块生成测试覆盖。

#### 2.5 正证据、递归与降低

所有声明先通过全图 coherence 认证，再检查调用所需的正证据。定义点的显式协议前提可直接携带 evidence parameter；它在实例化时填充，不按候选顺序重搜。若某候选在 ParamEnv 下已有正证据，可用已认证的成对互斥证书排除其他候选；其他候选单独求解为 Unknown 不妨碍这一排除。若只能得到有限、完备且互斥的条件分区，可保存编译期证据分支，后续实例化选定叶。每个可达分支均须在对应约束下完成成员签名、关联输出、方法体和权限检查；不能让某次实例化掩盖其他分支中的错误。无法证明分区覆盖则要求补充普通约束或诊断，不能把不完整分区延迟成运行时试错。

未被调用的泛型定义也检查全部证据叶。源级检查保留数组长度 N/M 的精确身份；独立的 Go 验证 AST 检查普通运算、赋值、返回和控制流，对 const 参数使用未知长度，不选择代表性长度，也不进入生成产物。实际生成仍只按已请求的具体长度专门化。

类型推导先依据 trait 声明签名及已有类型信息求出完整输入，规则见 RFC 0032；不反向枚举 impl 或 where 猜测未知输入。选定证据记录 DeclID、全部类型/长度代换、关联输出和前提证据。方法候选、投影、缓存、实例队列、绑定器和符号身份均须包含完整 application；实现声明身份与义务查询身份分别维护。

区分三张递归图：

- 逻辑义务用 canonical query tabling，普通 trait 采用归纳证据；遇到同一义务不默认真，也不立即缓存为假。有限且完整展开、无开放外部分支的 SCC 可用最小不动点判定没有基础证明；类型增长、外部残余或未完成展开返回 Unknown。
- coherence 证书依赖图按 2.2 节处理关联输出循环；不能以自身待证唯一性反证自身冲突。
- 普通业务调用与生成需求图沿第 3 节处理。已登记无条件具体 impl 的方法体可相互递归；实例队列去重，不能把业务递归误判成资格递归。

预算按确定性的节点、分支、类型项和推理步数计费。耗尽报告查询链和剩余条件；不按墙钟时间改变判定，不把溢出或临时 SCC 结论写成永久负缓存。固定输入、配置和预算下的诊断及生成结果应稳定。

Go 降低继续使用已选 ops、定义包模板和精确签名回调，普通 Go 能表达的泛型保留；不引入动态字典、反射或全局 impl 注册表。不同的同 Head 条件声明有不同生成符号，但不向原始泛型类型无条件追加能力。私有字段业务留在定义包，std 不反向 import 消费者；真正被调用的方法承载业务体，直接调用和方法值保持求值、槽位、defer/recover。Go 消费者沿 D38 调用作者公开的普通入口。

Default 函数调用图规则调整及 Eq marker 仍是既有暂缓项。本节解决普通资格证明，不借此放宽业务宏合同。serde 递归具体类型可由宏生成无条件 Serialize/Deserialize impl，先登记签名，再在业务体中选择已登记 Node 与容器操作；带有 `where *Node:Serialize` 的资格环则需要归纳基础，不能因运行时递归合法而默认成立。Clone 宏显式生成的循环字段前提继续失败；两种策略都不保证运行时对象图终止。

#### 2.6 通用过程宏迁移验收

以下是完整验收合同，测试入口及发布状态见状态表。每一行包含对应源级诊断或正例；不能只测内部 helper，也不能用具体类型宏探针代替未知类型/长度的定义期证明。

| 编号 | 最低覆盖 | 预期 |
| --- | --- | --- |
| GC-01 | 同 Head 的 Box[T] 两条 impl 分别要求 ~int、~string；交叉 union 与真实交集对照 | 符号互斥通过；交集有见证则拒绝；不同 DeclID 不混淆 |
| GC-02 | 同一 T 的独立已认证 Q.Item=int/string；仅输出不同的两个 impl；相互担保 coherence 的 SCC | 前者通过，后两者冲突或明确 Unknown，不循环自证 |
| GC-03 | 数组前提联合产生 N=2/N=3、N 相等及未知合法长度 | 符号长度证明；不枚举全部 N，不开放 const 算术 |
| GC-04 | Wrap[T] → []T 的独立 Into；添加对应 From；同容器逐元素 From 在 S=T | 非 ground 独立 Into 通过；后两项实际冲突，不能特化 |
| GC-05 | 未知 T 可取未来下游本地类型；有序 owner、* 链、covered 参数、alias、同 module 不同 package | 预留合法未来分支；消费者保留定义包证书；不把未加载/未导入等同于不存在 |
| GC-06 | impl[T NativeGet] Q for Box[T] 与 Box[foreign::Row] 的具体 impl；Row 后续新增 Get、签名矛盾及外部嵌入提升 | 原生 Go 约束按未来方法环境证明；当前缺 Get 不能误判负 |
| GC-07 | 原生 Go 函数/方法/interface 调用；外部对象由本地包装类型和普通有体 impl 适配；trait owner 为外部类型实现 | 双向调用、原指针身份及 owner 正反例；不自动登记协议或注入外部方法 |
| GC-08 | 源码及三类宏输出 declare impl；declare func/type 对照；重复有体 impl | 原生采用入口拒绝；其他外部声明保留；重复业务实现不合并 |
| GC-09 | 正证据加成对互斥证书；泛型 ParamEnv 传递；完备/不完备条件分区 | 不取首个、不按包优先；只保存可证明的编译期选择 |
| GC-10 | 封闭无基础逻辑环、可证明基础、类型增长、预算边界与循环关联输出 | 有界、稳定诊断；业务递归不误拒；溢出不当否定 |
| GC-11 | From/Into 的单个/多个已知 application、实参/返回上下文推导、歧义、私有类型及限定方法值 | 按声明签名推导；不通过 impl 试重载或反推关联输出 |
| GC-12 | 手写、声明宏、WASI function-like/attribute/derive 生成相同泛型条件实现 | 同一语义与诊断；至少覆盖泛型 struct/enum、嵌套容器、关联输出和长度参数 |
| GC-13 | 至少三个独立 module：协议/通用库、原生或适配库、业务与 Go 消费者；公开发布 pin | 真实 .gox 生成 Go 后 build/run/test；私有类型/字段、runtime view、无反向依赖、Go 普通入口 |
| GC-14 | 文件/import/重导出/依赖枚举顺序重排，重复生成，cfg/版本变化后重检 | 字节一致；声明不重复，负缓存失效正确，新增冲突不改变旧选择 |

GC-12/13 的真实 WASI 宏只读取语法并输出普通声明；至少一例在泛型声明仍含未知参数时证明互斥，随后用多个实例实际运行，不能先穷举 int/string 再宣称泛型完成。derive、attribute、function-like 三类均须端到端接入；错误中的义务链与宏来源位置可追踪。已有 `syn_generic_test.go`、`attribute_protocol_migration_test.go` 和 `const_macro_test.go` 可作为基础，新增验收落在相关测试中，不另造证据文档。

Go 行为同时覆盖 nil/typed-nil、值副本与真实借用槽、权限反例、单次求值、直接及保存方法值的 recover、源码行号和普通 Go 调用。通用基础通过后，各业务宏仍按本篇既有 Default/Clone/Debug/PartialEq/Eq/serde/Error 合同逐项迁移；新增基础验收不抵消深层 enum 比较、缺失字段协议和正式库发布等尚存缺口。只有完整泛型能力、业务等价与相关 hook 移除都通过，才能将对应迁移标为完成。

### 3. 转译范围、需求闭包与 Go 入口

项目转译以本次选中的模块为入口，原生 Go 的原位 `gen` 以当前包为入口。已选源码依赖提供普通声明及模板；请求沿实际调用、函数值和 impl 前提追踪，不枚举可见类型与全部协议的笛卡尔积。关闭 sugar 不删除协议需求。

具体 impl 先完成前提检查，再直接生成全部成员。泛型/条件模板先在声明的约束下检查方法体；只有进入已确定请求的模板才产生实例。普通函数体和包级初始化、具体 impl 的成员体，以及作者明确提供的具体公开函数均可引出请求。仅给仍含协议参数的模板加 `pub` 不产生未来所有 Go 类型的实例。

工作队列按 canonical 声明身份、类型实参、关联类型绑定及已选条件实现去重。每个实例把代换应用到其调用、函数值、关联投影及内层 impl 条件，再加入新的请求。相同实例递归只生成一份声明；前提证明循环仍由 checker 拒绝。请求顺序和依赖遍历顺序不改变产物。无法形成有限实例闭包的类型增长递归在源位置诊断，不生成部分成功产物。

普通 Go 泛型继续保留：例如 `Box[T any]`、`identity[T any]` 和能直接按 Go 方法集检查的函数，不因同包存在条件 impl 而全部单态化。Goxide 协议条件只约束使用该能力的 impl/函数；`Box[NoDefault]` 可以存在，只有请求其 Default 时才检查条件。

原生 Go 通过明确的普通源函数使用条件能力。例如业务库提供 `encodeRoot` / `decodeRoot`，分别确定 Root 的 Serialize / Deserialize 及其字段依赖。对应 Go 函数保持源码公开名称和业务签名；Go 调用者不构造 Ops、不传证据类型参数、不引用实例化产生的哈希符号。Encode 与 Decode 分别引出自己的条件闭包。

实例化保持定义点绑定、Go 包身份与接收权限。不能为了展开库实现，把访问私有字段或私有 helper 的业务体搬到无权访问的包，也不能让定义包反向依赖消费者私有类型。必要时在定义包保留普通 Go 泛型 helper，传入已确定签名的操作函数；例如 `CloneVecFunc[T](value Vec[T], clone func(T) T)`。跨包 helper 使用稳定、可读的名称；具体公开入口负责提供操作，Go 业务调用者不处理编译器证据。多方法和关联类型按实际签名传递，不把任意协议抹成 `any` 或反射。

普通泛型函数接受 Go 的单个多返回调用实参，例如 `Pair(native())`：逐项推导类型、数组长度与读写权限，再证明 trait 条件；可变参数同样按结果位置处理。原调用只求值一次，保留所在函数帧。跨包私有类型需要附带协议操作时，生成临时结构保存返回值，再进入现有绑定路径。`internal/check/sole_result_test.go` 与 `internal/lower/sole_result_test.go` 覆盖权限正反例、私有类型与关联类型、求值顺序、defer/go 时机和 recover，并执行 Go 产物及重复生成检查。

保存 trait 方法值时，接收者在绑定时只求值一次；value 接收者每次调用取得独立副本，引用接收者继续指向原槽位。绑定器返回含原业务体的 Go 方法值；普通原生方法值直接绑定 Go 方法，保留 `defer`/`recover` 的调用帧。已有公开 Go 泛型入口需要保存方法时，其可用具体 carrier 同步提供对应绑定器，即使库内没有 Goxide 调用。`trait_method_value_frame_test.go`、`trait_go_binding_boundary_test.go` 和 `trait_concrete_frames_test.go` 覆盖这些行为及具体 impl 的业务体注释与 panic 源行。

实例只写本次生成目录或受管原位产物，不修改依赖 checkout、模块缓存。只有生成 Go 的依赖提供其现有 API；需要新的源级条件实例时由 Goxide 取得模板并重新生成。

已有具体 impl 的 wrapper、marker、固定工厂、As/For、carrier 和关联类型 witness 保留完整协议入口。静态条件实例化不向 `Box[T any]` 强加条件性 anchor，也不把源目标 T 换成运行时 wrapper。运行时视图仍按已选 impl 保存值或真实借用 owner。[Go 成员 sugar](../design/traits.md#go-成员-sugar) 在全部 impl 成员确定后按模块配置处理；实例化 `Box[User]` 不会使 Go 允许给这个特定实例单独追加方法。

### 4. 最小数组长度参数

本节已实现。`const N int` 首批只服务固定长度数组和原样转传的长度参数：N 从实际 `[n]T` 的长度匹配，也可由显式非负整数常量提供。首批不接受负长度、非常量、`[N+1]` 模式求解、改变 N 的递归实例化或任意类型的 const 参数；不开放 const 泛型具名类型，也不将 `[N]T` 扩展为 Go 类型集合约束。显式值可使用整数常量表达式与导入的 Go 整数常量；当前不将 `len` 等内建调用作为常量参数求值。

沿用第 3 节的需求闭包，为需要的具体长度生成普通 Go 声明。元素类型能继续保留 Go 泛型时不额外展开；不能以每种数组长度一个编译器协议 hook 代替普通模板。原生 Go 通过已有具体入口使用结果，新的长度由 Goxide 源请求引出。

### 5. serde 迁移所需的普通表达性

仅有 Serialize/Deserialize 方法签名还不足以保持现有行为。本节包含已确认的通用 enum 检查，以及迁移前必须落实的普通库协议；不把字段分派搬成编译器隐式 hook。

**显式 enum 检查已实现。** 内建调用 `enum_valid(value)` 沿普通内建的解析和遮蔽规则。输入的静态类型须为具体 enum 或已知 enum 的泛型实例，透明 alias 同样适用；不接受任意 `T any`、接口或自动解引用的指针。参数只求值一次，结果为 bool：外层值属于本 enum 声明的一个变体且变体存储有效时为 true；nil 接口、typed-nil 变体、未知外层变体以及空 enum 的任何值为 false。该检查不遍历 payload，不执行用户方法，不使用反射或 recover；原生 Go 可传入的边界值也按同一规则处理。

Option/Result 按各自现有表示检查外层变体；None 是有效值，Some(nil) 的外层也有效，载荷为 nil 不在此处递归检查。普通 `match` 保留现有分派前的 nil panic，`_` 不接收非法值；显式检查不构成永久有效性证明，后续 match 仍执行自己的检查。serde 宏可先检查普通指针参数是否为 nil，再对解引用后的 enum 调用此操作，返回现有 Type error 后才进入 match。表示与检查规则统一见 [enum 合同](../design/enum-representation.md#21-显式外层有效性检查)；普通可命名输入在调用处生成检查。跨包私有 enum、私有变体或私有类型实参无法在调用处命名时，允许按需在 enum 定义包生成编译器内部检查函数，并由 Go 推导类型实参；该函数不增加 Goxide 公开 API，不暴露私有变体名，也不改变单次求值和无用户方法调用的规则。

**Deserialize 的缺失字段协议。** 对字段 `type Maybe = Option[int]`，宏不能仅凭拼写决定缺失是否合法。候选普通库 trait 可把该行为作为一个明确静态成员，例如：

```goxide
pub trait Deserialize {
    func deserialize(d Deserializer) (Self, error)
    func missing() (Self, bool)
}
pub func read[T Deserialize](d Deserializer) (T, error) {
    return T::deserialize(d)
}
```

`missing` 返回缺失时的值和是否接受缺失；它不是默认方法，每个库实现显式提供。Option 实现返回 None 和 true，普通 scalar、pointer、容器及派生业务类型返回零值和 false；pointer 可接受显式 null，不因此接受字段缺失。该方法不承担 `#[serde(default)]` 的业务默认工作。宏通过普通协议选择处理 alias，先完成读取和所有必填校验，之后才按字段顺序应用 default/skip；已有字段的格式错误不触发 default。库的最终方法名和 API 可在迁移设计中收敛，但不能省略这个可表达机制、改用宏类型查询或按 Option 拼写判断。

Serialize 与 Deserialize 必须为独立协议。Serialize 的非 skip 字段只要求 Serialize；Deserialize 的非 skip 字段要求 Deserialize，default/skip 字段另要求 Default，skip 字段不要求任何读写 codec。仅 Serialize 上出现 default 不应额外要求 Default。返回 Self 的 Deserialize 通过上例显式类型实参的普通 read 函数选择具体实例，不依赖从 Deserializer 输入猜出目标类型。用户泛型 derive 暂拒绝，不影响库容器实现独立支持两个方向。

serde 的 map 实现首批精确针对 `map[string]T`；string 的透明 alias 同样匹配，defined string key 不自动匹配，保持当前边界。一般 std map 协议若引入 K 参数，还必须保留普通 Go `comparable` 类型约束，K 的 Go 声明不能降低成 any。一个仅含 marker 的库 trait 或手写 Eq 都不能让原本不可比较的类型成为 Go map key。comparable 只保证 map 类型可形成，不扩大各协议原有的键支持范围；需要更窄的稳定相等约束时，再使用普通显式库协议，不让 comparable 自动授予 Eq。

## 转译示例

以下展示已确认的输出边界；库协议名称在派生迁移时接入，示例不将当前内建名称误认为已迁移。

### GP-01：slice 条件实现由具体入口请求

```goxide
pub trait Clone { func clone(self &Self) Self }
impl Clone for int {
    func clone(self &Self) Self { return *self }
}
impl[T Clone] Clone for []T {
    func clone(self &Self) Self {
        if *self == nil { return nil }
        result := make([]T, len(*self))
        for i := range *self { result[i] = Clone::clone(&(*self)[i]) }
        return result
    }
}
pub func cloneInts(value &[]int) []int { return Clone::clone(value) }
```

Go 的公开入口为 `CloneInts(value *[]int) []int`。实例化后的元素调用选择 int 的具体实现，循环只复制一次每个元素；nil 保持 nil，非 nil 空 slice 保持非 nil。未请求的 `[]Other` 不额外生成，宏不根据元素类型自行分派。

### GP-02：条件 impl 不改变泛型类型声明

```goxide
pub struct Box[T any] { value T }
impl[T Clone] Clone for Box[T] {
    func clone(self &Self) Self {
        return Self{value: Clone::clone(&self.value)}
    }
}
pub func cloneIntBox(value &Box[int]) Box[int] {
    return Clone::clone(value)
}
```

`Box[T any]` 保留普通 Go 泛型。Go 消费者调用 `CloneIntBox`，可以另行构造 `Box[NoClone]`；只有源级请求克隆后者才要求其元素满足 Clone。具体业务类型的 derive 只输出普通 impl，字段协议由同一个 checker 证明。

### GP-03：跨包私有类型与普通 helper

容器包保留能访问其私有字段的普通泛型 helper，元素复制作为精确函数参数。业务包的具体公开函数传入本包的操作，不让容器包导入业务包或命名其私有元素类型：

```go
// 容器定义包
func CloneBoxFunc[T any](value *Box[T], clone func(*T) T) Box[T] {
    return Box[T]{value: clone(&value.value)}
}

// 业务包
func CloneRecord(value *Record) Record {
    return Record{payload: model.CloneBoxFunc(&value.payload, cloneSecret)}
}
```

上例仅展示 Clone 的操作签名。其他协议按其真实参数、结果和关联绑定生成；不增加通用运行时字典或反射兜底。

### GP-04：Default 函数约束与类型约束分开

`Box[T any]` 与 `defaultBox[T Default]() Box[T]` 使用不同约束。现有 carrier 能让 Go 从具体类型推导 Default 操作时，函数可以保留对应普通 Go 泛型；这不自动授予任意 `Box[T]` 一个普通 anchor。对没有 anchor 的容器组合，作者提供例如 `defaultUserBox() Box[User]` 的具体入口，由 Goxide 完成条件选择。

已有 Default 的业务默认、字段顺序和递归拒绝继续按当前合同处理；调整既有递归规则仍是本篇单列候选。

## 拒绝与边界

以下记录当前合同；GP-05/06 在通用方案实施后改按第 2 节的联合约束判定，其余行为不变。错误均由 Goxide 前端诊断，不能留给 Go 错误或静默退化：

| 编号 | 输入或条件 | 原因 |
| --- | --- | --- |
| GP-05 | 同时有 `impl[T Clone] Clone for []T` 与 `impl Clone for []int` | 目标模式重叠，具体实现无优先权 |
| GP-06 | `impl[T A] P for Box[T]` 与 `impl[T B] P for Box[T]` | 不以当前 A/B 约束区分重叠 |
| GP-07 | 第三方包为外部 trait 和外部类型写有体 impl，或为 `[]Local` 写外部 trait impl | 不满足 owner 规则；使用本地包装类型，不通过 declare 绕过 |
| GP-08 | 只具有同名 Go 方法，或只有伪造 helper，没有显式协议关系 | Go 结构相容不建立 Goxide impl |
| GP-09 | Node 的显式 where 前提要求 `[]Node: Clone`，容器又要求 `Node: Clone` | 输出有限诊断链，拒绝资格证明循环；不拒绝无条件具体 impl 的普通方法体递归 |
| GP-10 | `const N` 为负数、非常量、未能匹配，或需要求解 `[N+1]` 模式 | 超出首批长度参数规则 |
| GP-11 | Clone 方法通过只读 self 修改字段，或返回不相容 Self | 普通权限/类型检查；宏来源不豁免 |
| GP-12 | Eq 的 PartialEq 前提缺失、trait 前提形成环或关联类型绑定矛盾 | 普通协议证明失败 |
| GP-13 | `enum_valid` 的输入为普通接口、指针或无 enum 约束的类型参数 | 无法按静态 enum 身份检查，不插入反射或隐式解引用 |
| GP-14 | 只有 Serialize 的字段用于 Deserialize，或反向使用 | 两种协议独立，不能以一个共同 Codec 约束暗中补足或额外要求另一方向 |
| GP-15 | `type Score int` 没有显式协议，却因 int 的 impl 请求其协议 | defined 身份独立；透明 alias 对照用例应成功 |

库实现继续承担既有行为：Default 按字段顺序调用业务默认，enum 只要求默认变体 payload；Option 默认 None 不要求元素 Default，Result 不自动取得 Default；容器 Clone 说明存储和元素复制，不能改用赋值兜底；Debug 保持字段/变体名、数据及字符串引号，map 输出的排序等行为由库实现；serde 保持方向独立、缺失/default/skip、错误与范围检查、nil/空和 Option 规则。enum 的检查和变体身份属于通用 enum 合同，显式可失败检查按第 5 节实现，宏不能绕过普通 match 的检查。

这些要求需逐项迁移验收，不因泛型 impl 可以表达某个签名就认为业务等价。原生 map 键资格等额外能力若需证明，应由普通库协议及约束表达；手写 Eq 不自动取得该资格，浮点等类型也不能因 Go comparable 就被授予 Eq。不能在 checker 中按 Default、Clone 或 serde 名字偷偷补实现。

**PartialEq 的 enum 深层校验仍是迁移缺口。** 现有 enum 相等入口在比较变体或执行手写相等方法前，先校验左操作数、再校验右操作数的有效载荷；校验会穿过结构体字段、数组、slice 和 map 的值，即使载荷类型有手写 PartialEq。普通结构体和容器的相等比较则仍按访问顺序短路。`enum_valid` 只检查外层，不能代替这个既有合同。例如外层 enum 的有效变体携带一个结构体，结构体中未初始化的内层 enum 仍须导致 panic，即使另一操作数属于不同外层变体，且载荷的手写相等方法不会被调用。`tests/acceptance/equality_test.go` 与 `manual_equality_test.go` 保留这些行为验收；当前普通库证明尚未提供等价的通用表达方式，不得据此移除该校验或偷偷增加字段协议要求。所需通用能力或行为调整须另行确认。

普通库迁移基础已有真实 WASI 验证：`internal/compile/protocol_migration_test.go` 从公共 syn 读取实际字段和变体，生成五种 `Library*` 普通协议实现。具体 struct/enum 的 Default/Clone/Debug 证明覆盖默认变体字段的约束与调用顺序、Option None 不要求元素 Default、Result/容器不自动取得 Default；Clone 覆盖 slice/array/map、enum 与 Option/Result 的元素协议、独立存储和 nil/empty 状态；Debug 覆盖字段/变体名、字符串转义及 map 稳定排序。map Clone 的键限于非 complex 的内建 scalar，Debug 限于 string，透明 alias 对照通过，defined key 不自动匹配。14 个拒绝用例检查缺失协议、helper 选择及键资格；原有私有字段和手写字段相等协议断言保留。该宏仍只接受具体类型，未补齐上述 enum 深层 PartialEq、Eq 派生资格或 Default 调用图规则。

`internal/compile/error_protocol_migration_test.go` 以普通 `LibraryError` 宏读取 struct 字段和自定义 helper，生成 inherent `Error() string` 与有 source 时的 `Unwrap() error`，不使用内建 Error hook。证明覆盖消息插值与转义、scalar/error 透明 alias、私有字段、nil/typed-nil source 的保留，以及原生 `errors.Is` / `errors.As` 到达原 cause；14 个拒绝用例覆盖消息/helper 错误、source 与 scalar 类型资格、手写方法冲突。此证明限于具体 struct，不包含 enum 原生 error 桥接或 `#[from]` 注册。

两项证明均实际执行 Go 消费者 build/run/test，并核对锁定重放的生成内容与锁文件字节一致。`internal/compile/serde_protocol_migration_test.go` 验证普通 Serialize/Deserialize 协议与现有公开 serde API 的接线、条件容器、读写方向独立、Option alias 与指针的缺失区别、显式 null、default/skip 顺序及 enum 错误路径。`attribute_protocol_migration_test.go` 覆盖 attribute 产出的条件 impl，`print_protocol_migration_test.go` 覆盖格式解析、普通 Display/Debug 调用、求值顺序和调用帧。它们使用普通宏与库入口；std/serde 正式迁移发布及完整业务等价验收仍待完成。Default 专用调用图检查与 Eq 派生资格仍按上面的暂缓决定处理；旧入口的移除须随对应库的正式迁移验收进行。

### 后续迁移边界

已确认固有成员的新 Go 映射、enum 校验的 std 宏入口，以及后续 `std::prelude` 方向；这些实现与验收尚未完成。From/Into 基础已按 RFC 0032 实现，下述配置/返回适配迁移边界保持独立。Default 递归与 Eq marker 继续暂缓。

**保留配置选择和 Go 返回适配为语言机制。** 普通宏可输出现有 `cfg` / `cfg_attr` / `cfg!` 语法，由编译器继续按来源和构建角色求值。原始源码的配置选择先于入口登记与宏执行，宏生成项按现有展开流程继续筛选；完整规则包含宏 Context 不可查询的环境，不能以普通 attribute 执行代替。`go_result!` / `go_result_keep!` 继续依据真实单值或双值返回签名选择适配，按原静态错误类型判断 nil，并保留调用帧。让它们完整迁出需要另行设计配置阶段接口及静态返回形状能力，不能把仅支持 target/feature 或精确 `error` 的库函数算作完整替代。该候选保留 RFC 0030 的纯语法边界及 D36 的行为。

**固有成员的统一 Go 映射。** 已确认沿普通 `impl T` 实现，不增加 `#[go_method]` 属性，也不按 Error 名称增加方法。完整规则见 [RFC 0020](0020-associated-functions.md)。

| 固有成员 | 已确认的 Go 位置 |
| --- | --- |
| 无 self，任意返回类型 | 类型的空 carrier |
| enum 有 self，含返回 Self | E 接口声明签名，各指针变体承载方法业务体 |
| struct 有 self，含返回 Self | 按接收权限生成普通 Go receiver 方法 |

是否带 self 优先于返回类型；固有成员不生成包级纯函数。当前后端仍把关联函数与普通 enum 实例方法生成为包函数，现有 carrier 主要服务 trait，需补齐仅有固有 impl 的类型与泛型 carrier。trait wrapper、marker 和 As/For 的合同继续按 [enum 表示](../design/enum-representation.md#11-p4-的类型锚与实例适配)。

Self 始终是完整 enum；`self &Self` / `self &mut Self` 须保留真实 owner 槽，不能从变体临时装箱取址。carrier 与变体直接承载业务体，绑定方法值也须保持原求值、捕获和 `defer` / `recover` 行为。具体借用 owner 的承载、公开 Go 签名，以及 nil/typed-nil 在直接调用、方法值绑定和调用时的行为仍需闭合；条件 impl 的能力不能无条件写入整个泛型 enum 家族的方法集。固有成员不受 trait sugar 开关控制，按普通可见性和每个变体的字段/方法冲突规则检查。

验收须包含无 self 的 carrier、普通非 Error 方法、返回 Self、原生 Go interface 调用、直接与保存方法值的 recover、nil/typed-nil、私有变体、名称冲突，以及借用槽替换和条件泛型实例。Error 库以后输出相同的普通 impl，不另设专用桥接路径。

**From/Into 转换协议。** 当前讨论和实现范围是转译器与 std 基础：std 定义转换协议，编译器检查普通 impl 并为 `?` 选择转换；Error 派生库以后读取 `#[from]` 并输出普通转换 impl。属性、错误消息、source 与上下文选择属于库的职责，不作为转换协议的编译器登记入口。

D22 与 [RFC 0005](research/0005-option-result-question.md#11-thiserror-与静态错误类型) 已确认 `?` 先普通可赋值，再精确静态来源与目标的唯一一步 From；不搜索链、不为匹配先装箱、不猜 Go error 的动态类型。当前实现查询 canonical `std::convert::From`，不再登记本包 helper 或 Error 派生的转换函数。

[RFC 0032](0032-from-into-conversions.md) 以完整 trait application 区分来源与目标，std 用普通 identity 与 Into blanket，统一跨包归属、通用互斥证明及 `?` 证据选择。此前无输入 trait、参数只出现在 Self、裸参数 target 禁令已被完整头规则替代；通用互斥及原生采用移除统一按本篇第 2 节。没有新增 `#[try_from]` 登记属性。

**enum 比较前的有限结构校验。** 公开入口采用 `std::enum_assert!(value)`，名字不带 valid 后缀。宏表示检查请求，类型布局及字段访问由后续类型检查与降低处理，不向过程宏开放类型查询。首批用于 payload 静态类型完全确定的 enum 实例：实参求值一次，无返回值；普通 PartialEq 宏在两个操作数都已求值后，先对左值、再对右值调用，随后才比较变体或执行业务相等方法。校验读取活动 payload，穿过结构体字段、array、slice 和 map 值，不调用字段的业务协议，也不把额外协议条件加到字段类型上。非法 enum 按既有规则 panic。现有 `enum_valid` 的外层 bool 检查保持不变。

校验范围沿用当前有限遍历：不遍历 pointer、普通 interface 或 map 键；遇到当前包含路径上已有的规范类型，终止该条边且不再次校验该节点。例如 `E` 的变体携带 `[]E`，slice 元素会在静态回边处截断，不能据此证明整个运行时对象图有效。若将不可访问字段的检查移到其定义包，生成 helper 仍须保留调用点的路径截断，不能重新开始校验。只在实际需要读取的字段无法合法访问且没有可生成检查的定义源码时诊断；无关私有 scalar 字段不构成拒绝理由。这部分跨包实现与等价验收尚未完成，不能把 helper 设想记作支持。迁移时以普通库宏验收有限遍历、左右顺序、手写 payload 协议及私有字段路径，再移除对应 hook。

**后续 std::prelude。** 已确认提供标准库常用入口的 prelude 方向。具体重导出清单、导入方式和遮蔽规则以后设计；当前仍沿显式 use 与宏路径解析，不由这项决定增加隐式注入或新的名称优先级。

## 双向互操作

Goxide 调用原生 Go 函数和方法保留原签名；普通 Go interface 使用 Go 方法集，不搜索 Goxide 条件 impl。原生类型需要 Goxide 协议时，使用符合 owner 规则的有体 impl 或本地包装类型；不恢复 declare 采用。

Go 调用已生成的具体业务入口、具体 impl 的完整协议入口以及启用的 sugar。只有 Goxide 源构建解析新的条件需求；纯 Go 下游新增类型组合不会触发编译器隐式生成。原生 Go 的 `Box[T]` 继续保持源类型参数和身份，条件 impl 不回写容器声明约束。

[sugar](../design/traits.md#go-成员-sugar) 的签名参与 Go 方法集、接口和原生泛型约束检查，不登记成新的 Goxide 固有成员。显式固有/手写 Go 方法先占名，剩余候选重名时报错。模块开关不改变 impl 选择、宏输入、接收权限或完整协议入口。

## 验收与未定项

实施顺序：普通泛型/条件 impl 和多约束 → 转译范围内需求闭包与普通 Go 入口 → 模块 sugar 与属性生命周期 → 普通库 impl 和过程宏接线 → 删除对应固定派生检查及降低。转译改动按主题独立原子提交，与业务宏迁移分开验收。宏输出始终回到 `expand → resolve → check → lower`。

验收至少包括：

1. 具体 impl 在没有 Goxide 调用时仍可被 Go 使用；未请求的条件实例不生成，普通 Go 泛型继续保留。
2. slice、map、具名容器嵌套及具体字段请求形成完整闭包；Go 消费者只调用明确普通入口，验证字段/元素调用顺序、nil/空和存储身份。
3. 泛型函数显式/推导调用、函数值、关联类型及多约束传播正确；具体和相互递归实现去重，证明循环与类型增长递归有源位置诊断。
4. 跨包私有元素、私有容器字段和定义点 helper 保持可见性；无反向依赖，同输入两次生成字节一致。
5. 普通函数的业务体保持调用帧与源码位置，覆盖 `defer`/`recover`、体内注释和行号；不以额外转发层改变语义。
6. sugar 的默认/TOML/CLI 优先级、依赖配置隔离、原位生成和缓存切换；Go 混写与独立 dist 消费真实 build/run/test，关闭 sugar 后完整协议入口仍可用。
7. 手写、声明宏、attribute 和 derive 产出的 impl 使用同一路径；显式成员先占名、剩余冲突诊断、借用保留槽位身份，sugar 不成为源级新固有方法。
8. Serialize/Deserialize 需求独立，保留 Option alias 缺失、pointer 缺失、present 错误、default/skip 顺序及 enum 错误行为；不按 derive 名称补隐式能力。
9. 惰性属性遵循消费者生命周期，attribute 替换负责转交、derive 保留原项；未知属性仍拒绝，不实现 `no_mangle`。

最小数组长度与 enum 显式检查已按上述最小范围实现；Default 既有递归规则调整和 Eq marker 候选暂缓。跨仓库写入与发布遵循对应授权。阶段状态记录在状态表，不把机制原型或未执行的迁移验收记为完成。

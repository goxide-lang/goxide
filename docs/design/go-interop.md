# Go 原生类型的显式适配

双路径研究（D21/U16）见 [RFC 0004](../rfcs/0004-trait-lowering-paths.md)：省掉内部包装与改变 Go 动态身份是不同操作；当前 D05 的原包 marker 模式未因研究而撤销。

状态：本文的 `declare impl` 表示采用已有业务实现并补 marker；有体 impl 表示提供自定义 wrapper 实现。声明可写在用户自己的 `.hgo` 包中，只由 hgo 编译器读取，纯 Go 项目不受影响。伴生文件的构建接口仍为设计草案；其他 declare 形式见[原生 API 与裸指针](foreign-api.md)。

| 状态 | 范围 |
| --- | --- |
| 已确认 | D05、D06：C 包声明，原生无需预备 wrapper；无体补 marker / As 返回自身；既有纯 Go 项目不受影响。 |
| 暂定 | T07、T08：伴生文件、独立源码副本 / replace、匿名接口减少循环、发布路径。 |
| 未定 | U03、U07：接收权限身份、构建 / 缓存 / 声明选择与可复现产物发布。 |

状态编号见[决策登记](decisions.md)。除已确认范围外，正文的具体规则与验收条件均为暂定方案；Go 示例验证不代表 hgo 功能已实现。

本文 `*native::T` 和它的别名用于描述 Go 方法集的精确接收者身份，不能绕过新引入的 Ptr/借用权限体系。安全 hgo 调用还须选定合法的引用/句柄视图；接收者语法接合状态见[源级权限约束](overview.md#35-源级权限与-go-表示必须分开)。

## 1. 声明属于使用方 C 包

```hgo
package C

import AorB "example.org/protocols"
import BBB "example.org/native"

declare impl AorB::XXX for BBB::Type;
```

这条声明由 C 包拥有。hgo 编译器记录“C 明确采用 BBB::Type 的已有方法来实现 AorB::XXX”，并生成需要的 marker。不要求 AorB 或 BBB 作者配合，不把声明写回这些包，也不修改它们的原始源码。

声明只存在于实际参与本次 hgo 编译的源码与元数据中。纯 Go 工具链不读取 `.hgo` 文件；其他纯 Go 项目仍以原版 BBB 正常构建，看不到这条声明，也不承担新增 marker 的要求。

此种显式 impl 不要求专门的 `.d.hgo` 文件、`declare go` 包描述块或手工描述 wrapper 对应关系。`.d.hgo` 可以作为原生 API 类型契约的可选分发形式，见[原生 API 与裸指针](foreign-api.md)。原生 Go 已有的结构体、字段和方法仍以实际包类型信息为 ABI 校验依据。

## 2. 原生包只需要业务方法

例如 `example.org/native/buffer` 只有以下代码，没有 wrapper、marker 或 `AsXXX`：

```go
package buffer

type Buffer struct { Data []byte }

func (b *Buffer) Read(p []byte) (int, error) {
    return copy(p, b.Data), nil
}
```

使用方写：

```hgo
package adapters

import native "example.org/native/buffer"
import byteio "example.org/hgo/byteio"

type Buffer = *native::Buffer

declare impl byteio::Reader for Buffer;
```

本例 `Buffer` 是原生指针类型的本地别名，不产生新结构体；也可直接写 `declare impl byteio::Reader for *native::Buffer;`。如果写 `for native::Buffer`，则检查值类型的方法集，不会为通过检查而偷偷取地址。[Go 方法集](https://go.dev/ref/spec#Method_sets)

declare 不是等待以后补齐业务方法的前置声明。编译器先校验实际业务方法：缺少 `Read` 是错误，缺少 marker 则是本声明要完成的生成工作。

`declare impl` 同时表示声明作者可信地采用该 trait 全部方法的调用契约；不要求为相同契约再逐方法补写 `declare func`。编译器校验签名与声明兼容，不能仅凭验签证明原生方法的不逃逸、只读等行为。marker 本身也不将方法的独立原生调用视图改为 safe，详见[原生 API 与裸指针](foreign-api.md)。

## 3. 生成 marker，AsXXX 返回自身

假设 trait 的完整 Go 表示是：

```go
// 片段：byteio 的 Go 输出。
type Reader interface {
    Read([]byte) (int, error)
    HyperGoTraitByteReader()
}
```

对于上面的原生类型，hgo 在本次构建视图中生成同包伴生文件：

```go
package buffer

func (s *Buffer) HyperGoTraitByteReader() {}

func (s *Buffer) AsByteReader() interface {
    Read([]byte) (int, error)
    HyperGoTraitByteReader()
} {
    return s
}
```

没有新增业务方法体，也没有额外 wrapper。接口值的动态类型仍是原始 `*Buffer`，`AsXXX()` 返回同一个指针。marker 保留；它正是 declare 在 Go 方法集层补充的显式标记。

marker 使用稳定 trait 身份编码，形式为零参数、零结果的方法。示例采用短名称，正式编码须区分包身份、trait 参数并检测冲突。已有同名同签名 marker 可以校验后采用；不同签名必须诊断，不能覆盖用户方法。

### 3.1 声明归属与生成位置不同

声明源码属于 C，但 marker 是普通 Go 方法，必须进入接收者类型的定义包，不能放在 C 的 Go 包里给外部类型增加方法。[Go 方法声明](https://go.dev/ref/spec#Method_declarations)

这是后端生成位置的约束，不意味着向 BBB 的仓库、原始目录或共享模块缓存写文件。伴生代码只存在于 hgo 构建视图中；普通 Go 项目继续读取原始 BBB。

别名要解析到真正的类型定义包。首版仅支持 Go 允许添加方法的具体 named type 及其指针；接口、内置类型或其他不合法接收者须诊断，不能偷偷转换成另一种 wrapper 模式。泛型接收者生成另行设计。

同一基类型的 marker 要集中去重。值接收者 marker 已使 `*T` 也拥有该方法时，不能再声明同名指针方法；hgo 的 T 与 *T 显式关系仍分别保存在元数据中，不根据 Go 的方法提升反向创造 impl。

### 3.2 避免仅为 AsXXX 造成包循环

原生包不能总是反向导入 trait 包：trait 包可能已经导入该原生包。因此生成的 `AsXXX` 可以返回含有完整业务方法和 marker 的匿名接口或本包接口，该结果可赋给 trait 包中的命名接口。

这不意味着 `func() 本包接口` 与 `func() byteio.Reader` 是相同的函数类型；函数值使用处仍需精确适配。这个方案只消除返回类型引入的额外依赖，不能消除其他真实包循环。

## 4. 本次 hgo 构建如何看见伴生文件

建议流程：

1. 读取原始 Go 模块图与包类型信息，以及 C 等实际导入的 hgo 包元数据。
2. 校验 declare 所采用的业务方法，检查接收者合法性和 impl 唯一性。
3. 按接收者的真正定义包聚合生成 marker 和适配入口。
4. 在独立的 Go 构建视图中加入伴生文件，再检查、编译完整方法集。
5. hgo 编译器和 LSP 使用同一声明集合与构建视图，生成位置可追溯到 C 的 declare。

Go `-overlay` 可以将不存在的目标路径视为一个源码文件，适合本地包的虚拟伴生文件。但是当前 Go 工具链禁止覆盖 `GOMODCACHE` 下的文件，不能直接向任意已下载模块使用 overlay。[Go 构建参数](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)

对模块缓存中的依赖，可在缓存外生成保持 module/import path 的私有源码副本，再将伴生文件和生成的构建配置放入该视图。使用本次构建自己的根配置和本地 replace 指向副本，不改用户原始 go.mod、依赖文件或共享缓存。

标准库不能通过模块 replace 改写。可针对当前工具链的 GOROOT 使用构建 overlay 注入伴生文件而不改原文件；Go 1.27.1 的本地实验已验证 bytes.Buffer 可新增 marker 和 AsXXX。工具链、平台与缓存的完整适配仍需在实现时验证。

若无法合法生成 marker，或者生成入口与已有方法冲突，应诊断，不能静默删除 marker 或切换实现模式。

### 4.1 与纯 Go 使用的边界

已有纯 Go 项目不需要采用任何生成配置，原样使用其依赖即可。隔离视图只属于正在进行的 hgo 构建。

另一种情况是用户要把依赖这些新增方法的 hgo 输出交给纯 Go 下游：此时需要提供完整的生成源码视图或可复现的构建入口。原版依赖并没有新增方法，仅交付调用方 Go 文件不足以复现增强构建。这个分发问题不改变前述“现有纯 Go 项目不受影响”的原则，具体发布布局另定。

## 5. 自定义 impl 使用自己的实现

沿用第 2 节的 Buffer 别名，可以写：

```hgo
import textio "example.org/hgo/textio"

impl textio::Reader for Buffer {
    read() string {
        return string(self.Data)
    }
}
```

hgo 生成保存原始 `*native.Buffer` 的 wrapper，业务方法体和该 trait 的 marker 都在 wrapper 上。原生包不需要已有这种包装；自定义行为的 marker 不加到原始类型上，避免原值绕过自定义方法体。

即使原生方法与自定义方法同名同签名，选中该 impl 也必须使用其方法体。不能因为 Go 方法集碰巧满足就优化成直接返回原对象。

| 源级形式 | 业务方法来源 | marker 位置 | Go 接口动态类型 |
| --- | --- | --- | --- |
| `declare impl Trait for T;` | T 已有方法 | T 的定义包构建视图 | 原始 T |
| `declare impl Trait for *T;` | *T 已有方法 | T 的定义包构建视图 | 原始 *T |
| `impl Trait for T { ... }` | 用户方法体 | 生成 wrapper | 对应 wrapper |

首版有体 impl 完整提供必需方法，不把漏写的方法默认为原生委托。需要转发时写正常方法调用。

原生包已有其他包装类型时，可对包装类型本身写 declare；它不与另一个原始结构体成为同一类型。带游标、缓存等状态的包装应创建并保存一次，不能在每次方法调用时重新创建。构造、生命周期和委托使用普通语言能力，不再引入 wrapper/via/method 的专用声明语法。

## 6. 校验、来源与唯一性

业务方法按 trait 规定的规范 Go 名称与签名校验；不能随原生类型临时改名或加入隐式参数/结果转换。marker 和适配入口则在生成视图中接受真实 Go 类型检查。[go/packages](https://pkg.go.dev/golang.org/x/tools/go/packages) 和 [go/types](https://pkg.go.dev/go/types) 提供包信息与方法集操作，hgo 另行维护声明来源和实现身份。

外部类型与外部 trait 可以在用户包 C 中组合声明，不要求修改二者源码。实现元数据以 C 为定义来源，按 hgo 的实际导入关系使用，不成为 BBB 对所有项目公开的一条新声明。

在参与构建的有效实现集合中，同一个规范 `(接收者类型, trait, trait 参数)` 最多一份定义。别名归一；同一个公共适配包被多处导入不算重复；两个独立定义即使都采用相同方法也应报冲突。declare 与有体 impl 不能互相覆盖，关联类型不增加实现选择维度。

Go 包版本、replace、平台、tags 或声明集合改变时，要重新校验并更新构建视图。缓存包含工具链、构建配置、原包类型指纹及完整生成集合，避免把其他 hgo 构建的 marker 当成上游固有方法。

公开 marker 可以被 Go 代码手写，嵌入也可能提升它。因此 hgo 不根据 marker 或结构匹配自动推断新的显式 impl；声明元数据仍是源码级关系的依据。

## 7. LSP 与稳定性

```text
BBB::Type
  原生方法                      [导航到原 Go 源码]
  declare impl AorB::XXX        [定义来源 C；补 marker]
    trait 方法                 [导航到被采用的 Go 方法]
  自定义 impl                  [定义来源相应 hgo 包]
    trait 方法                 [导航到 hgo 方法体]
```

LSP 与编译器读取同一份声明元数据。生成的 marker 和 AsXXX 可以追溯到 C 的 declare，不伪装成上游源码本来就有的内容。纯 Go 项目仍看到自己的原始源码视图。

declare 固定使用原始动态类型，有体 impl 固定使用对应 wrapper。新增别的 trait 实现不能自动切换旧模式；主动把 declare 改成有体 impl 会改变行为来源和反射/类型断言结果，需要兼容性评估。

## 8. 实现验收条件

1. C 的普通 hgo 文件可以声明外部 trait 与外部类型的实现关系，不要求上游配合。
2. declare 校验已有业务方法并补 marker，AsXXX 返回同一个原始指针，不生成额外 wrapper。
3. 伴生文件参与本次真实 Go 构建；原始源文件和共享缓存未被修改；独立纯 Go 构建前后行为不变。
4. 接收者不合法、缺业务方法、名称或签名冲突时准确诊断。
5. 有体 impl 的业务方法和 marker 均位于 wrapper，同名原生方法不会绕过自定义实现。
6. 声明归属、impl 唯一性、生成位置和 LSP 导航一致。

这些是未来编译器的验收要求。当前只有设计文档和独立 Go 构建实验，尚无 hgo 端到端实现。

## 9. 原生 Go API 的两种视图

原生 Go `*T` 的旧 raw `Ptr[T]` 导入方案与普通参数桥接现列 U15 待协调；D19 明确普通调用不因未描述而要求 unsafe。此前将所有未知 `*T` 自动投影为 `Option[Arc[T]]` 的方案不再作为原生默认视图；`Option[Arc[T]]` 仍可用于明确选择的声明或包装接口。

普通 `declare func` 可以提供 `&T`、`mut& T`、`Arc[T]` 等精确的调用接口，声明作者负责相应外部行为契约。`.d.hgo` 和独立声明包用于分发可复用的 hgo 视图；这与本篇 `declare impl` 的方法采用及 marker 生成规则相互补充。

Ptr 的 Go 表示、AsPtr 的 trait 实现、unsafe 表达式、函数值传播、声明解析与双向边界见[原生 Go API、安全声明与裸指针](foreign-api.md)。

# 用户关联协议与泛型调用

本片为用户定义协议提供无实例接收者的关联函数。协议成员用 `func` 声明，`impl` 显式给出实现，具体类型和受约束类型参数都使用 `::` 调用。

```goxide
trait Build {
    func Abc() Self
}
type User struct { Name string }
impl Build for User {
    func Abc() Self { return Self{Name: "Ada"} }
}
func Make[T Build]() T { return T::Abc() }
```

`User::Abc()` 和 `Make[User]()` 选择同一个显式实现。`Self` 在实现签名和函数体中指向实现目标；它不是 carrier 类型，也不是运行时 trait wrapper。函数体中的字符串、注释和普通词法绑定不按文本批量替换。可执行入口见[示例](../examples/static_protocol/README.md)。

## 实现范围

协议自身非泛型；实现目标限声明包内的具体、非泛型 struct 或 enum。函数可用单个协议约束类型参数，例如 `func Make[T Build]()`；没有该约束的 `T any` 不能调用 `T::Abc()`。协议成员、参数、结果和 `Self` 必须与实现一致，不按同名 Go 函数或相似方法集推断实现。

跨包使用 `pub trait` 发布协议，再通过显式 `use` 导入。类型别名保留目标身份；新定义的同形类型不会继承 impl。`pub use` 重导出保留协议、类型和公开泛型函数的真实来源；不同文件中的同名 namespace 各自按该文件的绑定解析。

这次不扩展为完整 trait 系统：实例方法与运行时 trait 视图、泛型协议、泛型 impl、多协议组合、外部目标实现、一般固有 `impl T` 和自动适配均不在此片范围。原有 Default、Clone、Debug、serde、PartialEq/Eq 等业务协议及派生入口仍按各自合同使用，未统一迁写成这里的新语法。

## 普通 Go 降低与消费

生成目标类型带精确 `Self` 类型锚，关联函数由无字段的结构 carrier 承载。普通库 `github.com/goxide-lang/std/typeinfo` 定义 `CarrierFor[T,S]` 和 `Of[T]()`；后者返回 carrier 零值，不创建目标值，不调用类型锚，不使用反射或运行时注册表。

生成的泛型函数把 carrier 类型参数追加在源类型参数之后。Go 从目标类型的精确锚推导它，所以独立 Go 调用者仍写 `Make[model.User]()`，无需知道生成 carrier 名称，也可直接调用 `typeinfo.Of[model.User]().Abc()`。

函数的 `pub use` 保持已有透明重导出规则：Goxide 下游通过 facade 的公开绑定调用，普通 Go 下游调用实际声明包的函数；不为重导出另外生成函数 wrapper。公开类型 alias 则是普通 Go 的真实 alias。

`GoxideInternalTypeCarrierForSelf` 是编译器保留的锚名称；用户的 `.gox` 或同包原生 `.go` 声明与它冲突时拒绝。普通独立 Go 包可以自己实现满足 `typeinfo` 结构约束的 carrier 并调用该库，这不会凭空获得 Goxide 的显式 impl 登记。既有 Go 方法表达式 `U::Method(receiver, ...)`、实例方法和 Go interface 互操作继续遵循原来的语义。

## 生成物与依赖

跨模块消费从 `.goxide.protocols.json` 恢复协议、实现和源泛型参数信息，并校验生成 Go、依赖及签名。保留 Go 文件却删除 sidecar、修改生产者后继续使用旧登记、或通过错误的重导出认领实现都会拒绝。更换实际 `replace` 依赖时仍检查所选包的真实类型和函数签名；包路径与 helper 名称本身不是证明。

生成模块依赖 std，不反向依赖编译器。原生 Go 消费生成代码不需要运行 Goxide；再次由 Goxide 导入时才需要完整的有效 metadata。已有旧生成物应重新生成，不提供猜测旧协议登记的兼容入口。

验收入口为 `go test -run '^TestStaticProtocol' ./tests/acceptance`；其中真实 CLI 产生独立模块，再执行 Go race/vet，并覆盖别名、重导出、隐藏尾参、实际依赖替换及错误实现。文档中的运行时 trait 设计仍分别见 [RFC 0002](rfcs/0002-traits.md) 与 [RFC 0019](rfcs/0019-uniform-trait-wrappers.md)，不能将此片通过理解为这些设计全部完成。

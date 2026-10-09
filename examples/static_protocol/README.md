# 用户定义的关联协议

本例定义 `Build`，为本包 `User` 显式提供 `impl`，分别执行具体调用 `User::Abc()` 和受约束泛型调用 `T::Abc()`。`Self` 表示 `User`；`Make[User]()` 只填写源程序声明的类型参数。

在仓库根目录执行：

```sh
export GO_BIN=/absolute/path/to/go1.27.1/bin/go
work="$(mktemp -d)"
"$GO_BIN" mod download all
"$GO_BIN" build -o "$work/goxide" ./cmd/goxide
"$work/goxide" transpile -o "$work/generated" examples/static_protocol
(cd "$work/generated" && "$GO_BIN" run -race . && "$GO_BIN" vet ./...)
```

预期输出：

```text
Ada Ada
```

生成代码使用固定版本 `github.com/goxide-lang/std/typeinfo` 的普通泛型函数 `Of` 获取空结构 carrier，再调用其方法。原生 Go 调用者同样可以写 `typeinfo.Of[User]().Abc()` 或 `Make[User]()`，无需填写编译器追加的 carrier 类型参数。类型锚只用于约束与推导，不在运行时执行。

首片限非泛型协议、本包具体 struct/enum 的显式实现、函数上的单协议泛型约束；实例 trait 视图、泛型 impl、多协议组合及固有 `impl T` 不由本例承诺。已有 Default/Clone/serde 等派生入口保持各自合同。完整范围和跨模块行为见[关联协议说明](../../docs/static-protocols.md)。

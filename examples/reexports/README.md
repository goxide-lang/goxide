# use / pub use：统一 goxide API 入口

`api/api.gox` 使用 `pub use implementation::*;` 聚合公开类型、常量、函数、变量及本次源图内的用户宏。
`goxide.roots.toml` 将 `implementation` 精确映射到 Go 包 `example.org/reexports/impl`。
调用方的 `Counter` 和 `api::Count` 仍是 `implementation::Count` 的同一存储位置；
`api::Add` 调用原函数，没有生成函数变量或调用 wrapper。

```sh
go run ./cmd/goxide run --go /path/to/go examples/reexports
```

在仓库根运行，且 `go` 应是 Go 1.27+ 编译器。输出：

```text
shared 2 2 true
42 2 7
```

目录转译产物包含 `.goxide.uses.json` 和真实 Go 类型/常量 alias。
普通 Go 仍从 `impl` 调用函数和访问变量；这里不伪造 Go 同名函数/变量 alias。
宏别名保留原定义点；独立module生成产物尚不能发布宏。
完整支持范围和限制见 [RFC 0021](../../docs/rfcs/0021-reexports.md)。

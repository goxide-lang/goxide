# 显式项目目标

这个纯 TOML 项目含一个 lib、同一 src 目录中的两个 main，以及一个 example。`[namespaces]` 把 `greeting` 映射到本模块的库包；`fmt = { go = "fmt" }` 声明免下载的 Go 标准库包。各源文件再通过 `use` 绑定名称，使用 `::` 访问包成员。
在仓库根构建 CLI 后运行；把 GO_BIN 设为真实 Go 1.27+ 路径：

```sh
"$GO_BIN" build -o /tmp/goxide ./cmd/goxide
# 首次项目构建自动下载声明及 Go 支持库依赖。
(cd examples/project && /tmp/goxide build --go "$GO_BIN" --bin hello)
/tmp/goxide run --go "$GO_BIN" --bin hello --offline examples/project
/tmp/goxide run --go "$GO_BIN" --bin hello --no-default-features --offline --locked examples/project
/tmp/goxide run --go "$GO_BIN" --bin inspect --offline examples/project
/tmp/goxide run --go "$GO_BIN" --example greet --offline examples/project
/tmp/goxide build --go "$GO_BIN" --lib --offline examples/project
/tmp/goxide build --go "$GO_BIN" --bin hello --offline examples/project
/tmp/goxide transpile --go "$GO_BIN" --bin hello --offline examples/project
```

依次输出 `Hello, goxide!`、`hello`、`pretty: true`、`example: Hello, goxide!`。
build 打印默认 `target/bin/.../hello` 路径；transpile 打印保留的生成 module 路径。用 `--target-dir build-output` 可更换目录，相对路径以 examples/project 为根。本例已提交真实解析的 `goxide.lock`；新项目首次解析生成锁，实际项目应提交它，`target/cache/` 保存 Git 源缓存，`target/` 保存受管产物，不提交。

格式、依赖、cfg 与明确限制见 [RFC 0022](../../docs/rfcs/0022-project-toolchain.md)。

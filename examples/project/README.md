# 显式项目目标

这个纯 TOML 项目 含一个 lib、同一 src 目录中的两个 main，以及一个 example。
在仓库根构建 CLI 后运行；把 GO_BIN 设为真实 Go 1.27+ 路径：

```sh
"$GO_BIN" build -o /tmp/hgo ./cmd/hgo
/tmp/hgo run --go "$GO_BIN" --bin hello --offline examples/project
/tmp/hgo run --go "$GO_BIN" --bin hello --no-default-features --offline --locked examples/project
/tmp/hgo run --go "$GO_BIN" --bin inspect --offline examples/project
/tmp/hgo run --go "$GO_BIN" --example greet --offline examples/project
/tmp/hgo build --go "$GO_BIN" --lib --offline examples/project
/tmp/hgo build --go "$GO_BIN" --bin hello --offline examples/project
/tmp/hgo transpile --go "$GO_BIN" --bin hello --offline examples/project
```

依次输出 `Hello, goxide!`、`hello`、`pretty: true`、`example: Hello, goxide!`。
build 打印默认 `target/bin/.../hello` 路径；transpile 打印保留的生成 module 路径。用 `--target-dir build-output` 可更换目录，相对路径以 examples/project 为根。首次解析生成 `goxide.lock`；实际项目应提交它，`.hgo/` 是本地缓存，`target/` 是受管产物，不提交。

格式、依赖、cfg 与明确限制见 [RFC 0022](../../docs/rfcs/0022-project-toolchain.md)。

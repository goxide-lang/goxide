# 显式项目目标

这个独立 Go module 含一个 lib、同一 src 目录中的两个 main，以及一个 example。
在仓库根构建 CLI 后运行；把 GO_BIN 设为真实 Go 1.27+ 路径：

```sh
"$GO_BIN" build -o /tmp/hgo ./cmd/hgo
/tmp/hgo run --go "$GO_BIN" --bin hello --offline examples/project
/tmp/hgo run --go "$GO_BIN" --bin hello --no-default-features --offline --locked examples/project
/tmp/hgo run --go "$GO_BIN" --bin inspect --offline examples/project
/tmp/hgo run --go "$GO_BIN" --example greet --offline examples/project
/tmp/hgo build --go "$GO_BIN" --lib --offline examples/project
/tmp/hgo build --go "$GO_BIN" --bin hello --offline -o /tmp/hgo-hello examples/project
```

依次输出 `Hello, goxide!`、`hello`、`pretty: true`、`example: Hello, goxide!`。
首次解析生成 `hgo.lock`；实际项目应提交它，`.hgo/` 是本地缓存。

格式、依赖、cfg 与明确限制见 [RFC 0022](../../docs/rfcs/0022-project-toolchain.md)。

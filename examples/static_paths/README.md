# 显式依赖与静态路径

本例通过 `goxide.roots.toml` 精确绑定 serde、JSON 和 fmt；`mod logging;` 选择本地子包。`use serde::derive::Serialize;` 与改名后的 `wire::derive::Deserialize` 证明派生按 canonical owner 解析；JSON 往返后分别调用跨包宏、类型方法表达式和接收者方法。

```sh
export GO_BIN=/absolute/path/to/go1.27.1/bin/go
work="$(mktemp -d)"
"$GO_BIN" run ./scripts/build.go -o "$work/goxide"
"$work/goxide" transpile -o "$work/generated" examples/static_paths
(cd "$work/generated" && "$GO_BIN" run -race . && "$GO_BIN" vet ./...)
```

输出：

```text
{"Text":"static paths"}
12
12
```

生成模块使用固定公开版本的 std 与 serde 源码，通过输出目录中的受管相对 `replace` 引用已生成的依赖；不依赖 compiler 源码。私有 `use` 只属于其声明文件；外部根不因为在配置中出现而自动导入。把 `wire::derive::Deserialize` 改为 `json::Deserialize`、删去 `use serde::derive::Serialize;` 后继续用裸 `Serialize`，或把 `logging::show!` 改成点号调用，都会拒绝。详见[静态路径合同](../../docs/design/static-paths.md)。

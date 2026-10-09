# 单文件语言示例

这些文件分别编译，显式依赖根由 [goxide.roots.toml](goxide.roots.toml) 声明。从仓库根运行时传入此配置，避免继承仓库模块的其他配置：

```sh
"$GO_BIN" build -o /tmp/goxide ./cmd/goxide
/tmp/goxide run --go "$GO_BIN" --roots examples/prototype/goxide.roots.toml examples/prototype/order.gox
/tmp/goxide run --go "$GO_BIN" --roots examples/prototype/goxide.roots.toml examples/prototype/conditional_log.gox
```

`GO_BIN` 应指向 Go 1.27.1。包和类型路径使用 `::`，值字段和方法使用 `.`；序列化派生来自 `serde`，JSON 函数来自 `serde::json`。

# 构建脚本：生成 Go 代码和嵌入资源

此例只使用标准库，没有第三方工具或 host helper。`tools/build/main.hgo` 读取 manifest 显式声明的 `schema.txt`，在 runner 提供的输出目录生成 `gen/schema.go` 和 `gen/data.txt`。应用导入生成包；该包用 `go:embed` 嵌入资源。源码目录不会出现 `gen/`。

需要 Go 1.27+ 和本分支的 `hgo`。可在仓库根目录执行 `go build -o /tmp/hgo-build-script-demo ./cmd/hgo`，然后将下列命令的 `hgo` 换成 `/tmp/hgo-build-script-demo`。所有选项都放在项目路径之前。离线首次运行前，请在编译器仓库根用同一真实 Go 执行 `go mod download github.com/goxide-lang/std github.com/goxide-lang/serde`，预热本例锁定的公开支持库；冷缓存不会被 `--offline` 自动补齐。

## 手动运行

先阅读 `goxide.toml` 和 `tools/build/main.hgo`，再选择是否信任。下面使用独立临时信任目录和产物目录，不写入日常授权记录。在同一个终端运行：

```sh
cd examples/build_script

demo_tmp=$(mktemp -d)
export HGO_TRUST_DIR="$demo_tmp/trust"
export HGO_TARGET_DIR="$demo_tmp/target"

# 只查看计划，不执行生成器；root_trusted 初始为 false。
hgo scripts --action plan --offline .

# 明确信任当前根项目，日常脚本编辑不需要逐次重新授权。
hgo scripts --action trust-project --offline .

# 首次显示 build-script run，并运行应用。
hgo run --bin app --offline .

# 再次显示 build-script cache hit；应用仍正常运行。
hgo run --bin app --offline .
```

应用输出：

```text
schema: Greeting v1
embedded: resource for Greeting v1
```

验证声明输入变化会重新运行生成器：

```sh
cp schema.txt "$demo_tmp/schema.before"
printf 'Greeting v2\n' > schema.txt
hgo run --bin app --offline .
# 显示 build-script run，应用输出 Greeting v2。
cp "$demo_tmp/schema.before" schema.txt
```

`build` 不需要 `-o`，会打印最终可执行文件的绝对路径。`--target-dir` 优先于 `HGO_TARGET_DIR`；未指定两者时默认使用项目根的 `target/`。相对路径以项目根为基准。

```sh
hgo build --bin app --offline --target-dir "$demo_tmp/chosen-target" .
hgo scripts --action clean --offline --target-dir "$demo_tmp/chosen-target" .
hgo build --bin app --offline --target-dir "$demo_tmp/chosen-target" .
# clean 后脚本重新运行。
```

`scripts clean` 只清所选受管目录内的脚本结果缓存，保留最终二进制、历史构建 bundle、其他 target 目录、源码、`goxide.lock` 和信任记录。它不是全项目清理命令；遇到陌生目录或不匹配的所有权记录会拒绝删除。`goxide.lock` 留在项目源目录；信任目录必须在源码、helper 和受管产物目录之外，不能放进 `target/` 或交给项目一起分发。

## 能力边界

- 执行模式是 **unconfined**：这是普通本机程序，不是操作系统沙箱。声明输入和输出检查约束缓存及产物发布，不能阻止脚本访问其他文件、网络或启动进程。请只信任已审查的脚本。
- `--offline` 禁止工具链获取依赖，不构成脚本网络隔离；声明输入缓存也不是 hermetic 构建证明。此例不使用网络。
- 当前只支持 host-only 构建。request 分别提供 host/target 字段，但非本机 GOOS/GOARCH 会在执行脚本前被拒绝。
- 根项目信任与第三方脚本/helper/tool 的精确许可相互独立。此例没有第三方执行主体，无需 `allow`；扩展为第三方依赖时不能仅凭 root 信任执行它们。
- 生成器 stdout 是日志，不是 shell 命令或任意编译参数接口。`build.gox` 是研究名称，本例使用现有 `.hgo` 扩展名。

# 显式 workspace 示例

根 `goxide.toml` 列出三个成员。只有两个应用的 `shared={workspace=true}` 建立依赖；成员资格不产生 `use` 绑定，也不覆盖同名远端依赖。共享模板的 `package` 明确指定 `shared` 的包入口；`fmt` 模板来自 Go toolchain，成员分别显式继承。根模板的 path 相对根清单，成员直接 path 相对成员清单。

在仓库根运行，`goxide` 为本分支编译的 CLI，Go 需 1.27+：

```sh
goxide run --bin web examples/workspace
goxide run -p example.org/worker --bin web examples/workspace
goxide run -p example.org/service --example hello examples/workspace
goxide build --workspace examples/workspace
goxide build --workspace --offline --locked examples/workspace
goxide test --workspace --offline --locked examples/workspace
```

前三条依次打印 `json`、`yaml`、`json`。同时 build 两个应用时共享库合并为 `json+yaml`，选择 `yaml` 分支；随后单独运行 service 恢复 `json`。两个成员都叫 `web`，产物按 module/target/config 分目录。service 的 `admin` 也是命名 bin；单独 run 必须选择 bin，example 不进入 workspace 默认批量构建。

`goxide.lock` 只写根目录；`target` 也由根所有权管理。成员不写锁。`--no-default-features` 只关闭本次所选根的默认 feature，显式依赖仍可请求默认 feature；多成员的 `--features` 使用完整 `module/feature`（例如给 shared 显式选择时 `-p example.org/shared --features json`）。

锁定全体成员的版本政策与全部可声明 feature 闭包；编译仅所选成员及其活动依赖，不编译未选成员源码或运行其脚本。首次锁定可能下载未选成员的依赖，但下载不执行脚本。

复制根锁到拆出的独立项目可以复用可达远端/Git pin，不读取无关记录。这个示例使用本地 path 与继承字段，拆出前必须显式物化继承并安排本地依赖目录；复制锁不会打包 path 源码，也不会自动转换为远端。当前核心范围和限制见 [RFC 0026](../../docs/rfcs/0026-workspaces.md)。

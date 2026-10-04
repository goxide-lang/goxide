# 项目 target 产物布局

本说明对应 RFC 0022 的可选 hgo.json 项目模式。布局实现位于 `internal/project/layout.go`，CLI 集成位于 `cmd/hgo/project.go`；无 manifest 的传统命令保持原输出规则。不是 Cargo 的完整 target/cache 协议。

## 选择与路径

`--target-dir` > `HGO_TARGET_DIR` > `target`。相对路径相对于规范化后的 manifest 根，不受启动 cwd 影响。无需新增 manifest 字段。默认受管源码内输出是显式例外，frontend 只有验证所有权标记后才允许它。

```text
target/
  .hgo-layout.json
  .layout-lock/
  units/<host>/<target>/<kind>/<name>/<config>/
    .hgo-unit.json
    runs/build-<nonce>/
      .hgo-run.json
      bundle/module/
      bundle/deps/<module-hash>/
      bundle/main-anchor/go.mod  # 原生 module 回边的政策锚，不含根包源码
      intermediate/
      script-out/                 # 只预留路径，布局不创建/执行脚本
    script-cache/                 # 只预留路径，布局不定义脚本缓存键
  scripts/<host>/<target>/<package-id>/
    .hgo-unit.json              # 包级脚本缓存容器，跨 bin/example 共享
    script-cache/              # runner 所有权标记及 entries/<完整输入键>
  bin/<host>/<target>/<kind>/<name>/<config>/
    .hgo-unit.json
    <name>[.exe]
    .hgo-binary.json
```

host/target 为 GOOS-GOARCH；kind 为 lib/bin/example。config 包含整个已解析 hgo 图各节点的 module/root/go.mod/manifest/feature、激活 Git commit、tags、目标入口与种类、编译器内容摘要、Go 目标环境选项、共享选版/来源计划以及 test 的 race 模式。**它是配置身份，不是源码内容缓存键。** 源码修改可复用同一最终二进制路径，但每次生成新 run；不会据 config 跳过转译。build/run/transpile 的相同普通配置可共享 unit，test 配置隔离。

本次继续明确拒绝非宿主 GOOS/GOARCH；只有路径分区并不解决前端跨平台类型尺寸问题。不可把目录字段视为已实现交叉编译。

## 构建与保留

`hgo build --bin web ./app` 发布到受管 bin 并打印路径。`hgo transpile --bin web ./app` 打印新 bundle/module 路径。run/test 保持程序/测试输出。lib build 检查库，不发布可执行文件。显式 `-o` 必须是源码和任何受管目录之外的全新路径；transpile 支持创建缺少的父目录。

每个 bundle 的 module 与 deps 一起保留，replace 指向该次绝对路径。后续构建不会改变旧 bundle；原位置可直接使用 Go 构建，移动后须重新生成。source maps 指向原始源码。目录入口保留相对路径及 embed 资源；文件入口仍只映射单文件，web 等资源项目应使用目录入口。

最终二进制仅在所有权和上次记录的 SHA-256/模式均匹配时覆盖；用户修改或没有记录的文件明确拒绝。发布通过临时文件 rename；若崩溃导致二进制和记录不一致，下次拒绝，不自动恢复或覆盖。失败运行也可能保留诊断用生成目录；frontend 可删除本次新建的不完整 module。

只有 `hgo scripts --action clean` 清理当前所选 target 根下的脚本结果缓存；没有全项目 clean、GC 或旧 run 删除命令；重复构建会增长磁盘占用。用户确认无构建运行后可自行处理自己的产物目录；工具不会递归清空旧目录、陌生文件或依赖缓存。

## 所有权与扫描

首次只认领不存在的目标根；已有目录即使为空，若无匹配的 `.hgo-layout.json` 也拒绝。标记包含格式版本、规范化项目根、module path 和产物根路径。不同项目不能共用一个目标根；可各自选择共享父目录下的不同子目录。搬移或复制目录后的旧标记不能自动取得新位置所有权。

拒绝目标等于/包含源码根、与依赖源重叠、使用 vendor/.git/.hgo 路径组件，或路径任何已有组件为 symlink。源码遍历验证标记后跳过所有本项目受管目录，包括此前使用的自定义路径；不因为普通目录叫 target 就忽略它。损坏或别的项目标记会报错。显式 -o 和新布局根均检查所有祖先标记，禁止布局嵌套，不能绕过另一布局的所有权/互斥。

`.hgo/build.lock` 继续保护源码锁写入，`.layout-lock` 保护整个产物根；冲突立即拒绝，不抢占或自动移除遗留 lease。崩溃后先确认无构建再人工移除锁目录。所有权保护误操作，不是抵抗同一用户同时恶意修改文件系统的安全沙箱。

## 与依赖、Go 缓存及脚本的界面

`hgo.lock` 仍在源码根；Git checkout 仍在 `.hgo/git`；Go module/build cache 仍由 Go 的 GOMODCACHE/GOCACHE 管理。target-dir 不迁移或删除这些缓存。最终 Go 命令的 GOTMPDIR 指向当次 intermediate；Go 自行管理其短期文件。离线/锁定策略沿用 RFC 0022。

runner 使用 `project.OpenLayout(root, module, targetDir)` 持有 lease，再调用 `Layout.Begin(UnitSpec)`。返回 `Unit` 的 Directory、Run、Bundle、Intermediate、ScriptOut、ScriptCache、BinaryDirectory。Close 只释放 lease，不清理产物。runner 必须自行定义脚本输出所有权与完整缓存键；不能仅凭 config 认定脚本结果新鲜。脚本信任授权必须存放于产物根之外，不得放在可清理、可版本控制的 target 或所有权标记中。布局模块本身不执行脚本、不提供信任决定、不触发安装脚本。联合runner见 [RFC 0023](../rfcs/0023-build-scripts.md)。

为保持包级生成结果跨bin共享，联合runner使用 `Layout.ScriptCache` 创建独立scripts空间，调用 `ScriptCachePath` 计算同一路径；沿用所有权标记和layout lease，不复用逐target Unit.ScriptCache。host/target/package决定容器位置，features/env/输入/工具等由runner完整缓存键分区。`CleanScriptCaches` 先验证全部脚本布局结构，再调用runner逐cache所有权检查与清理，保留其他units/bin、信任、源码和依赖缓存。Unit.ScriptOut尚未用于执行暂存，生成器使用短期隔离目录，成功字节保存在cache及当前bundle；这里的隔离目录不是OS sandbox。

## 验证入口

`internal/project/layout_test.go` 覆盖所有权、symlink、重复 lease、陌生 unit/bin、修改文件拒绝与保留。`cmd/hgo/layout_test.go` 使用真实 Go 临时多模块，验证默认输出/配置切换、多 bin/example/web embed、重复与离线构建、保留 bundle 的旧依赖可重复构建、源码映射、CLI/env 优先级、跨项目 -o 拒绝及跨平台明确拒绝。其余 feature/Git/锁定回归见 [RFC 0022](../rfcs/0022-project-toolchain.md)。

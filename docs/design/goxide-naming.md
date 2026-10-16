# Goxide 命名迁移清单

## 分类和风险

| 分类 | 处置 | 风险与验证 |
| --- | --- | --- |
| 当前 CLI/扩展名/文档/示例/帮助/补全 | 统一 goxide/.gox；展示 Goxide | 真实进程、输出路径、诊断、混合源 |
| 宏/cfg/发现/依赖/build scripts | 所有源扫描使用 .gox，拒绝 .hgo | 同 stem 冲突必须在写出前拒绝 |
| WASM/editor/playground/scripts | 编译、样例、bridge、缓存和构建 tag 同步 | 浏览器与 CLI 输出一致 |
| 生成公开符号及内部标识 | Goxide*/__goxide*，升级 metadata 和 sum ABI | 跨模块消费者与 std 同步重建 |
| 缓存/布局/trust | 新名称；旧产物不复用 | 不删除用户数据；要求清理旧 target、重新授权脚本 |
| goxide.toml/goxide.lock、module path、业务 schema | 原格式和身份不变 | 不机械改写锁或用户数据 |
| 历史文档、冻结基线、旧格式拒绝测试 | 有事实依据保留 hgo/hyper-go | 历史记录不是当前操作教程 |

## 执行待办

- [x] 核验 compiler 基线 27d2e274，建立 feat/goxide-naming。
- [x] 正式命名 RFC 和分类清单。
- [x] 核心 CLI/生成/发现/脚本与 pre-1.0 ABI 迁移。
- [x] 当前文档/示例/测试/工具一致性迁移。
- [x] 最终旧命名审计、独立审查与全部适用检查。
- [x] 提交、远端核验、官网 owner 接入说明。

官网源码提交 51405a6982e45167c732fb01906e2b666b39fcdd 位于 goxide-lang/goxide（当时为 hxsf/goxide；不是独立官网仓库）；本分支不部署。源码整合须保留该提交以来的官网功能，不能用较旧 compiler 基线下的 website 目录覆盖 owner 的最新网站。

## 用户迁移顺序

1. 将作者源码改为 `.gox`，构建脚本显式 entry 改为 `tools/build/build.gox` 等独立目录路径；同步 `//goxide:cfg`。不要只改文件名而遗留旧 directive。
2. 安装 `cmd/goxide`。移除旧 `HGO_*` 环境变量，使用 `GOXIDE_TARGET_DIR`、`GOXIDE_TRUST_DIR`；flag > 新 env > 默认 target。空值旧变量也拒绝。
3. 更新 std 至命名分支提交 `ce5bccb4ff9c7fb96b7f192a3a263051590d9288` 对应版本。serde 不变。按依赖顺序重新生成所有生产者和消费者，包括 Go 调用端的生成符号引用。
4. 使用新的 target 目录，或备份后自行清理旧受管产物；旧 `.hgo*` metadata 不得直接改名使用。重新审核脚本信任；不会自动迁移或删除旧用户数据。
5. 正常重新解析锁并检查差异。示例锁由真实 CLI 更新并保留旧记录；新旧版本校验和同时出现是锁历史，不是旧编译 ABI 兼容。

## 生成协议和跨仓库合同

| 项目 | 新值 |
| --- | --- |
| enum metadata | schema 8；GoxideEnumMetadata；.goxide.enums.json |
| reference metadata | schema 3；GoxideReferenceMetadata；.goxide.refs.json |
| use metadata | schema 2；GoxideUseMetadata；.goxide.uses.json |
| std sum 表示 | GoxideSumABI = 2；同一正式 std module path |
| 生成符号 | GoxideCase / GoxideNew / GoxideInst 等；内部 __goxide |
| 缓存与受管标记 | .goxide、.goxide-layout/unit/run/binary.json；旧缓存不复用 |
| 官网适配 | cmd/goxide-playground、goxideCompile、goxideweb build tag、.gox 示例 |

历史允许项：冻结基线文档、评审/验收记录、原始研究 results，以及 `tests/research/shared_std_candidate` / `serde_candidate` 的发布前快照及 provenance 均保留当时事实。当前文档链接可指向改名后的文件；历史命令并非当前教程。旧输入拒绝字符串与负例必须保留，以防回归。

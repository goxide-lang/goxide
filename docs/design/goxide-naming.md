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
- [ ] 核心 CLI/生成/发现/脚本与 pre-1.0 ABI 迁移。
- [ ] 当前文档/示例/测试/工具一致性迁移。
- [ ] 最终旧命名审计、独立审查与全部适用检查。
- [ ] 提交、远端核验、官网 owner 接入说明。

官网源码提交 51405a6982e45167c732fb01906e2b666b39fcdd 位于 hxsf/goxide（不是独立官网仓库）；本分支不部署。源码整合须保留该提交以来的官网功能，不能用较旧 compiler 基线下的 website 目录覆盖 owner 的最新网站。

# Cargo 对照独立审查

2026-10-04，独立 agent `adversarial_review` 只读审查 [逐项对照](build-gox-cargo-gap.md)，并查询当前官方 Cargo build-scripts、specifying-dependencies、config、changelog。未修改生产文件，未运行 Cargo 试验，也未安装 Rust。

| 严重性 | 发现 | 修订 |
| --- | --- | --- |
| 中 | 新增量建议流程把授权放快照前、epoch 复查放提交后；命中绑定也没有明确再次检查 | 改为完整执行闭包快照→subject/key→许可；命中绑定前查 epoch；miss 验证后提交前、绑定前复查；策略更新与发布/绑定需锁或 compare-and-commit，保留原 RFC 已写明的研究边界 |
| 低 | “新增依赖脚本”可能被误读为不包括没有 script 的恶意 helper/init | 明确第三方 host 执行闭包涵盖普通 helper 源码/init、外部工具内容，不因 root 项目信任自动取得许可 |
| 低 | project 信任的身份与后续更新范围不明确 | 明确 canonical module 路径+仓库身份+本地批准；manifest 无权扩大；接受后续 root 源码更新是主动选择项目级信任的取舍 |

reviewer 对 OUT_DIR 持久、host 条件 build-dependencies、cfg 不启用依赖、links/直接 DEP/override、MSRV、warning 可见性与 build.warnings、运行次数的保守措辞，未发现 Cargo 机制硬错误。本文没有把 root 信任/declared 增量当成已批准生产行为，也没有把 offline/取消当 sandbox。

作者补充核对：新项目工具链 `fa1461f` 的 manifest v1 拒绝未知字段；Graph/Node 当前没有 build-kind；生成入口和配置标记存在，但无 script runner。原生 build-list 一致性拒绝仍保留。已将这些接口限制补入对照文档。

最终校验：仓库 Markdown/link/RFC 检查与 git diff 检查。对照中的 Cargo 行为依据官方规范而非本机执行；本机 cargo/rustc/rustup 均不在 PATH，常见目录也未发现 toolchain。新修订建议仍待用户确认。

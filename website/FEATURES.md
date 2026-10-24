# 网站 RFC 特性状态索引

编译器基线：`a82bb80b54127dc670321536e44ad5009d6f1a9f`；证据复核日期：2026-10-23。

由 `website/catalog-source.json` 生成。状态是限定范围的人工判断；构建检查引用、源码版本、示例输出与拒绝例。此索引保留私有证据，公开构建仅导出摘要。

| 特性 | RFC | 状态 | 当前入口 | 实现/测试/设计证据（基线或注明的开发快照） |
| --- | --- | --- | --- | --- |
| 声明宏：模式与重复 | 0011 | 部分实现 | browser | `tests/acceptance/macro_packages_test.go` (cfb125531175); `docs/rfcs/0011-macros.md` (3c644d670136); `tests/acceptance/macro_fragment_categories_test.go` (ee628a75e239) |
| 封闭 enum 与明确状态 | 0008, 0015 | 部分实现 | browser | `tests/acceptance/enums_test.go` (b53640dbde79); `tests/acceptance/enum_external_test.go` (c1114f3e2069); `tests/acceptance/enum_payload_borrow_test.go` (375d874a8680) |
| match、guard 与表达式 | 0009 | 部分实现 | browser | `tests/acceptance/match_call_signatures_test.go` (c0c6dea31eaa); `tests/acceptance/match_named_dynamic_test.go` (a0451f40dbb3); `tests/acceptance/match_unary_composite_test.go` (c2cf4037f43e); `docs/rfcs/0009-match.md` (0bbaef749fbe) |
| Option / Result 与 ? | 0008, research/0005, 0032 | 部分实现 | browser | `tests/acceptance/propagate_test.go` (9b2ef3f3b85f); `tests/acceptance/propagate_values_test.go` (b8e69667b82a) |
| Go 返回值适配：静态 T/E 推导 | research/0005 | 已实现并验证 | browser | `tests/acceptance/go_result_inference_test.go` (55def7b4aa19) |
| PartialEq / Eq 与 Clone | 0008, 0013 | 部分实现 | browser | `tests/acceptance/equality_test.go` (9795b799ce20); `tests/acceptance/manual_equality_test.go` (6e557d42c15c); `tests/acceptance/macros_test.go` (2eb1ff91d8d6) |
| Default、Debug 与格式宏 | 0012, 0013 | 部分实现 | browser | `tests/acceptance/manual_default_test.go` (92643286ab09); `tests/acceptance/print_default_test.go` (e4f2504e90ac) |
| serde 核心协议与 JSON | 0013, research/0006 | 部分实现 | browser | `tests/acceptance/option_serde_test.go` (307f2282d19a); `tests/acceptance/serde_defaults_test.go` (b1d5df6af50f); `docs/serde-json.md` (a55217213af4) |
| Error 派生与错误链 | 0013, research/0005 | 部分实现 | browser | `tests/acceptance/serde_causes_test.go` (372778994a1a); `tests/acceptance/macros_test.go` (2eb1ff91d8d6) |
| use / pub use 与 API 聚合 | 0021 | 部分实现 | cli | `tests/acceptance/qualified_packages_test.go` (13115d24c2c2); `examples/reexports/README.md` (67e1513bf1e1) |
| 多包、feature 与编译目标 | 0001, 0022, 0025 | 部分实现 | cli | `internal/project/manifest.go` (3a7e476ca4f1); `internal/project/planner.go` (f2ebaedd3f49); `cmd/goxide/project_test.go` (1a70fd20e71c); `cmd/goxide/version_test.go` (891f8b0a6e38); `cmd/goxide/combination_review_test.go` (44a4bea56e38); `docs/rfcs/0025-unified-project.md` (63bafab76210); `cmd/goxide/import_go_test.go` (7f13a2b51e19); `cmd/goxide/single_file.go` (e4224765c260); `tests/acceptance/single_file_external_test.go` (61513ccd8973) |
| 从普通 Go 迁入：plan → 新目录 | 0025 | MVP 阶段恢复 | unavailable | `docs/rfcs/0025-unified-project.md` (63bafab76210); `cmd/goxide/import_go.go` (726a93405de6); `cmd/goxide/import_go_test.go` (7f13a2b51e19) |
| 宿主构建脚本与授权 | 0023 | MVP 阶段恢复 | unavailable | `internal/scripttrust/store.go` (1b8255621bb4); `cmd/goxide/scripts.go` (148c6ee0d37f); `cmd/goxide/buildscript_test.go` (81afc6dd298d); `cmd/goxide/combination_review_test.go` (44a4bea56e38) |
| 只读路径与写引用 | 0005, 0006 | 部分实现 | browser | `tests/acceptance/refs_test.go` (4c4d47ea744d); `tests/acceptance/composite_permissions_test.go` (8d534def4950) |
| 别名与借用风险分析 | 0005 | 已确认待实现 | unavailable | `docs/rfcs/0005-borrows.md` (f8d6395d5239) |
| 静态协议、关联函数与 impl | 0002, 0003, 0004, 0019, 0020, 0031 | 部分实现 | browser | `docs/static-protocols.md` (6e7409874de4); `tests/acceptance/static_protocol_test.go` (316640cd1637) |
| safe std、锁与集合 | 0006, 0007, 0010, 0016, 0017 | 已确认待实现 | unavailable | `docs/rfcs/0007-locks.md` (3a27dc2b760e); `docs/rfcs/0010-collections.md` (4227ef865b5f); `docs/rfcs/0016-safe-std.md` (1226aafb44c7); `docs/rfcs/0017-maps.md` (632d10a19c9a) |
| quote / syn 与过程宏 | 0014, 0030 | 部分实现 | browser | `docs/rfcs/0014-syntax-tools.md` (977cadd5ef80); `docs/rfcs/0030-procedural-macros.md` (16792bf27212); `internal/compile/proc_build_test.go` (a28671f3c38c); `internal/compile/proc_attribute_scope_test.go` (e2da6841c6aa); `internal/lower/proc_snapshot_e2e_test.go` (c987c5e5d484); `cmd/goxide/proc_macro_test.go` (195eb0c1cac6) |
| 降低约束与公开边界 | 0015, 0018, 0027 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (1124d35f2782); `docs/rfcs/0018-lowering-contract.md` (5d756137f2ad) |
| 显式依赖根与静态路径 | 0024, 0028 | 部分实现 | browser | `internal/paths/paths.go` (849ffb9f4799); `internal/rootconfig/roots.go` (a6fefb11ad44); `cmd/goxide-playground/main.go` (5b88d9027925) |
| 分仓标准库与编译器支持 | 0008, 0016 | 部分实现 | browser | `internal/lower/enum_packages_test.go` (f56e7533af81); `tests/acceptance/sum_clone_debug_test.go` (597c31969ab0); `tests/acceptance/option_serde_test.go` (307f2282d19a) |
| 上下文推导与生成 Go 可读性 | 0008, 0018 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (1124d35f2782); `tests/acceptance/go_result_inference_test.go` (55def7b4aa19); `internal/lower/enum_packages_test.go` (f56e7533af81) |
| CLI：明确的长短选项与参数边界 | 0022, 0023 | 部分实现 | cli | `cmd/goxide/cli_process_test.go` (af95ae5c25e0); `cmd/goxide/cli_dependency_graph_test.go` (a821704cb6b9) |
| 供应模块：local 默认与显式 remote 选择 | 0025 | MVP 阶段恢复 | unavailable | `cmd/goxide/vendor_import_test.go` (a1c47a52a068); `cmd/goxide/import_sources_test.go` (6ad3c0af55ff); `cmd/goxide/import_sources_process_test.go` (a65d2b0ccc05); `internal/project/supplied_sources.go` (4f367114e774) |
| Workspace：作者核心与统一根锁 | 0026 | MVP 阶段恢复 | unavailable | `docs/rfcs/0026-workspaces.md` (5514a7179db8); `cmd/goxide/workspace_test.go` (59cbf580d3f9); `internal/project/workspace_test.go` (0a282cee0ea3) |
| go.work 迁入与本地模块渐进增强 | 0026 | MVP 阶段恢复 | unavailable | `docs/design/workspace-import.md` (f2569eaddf22); `cmd/goxide/native_enhanced_test.go` (e5b9fc5df815); `cmd/goxide/native_standalone_test.go` (b6dd60b4aff0) |

开发快照：{}

管理建议：规范留在编译器仓库随版本评审；Issue 讨论/跟踪，PR 变更规范与实现；公共网站只发布审核后的摘要。未创建新仓库或 Issue。

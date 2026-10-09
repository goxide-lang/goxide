# 网站 RFC 特性状态索引

编译器基线：`db78da5768fa24e7679971762f75116b0df8974d`；证据复核日期：2026-10-09。

由 `website/catalog-source.json` 生成。状态是限定范围的人工判断；构建检查引用、源码版本、示例输出与拒绝例。此索引保留私有证据，公开构建仅导出摘要。

| 特性 | RFC | 状态 | 当前入口 | 实现/测试/设计证据（基线或注明的开发快照） |
| --- | --- | --- | --- | --- |
| 声明宏：模式与重复 | 0011 | 部分实现 | browser | `internal/macros/macros_test.go` (8ae11f2e22ab); `internal/macros/primitive_bindings_test.go` (a0ee917ba922); `tests/acceptance/macro_packages_test.go` (478e3a9da1d1); `docs/rfcs/0011-macros.md` (92e24451181f); `internal/macros/fragment_categories_test.go` (6296785c2a76); `tests/acceptance/macro_fragment_categories_test.go` (2a6b054a7871); `internal/macros/primitive_aliases_test.go` (64fbcf5726b0) |
| 封闭 enum 与明确状态 | 0008, 0015 | 部分实现 | browser | `internal/enums/lower.go` (37d7b9758299); `tests/acceptance/enums_test.go` (65bc4af51e3b); `tests/acceptance/enum_external_test.go` (54b952d14d39); `internal/frontend/enum_borrow_test.go` (917e7197dfd0) |
| match、guard 与表达式 | 0009 | 部分实现 | browser | `internal/matchexpr/lower.go` (b734739fd8f5); `tests/acceptance/match_call_signatures_test.go` (44d99676205b); `tests/acceptance/match_named_dynamic_test.go` (db6ad3dac07e); `internal/matchexpr/composite.go` (c8da66741e0d); `internal/propagate/match_literals.go` (0e9dd27f2fa4); `tests/acceptance/match_unary_composite_test.go` (12b82f05be20); `docs/rfcs/0009-match.md` (4433286cf7cb) |
| Option / Result 与 ? | 0008, research/0005 | 部分实现 | browser | `internal/propagate/lower.go` (853ad58ba1cc); `tests/acceptance/propagate_test.go` (34966d072ad3); `tests/acceptance/propagate_values_test.go` (b8e69667b82a); `internal/propagate/sum_inference.go` (c1f3a0d83660); `internal/frontend/shared_std_test.go` (fe6c342daf4a); `internal/frontend/explicit_std_test.go` (99bb19bb7dc1) |
| Go 返回值适配：静态 T/E 推导 | research/0005 | 已实现并验证 | browser | `internal/propagate/go_result.go` (8523249ea700); `tests/acceptance/go_result_inference_test.go` (be1abab13732) |
| PartialEq / Eq 与 Clone | 0008, 0013 | 部分实现 | browser | `internal/equality/lower.go` (f1d60dcc6bb7); `tests/acceptance/equality_test.go` (e453f3449854); `tests/acceptance/manual_equality_test.go` (709d1cdae896); `tests/acceptance/macros_test.go` (de90c1efd468); `internal/frontend/derive_container_bindings_test.go` (628fe056ba37) |
| Default、Debug 与格式宏 | 0012, 0013 | 部分实现 | browser | `internal/printing/lower.go` (b82c6e43bfb6); `internal/deriveextras/generate.go` (b93746d914d6); `tests/acceptance/manual_default_test.go` (6e04aad61f5d); `tests/acceptance/print_default_test.go` (e0c6d4d22a03); `internal/frontend/derive_container_bindings_test.go` (628fe056ba37) |
| serde 核心协议与 JSON | 0013, research/0006 | 部分实现 | browser | `tests/acceptance/option_serde_test.go` (3108bb35522c); `tests/acceptance/serde_defaults_test.go` (5293aed34336); `docs/serde-json.md` (27e4419ff1eb); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5); `internal/derives/registry.go` (0c476cb0dc51); `internal/frontend/derive_bindings_test.go` (d217bcbc18cd) |
| Error 派生与错误链 | 0013, research/0005 | 部分实现 | browser | `internal/deriveextras/error.go` (c8d5b2761b2e); `tests/acceptance/serde_causes_test.go` (372778994a1a); `tests/acceptance/macros_test.go` (de90c1efd468) |
| use / pub use 与 API 聚合 | 0021 | 部分实现 | cli | `cmd/goxide/directory.go` (b1ac56bc8e23); `tests/acceptance/qualified_packages_test.go` (0204440e9456); `examples/reexports/README.md` (67e1513bf1e1) |
| feature、依赖来源与 targets | 0001, 0022, 0025 | 部分实现 | cli | `internal/project/manifest.go` (4935361ceb2d); `internal/project/planner.go` (8615144bc19a); `cmd/goxide/project_test.go` (886a8b3a9e75); `cmd/goxide/version_test.go` (c1a0676e4c53); `cmd/goxide/combination_review_test.go` (0abc91d28a16); `docs/rfcs/0025-unified-project.md` (0f5d4ad1e494); `cmd/goxide/import_go_test.go` (1ed0248fd8c7); `internal/frontend/single_file.go` (d6bf7ef13f1a); `cmd/goxide/single_file.go` (07ef14971334); `tests/acceptance/single_file_external_test.go` (e59741f19fc7) |
| 从普通 Go 迁入：plan → 新目录 | 0025 | 部分实现 | cli | `docs/rfcs/0025-unified-project.md` (0f5d4ad1e494); `cmd/goxide/import_go.go` (5f156b6e5432); `cmd/goxide/import_go_test.go` (1ed0248fd8c7) |
| 宿主构建脚本与授权 | 0023 | 部分实现 | cli | `internal/scripttrust/store.go` (d521e32ece88); `cmd/goxide/scripts.go` (abc3ee116700); `cmd/goxide/buildscript_test.go` (2aee22398839); `cmd/goxide/combination_review_test.go` (0abc91d28a16) |
| 只读路径与写引用 | 0005, 0006 | 部分实现 | browser | `internal/refs/lower.go` (06958945a842); `internal/refs/flow.go` (8a7482599404); `tests/acceptance/refs_test.go` (23bf6da5dac2); `tests/acceptance/composite_permissions_test.go` (88fe8991473a); `internal/frontend/enum_borrow_test.go` (917e7197dfd0); `internal/refs/projections_test.go` (715e190fee83) |
| 别名与借用风险分析 | 0005 | 已确认待实现 | unavailable | `docs/rfcs/0005-borrows.md` (6ae0fbfb0b96); `ROADMAP.md` (39ae0da9312a) |
| trait、关联类型与 impl | 0002, 0003, 0004, 0019, 0020 | 已确认待实现 | unavailable | `docs/rfcs/0019-uniform-trait-wrappers.md` (1df569cda8f2); `docs/rfcs/0020-associated-functions.md` (003d302b642a); `ROADMAP.md` (39ae0da9312a) |
| safe std、锁与集合 | 0006, 0007, 0010, 0016, 0017 | 已确认待实现 | unavailable | `docs/rfcs/0007-locks.md` (3a27dc2b760e); `docs/rfcs/0010-collections.md` (4227ef865b5f); `docs/rfcs/0016-safe-std.md` (1226aafb44c7); `docs/rfcs/0017-maps.md` (632d10a19c9a) |
| quote / syn 与过程宏 | 0014 | 已确认待实现 | unavailable | `docs/rfcs/0014-syntax-tools.md` (977cadd5ef80) |
| 降低约束与公开边界 | 0015, 0018 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (edf7edba31f6); `tests/acceptance/source_mapping_test.go` (8b4f7794d1e5); `docs/rfcs/0018-lowering-contract.md` (abfe6d012fbf) |
| 显式依赖根与静态路径 | 0024 | 部分实现 | browser | `internal/paths/paths.go` (372570a90ffc); `internal/rootconfig/roots.go` (06cd24c6c0f1); `internal/frontend/static_paths_test.go` (2f3377a6056c); `internal/frontend/deferred_use_test.go` (05852da09703); `cmd/goxide-playground/main.go` (4b4a80a3e8ab) |
| 分仓标准库与共享 sum | 0008, 0016 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (fe6c342daf4a); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5); `docs/reviews/public-library-migration-20261004.md` (5c4089eda830) |
| 上下文推导与生成 Go 可读性 | 0008, 0018 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (fe6c342daf4a); `internal/frontend/readability_test.go` (4a76f296f046); `internal/enums/builtins_test.go` (56c0c64be628) |
| CLI：明确的长短选项与参数边界 | 0022, 0023 | 部分实现 | cli | `docs/reviews/cobra-cli-20261004.md` (8ea6aedbfb22); `cmd/goxide/cli_process_test.go` (5e824179e254); `cmd/goxide/cli_dependency_graph_test.go` (50a8779ca7d2) |
| 供应模块：local 默认与显式 remote 选择 | 0025 | 部分实现 | cli | `docs/reviews/vendor-import-20261004.md` (e34aa299b447); `cmd/goxide/vendor_import_test.go` (bfe8c6b79782); `cmd/goxide/import_sources_test.go` (e47dab18e690); `cmd/goxide/import_sources_process_test.go` (8c56c8aeb536); `internal/project/supplied_sources.go` (72144f6a51ac); `docs/reviews/source-selection-20261005.md` (10a95c11af1a) |
| Workspace：作者核心与统一根锁 | 0026 | 部分实现 | cli | `docs/rfcs/0026-workspaces.md` (e3abc1196e95); `docs/reviews/workspace-core-20261004.md` (cd447ed4f6a6); `cmd/goxide/workspace_test.go` (c77c1683d71a); `internal/project/workspace_test.go` (7856d06c586f) |
| go.work 迁入与本地模块渐进增强 | 0026 | 部分实现 | cli | `docs/design/workspace-import.md` (d2357bd79f3c); `docs/reviews/workspace-import-20261004.md` (62a5cfb20f03); `cmd/goxide/native_enhanced_test.go` (c70c35b5fda9); `cmd/goxide/native_standalone_test.go` (afc6388b3197); `docs/reviews/vendor-local-modules-20261004.md` (8a12349f3d78) |

开发快照：{"use-path": "5e4b118f25262d8022d1b8f72575d89d69f096aa"}

管理建议：规范留在编译器仓库随版本评审；Issue 讨论/跟踪，PR 变更规范与实现；公共网站只发布审核后的摘要。未创建新仓库或 Issue。

提案原文快照 `@use-path:docs/rfcs/0024-use-paths.md`：`website/evidence/0024-use-paths-5e4b118.txt`；来源 `5e4b118f25262d8022d1b8f72575d89d69f096aa:docs/rfcs/0024-use-paths.md`，SHA-256 `9d3a71b5d773fb3c7b721c7c61bc9e2ec20e6a9773be53b8ea1b66cfbf2a3af9`，Git blob `9e386b2155290b6d62ce06cead194e8b4ad10ee5`。仅供仓内审核，不导出到公开清单。

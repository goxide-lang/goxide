# 网站 RFC 特性状态索引

编译器基线：`05a53b907e7930b23d2c80015135dff0f61a7a44`；证据复核日期：2026-10-09。

由 `website/catalog-source.json` 生成。状态是限定范围的人工判断；构建检查引用、源码版本、示例输出与拒绝例。此索引保留私有证据，公开构建仅导出摘要。

| 特性 | RFC | 状态 | 当前入口 | 实现/测试/设计证据（基线或注明的开发快照） |
| --- | --- | --- | --- | --- |
| 声明宏：模式与重复 | 0011 | 部分实现 | browser | `internal/macros/macros_test.go` (ac3882cf4736); `internal/macros/primitive_bindings_test.go` (efff49717468); `tests/acceptance/macro_packages_test.go` (33c88acb78d8); `docs/rfcs/0011-macros.md` (92e24451181f); `internal/macros/fragment_categories_test.go` (6296785c2a76); `tests/acceptance/macro_fragment_categories_test.go` (0b11b9bf0d0f); `internal/macros/primitive_aliases_test.go` (0fde220258ea) |
| 封闭 enum 与明确状态 | 0008, 0015 | 部分实现 | browser | `internal/enums/lower.go` (7d08b4daf565); `tests/acceptance/enums_test.go` (96a4c4f24bd3); `tests/acceptance/enum_external_test.go` (6d7e7dcf01af) |
| match、guard 与表达式 | 0009 | 部分实现 | browser | `internal/matchexpr/lower.go` (a37c0040a656); `tests/acceptance/match_call_signatures_test.go` (de8c8aad608b); `tests/acceptance/match_named_dynamic_test.go` (e255d2344717); `internal/matchexpr/composite.go` (0208a27e7dbc); `internal/propagate/match_literals.go` (0e9dd27f2fa4); `tests/acceptance/match_unary_composite_test.go` (3c23bc266332); `docs/rfcs/0009-match.md` (637daf1db37d) |
| Option / Result 与 ? | 0008, research/0005 | 部分实现 | browser | `internal/propagate/lower.go` (701ceb6b98a0); `tests/acceptance/propagate_test.go` (bf545583d370); `tests/acceptance/propagate_values_test.go` (4d0b2d61d4e1); `internal/propagate/sum_inference.go` (5b9a34aff90d); `internal/frontend/shared_std_test.go` (a2cb837c392e); `internal/frontend/explicit_std_test.go` (af714c812a63) |
| Go 返回值适配：静态 T/E 推导 | research/0005 | 已实现并验证 | browser | `internal/propagate/go_result.go` (421c5cdd4406); `tests/acceptance/go_result_inference_test.go` (5b636349d061) |
| PartialEq / Eq 与 Clone | 0008, 0013 | 部分实现 | browser | `internal/equality/lower.go` (4467021df52e); `tests/acceptance/equality_test.go` (ba623fb5168e); `tests/acceptance/manual_equality_test.go` (ad14d9b199a0); `tests/acceptance/macros_test.go` (7227a317a3f6); `internal/frontend/derive_container_bindings_test.go` (cd9c502e9031) |
| Default、Debug 与格式宏 | 0012, 0013 | 部分实现 | browser | `internal/printing/lower.go` (b82c6e43bfb6); `internal/deriveextras/generate.go` (d09862ca6184); `tests/acceptance/manual_default_test.go` (13f14a9b529e); `tests/acceptance/print_default_test.go` (c154ae9cecd2); `internal/frontend/derive_container_bindings_test.go` (cd9c502e9031) |
| serde 核心协议与 JSON | 0013, research/0006 | 部分实现 | browser | `tests/acceptance/option_serde_test.go` (4d71bbdc7381); `tests/acceptance/serde_defaults_test.go` (03d817ca4697); `docs/serde-json.md` (7709e62022f8); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5) |
| Error 派生与错误链 | 0013, research/0005 | 部分实现 | browser | `internal/deriveextras/error.go` (10ec1a9760c5); `tests/acceptance/serde_causes_test.go` (372778994a1a); `tests/acceptance/macros_test.go` (7227a317a3f6) |
| use / pub use 与 API 聚合 | 0021 | 部分实现 | cli | `cmd/goxide/directory.go` (62864a33b3e7); `tests/acceptance/qualified_packages_test.go` (e252e434e9d1); `examples/reexports/README.md` (31989fe04d79) |
| feature、依赖来源与 targets | 0001, 0022, 0025 | 部分实现 | cli | `internal/project/manifest.go` (52a54105b5cc); `internal/project/planner.go` (cfe35df6ccc7); `cmd/goxide/project_test.go` (17a914ab7271); `cmd/goxide/version_test.go` (1370c81d2b73); `cmd/goxide/combination_review_test.go` (3cda1e895a84); `docs/rfcs/0025-unified-project.md` (fd33440cc6e6); `cmd/goxide/import_go_test.go` (1ed0248fd8c7); `internal/frontend/single_file.go` (6db9e92840a5); `cmd/goxide/single_file.go` (07ef14971334); `tests/acceptance/single_file_external_test.go` (c0158a226d55) |
| 从普通 Go 迁入：plan → 新目录 | 0025 | 部分实现 | cli | `docs/rfcs/0025-unified-project.md` (fd33440cc6e6); `cmd/goxide/import_go.go` (5f156b6e5432); `cmd/goxide/import_go_test.go` (1ed0248fd8c7) |
| 宿主构建脚本与授权 | 0023 | 部分实现 | cli | `internal/scripttrust/store.go` (d521e32ece88); `cmd/goxide/scripts.go` (a4e9d6db91b5); `cmd/goxide/buildscript_test.go` (d4c86f8a2a1c); `cmd/goxide/combination_review_test.go` (3cda1e895a84) |
| 只读路径与写引用 | 0005, 0006 | 部分实现 | browser | `internal/refs/lower.go` (cb1797dfdf0a); `internal/refs/flow.go` (8a7482599404); `tests/acceptance/refs_test.go` (a4125cbb0615); `tests/acceptance/composite_permissions_test.go` (823a192be1cf) |
| 别名与借用风险分析 | 0005 | 已确认待实现 | unavailable | `docs/rfcs/0005-borrows.md` (449fd0d8d478); `ROADMAP.md` (39ae0da9312a) |
| trait、关联类型与 impl | 0002, 0003, 0004, 0019, 0020 | 已确认待实现 | unavailable | `docs/rfcs/0019-uniform-trait-wrappers.md` (1df569cda8f2); `docs/rfcs/0020-associated-functions.md` (003d302b642a); `ROADMAP.md` (39ae0da9312a) |
| safe std、锁与集合 | 0006, 0007, 0010, 0016, 0017 | 已确认待实现 | unavailable | `docs/rfcs/0007-locks.md` (ade41b5149bf); `docs/rfcs/0010-collections.md` (4227ef865b5f); `docs/rfcs/0016-safe-std.md` (06d218c8a138); `docs/rfcs/0017-maps.md` (7edff0be2346) |
| quote / syn 与过程宏 | 0014 | 已确认待实现 | unavailable | `docs/rfcs/0014-syntax-tools.md` (977cadd5ef80) |
| 降低约束与公开边界 | 0015, 0018 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (298633fff97a); `tests/acceptance/source_mapping_test.go` (7e583cd57250); `docs/rfcs/0018-lowering-contract.md` (abfe6d012fbf) |
| use 路径与依赖别名 | 0024 | 提案 | unavailable | `@use-path:docs/rfcs/0024-use-paths.md` (9d3a71b5d773) |
| 分仓标准库与共享 sum | 0008, 0016 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (a2cb837c392e); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5); `docs/reviews/public-library-migration-20261004.md` (5c4089eda830) |
| 上下文推导与生成 Go 可读性 | 0008, 0018 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (a2cb837c392e); `internal/frontend/readability_test.go` (4a76f296f046); `internal/enums/builtins_test.go` (df7d2414ab1b) |
| CLI：明确的长短选项与参数边界 | 0022, 0023 | 部分实现 | cli | `docs/reviews/cobra-cli-20261004.md` (8ea6aedbfb22); `cmd/goxide/cli_process_test.go` (7eb2a08d0445); `cmd/goxide/cli_dependency_graph_test.go` (fcca67ecb0e8) |
| 供应模块：local 默认与显式 remote 选择 | 0025 | 部分实现 | cli | `docs/reviews/vendor-import-20261004.md` (e34aa299b447); `cmd/goxide/vendor_import_test.go` (bfe8c6b79782); `cmd/goxide/import_sources_test.go` (d87982597c9d); `cmd/goxide/import_sources_process_test.go` (8c56c8aeb536); `internal/project/supplied_sources.go` (72144f6a51ac); `docs/reviews/source-selection-20261005.md` (10a95c11af1a) |
| Workspace：作者核心与统一根锁 | 0026 | 部分实现 | cli | `docs/rfcs/0026-workspaces.md` (e3abc1196e95); `docs/reviews/workspace-core-20261004.md` (cd447ed4f6a6); `cmd/goxide/workspace_test.go` (8f2753c45dd2); `internal/project/workspace_test.go` (7856d06c586f) |
| go.work 迁入与本地模块渐进增强 | 0026 | 部分实现 | cli | `docs/design/workspace-import.md` (d2357bd79f3c); `docs/reviews/workspace-import-20261004.md` (62a5cfb20f03); `cmd/goxide/native_enhanced_test.go` (411614571f2c); `cmd/goxide/native_standalone_test.go` (afc6388b3197); `docs/reviews/vendor-local-modules-20261004.md` (8a12349f3d78) |

开发快照：{"use-path": "5e4b118f25262d8022d1b8f72575d89d69f096aa"}

管理建议：规范留在编译器仓库随版本评审；Issue 讨论/跟踪，PR 变更规范与实现；公共网站只发布审核后的摘要。未创建新仓库或 Issue。

提案原文快照 `@use-path:docs/rfcs/0024-use-paths.md`：`website/evidence/0024-use-paths-5e4b118.txt`；来源 `5e4b118f25262d8022d1b8f72575d89d69f096aa:docs/rfcs/0024-use-paths.md`，SHA-256 `9d3a71b5d773fb3c7b721c7c61bc9e2ec20e6a9773be53b8ea1b66cfbf2a3af9`，Git blob `9e386b2155290b6d62ce06cead194e8b4ad10ee5`。仅供仓内审核，不导出到公开清单。

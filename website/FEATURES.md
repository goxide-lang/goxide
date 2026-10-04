# 网站 RFC 特性状态索引

编译器基线：`7894ddff18f4d5e84c9bba395b0e6a32b27859f9`；证据复核日期：2026-10-04。

由 `website/catalog-source.json` 生成。状态是限定范围的人工判断；构建检查引用、源码版本、示例输出与拒绝例。此索引保留私有证据，公开构建仅导出摘要。

| 特性 | RFC | 状态 | 当前入口 | 实现/测试/设计证据（基线或注明的开发快照） |
| --- | --- | --- | --- | --- |
| 声明宏：模式与重复 | 0011 | 部分实现 | browser | `internal/macros/macros_test.go` (39da3595d0fb); `internal/macros/primitive_bindings_test.go` (efff49717468); `tests/acceptance/macro_packages_test.go` (1ba9ea30ef9e) |
| 封闭 enum 与明确状态 | 0008, 0015 | 部分实现 | browser | `internal/enums/lower.go` (7d08b4daf565); `tests/acceptance/enums_test.go` (a294eb9bffc8); `tests/acceptance/enum_external_test.go` (4fa5baffb003) |
| match、guard 与表达式 | 0009 | 部分实现 | browser | `internal/matchexpr/lower.go` (98de3e7c1b8b); `tests/acceptance/match_call_signatures_test.go` (c3c0e1f58455); `tests/acceptance/match_named_dynamic_test.go` (212f9b86bd3a) |
| Option / Result 与 ? | 0008, research/0005 | 部分实现 | browser | `internal/propagate/lower.go` (3b0969eb87f4); `tests/acceptance/propagate_test.go` (bf545583d370); `tests/acceptance/propagate_values_test.go` (4d0b2d61d4e1); `internal/propagate/sum_inference.go` (b81c6f26cd7a); `internal/frontend/shared_std_test.go` (a2cb837c392e) |
| Go 返回值适配：静态 T/E 推导 | research/0005 | 已实现并验证 | browser | `internal/propagate/go_result.go` (432885922507); `tests/acceptance/go_result_inference_test.go` (f18820be8cd8) |
| PartialEq / Eq 与 Clone | 0008, 0013 | 部分实现 | browser | `internal/equality/lower.go` (b9456af1badc); `tests/acceptance/equality_test.go` (319372e9bef5); `tests/acceptance/manual_equality_test.go` (ad14d9b199a0); `tests/acceptance/macros_test.go` (b8b834a2cd6e) |
| Default、Debug 与格式宏 | 0012, 0013 | 部分实现 | browser | `internal/printing/lower.go` (ecf6dd4e3401); `internal/deriveextras/generate.go` (1bf99445b72e); `tests/acceptance/manual_default_test.go` (3c5ce3d9ac6f); `tests/acceptance/print_default_test.go` (8d9cb286c186) |
| serde 核心协议与 JSON | 0013, research/0006 | 部分实现 | browser | `tests/acceptance/option_serde_test.go` (f6f2004804ca); `tests/acceptance/serde_defaults_test.go` (03d817ca4697); `docs/serde-json.md` (7709e62022f8); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5) |
| Error 派生与错误链 | 0013, research/0005 | 部分实现 | browser | `internal/deriveextras/error.go` (10ec1a9760c5); `tests/acceptance/serde_causes_test.go` (372778994a1a); `tests/acceptance/macros_test.go` (b8b834a2cd6e) |
| use / pub use 与 API 聚合 | 0021 | 部分实现 | cli | `cmd/goxide/directory.go` (62864a33b3e7); `tests/acceptance/qualified_packages_test.go` (25a4ac44f3c6); `examples/reexports/README.md` (31989fe04d79) |
| feature、依赖来源与 targets | 0001, 0022, 0025 | 部分实现 | cli | `internal/project/manifest.go` (42dc79d3fa45); `internal/project/planner.go` (2c13d82e8dcb); `cmd/goxide/project_test.go` (86c09149ddd1); `cmd/goxide/version_test.go` (1370c81d2b73); `cmd/goxide/combination_review_test.go` (3cda1e895a84); `docs/rfcs/0025-unified-project.md` (85cacb14b359); `cmd/goxide/import_go_test.go` (1ed0248fd8c7) |
| 从普通 Go 迁入：plan → 新目录 | 0025 | 部分实现 | cli | `docs/rfcs/0025-unified-project.md` (85cacb14b359); `cmd/goxide/import_go.go` (58fc6c7047ae); `cmd/goxide/import_go_test.go` (1ed0248fd8c7) |
| 宿主构建脚本与授权 | 0023 | 部分实现 | cli | `internal/scripttrust/store.go` (d521e32ece88); `cmd/goxide/scripts.go` (a4e9d6db91b5); `cmd/goxide/buildscript_test.go` (d4c86f8a2a1c); `cmd/goxide/combination_review_test.go` (3cda1e895a84) |
| 只读路径与写引用 | 0005, 0006 | 部分实现 | browser | `internal/refs/lower.go` (cc41b8b85b7e); `internal/refs/flow.go` (8a7482599404); `tests/acceptance/refs_test.go` (a4125cbb0615); `tests/acceptance/composite_permissions_test.go` (823a192be1cf) |
| 别名与借用风险分析 | 0005 | 已确认待实现 | unavailable | `docs/rfcs/0005-borrows.md` (449fd0d8d478); `ROADMAP.md` (648ac14e9ec7) |
| trait、关联类型与 impl | 0002, 0003, 0004, 0019, 0020 | 已确认待实现 | unavailable | `docs/rfcs/0019-uniform-trait-wrappers.md` (1df569cda8f2); `docs/rfcs/0020-associated-functions.md` (003d302b642a); `ROADMAP.md` (648ac14e9ec7) |
| safe std、锁与集合 | 0006, 0007, 0010, 0016, 0017 | 已确认待实现 | unavailable | `docs/rfcs/0007-locks.md` (ade41b5149bf); `docs/rfcs/0010-collections.md` (4227ef865b5f); `docs/rfcs/0016-safe-std.md` (06d218c8a138); `docs/rfcs/0017-maps.md` (7edff0be2346) |
| quote / syn 与过程宏 | 0014 | 已确认待实现 | unavailable | `docs/rfcs/0014-syntax-tools.md` (977cadd5ef80) |
| 降低约束与公开边界 | 0015, 0018 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (298633fff97a); `tests/acceptance/source_mapping_test.go` (b332d639202d); `docs/rfcs/0018-lowering-contract.md` (601d5fc37c38) |
| use 路径与依赖别名 | 0024 | 提案 | unavailable | `@use-path:docs/rfcs/0024-use-paths.md` (9d3a71b5d773) |
| 分仓标准库与共享 sum | 0008, 0016 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (a2cb837c392e); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5); `docs/reviews/public-library-migration-20261004.md` (5c4089eda830) |
| 上下文推导与生成 Go 可读性 | 0008, 0018 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (a2cb837c392e); `internal/frontend/readability_test.go` (4a76f296f046); `internal/enums/builtins_test.go` (df7d2414ab1b) |
| CLI：明确的长短选项与参数边界 | 0022, 0023 | 部分实现 | cli | `docs/reviews/cobra-cli-20261004.md` (8ea6aedbfb22); `cmd/goxide/cli_process_test.go` (7eb2a08d0445); `cmd/goxide/cli_dependency_graph_test.go` (fcca67ecb0e8) |
| 原生 Go vendor：实际字节与离线锁 | 0025 | 部分实现 | cli | `docs/reviews/vendor-import-20261004.md` (e34aa299b447); `cmd/goxide/vendor_import_test.go` (0ed50e05a975) |
| Workspace：作者核心与统一根锁 | 0026 | 部分实现 | cli | `docs/rfcs/0026-workspaces.md` (4c221c757252); `docs/reviews/workspace-core-20261004.md` (cd447ed4f6a6); `cmd/goxide/workspace_test.go` (8f2753c45dd2); `internal/project/workspace_test.go` (7856d06c586f) |
| go.work / workspace vendor 迁入与渐进增强 | 0026 | 部分实现 | cli | `docs/design/workspace-import.md` (6555f4c24c56); `docs/reviews/workspace-import-20261004.md` (62a5cfb20f03); `cmd/goxide/native_enhanced_test.go` (de840752fb2f); `cmd/goxide/native_standalone_test.go` (f53caf863cb8) |

开发快照：{"use-path": "5e4b118f25262d8022d1b8f72575d89d69f096aa", "shared": "61a01cc3a717585fb58600fe0dc70e1d5995e150"}

管理建议：规范留在编译器仓库随版本评审；Issue 讨论/跟踪，PR 变更规范与实现；公共网站只发布审核后的摘要。未创建新仓库或 Issue。

# 网站 RFC 特性状态索引

编译器基线：`b8bb3a5e21ea84ef29490ba845c39ce04dbdc739`；证据复核日期：2026-10-04。

由 `website/catalog-source.json` 生成。状态是限定范围的人工判断；构建检查引用、源码版本、示例输出与拒绝例。此索引保留私有证据，公开构建仅导出摘要。

| 特性 | RFC | 状态 | 当前入口 | 实现/测试/设计证据（基线或注明的开发快照） |
| --- | --- | --- | --- | --- |
| 声明宏：模式与重复 | 0011 | 部分实现 | browser | `internal/macros/macros_test.go` (1969fa6ab4c3); `internal/macros/primitive_bindings_test.go` (c0fcfd58b93e); `tests/acceptance/macro_packages_test.go` (b54eeec45e64) |
| 封闭 enum 与明确状态 | 0008, 0015 | 部分实现 | browser | `internal/enums/lower.go` (c1ef6dd60491); `tests/acceptance/enums_test.go` (8d1a2d6ed0ad); `tests/acceptance/enum_external_test.go` (166c8abc19b2) |
| match、guard 与表达式 | 0009 | 部分实现 | browser | `internal/matchexpr/lower.go` (5bcbaa55a621); `tests/acceptance/match_call_signatures_test.go` (df045452c6ba); `tests/acceptance/match_named_dynamic_test.go` (a454b7026077) |
| Option / Result 与 ? | 0008, research/0005 | 部分实现 | browser | `internal/propagate/lower.go` (0e150d2b3d15); `tests/acceptance/propagate_test.go` (f7ea036c894d); `tests/acceptance/propagate_values_test.go` (ed017b70ac61) |
| Go 返回值适配：静态 T/E 推导 | research/0005 | 已实现并验证 | browser | `internal/propagate/go_result.go` (b49565c2c198); `tests/acceptance/go_result_inference_test.go` (166aad6b5572) |
| PartialEq / Eq 与 Clone | 0008, 0013 | 部分实现 | browser | `internal/equality/lower.go` (807a9fe0f526); `tests/acceptance/equality_test.go` (bc292fd3c65d); `tests/acceptance/manual_equality_test.go` (c1db9022a8e9); `tests/acceptance/macros_test.go` (ddf8bb2e7dcc) |
| Default、Debug 与格式宏 | 0012, 0013 | 部分实现 | browser | `internal/printing/lower.go` (b623e0e543cc); `internal/deriveextras/generate.go` (58ff12efc1a2); `tests/acceptance/manual_default_test.go` (aca142ec3c50); `tests/acceptance/print_default_test.go` (664b57960f03) |
| serde 核心协议与 JSON | 0013, research/0006 | 部分实现 | browser | `tests/acceptance/option_serde_test.go` (82ba2abe3651); `tests/acceptance/serde_defaults_test.go` (cf1fac459f23); `docs/serde-json.md` (7709e62022f8); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5) |
| Error 派生与错误链 | 0013, research/0005 | 部分实现 | browser | `internal/deriveextras/error.go` (10ec1a9760c5); `tests/acceptance/serde_causes_test.go` (c64838abef35); `tests/acceptance/macros_test.go` (ddf8bb2e7dcc) |
| use / pub use 与 API 聚合 | 0021 | 部分实现 | cli | `cmd/hgo/directory.go` (42fa3bd1aee2); `tests/acceptance/qualified_packages_test.go` (c5bfce420056); `examples/reexports/README.md` (876444327fcb) |
| feature、依赖来源与 targets | 0001, 0022 | 部分实现 | cli | `internal/project/manifest.go` (ede03e85327e); `internal/project/planner.go` (ca315ed98d28); `cmd/hgo/project_test.go` (eb7273d8a10b); `cmd/hgo/version_test.go` (3b1a5866fd36); `cmd/hgo/combination_review_test.go` (23c1636f7550) |
| 宿主构建脚本与授权 | 0023 | 部分实现 | cli | `internal/scripttrust/store.go` (f0921cd5c9aa); `cmd/hgo/scripts.go` (8208d7b3eb8c); `cmd/hgo/buildscript_test.go` (e28e9b879b33); `cmd/hgo/combination_review_test.go` (23c1636f7550) |
| 只读路径与写引用 | 0005, 0006 | 部分实现 | browser | `internal/refs/lower.go` (f8bc43440764); `internal/refs/flow.go` (8a7482599404); `tests/acceptance/refs_test.go` (d86a8c58b151); `tests/acceptance/composite_permissions_test.go` (8aa1757b4743) |
| 别名与借用风险分析 | 0005 | 已确认待实现 | unavailable | `docs/rfcs/0005-borrows.md` (64077ea1ab96); `ROADMAP.md` (2eb41146a857) |
| trait、关联类型与 impl | 0002, 0003, 0004, 0019, 0020 | 已确认待实现 | unavailable | `docs/rfcs/0019-uniform-trait-wrappers.md` (2eb3455cf393); `docs/rfcs/0020-associated-functions.md` (fe9214afd14c); `ROADMAP.md` (2eb41146a857) |
| safe std、锁与集合 | 0006, 0007, 0010, 0016, 0017 | 已确认待实现 | unavailable | `docs/rfcs/0007-locks.md` (2cc232953b36); `docs/rfcs/0010-collections.md` (4584de88ecfc); `docs/rfcs/0016-safe-std.md` (4a44a92d8cdf); `docs/rfcs/0017-maps.md` (4f3f0e488622) |
| quote / syn 与过程宏 | 0014 | 已确认待实现 | unavailable | `docs/rfcs/0014-syntax-tools.md` (a353eb00b301) |
| 降低约束与公开边界 | 0015, 0018 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (54e4fca71f7c); `tests/acceptance/source_mapping_test.go` (c91583c97af2); `docs/rfcs/0018-lowering-contract.md` (fa6c8182e549) |
| use 路径与依赖别名 | 0024 | 提案 | unavailable | `@use-path:docs/rfcs/0024-use-paths.md` (9d3a71b5d773) |
| 分仓标准库与共享 sum | 0016, 0024 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (2b3c470b75c1); `internal/frontend/serde_protocol_artifact_test.go` (8ffaeb8e65c5); `docs/reviews/public-library-migration-20261004.md` (d6e5d23c1122) |
| 普通 sum 推导与输出可读性 | 0008, 0018 | 部分实现 | browser | `internal/frontend/shared_std_test.go` (2b3c470b75c1); `internal/frontend/readability_test.go` (3778ab0cf652); `internal/enums/builtins_test.go` (2dc5aeafcafc) |

开发快照：{"use-path": "5e4b118f25262d8022d1b8f72575d89d69f096aa", "shared": "61a01cc3a717585fb58600fe0dc70e1d5995e150"}

管理建议：规范留在编译器仓库随版本评审；Issue 讨论/跟踪，PR 变更规范与实现；公共网站只发布审核后的摘要。未创建新仓库或 Issue。

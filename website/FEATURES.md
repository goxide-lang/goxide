# 网站 RFC 特性状态索引

编译器基线：`17d9ae56a552e4910fda7fcb5482a8cabc0fa339`；证据复核日期：2026-10-31。

由 `website/catalog-source.json` 生成。状态是限定范围的人工判断；构建检查引用、源码版本、示例输出与拒绝例。此索引保留私有证据，公开构建仅导出摘要。

| 特性 | RFC | 状态 | 当前入口 | 实现/测试/设计证据（基线或注明的开发快照） |
| --- | --- | --- | --- | --- |
| 声明宏：模式与重复 | 0011 | 部分实现 | browser | `tests/acceptance/macro_packages_test.go` (a6de7df7125e); `docs/rfcs/0011-macros.md` (5a6dc8794541); `tests/acceptance/macro_fragment_categories_test.go` (9d7284a46b1d) |
| 封闭 enum 与明确状态 | 0008, 0015 | 部分实现 | browser | `tests/acceptance/enums_test.go` (b53640dbde79); `tests/acceptance/enum_external_test.go` (4215e89dc56e); `tests/acceptance/enum_payload_borrow_test.go` (a230736fbf96) |
| match、guard 与表达式 | 0009 | 部分实现 | browser | `tests/acceptance/match_call_signatures_test.go` (b15bde5ba8b2); `tests/acceptance/match_named_dynamic_test.go` (8794a29925c6); `tests/acceptance/match_unary_composite_test.go` (8bc7df20436e); `docs/rfcs/0009-match.md` (7d494bb3c056) |
| Option / Result 与 ? | 0008, research/0005, 0032 | 部分实现 | browser | `tests/acceptance/propagate_test.go` (93932a6284dd); `tests/acceptance/propagate_values_test.go` (04dc3cc45c92) |
| Go 返回值适配：静态 T/E 推导 | research/0005 | 已实现并验证 | browser | `tests/acceptance/go_result_inference_test.go` (507bc27e2d7a) |
| PartialEq / Eq 与 Clone | 0008, 0013 | 部分实现 | browser | `tests/acceptance/equality_test.go` (1eaa734477c6); `tests/acceptance/manual_equality_test.go` (890bd2a231f9); `tests/acceptance/macros_test.go` (130bf3043792) |
| Default、Debug 与格式宏 | 0012, 0013 | 部分实现 | browser | `tests/acceptance/manual_default_test.go` (4fd0844e2aab); `tests/acceptance/print_default_test.go` (5a604a13397c) |
| serde 核心协议与 JSON | 0013, research/0006 | 部分实现 | browser | `tests/acceptance/option_serde_test.go` (7c820d43d050); `tests/acceptance/serde_defaults_test.go` (f57e866f6705); `docs/serde-json.md` (824115ca4aac) |
| Error 派生与错误链 | 0013, research/0005 | 部分实现 | browser | `tests/acceptance/serde_causes_test.go` (734880ab6885); `tests/acceptance/macros_test.go` (130bf3043792) |
| use / pub use 与 API 聚合 | 0021 | 部分实现 | cli | `tests/acceptance/qualified_packages_test.go` (6f9a04aec9f4); `examples/reexports/README.md` (67e1513bf1e1) |
| 多包、feature 与编译目标 | 0001, 0022, 0025 | 部分实现 | cli | `internal/project/manifest.go` (3a7e476ca4f1); `internal/project/planner.go` (81f8dae61328); `cmd/goxide/project_test.go` (cc7e8a152fd3); `cmd/goxide/version_test.go` (9bc4f31c0e25); `docs/rfcs/0025-unified-project.md` (63bafab76210); `cmd/goxide/import_go_test.go` (f2a3757cbc07); `cmd/goxide/single_file.go` (bc07016d1220); `tests/acceptance/single_file_external_test.go` (046e053b07fc) |
| 从普通 Go 迁入：plan → 新目录 | 0025 | MVP 阶段恢复 | unavailable | `docs/rfcs/0025-unified-project.md` (63bafab76210); `cmd/goxide/import_go.go` (726a93405de6); `cmd/goxide/import_go_test.go` (f2a3757cbc07) |
| 宿主构建脚本与授权 | 0023 | MVP 阶段恢复 | unavailable | `internal/scripttrust/store.go` (1b8255621bb4); `cmd/goxide/scripts.go` (148c6ee0d37f) |
| 只读路径与写引用 | 0005, 0006 | 部分实现 | browser | `tests/acceptance/refs_test.go` (fa4096baf1cc); `tests/acceptance/composite_permissions_test.go` (8d534def4950) |
| 别名与借用风险分析 | 0005 | 已确认待实现 | unavailable | `docs/rfcs/0005-borrows.md` (f8d6395d5239) |
| 静态协议、关联函数与 impl | 0002, 0003, 0004, 0019, 0020, 0031 | 部分实现 | browser | `docs/static-protocols.md` (6e7409874de4); `tests/acceptance/static_protocol_test.go` (e6f941508e22) |
| safe std、锁与集合 | 0006, 0007, 0010, 0016, 0017 | 已确认待实现 | unavailable | `docs/rfcs/0007-locks.md` (3a27dc2b760e); `docs/rfcs/0010-collections.md` (4227ef865b5f); `docs/rfcs/0016-safe-std.md` (1226aafb44c7); `docs/rfcs/0017-maps.md` (632d10a19c9a) |
| syntax 语法工具与过程宏 | 0014, 0030 | 部分实现 | browser | `docs/rfcs/0014-syntax-tools.md` (977cadd5ef80); `docs/rfcs/0030-procedural-macros.md` (8884e124cf4e); `internal/compile/proc_build_test.go` (038a8d96c2a5); `internal/compile/proc_attribute_scope_test.go` (e2da6841c6aa); `internal/lower/proc_snapshot_e2e_test.go` (dd413226522e); `cmd/goxide/proc_macro_test.go` (389bd8b6a309) |
| 降低约束与公开边界 | 0015, 0018, 0027 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (1124d35f2782); `docs/rfcs/0018-lowering-contract.md` (5d756137f2ad) |
| 显式依赖根与静态路径 | 0024, 0028 | 部分实现 | browser | `internal/paths/paths.go` (849ffb9f4799); `internal/rootconfig/roots.go` (a6fefb11ad44); `cmd/goxide-playground/main.go` (ea8c596ab953) |
| 分仓标准库与编译器支持 | 0008, 0016 | 部分实现 | browser | `internal/lower/enum_packages_test.go` (f56e7533af81); `tests/acceptance/sum_clone_debug_test.go` (b4369a9a112b); `tests/acceptance/option_serde_test.go` (7c820d43d050) |
| 上下文推导与生成 Go 可读性 | 0008, 0018 | 部分实现 | browser | `tests/acceptance/source_lowering_test.go` (1124d35f2782); `tests/acceptance/go_result_inference_test.go` (507bc27e2d7a); `internal/lower/enum_packages_test.go` (f56e7533af81) |
| CLI：明确的长短选项与参数边界 | 0022, 0023 | 部分实现 | cli | `cmd/goxide/cli_process_test.go` (876a10b7c756); `cmd/goxide/cli_dependency_graph_test.go` (a821704cb6b9) |
| 供应模块：local 默认与显式 remote 选择 | 0025 | MVP 阶段恢复 | unavailable | `cmd/goxide/vendor_import_test.go` (4c6b1ec8c077); `cmd/goxide/import_sources_test.go` (4c0baf10849c); `internal/project/supplied_sources.go` (4f367114e774) |
| Workspace：作者核心与统一根锁 | 0026 | MVP 阶段恢复 | unavailable | `docs/rfcs/0026-workspaces.md` (5514a7179db8); `internal/project/workspace_test.go` (147ca83b8b34) |
| go.work 迁入与本地模块渐进增强 | 0026 | MVP 阶段恢复 | unavailable | `docs/design/workspace-import.md` (f2569eaddf22) |

开发快照：{}

管理建议：规范留在编译器仓库随版本评审；Issue 讨论/跟踪，PR 变更规范与实现；公共网站只发布审核后的摘要。未创建新仓库或 Issue。

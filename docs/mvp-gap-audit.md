# 当前能力状态

这是当前能力的唯一权威列表，其他文档里的示例不代表已实现

| 特性 | 状态 | 入口或示例 | 对应测试文件 |
| --- | --- | --- | --- |
| 声明宏 | 支持 | [四种位置与卫生示例](../internal/lower/testdata/macros/positions/)：表达式、语句、类型、item；定义点解析和 `$caller::`；[跨包宏](../examples/macro_packages/) | [macro_e2e_test.go](../internal/lower/macro_e2e_test.go)、[macro_binding_e2e_test.go](../internal/lower/macro_binding_e2e_test.go) |
| 用户 enum / match | 部分 | [enum 示例](../examples/enum_trial/)：密封接口、泛型、payload、单次分派、guard、nil 检查；控制头中的 match 未支持 | [core_e2e_test.go](../internal/lower/core_e2e_test.go)、[match_assignment_test.go](../tests/acceptance/match_assignment_test.go) |
| Option / Result 与 `?` | 部分 | [传播示例](../examples/propagate_values/)：构造、match、提前返回及单步错误转换；range 赋值目标和函数迭代器源中的 `?` 未支持；`std/sum` 仍使用 tagged API，未与用户 enum 统一为密封接口 | [core_e2e_test.go](../internal/lower/core_e2e_test.go)、[propagate_loops_test.go](../tests/acceptance/propagate_loops_test.go)、[package_test.go](../internal/playground/package_test.go) |
| 派生 | 支持 | [Default 示例](../examples/manual_default/)；`Default`、`Clone`、`Debug`、`PartialEq`、`Eq`、`Error`；字段协议、顺序和手写协议冲突检查 | [derive_test.go](../internal/lower/derive_test.go)、[error_test.go](../internal/lower/error_test.go) |
| serde JSON 派生 | 部分 | [默认值示例](../examples/serde_defaults/)：结构体、容器、enum、字段映射及缺失/重复/未知字段检查；payload 变体的 wire 映射和变体字段属性未支持 | [serde_test.go](../internal/lower/serde_test.go)、[enum_wire_test.go](../tests/acceptance/enum_wire_test.go) |
| 引用与 payload 原位投影 | 支持 | [借用示例](../examples/enum_borrows/)：`&`、`&mut`、`match &e`、槽位身份、重绑定及读写权限检查 | [reference_e2e_test.go](../internal/lower/reference_e2e_test.go)、[reference_test.go](../internal/check/reference_test.go)、[reference_flow_test.go](../internal/check/reference_flow_test.go) |
| trait / impl | 部分 | [静态协议示例](../examples/static_protocol/)：非泛型 trait、具体类型 impl、关联类型、静态方法、单 trait 约束、wrapper/marker 和 Go 侧适配；泛型 trait/impl、多 trait 约束、GAT 和默认方法未支持 | [p4_static_test.go](../internal/lower/p4_static_test.go)、[p4_runtime_test.go](../internal/lower/p4_runtime_test.go)、[traits_test.go](../internal/check/traits_test.go) |
| 多包与跨包 use | 支持 | [多包项目](../internal/compile/testdata/p5/app/)：Goxide 与 Go 包、跨包 use、导入合并、真实依赖下 build/run/test | [project_e2e_test.go](../internal/compile/project_e2e_test.go)、[project_test.go](../internal/compile/project_test.go) |
| 最小清单与锁文件 | 支持 | [goxide.toml](../internal/compile/testdata/p5/app/goxide.toml)：package、dependencies、bin 与[依赖库声明](../internal/compile/testdata/p5/kit/goxide.toml)；重复构建与 offline/locked 保持锁文件及生成内容稳定 | [project_e2e_test.go](../internal/compile/project_e2e_test.go)、[project_test.go](../internal/compile/project_test.go) |
| CLI 三种输入模式 | 支持 | [cmd/goxide](../cmd/goxide/)：单文件、目录、项目走同一前端；transpile/build/run；expand 仅单文件；test 要求包目录或项目；help/completion；项目选择限 bin | [main_test.go](../cmd/goxide/main_test.go)、[roots_test.go](../cmd/goxide/roots_test.go)、[single_file_test.go](../cmd/goxide/single_file_test.go)、[directory_test.go](../cmd/goxide/directory_test.go)、[native_file_test.go](../cmd/goxide/native_file_test.go)、[cli_process_test.go](../cmd/goxide/cli_process_test.go) |
| 网站与浏览器转译 | 支持 | [website](../website/)：新编译器 WASM、示例目录、单包多文件编辑、CLI/WASM 生成结果一致；浏览器只转译 | [qa.mjs](../website/qa.mjs)、[catalog-qa.mjs](../website/catalog-qa.mjs)、[compiler_test.mjs](../website/compiler_test.mjs)、[package_test.go](../internal/playground/package_test.go) |
| workspace、feature 与 example/lib 目标选择 | 未做 | 当前项目入口拒绝 workspace、feature、`--example` 和 `--lib`；[MVP-1](pre-mvp-plan.md#后续阶段mvp)：成员、依赖继承、根锁和按成员合并 feature | [project_test.go](../internal/compile/project_test.go)（workspace/feature 拒绝入口） |
| import-go | 未做 | CLI 入口已摘除；[MVP-2](pre-mvp-plan.md#后续阶段mvp) | [cli_process_test.go](../cmd/goxide/cli_process_test.go)（拒绝入口） |
| vendor 与供应来源选择 | 未做 | 当前项目入口拒绝 vendor；[MVP-3](pre-mvp-plan.md#后续阶段mvp) | 无新管线验收测试 |
| 构建脚本授权 | 未做 | 项目构建拒绝 build 配置，scripts CLI 入口已摘除；[MVP-4](pre-mvp-plan.md#后续阶段mvp) | [project_test.go](../internal/compile/project_test.go)、[cli_process_test.go](../cmd/goxide/cli_process_test.go)（拒绝入口） |
| source map | 未做 | `--source-map` 明确报错；旧映射测试随旧管线删除；[MVP-5 LSP](pre-mvp-plan.md#后续阶段mvp) | [main_test.go](../cmd/goxide/main_test.go)、[single_file_test.go](../cmd/goxide/single_file_test.go)（拒绝输出） |
| LSP | 未做 | 诊断、符号导航、悬停和补全入口；[MVP-5](pre-mvp-plan.md#后续阶段mvp) | 无 |
| safe std 首批入口、锁与集合 | 未做 | [MVP-6](pre-mvp-plan.md#后续阶段mvp) | 无 |

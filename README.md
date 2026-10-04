# Hyper Go（hgo）

hgo 在 Go 生态上实现一个可执行语言子集：声明宏、闭合变体 enum/match、Option/Result、显式派生和 serde JSON，生成普通 Go 源码。设计中的完整 trait、LSP、指针体系等并不因此成为当前实现承诺。

**宏 + 独立 enum 基线为 `55ef3b3020b3984a8ae360ca469fc13fee345bd2`，2026-10-04 已新增显式 Go 返回值适配宏。** 已验证 Go 1.27.1；[适配宏试用](examples/go_result/README.md)展示普通 Ok/Err 和 Reader 错误时保留部分值。此阶段已有可审查的业务闭环，**不宣称整个 MVP 完成**。

## 开始试用

先看 [最小双模块订单示例](examples/enum_trial/README.md)：一条脚本构建 CLI、生成生产者与消费者、运行及 race/vet；可直接观察正确/错误 JSON、None/Some(nil)/Some(empty)、equals 和跨包 derive。它复用既有联合业务，不含新指针语法。

```sh
# 改成你机器上真实的 Go 1.27.x；这是本次云环境的已验证路径。
export GO_BIN=/workspace/goxide-environment/go/bin/go
work="$(mktemp -d)"
bash examples/enum_trial/try.sh "$work"
```

工具链不能仅按 `go` 这个名字判断。CLI 的 `-go /absolute/go` 可选择执行编译器，但 importer 使用构建 CLI 的 GOROOT；目录运行/测试要求二者版本一致。生成模块仍通过 go.mod 依赖支持库，不是自包含 runtime 副本。

## 当前支持

| 能力 | 已验收范围与入口 |
| --- | --- |
| 声明宏 | 有限捕获、重复、嵌套、定义点绑定；源码包 `pub macro_rules!` 与 qualified 调用；[准确边界](internal/macros/README.md) |
| 闭合 enum 与 match | 名义身份、穷尽匹配、guard、非法值在使用点拒绝；有限表达式组合，包括命名单结果、静态已知方法/函数字段/返回函数/variadic/原生 Go 显式 generic 调用；[match 范围](internal/matchexpr/README.md) |
| Option / Result | 已登记具体实例、match、有限 `?` 组合、Go cause 链、显式 `go_result!` / `go_result_keep!`；跨独立模块恢复及透明 alias；[验收](docs/t0-acceptance.md) |
| 显式协议 | Default、Clone、Debug、serde、PartialEq/Eq 的有限具体类型及跨包登记；已支持手写 Default 和具体 equality，不隐式赋予所有类型协议 |
| serde | core 协议 + 独立 JSON 实现；default/skip、具体 Option 缺失/null、闭合变体；[JSON 范围](serdejson/README.md) |
| Go 互操作 | 单文件及现有 go.mod 中多文件/多包目录、独立模块 local replace、生成 helper 的原生 Go 消费；build tags/race 属于具体构建视图 |

Default 不改变 Go 零初始化；struct 要求全部字段满足 Default，enum 只要求显式选中默认变体的载荷。普通 Option 缺失/null→None 不依赖 Default。Clone 不改变普通 Go 赋值语义；enum 不是开放 C-style 数字枚举。

## 已知失败与未完成边界

以下是工程边界，不自动等于获批延期；具体正反例见[最新验收清单](docs/t0-acceptance.md#独立-enum-现阶段交付验收与待决清单2026-10-03统一调用组后)。

- **表达式和类型上下文**：多结果 `return match`、部分 unary/composite/block-arm 组合、完全不可命名的私有参数仍拒绝；`.hgo` 用户泛型定义尚未开放，与消费原生 Go 泛型函数不同。
- **模块与工具模式**：单文件外部增强恢复、受限 internal-origin/无源分发未全面验收；预编译宏 ABI、`.hgo` 测试文件与非宿主交叉构建未实现。切换 race/tags/平台不能直接复用另一视图的 metadata；[本试用的具体模式与已知拒绝](examples/enum_trial/README.md#运行与测试模式)。
- **协议资格**：手写 Clone/Debug/serde 登记、静态递归 Default、循环容器 equality 等仍有缺口或需另行设计；有限容器正例不证明任意对象图支持。
- **完整设计能力**：完整 trait、LSP、用户过程宏、格式 Value/PATCH/YAML 等未成为本阶段交付能力；不要把 RFC 示例当作已实现语法。

## 待用户决定

1. 公开函数是否在函数体前验证直接 enum 参数，以及非法值行为；当前使用点验证不等于统一入口验证。
2. blank 字段如何参与派生；embedded 字段的 Debug 名称、serde 嵌套/展开规则。
3. Proposed serde field adapters 的具体入口与自定义 `deserialize_with` 缺失策略。

既定方向和未定项以[决策登记](docs/design/decisions.md)及对应 RFC 为准，不将上述事项默认标为批准延期。

## 后续指针阶段

用户已有后续指针目标；本轮只推进明确授权的 Go Result 适配宏，不启动指针实现。仓库已有的有限读/写引用验收保留，但本试用不要求使用新指针语法。

`&T` 路径只读不承诺全局不可变，`mut&` 不承诺独占。复杂别名/借用风险分析是独立长期工作线，不是拿保守拒绝普通 Go 用法来完成的安全保证，也不是当前 MVP 的前置条件。详见[指针与引用设计](docs/design/pointers-and-borrows.md)。

## 验收与设计导航

- [交付试用与逐条命令](examples/enum_trial/README.md)：本次新增示例的实际验证。
- [T0 验收矩阵](docs/t0-acceptance.md)：真实 hgo→Go 与独立生成模块证据、已知失败及待决。
- [实现历史](IMPLEMENTATION.md)、[Roadmap](ROADMAP.md)：按阶段保留历史；旧限制以最新验收覆盖，不能截取早期条目代表当前能力。
- [逐特性 RFC](docs/rfcs/README.md)、[设计总纲](docs/design/overview.md)、[决策登记](docs/design/decisions.md)：区分已确认、暂定、未定。完整 trait 包装器/发布 ABI 等仍须按各自状态阅读。
- [宏与派生设计](docs/design/macros.md)、[enum/match 设计](docs/design/enums-and-match.md)、[serde core 与格式](docs/rfcs/research/0006-serde-core-and-formats.md)。
- [测试说明](tests/README.md)：`scripts/check_design.py` 验证文档中的 Go 机制，不能替代真实 hgo 编译执行。各轮全树 race/vet/设计检查与独立生成模块证据见验收矩阵。
- [冻结 MVP 文档基线](docs/mvp-doc-baseline.md)：标签记录设计基线，不代表实现完成；本阶段不修改标签、不合并或发布。

## 许可证

[Mozilla Public License 2.0](LICENSE)（`MPL-2.0`）。项目暂称 hgo / Hyper Go，仓库名为 `goxide`。

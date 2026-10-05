# Goxide（goxide）

Goxide 在 Go 生态上实现一个可执行语言子集：声明宏、闭合变体 enum/match、Option/Result、显式派生和 serde JSON，生成普通 Go 源码。设计中的完整 trait、LSP、指针体系等并不因此成为当前实现承诺。

**宏 + 独立 enum 基线为 `55ef3b3020b3984a8ae360ca469fc13fee345bd2`，2026-10-04 已新增显式 Go 返回值适配宏。** 已验证 Go 1.27.1；[适配宏试用](examples/go_result/README.md)展示普通 Ok/Err 和 Reader 错误时保留部分值。此阶段已有可审查的业务闭环，**不宣称整个 MVP 完成**。

正式命名为 **Goxide**；CLI `goxide`、源码 `.gox`、环境变量 `GOXIDE_*`。pre-1.0 命名迁移要求重新生成所有旧产物并更新 std，见[迁移合同](docs/rfcs/0027-goxide-naming.md)。

## 开始试用

先看 [最小双模块订单示例](examples/enum_trial/README.md)：一条脚本构建 CLI、生成生产者与消费者、运行及 race/vet；可直接观察正确/错误 JSON、None/Some(nil)/Some(empty)、equals 和跨包 derive。它复用既有联合业务，不含新指针语法。

```sh
# 改成你机器上真实的 Go 1.27.x；这是本次云环境的已验证路径。
export GO_BIN=/workspace/goxide-environment/go/bin/go
"$GO_BIN" mod download all # 首次预热完整固定模块图；试用脚本本身离线。
work="$(mktemp -d)"
bash examples/enum_trial/try.sh "$work"
```

工具链不能仅按 `go` 这个名字判断。CLI 的 `--go /absolute/go` 可选择执行编译器，但 importer 使用构建 CLI 的 GOROOT；目录运行/测试要求二者版本一致。生成模块仍通过 go.mod 依赖支持库，不是自包含 runtime 副本。

CLI 长选项使用 `--name`，短选项使用 `-h`、`-o`，拒绝旧式 `-go/-offline`。用
`goxide --help`、`goxide help scripts allow` 查看中文帮助，或 `goxide completion bash` 生成补全。
选项放在输入路径前；程序参数用 `goxide run main.gox -- ARGS...` 原样传递。scripts
支持六个显式动作子命令，仍兼容 `--action`，不会默认授信当前目录。详细规则见
[CLI 语法合同](docs/design/cli-parser-migration.md)。

## 网页与浏览器 Playground

[构建独立 goxide-web 二进制](website/README.md)：中文特性目录、十组真实 goxide → Go 对照与浏览器 WASM 转译。资源全部 Go embed，默认本机监听，不需要 Node、外部服务或运行时资源目录。浏览器仅支持清单内包的单文件转译；CLI 与 WASM 示例逐字节校验。

## 当前支持

| 能力 | 已验收范围与入口 |
| --- | --- |
| 声明宏 | 有限捕获、重复、嵌套、定义点绑定；源码包 `pub macro_rules!` 与 qualified 调用；[准确边界](internal/macros/README.md) |
| 闭合 enum 与 match | 名义身份、穷尽匹配、guard、非法值在使用点拒绝；有限表达式组合，包括命名单结果、静态已知方法/函数字段/返回函数/variadic/原生 Go 显式 generic 调用；[match 范围](internal/matchexpr/README.md) |
| Option / Result | 已登记具体实例、match、有限 `?` 组合、Go cause 链、显式 `go_result!` / `go_result_keep!`；跨独立模块恢复及透明 alias；[验收](docs/t0-acceptance.md) |
| 显式协议 | Default、Clone、Debug、serde、PartialEq/Eq 的有限具体类型及跨包登记；已支持手写 Default 和具体 equality，不隐式赋予所有类型协议 |
| serde | core 协议 + 独立 JSON 实现；default/skip、具体 Option 缺失/null、闭合变体；[JSON 范围](docs/serde-json.md) |
| Go 互操作 | 单文件及现有 go.mod 中多文件/多包目录、独立模块 local replace、生成 helper 的原生 Go 消费；build tags/race 属于具体构建视图 |

Default 不改变 Go 零初始化；struct 要求全部字段满足 Default，enum 只要求显式选中默认变体的载荷。普通 Option 缺失/null→None 不依赖 Default。Clone 不改变普通 Go 赋值语义；enum 不是开放 C-style 数字枚举。

可直接运行 [构建脚本示例](examples/build_script/README.md)。显式代码生成、host helper/tool、许可与缓存的接口和正反例见 [宿主构建脚本 RFC](docs/rfcs/0023-build-scripts.md)。脚本仅在主动授权后运行；普通Go执行不构成OS沙箱。

## 已知失败与未完成边界

以下是工程边界，不自动等于获批延期；具体正反例见[最新验收清单](docs/t0-acceptance.md#独立-enum-现阶段交付验收与待决清单2026-10-03统一调用组后)。

- **表达式和类型上下文**：多结果 `return match`、部分 unary/composite/block-arm 组合、完全不可命名的私有参数仍拒绝；`.gox` 用户泛型定义尚未开放，与消费原生 Go 泛型函数不同。
- **模块与工具模式**：单文件外部增强恢复、受限 internal-origin/无源分发未全面验收；预编译宏 ABI、`.gox` 测试文件与非宿主交叉构建未实现。切换 race/tags/平台不能直接复用另一视图的 metadata；[本试用的具体模式与已知拒绝](examples/enum_trial/README.md#运行与测试模式)。
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
- [T0 验收矩阵](docs/t0-acceptance.md)：真实 goxide→Go 与独立生成模块证据、已知失败及待决。
- [实现历史](IMPLEMENTATION.md)、[Roadmap](ROADMAP.md)：按阶段保留历史；旧限制以最新验收覆盖，不能截取早期条目代表当前能力。
- [逐特性 RFC](docs/rfcs/README.md)、[设计总纲](docs/design/overview.md)、[决策登记](docs/design/decisions.md)：区分已确认、暂定、未定。完整 trait 包装器/发布 ABI 等仍须按各自状态阅读。
- [宏与派生设计](docs/design/macros.md)、[enum/match 设计](docs/design/enums-and-match.md)、[serde core 与格式](docs/rfcs/research/0006-serde-core-and-formats.md)。
- [测试说明](tests/README.md)：`scripts/check_design.py` 验证文档中的 Go 机制，不能替代真实 goxide 编译执行。各轮全树 race/vet/设计检查与独立生成模块证据见验收矩阵。
- [冻结 MVP 文档基线](docs/mvp-doc-baseline.md)：标签记录设计基线，不代表实现完成；本阶段不修改标签、不合并或发布。

## 许可证

[Mozilla Public License 2.0](LICENSE)（`MPL-2.0`）。项目暂称 goxide / Goxide，仓库名为 `goxide`。

目录包 API 聚合现支持有限的 `use` / `pub use`（逐项、别名、glob），保持变量存储和原函数身份。见 [可运行示例](examples/reexports/README.md) 与 [RFC 0021](docs/rfcs/0021-reexports.md)。这不包含完整 `mod` / `pub(scope)` 或 Cargo 工具链。

## 可选项目配置

Cargo 风格第一版通过 `goxide.toml` 增量启用默认/显式 feature、文件级 cfg、Go version/Git/path 依赖和命名 bin/example。Go 仍负责 module 解析；不声称完整 Cargo 兼容。见 [可运行项目示例](examples/project/README.md) 和 [RFC 0022：语法、命令、锁定与限制](docs/rfcs/0022-project-toolchain.md)。

项目构建默认将产物保留在 `target/`：`goxide build --bin web ./app` 会打印二进制路径，`goxide transpile --bin web ./app` 会打印可独立 Go 构建的生成 module 路径。可用 `--target-dir` 或 `GOXIDE_TARGET_DIR` 配置；相对路径以项目根为准。见 [产物布局与安全边界](docs/design/target-layout.md)。

作者 workspace 核心支持显式成员和 `{ workspace = true }` 依赖继承、全工作区统一版本与根锁、按所选成员合并 feature。用 `goxide build --workspace ROOT` 或 `goxide run -p MODULE --bin NAME ROOT` 选择；成员资格不自动提供依赖。见 [可运行 workspace 示例](examples/workspace/README.md) 与 [实现范围及限制](docs/rfcs/0026-workspaces.md#本次实现范围)。

`version` 渠道现可直接取得带 `goxide.toml` 的源码并统一 features；Go 负责共享 MVS 与 sums，稳定后才生成/执行已授权脚本。仅原生 go.mod 不隐式激活 goxide feature；固定配置 Go 发行物不重配。见 [version 来源合同与验收](docs/design/version-source-plan.md)。

项目采用 [统一项目模型](docs/rfcs/0025-unified-project.md)：根以 `goxide.toml`（`package.format = 2`，显式 module/go）和 `goxide.lock` 为配置与锁定权威，`.go` / `.gox` 可共存。原生 require/replace/exclude 写入 TOML；Go 元数据仅生成到 target。普通 Go 工程可用 `goxide import-go --plan --bin app=cmd/app ROOT` 查看方案，再用 `--out NEWDIR --offline` 在新目录验证迁移，原工程保持不变。旧 JSON、format 1、双根配置和旧锁均明确拒绝；不自动删除或改名。 `import-go` 未指定输出时默认只读计划；原生 Go vendor 仅是迁入输入，验证后物化为显式本地模块，保留修改字节、资源和许可；Goxide 没有 vendor 构建模式。旧 vendor 清单/锁须从原工程显式重新迁入，详见 [供应源码迁入合同](docs/design/vendor-local-modules.md)。

共享 sum 与 serde 已迁入独立 Go 模块：`github.com/goxide-lang/std/sum`、`github.com/goxide-lang/serde`、`github.com/goxide-lang/serde/json`。本 checkout 的 go.mod/go.sum 固定真实公开提交；不是未发布候选 replace。具体版本、发布白名单和整合验收见[迁移记录](docs/reviews/public-library-migration-20261004.md)。旧 schema 6 产物需用此编译器重新生成；不承诺与旧私有 marker 二进制兼容。

供应模块来源选择：真实终端迁入默认展示一次模块清单，空行保留全部 local；非终端默认 local，不等待读取 stdin。重复 `--source MODULE=local|remote` 可确定性选择清单证实的精确版本；`--plan` 只显示请求，不联网比对。remote 与供应字节有差异时须逐模块确认，非终端使用 `--accept-source-diff MODULE`；所有原供应字节仍保留于输出本地模块和审计。`--offline` 的 remote 选择只用已缓存完整包，缓存不足失败，不访问代理、sumdb、Git 或下载工具链。verified 仅证明供应文件集合相等，不证明完整发行物等价；不猜 Git URL，不支持任意远端或 registry 搜索。

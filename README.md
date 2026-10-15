# Goxide（goxide）

Goxide 在 Go 生态上实现一个可执行语言子集：声明宏、闭合变体 enum/match、Option/Result、显式派生和 serde JSON，生成普通 Go 源码。设计中的完整 trait、LSP、指针体系等并不因此成为当前实现承诺。

**宏 + 独立 enum 基线为 `55ef3b3020b3984a8ae360ca469fc13fee345bd2`，2026-10-04 已新增显式 Go 返回值适配宏。** 已验证 Go 1.27.1；[适配宏试用](examples/go_result/README.md)展示普通 Ok/Err 和 Reader 错误时保留部分值。`go_result!` / `go_result_keep!` 是无需 import 的编译器保留 builtin，结果类型由共享 `std/sum` 提供，不是 std 导出宏。此阶段已有可审查的业务闭环，**不宣称整个 MVP 完成**。

正式命名为 **Goxide**；CLI `goxide`、源码 `.gox`、环境变量 `GOXIDE_*`。pre-1.0 命名迁移要求重新生成所有旧产物并更新 std，见[迁移合同](docs/rfcs/0027-goxide-naming.md)。

## 开始试用

先看 [最小双模块订单示例](examples/enum_trial/README.md)：一条脚本构建 CLI、生成生产者与消费者、运行及 race/vet；可直接观察正确/错误 JSON、None/Some(nil)/Some(empty)、equals 和跨包 derive。它复用既有联合业务，不含新指针语法。

```sh
# 改成你机器上真实的 Go 1.27.x；这是本次云环境的已验证路径。
export GO_BIN=/workspace/.cloud-setup/toolchains/go1.27.1/bin/go
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

已有 Go module 中的单文件输入按其真实路径选择最近的 `go.mod`，恢复已生成依赖中
登记的外部 enum 和协议，并在同一依赖图中 build/run。只选指定文件；同目录 sibling
不提供声明，`--runtime` 不替代输入项目的依赖。配置缺失须由 consumer 补齐。
已知 module 的文件输出须使用 module 外的新路径；未给 `-o` 的 transpile 仍输出 stdout。
具体正反例与边界见 [单文件恢复合同](docs/rfcs/0001-package-loading.md#a06-已知-go-module-的单文件外部恢复2026-10-06实施前合同)。

## 网页与浏览器 Playground

[构建独立 goxide-web 二进制](website/README.md)：中文特性目录、真实单文件与同包多文件 goxide → Go 对照与浏览器 WASM 转译。资源全部 Go embed，默认本机监听，不需要 Node、外部服务或运行时资源目录。浏览器支持一个用户包内的多个 `.gox` 文件及固定随附依赖，也提供单文件入口。完整生成 Go 按文件查看，依赖源码只读并标明真实来源。CLI 与 WASM 示例逐字节校验。

## 当前支持

| 能力 | 已验收范围与入口 |
| --- | --- |
| 声明宏 | 有限捕获、重复、嵌套、定义点绑定；源码包 `pub macro_rules!` 与 qualified 调用；[准确边界](https://github.com/goxide-lang/goxide/blob/bd90e606dcfa220b4054fe41c56bdc63a5687355/internal/macros/README.md) |
| 闭合 enum 与 match | 名义身份、穷尽匹配、guard、非法值在使用点拒绝；有限表达式组合，包括命名单结果、静态已知方法/函数字段/返回函数/variadic/原生 Go 显式 generic 调用；[match 范围](https://github.com/goxide-lang/goxide/blob/bd90e606dcfa220b4054fe41c56bdc63a5687355/internal/matchexpr/README.md) |
| Option / Result | 已登记具体实例、match、有限 `?` 组合、Go cause 链、显式 `go_result!` / `go_result_keep!`；跨独立模块恢复及透明 alias；[验收](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/t0-acceptance.md) |
| 显式协议 | Default、Clone、Debug、serde、PartialEq/Eq 的有限具体类型及跨包登记；已支持手写 Default 和具体 equality，不隐式赋予所有类型协议 |
| 用户关联协议 | 非泛型 `trait`、本包具体 struct/enum 的显式 `impl`、`U::Abc()` 与单协议约束的 `T::Abc()`；[范围](docs/static-protocols.md)、[示例](examples/static_protocol/README.md) |
| serde | core 协议 + 独立 JSON 实现；default/skip、具体 Option 缺失/null、闭合变体；[JSON 范围](docs/serde-json.md) |
| Go 互操作 | 单文件及现有 go.mod 中多文件/多包目录、独立模块 local replace、生成 helper 的原生 Go 消费；build tags/race 属于具体构建视图 |

Default 不改变 Go 零初始化；struct 要求全部字段满足 Default，enum 只要求显式选中默认变体的载荷。普通 Option 缺失/null→None 不依赖 Default。Clone 不改变普通 Go 赋值语义；enum 不是开放 C-style 数字枚举。

可直接运行 [构建脚本示例](examples/build_script/README.md)。显式代码生成、host helper/tool、许可与缓存的接口和正反例见 [宿主构建脚本 RFC](docs/rfcs/0023-build-scripts.md)。脚本仅在主动授权后运行；普通Go执行不构成OS沙箱。

## 已知失败与未完成边界

完整差距、范围依据、依赖与验收见 [MVP 逐 RFC 台账](docs/mvp-gap-audit.md)。以下是工程边界，不自动等于获批延期；具体正反例与验证见 [A06 单文件外部恢复](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/single-file-external-recovery-20261006.md)和 [A04 unary/composite 交付](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/match-unary-composite-20261006.md)，历史验收保留在 [T0 清单](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/t0-acceptance.md)。

- **表达式和类型上下文**：单值 match 已接通 `+ - ! ^` 及普通 struct/slice/array/map 字面量组合；多结果 `return match`、引用/收发一元、复杂 postfix/receiver、控制头和 block-arm、完全不可命名的私有参数仍拒绝。用户泛型函数新增单个关联协议约束子集；泛型协议、泛型 impl 和多协议组合不由此成为已支持能力。
- **模块与工具模式**：单文件恢复限配置已知、源可用、已有生成 Go 和合法 metadata 的依赖；不可导入的 internal-origin 增强操作、无源分发、预编译宏 ABI、`.gox` 测试文件与非宿主交叉构建仍有边界。单文件不隐选 TOML/workspace target/features；这些入口仍使用目录模式。切换 race/tags/平台不能直接复用另一视图的 metadata；[本试用的具体模式与已知拒绝](examples/enum_trial/README.md#运行与测试模式)。
- **协议资格**：手写 Clone/Debug/serde 登记、静态递归 Default、循环容器 equality 等仍有缺口或需另行设计；有限容器正例不证明任意对象图支持。
- **完整设计能力**：完整 trait、LSP、用户过程宏、格式 Value/PATCH/YAML 等未成为本阶段交付能力；不要把 RFC 示例当作已实现语法。

## 待用户决定

1. 公开函数是否在函数体前验证直接 enum 参数，以及非法值行为；当前使用点验证不等于统一入口验证。
2. blank 字段如何参与派生；embedded 字段的 Debug 名称、serde 嵌套/展开规则。
3. Proposed serde field adapters 的具体入口与自定义 `deserialize_with` 缺失策略。

既定方向和未定项以[决策登记](docs/design/decisions.md)及对应 RFC 为准，不将上述事项默认标为批准延期。

## enum payload 引用

引用类型为 `&T` / `&mut T`，取引用使用 `&value` / `&mut value`。`V(ref p)` / `V(ref mut p)` 绑定原 payload；`match &e` / `match &mut e` 为普通 payload 绑定提供相同的读写默认值。enum 和共享 Option/Result 使用各变体独立内联槽，普通赋值仍是 Go 浅复制。完整语义、Go API 和迁移见[原位 payload 合同](docs/design/enum-payload-borrows.md)。

`&T` 路径只读，`&mut T` 不承诺独占。普通引用可复制、保存、返回和捕获。本轮不新增生命周期、逃逸、借用冲突或跨变体失效检查；后续分析独立跟踪于 [#17](https://github.com/goxide-lang/goxide/issues/17)。

## 验收与设计导航

- [交付试用与逐条命令](examples/enum_trial/README.md)：本次新增示例的实际验证。
- [T0 验收矩阵](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/t0-acceptance.md)：真实 goxide→Go 与独立生成模块证据、已知失败及待决。
- [实现历史](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/IMPLEMENTATION.md)、[Roadmap](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/ROADMAP.md)：按阶段保留历史；旧限制以最新验收覆盖，不能截取早期条目代表当前能力。
- [逐特性 RFC](docs/rfcs/README.md)、[设计总纲](docs/design/overview.md)、[决策登记](docs/design/decisions.md)：区分已确认、暂定、未定。完整 trait 包装器/发布 ABI 等仍须按各自状态阅读。
- [宏与派生设计](docs/design/macros.md)、[enum/match 设计](docs/design/enums-and-match.md)、[serde core 与格式](docs/rfcs/research/0006-serde-core-and-formats.md)。
- [测试说明](tests/README.md)：`scripts/check_design.py` 验证文档中的 Go 机制，不能替代真实 goxide 编译执行。各轮全树 race/vet/设计检查与独立生成模块证据见验收矩阵。
- [冻结 MVP 文档基线](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/mvp-doc-baseline.md)：标签记录设计基线，不代表实现完成；本阶段不修改标签、不合并或发布。

## 许可证

[Mozilla Public License 2.0](LICENSE)（`MPL-2.0`）。项目暂称 goxide / Goxide，仓库名为 `goxide`。

`.gox` 统一用 `use` 绑定显式依赖根、`pub use` 重导出；包、类型、宏和派生路径用 `::`，值字段与接收者方法用 `.`。`mod child;` 只声明目录模式选中的本地子包。旧 `import`、包点号调用和未绑定裸序列化派生均拒绝；原生 `.go` 保持 Go 语法。见 [依赖根配置](docs/design/dependency-roots.md)、[静态路径合同](docs/design/static-paths.md) 和 [可运行示例](examples/static_paths/README.md)。

序列化派生归属 serde core：`use serde; #[derive(serde::Serialize, serde::Deserialize)]`。JSON 只实现数据格式；`serde::json::Serialize` 不是派生。显式 `use serde::Serialize;` 后可写 `#[derive(Serialize)]`，这里解析的是绑定身份，不是全局裸名白名单。

## 默认项目模式

build/run/test/transpile 省略目录时，从 cwd 向上发现 `goxide.toml` 与 workspace，go.mod 不阻挡发现。Cargo 风格字段声明 Git/path/Go 依赖、features 和命名目标，schema 见 [清单文档](docs/design/toml-manifest.md)。Go 仍负责 module 解析；不声称完整 Cargo 兼容。见 [可运行项目示例](examples/project/README.md) 和 [RFC 0022：语法、命令、锁定与限制](docs/rfcs/0022-project-toolchain.md)。

项目构建默认将产物保留在 `target/`：`goxide build --bin web ./app` 会打印二进制路径，`goxide transpile --bin web ./app` 会打印可独立 Go 构建的生成 module 路径。可用 `--target-dir` 或 `GOXIDE_TARGET_DIR` 配置；相对路径以项目根为准。见 [产物布局与安全边界](docs/design/target-layout.md)。

作者 workspace 核心支持显式成员和 `{ workspace = true }` 依赖继承、全工作区统一版本与根锁、按所选成员合并 feature。用 `goxide build --workspace ROOT` 或 `goxide run -p MODULE --bin NAME ROOT` 选择；成员资格不自动提供依赖。见 [可运行 workspace 示例](examples/workspace/README.md) 与 [实现范围及限制](docs/rfcs/0026-workspaces.md#本次实现范围)。

`go = "模块路径"` 加 `version` 渠道可直接取得带 `goxide.toml` 的源码并统一 features；Go 负责共享 MVS 与 sums，稳定后才生成/执行已授权脚本。仅原生 go.mod 不隐式激活 goxide feature；固定配置 Go 发行物不重配。见 [version 来源合同与验收](docs/design/version-source-plan.md)。

项目采用 [统一项目模型](docs/rfcs/0025-unified-project.md)：根以 `goxide.toml`（`package.format = 2`，显式 module/go）和 `goxide.lock` 为配置与锁定权威，`.go` / `.gox` 可共存。原生 require/replace/exclude 写入 TOML；Go 元数据仅生成到 target。普通 Go 工程可用 `goxide import-go --plan --bin app=cmd/app ROOT` 查看方案，再用 `--out NEWDIR --offline` 在新目录验证迁移，原工程保持不变。旧 JSON、format 1、双根配置和旧锁均明确拒绝；不自动删除或改名。 `import-go` 未指定输出时默认只读计划；原生 Go vendor 仅是迁入输入，验证后物化为显式本地模块，保留修改字节、资源和许可；Goxide 没有 vendor 构建模式。旧 vendor 清单/锁须从原工程显式重新迁入，详见 [供应源码迁入合同](docs/design/vendor-local-modules.md)。

共享 sum 与 serde 已迁入独立 Go 模块：`github.com/goxide-lang/std/sum`、`github.com/goxide-lang/serde`、`github.com/goxide-lang/serde/json`。本 checkout 的 go.mod/go.sum 固定真实公开提交；不是未发布候选 replace。具体版本、发布白名单和整合验收见[迁移记录](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/public-library-migration-20261004.md)。当前 enum metadata 为 9、sum ABI 为 3。旧 interface/case 产物须全部重新生成；不保留旧 case 类型或 marker 转发层。

格式化写入与赋值传播支持也由 `github.com/goxide-lang/std/formatting` 和
`github.com/goxide-lang/std/runtime` 提供。旧 compiler 支持包已删除，项目须更新
std 并重新生成；不保留旧路径转发。单文件 CLI 的 `--runtime` 选择支持依赖清单，
生成模块只包含公共支持库的实际依赖图，不再注入 compiler 模块。格式字符串检查
仍属于编译器，正常 Go 互操作及卫生类型保护保持原语义。见[支持库迁移合同](docs/design/support-library-migration.md)。

供应模块来源选择：真实终端迁入默认展示一次模块清单，空行保留全部 local；非终端默认 local，不等待读取 stdin。重复 `--source MODULE=local|remote` 可确定性选择清单证实的精确版本；`--plan` 只显示请求，不联网比对。remote 与供应字节有差异时须逐模块确认，非终端使用 `--accept-source-diff MODULE`；所有原供应字节仍保留于输出本地模块和审计。`--offline` 的 remote 选择只用已缓存完整包，缓存不足失败，不访问代理、sumdb、Git 或下载工具链。verified 仅证明供应文件集合相等，不证明完整发行物等价；不猜 Git URL，不支持任意远端或 registry 搜索。

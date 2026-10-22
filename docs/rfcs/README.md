# goxide 逐特性 RFC 与转译约束

每项新特性在这里单独评审：先看 goxide 输入和 Go 输出，再看必须保持的行为、拒绝例子及双向互操作。本文档目录是设计约束入口；[设计专题](../design/overview.md)保留背景与取舍，[决策登记](../design/decisions.md)维护确认状态。

当前逐项对照见 [MVP 差距台账](../mvp-gap-audit.md)：状态以实际子集与测试为单位，历史 Proposed/未实现不覆盖后续验收，也不把工程边界自动归为延期。

深入阅读从[语言设计入口](../design/reading-guide.md)开始：[词法与文法](../design/language-grammar.md)定义 token 与 EBNF，[编译与转译语义](../design/translation-semantics.md)给出阶段函数、判断式和 Go 降低。RFC 0008、0009、0011、0022 的形式模型分别说明变体构造、分派、宏展开与配置选择；每篇保留原有输入/输出对照及设计取舍。

## 使用规则

1. 新增或修改语言能力，先新增或更新对应 RFC。使用[模板](TEMPLATE.md)，不要只在总纲里增加一句功能描述。
2. 每篇明确区分已确认目标、暂定语法/ABI、未定问题。实现分支已有有限 goxide 端到端子集，准确范围见实现记录；有 RFC 不代表该篇所有能力已实现或整个方案已经批准。
3. 每个正例给 goxide 输入、Go 输出、预期结果及保持的性质。涉及外部包时写明生成文件属于哪个包。标明片段、伪代码、候选表示，不能伪装成已可独立编译的输出。
4. 反例注明由 goxide 前端拒绝、Go 编译器拒绝，还是运行时检查拒绝。goxide 拒绝的程序没有合法的生成输出，可列错误降低以说明禁止原因。
5. 每篇说明 goxide 调 Go 和 Go 调 goxide。编译期能力无需对应 Go 语法，但要说明其生成 API 如何使用。
6. 约束是可观察语义，通常不要求生成文本逐字一致。更换表示、消除 wrapper 或内联均不能改变选中实现、求值次数、别名、复制和控制流。
7. 例子使用稳定的局部编号，例如 `TR-01`。未来前端测试按编号绑定正例、拒绝例和行为检查；只有手写 Go 机制验证时单独标识。
8. 已确认语义与候选 ABI 冲突时，记录反例并修订 ABI。不能为了保留某段生成代码，暗中删掉源级功能或更改已确认语义。
9. 规则先定义输入域、环境与输出构造，再用判断式、等式或伪代码给出转换；对每项前提说明去掉它时的反例。普通文字解释规则，不替代规则。公式是设计契约，只有实际完成的证明才能称为已证明。

## 特性清单

| RFC | 独立特性 | 对照重点 |
| --- | --- | --- |
| [0001 包加载与发布](0001-package-loading.md) | `.go` / `.d.gox` / `.gox`、dist | 同一个 Go 模块；本地 wrapper 的完整发布 |
| [0002 trait](0002-traits.md) | 显式实现、限定分派与期望类型适配 | 统一 wrapper、marker、一次求值 |
| [0003 declare impl](0003-declare-impl.md) | 原生方法采用与声明合并 | 声明归属、生成包、重复与冲突 |
| [0004 关联类型](0004-associated-types.md) | 定义、绑定与投影 | 固定 Go 接口、实现证据 |
| [0005 引用与句柄](0005-borrows.md) | 读写借用、Arc、解引用与再借用 | 相同指针表示下的不同权限 |
| [0006 原生调用](0006-foreign-api.md) | declare func、unsafe、Ptr / AsPtr | 调用与函数值、可信契约 |
| [0007 锁回调](0007-locks.md) | Lock / RWLock、Unit | 回调权限、泛型结果、解锁时机 |
| [0008 变体枚举](0008-enums.md) | enum、Option / Result | 变体身份、空值、初始化与 ABI |
| [0009 模式匹配](0009-match.md) | match、guard、值与借用模式 | 原存储、穷尽、控制流与顺序 |
| [0010 Vec 与视图](0010-collections.md) | 动态序列与 slice 借用 | 复制、别名、扩容与初始化 |
| [0011 声明宏](0011-macros.md) | 模式、模板与卫生 | 展开、符号身份、重复求值 |
| [0012 格式化](0012-formatting.md) | print / println 与格式检查 | 参数求值、格式协议、I/O 行为 |
| [0013 内建派生](0013-derive.md) | Default / Clone / Debug | 静态 Self、字段协议与复制成本 |
| [0014 语法工具](0014-syntax-tools.md) | quote / syn | 语法数据、上下文与权限来源 |
| [0015 公开边界](0015-public-api.md) | pub 与公开输入 | 导出命名、运行时检查与 Go 保证范围 |
| [0016 safe std](0016-safe-std.md) | Go 标准库的声明与包装 | 实际契约、部分成功与适配成本 |
| [0017 Map](0017-maps.md) | 多种 Map 实现与共同协议 | 查找、元素借用、后端差异 |
| [0018 转译对应约束](0018-lowering-contract.md) | 全特性的共同降低规则 | 副作用、控制流、元数据与源码映射 |
| [0019 统一 trait wrapper](0019-uniform-trait-wrappers.md) | 当前 trait 表示与 Go 便利入口 | marker、包级工厂、本地 As 与外部 ForT.As |
| [0020 类型关联函数](0020-associated-functions.md) | impl 成员、T::method 与构造函数 | 原生 Go 声明边界、Self/self、Go 包函数与方法、泛型与初始化 |
| [0021 use 与 pub use](0021-reexports.md) | 目录包公开API聚合 | canonical身份、变量存储、函数defer、合法Go路由与metadata |
| [0022 feature 与项目工具链](0022-project-toolchain.md) | feature、cfg/cfg_attr/cfg!、依赖来源与目标 | 新管线已支持 feature：顶层条件属性先选择，cfg! 只查布尔值；原生 Go/TOML 配置隔离；target/env、双模式目标与统一快照已实现，test 不开放 |
| [0023 宿主构建脚本](0023-build-scripts.md) | 显式生成、本地host依赖、信任与声明缓存 | 未授权拒绝、原子产物、无沙箱边界 |
| [0025 统一项目与迁入](0025-unified-project.md) | TOML/lock唯一依赖权威、Go导入与target模块 | 无损指令子集、staging验证、source/Go发行边界 |
| [0026 workspace](0026-workspaces.md) | Cargo风格成员/继承/锁与Go多模块 | 作者核心、根锁及受限go.work迁入已实现；vendor仅作输入转显式local模块；统一host resolver等扩展仍待设计/实现 |
| [0027 正式命名](0027-goxide-naming.md) | Goxide / goxide / .gox / GOXIDE_* | 已完成命名与ABI迁移，旧输入/产物明确拒绝并重生成 |
| [0028 crate 与路径根](0028-crate-roots.md) | lib/bin/example、Go 混编、依赖别名与标准库 fallback | 已实现；各 target 独立 crate，删除全部 namespaces，保留 alias 根并按真实 Go 子路径解析 |
| [0030 过程宏基础草案](0030-procedural-macros.md) | TokenStream、卫生、登记、依赖角色、WASI 宿主 | Draft；核心调用、宿主及依赖角色已实现；业务宏迁移与浏览器物理执行硬截止待完成 |
| [0031 普通库协议与条件 impl](0031-generic-protocol-impls.md) | 泛型/条件 impl、多约束、按需实例化与 Go 普通入口 | 核心能力、最小数组长度及 enum 检查已实现；普通库迁移基础已验证，Default/Eq 规则调整暂缓 |
| [0032 From / Into](0032-from-into-conversions.md) | 多来源转换、参数化 trait、实现归属/冲突、一步传播及 Go 表示 | 完整设计提案；Go 机制已验证，语言与 std 新协议未实现 |

RFC 0024 use路径仍在[独立提案分支](https://github.com/goxide-lang/goxide/blob/5e4b118f25262d8022d1b8f72575d89d69f096aa/docs/rfcs/0024-use-paths.md)，该历史提案未整体批准。当前 `crate::` 及根选择合同见 RFC 0028；其他未确认的路径语法不由此开放。

RFC 0029 声明式 derive / attribute 原型与草案保留在本地 `draft-rfc/declarative-derive-attributes` 分支，暂不合并主线；RFC 0030 从主线独立设计，不以接受该提案为前提。

## 范围与验证

LSP、自举、构建缓存等工程架构继续由[工具链专题](../design/compiler-and-tooling.md)管理；凡引入新源级行为或改变产物契约，仍需相应 RFC。Box、supertrait、GAT、特化、用户过程宏、锁块等未定或延期能力列在[决策登记](../design/decisions.md)，启用前必须另立 RFC，不能从名字补出未经确认的语义。

运行 `python3 scripts/check_design.py --docs-only` 检查链接、围栏及 RFC 基本结构。完整命令继续验证原有手写 Go 机制；不会自动编译这里所有 Go 片段，也不替代已有 goxide 前端的接受测试。每篇设计验收表不自动构成通过记录；实际子集见源码、测试和阶段报告。

## 研究补充与同步边界

早期研究 RFC 0001–0006（含诊断业务补充）保留于 [research](research/0001-scoped-borrow-views.md)，与本目录逐特性 0001–0020 分开编号；原有链接已重定位。目录根的编号特性 RFC 继续接受唯一编号、六章节与 goxide/Go 示例检查；研究文档仍检查链接和围栏，不冒充已冻结特性规范。远端独立决定编号记 R19–R26，见决策登记。

所有借用/效果相关 RFC 受 D19/D20 约束：无法证明独占/不逃逸或缺少摘要，不是默认准入拒绝的依据；风险分析不属 MVP。基础类型权限和显式 unsafe 规则不降级。统一 wrapper（0019）和关联函数（0020）方向已同步；手写 Default/具体 equality 已有有限入口，对应通用 trait/固有 impl 前端仍未完成。

# Hyper Go（hgo）

hgo 是面向 Go 生态的语言设计：增加显式 trait、变体枚举、模式匹配和卫生宏，生成标准 Go 源码，并提供共享语义分析前端的 LSP。

架构以 **转译器 + 转译前 linter + 普通 Go 支持库** 为边界。最低目标语言及工具链版本为 **Go 1.27**，使用其新增的泛型方法表达带独立返回类型的锁回调等 API。

当前仓库处于设计阶段，尚未实现编译器或 LSP。文档中的 hgo 代码用于规定和讨论语义，不代表已有可运行语法。

项目暂称 hgo / Hyper Go，当前远端仓库名为 `goxide`。换机器继续时先看 [Roadmap](ROADMAP.md) 和[决策登记](docs/design/decisions.md)：分别记录实现进度，以及**已确认 / 暂定 / 未定**的设计边界。

用户在自己的 C 包 `.hgo` 文件中写 `declare impl AorB::XXX for BBB::Type;`。声明只由 hgo 读取，所需 marker 仅加入本次 hgo 的隔离构建视图；不修改 AorB、BBB 的原源码或共享缓存，既有纯 Go 项目完全保持原样。

## 设计文档

- [设计总纲](docs/design/overview.md)：目标、决策状态、各部分边界与实现顺序。
- [编译器、LSP 与自举](docs/design/compiler-and-tooling.md)：Go 工具复用边界、独立前端、启动实现与自举路线。
- [指针拆分与引用](docs/design/pointers-and-borrows.md)：Option、读写借用、Arc、Ptr、Box、解引用及尚未闭合的权限规则。
- [显式 trait 与多接口实现](docs/design/traits.md)：无体声明补充 marker、自定义 wrapper 实现、同名方法与使用处转换。
- [Go原生类型的显式适配](docs/design/go-interop.md)：普通 `.hgo` 中的显式声明、原生方法校验、伴生源码与独立构建视图。
- [原生 Go API、安全声明与裸指针](docs/design/foreign-api.md)：原生 unsafe 视图、declare、Ptr/AsPtr 和 `.d.hgo` 声明分发。
- [关联类型](docs/design/associated-types.md)：普通关联类型的最小设计及 Go 降低方式。
- [锁与作用域回调](docs/design/synchronization.md)：指针形式的锁句柄、Go 1.27 泛型返回值、解锁和 linter 边界。
- [变体枚举与模式匹配](docs/design/enums-and-match.md)：Option / Result、穷尽匹配、值型 case、Go 零值与边界验证。
- [卫生宏与基础派生](docs/design/macros.md)：参考 Rust 的声明宏、print / println、Default / Clone / Debug、符号卫生和 Go 降低；用户过程宏延期。
- [基础包与 safe Go 标准库](docs/design/standard-library.md)：针对现有 Go std 的逐 API 声明与包装，以及泛型 slice 的权限、复制和别名。
- [quote / syn 与语法工具](docs/design/syntax-tooling.md)：额外语法包、普通代码生成工具、上下文来源和后续过程宏的边界。

总纲负责导航与记录决策，详细文档负责语义、示例、诊断和验收条件。新增专题时从总纲链接，避免把完整设计重复写在多个位置。

## 验证设计示例

```sh
python3 scripts/check_design.py --docs-only
python3 scripts/check_design.py
```

完整验证需要 Python 3.10+、Go 1.27 或更新工具链及可用的 race detector。范围与限制见 [tests 说明](tests/README.md)：验证的是文档中的 Go 机制，不能证明尚不存在的 hgo 前端正确。

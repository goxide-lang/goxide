# T0 独立复核（2026-10-04）

独立复核者审查 `macro/`、`integration/` 的真实源、生成结果、原始日志和建议。基线现场确认为 `3098a30e8e5fcb4696687246b2f39988218f4006`。本轮只写本文件，未修改生产代码、切换分支、推送或合并。仓库内未找到 AGENTS.md / .agents/skills，`/workspace/.agents` 为空。

## 结论与需要收紧的表述

1. **primitive alias 静默重绑定成立，是正确性缺陷。** 从当前 HEAD 重新构建 CLI、重新转译两个 `.hgo` 样例，再执行生成 Go 的 `run -race`，独立得到 `enabled=false disabled=true`、`value="\x00" type=main.int`。调用者的顶层 `true/false/int` 声明是合法 Go；宏定义包没有这些遮蔽，定义点承诺要求保留原语义。`internal/macros/README.md` 明确承诺 primitive definition-site aliases，因此不能用通用“宏仍实验性”或 len/cap 的显式限制注销此缺陷。constant 样例 vet 通过，type 样例 vet 报单 rune 转换；race 通过不代表语义正确。两例应合并计一个缺陷。

   最小防错诊断建议合理；须同时核对定义点绑定身份，不能假定所有同名标识符都是预声明名称。若后续采用定义包 helper，须保持原 Go 类型身份和 import/可见性规则；不得靠重命名用户 named type 解决。生成无名字布尔表达式仅适用于已经确认预声明 true/false 的情况。

2. **Result 逐项分配及静态调用改善成立，但“经两层函数变量”应解释为初始化别名链。** 两个 `var` 的初始化复制同一个函数值；运行时调用点只有一次通过函数变量的间接调用，并非每项调用两个中间包装器。`direct_calls.py` 保留中间 var，只把使用点的 alias 改成直达原 validator 的具名转发函数，同时替换 Mode validator。它不是把所有 imported validators 的两层变量都修复了。

   我核对生成文件 diff 仅这两处；`Sum` 不使用 Mode，所以 Mode 改动不能解释 Sum 的 allocation 降幅。原始和 direct 两版完整 app `test -race -count=1 ./...`、`vet ./...` 均独立通过。单 P、优化构建、100ms × 2 的 1024 项复跑如下：

   | 版本 | ns/op（两次范围） | B/op | allocs/op |
   | --- | ---: | ---: | ---: |
   | 原始 hgo | 13213–13588 | 8200 | 1025 |
   | direct hgo | 2064–2182 | 8 | 1 |
   | 原始进程 native | 723.5–724.4 | 0 | 0 |
   | direct 进程 native | 813.5–834.0 | 0 | 0 |

   native 数值差说明墙钟对照有环境噪声，不宜发布固定倍数加速承诺。native `(int,error)` 与 Result 的 ABI、校验和内联机会不同，只是用户成本参照；**原始/direct 的同 ABI 差分**才是此修复建议的关键因果证据。保留 validator、使用唯一包级具名函数及已可信具体类型的建议不要求改变 enum ABI。现有 direct 副本没有覆盖所有非法 sum、类型遮蔽及不可命名类型，不是已经完成的通用编译器补丁。

3. **宏 imported-selector 检查存在 O(N²) 子过程，不能把整个 Expand 的时间差都归因于它。** `hygiene.go` 外层每遇 imported selector，就无条件完整 `ast.Inspect(root)`；找到调用后也未停止，N 个 selector 与线性大小 AST 直接构成二次扫描。预收集 `CallExpr.Fun` 集合再查指针身份保持原语义；必须保留原 export/type/generic 校验。现有 calls 集合只有 `*ast.Ident`，应扩展为 `ast.Expr` 或另建集合，不能照搬其键类型来查 selector。

   独立复跑现有 isolated benchmark（单 P、`-benchtime=1x`）在 2048 项得到 local 63.6ms / 84.9MB、imported 538.3ms / 104.1MB。128 项 imported 49.2ms、512 项 46.3ms 也说明小样本包含导入解析/首次缓存等成本。这个 benchmark 的 helper 内容不同，但此处不执行 helper，主要混杂是 Go export-data/import resolution 及其它 expansion 工作。源码足以证明二次扫描；要发布修复收益，需同一 imported 输入在仅替换扫描算法的前后对照，当前未做。不能称生成 Go 的运行期开销或宣称整个 expander 只有这个复杂度来源。

## 发布与回归建议

可以诚实展示这些实际 `.hgo` 到生成 Go 的组合：同模块包宏条件日志、显式 once 绑定、enum/match、跨独立已生成模块的 Result `?`、thiserror 原因链、Option 与 nil/empty 的显式区别，以及 fixture 中的 Default/Clone/Debug/PartialEq/Eq、serde JSON。表述为“本地组合案例已通过”，不承诺所有类型/场景、稳定宏 ABI、零成本或 CI 已通过。

宏应标明有界原型：没有隐式 once 求值、不是完整 Rust 宏、预编译跨模块宏分发不在该能力范围。双模块业务成功不等于宏本身可以跨预编译模块分发。当前 primitive shadowing 需公开限制或先修复；跨模块 Result 存在已测得成功路径分配开销。Clone 深复制证据限于 fixture 支持载荷；普通 Go 赋值仍是现有浅复制语义。

最高价值回归门槛：primitive 定义/调用点同名遮蔽及诊断位置；保留校验的跨模块非小整数 `?` allocation；非法/typed-nil sum 与错误链身份；大 repetition imported selector 的扫描计数或可重复复杂度检查。不要把不稳定墙钟耗时当唯一门槛。

## 本轮执行范围

工具 `/workspace/goxide-environment/go/bin/go`，`GOCACHE=/tmp/t0-gocache GOPATH=/tmp/t0-gopath GOTOOLCHAIN=local GOPROXY=off GOWORK=off`。新重建宏 CLI `/tmp/t0-independent-hgo`；新生成复现 `/tmp/t0-independent-shadow-{constants,types}`。Result 复核复用作者生成的 `/tmp/t0-integration-final/app` 和 `/tmp/t0-integration-final-direct`，并现场核对二者 diff；没有独立重新转译整套双模块源。执行了上述 race/vet、短 benchmark；未另跑 fuzz、全树测试、跨平台或 CI，也未验证正在另分支开发的适配宏。无下载、无权限拒绝或剩余执行阻塞。

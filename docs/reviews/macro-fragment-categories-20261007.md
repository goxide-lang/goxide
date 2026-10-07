# A01 模板明确类型槽：精简交付记录

宏模板现在在替换捕获内容之前，按 AST 的类型／表达式角色检查占位符。`expr`、
`ident` 和有限 `tt` 捕获进入变量类型、函数签名、组合类型、断言、类型 switch 或
多实参泛型实例的类型实参时，在准确模板 token 处拒绝。数组长度仍是表达式，
其中模板写出的类型槽继续检查；完整捕获 `make([]Local,2)` 保留调用点类型和行为。

规则见 [RFC0011 的 FC-01–10](../rfcs/0011-macros.md#a01-模板明确类型槽的捕获类别检查2026-10-07实施前合同)，
实现为统一[角色检查](../../internal/macros/fragment_categories.go)，沿用已有
allocation 门禁和诊断来源；没有按名称特判、改名用户类型或增加 wrapper。

## 提交与范围

| 身份 | 完整提交 |
| --- | --- |
| 基线 | `2804d655a7660479839a0d9741319afdd19151f6` |
| 实施前合同 | `1442ae1e5fbe27f73be2fa2e3227fdc668865f2a` |
| 初版修复 | `81eeb2846341ab892ab63618593b6057689684c9` |
| FC-10 补充与最终生产 pin | `afa3f0a52b2d8206d75df38d10833e537ea7a0dd` |
| 完整验证的源码／工具 | `35221fe9f5cb49b152ea5faad9529dbefa14cd69` |

126 个生产输入在最终 pin 与验证提交之间逐字节一致，摘要为
`2412f1a252c1dd195d1a4d5deed55e2dcd605e65d18edb0b838da36b1976203c`。
后续报告提交另记，不冒称完整门槛当时执行的 HEAD。计数、版本与日志哈希见
[精简摘要](macro-fragment-categories-summary.json)。本批只作本地提交和审查，未 push。

## 失败、修正与完整门槛

独立审查最初 22 项有效检查通过，扩大检查后发现初版漏掉 `IndexListExpr`：
4 个类型捕获反例仍可通过真实 CLI 并运行原生 Go。先补充 FC-10 合同，再修复
多实参的明确类型角色；同组 4 个反例准确拒绝，3 个正控和另 6 项相关回归通过。
这 13 项增量与原检查共 29 个不同用例，不宣称最终提交重新跑了全部 29 项。
仓内回归随完整门槛再次执行。初始独立 nil 夹具和 CLI `pub func` 夹具曾先触发
既有语法限制，修正为本片实际支持的输入后单独重验，未据此放宽生产规则。

第一次全树 race 退出 1：macOS 默认临时路径含 `/var` 符号链接，触发已有路径
保护及 raw/canonical 身份比较，共 131 个叶失败、147 个含父项的失败测试事件。
没有改动生产保护；改用仓外可写物理路径。第二轮已通过 17 个测试包、615 项顶层、
1603 项子测试后，执行器会话消失且无终态；核查未发现残留测试进程或 Go 超时记录。
该轮记为未完成，不算通过。保留两轮日志后，同一干净提交重新执行完整 race。

| 门槛 | 最终实际结果 |
| --- | --- |
| Mac 全树 race | 631 项顶层 / 1610 项子测试通过，零失败、零测试跳过；18 个测试包、5 个无测试包 |
| 全树 vet / design | 通过；design 含 13 条 Go 机制命令及预期拒绝 |
| 原生 CLI | 生成模块 race/vet/run，重复输出、LF/CRLF、嵌套跨包、捕获原类型及 named-type/alias 边界通过 |
| 网站 CLI/WASM | 七阶段通过；117 fixture / 11 示例，原 97/10 与 A04 增量输入保留 |
| 网站旧／新行为对照 | 固定旧基线 `fd8380c` 对最终 `afa3f0a`：33 对完整 Go AST 相同，27 对字节相同、6 对不同；5 对原生程序行为相同 |
| A01 网站增量 | 12 fixture；CLI 两批各 48 次，UI 24 次＋恢复 24 次，真实 Worker 48 次＋恢复 48 次；2 个当前版本原生程序输出匹配 |
| 独立审查 | 代码、绑定、准确来源、最终网站原始记录与待交付对象分别复核 |

网站保留原 52 次诊断观察和 A04 的独立增量。A01 的当前版本结果不冒称旧基线也
支持；浏览器不加载任意 module。Worker 的本地 QA 记录新增完整性、输入和失败
字段，以保留失败前观察；公开 Worker 协议和 std/serde ABI 未变。UI 日志证明
输出恢复与拒绝时清空 Go／禁用复制，不证明成功时旧诊断 `textContent` 已清空。

仅有 Mac 本机证据，Linux、托管 CI、网站部署均未执行。复用既有工具与完整
离线依赖缓存；没有安装工具或下载依赖，也不把构建缓存复用说成全冷缓存。

## 复现与证据

使用仓内[宏单元用例](../../internal/macros/fragment_categories_test.go)、
[诊断用例](../../internal/macros/fragment_category_diagnostics_test.go)、
[真实 CLI 验收](../../tests/acceptance/macro_fragment_categories_test.go)及
[网站完整入口](../../website/validation/README.md)。配置可写 build cache、完整依赖
缓存，并把 `TMPDIR` 设为仓外可写物理路径；全树测试须保留夹具自带的 workspace。

```sh
env -u GOWORK GOTOOLCHAIN=local GOPROXY=off go test -json -race -count=1 -timeout 30m ./...
env -u GOWORK GOTOOLCHAIN=local GOPROXY=off go vet ./...
python3 -B scripts/check_design.py
python3 -B website/validation/run.py --help
```

网站参数按其 README 配置；帮助命令本身不是验收。原始日志、截图和大型记录留在
仓外，摘要仅保存身份、计数和哈希。暂存后、提交前运行
`python3 -B scripts/check_review_artifacts.py`；不复制原始证据目录入库。

## 保留边界与后续合同

实际遮蔽模板 builtin 的调用点 `type len int` 或局部 alias 仍触发已有工程边界
拒绝；无关作用域、声明生效前的调用、定义点真实函数及完整捕获保持正常。
这不是已批准延期。简单改名 alias 可能改变匿名字段名、反射或提升方法集，
不能用改名换取放行。普通调用与单 `IndexExpr` 的语义歧义也不凭名字猜测。
普通实参类型、arity 及目标上下文仍由最终 Go 检查；CLI/WASM 转译成功不代表完整
Go 类型检查。FC-10 使用原生 Go helper 验收，未开放 `.gox` 泛型声明或自由泛型 helper。

后续须分别明确通用 type fragment 的类别／绑定／可见性合同、固定定义点命名或
导入类型的分配支持，以及能证明字段和方法身份不变的局部类型卫生方案。
本片不暗中扩大这些合同；A01、A03、RFC0011 与 MVP 整项继续开放。

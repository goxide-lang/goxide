# 修复独立复核（2026-10-04）

这是实际修复的第二轮独立 review，与 `INDEPENDENT_REVIEW.md` 的基线发现复核分开。基线仍为 `3098a30`；审查该基线上当前工作树的 Result forwarding 与 macro primitive collision 修复。复核者未修改生产代码、测试代码，未切分支、commit 或 push。

## 结论

**当前复核版本无剩余阻塞发现。** 初轮发现一个真实 EOF 拼接回归，已由修复方修正，独立复跑确认通过。宏修复将危险绑定改为明确诊断，不宣称全面支持冲突场景；Result 修复保留 producer 具体校验和公开 ABI，消除了此已测场景的逐项逃逸。

### 已发现并修正：无末尾换行源文件的 prelude 拼接

首轮 `TestCrossModuleBuiltinArtifacts/constrained-alias-ctor` 实际报 `expected ';', found 'type'`，没有返回预期的 `does not satisfy`。删除 `imported_family.go` 的 validator var 循环也删除了该 prelude 原来的前导换行；`compile.go:276` 直接 append，导致源末尾 `}` 与生成 `type` 黏连。有效输入同样可能受影响，不能只放宽负例断言。

修复方现于 `var b strings.Builder` 后显式 `b.WriteByte('\n')`；acceptance 的 `appFor` 对全部 consumer 源执行 `TrimRight(source,"\n")`，有效正例也验证没有 EOF newline。第二轮该负例恢复约束诊断、有效 consumer 完成转译、allocation、race/vet。此项已关闭。

### Result：绑定、可命名性、深层校验

- `imported_family.go` 现在只登记 `origin.Validator`，没有新增信任来源。既有 artifact 校验仍要求 producer 中的真实公开 `*types.Func`、正确 signature/family；typed pass 又按 package/nominal identity 和具体参数身份选择实例。
- `bindImportedFamily` 先生成包级具体类型 alias，再把该 alias 用作具名 forwarding function 参数；目标仍是 producer 的具体 `HgoInst...`。调用者函数内部的 `m := ...`、同名 payload 局部不会进入 helper 的包级作用域。生成名称受既有 namespace/serial 与 `__hgo_` 保留标识符规则保护。
- `typeSpelling`、透明公开 alias 与不可命名类型检查未删除。复跑覆盖 reordered/fixed/constrained aliases、private payload 经公开 alias 的合法访问，以及无公开 alias 的拒绝。局部 import shadow 的现有 `Shadow` 正例通过。
- `nil`、typed-nil、非规范 case pointer、forged embedded sum、producer 的非法嵌套 enum 仍在原校验点 panic；guard 副作用不得提前发生的断言通过。错误 sentinel、typed-nil error、显式 From 路径保持原结果。
- 没有修改 enum representation、constructor 或 producer validator 实现。独立 enum 的 `internal/frontend/imported.go` var bindings 尚未在本修复内去除，不能宣传为所有 enum 调用均静态化。
- 新 allocation gate 使用非小整数、优化构建与 size 1/32/1024，允许至多一个最终 Result 分配；独立复跑通过。它按 Go 1.27 工具链优化结果设门槛，不是任何工具链/载荷的语言层零分配保证。

### Macro：保守诊断与正常绑定

审查 `primitive_bindings.go`、`hygiene.go`、`sums.go` 及对应单元/acceptance 测试：

- 普通 AST hygiene 在 placeholder、模板局部对象、定义点函数分支之后才检查 primitive，故显式捕获与合法模板局部不会按拼写误判成预声明对象；定义点非泛型同名函数仍走原函数绑定。
- 检查同时覆盖定义点声明/导入、调用包声明及调用文件 import。调用文件 scope 显式取 `e.pkg.scopes[e.file]`；其它文件的 import 不误算当前 alias 的作用域。隐式 import 的实际 package name 通过现有 Go importer 判定。
- `prepareSums` 在 fixed builtin 类型语法校验之后扫描参数 token，补上 `Option/Result` 类型在 AST hygiene 前被替换的缺口，包括嵌套 slice/map payload。它没有开放任意 named type 或改变 enum ABI。
- caller-local `type int string`、`true` 局部和显式 `$t:ident` 捕获的正例独立通过，reflect name 仍为 `int`。没有为解决冲突而重命名用户类型。
- 冲突报错包含定义和调用位置；失败不返回可用展开输出。当前行为是显式工程限制，不是已经实现所有 definition-site primitive binding。二次 AST 扫描性能发现仍未修复，不应与此正确性补丁混为一谈。

## 独立执行证据与边界

真实 Go：`/workspace/goxide-environment/go/bin/go`；缓存 `/tmp/t0-gocache`、GOPATH `/tmp/t0-gopath`、`GOTOOLCHAIN=local GOPROXY=off GOWORK=off`，PATH 首项亦指向真实 toolchain。

已执行：

- `go test ./internal/propagate ./internal/frontend -count=1`：通过。
- `go test ./internal/macros -run 'TestPrimitive' -count=1`：通过。
- `go test ./tests/acceptance -run '^(TestCrossModuleBuiltinArtifacts|TestMacroPrimitivePackageCollisionsAreLocated|TestMacroPrimitiveLocalShadowsAndExplicitCapturesRace)$' -count=1 -v`：修正前发现上述回归；修正后完整通过，18.236s。内部真实生成 Go 的 producer/consumer/plain race/vet 与 allocation gate 均执行。最终完整日志 `/tmp/t0-independent-fix-acceptance.log`。
- `git diff --check`：通过。

本轮没有独立运行全树 race/vet、长时间 fuzz、跨平台或 CI；主任务的这些结果应单独引用，不能归入本独立复核。本轮未复测整个业务 benchmark 的墙钟加速，只验证正式 allocation gate 和语义回归。无权限拒绝或执行阻塞。

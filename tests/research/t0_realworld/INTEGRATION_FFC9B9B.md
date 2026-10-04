# 与 Go Result 适配宏提交的集成检查

2026-10-04；再次 fetch 后确认 `feat/t0-completion` 为 `ffc9b9b501fbccc56c73794a05eaeb427acd2e2e`。本修复分支快照为 `999d6aafdc4c1e474ac01eca5e21993407f5ed50`，已推送并核验。网站分支没有被 checkout、修改或合并。

## 无文本冲突，保留新宏

对这两个准确提交执行 `git merge-tree --write-tree --messages`，退出 0；合成 tree 为 `066f0300519d8ebaac73bf75f28d0840e9a322c0`，完整输出见 [merge-tree.log](results/integration-ffc9b9b/merge-tree.log)。没有 merge commit，也没有更新 feat 分支。

共同修改且自动合并成功的文件：

- `internal/macros/hygiene.go`：适配宏新增 `__hgo_go_result` marker 处理；本修复在后面的 primitive alias 生成前检查绑定冲突，两个逻辑同时保留。
- `internal/propagate/lower.go`：适配宏新增 Lower 入口 marker 检测和 `checkGoResults()`；本修复修改 `bindImportedFamily` 的 validator 生成，两个逻辑同时保留。
- `internal/macros/README.md`：分别新增适配宏能力和 primitive 冲突诊断边界。

之前提示可能重叠的 `internal/macros/sums.go` **不在 ffc9b9b 的 diff 中**，因此这次没有该文件冲突。适配宏的新文件 `internal/macros/go_result.go`、`internal/propagate/go_result.go` 及其测试没有被本补丁修改。`go_result!` / `go_result_keep!`、error-only 形式和 `struct{Value T;Err error}` 语义保持。

## 临时合成树验证

在新的 detached worktree `/tmp/t0-integration-ffc` 中，以 ffc9b9b 为 HEAD，用 `git read-tree --reset -u` 检出上面合成 tree 的内容与 index。只验证临时快照，不产生集成提交，不修改任何目标分支。

真实 Go 1.27.1、GOPROXY=off/GOTOOLCHAIN=local，已执行并退出 0：

```sh
go test -race -count=1 ./internal/macros ./internal/frontend ./internal/propagate
go test -race -count=1 -run '^(TestGoResult.*|TestCrossModuleBuiltinArtifacts|TestMacroPrimitive.*)$' ./tests/acceptance
go vet ./...
```

[三包 race](results/integration-ffc9b9b/unit-race.log)通过；[组合 acceptance](results/integration-ffc9b9b/acceptance-race.log)通过，23.175s，包含新适配宏与独立模块用例、primitive 冲突/正常局部捕获、Result allocation gate、producer 深层校验和生成 Go race/vet；[全树 vet](results/integration-ffc9b9b/vet.log)退出 0。这是本地合成树验证，不是 CI。

**没有在这个合成树重新运行全树 race。** 修复分支自身的全树 race 339.951s 通过证据见 FIXES.md；新宏提交自身全树通过是父任务提供的另一组证据，不能与本轮组合聚焦检查混称为一次联合全树运行。

## 安全集成方式

如果只需要生产修复，可从 ffc9b9b 新建集成分支，按顺序 cherry-pick：

1. `122caf301259db102da8dc8791b691b277347840`：宏 primitive 冲突诊断、对应测试与宏 README。
2. `69cdd28d8d5f17574604889165dcff913c5d3d43`：Result 静态 validator、allocation/无 EOF newline 回归与传播 README。

这两个提交不包含网站、不包含适配宏重实现，也不改 enum ABI。按实际集成分支重新跑 `go test -race -count=1 ./...` 和 `go vet ./...` 后再考虑合并。若要保留整个审查与网站交接资料，可在单独集成分支合入 review 分支；本次三方检查已验证没有文本冲突，但仍不要用整文件覆盖解决后续变动。

用户要求继续保持独立修复分支，本任务没有执行上述 cherry-pick 或向 feat merge。此说明之后的提交只增加集成报告/日志，不改变已验证的生产代码。

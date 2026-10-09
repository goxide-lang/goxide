# Match payload 借用：纯 Go 表示研究

这是 issue #14 准备阶段的可重放机制实验。**表示提案尚未批准，本目录不是前端验收，也不意味着 ABI 已选定或递归 Goxide enum 已支持。** 测试直接构造普通 Go 类型，比较候选表示及反例；不会执行 Goxide 编译器、改生产代码或部署网站。

研究沿用 [match RFC](../../../docs/rfcs/0009-match.md) 中普通引用可复制、保存和返回、可写引用非独占的约束。本轮引用类型拼写为 `&T` / `&mut T`，payload 引用绑定模式为 `ref p` / `ref mut p`。测试保留逃逸的普通 Go 指针，观察整体赋值、复制和 guard 副作用后它们实际指向的存储。通过测试只表示这些明确写出的 Go 观察成立，不表示语言必须采纳某一种行为。

## 重放

需要 Python 3.10+、Go **1.27.1** 和该平台的 Go race detector 支持。运行器只接受 `GO_BIN` 或显式 `--go`，不会回退到系统默认 Go。云环境示例：

```bash
source /workspace/.cloud-setup/env.sh
export GOROOT=/workspace/.cloud-setup/toolchains/go1.27.1
export GO_BIN="$GOROOT/bin/go"
export PATH="$GOROOT/bin:$PATH"
hash -r
"$GO_BIN" version
python3 tests/research/match_payload_borrows/run.py
```

默认创建并保留 checkout 外的临时证据目录，输出其绝对路径；可用 `--output-dir /absolute/path/to/new-evidence` 指定新目录或空目录。拒绝 checkout 内路径及非空目录，避免把原始历史日志提交进仓库或覆盖已有证据。`--timeout` 控制每条命令的超时秒数。

运行器把 `*.fixture` 复制到临时独立模块，运行：

```text
$GO_BIN test -mod=readonly -race -count=1 -json ./...
$GO_BIN vet -mod=readonly ./...
```

只在子进程设置 `GOTOOLCHAIN=local`、`GOWORK=off`、`GOENV=off`、空 `GOFLAGS`、`GOPROXY=off`、`GOSUMDB=off`；没有第三方依赖、自动工具链下载或持久环境更改。保留已激活的 `GOROOT` 和 race 所需环境。临时模块执行后删除；`*.fixture` 后缀使这些探针不加入仓库普通 `go test ./...`。

证据目录保留：

- `fixtures/`：本轮输入副本；可复制并去掉 `.fixture` 后缀再次运行上述命令。
- `manifest.json`：fixture/runner SHA-256、实际 Go 版本及路径、命令、工作目录、环境覆盖、退出码和时间。
- `race.jsonl`、`race.stderr.log`、`vet.stdout.log`、`vet.stderr.log`：未经改写的命令输出。
- `observations.jsonl`：测试输出中 `OBSERVATION ` 后的 JSON 原文，按输出顺序保存。
- `summary.json`：实际测试与观察数量、通过/失败/跳过列表及 race/vet 退出码；终端打印同一摘要。

只有 race 与 vet 都成功、至少运行一个测试、无失败/跳过且每个测试产生唯一观察时才标为 `PASS`。本目录不携带某次机器执行的原始日志。

## 实验矩阵

以下名称均为 `Test` 后的完整测试名，实际断言见 [representation fixture](representation_test.go.fixture)、[guard fixture](guard_test.go.fixture) 和 [GC fixture](gc_test.go.fixture)。反例测试成功表示它确实暴露了差异，不能把候选方案解释为通过语言验收。

| 测试 | 观察与边界 |
| --- | --- |
| `InlineCopyBeforeAndAfterBorrow` | 借用前、借用后的内联标量副本都独立；旧指针修改原槽位。 |
| `InlineSameVariantReplacement` | 回调把 A 整体替换成新 A，旧指针看到新值并可修改它。 |
| `InlineOtherVariantAndReturn` | A→B 清零 A 槽；旧指针可写非活动 A 而不改 B；回切 A 仍复用该槽位。 |
| `SameTypedSlotsCannotBeCoalescedTransparently` | A/B 共用同类型槽会让旧 A 指针修改活动 B，与独立槽行为不同。 |
| `ReturnedStoredAndCapturedReferenceAcrossGC` | 返回、复制、容器保存、闭包捕获的引用仍指向同一槽位；此例还保留 owner，单独的 GC 例补足内部指针边界。 |
| `ParentAndNestedReplacement` | 整体父对象或内层 enum 赋值复用字段地址；重绑父指针不会让旧引用转向新分配。 |
| `ArrayStorageAndSliceBackingRelocation` | 数组赋值复用原存储；强制扩容的 slice 产生新 backing storage，旧引用仍指向旧分配。 |
| `ExplicitRecursiveIndirectionIsShallow` | 显式 Go 指针和 slice 字段仍按 Go 浅复制共享；不验证递归 Goxide enum。 |
| `PointerBoxCopiesShareAndReplacementDetaches` | 指针盒在借用前就共享；替换盒后旧指针留在旧对象。 |
| `LazyBoxChangesCopySemanticsAfterBorrow` | 首次借用装箱后，原本独立的值复制变成共享复制，行为取决于借用历史。 |
| `CopyHookMissesNativeAggregateAndContainerCopies` | 显式 clone 只覆盖主动调用；普通 Go 赋值、聚合与容器复制仍共享。 |
| `LateCopyOnWriteCannotRecoverEarlierSnapshot` | 逃逸 raw pointer 已写入后才分离，无法恢复复制时的值。 |
| `FalseGuardSnapshotAndLiveDispatchDiffer` | false guard 把 A 换成 B，固定入口标签与读取当前标签选择不同分支。 |
| `LiveGuardDispatchCanPassAnEarlierExhaustiveArm` | B arm 在前时，A guard 换 B 后失败，按原顺序继续读取当前标签可落入 no-arm，即使 A/B 均有无 guard 的 arm；不研究重启匹配算法。 |
| `TrueGuardCanReplaceVariantBeforeBorrowingBody` | true guard 换成 B，已选 A body 仍能写非活动 A 槽；B payload 保持原值。 |
| `CopyBindingAfterBorrowGuardHasIndependentSnapshotChoice` | 标签未变，仅引用 guard 写入 5 后失败，后续值绑定从入口快照得到 1，从当前槽得到 5。 |
| `InactiveSlotsAffectRawGoEqualityAndMapKeys` | 活动标签和 payload 相同，但非活动槽残留改变原生 Go `==` 与 map key 行为。 |
| `GenerationCheckedProxyDoesNotConstrainRawPointer` | 带代数检查的代理能拒绝访问，已经逃逸的普通 Go 指针仍可读写。 |
| `BorrowPayloadCellDiffersFromCopiedPointee` | 对指针 payload 槽取引用产生 `**T`，与复制 pointee 值是不同操作。 |
| `InteriorReferenceAloneKeepsOwnerAndInactivePayloadAlive` | factory 只返回内部 `**payload` 字段指针和独立 cleanup 标志；跨 GC 后仍可访问 owner 中的非活动引用字段及其对象。 |

guard 的标签选择、一次按源顺序扫描的覆盖性、后续值绑定从哪里读取，是三个独立问题。实验同时保留观察差异，未把当前提案偏好的入口快照策略写成语言承诺。race detector 通过也不说明可无同步并发修改；这些场景都是顺序执行的机制实验。

## 等价性与 GC 限制

原生 Go equality/map 反例说明表示选择的互操作成本。当前 Goxide 前端拒绝 enum 间直接 `==`；这里不声称发现了现有前端 equality 漏洞，也没有验证 enum/nil、语言 equality 协议或生成代码的 map 约定。

内部指针 GC 例不向调用方返回 owner 或独立 payload 指针。`runtime.AddCleanup` 回调只写独立 `atomic.Bool`，不捕获受观察对象；所有读取后执行 `runtime.KeepAlive(cell)`。它检查可达对象不会提前清理、且字段和 payload 在 GC 后仍可用。测试不要求失去引用后在某个时限回收，不以 cleanup 尚未执行证明对象永久保活，也不测内存占用上界。非活动字段仍是普通 Go GC 扫描的字段；tag 不会自动使其中的指针失效。

这些探针沿用独立研究中已复现的输入，将它们整理为可重放断言；完整编译器 lowering、诊断、跨模块协议、ABI 和网站验证属于后续实现与验收范围。

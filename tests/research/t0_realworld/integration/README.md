# 双模块业务与跨包 Result 性能审查

基线：`3098a30e8e5fcb4696687246b2f39988218f4006`，2026-10-04，Go 1.27.1 linux/amd64。只新增研究文件。源业务复制自 `examples/enum_trial`，新增独立生产者 `config.Step/Fail`、消费者循环、包宏、差分/性质测试及 benchmark；并非将原有案例重新命名为新发现。

## 运行

```bash
export GO_BIN=/workspace/goxide-environment/go/bin/go
bash tests/research/t0_realworld/integration/run.sh /tmp/my-t0-review
bash tests/research/t0_realworld/integration/compare_direct.sh /tmp/my-t0-review/app /tmp/my-t0-direct
```

两个输出目录必须不存在或为空（第二个必须不存在）。脚本禁用下载、使用真实 Go，并把缓存放在 `/tmp`。生产者和消费者各有 go.mod；先转译生产者，再将消费者 replace 指向生成后的生产者。源 fixture 的相对 replace 是占位值，runner 在临时输入副本中改成绝对路径；请通过 runner 运行。

`run.sh` 执行两个生成模块的 race/vet、优化构建 benchmark（200ms × 3）、5 秒双 worker fuzz、普通 release build 和 CLI 运行。`compare_direct.sh` **只编辑临时生成 Go 副本**，保留全部校验与公开 ABI；该副本的 metadata 已过时，不能再作为 hgo 输入。随后用原生 Go race/vet、单 P 优化 benchmark 与 `-m=2` 比较。没有修改编译器。

## 新发现：跨模块 ? 的 validator 函数变量导致逐项逃逸

真实源：[生产者](model/config/pipeline.hgo) 的 `Step` 返回 `Result[int,error]`；[消费者](app/pipeline.hgo) 的 `Sum` 对每项执行 `cfg.Step(item)?`。基准使用 `1000+i`，避免小整数的 runtime 静态装箱缓存掩盖分配。手写 Go 对照位于 [pipeline_test.go](app/pipeline_test.go)，使用相同错误 sentinel、相同求和与失败分支。

预期：既然 producer 的类型与 validator 已可信登记，单纯校验不应强制每个成功值逃逸。并不要求所有 Result 跨函数返回零分配。

实际：validator 使用两层 package-level function variable 初始化别名；调用点通过最终函数变量进行一次间接调用（不是两次链式调用）。优化 Go 不能分析其目标，导致全部 item 结果逃逸。1024 项为 1025 alloc / 8200 B。显式类型的具名转发函数保留调用位置、目标校验与 nominal 类型，仅消除间接绑定后为 1 alloc / 8 B；剩余的是返回的总和 Result。本地检查全部通过，不能据此宣称通用生产补丁已验证。

源码位置：

- `internal/frontend/imported_family.go:26`：导入 instance validator 为 `var`。
- `internal/propagate/lower.go:207`–`:240`：`bindImportedFamily` 再将 validator 绑定为 `var`。
- `internal/frontend/imported.go:70`、`:142`：独立 enum validator 也使用同类绑定。
- 生成证据：`pipeline.go` 中 `__hgo_qimport_f1_2` 指向 `__hgo_enum_family_import_*_inst0`；后者指向 producer 的 `HgoInst...`。

最小建议：对编译器登记且可命名的具体 validator，生成 package-level **具名且带具体参数类型的转发函数**，并确保两层绑定都能到达静态目标。继续使用唯一名称/类型 alias 防止局部遮蔽。不要删除 validator，不修改 enum ABI。需补回归：跨模块 `?`、本地遮蔽、非法/typed-nil sum、producer 内层载荷校验、非小整数成功值的 allocation gate；完全不可命名类型维持原诊断/安全路径，不在此定案。

`direct_calls.py` 是此 fixture 的有界因果实验，不是通用 Go 转换器；只修改 Result 与 Mode 两处绑定，其中 Result 转发函数直达 producer validator，原中间变量仍留在生成文件中但不再用于这个调用点。原始/修改后的 escape 摘录和全部 benchmark 在 [results](results/)。`Dispatch` 的同 ABI native switch 是额外上下界参照；不把它的时间差全部归因于 validator。

## 已执行的组合验证

- 781 组长度 0–4、取值 `{0,1,127,-128,-1}` 输入 × 4 种 enum 模式 × 2 种日志开关，与手写 Go 比较数值、错误状态、完整 trace、失败前日志次数、defer 顺序及 `errors.Is/As/Unwrap` 身份。
- 同一性质的 5 秒 fuzz：最终 fixture 本次 133,920 次执行，使用已有缓存 corpus 12 项；不是可穷尽证明。先前版本另跑 143,514 次，不累计为最终版本次数。
- 351 个 JSON→独立 model→consumer match/?→thiserror 流程组合。
- `Err(nil)`、typed-nil error、sentinel error 经跨模块 `?` 与 `#[from]` 保持 Err 分支和原始 cause；不将 typed-nil 转成 nil。
- Option None / Some(nil) / Some(empty)、显式 equals、Clone 保留 nil 状态及深复制载荷，跨模块 Default/Clone/Debug/PartialEq/Eq。
- 继承订单用例的 serde required/default/unknown variant、默认值副作用、strconv cause、失败不触发成功日志。
- 生成生产者/消费者 race/vet，正常优化 build/运行；临时 direct-call 副本再次 race/vet。
- 本地 `go test -race -count=1`：macros、enums、deriveextras、serdejson、serde、formatting 六包通过（[日志](results/regression.txt)）。这是有针对性的回归检查，未运行全树 acceptance，不称为 CI。

没有改变 Copy/move、借用规则、enum ABI、公共 enum 参数提前校验、blank/embedded derive 或 serde adapters。无外部依赖下载，无推送/merge。

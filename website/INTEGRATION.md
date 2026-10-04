# 官网与编译器联合交付验证（2026-10-04）

本记录对应独立集成分支 `integrate/t0-web-result-review-20261004`。受测实现提交为 `1f768364a38fd8eb0c208e3b5a36af1bee56a881`；之后的交付记录提交只添加本报告与日志，不改变受测实现。最终资源和各平台二进制仍在报告提交后重新构建，内嵌清单记录实际交付 HEAD。

## 有来源的整合

从 Result 宏提交 `ffc9b9b501fbccc56c73794a05eaeb427acd2e2e` 建新分支，依次 `git cherry-pick -x`：

| 原提交 | 集成提交 | 内容 |
| --- | --- | --- |
| `122caf301259db102da8dc8791b691b277347840` | `0bba912` | primitive 绑定冲突改为带定义/调用位置的诊断 |
| `69cdd28d8d5f17574604889165dcff913c5d3d43` | `9850a46` | 跨模块 Result 静态 validator，保留校验与 nominal 身份 |
| `bcc1834f58bc05c3bf21e8882bd55b82be5e260b` | `038f467` | 原网站的独立 WASM 适配 |
| `2f2177ea7fb877b42b5a8996f2cc8ad857dd32f0` | `3143d5f` | 原网站及 embed HTTP 程序 |

自动整合无冲突，没有整文件覆盖。`1f76836` 添加第五组单文件 Go 适配宏示例、四项 CLI/WASM 正反差分、准确更新修复边界，并让设计文档扫描排除 `node_modules`（测试依赖的 README 原本触发缺失相对链接）。

两种 Result 宏实现保留；没有合入 `mod` / `pub use` 研究、Cargo 草稿或其它未实现能力。没有改写原分支、main 或冻结标签，没有创建 release 或公共 Site。

## 联合验证

在 Go 1.27.1 linux/amd64、`GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOMAXPROCS=4` 下运行，全部退出 0：

```sh
go test -race -tags hgoweb -count=1 ./...
go vet -tags hgoweb ./...
python3 scripts/check_design.py
GOCACHE=/workspace/goxide-environment/cache \
  QA_URL=http://127.0.0.1:8082 npm test --prefix website
```

- [联合全树 race](validation/2026-10-04/full-race.log)：包含编译器、两宏、primitive 诊断、跨模块 validator/allocation gate、serde、CLI 与嵌入服务器；acceptance 284.548s、prototype 7.731s。此处是一次真正的组合全树运行，不是拼接各分支的历史通过结果。
- [联合全树 vet](validation/2026-10-04/full-vet.log)：正常退出，无输出，因此日志为空。
- [设计检查](validation/2026-10-04/design.log)：67 个 Markdown、891 个本地链接/锚点，20 个 RFC 的结构及 13 条 Go 命令通过。Go 机制检查有预期失败反例，不冒充 hgo frontend 的测试。
- [浏览器回归](validation/2026-10-04/browser.log)：五组官网例子 CLI/WASM 输出逐字节一致；新增 error-only 与 typed-nil 生成一致性，以及错误 T、指针 error 返回值的规范化文件名后诊断一致性。
- 浏览器实际编辑改变输出，错误恢复、连续快速输入、复制、Ctrl+Enter/Tab、390px 窄屏、32 KiB、未知 import、XSS 字面文本、中断、重置和无外部请求均通过，JS 错误为零。超时通过 QA 路由替换不返回的 Worker 确定性触发，随后恢复真实 WASM；产品没有测试后门。
- [五组 CLI 实际运行](validation/2026-10-04/cli-examples.log)通过。新适配示例输出 `next: 42`、`parse error: true`、`partial: 3 abc true`。

浏览器只转译，typed-nil 的浏览器差分不等于在浏览器执行其运行语义；运行语义由真实 CLI/acceptance 覆盖。

## 独立最终审查

独立只读审查者检查 `1f76836`，未发现阻断。其额外聚焦执行：

```sh
go test ./internal/macros ./tests/acceptance \
  -run 'Test(Primitive|GoResult|MacroPrimitive|CrossModuleBuiltinArtifacts)' -count=1
```

两包通过；跨模块验收内部还运行生成工程 race/vet。审查确认两种宏相对 `ffc9b9b` 完整保留、修复来源和必要集成挂钩正确，独立重算 compiler/export 摘要相符，8 个 dist/embed 资源逐字节相同。

原网站审查的两项问题继续保持修复：`hgoweb` 标签避免干净 checkout 缺资源时破坏默认检查；摘要覆盖支持库与 export data/目标平台。

## 交付边界

- 单文件、随附包/依赖闭包、浏览器 Worker 真实转译；新例子使用直接调用，不伪造多文件或外部宏包支持。
- primitive 冲突现在明确拒绝，不代表完整 hygiene；静态 validator 只证明具体循环消除逐项分配，不承诺所有路径零分配。
- 网页/CLI/WASM 绑定同一源码。此轮编译器源摘要 `424cb6f7bc63a20c314481f007817dae5278e57ed15d8493ed75865f5c27c5c6`；export 摘要 `fb6815c13033abf01b33b5c3d47c0964d8e693c99b6a49ebc840c78d15e18256`，导出目标 Linux/amd64。版本详情以最终内嵌 `examples.json` 为准。
- 最终程序仍是单二进制，不依赖 Node/Go/静态目录/CDN；无服务器代码执行接口。运行默认仅 loopback。其他平台交叉构建不等于已在对应操作系统运行。
- 新包只在云环境生成。本轮没有重试 Library 批量助手的 HTTP 401，也没有切换已启动的上传路径；父任务协调后续分发。

## T/E 推导分支验证（2026-10-04）

`feat/result-type-inference` 在 `f097b1951dde4f8653a0629a7da8a0bb5646ec79` 上独立实现，不包含并行的 version/source-feature 分支。网站示例改用省略 T 的调用，描述保留 E 与原类型 nil 判断。

本地实际浏览器运行通过：5 个示例逐字节 CLI/WASM 对照；8 项适配检查为 error-only、接口 typed nil、错误显式 T、nil 指针错误、具名接口、具名 slice 错误、非 nilable 错误拒绝、返回数量拒绝。原有编辑、错误恢复、快速输入、复制、大小限制、导入限制、XSS、键盘、移动尺寸、超时、取消、重置和无外部请求检查通过。CLI/WASM 差分只验证转译；原生执行与生成模块 race/vet 在 acceptance 中另行验证。

构建资源仍为本地忽略产物，没有发布网站。与 version/source-feature 整合后须重新构建并验证，使网站内嵌版本与最终编译器一致；此前章节的历史 commit/digest 不代表本轮最终版本。

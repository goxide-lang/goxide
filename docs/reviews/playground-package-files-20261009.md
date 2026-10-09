# Playground 同包多文件交付（2026-10-09）

关联 issue #5。分支 `feat/playground-package-files` 从实际 remote main
`eb6ddf0ce026251b4e9b791d0271ee05b9438749` 建立；没有使用未合入 PR #8/#9/#10
替代 main，没有合并、部署、打 tag 或修改 CI workflow。

## 实现范围

- `goxideCompilePackage([{path, source}])` 接入真实 `frontend.CompilePackage`，
  保留每个文件的名字、import 作用域、跨文件声明/宏/派生与诊断。
  仅一个用户包，1–16 个普通 `.gox` 文件，总 UTF-8 源码 ≤32 KiB，输出 ≤2 MiB；
  拒绝重复（含大小写冲突）、路径逃逸、测试/保留名及混合包。
- 用户文件可编辑/增删；完整生成 Go 按文件查看、复制。CLI 与浏览器共用
  `PackageGoFiles`，真实独立 helper 为 `goxide_helpers_<原文件名>.go`；
  不截断、隐藏或复制依赖定义冒充精简。保留构建约束、平台后缀和来源映射。
- 旧 `goxideCompile(string)`、11 个旧例的生成结果保持兼容。新增文件切包规则；
  删回一个文件仍是包规则，重置旧例恢复单文件规则。
- 依赖视图只读，12 个固定直接依赖、94 个原生构建选中的完整 GoFiles/CgoFiles。
  来源取自与 export archive 同批 `go list`，显示原路径、实际版本、SHA-256；
  manifest 保存导出归档摘要、模块 sums、许可证。明确不覆盖所有平台/测试/汇编文件；
  传递依赖闭包仍在 manifest，未增加任意下载、外部脚本、多包编辑或上传功能。
- 基础多文件、helper 与现有隐式 std 语义无需 PR #8；没有捆绑该 PR 的 alias 精简
  或显式 std 增强。没有新增语言语义决策。

## 本次验证

Go 1.27.1，每个 Go shell 按 start.md 激活；原生 GOMAXPROCS=4。
真实 Chromium 在工具批准上下文启用 `chromium_sandbox=True`，同上下文运行
本次嵌入资源 HTTP 服务。未使用 `--no-sandbox`，未降低站点 CSP；服务已停止。

- `internal/playground` 定向包测试通过，包含真实生成 Go 构建执行、独立 imports、
  跨文件宏/derive、隐式 Option alias、集合校验、冲突与恢复。
- `internal/frontend` 的 `TestPackageGoFiles*`、`TestPackageHelperRejectsUnselectedNativeCollision`、
  `TestGeneratedModuleGoxideAndReservedSidecar`、`TestGoxideBuildSelection` 等受影响检查通过。
- `website/build_support_test.py` 26 项、`website/validation/run_test.py` 14 项通过；
  `website/build.py` 构建通过，`cmd/goxide-web` 带 goxideweb 标签测试通过。
- `website/validation/package-files/run.py` 最终 `passed: true`：51 次真实 Worker
  观察，LF/CRLF CLI/WASM 逐文件字节对照，规范化诊断完全一致，11 个旧例保持兼容；
  编辑、切文件、增删、非法名字、切例、错误恢复、重复转译、helper 全文剪贴板，
  旧请求 ID/旧 Worker 实例与旧剪贴板回调隔离、Unicode 包名及 390px 布局通过。
- 实际 HTTP 的 manifest/app/Worker/CSS/Go JS runtime 与构建字节一致；WASM 摘要、
  每个依赖文件摘要、实际 Go 工具链与 go.mod 选版均核验。

完成一次独立聚焦审阅并修复：拆 helper 丢构建约束、覆盖未选中原生文件；
同时修复旧剪贴板失败回调和 Unicode 包名边缘情况。浏览器迭代记录保留：
首次测试等待方式触发 CSP、第二次 helper 断言未同步新命名，第三次发现移动端
依赖选择器横向溢出；均已修复，第四次通过。不把失败尝试记成成功。

未重跑本地整树或历史七阶段总验收；最终统一托管验收待 CI 配置进入 main 后执行。
没有剩余实现阻塞；issue 在合并前保持 open。

## 构建身份与原始证据

- Compiler: `7779255b9b058a083e0a97184d8d780d9bbd2a8e`
- Website build: `45b2fd638bae5e3c020b77406ea0da86e2694e25`
- Compiler SHA-256: `aa2a8596c0df606df9ae083af78a7c5918456221aa21b69d2c0f89b8d43d4f2d`
- WASM SHA-256: `c96f3e0bbd2ac07f6e975c7783c3029e7c29d7b9bd8499d457f7fcf35758133e`
- `github.com/goxide-lang/std`: `v0.0.0-20261004173304-ce5bccb4ff9c`
- `github.com/goxide-lang/serde`: `v0.0.0-20261004120911-a1b791e96d66`

最终原始证据位于执行器 `/tmp/goxide-issue5-browser-4/`：`summary.json`、
`worker.json`、native 运行结果、桌面/移动截图；之前失败尝试分别在
`/tmp/goxide-issue5-browser-{1,2,3}/`。构建日志在 `/tmp/goxide-issue5-build-final.log`。
产物与截图不提交 Git；可按验证 README 从当前源码重建。

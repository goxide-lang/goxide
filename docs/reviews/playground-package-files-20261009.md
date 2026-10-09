# Playground 同包多文件交付（2026-10-09）

关联 issue #5、[PR #11](https://github.com/goxide-lang/goxide/pull/11)。
本记录覆盖与 #8 语义清理、#9 CI 和 #10 宏/Eq 来源追溯修复组合后的源码。
最终托管门禁、合并 SHA 与 main 验收状态以 PR 记录为准；没有部署或打 tag。

## 实现范围

- `goxideCompilePackage([{path, source}])` 使用真实 `frontend.CompilePackage`，
  保留文件名、独立 import 作用域、跨文件声明/宏/派生和诊断。
  仅一个用户包，1–16 个普通 `.gox`，总 UTF-8 源码 ≤32 KiB，输出 ≤2 MiB；
  拒绝重复、路径逃逸、测试/保留名和混合包。
- 文件可编辑/增删；生成 Go 按真实文件完整查看、复制。CLI 与浏览器共用
  `PackageGoFiles`，helper 独立为 `goxide_helpers_<原文件名>.go`，保留构建约束、
  平台后缀和来源映射。目录转译因此新增 helper 文件，消费者必须读取完整文件集合。
- 保留旧单文件入口和 11 个旧例；新增文件切到包级规则，删回一个文件仍是包级规则，
  重置旧例恢复单文件规则。浏览器仅转译，不执行生成 Go。
- 12 个固定直接依赖、94 个原生选中 GoFiles/CgoFiles 以只读源码展示；来源与 export
  archives 取自同批 `go list`，记录真实版本、路径、摘要、模块 sums 和许可证。
  不涵盖其他平台、测试或汇编文件；传递闭包另列于 manifest。没有任意依赖下载、
  外部脚本、多包编辑或上传功能。

## 独立审查与组合修复

独立会话审查源码、反例和跨 PR 契约。修复 helper 拆分导致的来源证据丢失：
使用 ASTEvidence 传递已有证明，不给生成 helper 伪造来源；增加 LF/CRLF、直接/
嵌套/重复宏的严格回归。旧实现负控制失败，修复后定向测试通过。

修复相邻注释 `package/**/示例` 的包名识别，并在真实浏览器用前置干扰注释覆盖。
CLI 来源验收现在比较所有生成文件和 map 的重复输出，保留 helper 数量及来源断言；
跨模块 enum 漂移夹具同步完整 producer 产物，仍要求 relay 的旧依赖闭包被拒绝。
两处夹具均有定向红绿证据，修复后再次独立复审通过。

历史七阶段 runner 保留原 `afa3f0a...` 冻结身份，不改历史 oracle 来接受新输出。
当前组合使用 [同包验收入口](../../website/validation/package-files/README.md)。

## 当前组合的构建与浏览器验收

Go 1.27.1，原生 GOMAXPROCS=4。离线网站构建、CLI、匹配 WASM 与嵌入 HTTP 程序通过；
131 项编译器输入与固定对象逐文件一致。真实 Chromium 使用 `chromium_sandbox=True`，
站点 CSP 保持不变；本次 loopback 服务已停止。

- 51 次 Worker 观察通过：LF/CRLF CLI/WASM 逐文件字节对照、规范化诊断、集合边界、
  每次失败后恢复、重复转译和全部 11 个旧例。
- 文件/输出/helper tabs、编辑保持、增删、非法名字、完整 helper 剪贴板、旧请求 ID、
  旧 Worker 与旧剪贴板回调隔离、Unicode 包名及 390px 无横向溢出通过。
- 实际 HTTP manifest/app/Worker/CSS/runtime 与构建字节一致；WASM 与每项依赖源码
  SHA-256、Go 工具链和固定模块版本一致。

构建身份：compiler `e68bfe6ed83de432e2c2ee79936bda4150cc78ca`；
website `2797dfd`；compiler SHA-256
`26be044904505b198ee009615cc318ecd2286db37f74a19b0919a8b712ee9d52`；
WASM SHA-256 `7c9062eab6f9b4a62efae0f3754551b43a91a631fd0b25dc6103a5086738d204`。
依赖保持 std `v0.0.0-20261004173304-ce5bccb4ff9c`、serde
`v0.0.0-20261004120911-a1b791e96d66`，没有本机 replace。

没有重复本地全树或运行历史七阶段；完整 race/vet/设计门禁交给最终 SHA 的托管 CI。
CI 浏览器自动化仍是 docs/ci.md 所列缺口，本轮本地浏览器成功不等于该层已自动接入。
原始日志、观察和截图留在执行器，未提交 Git；验收入口可从匹配产物复现。

# Goxide 命名发布同步 · 2026-10-04

完整树来自d23064760e1fb5d4bb77cae17e5d7818fb73139d；保留上游官网功能，不重新合入旧名分支。compilerDigest为32afe88be9223d2fb51a3129b7cc4973bf9bb8f44b5c137529c3c35d45be3ba3。public std版本v0.0.0-20261004173304-ce5bccb4ff9c（commit ce5bccb4ff9c7fb96b7f192a3a263051590d9288），serde不变，无replace。go mod download all只按现有固定依赖预热，构建实际校验zip/go.mod sums与GoxideSumABI=2。

manifest abiVersions从源提取enum8/ref3/use2/sum2，browser再次断言。编译器CLI/源码/环境/生成marker、WASM入口及worker bridge整组迁移。修复上游机械替换导致的“旧goxide.lock”说明，恢复为明确拒绝“旧hgo.lock”。旧嵌入构建残留已移除，没有纳入新提交。

本轮定向race：cmd/goxide 25.460s、internal/frontend 24.206s、tests/acceptance 97.351s，通过Naming/SharedStd/CrossModule/GoResultInference/CLIProcessContract。上游全树race与vet/design证据见docs/reviews/goxide-naming-20261004.md；不将本轮定向验证声称全树重跑。

本地26卡片、10live逐字节、10目录negative、8adapter加1旧directive拒绝、390px/剪贴板/中断/超时/清单故障/损坏WASM闭锁通过，根/子路径资源路由通过。新增naming-audit.py检查当前public静态文本及examples；仅两条明确旧名拒绝说明允许。WASM保留七类旧marker/旧源/旧directive拒绝字符串，无旧bridge或__hgo生成前缀；原始hgo substring扫描中的Go goarch等拼接不属于旧名称。

首次对js/wasm-only包用原生vet命令被build constraints拒绝，随后分别使用原生web tag与GOOS=js GOARCH=wasm验证。最终检查、源码绑定重建、12文件remote/HTTPS hashes与Pages终态保存在发布环境记录。workspace vendor拆出仍不支持，未开展转换协议实现；无main/tag/release变更。

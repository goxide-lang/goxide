# Workspace 迁入网站同步 · 2026-10-04

固定编译器27d2e2740c5ee7dc3e3766bd7b5f0eec6bb852b8，合入上一网站源码d76ceaab；生产文件与基线一致，摘要c943fd6c94f7ef6d654449a316bf92e5088ae5f8b98ee8cfd02791c2e356ab11。WASM重新构建，15,889,094字节。上游全树race/vet/design证据见基线docs/reviews/workspace-import-20261004.md；本轮不冒称重跑全树。

新增workspace_import_snapshots.py：原生go.work真实plan、新目录迁入、原字节/权限不变；之后将lib.go替换为.hgo，默认feature/关闭/恢复与test通过。显式物化继承/path后复制schema4根锁，独立offline/locked运行且原锁字节不改。另一fixture删除原replace目录、用空modulecache/proxy关闭，保留修改过的vendor字节/权限，在增强宏调用中得到modified vendor enhanced。go work vendor只制造fixture，迁入过程不重建。脚本说明依据真实NativeWorkspaceProgressiveScriptPermissions回归，不在网站构建中授权/运行脚本。

26卡片、10实时示例、10拒绝例的本地目录浏览器对照已通过。11资源审计通过，配置仅含gcc/g++/pkg-config及原生flags，无环境绝对工具路径。独立只读审查无blocking。workspacevendor独立拆出明确未支持，需另确认转换合同；hostresolver/publish等未交付。

本轮执行命令为 `go test -race -count=1 -timeout=15m ./cmd/hgo ./internal/project -run 'Test(NativeWorkspace|Workspace|CLIProcessContract|VendorImportColdOffline|VendorImportRejections|CLINativePromotedLegacyGraphOffline)'`；结束结果、最终源码绑定构建、全部浏览器验证、Pages终态与12文件线上hash由发布环境独立证据记录。在这些检查通过前不得发布。

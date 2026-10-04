# CLI/vendor 网站同步 · 2026-10-04

编译器固定 `09f1eb258fc98217e5d03506f34957c9ee9aec4f`，网站从 `02e003f74ea097db172c4c7920c67adb8c4b7f5f` 合入独立分支。生产编译器文件与基线逐字节相同，摘要 `9cebac63731cc5490e3ffc00a8a5b9b7ef0869d72482e29f1d8e7ae9bb16ec77`。完整 race/vet/design 上游证据为基线 docs/reviews/cobra-cli-20261004.md 及对应 validation JSON，不将本轮 scoped 测试冒充全树复跑。

本轮 scoped CLI 检查：`go test -count=1 -timeout=15m ./cmd/hgo -run 'Test(CLIProcessContract|VendorImportColdOffline|VendorImportRejections|CLINativePromotedLegacyGraphOffline)$'` 通过（12.525s）。构建执行全部10示例，10目录拒绝例；继续验证共享 std/serde 身份、feature A/B/A、普通 Go 迁入，并新增真实 vendor 与 argv 快照。首次构建发现 plan 新增说明注释；改为解析 TOML 等价核验，保留原始命令输出。vendor 在空 module cache、proxy/sumdb关闭、删除原replace来源条件运行，Go构建缓存仍可用；locked检测字节变化。通用浏览器检查首次因未传配置缓存路径失败，补正确GOMODCACHE/GOCACHE后复测，不修改编译器。

目录浏览器检查已通过25卡片、10例逐字节一致、10负例、筛选/链接/快照/390px、无外源请求；清单故障与损坏WASM闭锁通过。最终提交后重新构建并进行资源审计、正式HTTPS与12文件哈希核验；最终部署证据由发布环境记录，不能仅凭本记录宣称部署成功。

RFC0026引用 `be41fba3cc70b788c889dab58c341e6e3a764f8a`：整体proposal未实现，仅显式继承约束确认。RFC0024仍proposal。独立只读审查未发现阻塞，两项文案建议已修复。公开产物不含本记录或私有RFC全文。

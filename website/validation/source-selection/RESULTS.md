# 来源选择网站同步 · 2026-10-05

编译器固定5d777820c5a617db57bcedb50b4d31ad6f2d3f15，digest27323c6bf7a3066bab1d6546355e681e2b2d548e10c07389ffe2793f6541aa42。整合上一官网4152dbb的命名/ABI/provenance检查。std ce5bccb和ABI8/3/2/2保持；新增公开依赖按现有go.mod download all预热，不更换来源或扩大权限。

source_selection_snapshot.py实际创建本地file proxy模块v1.2.0并预热，然后关闭proxy。非TTY输入remote文本仍默认local；显式remote缺差异确认不发布，确认后运行remote，原供应修改字节保留；冷cache offline失败。真实PTY一次选择与逐模块确认后，读取source_choices/differences_accepted并实际locked运行验证remote生效，不仅看终端回显。公开快照保留真实命令输出、错误诊断及PTY捕获内容，仅去临时路径和规范CRLF。

初轮构建发现快照脚本误用审计JSON字段sources/accepted，已按实际source_choices/differences_accepted修正；生产编译器未改。独立只读review复核字段与范围，补充TTY结果断言已落实。重建通过，WASM16,138,905字节；11资源隐私和名称审计通过。

定向race命令 `go test -race -count=1 -timeout=15m ./cmd/goxide ./internal/project -run 'Test(ImportSource|Supplied|Vendor|OldVendor|NativeWorkspace.*(Supplied|Vendor|CopiedLock)|Naming|CLIProcessContract)'` 包含PTY/非TTY、HTTP零请求、完整性、混合来源及既有迁入/CLI检查。最终结果与本地/正式站点浏览器、Pages及12文件hash记录于发布环境sources-release.json；上游全树证据在docs/reviews/source-selection-20261005.md，不声称本轮重跑全树。

remote范围为原清单精确Go module/version及版本化replacement；无Git猜测、registry、任意URL或版本搜索。verified subset不等于完整上游发行物等价。offline控制工具网络，不隔离任意用户脚本。浏览器没有CLI网络迁入功能；不恢复vendor运行模式，不改main/tag/release。

# Workspace 作者核心网站同步 · 2026-10-04

编译器生产源码固定2aa0d06625f3d25ee6ad08ea2666c226eb4b1dac，独立网站分支合入上一轮e21dfc8。生产文件与基线相同，摘要be5fa4d4276d4cd9cd8cffbe7fa55c63664be89e8fd08636cf3b0c3b51012dd4。

本轮实际执行 `go test -race -count=1 -timeout=15m ./cmd/hgo ./internal/project -run 'Test(Workspace|CLIProcessContract|VendorImportColdOffline|VendorImportRejections|CLINativePromotedLegacyGraphOffline)'`，CLI83.280s/project1.042s通过。全树race/vet/design上游证据保留于基线docs/reviews/workspace-core-20261004.md，不声称本轮重跑全树。

新增workspace_snapshots.py使用本地file proxy发布刻意fixture的1.0/1.1版本，验证全workspace锁1.1、单service json/worker yaml、显式example、默认批量3bin、批量feature合并和单独恢复、唯一根schema3锁。第二个本地Git fixture实际移动main并删除无关仓库，复制根锁后locked与offline/locked仍输出one。Git快照使用已有缓存，不称全新缓存；path源码与继承物化边界明确。

构建只导出明确源码文件名单，不递归导出受管缓存；锁及命令输出移除临时绝对根目录前缀。归一化锁仅展示不能直接复用。首轮隐私扫描将fixture/workspace相对路径误识别为根路径，因此调整为去除前缀，不放宽审计规则。

独立只读审查无blocking，修正旧卡片go.work迁入表述。workspace部分实现且仅CLI，go.work/workspace vendor迁入、统一host resolver、细粒度lockupdate、publish/registry均未实现。正式发布须完成最终构建、11资源审计、12远端/HTTPS哈希与现有浏览器检查；结果保存在发布环境独立证据文件。

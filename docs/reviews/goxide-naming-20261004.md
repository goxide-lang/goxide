# Goxide 命名迁移验收（2026-10-04）

基线 27d2e2740c5ee7dc3e3766bd7b5f0eec6bb852b8，分支 feat/goxide-naming。最终策略按用户 pre-1.0 指示：移除旧 CLI/后缀/env/生成 ABI，不把永久兼容当作完成。

## 已验证范围

- 真实进程 TestNamingMigrationProcess：新名称、混合 Go/Goxide、main.go 输出、真实运行、旧 .hgo 的 run/build/expand/transpile 拒绝、同名 .gox/.hgo 不留下输出、空旧 env/双变量拒绝、Bash/Zsh/Fish/PowerShell 帮助补全。
- TestNamingRejectsOldProducer、TestNamingModuleInputs：旧 enum/reference/use/sum marker，目录旧 directive、GeneratedFiles 旧源/旧 directive/旧生成配置均明确拒绝；字符串中的历史文本仍视为用户数据。
- enum schema 8 / reference 3 / use 2 / sum ABI 2；跨模块生成、serde、Option/Result、宏/cfg、source maps、依赖与脚本均由迁移后的既有测试覆盖。
- std ce5bccb 的 race/vet 通过并核验远端；serde 无命名变更，race/vet 通过。正式 module import 路径未改。
- 最新官网源码 51405a69 的功能已保留。浏览器 10 例与 CLI 逐字一致，9 项额外成功/拒绝对照含旧 cfg；26 项目录、10 个目录拒绝例、工程快照、390px、复制、错误恢复/超时/中断、损坏 WASM 拒绝、同源网络与根/子路径资源路由通过。WASM 15,898,810 bytes，sha256 838a567da027420f4b7a6d665c18d001434257d7119ba1e90c30e52bb5e1f2c7。
- web tag goxideweb 的 race/vet 通过；设计检查 13 个 Go 命令（含预期拒绝）通过。原始输出见 [证据目录](goxide-naming-evidence/naming-design-final.log)。

## 独立审查与修复

独立 reviewer 只读审查并真实复现目录旧 cfg 绕过；还发现 GeneratedFiles 绕过与 WalkDir 空指针顺序。8c717a5 修复三项，reviewer 再次运行 TestNaming、复验真实 CLI 退出 1 并确认无新增阻断。最终校验下沉到 preparePackageContext，生成输入同样检查，walkErr 先于 DirEntry 使用。

首次回归出现未预热间接 Go 依赖的 offline 失败；通过 go mod download all 预热完整锁定依赖，没有放宽 offline 或 checksum 检查。旧 schema 测试更新为 8→7。官网旧快照同时要求锁不存在又读取该锁，修正为验证锁存在且根没有 Go 元数据。

## 交付与边界

官网 owner 使用此分支完整源码构建；catalog compilerCommit/digest 绑定 7894ddf，生产编译器源与最终树相同。CLI cmd/goxide、WASM cmd/goxide-playground、JS goxideCompile、tag goxideweb、GOXIDE_*、.gox、schema 和 std 必须整组同步，不能混用旧 WASM/JS/metadata。无部署、main 合并、tag/release 或仓库权限变更。

历史基线、原始研究结果和发布前候选/provenance 保留旧名。锁中的旧版本记录是解析历史，未机械替换。workspace vendor 拆出不在本次实现范围。

首轮全树 race：CLI 整包达到 Go 默认 10 分钟超时；acceptance 两个动态扩展名夹具错误地拼出 .goxide，修正为 .gox（5cca928）。以 30 分钟预算复验。CLI 展示名另修为 Goxide；真实 CLI/帮助重点 race 验证通过。

## 最终结果

完整 `go test -race -timeout 30m -json ./...` 退出 0：22 个包（含无测试包）、505 个宿主顶层测试、1059 个宿主子测试通过；CLI 94、acceptance 148 个顶层用例全部通过，无失败。生成模块的内部测试由这些宿主用例运行，不混入上述计数。最终 `go vet -tags goxideweb ./...`、web 包 race、最终设计检查（13 Go 命令）均通过。帮助展示名最后微调另有真实 CLI/帮助重点 race 通过。详见[完整包汇总](goxide-naming-evidence/naming-race-summary.log)。

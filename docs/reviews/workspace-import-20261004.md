# 原生 workspace 迁入验收（2026-10-04）

基线 `2aa0d06625f3d25ee6ad08ea2666c226eb4b1dac`，独立分支 `feat/workspace-import`。先提交 [迁入合同](../design/workspace-import.md)（`514b864`），再实现 CLI / project 的纯 Go 传输模式；不修改前端、宏或其他分支。具体接口和已知限制以上述合同为准。

## 实现证据

- `cmd/hgo/import_workspace.go`：先 plan、临时副本分析、实际包归属→显式边、目标选择、成功后发布新目录。
- `internal/project/native_workspace.go`：Go work/member 政策、受控 GOWORK、仅显式源码闭包、schema 4 锁与 sums、配置身份；原生 Go 负责 MVS/checksum/编译。
- `cmd/hgo/native_workspace.go`：选择单个成员或批量、按需目标、正常/测试视图隔离、根写锁与 owner、锁定/离线校验。
- `internal/project/vendor.go`：独立 go-work vendor 标记、逐文件字节/模式及政策摘要；不重建供应的 vendor。

## 实际 fixture

全部使用临时目录、真实 Go1.27.1 linux/amd64，本地 file proxy 或本地 replace；无私人来源、凭据扩权、安装脚本。

| 验证 | 结果 |
| --- | --- |
| 原 A 无 require 却 import 成员 B | plan 显示 workspace=true；新目录正常构建；原目录 hash 不变 |
| 成员 go1.26/1.27、work/member godebug | 生成政策保留，迁入运行 DefaultGODEBUG 与原生输出一致 |
| 本地 file proxy 两个 require 版本 | Go 全工作区选取1.1.0，锁定后离线重建 |
| 两成员 replace 冲突、work 根 override | 保留作用域，运行选择 work 指定的依赖 |
| 根 vendor 手动修改、原 replace 目录删除、空缓存 | 修改值保留，锁定冷离线运行；后续篡改拒绝 |
| 图外 C import | 单选与 --workspace 均拒绝隐式扩图 |
| 外部 use、合法 .test 名称、成员间原 replace | 映射新成员目录和受管 replace，原路径删除后仍可运行 |
| 仅 race 测试 import | 并入显式边，所选成员 test -race 可运行 |
| 未知指令、重复 module、模块级循环、重复 bin、.hgo | 迁入拒绝，原文件不变且目标不发布 |
| 未知 owner 标记 | 不再静默丢弃普通目录 |
| 架构/C编译器配置改变 | 配置身份变化，不能复用旧配置范围 |

## 独立 review

独立 reviewer 发现并复现两个问题：未知 owner 标记导致静默跳过目录、GOAMD64/GOEXPERIMENT 未完整进入配置范围。均已修复并永久回归；又按建议补齐 CC/CXX/CGO flags/PKG_CONFIG。独立 overlay 与全部 `TestNativeWorkspace*` race 通过（CLI22.247s/project1.031s）。补充实际搬迁 probe 发现成员间 replace 指向旧路径，已改为按 module+目录双重身份映射新成员/Go视图，外部replace保留引用；新增删除原目录回归。追加缺 vendor 包负例 race 通过（1.051s），确认原目录不变、目标不发布；锁校验和异常在执行程序前拒绝。成员替换增量也经独立 race 复测（12.350s），最终增量审阅没有新增阻断。

## 检查记录

使用 `/workspace/goxide-environment/go/bin/go`，不是 `/usr/bin/go`；GOTOOLCHAIN=local、GOWORK=off、GOPROXY=off。

- CLI/import/vendor/workspace/layout 相关 race 回归通过：CLI82.665s、project1.043s。
- 最终原生迁入 race 通过：CLI40.885s、project1.047s（含多bin/显式example、vendor许可证字节/模式）。
- `go vet ./...` 通过。完整 `scripts/check_design.py` 通过：25 RFC、13 条真实 Go 命令（含预期失败）；新增文档后 docs-only 通过103文件/1038链接。
- README 命令脚本由本分支构建的 CLI 实际执行通过：两bin输出 hello workspace / web: hello workspace，test通过。
- 成员路径修复后全部原生迁入 race 再通过：CLI43.964s、project1.052s；work 根限定版本 replace 搬迁增量 race 通过5.567s。最终生产增量再次 `go vet ./...` 通过。
- `go test -race ./... -count=1` 全树通过：CLI422.497s、acceptance351.991s、project1.151s，frontend/macros/typecheck/buildscript 等全部通过。该轮启动后补入的原生模式修复由以上最终专项 race 覆盖；没有修改前端。

`70964a8` 时的边界（下方续作已解除增强与非vendor拆锁限制）：只保证记录的普通/race测试配置；当时不支持增强 .hgo/features/scripts、额外 tags、自动工具链下载、schema4 拆出复制锁、统一 host resolver 或发布。非vendor的外部本地 replace 保留显式绝对原目录引用，不承诺自动自足。只有迁入发布新目录是整体事务；之后批量 build 沿用逐目标发布语义。

## 提交与交付

先行合同 `514b864`；生产实现 `30238cba62c3bdf003463474320d4ac098614af5`；成员替换搬迁修复 `fbe43f96a1e2cc9116c417aacd4037802f3d9b26`，均已推送独立分支，远端 refs 核验一致。本记录和示例随后独立提交。未合并主线、未创建tag、未部署网站；没有权限拒绝或遗留执行阻断。

## 渐进增强与拆锁续作

父审阅正确指出 `70964a8` 只完成原生保真迁入，不能作为完整第二阶段交付。续作合同先行提交 `28a0e63`，生产衔接提交 `d3edf65`。

新增真实验证：

- 迁入后 Go app 调用 `.hgo` lib，默认feature转发/关闭默认/再次默认结果切换，test模式成功；未知feature拒绝。
- 可选成员关闭后，其错误源码不参与编译；未选cfg文件不解析名称和import，仍校验既有token tree/package-import前缀合同。未改变前端原有“完整表达式语法启用后才检查”的规则。
- 修改过的 workspace vendor 在原replace目录删除、空modulecache条件下，继续被增强宏代码调用，字节保持不变。
- 依赖构建脚本先plan/明确subject allow；未授权不执行，成功后可复用缓存，revoke后缓存也拒绝。迁入与workspace成员资格均不授予权限。
- 非vendor复制schema4根锁，A仍使用整个工作区锁定的D1.1.0；独立后恢复成员panicnil=0，复制锁原字节不改，无关原workspace删除后仍可离线运行。
- 显式物化成员path并搬迁后，复制锁可运行；缺可达member/module记录、远端checksum篡改、子成员清单变化均拒绝。
- work override与成员自身replace选择不同来源时，拆出拒绝静默变源；没有偷偷继承原work override。

独立review发现依赖lib的required_features/缺lib检查必须先于脚本，已移入图预检；又补齐vendor摘要进入增强metadata/脚本PlanID。两项均修复。copied-lock独立race通过13.480s，无新增阻断。

新增测试：`native_enhanced_test.go`、`native_standalone_test.go`。集成race（NativeWorkspace/Workspace/BuildScript）通过CLI294.731s/project1.067s；复制锁/可选cfg负例race通过25.764s；政策丢失/vendor独立/依赖gate负例race通过14.552s。续作生产代码 `go vet ./...` 通过，完整设计检查13条Go命令通过。实际新CLI也执行了README渐进增强脚本，输出先为 hello workspace / web: hello workspace，再为 enhanced workspace / plain workspace。最终生产代码 `go test -race ./... -count=1` 全树通过：CLI535.324s、acceptance334.772s、frontend50.414s、project1.109s、typecheck1.297s，其余包全部通过，见[原始race输出](evidence/workspace-progressive-race-20261004.txt)。全树启动后追加的政策负例另有上述14.552s专项race；启动后未修改生产代码。

保留的真正转换边界：`TestNativeWorkspaceVendorStandaloneNeedsConversion` 同时验证hgo明确拒绝与真实Go的explicit不一致错误。A→D、B→E的workspace vendor不能直接当A的module vendor；擦掉E/marker改变原政策，隐藏保留B/go.work又违背独立政策恢复。建议先确认显式派生vendor协议（原快照验证、独立政策验证、字节保留、原/派生摘要），该部分暂停，未冒称完整第二阶段全部完成。其余上述渐进增强与非vendor拆锁已实现。

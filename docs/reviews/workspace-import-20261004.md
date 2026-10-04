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

明确边界：只保证记录的普通/race测试配置；不支持增强 .hgo/features/scripts、额外 tags、自动工具链下载、schema4 拆出复制锁、统一 host resolver 或发布。非vendor的外部本地 replace 保留显式绝对原目录引用，不承诺自动自足。只有迁入发布新目录是整体事务；之后批量 build 沿用逐目标发布语义。

## 提交与交付

先行合同 `514b864`；生产实现 `30238cba62c3bdf003463474320d4ac098614af5`；成员替换搬迁修复 `fbe43f96a1e2cc9116c417aacd4037802f3d9b26`，均已推送独立分支，远端 refs 核验一致。本记录和示例随后独立提交。未合并主线、未创建tag、未部署网站；没有权限拒绝或遗留执行阻断。

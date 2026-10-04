# 原生 go.work 与 workspace vendor 迁入合同

状态：2026-10-04 第二阶段实施合同，基于 `2aa0d066`。本文件先于生产实现落地；验收结果另记。保持原目录不变，仅在临时 staging 运行会写 Go 元数据的分析/构建命令，成功后发布新的输出目录。

## 接口与支持范围

`hgo import-go --workspace --plan ROOT` 显式读取 ROOT/go.work；`--out NEWDIR` 在不覆盖既有目录的前提下执行该方案。默认只有 plan。单模块 import-go 原接口保留。工作区可以自动识别当前配置的 main 包作为命名 bin、成员根 library；重名 bin 或没有可推导目标时拒绝，允许 `--member-bin MODULE::NAME=PATH`、`--member-lib MODULE=PATH` 显式指定。

首片是**原生 Go 传输模式**：输出仍以根/成员 goxide.toml 和根 goxide.lock 为作者权威，所有 go.mod/go.work 只生成在受管 target。保留每个成员自己的 go/require/replace/exclude/godebug、根 work go/toolchain/godebug/replace。不把 work.go 赋给所有成员；不把成员 godebug 合并成 work godebug。实际 list/build/run/test 使用同一受控 GOWORK。`GOTOOLCHAIN=local`，不自动安装工具链。

原生模式拒绝 .hgo、增强 features、构建脚本、增强远端依赖与额外 member-vendor；这些要等待前端完整传播原生工作区上下文后另行开放。不扩展 registry、发布或 host resolver。

## 显式图与分析范围

迁入在 staging 使用真实 Go `list -deps -test -json` 分析各成员的实际包归属，生成根 path 模板和成员 `{workspace=true}` 边；不能仅按 import 字符串最长 module 前缀猜来源。原本无 require 的 `A import B` 会在 plan 明列新增 A→B 声明。原 native require 仍保留原版本；显式图不产生 `B@v0.0.0` 伪 require，也不改 vendor explicit 记录。

分析范围固定为迁入时的 GOOS/GOARCH/CGO_ENABLED、Go 工具版本和空 build tags，写入 `[workspace.go]` 配置。切换配置明确拒绝并要求重新分析；不能把单配置分析冒称所有平台的无损迁移。源码或新的 import 改变后，构建仍按声明图检查，不能运行时借 go.work 自动扩图。跨成员包级无环但 module 级循环，首片明确拒绝。

构建某成员时仅复制其显式依赖闭包的源码；其余成员只保留原生 Go 元数据以维持全工作区版本政策。每个选中根独立校验，批量选择本身不使别的成员源码成为允许来源。纯 Go 源码没有宏/脚本展开路径。

## replace 与路径

保留 work/member replace 的作用域和限定版本，冲突由真实 Go 检查；work 根 override 不会被错误展开为所有成员政策。显式 use 的每个成员单独复制，不递归寻找其他模块；拒绝 symlink、特殊文件、增强旧清单与无法映射的未知指令。外部 use 可映射到新输出的显式成员位置，plan 列出旧→新路径。

非 vendor 的本地非成员 replace 保持显式、规范化的原目录引用，不暗中复制任意目录；因此输出不承诺这种本地依赖自动自足。vendor 模式保留工作区根坐标下的 replace 政策，使其与原 modules.txt 一致；不要求已被 vendor 物化的原 replace 目录仍存在。

## workspace vendor

显式 `[workspace.vendor] mode="go-work"`，原 `## workspace` 必须存在；module vendor 仍是 mode=go-mod，二者不能混淆。逐字复制 modules.txt、所有包/资源/许可证与文件权限。锁记录独立内容摘要和政策摘要，不把内容哈希写成 h1，也不声称 go.sum 证明本地修改的 vendor。

用受管原生 go.work（完整主模块集合）和真实 `-mod=vendor` 检验一致性；绝不调用 go work vendor 重建输入。缺 package、错误 explicit/replace、未知条目、symlink、额外增强源码或多个 vendor 冲突均失败，不下载补全。供应的 vendor 含本地修改时保留修改；删除原 replace 目录和清空 modulecache 后仍应可离线构建。

## 白例与黑例

| 输入 | 结果 |
| --- | --- |
| A 无 require 却 import use 成员 B | plan 显示并物化 A→B 显式边；新目录可构建，原文件摘要不变 |
| A1.26/B1.27，work1.27 且有 godebug | 保留每成员语言 go 与 work 运行时政策；与原生 DefaultGODEBUG 比较 |
| 成员 replace 冲突，但 work replace 裁决 | 保留三层声明并由 Go 验证，不任挑一个 |
| root vendor 有修改字节，原 replace 目录已删除 | 保留字节/权限；冷缓存 -mod=vendor 成功 |
| A 新增对图外 C 的 import | 锁/显式可达图拒绝；不会因为 C 是成员而提供源码 |
| 改 GOOS、tags、Go 工具版本 | 配置范围错误，要求重新分析 |
| vendor 缺一个文件/包或被篡改且 --locked | 明确失败，不代理下载、不重建 vendor |
| 工作区中增强源码/脚本、未知政策、重复 module、模块级循环 | 明确拒绝；原目录和 NEWDIR 均不改变 |

依据：Go 官方 [模块/工作区](https://go.dev/ref/mod#workspaces) 与 [GODEBUG](https://go.dev/doc/godebug)，以及本仓库 RFC0026 的原生机制研究。最终验收使用临时多模块项目、本地 file proxy 和真实 Go，不访问私人第三方。

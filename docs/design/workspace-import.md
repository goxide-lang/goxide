# 原生 go.work 与 workspace vendor 迁入合同

状态：2026-10-04 第二阶段原生传输子集已实现，基于 `2aa0d066`。本文件先于生产实现落地；验收结果见 [迁入验收](../reviews/workspace-import-20261004.md)。保持原目录不变，仅在临时 staging 运行会写 Go 元数据的分析/构建命令，成功后发布新的输出目录。

## 接口与支持范围

可运行脚本见 [迁入示例](../../examples/workspace-import/README.md)。

`goxide import-go --workspace --plan ROOT` 显式读取 ROOT/go.work；`--out NEWDIR` 在不覆盖既有目录的前提下执行该方案。默认只有 plan。单模块 import-go 原接口保留。工作区可以自动识别当前配置的 main 包作为命名 bin、成员根 library；重名 bin 或没有可推导目标时拒绝，允许 `--member-bin MODULE::NAME=PATH`、`--member-lib MODULE=PATH` 显式指定。

首片是**原生 Go 传输模式**：输出仍以根/成员 goxide.toml 和根 goxide.lock 为作者权威，所有 go.mod/go.work 只生成在受管 target。保留每个成员自己的 go/require/replace/exclude/godebug、根 work go/toolchain/godebug/replace。不把 work.go 赋给所有成员；不把成员 godebug 合并成 work godebug。实际 list/build/run/test 使用同一受控 GOWORK。`GOTOOLCHAIN=local`，不自动安装工具链。

迁入的输入仍必须是原生 Go 工程；迁入完成后允许成员渐进加入 .gox、现有 features 与明确授权的构建脚本。额外 member-vendor 仍拒绝；增强依赖图当前连接显式成员，其他远端依赖保持原生 Go 版本来源。不扩展 registry、发布或 host resolver。

## 显式图与分析范围

迁入在 staging 使用真实 Go `list -deps -test -json` 分析各成员的实际包归属，生成根 path 模板和成员 `{workspace=true}` 边；不能仅按 import 字符串最长 module 前缀猜来源。原本无 require 的 `A import B` 会在 plan 明列新增 A→B 声明。原 native require 仍保留原版本；显式图不产生 `B@v0.0.0` 伪 require，也不改 vendor explicit 记录。

分析范围固定为迁入时的 GOOS/GOARCH/CGO_ENABLED、架构特征（如 GOAMD64）、GOEXPERIMENT/GOFIPS140、Go 工具版本及 C 工具/flags/PKG_CONFIG，且 build tags 为空，写入 `[workspace.go]` 配置。分析普通与测试包；CGO 开启时额外合并 race 配置的实际成员边（现有 goxide test 使用 -race）。切换配置明确拒绝并要求重新分析；不能把单配置分析冒称所有平台的无损迁移。源码或新的 import 改变后，构建仍按声明图检查，不能运行时借 go.work 自动扩图。跨成员包级无环但 module 级循环，首片明确拒绝。

构建某成员时仅复制其显式依赖闭包的源码；其余成员只保留原生 Go 元数据以维持全工作区版本政策。每个选中根独立校验，批量选择本身不使别的成员源码成为允许来源。纯 Go 源码保持原生路径；增强成员按依赖顺序在同一受管工作区展开，仍不为未选成员提供源码或执行脚本。

## replace 与路径

保留 work/member replace 的作用域和限定版本，冲突由真实 Go 检查；work 根 override 不会被错误展开为所有成员政策。显式 use 的每个成员单独复制，不递归寻找其他模块；拒绝 symlink、特殊文件、增强旧清单与无法映射的未知指令。外部 use 可映射到新输出的显式成员位置，plan 列出旧→新路径。

非 vendor 的本地 replace 若 module 身份和目录都明确对应 use 成员，改写到新成员，再映射到受管 Go 视图；迁入后删除原成员路径仍可构建。非成员 replace 保持显式、规范化的原目录引用，不暗中复制任意目录；因此输出不承诺这种本地依赖自动自足。vendor 模式保留工作区根坐标下的 replace 政策，使其与原 modules.txt 一致；不要求已被 vendor 物化的原 replace 目录仍存在。

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
| 迁入输入已带增强源码/脚本、未知政策、重复 module、模块级循环 | 明确拒绝；原目录和 NEWDIR 均不改变 |

依据：Go 官方 [模块/工作区](https://go.dev/ref/mod#workspaces) 与 [GODEBUG](https://go.dev/doc/godebug)，以及本仓库 RFC0026 的原生机制研究。最终验收使用临时多模块项目、本地 file proxy 和真实 Go，不访问私人第三方。

## 已实现命令与锁边界

```sh
goxide import-go --workspace --plan --offline /projects/original
goxide import-go --workspace --out /projects/migrated --offline /projects/original
goxide build --workspace --locked --offline /projects/migrated
goxide run -p example.org/app --bin web --locked --offline /projects/migrated
goxide test -p example.org/app --locked --offline /projects/migrated
goxide transpile -p example.org/app --bin web --locked --offline /projects/migrated
```

`transpile` 输出受管 Go workspace bundle，可由真实 Go 使用其 go.work 编译；增强成员在此阶段执行既有 lowering。所有 flags 在位置 INPUT 前。没有缓存的非 vendor 远端依赖初次迁入不能使用 `--offline`；此时仍由 Go 原生 proxy/checksum 机制获取，没有安装脚本。

根 schema 4 锁保存原生全工作区选版、各成员 sum、work.sum、政策输入摘要以及独立 vendor 清单摘要；它与增强作者模式的 schema 3 锁显式分开，旧 schema 不隐式升级。schema 4 在补齐逐成员记录后支持非vendor成员显式物化并复制根锁独立运行；workspace vendor 拆出仍要求另行明确转换政策。原本合法的非vendor本地 replace 是可变作者源码，锁不伪造内容不可变保证。

bin 自动发现只扫描显式 use 成员内当前配置的 Go packages（跳过嵌套 module），不会查找任意兄弟目录。重复目录名可通过重复 `--member-bin example.org/app::first=a/web` 与 `--member-bin example.org/app::second=b/web` 显式命名；对一个成员给出的首个覆盖会替换该成员全部自动 bin 列表。examples 不自动推断，迁入后可在成员 TOML 使用既有 `[[example]]`，仅 `--example NAME` 选择；没有新增发布功能。该模式目前拒绝 `-o`，使用受管输出。

输出目录、Go 视图和锁共用已有 owner/lease/原子发布机制；配置/成员/目标/normal-test 模式进入 unit 身份，每次仍创建新源码 generation。不存在复用增强外部 metadata 的旁路。未知嵌套 owner 标记拒绝，不能以伪造标记静默丢弃原工程普通目录。

## 渐进增强衔接（继续实施）

`70964a8` 完成的是原生 Go 保真迁入，尚非完整第二阶段。`.gox`、features、脚本与 schema4 拆锁的拒绝是临时实现缺口，不能成为用户迁入后的最终工作流边界。后续仍遵守 RFC0022/0023/0026，不通过删去 work/vendor 政策来绕过缺口。

增强编译将保持同一受管 go.work 与完整政策集合，仅生成显式活动依赖闭包。复用现有 Resolve feature 固定点、cfg→宏→类型检查顺序、目标选择和脚本主体授权。类型导入器默认仍 GOWORK=off，只允许构建器通过显式接口传入已拥有的 Go 工作区；不接受源码/脚本自行指定上下文。未选成员只有元数据，不注入源码。

脚本继续使用 RFC0023 的独立本地 host 快照和明确 trust/allow；本片不引入新的 host resolver。其产物参与普通 lowering，并在执行前后复核授权。workspace 成员身份与迁入操作都不是脚本授权。

拆出复制根锁仍按已确认规则只使用可达记录。必须分别处理原生 work 开发政策、成员独立政策及 workspace vendor 主模块集合，不能静默转换 vendor/丢失 replace/godebug。若某输入无法在既定显式物化规则下等价拆出，先提供最小正反例并明确拒绝该输入；普通无冲突拆出路径继续实施。

### 已完成衔接与真正需要单列的转换边界

新增可验证路径：成员逐步加入 `.gox`、现有 features/可选成员依赖/required_features、带原生 vendor 的增强宏，以及 RFC0023 明确授权的构建脚本。前端显式接受受管 Go workspace，不改变 cfg/宏/类型检查顺序；vendor 内容参与 frontend 配置与脚本计划身份。

schema4 增补逐成员输入记录；非vendor拆出显式物化继承/path后直接复制根锁，复用既有 Go/Unified可达解析和校验，不读无关成员源码、不激活无关脚本。显式本地成员路径可搬迁，逐节点仍核对模块身份与有效清单。成员自身 go/toolchain/godebug/replace 恢复生效；若来源因 work override 消失而改变，必须报错而非沿旧来源偷偷构建。旧schema4缺成员记录时先在原workspace普通构建补锁；复制锁的原字节保留。

**workspace vendor 拆出仍需显式转换合同**。最小反例：A require D，B require E，工作区 vendor 标记 D/E 都 explicit。A 独立后若原样复制整份 vendor，E 的 explicit 不再对应主模块 require，真实 Go 拒绝；若改 modules.txt 删除 E，则已不是原 vendor 政策/摘要；若保留隐藏的 B/go.work，则仍施加原 work replace/godebug，违反已确认独立政策恢复。普通非vendor A 可直接复制根锁、只校验D；vendor A 当前明确拒绝并要求显式转换，绝不擦标记或丢修改字节。建议另立最小转换协议：验证原快照后，依据独立政策派生vendor元数据，保留实际文件和原/派生摘要；在协议确认前暂停这部分，而不阻塞非vendor及workspace内渐进增强。


条件编译语法与既有前端完全一致：`//goxide:cfg` 在导入发现/宏/名称解析前移除；未选文件仍校验词法、token tree 与 package/import 前缀，完整声明/表达式语法按既有合同延后到启用时验证。本次未扩大为完整语法解析，也未引入新宏语义。

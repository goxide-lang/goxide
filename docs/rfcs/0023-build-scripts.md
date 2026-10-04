# RFC 0023：显式授权的宿主构建脚本

## 状态与范围

用户已确认第一阶段：代码/资源生成、本地 host 工具与依赖、声明输入增量缓存；根项目一次信任，第三方执行闭包单独精确许可；native linking 后置。本分支实现该子集。`build.gox` 仍是研究名称，实际入口支持 `.go` / `.hgo`，不更改语言后缀。

本 RFC 接续 [项目工具链](0022-project-toolchain.md)。研究历史保留于 `research/build-gox-rfc` 分支：`4c54b12290fffd5d593e674a403306a60b3c9708` 包含最初 RFC、官方 Cargo 差距对照、独立审阅与可复现 Go/OS fixture。旧研究中的严格 hermetic/sandbox 门槛与逐 target 运行属于旧候选；以本篇已确认的本地 declared-input 模式为准。

这不是 sandbox。普通 Go 可执行文件可以访问当前用户能访问的文件、网络、凭据和进程；快照、路径检查、清理范围和许可用于控制工具自身行为，不构成对恶意脚本的 OS 隔离。第一次需要许可时失败并给出诊断，不默认执行依赖脚本，也不隐式交互批准。

## 源级规则

### 显式声明

```toml
[package]
format = 2
module = "example.org/app"
go = "1.27"

[[bin]]
name = 'app'
path = 'cmd/app'

[build]
entry = 'tools/build/main.hgo'
env = ['SCHEMA_MODE']
inputs = ['schema.txt', 'templates']
outputs = ['gen/schema.go', 'gen/data.txt']
timeout_seconds = 60

[build.dependencies]
'example.org/codegen' = '../codegen'

[build.tools]
formatter = 'tools/bin/formatter'
```

`[build]` 缺省为关闭；TOML 没有 null，false 也不属于该表类型。入口必须在独立子目录，整个目录构成 host 包快照，不能包含 go.mod/goxide.toml/hgo.json。该目录不再作为 target 源包参与扫描。不能隐式寻找 build.rs、build.gox、Go generate 注释或依赖安装命令。

inputs 为包内明确的文件或递归目录，不支持 glob；目录成员和空目录均进入缓存身份。文件内容变更而 mtime 相同仍失效。路径必须规范相对路径；拒绝 `..`、链接、特殊文件、可检测的硬链接、保留目录。输出必须枚举具体文件，不能覆盖源码、manifest、lock、Go module 文件、配置/metadata/source-map 保留名称；缺少或多出文件均失败。每文件最大 32 MiB，快照闭包最大 128 MiB/10000 项；不是无限制资源配额或 OS 内存隔离。

env 只传入明确列出的已设置变量，未设置与空字符串在缓存中不同。`HOST`、`TARGET`、`OUT_DIR`、`SOURCE_DIR` 为 runner 提供；Go/CGO、PATH/HOME/TMP 与动态装载相关保留变量不能由声明覆盖。没有自动读取系统环境、时间或网络作为隐式缓存输入。

dependencies 是模块名到显式本地源码路径；它们只供 host 脚本使用，不加入 target 依赖图。Go/hgo helper 的完整本地依赖闭包都必须列出。go.mod 仅支持 module/go/require；不接受 replace/toolchain 等其他 directive、嵌套 module 或 helper 自己的 build 脚本。第一版不自动获取 registry/git host helper，也不复用 target resolver 的同模块 feature 节点。Git 来源的脚本及 helper/tools 不能越出该 checkout。

tools 是别名到已存在的本地可执行文件；执行前冻结二进制内容，独立许可，request 提供快照绝对路径。动态库、外部配置、工具再次调用的程序不被自动封装或追踪；工具闭包 hermeticity 不受保证。不运行安装器。

### 执行与授权顺序

1. 解析普通 target/feature/依赖图及锁定来源，验证 host-only 限制。
2. 冻结全部脚本、声明输入、本地 helper、工具、环境和配置；任何脚本运行前检查所有所需许可。
3. 根项目身份绑定规范化路径、module 和最近祖先 Git 仓库身份（含 worktree/common 配置；不含 HEAD）。无 Git 时为路径/module 范围。根脚本日常编辑不重复授权；改变根路径或仓库配置会改变身份。
4. 第三方 helper/tool 分别按内容摘要许可；非根包脚本还需整个执行闭包摘要许可，包含声明、来源与锁定 Git commit。来源或内容更新需新许可。输入变化也可能保守地使依赖脚本许可失效。
5. 缓存读取前再次授权；miss 时先依真实 host import 图准备 helpers，再 lower/build host 脚本，执行生成器，检查退出状态及输出契约。
6. 只有完整成功结果才原子发布；生成文件随后加入 target 的普通选择、lowering、类型检查与 Go build。脚本 cache hit 不跳过 target 源码转译。

脚本以 `generator /absolute/request.json` 启动。request v1 字段：`version`、`source_dir`、`out_dir`、`host_os`、`host_arch`、`target_os`、`target_arch`、`features`、`tools`、`env`。cwd 是声明输入快照，输出目录初始为空。`features` 是所属包解析后的 feature 集合，不能启用 target 依赖或反向改变 feature 图。stdout/stderr 仅日志，最多转发 1 MiB，不解释为 Cargo 指令或编译参数。

host 编译固定本机 GOOS/GOARCH、CGO=0、GOWORK=off、GOTOOLCHAIN=local、依赖获取关闭。环境通过调用参数传递，不修改进程全局环境。当前 frontend 仍拒绝非 host target；request 分开保留 host/target 字段不表示交叉编译已经可用。原始 `go.mod` target 依赖策略仍由项目工具链控制。

### 缓存与信任操作

```sh
hgo scripts --action plan ./app
hgo scripts --action trust-project ./app
hgo scripts --action allow --subject EXACT_SHA256 ./app
hgo run --bin app ./app
hgo scripts --action revoke --subject EXACT_SHA256 ./app
hgo scripts --action forget-project ./app
hgo scripts --action clean ./app
```

所有 flags 放在项目路径之前；可指定 `--target-dir`、`--go`、`--features`、`--no-default-features`、`--offline`、`--locked`。plan 输出 JSON 与逐主体 trusted 状态，不编译/执行脚本；依赖解析仍可能访问网络，应按需要用 offline/locked。allow 只接受当前 plan 中存在的完整摘要，不顺便信任根项目。revoke/forget 不依赖仍能解析旧脚本，缓存存在也不能绕过撤销。

信任记录为用户配置目录 `goxide/build-trust/grants.json`，可由 `HGO_TRUST_DIR` 指定给 CI/测试；CLI 拒绝设置到当前图源码、选定产物根及任何已知受管布局内。项目声明不能授予信任。policy lease 覆盖整次脚本项目构建/运行，防止协作命令在授权使用中途成功撤销；并发修改返回 `B-TRUST-BUSY`，并非即时终止已启动程序。全局 lease 暂时串行化使用同一 store 的不同项目，崩溃遗留锁需确认无进程后人工处理。target 执行期间也持有 lease，长运行服务须注意这一限制。

脚本结果缓存键包含完整冻结输入、目录成员、工具、env、features/tags/race、host/target、编译器身份与脚本声明。package 配置不包含 bin/example 名，多个 target 可共享脚本结果。CLI 缓存位于所选 target 根的 `scripts/<host>/<target>/<package-id>/script-cache/entries/<key>`；通过 layout 包级所有权容器与 core 自有cache双层校验。它不放入逐bin Unit.ScriptCache，避免同包不同bin重复执行。输入/执行临时目录仍由runner创建并回收，生成文件保留在当次bundle；Unit.ScriptOut仍为预留。core独立单测的默认 `.hgo/build-scripts` 不是CLI默认。信任 store 与产物分离。Clean 只删除匹配所有权标记的脚本 entries，拒绝陌生结构或 busy lock，保留源码、信任及其他依赖缓存。不是全项目 clean/GC；保留target二进制与旧bundle。遍历先验证所有layout结构，再逐cache清理；后续cache所有权错误不会回滚已清理的其他cache。

超时覆盖 host lowering 的 importer 子进程、host 编译及运行，取消传播至 Go 查询；同步 CPU lowering 在包边界检查，不能保证实时抢占。CommandContext/WaitDelay 不保证杀死所有后代；脚本外部副作用不会回滚。失败不发布新 artifact，但之前完整缓存可保留。

## 转译示例

BS-01 白例：schema 输入显式声明，已批准根项目；host 生成器读取 request.SourceDir 下 schema.txt，在 request.OutDir 生成模块相对 `gen/schema.hgo`，其内容为：

```hgo
package gen
func Version() int { return 42 }
```

target frontend 将它与普通源码同等处理，生成 Go 的核心为：

```go
package gen
func Version() int { return 42 }
```

消费包 import `example.org/app/gen`，无需把生成文件写回源码。generated `.go` 和资源可使用正常 `go:embed`；文件入口映射仍遵循 RFC 0022，不自动移动同目录资源。完整可运行生成器见 [试用示例](../../examples/build_script/README.md)，自动验收见 [端到端测试](../../cmd/hgo/buildscript_test.go)。

BS-01 黑例：未信任根项目，输入完全相同，构建在任何 host 编译/执行前返回 `B-TRUST-ROOT`；不会因仅需 transpile 而绕过。trust-project 后再走以上顺序。

## 拒绝与边界

下面“实测”指代码测试；“机制/方案”不会冒充完整平台保证。

| 对照 | 白例输入、判定与顺序 | 黑例输入、判定与诊断 | 证据 |
| --- | --- | --- | --- |
| BS-02 依赖许可 | 精确批准当前 helper/tool/依赖脚本，root也可信：授权→缓存/编译→运行→发布 | 缺许可、撤销或更新脚本/tool/hash：先 `B-TRUST-SUBJECT`，即使旧cache存在也拒绝 | CLI helper/tool/dependency tests |
| BS-03 输入 | 声明schema文件及templates目录：内容/成员/空目录变化→miss→重跑 | 漏声明schema：快照读不到而运行失败；若脚本用硬编码绝对路径绕出快照，只能警示declared-cache可能陈旧，不能检测所有漏项 | core snapshot/目录 tests；绕出为既有OS机制证据 |
| BS-04 环境 | MODE在env：值变/设空/取消设置都影响键→重跑 | 未声明MODE不传入；声明PATH/GOFLAGS/OUT_DIR先拒绝 `invalid environment name` | core/CLI env tests |
| BS-05 路径 | 包内普通相对文件可快照→生成 | `../x`、输入/输出symlink、硬链接、Git checkout外helper拒绝；source-map保留名先拒绝。敌对同UID并发替换不受sandbox保证 | core path/link tests，frontend sidecar test |
| BS-06 host/target | 本机host脚本生成本机target：request与执行架构一致 | GOARCH设置成外架构：Prepare/frontend先拒绝cross compilation，生成器不执行 | CLI cross-target test；研究Go ELF交叉编译不是hgo支持 |
| BS-07 多target/feature | 两个bin同包配置：首run、后hit；feature变化：新键、重跑 | 正在构建的相同store/cache再次进入：busy而不混写；不允许混用不同env/tool/config的结果 | CLI incremental、core parallel lock tests |
| BS-08 失败与原子性 | 输出全部存在、退出0：校验→rename→target编译 | 写部分输出后sleep超时/非0/少文件/多文件：失败无新cache；修好后重跑 | CLI timeout恢复、core missing/extra/failure tests |
| BS-09 清理 | 自有cache且无锁：只删entries，下一次有许可才重跑 | 无owner/陌生文件/正在构建：拒绝clean；不能用clean撤销或授予信任 | CLI clean、core ownership tests |
| BS-10 离线/网络 | 显式本地helper，offline构建不下载host依赖 | 未声明远端host import：离线Go解析失败；脚本自行HTTP仍可能成功，offline不是网络隔离 | core helper；研究loopback实测 |
| BS-11 输出/flags | 声明gen/schema.go和data.txt：只按源码/资源消费 | 输出go.mod、config、map sidecar、额外文件：拒绝；stdout打印rustc-link/cfg无效，只作日志 | core contract/保留名tests；无flag protocol由实现保证 |
| BS-12 bootstrap | 独立本地helper无build脚本，按真实host import依赖顺序编译 | helper也有build或循环import、脚本import仅target阶段才产生的包：拒绝bootstrap或Go离线导入失败；不会先编target来满足host | core bootstrap/helper tests；后者阶段契约 |

普通脚本生成含危险运行时行为的 Go 代码，仍属于受信任代码执行；输出白名单不是内容安全审计。初版不提供严格可复现、共享远端缓存、自动隐式输入追踪、OS沙箱、持久工作目录、网络allowlist、native link flags、系统库探测协议、Cargo links/DEP传递或自定义cfg输出。

## 双向互操作

host `.hgo` 可以 import 已声明本地 Go helper，Go host 可以 import 已 lower 的 hgo helper；helpers不会自动变成target链接依赖。host与target若使用同module，是独立准备的角色视图，不会把host init混入target。生成Go源码是普通Go包，可被target hgo或Go包导入；生成hgo仍经过同一语言前端，不绕过类型检查/metadata约束。

与 Cargo 的取舍：采用 host 执行、OUT_DIR分离、按输入重跑的概念，但不直接兼容 stdout Cargo 指令。Cargo build.rs 的默认信任、mtime重跑与持久OUT_DIR不等于本工具策略。[Cargo官方build scripts](https://doc.rust-lang.org/cargo/reference/build-scripts.html)、[build dependencies](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html#build-dependencies)、[features](https://doc.rust-lang.org/cargo/reference/features.html)、[Go generate](https://pkg.go.dev/cmd/go#hdr-Generate_Go_files_by_processing_source)、[Go build](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)、[cgo交叉编译](https://pkg.go.dev/cmd/cgo)是研究来源。Go generate不由go build隐式执行；cgo交叉构建需要目标C工具链，本阶段不隐式引入。

## 验收与未定项

联合分支整合 `feat/target-layout` 的所有权/持久bundle接口，保留修改二进制拒覆盖、symlink/陌生产物拒绝及源码扫描隔离；不以文件合并无冲突代替联合测试。

独立审阅发现并修复：host lowering取消链遗漏；注释伪造helper依赖环（改真实imports与host选择）；重叠module最长匹配；生成source-map资源覆盖；monorepo信任遗漏祖先仓库。对应自编测试进入版本库，没有运行未授权第三方脚本。

复现使用 Go 1.27.1：

```sh
GOCACHE=/workspace/goxide-environment/cache /workspace/goxide-environment/go/bin/go test -race ./internal/buildscript ./internal/scripttrust ./cmd/hgo ./internal/frontend ./internal/typecheck
python3 scripts/check_design.py --docs-only
```

成本：每次plan读取与哈希执行闭包和工具；miss时host编译（复用Go自身缓存）；两层锁暂牺牲并发吞吐。收益：普通schema/embed无需手工先生成；输入变更自动失效；许可更新可检查；target语义继续由既有frontend负责。下一阶段真正的产品选择集中于：是否需要OS sandbox、何时引入远端host依赖resolver、native探测/链接接口；本轮无需再次确认已批准的生成子集。

整合来源、独立审阅修复和实测结果见 [联合验收记录](../design/build-script-validation.md)。

## version 来源衔接

[version 来源计划](../design/version-source-plan.md)要求完整选版/feature 图稳定后才准备并授权脚本，选中版本及 Go sums 进入非根来源身份；版本变化不能复用旧精确许可。现在由共享规划器与全图 module 预检实现；脚本 SourceID 绑定稳定计划及来源摘要。选版/许可测试只运行本仓库 localproxy 中的自编 fixture，不默认执行第三方下载脚本。

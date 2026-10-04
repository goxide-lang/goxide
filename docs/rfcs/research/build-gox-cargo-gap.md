# build.gox 与 Cargo build.rs：逐项差距及收敛建议

## 结论与比较基线

**现稿有相近的 host 生成器架构，但尚不是 build.rs 等价方案。** 最影响常见项目的是缺少可用的脚本依赖/工具入口、跨次增量复用、native 产物接入；其次是 package-local cfg、跨包 metadata 与 override。原稿额外选择的“root 每次闭包变动重授权、unconfined 一律重跑、每个 bin 独立生成”会明显增加摩擦，并非 Cargo 要求，也不是任意可执行脚本的必然限制。建议改成作者声明驱动的本地增量和一次显式 root 项目信任，将严格模式保留为可选；这仍需用户确认。

比较对象：

- 本仓库 [build.gox RFC](build-gox-proposed.md)，原提交 `bce1efd3b1a34cf95b70e13ef084b73f7981a350`。下文“原稿”均指该提案；**其中的 runner 能力一项也未因本文变成已实现**。
- 只读 fetch 的项目工具链 `fa1461f2e4d6008f15db07b46db1875369caeaa1`：[RFC 0022][H1]、[示例][H2]、[manifest 实现][H3]。它已有可选 hgo.json、feature 合并、Go/Git/path、锁与目标选择；没有脚本字段、host-build 依赖域或 runner。未切换/编辑该生产分支。
- 2026-10-04 读取当前官方 Cargo Book；[changelog][C0] 已列 Cargo 1.99（2026-10-01）。本机 PATH 和常见安装目录未发现 cargo/rustc，**未运行 Rust 机制试验、未下载安装工具链**。Cargo 结论来自当前官方规范，Go/OS 机制证据沿用[原实验结果](../../../tests/research/build_gox/results.json)。不把 Go 实验当 Cargo 实测。

### 最小差距表（可先给用户看）

| 需求 | Cargo build.rs | 原稿差距 | 建议优先级 |
| --- | --- | --- | --- |
| schema/protobuf | host helper / 外部生成器，结果供目标编译 | 原稿只有纯 stdlib 生成器首片，真实 protobuf 往往还要库或 protoc | P0：host helper + 显式固定工具；不自动安装 |
| 日常重复 build | freshness 命中可跳过脚本 | 默认每次重跑，且按 unit 拆分 | P0：作者声明的增量；默认 package/config 共享 |
| 版本/资源 | 编译期环境与生成文件 | 生成 Go 常量/资源可表达，缺便捷 API | P0：用 Go 常量、embed，没必要复制 env! |
| 条件编译 | cfg 与 check-cfg | 只有 feature cfg 已落地，脚本自定义 cfg 未落地 | P1：独立 typed cfg 域，不冒充 feature |
| C/C++、系统库 | cc/pkg-config helper + 链接协议 | 原稿全部延期 | P1：cgo/native adapter；这是 native 用例的硬缺口 |
| 安全/信任体验 | 通常 build 即执行应运行的脚本，无本 RFC 式逐闭包许可 | root/依赖均逐闭包授权、无隔离仍重跑 | 调整设计：root 显式信任项目，依赖精确批准；增量与隔离分开 |

表内 Cargo 生成与 native 用例见[官方示例][C2]，freshness/配置接口见[build scripts][C1]；优先级及 hgo 替代是本研究判断，不是官方建议。

## 一、已同等设计：方向相近，不代表实现/细节齐平

| ID / 能力 | Cargo 机制与来源 | 原稿及实际黑白对照 |
| --- | --- | --- |
| E1 host 编译、target 信息 | 脚本是 host 程序，读取 HOST/TARGET 与 CARGO_CFG_*。[环境][C3] | 原稿 request 分离两者。白：amd64 生成 arm64 源码；黑：用脚本 runtime.GOARCH 代替目标架构。Go fixture 仅验证分离与 ARM64 ELF；hgo cross-target 仍拒绝。 |
| E2 前置生成及失败阻断 | 脚本成功后才继续所属 package；失败使构建失败。[生命周期][C1] | 原稿 H→R→V→target 同方向。白：schema 生成后类型检查；黑：生成失败不能继续编译旧产物。原稿多了显式 seal/原子提交，不是 Cargo 等价事务保证。 |
| E3 生成源/资源 | 输出到 OUT_DIR，源码显式 include! 生成文件。[生成示例][C2] | 原稿生成 `.hgo`/`.go`/资源到 view。白：生成 model 类型；黑：生成文件未接入包，不能仅因写出就声称被编译。include! 的源码入口机制由 Go 目录包视图替代。 |
| E4 feature 已解析再跑脚本 | 脚本可读取所属 package 的已启用 feature；cfg 输出不重做 feature resolution。[环境][C3]、[resolver][C6] | 原稿同意此顺序。白：pretty 已启用，生成对应格式；黑：脚本输出 feature=network 不能动态激活 optional 依赖。Cargo 允许发出同名 cfg 的低层行为不等于启用真正依赖，见 A2。 |
| E5 配置/产物身份 | profile、target、host 与 target 构建有区分，cache layout 为内部细节。[cache][C8]、[profiles][C9] | 原稿有配置与产物 digest。白：ARM64 与 AMD64 不串用；黑：仅用包名作为 key。实际 0022 已把 feature/entry/compiler 配置标记接入 metadata，但没有脚本结果 cache；不能说已有完整 Cargo 式 unit cache。 |

Cargo 的顺序应按依赖边理解：host build-dependencies 就绪→编译 script；所需直接 links-provider metadata 就绪→执行 script；script 成功→所属 package 的消费者 target 编译。独立节点可并行，不是“每个 crate 的 build.rs 全部先跑一遍”或“lib/bin/example 各必跑一次”。不同 feature/profile/host/target 上下文可能需要额外 unit；本文依据文档只比较约束，不声称已测具体 scheduler 的运行次数。[build dependencies][C5]、[metadata][C1]、[cache][C8]

### 输入上下文映射

| Cargo 输入 | 原稿候选替代 | 白/黑例 |
| --- | --- | --- |
| `OUT_DIR` | runner 分配 stage/output 映射 | 白：写声明产物；黑：把它当可任意写 source 的许可 |
| `CARGO_MANIFEST_DIR` | package 源快照根/manifest identity | 白：定位该 dependency 的模板；黑：误用 workspace 根替代依赖包根 |
| `HOST` / `TARGET` | host/target triple | 白：host 上跑生成器；黑：直接执行 target ELF |
| `CARGO_FEATURE_*` / `CARGO_CFG_*` | sorted features / typed target cfg | 白：区分应用feature和CPU特性；黑：把 CPU `sse2` 当项目 optional feature |
| `OPT_LEVEL` / `DEBUG` / `PROFILE` | 明确 profile 设置 | 白：按实际优化/调试参数配置native工具；黑：只看PROFILE字符串推断全部设置 |
| `NUM_JOBS` / `CARGO_MAKEFLAGS` | 待设计资源预算/jobserver | 白：子工具遵守共享预算；黑：复制NUM_JOBS并让每个子任务都开满核 |

这些 Cargo 名称和作用见[官方环境表][C3]；hgo request 不是已存在的环境变量兼容层。

## 二、首版缺失：直接限制常见 build.rs 用例

| ID / 缺口 | Cargo 的现有能力 | 原稿代价、具体白/黑及建议 |
| --- | --- | --- |
| M1 build-dependencies / helpers | `[build-dependencies]` 独立于普通/dev 依赖；可再依赖其他 Cargo 包。host 条件的 build dependency 在 host 上选。[dependencies][C5] | 原稿首片可限 stdlib，helper 自带 script 一律拒绝。白：纯 host JSON helper 可纳入锁与信任闭包；黑：现有 target dependency 不自动成为生成器 import。对 protobuf，先允许无脚本的 host helper 和显式 protoc 工具，否则“支持代码生成”只覆盖玩具或自写生成器。嵌套脚本开放仍需逐闭包授权。 |
| M2 rerun / 增量 | `rerun-if-changed` 报告路径、目录递归；`rerun-if-env-changed` 报告调用 Cargo 时的外部 env；默认没有 rerun 指令时用 package 文件集合（受 include/exclude 影响）。mtime 变化检测。[manifest][C4]、[变化检测][C1] | 原稿只静态声明，且 unconfined 禁跨次 result reuse。白：proto/env 未变应复用；黑：漏报 imported.proto 可能陈旧，不能称已自动追踪。建议先静态完整输入+本地声明驱动缓存；动态 dependency discovery 不必首片照搬。 |
| M3 native 编译/查找 | cc、pkg-config、cmake 等是生态 helper，非 Cargo 内置 C 编译器；它们对接 native 输出。[官方示例][C2] | 原稿不支持 native adapter。白：固定 host cc 编译 target archive；黑：随手调用 host gcc 后把 host `.a` 链进 arm64。优先复用 Go/cgo 工具能力与 sysroot 配置，不承诺自动安装 C 工具链。 |
| M4 link-lib / search | `rustc-link-lib` 指定库，`rustc-link-search` 指定查找路径；含库种类/搜索种类的 Rust 语义。[链接协议][C1] | 原稿没有 typed native result。白：`foo` 库+已验证目录+target ABI；黑：把目录任意拼进 GOFLAGS。需要 native adapter 映射，不能把 Rust 字符串直接传 Go。 |
| M5 link-arg 作用域 | 可针对 bin 名、全部 bins、tests、examples、benches、cdylib 发 linker 参数。[链接协议][C1] | 原稿全拒绝。白：只给某个 CLI 增加所需 native 链接设置；黑：传播给所有 dependency。应先覆盖实际 Go bin/cgo 需求，Rust cdylib/crate-type 无须成为 hgo 通用接口。 |
| M6 cfg / check-cfg | 可设置编译 cfg，并登记允许的 cfg 名/值以支持 lint。[cfg][C1] | 原稿有 typed schema 设想但明确未实现。0022 `feature(...)`/filecfg 不是这个能力。白：检测可用 API 后设 `has_fast_api`；黑：新值 typo 若无 schema 会静默选错。新增独立命名域和包作用域，不能全模块 tags 注入。 |
| M7 links / metadata / DEP_* | links 名唯一性参与 resolver；links 要求该 package 有 build script（override 可替代执行）；script metadata 传给直接依赖者的 script，非任意传递广播。[resolver][C6]、[环境][C3] | 原稿 BuildArtifact 是内部 provenance，不是跨包公共 metadata API。白：foo-sys 告诉 adapter headers 路径；黑：让不直接依赖的第三层自动拿 DEP_FOO。需要版本化 native provider/typed metadata，不能把 feature cfg 当 metadata。 |
| M8 override | `[target.<triple>.<links>]` 可提供链接/cfg/env/metadata，使对应脚本不编译、不运行。[config][C7] | 原稿只有“选已生成 dist”替代，不等于 per-target native override。白：发行版预装 target SDK，直接提供校验后的 artifact；黑：只禁脚本、却不提供依赖必需的产物而仍报成功。native 阶段值得补，可减少授权/不确定探测。 |
| M9 打包验证 | cargo package 生成 archive，再解包构建并检查 script 未改 source；可用 no-verify 跳过验证。[package][C11] | 0022 没有 package/publish 命令；原稿也没实现 pristine 打包验收；0022 当前 bundle 的绝对 replace 移动后需重生成，不能当成已完成可搬迁 dist 发布。白：archive 带 schema/template，消费者能生成；黑：只在作者工作树有 schema，发布后失败。先补发布内容清单/新目录 Go 消费测试，无需立即造 registry。 |
| M10 warning / error | warning 是非致命诊断；error 会在 script 结束后令构建失败，即使程序本身返回成功。[诊断][C1] | 原稿结构化 warning+非零退出，尚未定义结构化 error/显示策略。白：找不到要求的 SDK 报 error 并终止；黑：把 error 当普通日志且 exit0 后继续。SDK 应区分 fatal/error 与 warning，并保留依赖来源和可见性规则。 |

M4–M5 的范围不能过度简化：Cargo 的 link-lib 通常仅给所属 package 的 lib target；没有 lib 时才给该 package 所有 targets。`rustc-flags` 的 script 通道也只允许 `-l`/`-L`，**不是任意 rustc flag 注入通道**。指令顺序可影响 native linker 顺序，hgo typed 数组必须保序；不能以排序规范化破坏静态库顺序。[链接规则][C1]

## 三、刻意不同：有取舍，不能算 Cargo 自带的安全性

| ID / 策略 | Cargo 默认/契约 | 原稿的黑白对照与评估 |
| --- | --- | --- |
| D1 自动发现/禁用 | package 根 `build.rs` 默认发现；`package.build` 可改 entry，`build=false` 关闭发现。[manifest][C4] | 原稿必须显式 path，尚无冻结 disable 字段。白：配置 entry 明确启用；黑：仅放一个 build.gox 不执行。适合渐进增强，可加统一 false/disable 表示；不必复制默认发现，也不改 `.hgo` 后缀。依赖源码自身的 build=false 不是消费端任意禁用脚本的总开关。 |
| D2 root 与依赖许可 | 标准文档的 build 路径会执行图中所需脚本，没有本 RFC 的按 subject 授权/撤销协议。[生命周期][C1] | 白：原稿经本地许可后执行；黑：新 clone、未准依赖在编译前停住。它减少意外执行，但自编 root 改 helper/工具链也反复审批，且批准后仍可任意 OS 操作。建议 root 项目范围一次显式信任，依赖保持内容锁定许可；均不由 manifest 自授。 |
| D3 缓存与隔离绑定 | Cargo 的 freshness 契约依赖脚本作者报全输入，不以强 sandbox 为前提。[rerun][C1] | 原稿无隔离就每次跑，安全性并不因此增强，反而更频繁执行同一可信程序。白：新建议允许 `declared` 增量；黑：不能将该缓存标为 hermetic/证明无漏读。保留 always-run/debug 和要求 sandbox 的严格模式，见后文修订。 |
| D4 stdout 协议 | stdout `cargo::` 行是控制协议，其他输出非指令；旧 `cargo:` 仍有兼容含义，旧式未知 key 可作为 metadata。[协议][C1] | 原稿 request/result JSON，stdout 仅日志。白：打印 `cargo::...` 也不会改变 hgo flags；黑：result 未知字段拒绝。JSON 是合理替代；应提供小 SDK，不让作者手写重复协议。必须能表达 ordered link actions，warning/error，不只文件列表。 |
| D5 输出寿命/发布 | OUT_DIR 可跨重跑保留，不保证空。[环境][C3] | 原稿 fresh stage+exact outputs+seal。白：失败不提交新对象；黑：依赖上一轮临时 `.o` 的 native build 每次重做，动态生成多个文件还要列清单。保留原子最终产物；后续可加独立私有 work cache，不能把 workspace scratch 与 committed artifact 混为一体。 |
| D6 env 与输入路径 | Cargo 提供工作目录/manifest 路径及上下文，脚本也能使用 OS 环境；这些是构建约定。[环境][C3] | 原稿显式 env、snapshot 路径，无继承 PATH/凭据，拒绝 symlink/hardlink。白：工具绝对路径已解析后运行；黑：传统 generator 按 PATH 找 protoc 或沿 symlink 模板会被 adapter 拒绝/读不到。允许显式 tool/source-root 映射，但“少传 env”仍不是访问限制。 |
| D7 每个 unit 跑一遍 | Cargo script 属于 package 构建，不能简单等同“每个 bin 一个脚本”；不同 host/target/profile/features 构建上下文可产生不同工作。[cache][C8]、[profiles][C9] | 原稿保守把 unit 名放 key，强制 lib/bin/example 分开。白：真正 unit-specific 生成隔离；黑：同 package 三个 CLI 共享 schema 却生成三次。建议默认 package/config scope，只有请求 unit 上下文时才拆分，不能仅删 key 中 unit 而仍传不同 unit request。当前无 Cargo 实测计数，不承诺任意 cargo 命令都恰好执行一次。 |
| D8 clean 与失败边界 | cargo clean 有 target/profile/package 等范围，也有 dry-run；不是通用脚本副作用回滚。[clean][C14] | 白：原稿仅删除自有 cache/lease；黑：不能清除 script 写到 source 外的任意副作用。Cargo 不承诺原稿的 fresh-stage 事务；我们也不能把“不发布部分输出”说成能撤销网络/文件操作。 |

### 保守程度：应该修订，不必因此照搬 Cargo 全部隐式行为

设同配置重复构建 B 次、独立 target unit 有 U 个、该生成器单次耗时 S（含文件 I/O/外部工具）。原稿脚本执行部分近似 B×U×S；若 package 输出通用且输入未变，声明驱动复用可近似 S 加 freshness 检查。**这是机制成本模型，不是 Cargo benchmark**。host binary cache 仅省掉编译生成器；它不省 protobuf 重生成、模板扫描或 pkg-config 的执行，Go 下游 cache 也不能替代这一层。fresh bundle 的路径/配置稳定性还会影响 Go 自身复用，不能保证生成字节相同就完全零成本。

root 精确 hash 许可的开销更像开发流程问题：改自己写的生成算法或 helper 就打断一次，升级 Go 也可能要求重新许可；却未带来 OS 隔离。合理分离是 **cache identity 精确，trust policy 有用户选定的范围**。工具链版本变化至少使 cache 失效，不必一律使 root 项目信任失效；新增 native tool/权限/依赖脚本仍需符合原批准范围。依赖 hash 改动仍暂停许可，避免 root 的宽松模式顺便批准未知第三方代码。

## 四、Go 生态不适用或已有更直接替代

| ID / Rust 接口 | hgo/Go 替代及黑白边界 |
| --- | --- |
| A1 rustc-env / env! | Cargo rustc-env 常由 env! 在编译期读取（cargo run/test 也传这些值，但官方不鼓励依赖该运行环境）。Go 版本号/Git SHA 生成 `const BuildVersion = "..."` 最直观；若确需 link-time 注入，Go `-X importpath.name=value` 仅对合适 string 变量生效，不是任意值或普通 const。白：程序脱离构建环境仍读到嵌入版本；黑：用运行时 os.Getenv 冒充编译期固定值。[Go linker][G1] |
| A2 rustc-cfg / check-cfg | Go tags 是 Go 文件级选择，hgo 可新增 typed cfg；不能机械复制 Rust cfg grammar。白：文件级屏蔽不支持 API；黑：`if cfg!(...)` 的另一分支仍需类型合法（0022 限制），不能当 Rust `#[cfg]`。Cargo script 的 `rustc-cfg=feature="x"` 可以影响 Rust 条件编译，但**不会激活真实 optional dependency**；我们的保留名拒绝是额外约束。[cfg][C1]、[0022][H1] |
| A3 include!/OUT_DIR | Go 生成文件加入正确 package view；资源用 `//go:embed`。白：把资源放在生成包可匹配路径；黑：embed 使用 `..`、symlink 或未匹配文件不能绕过 Go 规则。0022 单文件 entry 会搬移文件，不搬其邻接资源；当前含 embed 的入口应选 directory，不能声称 runner 已自动修复。[embed][G2]、[示例][H2] |
| A4 cc / pkg-config | cgo 已会调用 C compiler，支持源码旁 C 文件及 `#cgo pkg-config:`；简单 Go 原生 C 绑定不一定需新脚本。但 hgo frontend 支持范围、目标 sysroot、封装 metadata 还要端到端验证。白：固定 target C compiler +库配置；黑：CGO_ENABLED=0 下仍要求 import C 或把 host 库当 target 库。[cgo][G3] |
| A5 link-lib / link-arg | Go 内部链接/cgo/外部 linker 与 Rust crate/link 模型不同。白：typed native libs/search/ordered args 经对应 adapter；黑：将 Rust `static:+bundle=foo` 字符串原样塞 GOFLAGS。无需首版提供 benches/cdylib 全族；扩展前先列 hgo 实际产物类型。[cgo][G3] |
| A6 任意重跑生成指令 | Go generate 是显式作者工作流，Go build 不自动执行。白：提交生成 `.go` 给普通 Go 消费者；黑：只发布 build.hgo+schema 就称普通 Go 可编译。Cargo 源码包可携带 build.rs 供消费者重建，两生态发布策略不同。[Go generate][G4]、[package][C11] |

## 五、仍未定：需要接口或验收，不能用名称补齐

| ID / 事项 | Cargo 对照、实际例子及需收敛内容 |
| --- | --- |
| U1 工作区/作用域 | Cargo workspace 共用锁和 output 根，各 member 是 package；virtual root 无自身 package build script；配置发现从 invocation 目录及祖先读取，workspace 根调用不会读取每个 member 的 .cargo/config。[workspace][C10]、[config][C7] 白：成员各自生成；黑：把根 build.rs 当全 workspace hook。0022 无 workspace resolver；我们的 root 项目信任可以先只指当前 module，不能暗中授权所有 path dependencies。 |
| U2 host feature 域与 target-specific deps | Cargo resolver v2/v3 对 build/normal feature 域做区分；v3 是 edition 2024 默认。[resolver][C6] 白：host helper 开 std，target 库保持 no_std；黑：把 build helper feature 并入 target。0022 当前按 module/source 合并，没有 build-kind；扩展必须显式加 role/context。`[target.'cfg(unix)'.build-dependencies]` 是按 host 匹配，不是最终 TARGET。[dependencies][C5] |
| U3 unit graph、并发资源 | Cargo jobs/jobserver 协调编译与 script 子任务；单个 script 初始占一个 slot，多核 helper 要协调。[jobserver][C1] 白：cc parallel 受 jobserver 控制；黑：十个 script 各自启动全部 CPU。0022 每次只选一个 root target，`.hgo/build.lock` 冲突即失败；不能承诺已有跨 target 并行调度。需先区分 node reuse 与并行 executor，再考虑协议。 |
| U4 cross target / probe | Cargo 可在 host 执行脚本，给 target 信息；`target.runner` 用于 run/test/bench，并不自动拦截脚本自己启动的 target 程序。[config][C7] 白：用目标编译器 compile-only probe；黑：交叉编译时直接 exec target probe。hgo frontend target sizes/metadata 仍需扩展。支持脚本 request 不等于支持所有目标平台。 |
| U5 取消/超时/资源限制 | 当前引用的 Cargo Book 没有承诺统一 build-script timeout、撤销信任或杀净恶意 daemon 的安全契约。白：脚本退出非零使构建失败；黑：据此推导网络副作用回滚、所有 descendants 被消灭。原稿有 timeout/epoch 设计，OS-specific backend 未实现；Go CommandContext 反例只证明该 Go 默认机制，不能冒充 Cargo 取消实测。 |
| U6 可复现/缓存完整性 | Cargo lock 约束依赖选择，freshness 不等于 hermetic。白：工具版本、schema 导入树、env 都列入输入；黑：网络响应、时钟或 `.git/packed-refs` 漏列导致旧结果。我们需定义 declared/volatile/strict 等级、输出摘要和受信 provenance；严格缓存/远端缓存另验收，不应阻塞普通本地增量。[lock][C12]、[unstable][C13] |
| U7 discovery / disable UX | 应决定 hgo 是仅显式 path、兼容默认名，还是两者；目前 hgo.json v1 未知字段直接报错。白：未来显式 disable 不误执行；黑：今天在 hgo.json 写 `build` 字段便声称可用。建议先显式 path+disable，别为模仿 build.rs 全项目换后缀。[manifest][H3] |

### 离线、信任、沙箱：需精准分开

Cargo `--offline` 限制 Cargo 网络访问；`--frozen` 相当于 locked+offline。它们不是 script 的 OS 网络沙箱，也不是内容闭包批准记录。标准 build-script 接口允许作者运行任意构建任务，本文没有找到其提供原稿那种细粒度 trust store/revoke/网络隔离契约，不能将原稿的建议写成 Cargo 机制。要建立强保证，需要外部/独立隔离能力。[CLI offline][C12]、[build scripts][C1]

白：Go 与 Cargo 都可在依赖已缓存时离线编译纯生成器；黑：不能由 offline 推出脚本 HTTP 被阻止。本轮没有 cargo 二进制，因此 Cargo 的网络/取消未实测。原 Go fixture 已证明 GOPROXY=off 下 loopback HTTP 成功，但不证明 Cargo 使用相同内部实现。namespace 失败记录也仅限本云环境的两种探针。

## 常见场景：最值得补什么

| 场景 | 白例：期望可用路径 | 黑例/当前缺口 | 最小补齐 |
| --- | --- | --- | --- |
| protobuf/schema | 固定 host generator/protoc，完整 schema imports → 声明 `.go` | 仅 stdlib 时难复用成熟生成器；漏列 imported proto 可错误命中 | host-build helpers/tools +静态 inputs/目录+增量+package view |
| Git SHA/版本 | 由调用方传 `VCS_REV`/版本参数，生成常量 | 只 watch `.git/HEAD` 忽略 symbolic ref、packed-refs、worktree；用时间戳导致每次变化 | 生成常量 helper；显式 env/input；不强制内建 git subprocess |
| 嵌入静态资源 | 目录 entry 保持资源相对包路径，embed 编译 | 文件 entry 搬移后找不到资源；不能期望全局 OUT_DIR 能被任意 embed | 目录/输出映射；无需 Rust include! 同构 API |
| 编译 C/C++ | target compiler/sysroot 明确，archive 为该 target ABI | host gcc 默认不保证交叉目标；简单复制 cc crate 名称无意义 | native/cgo adapter +tool identity+ordered link metadata |
| pkg-config 系统库 | host pkg-config 查询 target sysroot，typed libs/search | PATH 上 host pc 文件可能误选；查询工具 stdout 不应 shell 拼接 | 工具和 sysroot 接口；probe volatile 或明确 fingerprint；后续 override |
| 目标探测/cfg | 已知 target config 或 compile-only probe，产出包内 cfg | 运行 target binary；用 cfg 输出启用新依赖；把宿主 runtime 架构当目标 | typed cfg schema +host/target分离；跨前端能力单独实现 |

## 建议收敛为三个用户选择

1. **采用“显式接入、一次 root 项目信任、依赖精确许可”吗？** 推荐是。root 可选择当前 module/session/project 范围信任，修改自有生成器仅使 cache 失效；第三方 host 执行闭包（含普通 helper 的源码/init、已安装工具内容）、来源变化、能力越界仍检查；并不限于另有 build script 的依赖。持久 root scope 绑定 canonical module 路径、仓库身份与本地显式批准记录，manifest 不能扩大范围；branch/pull 更新该 root 源码不逐 hash 提示，是用户选择项目级信任时明确接受的取舍。依赖批准可一次展示一批精确 subject，但不能泛授所有未来依赖。严格逐 hash root 许可仍保留选项。它比原稿轻，比 Cargo 默认更显式。
2. **允许作者声明驱动的本地增量吗？** 推荐是，不等强隔离。静态 inputs/env、源码/helper/tool、target/features/profile/output schema 入 key；每次命中先查 trust/revoke 与产物完整性，标为 `declared`，明确可能漏报，提供强制重跑。默认 package/config scope；特殊 unit scope 显式选择。严格 hermetic/remote cache 以后做。这样可缩小最大的开发体验差距。
3. **首版边界选“生成器够用”，native 紧随其后吗？** 推荐 P0 同时支持纯 host helper/显式已安装工具、生成源码/资源、增量、warning/error 小 SDK；P1 加 package-local cfg、cgo/native metadata，再补 links/provider override。仅 stdlib demo 不足以覆盖常用 protobuf；完整 Rust linker flag 家族无需首版照搬。

这些是**对 bce1efd 提案的修订建议**，未获批准，未改变生产语义。具体协议/manifest 版本、工作区定义、持久信任存储仍需与 0022 接口对齐。

### 增量建议的具体执行契约（避免一句“像 Cargo”掩盖问题）

候选 `declared` 模式：resolve host/target 闭包→快照完整执行闭包及静态输入并算 subject/key→验证该快照的授权范围/epoch→确认 cache 产物/hash/provenance。命中时在绑定前再次查 epoch；未命中时编译/运行同一快照→验证产物→复查 epoch 后原子提交→绑定前再复查。策略更新与发布/绑定须以锁或 compare-and-commit 协调，不能留下检查通过后撤销却仍提交的竞态窗口。漏列输入不报“安全成功”，只代表作者契约未满足；强制重跑用于诊断，不是防恶意功能。系统探测默认 volatile，可显式升级为已固定 sysroot/tools 的 declared。产物损坏、schema/policy/source 变化、授权撤销均不能复用。

若将来支持动态 rerun 列表：上次成功的 discovery list 参与下次检查；首跑无列表必须运行；脚本/helper 内容变必跑；新增 schema 文件要依赖父目录 inventory。输出 atomically 提交新的列表与产物。不能检测“脚本从未声明的任意新外部文件”是固有限制，应文档说明；无需以此否定所有本地增量。首片可不支持动态列表，先用显式目录输入覆盖 generator import 树。

package-scope 共享必须保证 request 不暴露 bin/example 名等 unit 信息，outputs/config 也不因 consumer 改变；其 key 包含 package/source、target、features、profile、工具等全部上下文。unit-specific 脚本继续把 unit 放 key。已落地 0022 的最终模块配置标记仍可含 selected target；需把 script-node key 与最终 view key 分开，不能直接删现有配置指纹字段。

### 对已落地 0022 的具体接入点（仍仅提案）

只读核对 `internal/project/graph.go`：Node/requests 目前按 module 组织，`Graph.Load` 没有 host-build role；因此 build dependencies 不能简单塞进普通 dependencies 后沿用 feature 并集。`cmd/hgo/project.go` 的 `projectLayout` 决定 entryFiles，`CompileModuleWithOptions` 消费 Configuration/Features/EntryFiles；脚本生成必须先构造经过验证的额外输入 view，再进入该调用，生成文件要保留原 source/provenance 映射。脚本 unit 的输出目的包必须明确映射，不能向 `hgo_targets` 或 `hgo_project_config.go` 等保留路径注入。

配置标记已有 source/target/compiler/metadata 失效链，可承接 BuildArtifact digest；目前没有持久转译 cache，不能把 Go cache、项目配置指纹和脚本 result reuse 混称一个现成设施。原生依赖 build list 不一致仍按 0022 拒绝，脚本不能更改这个 resolver 决策。跨 workspace、multi-target 并发、host role 需要独立扩展；不在研究分支修改那些生产文件。

## 当前版本核验与容易误写的细节

- `cargo::KEY=VALUE` 与新 metadata 语法的 MSRV 是 1.77；旧 `cargo:KEY=VALUE` 有兼容规则。`rustc-check-cfg` 自 1.80 被尊重，`cargo::error` 自 1.84 被尊重；error 指令能让脚本结束后的构建失败，不能只认进程 exit code。[build scripts][C1]
- warning 正常主要显示本地 path 包，非 path dependency 默认抑制（失败或 -vv 可见）；1.97 稳定的 `build.warnings` 控制的是 lint warnings，不应误当能把所有 `cargo::warning` 自动升级为错误。[config][C7]
- `OUT_DIR` 保留状态是当前文档明确写出的行为，不宜假设 Cargo 每次清空并原子提交。build-dir 与 target-dir 可以分开配置，不能硬编码 `target/debug/build` 作为协议路径。[environment][C3]、[cache][C8]
- nightly `-Zchecksum-freshness` 仍列 unstable，而且文档明确 build-script 读取文件暂时仍用 mtime；不是“新 Cargo 已对所有脚本做内容哈希追踪”。`host-config`/`target-applies-to-host` 也不能冒充默认稳定统一行为。[unstable][C13]
- `CARGO_FEATURE_*` 表示 package feature，`CARGO_CFG_TARGET_FEATURE` 表示 CPU feature，二者不同；OUT_DIR/TARGET 等运行上下文应在 script 运行时读取，不能用编译 script 时的 env! 代替。[environment][C3]
- `rerun-if-env-changed=TARGET` 不是监控 Cargo 生成 target 上下文的办法；该指令针对 Cargo 调用者的外部环境。env!/option_env! 的环境依赖自动检测从 1.46 已存在。[变化检测][C1]
- `[target.<triple>.<links>]` override 是 native links 定向替代，warning/rerun 指令不适用；不是任意 package 名的 build-script disable API。[override][C7]
- clean 最近也在演进：1.97 changelog 记录了显式 target-dir 不像 Cargo 目录时的错误检查。因此不能写“Cargo clean 对任意路径毫无保护”；也不能由该检查推导防恶意脚本的 OS 隔离。[changelog][C0]

## 验证范围与剩余限制

本轮仅文档研究及只读源码核对，未增加 production runner。Cargo 指令例子是官方语义分析，不是本机执行记录；未测 Cargo 实际运行次数、race/取消、跨平台或 native linker。Rust toolchain 缺失未阻塞规范比较，也未擅自下载。

原 Go/OS fixture 足以支持“普通程序不是 sandbox”的机制结论；不重复运行未改的 fixture。对照文档及相关链接使用仓库 `python3 scripts/check_design.py --docs-only` 校验；该工具不核验外部官网内容，也不编译 Rust 示例。独立审查记录见 [Cargo 对照 review](build-gox-cargo-review.md)。

## 官方来源与精确项目基线

[C0]: https://doc.rust-lang.org/cargo/CHANGELOG.html
[C1]: https://doc.rust-lang.org/cargo/reference/build-scripts.html
[C2]: https://doc.rust-lang.org/cargo/reference/build-script-examples.html
[C3]: https://doc.rust-lang.org/cargo/reference/environment-variables.html
[C4]: https://doc.rust-lang.org/cargo/reference/manifest.html
[C5]: https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html
[C6]: https://doc.rust-lang.org/cargo/reference/resolver.html
[C7]: https://doc.rust-lang.org/cargo/reference/config.html
[C8]: https://doc.rust-lang.org/cargo/reference/build-cache.html
[C9]: https://doc.rust-lang.org/cargo/reference/profiles.html
[C10]: https://doc.rust-lang.org/cargo/reference/workspaces.html
[C11]: https://doc.rust-lang.org/cargo/commands/cargo-package.html
[C12]: https://doc.rust-lang.org/cargo/commands/cargo-build.html
[C13]: https://doc.rust-lang.org/cargo/reference/unstable.html
[C14]: https://doc.rust-lang.org/cargo/commands/cargo-clean.html
[G1]: https://pkg.go.dev/cmd/link
[G2]: https://pkg.go.dev/embed
[G3]: https://pkg.go.dev/cmd/cgo
[G4]: https://go.dev/blog/generate
[H1]: https://github.com/hxsf/goxide/blob/fa1461f2e4d6008f15db07b46db1875369caeaa1/docs/rfcs/0022-project-toolchain.md
[H2]: https://github.com/hxsf/goxide/blob/fa1461f2e4d6008f15db07b46db1875369caeaa1/examples/project/README.md
[H3]: https://github.com/hxsf/goxide/blob/fa1461f2e4d6008f15db07b46db1875369caeaa1/internal/project/manifest.go

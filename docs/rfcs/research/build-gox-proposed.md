# 研究 RFC：受信任的 host 构建脚本（build.gox 候选名）

## 状态与范围

**状态：供用户确认，未批准、未实现 production runner。** 用户确认的是先研究设计并提交黑白例子，不是允许执行任何依赖脚本。本次只添加研究文档与自编临时 fixture；不改生产代码、不改全项目后缀。

研究基线：2026-10-04 fetch `feat/use-macro-reexports`，提交 `0dfe58bc7b529819e6402aa19ca53f6a3d473a6c`。本机 Go 为 `/workspace/goxide-environment/go/bin/go`，`go1.27.1 linux/amd64`。仓库及父工作目录未发现适用 AGENTS.md 或 `.agents/skills`。遵循[既有 RFC 规范](../README.md)与[模板](../TEMPLATE.md)，不抢占正式 RFC 编号。

现状证据：

- [CLI](../../../cmd/hgo/directory.go) 先 `CompileModuleWithOptions`，再执行 Go；每次建 fresh 临时模块，并限制输出不能覆盖已有文件。
- [ModuleOptions](../../../internal/frontend/module.go) 目前仅 tags/race；目录模式拒绝非 host GOOS/GOARCH，尚不能宣称 hgo 交叉编译已支持。
- [localEnv](../../../cmd/hgo/main.go) 设置 GOPROXY/GOSUMDB off，但继承很多宿主环境；不是脚本隔离接口，不能原样用于新 runner。
- [PackageFingerprint](../../../internal/typecheck/importer.go) 已包含 Go 版本、构建配置、选中文件和依赖指纹。脚本产物需进入这个下游身份链，不能只把生成文件路径放进 cache key。
- feature/default/transitive、Go module/git/local、多 bin/examples 正在另一个任务设计实现。这里仅提构建图接口，不假定它的 manifest 或类型已冻结。

## 决策摘要：只需确认三项

1. **是否接受本 RFC 的信任起点？** 推荐 root 与依赖都要本地显式授权；root 可一次授权当前内容闭包，依赖按精确来源/内容闭包单独授权。manifest 只声明意图，不能自授执行权。普通 `build` 不默认运行新 clone 的 root 脚本。
2. **首片能力是否限制为 host 纯 Go/hgo 生成器 → 声明的 `.hgo`/`.go`/资源？** 推荐是；typed package-local cfg 可紧随其后；本机库探测、原生编译与 linker flags 延期。名称先显式路径 `tools/build.hgo`，`build.gox` 只保留候选别名，不改语言后缀。
3. **是否接受跨平台第一版为显式受信任、无 OS 隔离的执行模式，默认每次构建重跑？** 推荐是，但诊断必须明确 unconfined。强隔离为独立能力，要求它而平台不可用时失败，不偷偷降级；在具备完整 hermetic 输入边界前，不启用跨 invocation 脚本结果复用。可先只开放 root，依赖授权协议同时设计，依赖执行后续单独验收。

以上任一选择不等于本轮授权实现。若要求首片即强隔离，交付范围应收缩为选定平台 backend，不能承诺 Linux/macOS/Windows 同时完成。

## 官方机制对照与取舍

Cargo 把 build script 编译成 host 程序、在目标包编译前运行，允许向 OUT_DIR 写产物并给编译配置；rerun 依赖由脚本报告，缺省覆盖包文件。target 信息必须区别于脚本自身 cfg。我们借用独立阶段和 host/target 分离，不复制它的自动脚本发现、stdout 命令通道及默认执行方式。[Cargo build scripts](https://doc.rust-lang.org/cargo/reference/build-scripts.html)

Go generate 是作者显式执行的步骤，不属于 go build，也不做依赖分析。仅包装 generate 可以满足手动生成，不能提供构建节点/可复用结果/授权边界。[Go 官方 generate 说明](https://go.dev/blog/generate)

Go build 有自己的文件选择与 cache；官方帮助也说明缓存不检测某些 C 库变化。因此不能以 Go build 成功或已有 GOCACHE 为脚本输入完备性的证据。[Go build/cache 文档](https://pkg.go.dev/cmd/go)

cgo 的编译器、pkg-config 和 linker 行为涉及宿主工具及目标 ABI；交叉构建需目标 C compiler。cgo flag 过滤也不隔离 Go 可执行程序。首片生成代码不等于完整 cgo 构建支持。[cgo 官方文档](https://pkg.go.dev/cmd/cgo)

Cargo resolver v2 区分 build dependency 与正常依赖的 feature 合并域；这是对本提案 host-build/target 区分的参考，不能直接规定另一个任务的 resolver 语义。[Cargo resolver](https://doc.rust-lang.org/cargo/reference/resolver.html#feature-resolver-version-2)

| 方案 | 收益 | 成本/边界 | 建议 |
| --- | --- | --- | --- |
| 手动 Go generate | 最小新增代码、作者生成后提交 | 无集成顺序或 cache；客户不能自动补产物 | 保留为零 runner 替代方案 |
| 受信任 host 可执行脚本 | 任意纯代码生成，跨平台 Go 工具链可用 | 任意 OS 副作用；声明不约束系统调用 | MVP，明确授权、默认重跑 |
| 各 OS 强隔离 backend | 限定读写/network/子进程，具备可靠 cache 前提 | Linux namespace、macOS/Windows 各自工程；工具/sysroot/时钟还需控制 | 分期、fail closed |
| 自有解释器/Wasm build API | 更容易控制宿主能力 | Go 标准库/exec 兼容性与编译链显著受限 | 非本次 MVP |

## 源级规则

### 候选声明与协议（不是已存在的 manifest）

以下 JSON 是**规范化 build node 输入的示意**，不规定项目配置采用 JSON、TOML 或字段名。resolver 负责把未来 manifest 映射到它。一次节点只面向一个 package、一个 target unit、一个最终 feature 集合；v1 不跨 bin/lib 合并执行。

```json
{
  "entry": "tools/build.hgo",
  "inputs": [{"file": "schema/user.json"}, {"directory": "templates"}],
  "env": [{"name": "SCHEMA_MODE", "required": false}],
  "outputs": [
    {"path": "model/schema.hgo", "kind": "hgo", "package": "model"},
    {"path": "model/schema.json", "kind": "resource", "package": "model"}
  ],
  "cfgSchema": {"schema_backend": ["json", "compact"]},
  "timeoutSeconds": 60
}
```

约束：静态 exact output 清单，无 glob 输出；输入目录递归，按排序后的相对路径/类型/内容快照，包含添加/删除。文件权限中影响行为的 executable 位入指纹；拒绝 special file、symlink、hardlink（nlink>1）、绝对路径、`..`、平台设备路径和大小写归一化碰撞。必要时外部文件由用户显式绑定为只读逻辑输入，不隐式读取 `../../`。不存在的必需输入报错；可选文件的 absent 状态也入指纹。大小/文件数/深度受版本化上限约束。

env 只传声明项和 runner 生成的固定上下文；unset 与空串不同。保留 GOTOOLCHAIN/GOFLAGS/GOWORK/GOOS/GOARCH/CGO/PATH 等由 runner 管理的名字，不准同名覆盖；工具将来需解析为显式 tool input，不能以继承 PATH 绕过。默认不给 token、HOME、代理、云凭据；私有 HOME/TMPDIR 只是减少误用，不是阻止同 UID 读取真正 HOME。日志不打印环境值，敏感 env 可禁 cache，不能把低熵秘密的 hash 当脱敏保证。

runner 写独立 request.json，包含协议版本、package/source identity、host triple、target triple/CPU 参数、unit ID、profile、sorted resolved features、cfg schema、逻辑输入/输出映射；仅参数传入文件路径。不依赖 stdout 解释指令。脚本写 result.json：版本、实际输出列表及 hash、typed cfg、结构化 warning。runner 独立重算 hash；拒绝重复/未知 JSON 字段、重复路径、未声明输出、缺失输出、绝对路径、超限 result/log。保留全部诊断到限额，控制字符转义；普通 stdout 只是日志。

生成 `.hgo` 先进入已有前端、生成 `.go`，然后与手写 Go 及资源组成 fresh generated-module view。生成 `.go` 不反过来冒充 `.hgo` 同一实现；遇源文件/多个生成节点冲突直接拒绝，不覆盖。资源仅进入声明包目录，`//go:embed` 按 Go 原规则校验；不支持任意安装目录。未知 import 只能使用已解析的正常 target 依赖，不能触发新依赖下载。

build script 本体是独立 host 工具包，不能被业务目录扫描为普通源文件。MVP 首先支持显式 `.hgo` 路径，经现有前端降低后编译 host；可研究显式原生 `.go` entry 作为 bootstrap escape hatch，但不是隐式解析 `.gox`。目录/package 隔离适配尚待实现，本 fixture 用手写 Go 只验证机制。

### 授权与可撤销信任

计划时先列出脚本（不编译、不下载新工具、不执行 init），授权检查在 host compile **及每次 cache 查找前**。root/local path dependency/git/module 三类来源都不天然可信。依赖脚本无需执行便能解析的元数据只可当声明，不可当授权。

授权 subject = canonical source identity（module+version+sum、git URL+精确 commit+内容摘要、本地路径身份+内容摘要）、entry、脚本执行闭包摘要、Go/compiler/frontend identity、能力集合、策略版本。闭包含 entry、host dependencies、代码生成工具、会被其 init 运行的全部 Go 源、build 声明与嵌入脚本数据；不得只 hash 顶层 entry。复用依赖源码替换或编译器版本变化须重新比较完整 subject。授权不得藏在依赖 manifest 或依赖 cache；用户本地 trust store/CI 外部策略管理，repo 提交只能建议所需权限。CI 不交互，缺授权返回列出精确 subject 的错误，不默认 yes。

建议区分一次授权与持久授权；持久记录精确 subject，不支持“所有第三方脚本永远可信”快捷默认。撤销更新本地 policy epoch；所有新执行和 cache 使用必须读取当前 epoch，旧授权立即不可再用；发布产物和绑定下游 view 前重新检查 epoch/subject，in-flight 撤销阻止新提交。已运行进程请求取消但不承诺 unconfined 恶意子进程一定可终止；下游最终 build cache 同样先通过其依赖 provenance 的信任门，不能用旧二进制绕过撤销。删除缓存不回滚已执行副作用，hash pin 不证明代码善意。

root 输入/代码持续编辑可能频繁触发授权；先保守采用精确闭包，未来如需要 workspace session 授权须另确认，不能借“开发便利”默认授予依赖执行权。

### 构建图接口：给并行构建任务的提案

```text
resolve source + locked target AND host-build closures + features + target units
  -> validate capabilities / detect host bootstrap cycles
  -> materialize input snapshots and hash full execution closure
  -> trust gates over those exact snapshots (separate host feature domain)
  -> transpile + compile script for HOST (cached host artifact if authorized)
  -> run script for TARGET unit in private stage
  -> validate result + seal output/provenance
  -> assemble generated-module view
  -> target selection/typecheck/lower + refs/enum/use metadata
  -> Go target compile/link/test
```

这些是逻辑节点，不要求 resolver 提前执行代码。script host 闭包必须在 resolve 结束时已知，静态解析不执行 helper/init。授权摘要与随后编译/执行必须指向同一快照；不得授权 live path 的旧 hash 后重新读取已变化的源树。unconfined 同 UID 对快照的恶意篡改仍属下面列明的无隔离限制。最小接口契约：

| 输入/输出 | 归属及不变量 |
| --- | --- |
| `ResolvedPackageID` / `SourceDigest` / lock fingerprint | resolver 提供，不接受 script 改写；覆盖 Go module/git/local |
| `BuildContext` | target triple、CPU/CGO、profile/race/tags、unit(kind/name/import path)、最终 features；不可从宿主 env 临时猜 |
| `HostBuildClosure` | 独立 host context/feature 集；无目标产物依赖，无需要脚本执行才能解析的依赖 |
| `ScriptPlan` | 静态 inputs/env/outputs/cfg schema 和协议版本；授权先于执行 |
| `BuildArtifact` | 内容 digest、输出映射、typed cfg、source map provenance、trust subject/policy、cache eligibility |
| 下游 key | 引入 artifact 内容和配置 digest，再调用现有 package 指纹/metadata 流程；不能仅复用路径 |

0 或 1 lib + 多 bin/examples 不改变脚本属于 package 的事实，但第一版保守为每 unit/config 独立执行，generated output 仅在该 view 中可见。只有完整 node key 相同的请求可 single-flight；不可按 package 名缓存。将来证明 package-wide invariant 后再合并多个 unit。普通 Go 依赖没有 script entry 就不扫描其 `go:generate`；存在脚本而未授权且目标依赖其产物，则失败，不静默忽略以产出错误 binary。用户选择独立已生成 dist source mode 后，可在不执行脚本的图中构建。

feature 在 script 之前最终解析；输出 cfg 仅限声明包、声明 unit、cfg schema 内值，不能设置 `feature`/target_os/target_arch、影响 resolver、修改依赖 feature 或绕过 target 可用性。若当前前端没有 package-local cfg 通道，拒绝该输出 `B-CFG-UNSUPPORTED`，不能翻译成全 module `-tags` 造成泄漏。公共 ABI 改变必须进入该包 fingerprint 和下游 metadata 失效判断。

### host、target、bootstrap 与脚本依赖

Go 编译脚本时 GOOS/GOARCH=HOST，CGO_ENABLED=0（MVP），目标信息通过 request 传递。host 编译工具链由外部已安装的 Go/hgo 提供；不允许 build.gox 负责构建自身所需的 hgo 或 Go。脚本依赖是 `host-build` 边，锁定来源/版本，不能从普通 target features 自动并入；首片可限标准库，开放纯 host helper 时需额外解析闭包、信任摘要和 cycle 验收。任何 helper 自带 build script 或工具安装步骤，首片拒绝，而非递归授权后自动运行。

脚本 import 自己生成的包、A 的脚本依赖 B 而 B 的生成依赖 A、host 工具依赖其 target 产物，都在执行前输出闭环路径。正常 target package 使用自己脚本生成的源码是合法下游边。未知动态 import 不能重新进入 resolver。

现在 hgo 目录 cross-target 被明确拒绝；MVP 接受的 target 与现有前端能力一致，不能脚本跑完才发现 cross-target unsupported。纯 Go fixture 生成 Linux ARM64 ELF 仅证明 host→target 机制可行。今后解锁交叉前端时必须让类型尺寸、文件选择、metadata fingerprints、CPU 参数、cgo sysroot 全部匹配 target。交叉编译不执行 target binary；测试需额外 emulator/远端执行授权，MVP 不提供。

### rerun、缓存与正确性边界

分三层，不混同 Go 自身 cache：

1. **host binary cache**：脚本源码/host dependencies/锁文件/编译器及 frontend digest/host ABI/编译 options/协议版本。target 若不影响 host 编译无需加入，但 request 必须独立；授权检查仍必需。
2. **script result key（将来的可复用候选）**：上述 binary digest + package/source/lock identity + 输入内容树及 absent 标记 + env set/unset/value digest + target/CPU/CGO/sysroot + unit + features + cfg schema + profile/race/tags + output declaration + tool identities + executor/policy/protocol version。结构化、排序、length-delimited 编码，不字符串拼接；敏感原值不上日志。producer 内容 hash 与 validator 重算 output digest 分离。
3. **target artifact cache**：BuildArtifact 内容/config/provenance + 正常 package 和依赖指纹。信任/策略变化先使授权门失效，不能只给 key 加一字段而容许老结果绕过。

**MVP trusted-unconfined 默认每次 invocation 重跑 script**；同次 invocation 的相同已快照节点可 single-flight。声明 inputs 是正确性契约，不是检测器：脚本读遗漏文件、时钟、随机数、网络，runner 无法从声明发现。显式 inputs 也不使 unconfined 自动变 hermetic。因此不给这个模式跨 invocation result cache；仍可缓存编译好的 host 程序和基于实际产物内容的下游 Go 编译。未知系统库探测属 volatile，即使采用隔离也每次重探或要求固定 sysroot 与工具。

强隔离将来提供 cache，必须同时约束读写/网络/子进程、工具链/动态库输入及时间/随机等非文件来源；仅禁网络或只读挂载不构成 deterministic 保证。明确 opt-in 的确定性契约仍不抵抗恶意 nondeterministic 脚本；cache 日志必须标示保证级别。动态 rerun 列表首片不接受，避免先用旧列表命中而永远看不到新增读取；后续两阶段 discovered-input 协议另 RFC。

### 路径、失败、发布、clean

stage 每次新建，位于自有 cache 根且与最终对象同文件系统；输出路径相对 stage，脚本不能通过协议要求写 source/cache sibling。声明规则校验只是防 runner 误写，**unconfined 程序仍能直接写外部路径**。

输出校验拒绝 symlink/hardlink/special file、漏报/多报、大小写冲突及超限；用受目录约束的 handle 操作和稳定快照，不能仅 `EvalSymlinks` 后再按原字符串 open（TOCTOU）。Go `os.Root` 可帮助 runner 自身抵抗目录逃逸，不限制 child 的 `os.Open`。[os.Root 文档](https://pkg.go.dev/os#Root)

成功退出 → 确认受管理子进程全部停止 → 验证并拷贝到 runner 私有 seal 目录 → 计算清单 → 原子发布到不存在的不可变内容目录；相同 key 并行使用 lock/single-flight，发布输家验证赢家 digest，不能覆盖旧目录。必要文件与父目录 fsync 区分“读者原子可见”和“掉电持久性”；fixture 仅验证同文件系统 rename 的可见性机制，没验证断电。Windows 不能假设替换已有非空目录成功，因此不做 in-place replace。target view 单独绑定完整 commit，失败时不回退旧 key 冒充成功。

超时/非零退出/非法结果均失败、不给新 key 成功标记、不发布半成品；旧完成对象保留但不得用于当前失败构建。取消/超时涵盖 build、run、validation 各阶段，log/磁盘有上限。Linux process group、Windows Job Object 等后续 backend 需完整验收；`exec.CommandContext` 缺省 kill 仅直接进程，不是进程树回收。[os/exec 文档](https://pkg.go.dev/os/exec#CommandContext)

在 unconfined 下，恶意 daemon 可脱离进程组、持有写 fd、攻击同 UID cache，无法承诺杀净或输出不可篡改；private stage 和 seal 只保障合作脚本及正常故障。强对抗保证需独立安全主体/OS 容器等边界，不能因 Go path API 或 chmod 称安全。资源约束也分合作 deadline 与 OS CPU/memory/quota，后者尚未实现。

clean 只处理有 owner/version/lease 标记的本项目 cache 对象，且 marker 不是抗恶意伪造边界；不得由 script 提供任意删除路径。不跟随 symlink，拒绝根目录/源树/Go module cache/GO cache/用户输出目录，跳过 active lease，使用受根约束的删除操作；外部 dist/binary 仅显式指定自有输出且另确认清理策略。失败 stage 删除也遵此范围。事务不能撤销脚本已经写到 stage 外的内容。

### 网络与本机库探测

`--offline` 的拟议含义：resolver/Go 包管理不下载依赖、工具链或 VCS。**不是 OS 网络隔离**。若脚本要执行，诊断 `B-UNCONFINED` 明示“offline applies to dependency fetching; script retains OS network access”。拟议 `--deny-script-network` 只有已验证隔离 backend 可满足，否则 `B-NET-ISOLATION-UNAVAILABLE` 拒绝；不提示用户把 offline 当替代。名称只是 UX 候选，尚未增加 CLI。

系统库探测可在后续显式 tools/sysroot 能力下执行固定 host pkg-config，参数与环境显式，查询 target sysroot；禁止跑 target probe executable。探测不到时报告 expected target ABI、查找范围、工具 identity；不能偷装 brew/apt 或拉源码。其 stdout 不拼接进 shell/GOFLAGS。结果应是 typed native artifact/link metadata，限定到原生编译 adapter 并做 flag allowlist；首片不支持，报 `B-NATIVE-UNSUPPORTED`。

## 转译示例

### BG-01：root schema 生成（白）与未授权 root（黑）

候选输入：root entry `tools/build.hgo`，读 `schema/user.json`（内容 `{"name":"User"}`），输出 `model/schema.hgo`；不是已可运行的 hgo build API。生成器算法是读取/解析 schema 后输出下面完整 hgo 文件：

```hgo
package model
pub func schemaVersion() int { return 1 }
```

经前端的预期 Go 文件形状（包路径 `example.org/app/model`，名称映射仍遵守既有前端）：

```go
package model
func SchemaVersion() int { return 1 }
```

白：本地授权当前 root subject，接受；顺序 resolve→trust→host build→run→validate→assemble→lower→target compile，诊断 `B-RUN root model`；同配置重跑可复用 host binary，但 script 默认重跑。生成代码与业务同包可供 hgo/Go 调用；不会写入源树。fixture 的 schema 生成是原生 Go 的 `const Schema`，**不声称上述 hgo 端到端已实现**。

黑：仅 clone 项目、manifest 声明 entry，未本地授权；resolve→trust 拒绝 `B-TRUST-ROOT <subject>`，不得编译/运行脚本，stdin 不交互确认后自动 yes。该授权门尚属方案。

## 拒绝与边界：黑白成对验收表

下表所有 `B-*` 为候选诊断，未实现。**P**=方案的未来端到端验收，**M**=本轮手写 Go/OS 机制，**K**=简单 cache/path 模型；M/K 不证明 runner 已实现。表中“接受”都以依赖解析完成、精确授权、平台能力满足为前提。序列 T=trust，H=host compile，R=run，V=validate/seal，C=target compile；plan 检查在 T 前，不执行任何脚本。

| ID / 输入 | 白：接受/授权/警告、顺序与诊断 | 黑：拒绝/警告、顺序与诊断 | 证据 |
| --- | --- | --- | --- |
| BG-02 依赖 `dep@sumA` 的 script | 本地授权完整 dep host 闭包与 unconfined 能力；T→H→R→V→C；`B-TRUST-MATCH` | 未许可：T 拒绝 `B-TRUST-DEPENDENCY`，绝不先执行再提示；root 的许可不能覆盖 dep | P |
| BG-03 已缓存 dep，撤销或更新 | 不变 subject 且有效授权：先 T 再查 host cache，R→V→C | revoke、sumA→sumB、helper/init 改变而 entry 不变：在 T 拒绝 `B-TRUST-REVOKED/CHANGED`；host/result/final cache 都不能绕过 | P；fixture 不伪装有 trust store |
| BG-04 schema 显式或漏列 | `schema/user.json` 列入 inputs，内容变→新快照→T/H→R→V→C；`B-INPUT-CHANGED` | 漏列但脚本读 source：unconfined 可能成功且无法检测，发模式级 `B-NONHERMETIC`、禁 result reuse；严格只读快照未来应 OS 拒读并失败，不能声称今天能报漏读路径 | M schema + K omitted-key |
| BG-05 env 与目录输入 | MODE unset/empty 区分；templates add/delete/content 变触发新指纹；T/H→R→V→C；`B-INPUT-CHANGED` | 必需 env 缺失：plan 拒绝 `B-ENV-MISSING`；声明 GOFLAGS/PATH：plan `B-ENV-RESERVED`；漏列 env 不传，程序另读宿主资源仍是 unconfined 风险 | K inventory/env；P env gate |
| BG-06 symlink/越界输入输出 | 相对普通文件/目录、无链接，snapshot→T/H→R→V→C | `../secret`、绝对路径、`templates/x` symlink→外部、output hardlink：plan/V `B-PATH-ESCAPE/LINK`；竞态需 root handles；直接 OS 越界写 unconfined 可成功，不能假报已阻止 | M outside-write，K symlink；TOCTOU P |
| BG-07 host vs target | 当前 host/host：H host→R→V→C；未来 linux/amd64 host→linux/arm64 target 通过 request 选代码 | 当前 hgo 跨 target：plan `B-TARGET-UNSUPPORTED`，不先 R；用 runtime.GOARCH 当 target 是脚本逻辑 bug，unconfined 无法自动判断；执行 target probe `B-TARGET-EXEC-UNSUPPORTED`（adapter） | M host/request + ARM64 ELF；hgo cross P |
| BG-08 多 target/feature 并行 | lib/cli/example、feature a/b 不同 key/stage/view；各自 T/H/R/V/C，相同完整节点 single-flight | 同 package 名共享 OUT_DIR/复用 feature a 产物给 b：计划/内部断言 `B-NODE-CONTEXT-MISMATCH`；不能跨 unit 注入 cfg | K key partitions；真实并行/locks P |
| BG-09 timeout/非零/原子输出 | 完整 exit0 输出集，经 V 发布全新 immutable 目录，C 只见整套；`B-PUBLISH` | 先写 partial 后超时、exit1、退出0但缺输出：R/V `B-TIMEOUT/EXIT/OUTPUT-MISSING`，不 C、不发布新对象，不以 old key 兜底；恶意 daemon 不在 unconfined 保证内 | M partial/rename/CommandContext descendant；跨 OS P |
| BG-10 clean | 只删除已确认自有且无 active lease 的 stage/cache，`B-CLEAN-OWNED`，保留用户文件 | clean path=`../`/源码/Go module cache，或 symlink 指向用户文件：先 validation `B-CLEAN-NOT-OWNED`，不跟随链接；脚本私自删用户文件无 OS 隔离时无法阻止 | M Python owned-tree sentinel；Go clean/lease P |
| BG-11 offline/network | offline 且纯生成器、依赖齐备：T/H/R/V/C；明确 `B-UNCONFINED`；用户要求 deny-network 且 backend 经验证才能接受 | `GOPROXY=off` 下脚本 HTTP 仍可能成功，不宣称禁网；要求 deny-network 但 backend 不可用：plan `B-NET-ISOLATION-UNAVAILABLE`，不运行/不降级 | M loopback HTTP、namespace capability |
| BG-12 output/flag 注入 | 仅 exact 声明输出和已支持的 typed cfg 值：R→V→C；stdout `hello` 只是日志 | result 加 `goFlags:["-toolexec=evil"]`、`ldflags`、未知字段：V `B-RESULT-FIELD`，不 C；额外 `.go` 或覆盖源码 `B-OUTPUT-UNDECLARED/COLLISION`；生成 Go 的 go:generate 也不自动运行 | M explicit generate；protocol gate P |
| BG-13 bootstrap/导入自己输出 | 标准库 host script 生成 model，target 导入 model：H→R→V→C；`B-PLAN-ACYCLIC` | script import model（自己输出），或 A build→B generated→A：plan `B-BOOTSTRAP-CYCLE`，列闭环路径，无 H/R；脚本下载 compiler `B-TOOL-UNDECLARED` 仅可信 adapter 能报，直接网络只能靠 OS backend 阻止 | P |
| BG-14 cfg/feature 作用域 | 声明 `schema_backend=json`，在实现 package-local cfg 后只作用该 unit/pkg，R→V→C；目前不支持则拒绝 | 输出 `feature=ssl`、target_arch=arm64、要求影响依赖：V `B-CFG-RESERVED/SCOPE`，不重新 resolve；绝不能全 module tags 注入 | P |
| BG-15 系统库/交叉探测 | 后续固定 host tool+target sysroot+显式权限，T/H→probe→typed native metadata→C；无 native adapter 的首片拒绝 | 误用 host `/usr/lib` 代表 target 库，或 `apt install`/target executable：plan/adapter `B-NATIVE-UNSUPPORTED/TARGET-EXEC-UNSUPPORTED`；unconfined 任意 exec 仍无法由声明拦住 | P；未运行 pkg-config/第三方安装 |

### 不支持项与可执行代码入口补充

首片不支持动态新增依赖、依赖安装 hook、递归 build script、动态 outputs、用户任意编译/link flag、自动包管理、隐式网络下载、自动运行 go:generate、target 可执行探针、跨 package cfg 注入、remote result cache、watch daemon、跨机器结果复用、强恶意进程隔离保证。

生成 `.go` 可含 `import "C"`、`//go:linkname`、cgo directives 等，结果 JSON 校验不等于源代码安全审计。MVP 的脚本 host 编译禁 cgo，target 仍遵守工程原有 cgo/unsafe 策略；若用户要求“完全不执行任何不受信编译工具”，必须对整个后端构建工具链单独设隔离，不能只隔离 script。script 输出代码本来就获得被编译进程序的权限，这必须作为授权能力说明。

## 双向互操作

hgo→Go：生成纯 `.go` 可作为声明包的 native input，生成 `.hgo` 用现有 lowering 与元数据路径，包依赖只能来自提前 resolve 的 target 闭包。不能把普通 `.go` 当作额外权限声明恢复 `.hgo` 语义。

Go→hgo：发布完整生成 dist（含 Go 及所需资源/声明/metadata），Go 消费者直接 build，不运行本 RFC 脚本。原始 `.hgo`+未生成 schema 的源码分发不能宣称普通 Go 可用；Go 不认识未来 manifest 的脚本规则。[包加载 RFC](../0001-package-loading.md) 的同实现来源选择继续适用。

## 验收与未定项

复现命令（仅自编代码、stdlib，无第三方安装）：

```sh
python3 tests/research/build_gox/run.py
python3 scripts/check_design.py --docs-only
```

fixture 每次使用 TemporaryDirectory 并销毁，单独 GOCACHE/GOPATH；首次编译 Go 标准库较慢。可用 `BUILD_GOX_RESEARCH_GO` 指定同版本工具链。它是 Linux/POSIX 机制实验，不是跨 OS runner，也不是生产安全测试套件。版本必须 go1.27.1，生成 ARM64 ELF 不运行。机器不能使用 namespace 的结果作为 capability observation 记录，不升级权限绕过。

本机结果：

- host 为 Linux/amd64，request target 为 Linux/arm64；原生 Go 交叉产物 ELF machine=183，未执行 target binary。
- schema 生成 Go 经 Go 编译成功；普通 `go build` 不运行生成指令，显式 `go generate` 才执行。
- 自编程序能写 cwd/output 外的临时 marker，并在 GOPROXY/GOSUMDB off 下访问自建 loopback HTTP；直接进程被取消后，child 仍能响应 release 并写 marker。
- partial 输出 timeout 不影响旧 committed 目录；fresh 完整目录可 rename 到同文件系统新 key。目录/缓存模型检查通过，不代表产品实现。
- `unshare -Urn true` 和 bubblewrap 的 user+network namespace 探针都因 uid_map 只读失败（exit 1）。本云环境这两种配置不可用，**没有获得强隔离/禁网的成功证据**；未提权重试。这不证明所有 Linux backend 均不可用。

完整结果见 [results.json](../../../tests/research/build_gox/results.json)，范围说明见 [fixture README](../../../tests/research/build_gox/README.md)。独立对抗评审与修正见 [review](build-gox-review.md)。最终实现验收还需：完整 BG-01–15、CLI noninteractive trust/revoke、snapshot/TOCTOU、sandbox backend 逃逸负例、跨 OS 子树回收、并行同 key 多进程、crash recovery、缓存污染/撤销、generated metadata ABI 变更以及 feature resolver 合同测试。本轮未运行这些未存在的产品能力。

### 工程成本与分期

- 阶段 0（本轮）：本 RFC + 可复现机制证据，无生产风险变化。
- 阶段 1：root-only 授权与纯 host 生成；需新增图节点、host 编译、typed protocol、fresh stage/seal、diagnostics、生命周期与 source-map 集成。主要收益是 schema/资源生成不再靠用户手工排序；主要代价是每次脚本重跑和信任操作。预算估计 1–2 人周属于未验证规划，待构建图接口稳定后重估。
- 阶段 2：依赖授权、纯 host helpers、typed cfg、可靠并行/cache provenance；约再 1–2 人周的规划占位，撤销和 ABI metadata 比协议解析更容易漏测。
- 阶段 3：OS 隔离和 hermetic result cache、native probes/cgo adapter；每个平台单独评估，无法据当前两项 Linux namespace probe 给出可信工期。无 backend 时只允许明确 unconfined 或拒绝，不能将静态声明包装成安全卖点。

为协调任务预留的未定接口：最终 manifest 形状/名称、resolver host feature domain、artifact publication owner、package-local cfg、locked native工具表示。它们不能由本 RFC 擅自冻结。推荐先确认三项产品选择，再与构建图实现对齐这些接口、分片实现。

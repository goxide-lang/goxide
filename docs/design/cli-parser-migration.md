# CLI 解析层与命令树迁移提案

状态：用户已批准 Cobra 迁移及下述语法合同，现已从 vendor 交付
`d51117d36fa2d1ccfcf322b81195b0d753cbb8de` 实施。用户明确撤销旧版对
`-go/-offline` 等 Go 单短横长选项的兼容。这是 POSIX 风格短选项加 GNU 风格长选项
的项目约定，不宣称 `--long` 是 POSIX 强制要求。历史只读审计基线为
`69c1ab0d6769facd266886deec90337fe4b52498`，第2节与第8节历史探针仅描述该基线。
实现和本地验证证据见[交付记录](../reviews/cobra-cli-20261004.md)。

## 1. 决策

**采用 Cobra 管命令树/help/completion，独立 typed request 管业务输入；未来自有
clap 只更换解析适配器。** 先抽离边界、冻结业务行为与明确变更清单，再引入 Cobra；不要先把所有
`flag` 调用机械替换成 `pflag`。标准库 flag 足以保留现状，Cobra 的收益是维护统一
命令树和帮助，而非更快编译或更安全授权。不在本任务开发 clap。

| 方案 | 本项目成本与收益 | 判断 |
| --- | --- | --- |
| 继续 flag | 无新依赖、现行为最容易保持；已有三组 FlagSet，继续手写 scripts 分派、帮助层级与补全 | 可以作为抽离服务时的过渡；长期 UI 工作会重复造轮子 |
| Cobra + pflag | 子命令与帮助能力适合已有 scripts 多动作；需要处理旧单短横长选项、参数顺序、错误输出及授权入口；增加依赖 | 已批准，需词法校验与解析契约测试 |
| 现在等/实现 clap | 把 CLI 改进绑定到尚未就绪库；当前没有被授权的库实现范围 | 不推荐，保持可替换接口即可 |

标准库 flag 支持独立 FlagSet，单/双短横等价；遇首个位置参数或 `--` 停止。
见 [Go flag 官方文档](https://pkg.go.dev/flag#hdr-Command_line_flag_syntax)。
pflag 的短选项及交错参数规则不同，不能仅换 import；其
[官方说明](https://github.com/spf13/pflag#command-line-flag-syntax)和
[SetInterspersed API](https://pkg.go.dev/github.com/spf13/pflag#FlagSet.SetInterspersed)
是语法设计依据；其默认宽容解析不是本项目的最终合同。Cobra 的 local/persistent flag 分层见
[官方指南](https://cobra.dev/docs/how-to-guides/working-with-flags/)。

本次固定 Cobra v1.10.2、pflag v1.0.9 与间接依赖 mousetrap v1.1.0；不引入 Viper 或 cobra-cli 生成器。
[Cobra 官方 go.mod](https://github.com/spf13/cobra/blob/main/go.mod)并非零依赖，
实施采用固定 release 和 go.sum，不把在线 main 分支当版本锁。

## 2. 已核实的现状

源码入口：[main.go](../../cmd/hgo/main.go)、
[import_go.go](../../cmd/hgo/import_go.go)、[scripts.go](../../cmd/hgo/scripts.go)、
[project.go](../../cmd/hgo/project.go)、[directory.go](../../cmd/hgo/directory.go)。

- `main.go:24–29` 把任意返回错误打印到 stderr 并退出 1，包含 ErrHelp、参数错误、
  编译错误、子进程退出错误。没有稳定的 usage=2 分类，也未透传用户程序退出码。
- `run:31–68` 手写七个顶层入口：transpile/expand/run/build/test/scripts/import-go。
  没有 root help、completion 或命令别名；`help`、`--help` 被当作未知根命令。
- 五个编译命令共用一个 FlagSet，`ContinueOnError` 输出指向 stderr；要求恰好一个
  INPUT，flags 在其前。`test` 要目录，`expand` 只支持单文件。
- 单文件、Go module 目录、goxide.toml project 是输入判别分支，不是三组 CLI 子命令。
  `project.Detect` 决定模式，不能因为 parser 更换就改成搜索父目录或默认当前目录。
- project 从 goxide.toml/goxide.lock 读取目标、特性和依赖。`--lib` 与 `--bin/--example`
  冲突；一个 bin 时可自动选中；多个 bin 必须显式选；无 bin 有 lib 时可默认 lib，
  但 lib 不能 run。manifest/lock 是服务的配置权威，没有独立 manifest/lock 命令。
- `test` 的目录/project 路径已自动启用 race；当前没有 `hgo test --race` 或测试参数
  透传。`run` 的 Go argv 也没有程序参数尾部，不能声称现有 `--` 可透传。
- scripts 只有 `--action`，默认为 plan，六个合法值见下面命令树。plan 输出 JSON；
  它不运行 build script，但会取得项目/策略锁、解析依赖、准备计划，不能承诺整个过程
  零文件系统/网络副作用。不得从 help/completion 自动调用完整 plan。
- import-go 要求 `--plan` 与 `--out NEWDIR` 恰一，以及 `--lib PATH` 与一或多个
  `--bin NAME=PATH` 恰一。plan 输出候选 TOML；out 使用新目录 staging/build/rename。
  69c1ab0 明确拒绝已有 vendor/manifest/锁；vendor 任务合流后需重新确认，不固化这项
  拒绝为未来契约，也不在 parser 层复写其迁移逻辑。
- README/设计文档以中文为主，实际 FlagSet usage/错误文本主要英文。不存在当前已实现
  的中文 help 或 locale 系统。

### flags 分类（“共同出现”不等于“全局”）

| 类别 | 现有选项/默认值 | 迁移归属 |
| --- | --- | --- |
| 真正全局 UI | 当前没有；拟新增 root/各级 `-h/--help` | Cobra UI；不要自动加入 verbose/config/yes 等未实现能力 |
| 常见执行上下文 | `--go=go`；`--offline=false` | 仍按叶命令注册，复用注册函数；不因提升为 persistent flag 而扩大作用域 |
| project 配置 | `--features=""`、`--no-default-features=false`、`--locked=false`、`--target-dir=""` | 编译命令及 scripts；目标目录保持 flag > HGO_TARGET_DIR > target，路径语义不变 |
| project 目标 | `--bin=""`、`--example=""`、`--lib=false` | 五个编译命令；由现有目标服务验证支持组合 |
| 输出/生成 | `-o=""`、`--source-map=""`、`--expanded=""` | 编译叶命令；map/expanded 仅单文件，-o 按模式保留意义 |
| 构建相关 | `--tags=""`、`--runtime=""` | tags 仅目录；runtime 实际用于单文件 run/build；不要从其它模式删去历史可解析选项 |
| scripts | `--action=plan`、`--subject=""` | action 兼容入口；subject 只给 allow/revoke；clean/trust 的服务语义不变 |
| import-go | `--plan=false`、`--out=""`、`--lib=""`、可重复 `--bin`，另有 go/offline | 完全独立选项定义；此处 lib 是 string、bin 是累积列表，不得与 build 共用变量 |

五个编译入口历史上接受部分无效于当前 mode 的 flag。例如实测单文件 transpile 的
`--go unused` 不检查该工具；run/test 的 -o 也不是统一输出控制。首轮保留合法语法
下的业务行为（不保留已撤销的单dash长名），并在
帮助中说明作用域，不能把原 no-op 悄悄改为实际副作用；后续严格化另行公布。
features/tags 目前是 string，重复出现最后值胜出，之后按逗号/空格拆分。
import-go bin 是逐次原样追加，不按逗号拆分。不能一律换成 StringSlice。

## 3. 建议命令树

下面是现已实施的 UI。
除 UI 外，每个操作都对应现有服务；保留顶层常用命令，不增加多余 `project build` 层。

```text
hgo
├── build      [flags] INPUT
├── run        [flags] INPUT [-- PROGRAM_ARGS...]
├── test       [flags] DIRECTORY
├── transpile  [flags] INPUT
├── expand     [flags] FILE.hgo
├── import-go  (--plan | --out NEWDIR) (--lib PATH | --bin NAME=PATH...) ROOT
├── scripts
│   ├── plan           [project-flags] PROJECT
│   ├── trust-project  [flags] PROJECT
│   ├── allow          --subject SHA256 [project-flags] PROJECT
│   ├── revoke         --subject SHA256 [flags] PROJECT
│   ├── forget-project [flags] PROJECT
│   └── clean          [flags] PROJECT
├── help       [COMMAND ...]              # 新 UI
└── completion bash|zsh|fish|powershell    # 新 UI，只输出脚本
```

保留 `hgo scripts [flags] PROJECT` 和 `--action ACTION` 的旧拼法，仍默认 plan。
不要把 trust-project 简化成含义更宽的 trust/approve-all，不新增 add/update/publish、
manifest edit、lock update、target list、全局 clean 或 vendor 子命令。
import-go 的迁移策略由 vendor 任务持有；本轮不拆成 import/plan/apply 新语法。

别名只做旧拼法到同一个 typed request 的映射，不添加 b/r/t 等新缩写。
特别测试名为 plan、allow、clean 的 PROJECT：旧 `hgo scripts plan` 是针对路径 plan
的默认 plan，不能因 Cobra 自动子命令发现改变含义。建议在进入 Cobra 前做明确的
legacy grammar 路由：显式旧 --action 或“只有一个项目位置参数”的旧调用保留；
新动作后必须显式提供 PROJECT（或请求该动作 help），不自动补 `.`。
歧义不能通过执行项目、联网探测或默认信任来解决。

## 4. 正式语法与输出契约（覆盖旧版兼容建议）

### argv：短选项、长选项、operand 和终止符

下面是已批准并实施的合同，**不是 69c1ab0 已有能力**。所有拒绝都先于服务调用，
exit 1、错误到 stderr；不得部分解析后先写文件/授权，再报告余项错误。

1. **长选项必须 `--name`**。带值支持 `--go PATH`、`--go=PATH`；布尔支持
   `--offline` 和 `--offline=false`，不吞掉其后的 `false` operand。保留已存在的
   `--o`（配 `-o`）和 import-go `--out`，不借本次另造输出语义。
2. **短选项只注册单个字符**，首期已有用途仅 `-h`（help）与编译命令 `-o`（需值）。
   不为让组合看起来丰富而新增 -a/-g/-f/-q/-v。需值短选项支持 `-o FILE` 和 `-oFILE`；
   余串全部是该选项的值，不再拆成 flags。`-o=FILE`、`-h=true` 首期拒绝，提示使用
   `-o FILE` 或 `--o=FILE`、`--help=true`，不假装这些额外语法是 POSIX 要求。
3. **短组合采用受限、可审计规则**：从左到右，每个前缀必须是已注册的无值短选项；
   如包含需值短选项，只能是最后一个被识别的选项，其后字符或下一 argv 元素是值。
   每个无值短选项在同一 token 中最多出现一次。当前 `-ho FILE`、`-hoFILE` 可以表示
   help 加 output，但只展示帮助，绝不写文件。`-hh`、`-aaa` 必须拒绝；即使未来注册
   a，也不能把 -aaa 静默解释为三个 -a。未知字符拒绝，不回退成完整长名或忽略。
   这是为防旧拼法误解释增加的项目限制，不声称 POSIX 禁止重复短选项。
4. **旧 Go 风格先识别并拒绝**：建立从历史 FlagSet 名称生成、可审查的集合（go、
   offline、features、source-map、expanded、runtime、tags、no-default-features、
   bin、example、lib、target-dir、locked、action、subject、plan、out、help）。
   在选项区域，`-name`/`-name=value` 命中该集合就报明确迁移错误，例如
   `-offline is not supported; use --offline`；不得先交给 pflag 拆短选项。
   这条优先于附着值：`-offline` 不能变成 `-o ffline`；确要该文件名请用
   `-o ffline`。不注册 Go FlagSet 作为逃生入口，不自动把错误拼法改成双短横。
5. **校验器必须识别 option arity**。`--go -offline` 的第二项是 go 的值，不是新
   选项；`-o -aaa` 的值也是普通文件名。不要对整条 argv 做正则拒绝。尚待值时单独
   的 `--` 视为缺值错误而非吞作值；需要字面 `--` 的值可写 `--o=--`。
6. **位置参数放在 options 后**。每个叶命令遇首个 operand 就停止选项解析，使用
   并测试 `SetInterspersed(false)`；除 run 明确的程序边界外，每个现有执行叶要求
   恰好一个 INPUT/PROJECT/ROOT。`build INPUT --offline` 是多余 operand 错误，
   不重新扫描成选项。子命令按树顺序出现，不支持任意父级/子级flags交错。
7. **`--` 终止 hgo options**：如 `transpile -- -source.hgo`，后者是文件名，不能被
   旧选项拦截；`--` 本身从解析结果移除，后续项目 operand 原样保留。
   `scripts -- plan` 明确指定名为 plan 的项目目录，不发现 plan 子命令。
8. **run 程序参数使用独立边界**：迁移目标支持 `run [flags] INPUT -- ARGS...`，
   input 后没有 `--` 时不接受额外参数。若 input 以短横开始，则
   `run [flags] -- -input.hgo -- ARGS...`：第一个 `--` 终止 hgo options，第二个
   在唯一 INPUT 后划分程序 argv。无尾部参数的原调用继续有效，空尾部也有效。
   边界之后的 -aaa、--offline、空字符串、再次出现的 -- 都逐项保留，绝不再解析为
   hgo options。使用 exec argv，不拼 shell；按已选 Go 工具/构建路径传递，不允许
   程序参数插入 Go 工具的 flags 区。test/build/transpile/import-go/scripts 不提供
   自由透传；test 的 Go flags 和测试二进制参数不是 run 的同一接口。
   Cobra 的 [ArgsLenAtDash](https://pkg.go.dev/github.com/spf13/cobra#Command.ArgsLenAtDash)
   仅作辅助：关闭 interspersed 后，INPUT 后的 -- 可能保留在剩余 argv，适配器必须
   明确划分 input 与 program args。ProgramArgs 放入 typed request，不能事后全局重解析。
9. **Cobra 前置词法门禁不是执行器**：只按对应 command schema 检查选项token、旧拼法、
   组合与边界；不得访问 manifest/trust、联网或执行任何服务。之后才进入 pflag/Cobra
   和统一 request 验证。保留 unknown-flags 拒绝；禁用忽略错误与相似命令自动执行。
10. 不引入 Viper/env 自动绑定；保留 HGO_TARGET_DIR/HGO_TRUST_DIR 的既有规则。
    `--offline` 只约束依赖获取，不保证脚本网络隔离。

### 接受／拒绝测试矩阵（待实现后执行）

以下以词法/请求合同为准；“接受”仍需通过正常路径/编译/权限检查，不代表服务必定成功。
所有拒绝用 mock 服务断言调用数为零；不以真实授权动作验证解析。

| argv 摘要 | 合同期望 |
| --- | --- |
| build --go /tool/go --offline INPUT | 接受，两个命令选项 |
| build --go=/tool/go --offline=false INPUT | 接受，Offline=false |
| build -o FILE INPUT / build -oFILE INPUT | 接受，相同 Output |
| build -o -aaa INPUT | 接受，Output 为字面 -aaa |
| build --o=-- INPUT | 接受，Output 为字面 -- |
| build -hoFILE / build -ho FILE | 接受为显式 help，stdout/0，零文件写入 |
| build -o=FILE INPUT | 拒绝并提示 -o FILE / --o=FILE |
| build -go /tool/go INPUT / build -go=/tool/go INPUT | 拒绝并提示 --go |
| build -offline INPUT / build -offline=false INPUT | 拒绝，不能拆成 -o 的值 |
| scripts -action allow --subject SHA PROJECT | 拒绝并提示 --action，零授权 |
| build -help / build -hhh | 拒绝；旧长拼法提示 --help，重复short提示展开单项 |
| build -aaa INPUT | 拒绝；另用模拟已注册 a 的schema确认仍拒绝重复组合 |
| build -hz INPUT / build -z INPUT | 拒绝未知short；help不掩盖其它非法语法 |
| build --unknown INPUT / build --go | 拒绝未知flag／缺值 |
| build -o -- INPUT | 拒绝缺值，不吞终止符 |
| build INPUT --offline / build --offline false INPUT | 拒绝额外operand，不隐式重排 |
| transpile -- -go | 接受字面文件名 -go，不能发旧flag迁移错误 |
| scripts -- plan | 旧默认plan动作，PROJECT为字面 plan |
| scripts plan PROJECT | 新规范plan动作，与旧 --action plan 请求相同 |
| scripts --action allow --subject SHA PROJECT | 保留兼容；仍需当前精确subject授权验证 |
| scripts allow --action trust-project PROJECT | 拒绝矛盾动作，零授权 |
| scripts trust-project（缺PROJECT） | 不补cwd、不授信；保留旧同名路径的plan解析边界 |
| run INPUT -- --offline -aaa "" -- | 接受，ProgramArgs为四个原样元素 |
| run -- -input.hgo -- -h / run INPUT -- | 接受带短横input／空参数列表 |
| run INPUT arg / test INPUT -- -run TestX | 拒绝无界尾部／未提供的test透传 |
| --help / help scripts / scripts allow --help | stdout/0；不读plan、不改trust |

还需参数化遍历所有历史单短横长名（包括 =value 形式），并通过模拟schema测试合法
不同布尔short组合、末位取值、重复short和旧长名优先拒绝；不会因此注册新的生产选项。
同一 argv 在新 Cobra 与未来 clap 适配器中应产生相同 request/error。

### help、错误和退出码

- 显式 help 已批准统一输出 stdout、退出 0。先完成词法合法性检查；例如 -hz
  不因含有 h 就吞掉未知 z。有效 help 不要求存在真实输入路径，也不执行服务。
  无参数/未知命令/缺少输入仍报错。业务失败首轮保持 exit 1，子进程原退出码不顺带
  改为透传；usage=2 也不偷偷引入。
- 解析层通过 UsageError 携带底层解析错误和简短 usage；服务保留现有错误类型与包装，
  不调用 os.Exit，不在本轮新增 CompileError/PolicyError/ChildError 全局分类。
  Cobra 配置 SilenceErrors/SilenceUsage，统一避免重复诊断；不为编译器或授权失败刷整屏 flags。
  对应开关见 [Cobra Command API](https://pkg.go.dev/github.com/spf13/cobra#Command)。
- 保持机器输出 stdout 干净：transpile/expand 是 Go 文本，scripts plan 是现有 JSON，
  import-go plan 是 TOML，managed build/transpile 是产物路径；日志/警告只到 stderr。
  schema/key 不翻译，不将 legacy deprecation 警告混入 stdout，首轮可不发弃用警告。
- help 的 Short/Long/Example 与使用指南采用中文，命令/flag 拼写仍英文。既有业务诊断
  首轮保持，不同时实施全套国际化或改错误文案。通过项目自有 help 模板统一章节文案，
  不承诺 Cobra 默认内部错误会自动中文化。
- completion 只输出补全脚本，不修改 shell 配置。采用
  [官方 completion 接口](https://cobra.dev/docs/how-to-guides/shell-completion/)，动态
  目标补全最多只读本地 manifest；不得运行脚本、解析远程依赖、写锁/授权或执行 plan。

## 5. typed request 与服务边界

```go
// Sketch only: request types and services do not import Cobra/pflag/clap.
type CompileRequest struct {
    Mode CompileMode
    Input string
    Output, SourceMap, Expanded string
    GoTool, RuntimeRoot string
    Project ProjectOptions
    Tags []string
    ProgramArgs []string // run only; verbatim argv after the program delimiter
}
type ScriptsRequest struct {
    Action ScriptAction
    ProjectRoot, Subject string
    GoTool string
    Project ProjectOptions
}
type ImportGoRequest struct {
    SourceRoot, Destination, Library string
    Plan bool
    Binaries []NamedTarget
    GoTool string
    Offline bool
}
```

建议分层 `cmd/hgo`（main/退出）→ `internal/cli`（语法门禁、旧action路由、Cobra、help）→
`internal/toolchain`（类型化用例服务）→ 现有 frontend/project/buildscript/scripttrust。
Cobra RunE 只构造 request 并调用 service，不能将 `*cobra.Command`、FlagSet、
`cmd.Flags().Changed` 或包级 flag 指针传入业务层。若业务需要“是否显式设置”，在
request 中显式建模，不能将 parser 对象当配置存储。每次 Execute 建立独立命令和请求，
避免测试/多次调用遗留权限布尔值。

“纯服务边界”意指不依赖 CLI 框架，不是宣称构建/授权没有副作用：纯 ValidateRequest
只做语法/互斥校验；路径检查、manifest/lock、进程、文件写入、授权状态由现有服务和
既有 FS/Runner/PolicyStore/IO 处理。目前 stdout/stderr 显式注入；服务内部原有
Context/取消与 os.Stdin 转发保持。通用 FS/Runner/Context 接口注入是后续可测试性改进，
不是本轮已经完成的接口。
未来 clap 适配到同样 request 与 error 契约，无需重写编译、依赖图和权限策略。

## 6. trust 不变量

当前 `scripts.go:287–340` 在显式 trust-project/allow 时直接保存授权，没有交互 yes/no；
不能把“新增确认 UI”当原行为，也不能把 parser 的 default=true 当用户授权。

- 默认动作始终 plan；缺 PROJECT 不选 cwd，缺 subject 不选第一项/最近一项。
- trust-project 只授予对应 root identity；第三方 closure 仍需精确 allow。
  allow 必须匹配当前冻结计划 subject，revoke 必须完整 SHA-256。
- 保留 root/module/repository/worktree 绑定，不把工作目录或 HEAD 换成另一种 identity。
- revoke/forget-project 当前特意绕过 manifest 内容解析；不得把 LoadGraph 放进通用
  PersistentPreRunE 而让损坏 manifest 阻止撤销。原有必要的路径/工具检查仍保留。
- clean 仅清理 owned script cache，不撤销 trust、不清掉 retained target 输出。
- help、completion、UsageError、未知 action、解析失败不调用授权服务。
- 原始 --action 与新子命令若混合且冲突，明确拒绝；不采用最后一个胜出来扩大权限。
- 保留执行及缓存复用前的 Authorize；权限检查不能只放在 Cobra hook。未来 clap
  以及程序化调用也必须走相同授权服务。HGO_TRUST_DIR 仍须在源码/产物之外。

## 7. 实施顺序与 vendor 协调门槛

1. **契约先行**：保存旧 CLI argv→request/exit/streams 的 golden cases。覆盖本报告
   18 个历史只读探针，但标出已批准改变：旧单dash长名拒绝、help stdout/0、run显式参数
   边界；增加第4节接受/拒绝矩阵，不能用旧探针结果锁回已撤销的兼容行为。
2. **vendor 前置条件已完成**：从 d51117d 精确交付点重新审计 cmd/hgo/import-go/project
   和 vendor 选项后才开始生产修改；保留原生 vendor 模式与独立内容摘要。
   随后抽服务：现有 flag parser 可作为内部过渡，只把解析结果变成上述 request；
   按 main/scripts/import-go 分项提交，复用 vendor 新服务，不回滚其新选项/迁移保护。
3. **接 Cobra 树**：先普通编译叶命令与 root help，再 scripts canonical 子命令和 legacy
   路由，最后 import-go。保持有效双dash/短选项的业务request一致，按新合同拒绝旧
   单dash长名；服务mock保证help/参数失败没有副作用。对外入口不得绕过词法门禁回落
   到 Go flag 的旧语法。业务服务不能调用旧 run(argv) 作为长期架构。run ProgramArgs
   是明确的服务输入；每一种执行模式都验证参数顺序，不把尾部传给任意Go工具命令。
4. **help/completion**：一套命令定义生成中文帮助和例子；先静态补全，再受限本地目标
   补全；固定父/子hook执行测试，不从 completion 推导授权。
5. **验收后切换**：完整旧CLI测试、import-go/vendor验收、target/manifest/lock模式、
   source map、真实子进程失败和stdin转发、trust撤销/缓存复用、无TTY自动化。测试退出码
   和stdout契约；不以“Cobra默认”替代兼容结果。后续自有clap复用这组parser合同测试。

首轮不做：自由interspersed解析、test参数透传、退出码全量重设计、目标选择语义
重写、manifest编辑命令、自动信任、clap库或public部署。

## 8. 历史检查、当前验证状态

在真实 Go 1.27.1 下构建了 **69c1ab0 CLI**，执行 18 个受限只读/解析探针：

| 调用 | 当前退出码与结果 |
| --- | --- |
| 无参数 / root --help / help | 1；usage 或 unknown command |
| build -h / build --help / scripts --help / import-go --help | 1；stderr usage，末尾 flag: help requested |
| transpile FILE / -go unused FILE / --go unused FILE | 0；同一生成 Go；此模式没有消费 go flag |
| transpile FILE -o ignored.go | 1；要求 flags 在 INPUT 前，未写 ignored.go |
| transpile -- FILE | 0 |
| run FILE -- hello | 1；未执行程序 |
| scripts plan ROOT | 1；当前无 plan 子命令 |
| scripts -action allow -subject abc 无manifest目录 | 1；在打开trust store前拒绝，无授权写入 |
| import-go --plan --out new --lib . ROOT | 1；互斥校验失败，未迁移文件 |
| test -race ROOT | 1；未知flag，当前race由服务自动启用 |
| build -offline false ROOT | 1；bool值需用等号，false被当位置参数 |

证据保存于 `/workspace/scratch/cli-parser-review`（probe.py、probes.json）。
历史只读阶段未运行 trust mutation、真正 import-go 迁移、Cobra prototype、全树race或性能benchmark；
该阶段未修改生产代码或依赖。后续实现与测试见下方交付记录，不能混作历史探针结果。
为构建精确基线，缓存缺少其已有 go-toml/v2@v2.4.3；先 offline build 明确失败，再
仅下载该锁定依赖并完成构建，未安装 Cobra 或未知来源工具。扫描缓存时访问
`/tmp/codex-daemon-1000` 被文件权限拒绝，未重试进入或绕过；不影响本审查。

用户已批准 help stdout/exit 0、failure exit 1，以及 Cobra/POSIX 风格短选项与 GNU 长选项。
第4节矩阵已落实为[真实进程测试](../../cmd/hgo/cli_process_test.go)与
[模拟 schema 测试](../../cmd/hgo/cli_schema_test.go)。每次调用建立独立 Cobra 树和 typed request；
业务入口为 executeCompile、executeImportGo、executeScripts，均不依赖 Cobra/pflag。
直接路由仅检查领先命令词，避免 Cobra 自动发现把同名项目路径改成授权动作。
隐藏 `__complete` 协议也校验已完成选项；最后未完成前缀允许继续补全，且不调用服务。

vendor 保留和一致性由已交付 vendor 服务负责，本轮未把它转换为伪造的完整本地模块。
未实施 clap，也未 merge、tag 或部署网站。执行过的检查及其结果见
[交付记录](../reviews/cobra-cli-20261004.md)。

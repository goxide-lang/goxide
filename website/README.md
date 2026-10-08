# goxide 语言介绍与自托管 Playground

中文优先的静态语言介绍网站，以及真实单文件 goxide → Go 浏览器转译器。网站源、十一个 `.gox` 示例、WASM 适配与编译器在同一仓库。最终交付 `goxide-web` **一个 Go 二进制**，内嵌网站、示例、真实编译器 WASM、Go JS runtime 和 license。运行不需要 Node、Go 工具链、资源目录、CDN、账号或数据库。

本轮验证 A01 宏捕获类别与明确类型槽边界，并保留 A04 / A06 对照；编译器源码固定为
`afa3f0a52b2d8206d75df38d10833e537ea7a0dd`，生产源摘要固定为
`2412f1a252c1dd195d1a4d5deed55e2dcd605e65d18edb0b838da36b1976203c`。
`websiteCommit` 独立记录实际网站工具 HEAD。仅本地验证，没有部署或更新线上版本。
[仓内验证说明](validation/README.md)是本片入口；[最新组合记录](LATEST-INTEGRATION.md)和
[早期交付记录](INTEGRATION.md)中的既往结果不替代这次运行。

## 从干净 checkout 复现

使用本分支的完整、非 shallow checkout。当前编译器 pin 与旧对照
`fd8380c39876c96441857a737be8e32c322a0467` 都是本分支祖先；验证器只读取本地 Git 对象，
缺少时明确失败，不自动 fetch。RFC 0024 原文另存于 `website/evidence/`，保留原提交、
文件路径、SHA-256 和 Git blob 校验；无需额外拉取提案分支，原文不进入发布目录。

准备已安装的 Go 1.27.1、race 可用的 C 编译器、Python 3.11+、Node、Playwright 1.58.2
和 Chromium，以及完整的 `go.mod/go.sum` 模块缓存。检查不安装工具、不下载依赖；本地
file proxy 和临时 Git 仓库只制造工程验收数据。Go 缓存和输出目录必须可写，浏览器测试
需要 loopback 监听与本机浏览器进程权限。

```sh
python3 -B website/validation/run.py \
  --out /absolute/new-validation-output \
  --go /absolute/path/to/go \
  --node /absolute/path/to/node \
  --chromium /absolute/path/to/chromium \
  --playwright-node-modules /absolute/existing/node_modules \
  --go-cache /absolute/writable/go-build \
  --mod-cache /absolute/complete/go-mod
```

入口从准确提交创建隔离副本，逐字节保留原 97 个 fixture、10 个示例；A04 单独追加
8 个 fixture 和 1 个业务示例，A01 再追加 12 个 fixture，共 117 个 fixture、11 个示例。
七阶段输出和失败记录保留。
旧/新 Go 字节、完整 AST、实际运行分别记账；原 33 组成功输出与 5 对原生程序保持不变，
新增语法只测当前编译器；A04 另存 32 次 LF/CRLF CLI 观察及 5 个实际原生程序输出，
A01 另存 48 次 CLI 观察及 2 个原生程序输出。
既有排版差异不计为字节相等。正式运行要求
源树已提交且干净，输出必须是新的外部目录。快速构建辅助自测为
`python3 -B website/build_support_test.py`；它不代替七阶段验收或完整原生测试。

## 构建与运行

以下是单独构建入口；完整验证优先使用上面的隔离入口。在仓库根目录，以 **Go 1.27.x**
和 Python 3.11+ 构建。Go 命令必须与用于构建 CLI 的 GOROOT 一致。构建只使用已缓存依赖，
保留 std/serde 精确版本、checksum 和无本地 replace 检查。Go LICENSE 使用所选工具链的
`GOROOT/LICENSE`，或 Homebrew `libexec` 对应发行目录的 `LICENSE`；缺失即失败。

```sh
export GO_BIN=/absolute/path/to/go
python3 website/build.py
"$GO_BIN" build -tags goxideweb -trimpath -o bin/goxide-web ./cmd/goxide-web
./bin/goxide-web -version
./bin/goxide-web                       # http://127.0.0.1:8080
./bin/goxide-web -listen 127.0.0.1:9090
```

`goxideweb` 标签使干净 checkout 的常规 `go test ./...` 不依赖尚未生成的嵌入资源。网站构建后用 `go test -tags goxideweb ./cmd/goxide-web` 测试服务器。不要使用普通 `go build ./cmd/goxide-web`。

若要在受限云环境复现本次验证：

```sh
export GO_BIN=/workspace/goxide-environment/go/bin/go
export GOCACHE=/workspace/goxide-environment/cache
export GOMODCACHE=/workspace/goxide-environment/modcache
```

显式传入 `-listen 0.0.0.0:8080` 才会监听所有 IPv4 接口。本服务器没有登录机制；需要私有远程访问时，应自行配置具有身份验证和 HTTPS 的反向代理。默认仅本机。Ctrl+C / SIGTERM 在最多 5 秒内优雅关闭。

构建阶段生成 `website/dist/`（可独立静态托管）并复制到 `cmd/goxide-web/dist/`，全部为被忽略的构建物。只把网站构建物部署出去，不要把整个仓库作为静态目录。没有创建公共站点；自托管程序不依赖 Sites。

## 真实转译与支持范围

- `cmd/goxide-playground` 调用现有 `internal/frontend.Compile`，不是演示替换或预设输出。WASM 只转译，不运行生成代码。
- 仅支持单文件。默认包：`fmt`、`errors`、`io`、`strconv`、`strings`、`math`、`encoding/json`、本仓库 `runtime`、`formatting`，以及公开 `std/sum`、`serde`、`serde/json`，以及真实导出数据包含的依赖闭包。**完整列表在 `examples.json.importClosure`**；未随附包给出诊断。
- 适配只在 `js/wasm` 下启用。原生 importer 使用离线 `go list -export`；已知 Go module 的 CLI 单文件使用输入 owner 的受管 modfile 查询同一依赖图，仅选择指定文件。WASM 导入来自构建时真实 Go export archive 的 `__.PKGDEF`，不是手写伪造类型签名，也不会调用外部服务。
- 导出数据来自构建机器的原生目标；清单记录 `exportTarget`。早期 Linux/amd64 + Go 1.27.1 记录是历史证据；A06 平台与结果以这次准确提交的验证报告为准，不把它描述为所有 GOOS/GOARCH 的类型环境。构建时请在宿主平台生成资源，之后可以交叉构建纯静态 HTTP 二进制。
- 没有浏览器文件系统、任意依赖解析、跨文件模块恢复、目录编译、外部宏包或 Go 执行 API。CLI 的 use/pub use、源码图宏聚合、version/path/git 依赖、feature、target 与受授权 host 构建脚本不在浏览器执行。
- 浏览器中一个 Worker，450 ms 去抖，32 KiB UTF-8 输入，10 秒编译超时、30 秒加载超时、2 MiB 返回输出上限。取消/超时终止 Worker；下次操作重建。编译器自身宏预算仍然生效。没有独立浏览器堆内存配额，极端输入仍可能造成浏览器内存压力。
- 失败、源码编辑与中断清空旧 Go，复制按钮同步禁用。Worker 实例 + revision 双重丢弃旧消息。通过 textarea/textContent 显示输入与诊断，不将源码插入 HTML。
- Clipboard API 不可用时选择生成结果供手动复制。Tab 保持正常导航；Ctrl/⌘+Enter 转译。390 px 布局切换上下编辑器。

## 内容来源与成熟度

页面依据根 README、`docs/t0-acceptance.md`、`internal/macros/README.md`、`internal/matchexpr/README.md`、`docs/serde-json.md` 和相应 RFC。示例明确演示宏、enum/match、Option/Result/?、Default/Debug/Clone、serde JSON、Error（thiserror 风格）和 `.equals`。

不宣称全部 MVP、完整 Rust 借用安全、完整 hygiene 或零分配。本集成分支已合入 primitive 名称冲突诊断与跨模块 Result 静态 validator 修复：包级/文件 import 冲突会拒绝并报告定义和调用位置，不能宣传为完整支持所有遮蔽；特定跨模块循环消除了逐项分配，不能外推为任意载荷零分配。单文件 `go_result` 示例真实演示 `go_result!(call)` 与 `go_result_keep!(call)`，按调用的静态签名推导 T/E；保值版保留 `struct{Value T; Err E}`，不是 Clone。E 必须实现 error 且可为 nil，使用原 E 判断 nil，具名接口与指针均保留身份；接口内 typed nil 仍为 Err。兼容显式 T 的双参数形式；单 E 返回得到 unit，多成功值须先组织为 struct。完整语义见 `examples/go_result/README.md`。源码包宏组合仍在 CLI 示例中验证，不冒充浏览器支持跨文件/预编译宏包。

## 更新与版本绑定

1. 切到要发布的编译器提交，修改 `website/examples/*.gox` 或 `website/static/*`。
2. 运行 `python3 website/build.py`。每个示例必须经真实 CLI 成功转译，否则构建失败。另将仓库 examples/reexports 多包工程真实转译并运行，写入 cliExamples；网页以只读工程快照展示，不作为浏览器示例。构建不会授权或运行用户构建脚本。输出不是手写 Go。
3. 构建脚本从当前工具链重新提取真实 export data、构建 WASM，并复制匹配版本的 `wasm_exec.js`。所有资源同批更新。
4. `examples.json` 记录源码提交、编译器与支持库源码 SHA-256、export archives SHA-256、工具链、导出目标与包闭包。摘要覆盖未提交源变化，因此只看提交号不足；正式发布应从干净提交构建。
5. 构建新 `goxide-web`，运行下面的验证。浏览器生成 Go 与 CLI 输出逐字节比较。生成 Go 若支持库语义改变，必须同时更新本地依赖与清单。

浏览器示例与拒绝例的原始字节先复制到无祖先 `go.mod` 的临时 `playground.gox`，
再调用真实 CLI；它们与 WASM 使用同一个 standalone 输入语境。正式验证器也要求
输出与临时目录没有外层 module。A06 跨包恢复使用原生 acceptance 验证，不把仓库
module 的已配置入口当成浏览器任意依赖加载能力，也不放宽 CLI/WASM 的逐字节门槛。

## 验证

```sh
"$GO_BIN" test ./...
"$GO_BIN" test -race -tags goxideweb ./cmd/goxide-web ./internal/typecheck
"$GO_BIN" vet -tags goxideweb ./cmd/goxide-web ./internal/typecheck
# 启动独立二进制后，在另一个终端：
QA_URL=http://127.0.0.1:8080 npm test --prefix website
```

Playwright 仅是测试依赖，固定在 lockfile；这里要求它已可用，不自动安装。默认使用
`/usr/bin/chromium`，可设置 `CHROMIUM=/absolute/browser`。隔离入口可用 `--playwright-node-modules`
指定既有安装。正常网站构建与运行均不需要 npm。

`qa.mjs` 验证十一例 CLI/WASM 逐字节一致，并以本地 CLI 对照 error-only、typed-nil、错误 T 和指针 error 返回值八项适配宏正反例（包括规范化文件名后的诊断一致性）、编辑真实改变结果、语法错误恢复、连续快速输入、32 KiB 拒绝、未知包、XSS 文本、复制、快捷键/Tab、桌面与 390 px 窄屏、中断/重置、无外部请求。超时测试只在测试浏览器路由中替换 Worker 为不返回结果的版本，确定性验证宿主 10 秒 watchdog，然后移除拦截恢复真实 WASM；不在产品引入测试后门。截图输出到忽略的 `website/qa-artifacts/`。

A04 的 4 个正例和 4 个拒绝例单独记录 CLI/WASM 差分：LF/CRLF 各两次、共 32 次；
UI 验证 16 次 LF 输入，真实 Worker 验证 32 次 LF/CRLF 输入，每次之后用已知合法输入
验证恢复。`a04-matrix.json` 提供独立的原生 stdout/错误预期；新增 JSON→enum→match→
Clone/Debug→JSON 示例只属于当前编译器。浏览器只转译，实际程序运行证据在隔离入口的
`native/a04/`，不会混入旧编译器的 33 组 AST 对照或原 13 个诊断 fixture。

A01 单独追加 2 个完整表达式正例和 10 个明确类型槽拒绝例。`a01-matrix.json` 固定
原 A04 输入摘要，并独立指定类型槽 `$` token 位置、宏调用栈及原生 stdout 预期。
CLI / 真实 Worker 各验证 48 次 LF/CRLF 输入，UI 验证 24 次 LF 输入，每次都有恢复
检查；中途 Worker 协议失败先保存部分观察再失败。浏览器不执行 Go；两个正例的实际
运行证据独立保存在 `native/a01/`。不扩展为 type 片段、歧义转换/单实参泛型判断或跨包浏览器支持。
FC-10 多实参泛型的明确类型槽另有真实原生 CLI 验收，不计入这 12 个网站 fixture。

`read_playground` WebMCP 只读工具在浏览器有 `document.modelContext` 时可注册；普通 Chromium 不支持该原生上下文，未将其作为已通过原生 WebMCP 验证的能力宣传。

HTTP 测试验证嵌入资源、WASM MIME、GET/HEAD、拒绝写入、目录/路径穿越/仓库文件不可见以及安全 header。服务器设置读取、写入、空闲超时和 header 大小限制。程序从只含二进制的独立目录启动后仍可完成浏览器测试。

## 静态托管准备

根路径与项目子路径的本地验证、发布资产允许列表、CF Direct Upload 与独立网站仓库 Pages 的待批准方案见 [托管准备说明](hosting/README.md)。这些准备不会自动发布。

## 文件布局

- `website/static/`：手写静态页面、样式、主线程与 Worker，无线上第三方依赖。
- `website/examples/`：十一个可执行 `.gox` 源例。
- `website/build.py`：可复现生成链与版本清单。
- `cmd/goxide-playground/`：WASM 桥接。
- `internal/typecheck/platform_{native,wasm}.go`：平台特定 importer。
- `cmd/goxide-web/`：Go embed HTTP 可执行程序（`goxideweb` 标签）。

## RFC 特性目录（公开摘要）

`catalog-source.json` 是人工审阅、可机器检查的状态源；`catalog.py` 校验 0001–0026 覆盖、状态/入口约束和固定提交的证据引用，并生成私有 [FEATURES.md](FEATURES.md) 索引。公开 `examples.json.catalog` 仅导出用途、边界、示例、状态与开发快照，不导出私有证据路径或 RFC 全文。

状态针对卡片的限定范围：提案 / 已确认待实现 / 部分实现 / 已实现并验证。开发快照与当前编译器支持分别记录；任务报告不等于可发布实现。RFC 0024 仍是提案。当前 Playground 已包含共享 std/serde、明确上下文下的普通 sum 推导及限定可读性改进；统一 TOML/锁与 import-go 只在 CLI 工程快照说明。

编译器 `commit` 固定为 `afa3f0a52b2d8206d75df38d10833e537ea7a0dd`，`websiteCommit` 记录网站源码提交；构建独立枚举 pin 与当前生产文件集，再逐文件核对字节和固定摘要，增加、缺少或修改源码均拒绝。只有真实 CLI 转译、执行输出断言和浏览器差分通过的单文件示例提供入口。生成 Go 默认折叠；拒绝例来自真实 CLI 并再次通过 WASM 对照。完整最小 feature 工程在临时目录运行，use 多包快照与脚本说明不冒充浏览器执行。

```sh
python3 website/catalog.py
QA_URL=http://127.0.0.1:8084 node website/catalog-qa.mjs
```

额外 QA 覆盖所有目录入口、11 例实际转译、10 个拒绝例、主题/状态/入口/搜索过滤、永久链接、缺失清单错误状态、复制、390px 与无外部上传。HTTPS 云环境若 Chromium 缺少平台代理 CA，可显式提供已安装 CA 的 `QA_PROXY_SPKI` 公钥摘要；测试不使用全局跳过证书校验。线上实际 HTTP 哈希独立经系统 CA 验证。

RFC 管理建议：当前规范仍跟随编译器仓库和版本，Issue 用于讨论/跟踪、PR 用于变更评审。没有为此次目录新建仓库或 Issue；公开网站不是私有 RFC 镜像。

## 统一项目版本的构建与证据

本次编译器基线 69c1ab0，使用公开 std 3f2204770fd3 与 serde a1b791e96d66 的真实 Go module 版本，无本机 replace。`publicLibraries` 记录版本和 zip/go.mod checksum，`metadataSchema` 为 7；旧 schema 6 producer 须重生成。生成 Go 的共享 sum 身份与本地 payload validator 分开，不能宣称无辅助代码或 v1 ABI 稳定。

`examples.json.wasmSha256` 是实际 WASM 字节摘要；Worker 根据 compilerCommit 使用版本 URL，执行前计算 SHA-256。app/style URL 绑定 websiteCommit，清单以 no-store 读取。哈希不匹配时拒绝执行并清空结果，而非混用旧编译器。测试覆盖损坏/陈旧 WASM 的拒绝状态。

CLI snapshot 构建实际运行 format 2 feature 工程 A/B/A、根无 go.mod/go.sum、target 生成 metadata/Go；另对普通 Go 输入执行只读 plan、独立新目录迁入、原文件摘要不变和首次 offline+locked run。只发布刻意示例及实际生成文本，不复制临时路径或执行服务。

完整编译器验收沿用 [统一项目验收](../docs/reviews/unified-project-20261004.md)：首轮 cold-cache 有 8 包因公开依赖未预热失败，预热后八包重跑通过，其他包在首轮通过；这是跨两轮的全包 race 覆盖。本网站任务另外运行定向 CLI race、真实十例 CLI/浏览器联合验证和独立审查，不声称在这里重跑一次全树全绿。

## Cobra CLI 与原生 vendor 同步

本轮基线09f1eb2；上述69c1ab0记录属于历史验证。新增严格长短选项、真实参数透传/旧旗标拒绝快照，以及空 module cache 下原生 vendor 离线迁入、locked运行和字节篡改拒绝快照。plan新增说明注释，按TOML解析结果比较目标配置，保留真实stdout。RFC0026整体仍为提案，仅显式继承约束确认；workspace未实现。RFC0024仍未实现。

## Workspace 作者核心同步

当前2aa0d06已经实现RFC0026作者核心，覆盖显式继承、全workspace统一版本、根唯一schema3锁、selected feature/build、多目标及复制根锁拆出。上节Cobra轮的“workspace未实现”是历史状态。本轮CLI快照在本地file proxy/Git fixture上实际执行，不使用公网示例模块，也不把工程送入浏览器。路径归一化后的锁仅展示，不能直接复用。go.work/workspace vendor迁入、统一host resolver、细粒度lock update、publish仍未实现。

## 原生 workspace 迁入同步

当前27d2e274已实现go.work/workspace vendor向新目录迁入，保留原生政策并物化显式成员边；迁入后支持渐进.gox/features/明确授权scripts。schema4非vendor根锁经显式物化可复制拆出。上节“迁入未实现”是历史状态；workspace vendor独立拆出仍未支持，须确认转换合同。既有host resolver/publish等限制不变。新增两个真实CLI快照，授权脚本由本轮NativeWorkspaceProgressiveScriptPermissions测试验证，网站构建本身不运行或授权脚本。

## Goxide 命名接入（2026-10-04）

本树已基于官网源码 51405a69 同步最新快照功能，并迁移 Goxide / goxide / .gox / GOXIDE_*。历史章节描述当时基线；当前 enum metadata schema 8、reference 3、use 2、sum ABI 2，std ce5bccb，serde 不变。旧生成产物需全部重新生成。CLI cmd/goxide、WASM cmd/goxide-playground、bridge goxideCompile、web tag goxideweb；构建脚本会校验实际 compiler SHA/digest。网站 owner 使用本分支的完整 website 和对应 compiler 源、go.mod/go.sum 构建，不能混用旧 WASM 与新 JS。只完成本地验证，不部署。

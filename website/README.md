# hgo 语言介绍与自托管 Playground

中文优先的静态语言介绍网站，以及真实单文件 hgo → Go 浏览器转译器。网站源、五个 `.hgo` 示例、WASM 适配与编译器在同一仓库。最终交付 `hgo-web` **一个 Go 二进制**，内嵌网站、示例、真实编译器 WASM、Go JS runtime 和 license。运行不需要 Node、Go 工具链、资源目录、CDN、账号或数据库。

本轮组合来源、联合全树验证与独立审查见 [集成交付记录](INTEGRATION.md)。

## 构建与运行

在仓库根目录，以 **Go 1.27.x** 和 Python 3 构建。Go 命令必须与用于构建 CLI 的 GOROOT 一致。

```sh
export GO_BIN=/absolute/path/to/go
python3 website/build.py
"$GO_BIN" build -tags hgoweb -trimpath -o bin/hgo-web ./cmd/hgo-web
./bin/hgo-web -version
./bin/hgo-web                       # http://127.0.0.1:8080
./bin/hgo-web -listen 127.0.0.1:9090
```

`hgoweb` 标签使干净 checkout 的常规 `go test ./...` 不依赖尚未生成的嵌入资源。网站构建后用 `go test -tags hgoweb ./cmd/hgo-web` 测试服务器。不要使用普通 `go build ./cmd/hgo-web`。

若要在受限云环境复现本次验证：

```sh
export GO_BIN=/workspace/goxide-environment/go/bin/go
export GOCACHE=/workspace/goxide-environment/cache
```

显式传入 `-listen 0.0.0.0:8080` 才会监听所有 IPv4 接口。本服务器没有登录机制；需要私有远程访问时，应自行配置具有身份验证和 HTTPS 的反向代理。默认仅本机。Ctrl+C / SIGTERM 在最多 5 秒内优雅关闭。

构建阶段生成 `website/dist/`（可独立静态托管）并复制到 `cmd/hgo-web/dist/`，全部为被忽略的构建物。只把网站构建物部署出去，不要把整个仓库作为静态目录。没有创建公共站点；自托管程序不依赖 Sites。

## 真实转译与支持范围

- `cmd/hgo-playground` 调用现有 `internal/frontend.Compile`，不是演示替换或预设输出。WASM 只转译，不运行生成代码。
- 仅支持单文件。默认包：`fmt`、`errors`、`io`、`strconv`、`strings`、`math`、`encoding/json`、本仓库 `runtime`、`formatting`、`serde`、`serdejson`，以及真实导出数据包含的依赖闭包。**完整列表在 `examples.json.importClosure`**；未随附包给出诊断。
- 适配只在 `js/wasm` 下启用。原生 importer 保持原来的离线 `go list -export` 行为。WASM 导入来自构建时真实 Go export archive 的 `__.PKGDEF`，不是手写伪造类型签名，也不会调用外部服务。
- 导出数据来自构建机器的原生目标；清单记录 `exportTarget`。当前已运行验证 Linux/amd64 + Go 1.27.1；不把它描述为所有 GOOS/GOARCH 的类型环境。构建时请在宿主平台生成资源，之后可以交叉构建纯静态 HTTP 二进制。
- 没有浏览器文件系统、任意依赖解析、跨文件模块恢复、目录编译、外部宏包或 Go 执行 API。CLI 的 use/pub use、源码图宏聚合、version/path/git 依赖、feature、target 与受授权 host 构建脚本不在浏览器执行。
- 浏览器中一个 Worker，450 ms 去抖，32 KiB UTF-8 输入，10 秒编译超时、30 秒加载超时、2 MiB 返回输出上限。取消/超时终止 Worker；下次操作重建。编译器自身宏预算仍然生效。没有独立浏览器堆内存配额，极端输入仍可能造成浏览器内存压力。
- 失败、源码编辑与中断清空旧 Go，复制按钮同步禁用。Worker 实例 + revision 双重丢弃旧消息。通过 textarea/textContent 显示输入与诊断，不将源码插入 HTML。
- Clipboard API 不可用时选择生成结果供手动复制。Tab 保持正常导航；Ctrl/⌘+Enter 转译。390 px 布局切换上下编辑器。

## 内容来源与成熟度

页面依据根 README、`docs/t0-acceptance.md`、`internal/macros/README.md`、`internal/matchexpr/README.md`、`serdejson/README.md` 和相应 RFC。示例明确演示宏、enum/match、Option/Result/?、Default/Debug/Clone、serde JSON、Error（thiserror 风格）和 `.equals`。

不宣称全部 MVP、完整 Rust 借用安全、完整 hygiene 或零分配。本集成分支已合入 primitive 名称冲突诊断与跨模块 Result 静态 validator 修复：包级/文件 import 冲突会拒绝并报告定义和调用位置，不能宣传为完整支持所有遮蔽；特定跨模块循环消除了逐项分配，不能外推为任意载荷零分配。单文件 `go_result` 示例真实演示 `go_result!(call)` 与 `go_result_keep!(call)`，按调用的静态签名推导 T/E；保值版保留 `struct{Value T; Err E}`，不是 Clone。E 必须实现 error 且可为 nil，使用原 E 判断 nil，具名接口与指针均保留身份；接口内 typed nil 仍为 Err。兼容显式 T 的双参数形式；单 E 返回得到 unit，多成功值须先组织为 struct。完整语义见 `examples/go_result/README.md`。源码包宏组合仍在 CLI 示例中验证，不冒充浏览器支持跨文件/预编译宏包。

## 更新与版本绑定

1. 切到要发布的编译器提交，修改 `website/examples/*.hgo` 或 `website/static/*`。
2. 运行 `python3 website/build.py`。每个示例必须经真实 CLI 成功转译，否则构建失败。另将仓库 examples/reexports 多包工程真实转译并运行，写入 cliExamples；网页以只读工程快照展示，不作为浏览器示例。构建不会授权或运行用户构建脚本。输出不是手写 Go。
3. 构建脚本从当前工具链重新提取真实 export data、构建 WASM，并复制匹配版本的 `wasm_exec.js`。所有资源同批更新。
4. `examples.json` 记录源码提交、编译器与支持库源码 SHA-256、export archives SHA-256、工具链、导出目标与包闭包。摘要覆盖未提交源变化，因此只看提交号不足；正式发布应从干净提交构建。
5. 构建新 `hgo-web`，运行下面的验证。浏览器生成 Go 与 CLI 输出逐字节比较。生成 Go 若支持库语义改变，必须同时更新本地依赖与清单。

## 验证

```sh
"$GO_BIN" test ./...
"$GO_BIN" test -race -tags hgoweb ./cmd/hgo-web ./internal/typecheck
"$GO_BIN" vet -tags hgoweb ./cmd/hgo-web ./internal/typecheck
# 启动独立二进制后，在另一个终端：
npm ci --prefix website
QA_URL=http://127.0.0.1:8080 npm test --prefix website
```

Playwright 仅是测试依赖，固定在 lockfile。默认使用 `/usr/bin/chromium`，可设置 `CHROMIUM=/absolute/browser`。正常网站构建与运行均不需要 npm。

`qa.mjs` 验证五例 CLI/WASM 逐字节一致，并以本地 CLI 对照 error-only、typed-nil、错误 T 和指针 error 返回值八项适配宏正反例（包括规范化文件名后的诊断一致性）、编辑真实改变结果、语法错误恢复、连续快速输入、32 KiB 拒绝、未知包、XSS 文本、复制、快捷键/Tab、桌面与 390 px 窄屏、中断/重置、无外部请求。超时测试只在测试浏览器路由中替换 Worker 为不返回结果的版本，确定性验证宿主 10 秒 watchdog，然后移除拦截恢复真实 WASM；不在产品引入测试后门。截图输出到忽略的 `website/qa-artifacts/`。

`read_playground` WebMCP 只读工具在浏览器有 `document.modelContext` 时可注册；普通 Chromium 不支持该原生上下文，未将其作为已通过原生 WebMCP 验证的能力宣传。

HTTP 测试验证嵌入资源、WASM MIME、GET/HEAD、拒绝写入、目录/路径穿越/仓库文件不可见以及安全 header。服务器设置读取、写入、空闲超时和 header 大小限制。程序从只含二进制的独立目录启动后仍可完成浏览器测试。

## 静态托管准备

根路径与项目子路径的本地验证、发布资产允许列表、CF Direct Upload 与独立网站仓库 Pages 的待批准方案见 [托管准备说明](hosting/README.md)。这些准备不会自动发布。

## 文件布局

- `website/static/`：手写静态页面、样式、主线程与 Worker，无线上第三方依赖。
- `website/examples/`：五个可执行 `.hgo` 源例。
- `website/build.py`：可复现生成链与版本清单。
- `cmd/hgo-playground/`：WASM 桥接。
- `internal/typecheck/platform_{native,wasm}.go`：平台特定 importer。
- `cmd/hgo-web/`：Go embed HTTP 可执行程序（`hgoweb` 标签）。

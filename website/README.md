# Goxide 官网与 Playground

网站源码位于本仓 `website/`，部署仓库只保存构建产物。Preact/Vite 提供首页、快速开始、使用参考、设计文档与 Playground；字体、脚本、编辑器、Worker 和 WASM 均随静态网站交付。

## 页面与内容

- `/`：真实 Go / Gox / 生成 Go 对照，以及渐进接入和完整项目两条入口。
- `/learn/?stage=mixed`：订单 CLI 快速开始，含原生 Go、混合项目、完整 Gox、包管理与 feature；`/learn/?stage=procedural` 继续编写函数式、属性与 derive 过程宏。完整项目显示 `goxide.toml`、锁文件、源码、生成结果和可复制命令。
- `/docs/?page=start`：中文指南、语言与配置参考、CLI 帮助和能力状态；支持本地搜索及页内目录。
- `/design/?page=overview`：完整词法/文法、编译与转译规则、表示规范、决策与逐特性 RFC；与使用参考分别搜索。
- `/playground.html`：真实浏览器单包转译。已有 `/rfc.html`、`/local.html` 入口继续可用。

快速开始源码保存在 `tutorial-projects/`，`tutorials.py` 与 `procedural.py` 在临时目录执行真实 build/run/test、重复生成和锁定离线重放，并生成五份 `tutorial-*.zip`。每份下载会解压到另一目录重新验证。页面中的源码、生成 Go 和 CLI 输出均来自这些实际运行；配置预设展示已验证结果，浏览器不执行完整项目。

文档由 `docs_content.py` 从仓库 Markdown 单源提取，CLI 参考读取本次编译器的帮助文本。设计文档同样直接读取 `docs/design/` 与编号 RFC；数学规则以可复制文本排版。Markdown 以安全文本、标题、代码、列表、表格和链接渲染。普通首页、快速开始和文档不加载 Monaco 或编译器 WASM；进入 Playground 才加载编辑器与编译 Worker。

## 构建

要求 Go 1.27.3、Python 3.11+、Node 22.12+（或满足 Vite 要求的新版本）及完整 Go 模块缓存。npm 版本由锁文件固定，网站构建不联网下载 Go 依赖。

网站与源码构建的 CLI 统一使用 Go 1.27.3；安装页将源码命令固定到清单中的编译器提交。
可通过 `GO_BIN` 指定 Go 可执行文件，并将其目录置于本次构建的 `PATH` 前部。
这样网页案例、下载项目、导出数据与 WASM 使用同一工具链。

```sh
npm ci --prefix website
GOTOOLCHAIN=local GOWORK=off go mod download all
python3 -B website/build.py --worktree
python3 -B website/hosting/audit.py
python3 -B website/hosting/preview.py --port 8084 --headers
```

构建把相同 CLI 的示例、文档、快速开始、Go 导出数据和 WASM 写入忽略的 `website/dist/`，并复制至 `cmd/goxide-web/dist/`。`--worktree` 记录实际编译器源码摘要，供本地预检；发布前省略该选项，以 catalog 固定的已提交编译器版本重建并验证源码身份。

`examples.json` 记录编译器来源、工具链、固定库版本、快速开始与文档内容。`macroBundle` 记录固定消费者清单、快照摘要和哈希命名的宏 WASM。构建使用 `scripts/build.go` 为 CLI 与快照工具绑定 SDK 身份，再用 Go overlay 把生成快照嵌入浏览器编译器；仓内 `proc_bundle.json` 保持空占位。`frontend-assets.json` 记录 Vite 资源及摘要；托管审计检查精确文件清单、下载归档、摘要、体积和本机路径泄漏。许可证随网站交付。

## 浏览器编译

Playground 转译一个用户包与固定随附依赖，最多 16 个 `.gox` 文件、UTF-8 总计 32 KiB；不运行生成的 Go、不下载任意依赖。所有浏览器示例的 CLI 基准使用 `validation/browser/goxide.toml` 的同一依赖图与 `playground` 包身份。一个编译 Worker，450ms 去抖，70 秒转译和 30 秒加载超时；编辑、失败和中断立即清空旧结果。

函数式、属性和 derive 示例实际调用随网站发布的 WASI 宏。页面校验 WASM 哈希并管理独立宏 Worker，宏单次执行最多 5 秒、每次编译累计最多 60 秒；停止与失败会关闭相关 Worker。固定快照包含已检查的宏注册、普通源包和运行时根，消费者编辑不能添加可执行宏或改变依赖选择。

Monaco 提供文件模型、撤销/重做、查找、基础着色与可定位诊断。站内切换页面保留 Playground 草稿和选择，刷新会重新加载示例。未知条目有错误提示，未知资源返回真实 HTTP 404。

## 验证

启动上面的本地服务后运行：

```sh
QA_URL=http://127.0.0.1:8084 npm test --prefix website
QA_URL=http://127.0.0.1:8084 node website/hosting/qa-routes.mjs
node website/compiler_test.mjs
npm run test:proc-macro --prefix website
GOTOOLCHAIN=local GOWORK=off go test -race -tags=goxideweb ./cmd/goxide-web
GOTOOLCHAIN=local GOWORK=off go vet -tags=goxideweb ./cmd/goxide-web
```

浏览器脚本使用锁定的 Playwright，`CHROMIUM` 可指定本机 Chromium 路径，始终开启 Chromium sandbox。新站验收覆盖代码对照、快速开始预设与下载、文档搜索和目录、移动布局及按需加载；catalog 验收检查真实 Worker/CLI 一致性、全部拒绝例、LF/CRLF 诊断隔离、三类宏的实际 WASM 请求、宏诊断/完整性失败/中断后的恢复，以及 Monaco 编辑和多文件行为。硬超时、依赖根篡改和真实 Worker 延迟结果也在同一入口检查。截图与运行日志保存在忽略目录，不能替代实际行为断言。

网站部署到 GitHub Pages 组织根路径 `/`。`learn/index.html` 、`docs/index.html` 与 `design/index.html` 支持直接访问和刷新；Go embed 服务和本地预览使用同样的入口。CI 执行网站构建与 HTTP 检查，浏览器验收需要单独运行。

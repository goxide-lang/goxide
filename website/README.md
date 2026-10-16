# Goxide 官网与 Playground

网站源码位于本仓 `website/`，部署仓库只保存构建产物。Preact/Vite 提供首页、快速开始、使用参考、设计文档与 Playground；字体、脚本、编辑器、Worker 和 WASM 均随静态网站交付。

## 页面与内容

- `/`：真实 Go / Gox / 生成 Go 对照，以及渐进接入和完整项目两条入口。
- `/learn/?stage=mixed`：订单 CLI 快速开始，含原生 Go、混合项目、完整 Gox、包管理与 feature 四个阶段。完整项目显示 `goxide.toml`、锁文件、源码、生成结果和可复制命令。
- `/docs/?page=start`：中文指南、语言与配置参考、CLI 帮助和能力状态；支持本地搜索及页内目录。
- `/design/?page=overview`：完整词法/文法、编译与转译规则、表示规范、决策与逐特性 RFC；与使用参考分别搜索。
- `/playground.html`：真实浏览器单包转译。已有 `/rfc.html`、`/local.html` 入口继续可用。

快速开始源码保存在 `tutorial-projects/`，`tutorials.py` 在临时目录执行真实 build/run/test、重复生成和锁定离线重放，并生成四份 `tutorial-*.zip`。每份下载会解压到另一目录重新验证。页面中的源码、生成 Go 和 CLI 输出均来自这些实际运行；配置预设展示已验证结果，浏览器不执行完整项目。

文档由 `docs_content.py` 从仓库 Markdown 单源提取，CLI 参考读取本次编译器的帮助文本。设计文档同样直接读取 `docs/design/` 与编号 RFC；数学规则以可复制文本排版。Markdown 以安全文本、标题、代码、列表、表格和链接渲染。普通首页、快速开始和文档不加载 Monaco 或编译器 WASM；进入 Playground 才加载编辑器与编译 Worker。

## 构建

要求 Go 1.27.x、Python 3.11+、Node 22.12+（或满足 Vite 要求的新版本）及完整 Go 模块缓存。npm 版本由锁文件固定，网站构建不联网下载 Go 依赖。

```sh
npm ci --prefix website
GOTOOLCHAIN=local GOWORK=off go mod download all
python3 -B website/build.py --worktree
python3 -B website/hosting/audit.py
python3 -B website/hosting/preview.py --port 8084 --headers
```

构建把相同 CLI 的示例、文档、快速开始、Go 导出数据和 WASM 写入忽略的 `website/dist/`，并复制至 `cmd/goxide-web/dist/`。`--worktree` 记录实际编译器源码摘要；省略时使用 catalog 固定的历史编译器版本，当前开发构建使用上面的显式选项。

`examples.json` 记录编译器来源、工具链、固定库版本、快速开始与文档内容。`frontend-assets.json` 记录 Vite 资源及摘要；托管审计检查精确文件清单、下载归档、摘要、体积和本机路径泄漏。许可证随网站交付。

## 浏览器编译

Playground 转译一个用户包与固定随附依赖，最多 16 个 `.gox` 文件、UTF-8 总计 32 KiB；不运行生成的 Go、不下载任意依赖。一个编译 Worker，450ms 去抖，10 秒转译和 30 秒加载超时；编辑、失败和中断立即清空旧结果。

Monaco 提供文件模型、撤销/重做、查找、基础着色与可定位诊断。站内切换页面保留 Playground 草稿和选择，刷新会重新加载示例。未知条目有错误提示，未知资源返回真实 HTTP 404。

## 验证

启动上面的本地服务后运行：

```sh
QA_URL=http://127.0.0.1:8084 npm test --prefix website
QA_URL=http://127.0.0.1:8084 node website/hosting/qa-routes.mjs
GOTOOLCHAIN=local GOWORK=off go test -race -tags=goxideweb ./cmd/goxide-web
GOTOOLCHAIN=local GOWORK=off go vet -tags=goxideweb ./cmd/goxide-web
```

浏览器脚本使用锁定的 Playwright，`CHROMIUM` 可指定本机 Chromium 路径，始终开启 Chromium sandbox。新站验收覆盖代码对照、快速开始预设与下载、文档搜索和目录、移动布局及按需加载；现有 catalog 验收保留真实 Worker/CLI 一致性、Monaco 编辑恢复、路由草稿和多文件检查。截图与运行日志保存在忽略目录，不能替代实际行为断言。

网站部署到 GitHub Pages 组织根路径 `/`。`learn/index.html` 、`docs/index.html` 与 `design/index.html` 支持直接访问和刷新；Go embed 服务和本地预览使用同样的入口。CI 执行网站构建与 HTTP 检查，浏览器验收需要单独运行。

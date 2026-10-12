# Goxide 网站与 Monaco Playground

网站开发源码在本仓 `website/`；`goxide-lang.github.io` 仅存部署产物。当前 UI 使用 Preact、preact-iso、Vite 与 Monaco。普通介绍页只加载页面脚本，Monaco 在文件视图打开时按需加载；所有脚本、编辑器 worker、编译器 WASM、字体/样式与许可证均随静态资源交付，不依赖 CDN。

浏览器只转译一个用户包及构建时固定的随附依赖，不运行生成 Go、不解析任意模块、不执行或授权构建脚本。可编辑 `.gox` 文件最多16个，总 UTF-8 ≤32KiB；一个编译器 Worker，450ms去抖、10秒转译/30秒加载超时。WASM 摘要、项目依赖根摘要、Worker实例和revision均校验。编辑/失败/中断清空旧结果。

Monaco提供文件模型、撤销/重做、查找、多光标、view state、当前文档词汇补全和基础Go着色。编译器诊断文本保留；明确的文件/行/列映射到markers。这里不是VSCode workbench，没有扩展宿主，也不提供完整Go/Goxide语义语言服务。

## 构建

要求已安装 Go 1.27.x、Python 3.11+、Node 22.12+（或满足Vite要求的较新版本）、完整Go模块缓存。npm依赖精确版本见package-lock.json；构建不下载Go依赖。工具链与缓存按工作环境启动说明设置。

```sh
npm ci --prefix website
python3 -B website/build.py
"$GO_BIN" build -mod=readonly -tags goxideweb -trimpath -o bin/goxide-web ./cmd/goxide-web
./bin/goxide-web
```

build.py校验当前源码与catalog编译器pin/摘要，使用同一CLI生成真实单文件/同包输出、固定依赖export data、工程快照与WASM；Vite生成hashed页面脚本、按需Monaco与worker。网站输出到忽略的website/dist/，同批复制到cmd/goxide-web/dist/；普通go test不需要尚未生成的embed资源。Node/npm只用于开发构建与验证，最终静态托管或Go embed运行不需要Node。

examples.json区分compiler commit/digest、websiteCommit、工具链、ABI、固定库版本与实际WASM摘要。frontend-assets.json固定Vite生成资源路径及SHA-256；审计拒绝额外文件或字节改动。THIRD-PARTY-LICENSES.txt包含锁定生产依赖的许可文本。

完整工程下载：`project-features.zip` 与 `project-targets.zip` 包含新 schema 清单、锁文件、全部源码和复现说明；构建时解压实际 ZIP 并核验 CLI 输出。首次本机构建可下载固定支持依赖，offline 运行需要已有缓存。#28 stack于#27候选690cd517，合并顺序27→28。

## 页面与工作区

preact-iso Router处理介绍 `/`、RFC `/rfc.html?feature=ID`、Playground `/playground.html?example=ID` 与本地说明 `/local.html`。实体HTML由同一个Vite壳生成，仅供直接打开/刷新；站内链接导航与前后退保留Playground实例、草稿、选择与models。刷新不承诺保留未保存草稿。未知条目显示明确错误，未知文件返回真实404。

源码、生成结果、依赖、RFC例子与CLI快照复用可收起tree + tabs + Monaco视图。可编辑tree/tabs各有新建按钮，共用命名校验。源/结果可选仅源、仅结果、左右、上下；两边按path独立选择。只读配置/依赖/快照不进入编译输入。显式选另一示例/重置清理models；删除、卸载dispose。

当前完整goxide.toml新schema示例仍依赖#23定稿。既有CLI快照说明当前合同，不是新schema承诺；#26运行入口未决。本任务不新增运行、上传、分享接口。

## 验证与本地静态预览

组织根站为https://goxide-lang.github.io/，Vite base固定 `/`；不使用 `/goxide/` 项目子路径。以下服务仅用于验证，默认loopback，不是产品执行服务。

```sh
python3 -B website/hosting/audit.py
python3 website/hosting/preview.py --port 8084 --headers
# 另一终端：
QA_URL=http://127.0.0.1:8084 npm test --prefix website
node website/hosting/qa-routes.mjs
"$GO_BIN" test -mod=readonly -race -tags goxideweb ./cmd/goxide-web
"$GO_BIN" vet -mod=readonly -tags goxideweb ./cmd/goxide-web
```

Playwright 1.58.2为固定测试依赖。Chromium路径默认/usr/bin/chromium，可设CHROMIUM；必须chromiumSandbox:true，不加--no-sandbox。在受限环境下使用启动说明允许的浏览器验证上下文，不能靠关闭浏览器沙箱通过检查。

catalog-qa.mjs验证真实Monaco键盘、多文件/readonly/布局、客户端路由与根站直达刷新、RFC内容、实际Worker/CLI字节一致、按需资源与桌面尺寸；截图在忽略的qa-artifacts/。qa.mjs保留较完整的编译器fixture与原生对照，使用Monaco交互助手；它是单独的test:compiler入口，不在每次UI调整重跑。历史七阶段隔离入口固定旧compiler与11例，说明/原native矩阵仍见validation/README.md；它不是当前Preact构建入口，不把过去运行作为当前UI通过证据。

CI从锁文件安装前端、构建匹配WASM与静态资源，运行定向HTTP race/vet及现有Draft smoke。浏览器UI仍须本任务实际Playwright记录，CI本身不声称已运行浏览器。

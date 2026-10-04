# 静态托管准备：尚未发布

从 `feat/use-macro-reexports@0dfe58bc7b529819e6402aa19ca53f6a3d473a6c` 建立独立准备分支。只构建、审计与本机浏览器验证，不创建账户/仓库/项目，不修改可见性、Pages 设置或授权，不执行上传/部署，不重试 Library。

Playground 已经是浏览器 Worker 内的真实 Go WASM 转译，不需要部署执行用户代码的后端。现有 embed 单二进制仍共享同一批静态资源。

## 本地构建、审计与验证

```sh
export GO_BIN=/absolute/path/to/go # 已验证 Go 1.27.1
python3 website/build.py
python3 website/hosting/audit.py
# 两个本地终端启动静态模拟器；始终只监听 loopback：
python3 website/hosting/preview.py --port 8084 --headers
python3 website/hosting/preview.py --port 8085 --prefix /goxide/
# 再一个终端：
QA_URL=http://127.0.0.1:8084 npm test --prefix website
QA_URL=http://127.0.0.1:8085/goxide npm test --prefix website
node website/hosting/qa-routes.mjs
```

模拟器验证真实浏览器与实际静态文件，不模拟编译结果。`--headers` 在根路径应用 CF `_headers`，子路径模拟 GitHub Pages 无自定义响应头环境。它不是 CF/GitHub 平台的端到端部署测试；实际平台仍需批准部署后验证 MIME、headers、状态码及访问级别。

资源全部使用相对地址；`/goxide` 必须规范化重定向到 `/goxide/`。根路径和项目子路径不需要分别改写 JS/WASM。404 使用独立纯 HTML，不回退到 Playground 假装成功；顶层 `404.html` 也避免 CF 缺失页默认走 SPA fallback。GitHub Pages 不消费 `_headers`，所以不宣称其提供与 CF/embed 服务器相同的 CSP headers。

## 发布目录与隐私范围

唯一拟发布目录是 `website/dist/`，构建时和审计时双重允许列表，共 11 个文件：

```text
index.html 404.html style.css app.js worker.js
compiler.wasm wasm_exec.js examples.json GO-LICENSE.txt
_headers .nojekyll
```

不复制根仓库、`.git`、完整编译器 `.go` 源文件、RFC、研究日志、QA、Node 依赖、凭据或构建脚本。`examples.json` 仅包含刻意展示的五例 `.hgo`/生成 Go、提交与源摘要、工具链、包清单。Go export data 刻意内嵌在 WASM，不单独发布归档。

构建现在对 export data 使用 `go list -trimpath`，对 WASM 使用 `-trimpath -buildvcs=false -ldflags='-s -w -buildid='`。审计检查允许列表、每文件 25 MiB、已知本机绝对路径、常见密钥标记与 WASM debug/name 自定义节。所有资源仍复制到 embed 目录，HTTP 二进制不走另一条构建链。

**公开 WASM 可以下载、反编译与分析。** stripped 不等于保密或不可逆；Go 的运行时符号/相对文件名、包路径、字符串常量以及类型 export data 仍可揭示实现信息。这里移除的是不必要的本机路径和调试节，不承诺隐藏编译器逻辑。已知 secret 模式扫描也不是对所有可能敏感值的证明。用户仍须明确同意公开这些有意提供的资产。

## 方案 A：CF Pages Direct Upload

本地构建并审计 `website/dist/`，由用户批准后上传这一目录。无需给 Cloudflare 私有 GitHub 仓库读取权；无需 Functions、R2、KV、D1 或代码执行服务器。

需要用户选择项目名称、Cloudflare 账户和访问级别，并批准创建项目及上传公开可访问资产。最少可用 dashboard 的拖放上传，或授权后的 Wrangler；如使用 API token，限定目标账户的 **Cloudflare Pages: Edit**，账户 ID 不是 token，不将任何 token 写入仓库。项目的 pages.dev/preview URL 默认不能当作私有访问控制；若需私有预览，必须另行配置/验证访问限制。

下面只是待批准后的命令形状，本任务没有安装 Wrangler、登录、创建项目或执行它：

```text
wrangler pages deploy /absolute/path/to/approved-static-folder --project-name APPROVED_PROJECT --branch main
```

执行时应从单独的干净 staging 目录操作，该目录只含允许列表资产，避免 Wrangler 意外发现仓库里的 Functions/配置。Direct Upload 项目不能直接切换成 Git integration；如以后需要 Git 自动构建，应另行选择方案。[官方 Direct Upload 说明](https://developers.cloudflare.com/pages/get-started/direct-upload/)

CF 当前单文件上限 **25 MiB = 26,214,400 bytes**，本次 WASM 实测低于此上限，不需要额外对象存储或 CDN。配额以重新部署时的官方信息和 `audit.py` 实际结果为准。[官方限制](https://developers.cloudflare.com/pages/platform/limits/)

## 方案 B：独立网站产物仓库 + GitHub Pages

用户批准后才能创建/选择**单独的公开网站仓库**；原 `hxsf/goxide` 保持私有且不用改变可见性。仅把审计后的 dist 内容放入这个新仓库的 `public/`。不把整个私有源码仓库 checkout 到该 workflow。

GitHub Free 可对公开仓库使用 Pages；项目站默认位于 `https://OWNER.github.io/REPOSITORY/`，因此测试了 `/goxide/` 子路径，但没有预定仓库名或制造真实站点 URL。[官方可用范围与地址规则](https://docs.github.com/en/pages/getting-started-with-github-pages/what-is-github-pages)

需要用户批准新仓库公开资产、仓库内容写入、启用 Pages（Source: GitHub Actions）及 Actions 环境。工作流仅需要 `contents: read`、`pages: write`、`id-token: write`，不需要私有 goxide token 或 Cloudflare 授权。

`github-pages.yml.example` 是**未启用的模板**，不在 `.github/workflows`，只有 `workflow_dispatch`，没有 push 自动部署。官方 Actions 固定到 2026-10-04 只读核验的提交；批准后才能复制为新网站仓库的 `.github/workflows/pages.yml`，手动触发前还应检查 pin 更新及目标环境保护规则。只上传 `public/`，不用仓库根目录作为 artifact。[官方工作流权限与动作](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages)

## 当前待用户决定

选择 A 或 B、确定公开资产范围及访问级别，然后才授权对应账户/仓库/项目操作。本次没有真实托管 URL；loopback 预览和本地成功不表示已经上线。Library 批量助手此前 HTTP 401 的状态未改变，本任务没有尝试新的上传路径。

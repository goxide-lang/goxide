# 静态托管：组织根站构建与本地审计

源码在goxide/website；goxide-lang.github.io只接收审计后的dist。当前Pages只读查询返回https://goxide-lang.github.io/，source path为 `/`。preact-iso Router使用根站路径，Vite base为 `/`，不是项目仓库子路径。实体 index/rfc/playground/local.html 与 learn/index.html、docs/index.html、design/index.html 共用页面壳，客户端导航不整页刷新；不存在的路径返回独立404。

以下命令构建并验证本地静态网站；正式构建与部署由部署仓库的 Actions workflow 完成。

```sh
npm ci --prefix website
python3 -B website/build.py --worktree
python3 -B website/hosting/audit.py
python3 website/hosting/preview.py --port 8084 --headers
# 另一终端：
QA_URL=http://127.0.0.1:8084 npm test --prefix website
node website/hosting/qa-routes.mjs
```

根发布文件允许列表为index.html、rfc.html、playground.html、local.html、404.html、worker.js、proc-macro-worker.js、proc-macro-wasi.js、compiler.wasm、wasm_exec.js、examples.json、frontend-assets.json、GO-LICENSE.txt、THIRD-PARTY-LICENSES.txt、_headers、.nojekyll，加 learn/index.html、docs/index.html、design/index.html、project-reexports.zip、五份 tutorial-{go,mixed,gox,features,procedural}.zip、宏清单列出的 `proc-macros/<sha256>.wasm`，以及 frontend-assets.json 列出的精确 assets/hashed JS/CSS/字体。审计递归检查集合/摘要、每文件25MiB、已知本机路径与secret标记、编译器和宏 WASM 的 debug/name 节；只发布批准的快速开始源码、测试、单源指南和构建产物，不复制完整仓库、node_modules 或凭据。

编译器Worker、受限过程宏Worker与Monaco编辑器Worker分别打包；浏览器只转译并执行内置宏展开，Monaco提供编辑器基础功能。Monaco需要动态内联布局/主题样式，CSP允许style-src 'self' 'unsafe-inline'；script仍只允许同源和WASM执行，worker同源，字体同源/data。_headers仅由支持它的托管读取；GitHub Pages不消费它。Go embed服务器设置等价响应头，测试模拟器可显式启用头部验证。

## CI 自动发布

在 [部署仓库的 Publish website workflow](https://github.com/goxide-lang/goxide-lang.github.io/actions/workflows/publish.yml) 手动填写 `source_ref`，可选 `main`、当前发布分支 `publish/serde-json-sugar` 或完整源码提交 SHA。workflow 先把 ref 解析为精确 SHA，读取公开源码，以 Go 1.27.3 和 Node 22 构建固定编译器版本、全部网站示例和下载项目，再运行 HTTP race/vet 与完整托管审计。

构建 job 只有读取权限。成功后，独立发布 job 下载同仓库本次运行的 artifact，核对网站源码 SHA、干净编译器身份和 WASM 摘要，再执行所选源码的完整审计。发布仅使用部署仓库自己的短期 `GITHUB_TOKEN`，无需跨仓库密钥、个人 token 或 Actions secret。普通源码 CI 的 `website_release=true` 仍只构建、审计并上传 artifact，用于检查；它不负责跨仓库部署。

发布脚本保留 `.github/`、`CNAME` 与不在旧产物清单中的文件，只替换已知静态文件，并从真实 manifest 更新部署 README。目标 `deploy/goxide-site-ae61ec6` 必须是当前 `main` 的祖先，远程 `main` 在 checkout 后不得变化；两个 refs 通过非强制原子 push 同步。

GitHub Pages 沿用 `deploy/goxide-site-ae61ec6` 根目录。因为 `GITHUB_TOKEN` 的 push 不会自动触发 Pages，workflow 显式调用 Pages build API，等待本次部署提交状态为 `built`，并核对线上 `examples.json` 的网站源码 SHA。并发组 `website-publish` 串行发布，不取消正在执行的部署。

该工作流执行网站构建、HTTP race/vet 与静态审计；浏览器 QA 和源码仓库的全树检查仍看各自结果。网站仍可完全静态托管或打包为Go embed二进制。WASM公开后可下载分析，审计不承诺实现保密或发现所有secret。

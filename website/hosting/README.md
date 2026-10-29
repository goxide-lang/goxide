# 静态托管：组织根站构建与本地审计

源码在goxide/website；goxide-lang.github.io只接收审计后的dist。当前Pages只读查询返回https://goxide-lang.github.io/，source path为 `/`。preact-iso Router使用根站路径，Vite base为 `/`，不是项目仓库子路径。实体 index/rfc/playground/local.html 与 learn/index.html、docs/index.html、design/index.html 共用页面壳，客户端导航不整页刷新；不存在的路径返回独立404。

以下命令构建并验证本地静态网站；发布产物需另行同步部署仓库。

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

网站仍可完全静态托管或打包为Go embed二进制。部署须由用户另行授权，发布仓库与源码仓库分开；旧部署流程模板仅作历史材料，不自动启用workflow或trigger。WASM公开后可下载分析，审计不承诺实现保密或发现所有secret。

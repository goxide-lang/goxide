# 本地托管兼容性验证结果

基线 `0dfe58bc7b529819e6402aa19ca53f6a3d473a6c`，分支 `prep/static-hosting-portability`。仅变更网站构建与托管准备，未修改生产编译器语义或工具链版本。

在真实 Chromium、同一批静态产物中，通过：

- 根路径 `/`（本地应用 CF `_headers`）与 `/goxide/`（无自定义响应头，模拟项目 Pages）各一次完整浏览器回归。
- 两次均包含五组示例 CLI/WASM 逐字节相同、四项 Go Result 宏正反例、编辑、错误恢复、快速编辑、复制、键盘、390px、大小限制、未知包、XSS 字面文本、中断、超时、重置、无外部请求；JS 错误为零。
- 独立 route QA：`/goxide` 重定向到带斜杠路径、worker/runtime/WASM/JSON 200 与 WASM MIME、五个不存在路径的 404 与独立错误页面。未知路由不返回 Playground。
- `go test -tags hgoweb ./cmd/hgo-web` 通过；embed 二进制构建成功，静态目录与 embed 目录文件和内容相同。
- 文档检查通过。此次没有重跑与网站部署准备无关的全树编译器 race。

只读独立审查确认没有阻断：11 个允许列表文件、无检测到的本机路径/已知 secret 标记，WASM custom section 仅 `producers`。DWARF/name section 被移除。审查提出的优化模式绕过 assert 问题已关闭：`audit.py` 在 Python `-O`/优化模式下会明确拒绝执行；普通模式仍必须全部通过。

WASM 实测 **15,460,293 bytes，14.7441 MiB**，比 25 MiB 单文件限制少 **10,754,107 bytes**。此大小不依赖 gzip 或服务器压缩。具体资产摘要与总大小由 `python3 website/hosting/audit.py` 输出，构建清单额外记录 strip/trimpath flags。

审计是允许列表加已知模式检查，不能证明所有秘密均不存在；WASM、export data 和运行时信息仍可分析。源码 RFC/研究文档/完整仓库不在发布目录。

这些都是**本地实际浏览器测试和平台行为模拟**；没有创建账户/仓库/项目，没有授权、上传、启用 Pages、改变可见性或公开部署。用户尚需选择 CF 或独立网站仓库，并批准公开资产和所需账户权限。不存在本次的线上 URL。

## Goxide 正式命名（2026-10-04）

以上历史记录保留当时名称；当前统一 Goxide / goxide / .gox / GOXIDE_*。旧输入明确拒绝，旧生成产物全部重新生成；enum schema 8、reference 3、use 2、std sum ABI 2。正式 module path 保持不变。迁移范围不包含 workspace vendor 拆出和官网部署。

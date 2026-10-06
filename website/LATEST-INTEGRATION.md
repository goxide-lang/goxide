> 当前仓内验证固定编译器源码为 `e36d23dacee64d19b34aac639900c3f5c29e55c2`，网站工具 HEAD 单独记录。
> 构建已整合来源选择与项目快照，复现入口见[验证说明](validation/README.md)；本片仅本地验证，未部署。
> 以下保留早期五例/旧模块集成记录，不代表当前实现或本片实际测试结果。

# 历史 CLI / 浏览器组合验证（ae61ec6）

独立分支 `integrate/latest-web-features-inference` 从共同基线 `f097b1951dde4f8653a0629a7da8a0bb5646ec79` 开始，按来源 cherry-pick（保留 -x）：

| 原提交 | 整合提交 | 内容 |
| --- | --- | --- |
| 25e3fa6 | 4b1c029 | version 源码规划设计 |
| 2081933 | 16098cc | version / features 联合 Go 规划 |
| bf365b9 | 861d763 | Result 静态 T/E 推导设计 |
| 59d6245 | 44635d7 | 推导实现与原始 E 的 nil 语义 |
| 0517111 | 0996213 | 11 文件静态托管准备与资产审计 |

网站更新在 3b3b053，审查建议和快照展示检查在 1470e58。生产编译器没有额外修改；基线中的 targets、受授权 build scripts、use/pub use 与源码图宏聚合保留。没有改动源分支、main、冻结标签或创建 release。

网站保持五个真实单文件可编辑示例，推导宏包括保值版本。CLI-only 段落解释已经实现的依赖、feature、target、host scripts 及其边界。构建时实际转译并运行 `examples/reexports`，其 hgo 文件、生成 Go 和 stdout 进入同一清单；不是浏览器多包执行。原生实现和 go.mod 在仓库示例中。输出为 `shared 2 2 true` 和 `42 2 7`。

新增 `TestVersionFeaturesScriptTargetIntegration` 使用本地 Go 版本代理和既有授权机制，在同一 fixture 验证下载版本源码 → root feature 传入脚本 → 改变生成常量 → target 路径隔离与 A/B/A 稳定；升级相同字节脚本仍需新许可，撤销后缓存不执行。独立模块 Result 测试同时验证 keep 的部分值与原错误身份、? 和 match。

## 发布边界

仅准备本地静态资产和自托管二进制；未发布网站。不得把本地 loopback URL 描述为互联网可访问私有 Site。浏览器仅单文件和随附 export data，不运行 Go、不运行构建脚本、不上传代码。host scripts 是需授权的非沙箱本机程序；offline 不限制脚本网络。WASM 包含可逆向的实现和 export data，审计不是源码保密保证。

未访问权限未就绪的公共网站仓库，未再次调用失败的 Library 上传路径。公开发布仍等待用户授权和正常连接权限。

## 验证环境与首次失败

Go 1.27.1 / Linux amd64，Chromium + Playwright 1.58.2。受限环境必须设置可写的 `GOCACHE` **及** `GOMODCACHE`。首次全树 race 中十个 path/git CLI 测试因默认 `/home/agent/go/pkg/mod` 不存在且无法创建而失败，Go mod verify 报 `missing ziphash: open hash`。独立 A/B 只改 GOMODCACHE，`TestProjectLocalFeatureUnion` 从失败变为通过（日志见 validation/latest/cache-*）。没有通过放宽源码校验绕过失败，没有更改生产编译器；后续全树使用 `/workspace/goxide-environment/modcache` 重跑。

## 完成检查

- 完整合成树 `go test -race -tags hgoweb ./...` 通过（CLI 包 256.678 秒；全部 acceptance 通过），`go vet -tags hgoweb ./...` 与 `scripts/check_design.py` 通过。日志保存于 `validation/latest/`；后续报告提交仅改文档与证据。
- 真实 Chromium 对单二进制、根静态路径和 `/goxide/` 子路径分别通过完整 QA：五例 CLI/WASM 逐字一致，八项推导/诊断差分，编辑、错误恢复、快速编辑、复制、32 KiB、未知 import、XSS、键盘、390px、中断、重置与无外部请求。超时是 QA 拦截 worker 的确定性宿主 watchdog 测试，不声称任意恶意源码内存受硬限额约束。
- 新增快照与 manifest 的 source/generated/stdout 文本一致性通过；CLI 只读示例不进入浏览器选择列表。390px 展开工程源码，Enter 键操作原生 details，无整页横向溢出。一次临时 QA 等待折叠内容 visible 超时，改为等待 attached 后展开即通过；产品无需修改。
- 本地 root / project-path 资源路由、WASM MIME、真实 404、模拟 CF 响应头通过。静态目录和 embed 11 文件一致；allowlist、25 MiB/单文件、已知绝对路径/secret 模式及 WASM debug/name 段审计通过。WASM 为 15,613,939 字节。
- 独立审查再次运行 CLI reexports 并逐字对照，确认真实输出、textContent 和发布目录范围；非阻断的源码快照范围建议已修正文案。

最终干净报告提交后重新构建资源和三个自托管平台包；清单写入最终 HEAD。交付目录的 audit.json、SHA256SUMS 与 standalone.json 记录最终文件身份和空目录/无工具链 PATH 验证。仅 Linux amd64 实机运行；macOS arm64、Windows amd64 只交叉构建，未签名、未宣称本机运行验收。

## 本地模块迁入修正（待网站 owner 同步）

`feat/vendor-local-modules` 的编译器核心为 `fceeb8669e738ebffc968b8bd948b2334fe627cb`，生产源 digest `c03e0b656ede234d0d11f2a8fbeb0a1204534594d70a69737b7d5781fb66b472`。当前 catalog、project/workspace snapshots 和首页文案已同步：原生 vendor 只作迁入输入，输出为普通本地模块；没有 Goxide vendor 构建或拆出模式。迁入先保留修改字节；local 默认的来源候选已显示，TTY/remote 切换尚未交付。

owner 从此分支执行 `python3 website/build.py`，将同批生成的 CLI/WASM/JS/catalog 一并验证发布，不能拿旧 vendor 文案或快照配新 CLI。`--offline` 始终双横线，仅用本地材料/缓存，不探测远端。当前只进行了云端 loopback 构建和验证，没有部署、合 main、tag 或 release。详细证据见 [本地模块验收](../docs/reviews/vendor-local-modules-20261004.md)。

## 来源选择后续交付（2026-10-05，待 owner 同步）

本节接续上面的本地核心，TTY/remote 已实现。编译器 `3a8f204a4eacc62ba11ac15fce7a29c91f0bd5d7`，生产源 digest `27323c6bf7a3066bab1d6546355e681e2b2d548e10c07389ffe2793f6541aa42`。终端迁入展示一次模块清单，默认 local；非 TTY 不读输入，重复 `--source MODULE=local|remote` 显式选择。远端仅支持原清单证实的精确 Go module path/version，包括版本化 replacement；未提供 Git/任意 URL/registry。差异按模块确认，非 TTY 必须 `--accept-source-diff MODULE`；所有供应原字节仍在输出中，remote 通过普通 replacement 成为运行来源。verified 仅针对供应文件集合；完整 Go graph 按普通规则验证。

`--offline` 禁止工具控制的代理、sumdb、Git fetch 与工具链下载，remote 只可用完整缓存；缺缓存明确失败。plan 不读取终端、不执行 remote 比对，正常 build 不询问。owner 同步当前分支 catalog/快照/文案并重建，不把旧“TTY 待实现”边界带入新版本；本轮没有部署。

来源选择完整验收与独立复审见 [2026-10-05 报告](../docs/reviews/source-selection-20261005.md)。

# #25：网站工作区的最小实施方案

状态：方案先行；本提交只有本文，无 UI 实现。关联 [#25](https://github.com/goxide-lang/goxide/issues/25)，依赖 [#23](https://github.com/goxide-lang/goxide/issues/23)；运行入口属于未决的 [#26](https://github.com/goxide-lang/goxide/issues/26)。起点为 main `cf56a52c22acff7a1ab353f86515745b542d6abd`。先建立 Draft PR 并报告链接，再在同一 PR 推进无争议 UI；不合并、部署、release 或修改权限/凭据。

## 栈与范围

源码在 `website/`；`goxide-lang.github.io` 只放部署产物。本次已读环境启动说明、根/网站/验证/托管 README、T0 与 gap 入口、RFC 0025/0026 和上述三个 issue。适用源码树未发现 AGENTS.md 或仓内 SKILL.md；这次是已有仓库的短方案提交，不套用建站/托管工作流。

当前是静态 HTML/CSS/原生 JS。`static/app.js` 已有 textarea、源码/结果 tabs、同包多文件增删、readonly 依赖源码、一个 Worker、去抖与 revision/实例校验。`package.json` 仅固定 Playwright 1.58.2 为验证依赖。复用原生 textarea 和编译调度，提取小函数/组件；暂无引入 Monaco、CodeMirror、React、路由库或打包器的必要。若实际交互证明原生编辑器不够，先说明具体缺口、依赖成本再调整方案。不自建 IDE、通用框架、插件系统或虚拟文件系统。

本片 UI 包括统一 tree + tabs + editor（源码、结果、依赖和工程快照都可复用），可收起 tree、可编辑集合的 tree 与 tabs 各有新建按钮、四种双编辑器布局、多页路由、RFC 左小列表右简介/设计说明/代码示例，以及两处文案。首页泛型限制按 `docs/static-protocols.md` 收窄：已有单关联协议约束的用户泛型函数；泛型协议/impl、多协议组合等仍有限。根 README 删除“十二组”的手工计数，说明真实单文件/同包示例，以当前 manifest 为准。文案与 UI 在 Draft PR 建立后再改。

完整项目示例是 #25 的剩余验收，等 #23 schema 定稿才改。已有 format 2 / roots 快照不是新合同；不自创 TOML schema，不把只读配置或跨包快照送进同包 WASM 输入。#26 未决，不增加远程 run、localhost 执行服务、上传或分享源码。用于 Playwright 的临时 loopback 静态预览仅为测试，不是产品运行入口。

## 两个小组件与编译边界

接口为拟实施的原生 JS 函数边界，名称可按实际代码简化；不增加公共组件协议。

| 组件 | 最小输入与状态 | 行为 |
| --- | --- | --- |
| FileView | `files: [{path, source}]`、`activePath`、集合级 `readonly`、`treeCollapsed`；本组件按集合单独挂载 | tree 与 tabs 从同一数组渲染，选择同一 activePath；textarea 显示当前文件；两处新建入口调用同一新增动作；readonly 隐藏新增/删除入口，textarea 设置 readOnly，修改动作也拒绝写入 |
| DualEditor | source/result 两个 FileView、`layout: source / result / horizontal / vertical` | 仅源码、仅结果、左右、上下；布局只决定可见性和排版，不复制文件状态或触发编译；结果始终 readonly |

FileView 仅需选择、编辑、新建、删除回调。文件 path 在集合内是稳定身份；依赖以包分组并显示已有 provenance。不引入随机 ID、事件总线或独立 store。共享一个新建文件名入口/校验逻辑，沿用当前普通 `.gox` 名称、重复文件名、最多 16 文件和 32 KiB 总量限制。只读集合包括生成结果、依赖、项目配置和 CLI 快照；可编辑集合仍仅为同一个用户包。

编辑时立即写回当前文件；换文件前保存光标/选择区/滚动位置，用按 path 的小 Map 恢复。tree 与 tabs 始终反映同一选择。源码与结果各自保留选中 path，不能假设一份源码恰好对应一份生成文件；转译成功后保留仍存在的结果 path，否则选择首个。readonly 内容可选择/复制，不能通过编辑或新增动作写回输入。

继续复用现有 compile/changed/kill、Worker 协议、WASM 与依赖摘要校验。源码改变、失败、中断仍清空旧结果并禁用复制，隐藏结果面板不改变这一规则；迟到响应不复活旧结果。不重写编译器、不扩大浏览器模块加载能力。tree 折叠与四种布局通过明确按钮控制；窄屏左右布局以 CSS 改为上下，保留用户选择，回到宽屏恢复。按钮带可理解的名称、选中/展开状态；Tab 保留导航，折叠/隐藏时将焦点移至可见控件。

## 多页路由与 RFC

拟用实体 HTML 入口：`index.html`（介绍/边界）、`rfc.html?feature=<catalog id>`、`playground.html?example=<example id>`、`local.html`（本地使用/工程快照）。使用 catalog 的已有 id，RFC 编号在列表显示；不复制第二套状态来源。直接加载或刷新由对应 HTML 提供页面骨架，共享相对 JS/CSS/manifest，根路径和项目子路径都能使用，不依赖服务端 rewrite。

少量 URL/History API 处理站内导航、pushState 和 popstate。同一文档中保持一个 Playground 状态/DOM，不因路由重新挂载或重置；切页保存/恢复页滚动，更新标题、当前导航和主内容焦点。RFC/示例参数通过已有 catalog/manifest 校验；相同示例不重置草稿，显式选择其他示例仍遵循当前重置语义。刷新保证回到正确页面；本片不承诺刷新或新标签保留未保存草稿，不增加持久存储/项目序列化机制。缺失页面仍走真实 404；不存在的 feature/example 显示明确未找到状态，不静默选择另一项。

RFC 页左侧窄列表（编号、名称、状态），右侧连续展示当前项的简介、设计/用法、边界和完整示例。复用 catalog 数据与安全 textContent/textarea 输出。代码文件统一 FileView，只读快照也使用 tabs，不以 card 网格或文件 details/accordion 组织主内容。手机尺寸将列表放上方，保持当前项可见和正常阅读顺序。RFC 链接与“打开 Playground”链接能直接访问、刷新及前后退。

## 文件边界与顺序

1. 本次只提交 `website/ISSUE-25-PLAN.md`，#25 留言归并文案，建立关联 Draft PR 并报告短方案。
2. Draft PR 后先做 FileView/DualEditor 与两处文案，复用现有 app/style；再接 RFC 布局与路由。替换旧 UI，不保留旧 card/accordion UI 或 hash 导航兼容层。单文件/同包编译入口仍按现有编译合同处理，不属于旧 UI 兼容层。
3. 路由新增 HTML/JS 文件需要同步构建拷贝、site commit URL 绑定、发布允许列表、hosting preview/audit 与 embed HTTP 路由测试，不能只改前端让直接刷新失败。这些位于 `website/build.py`、`website/hosting/*`、`cmd/goxide-web/*`；在改前先交父任务协调归属，避免与 #23 的生成清单工作冲突。协调完成前可先做现有 app/style 内的组件与文案。
4. #23 拥有项目 schema、构建/依赖实现及相关生成清单；本任务不并行修改这些文件、根 go.mod/go.sum、CLI 或 compiler。完整示例要拿到 #23 确定合同及对应提交，再保存完整 goxide.toml/必要源码与可重放命令，核对实际 target 输出/依赖来源，按风险做一次定向 CLI 复现。延后期间明确记录验收未完成。

## 定向验证与独立审核

本次文档检查：链接/路径、源码接口核对与 git diff 检查；不跑 native 全套或把历史网站记录当成本次验证。后续 UI 必须用匹配当前源码的构建产物与 manifest，Playwright 明确 `chromiumSandbox: true`，不带 `--no-sandbox`。现有 `qa.mjs` / `catalog-qa.mjs` 已启用 sandbox，`hosting/qa-routes.mjs` 仍有关闭 sandbox 的旧参数，触及路由验证时须更正；不改系统权限。

- 多文件：两处新增入口、重复/非法名称拒绝、切换后编辑/选择区/滚动恢复、删除当前文件后的选择一致；真实 Worker 多文件转译结果与当前 CLI 对照。
- readonly：结果/依赖/配置/工程快照可以选择复制，但编辑/粘贴不改变数据、不显示新增按钮、不进入 Worker 输入。
- 布局：四种模式、隐藏结果期间编辑/失败/中断、快速输入与迟到结果；宽屏和 390px 移动尺寸，无横向页面溢出，恢复宽屏布局。
- 路由：实体入口直接打开/刷新，根路径与 `/goxide/` 子路径，RFC/示例深链接、前后退、未保存源码在站内导航后保留、未知路由/参数、焦点与键盘导航；浏览器不请求外部执行服务。
- 只运行改动涉及的 UI/路由检查、一次必要的 WASM/CLI 定向对照；若修改构建/允许列表，追加对应构建/静态审计与 embed HTTP 测试。不反复重跑全套 native 验收。

独立审核重点为可删减机制：是否只有两种必要组件、是否复制状态/渲染器、History 处理是否能更短、是否误加持久化/框架/IDE 功能或旧 UI 兼容层；同时查 readonly 写入边界、路由直接刷新及 #23/#26 越界。审核结论与未完成依赖写回同一 Draft PR；保留 Draft 直至完整验收。

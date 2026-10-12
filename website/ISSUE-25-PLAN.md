# #25：React + Vite + Monaco 网站工作区方案

关联 [#25](https://github.com/goxide-lang/goxide/issues/25)、[Draft PR #28](https://github.com/goxide-lang/goxide/pull/28)。起点 main `cf56a52c22acff7a1ab353f86515745b542d6abd`。本方案按用户最新选择替代原生 textarea 路线；先更新 issue / Draft 方案，再实现同一 PR。不合并、部署、release 或修改凭据。

## 当前栈与技术选择

原网站为静态 HTML/CSS/原生 JS，已有同包多文件、生成 Go 与依赖源码浏览、Worker WASM 转译及 revision/实例防迟到响应。源码仍在本仓 `website/`，`goxide-lang.github.io` 仅为部署产物仓。已读启动说明、相关 README、RFC0025/0026、#25/#23/#26；适用源码树没有 AGENTS.md / 仓内 SKILL.md。

用户选择 React + Vite + Monaco Editor：面向桌面编程受众，保留熟悉的编辑快捷键与模型能力。React 负责 tree/tabs/布局与页面，React Router 负责客户端导航，Vite 负责模块、按需加载与 worker 打包，Monaco 负责编辑、逐文件 models、view state、undo 和 diagnostics markers。只接 Monaco 必要的编辑功能和 Go 基础着色，不引入整套 VSCode workbench、扩展宿主或未经实现的 Go/Goxide 语言服务。基础着色不等于语义补全。依赖固定在 npm lock，运行时资源同源，无 CDN。

原生 textarea UI 不保留，也不引入 Vue / CodeMirror / 双套兼容 wrapper。已有文件模型、命名/数量/大小限制和 WASM 调度可复用；编辑器选择/撤销/光标交给 Monaco，不另造 IDE 状态系统。编辑器模块只在打开文件视图时加载，普通介绍页不下载 Monaco。

## 最小组件与行为

| 部分 | 窄接口与责任 |
| --- | --- |
| FileView | files、activePath、readonly、select/edit/create/delete；可收起 tree 与 tabs 使用同一文件集合，两处新建按钮共用校验动作；只读集合没有创建/删除能力 |
| MonacoPane | 当前 file、readonly、markers、change；按文件 URI 管理 models，保存/恢复 editor view state，删除/重置/卸载 dispose；结果/依赖/快照可选择复制，不能写回源码 |
| 双面板 | source/result FileView 和 source/result/horizontal/vertical 布局；不复制文件状态、不因切布局编译；源与结果按 path 独立选择 |

可编辑仍仅为一个用户包，普通 `.gox` 名称、最多16文件、总UTF-8 32KiB与现有编译合同不变。WASM 在独立 Worker 中只转译，450ms去抖、10秒编译/30秒加载超时、停止后重建、摘要校验及迟到响应丢弃继续保留。编辑/失败/中断清除旧结果与复制能力；诊断文本保留，并把有可靠 path/line/column 的位置映射为源 model markers，不猜测无映射诊断。

## 路由与内容

采用 BrowserRouter 客户端路由：`/` 介绍、`/rfc.html?feature=<catalog id>`、`/playground.html?example=<id>`、`/local.html`。实体 HTML 仅由同一个 Vite 页面壳生成，供 GitHub Pages 直接打开/刷新；站内通过 Link / History 切换，不整页刷新。Playground 状态保存在页面顶层，离开再返回仍保留草稿与文件选择；显式选其他示例/重置可以重置该示例。不额外做草稿持久化、项目序列化或旧hash路由兼容层。

托管目标是 `goxide-lang.github.io` 根站，Vite `base: '/'`，不是 `/goxide/` 项目子路径。验证真实根站路径及本地同布局模拟；不改 Pages 配置。404仍为真实独立页；未知 feature/example有可读错误，不静默选择另一条。

RFC：左侧较窄编号/名称/状态列表，右侧连续简介、设计/用法、边界与代码；共享 readonly FileView，移除主内容 card/accordion，不重造状态来源。完整项目示例等待 [#23](https://github.com/goxide-lang/goxide/issues/23) 的新 schema 定稿，既有配置快照明确为当前合同，不自创字段。两处暂停文案在同一 PR：收窄首页泛型限制，根 README 不手工写死示例数量。[#26](https://github.com/goxide-lang/goxide/issues/26)未决，不实现远程 run、localhost执行服务、上传或分享。

## 构建与验证

父任务已协调网站 assets 归本任务：Vite 输出 hashed JS/CSS、Monaco editor worker 与第三方 license；build.py 与发布允许列表、静态模拟器、embed HTTP测试同步处理生成子目录。#23不并行提交 website生成清单；本任务不改其CLI/compiler/schema/根Go依赖文件。

先完成一个真实多文件编辑→转译例子，再复用到只读依赖/RFC/快照。Playwright在桌面Chromium且 `chromiumSandbox:true` 下检查：tree/tabs双新建、切文件编辑/undo/view state、readonly键盘输入、diagnostics/错误恢复/停止与过期响应、四种布局、客户端前后退草稿不丢、深链接/刷新/未知参数、Monaco按需加载与workers同源，实际根站assets路径。窄尺寸仅做阅读/排版回归，不据用户聊天设备设计两套编辑器。更新相关QA；使用真实WASM/CLI产物对照；定向build/HTTP/CI，不反复全套native。

独立审核关注删减：没有双套UI或wrapper、重复file store、额外框架/语言服务、错误兼容路由或运行接口；检查model/worker生命周期、readonly写入边界、路由草稿与实际asset清单。完整项目示例未完成前保持Draft。

配置参考：[Monaco官方ESM集成](https://github.com/microsoft/monaco-editor/blob/main/docs/integrate-esm.md)、[Vite官方Pages base规则](https://vite.dev/guide/static-deploy.html#github-pages)。本次只读Pages API已确认站点URL为https://goxide-lang.github.io/、source path为 `/`，未修改该配置。

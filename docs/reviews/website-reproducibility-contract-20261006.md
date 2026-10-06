# 网站验证仓内复现合同（2026-10-06，实施前）

基线为 `cbd872470d72ca7c3d6ac08acbd89f0799b6c8cc`，普通分支
`fix/website-validation-reproducibility`。本片只补交付与验证工具；A04 unary/composite
match 另批实施，不更改编译器语义、公开库 ABI、部署或 GitHub workflow。

## 身份与输入

- 被验证编译器源码固定为 `e36d23dacee64d19b34aac639900c3f5c29e55c2`，完整生产
  inventory 摘要为 `41f51dffc2aa68535a7617824231065d268cab190adeb7e164e239e3b7022848`。
  此 pin 不跟随纯网站、验证工具或文档提交滚动；catalog 不引用包含自身的新提交。
- 网站/验证工具记录自身 HEAD、tree、输入清单及工作区状态。准确源码、工具提交和
  最终文档证据提交分别记账；不得以 compiler pin 冒充本片工具验证的提交。
- 构建继续逐文件核对源码与 pin、完整 inventory 和固定 digest；缺少、增加或改动生产
  源码必须失败。公共 std/serde 精确版本、sum 和无本地 replace 校验保留。
- 旧对照编译器固定为 `fd8380c39876c96441857a737be8e32c322a0467`，从本仓库 Git
  对象物化、编译并记录源码/二进制身份，不能依赖以前任务中的二进制。
- 所需历史对象与明确的提案证据引用须可检查；缺少时清楚失败，不隐式联网或修改远端。
  不依赖任务目录、历史 overlay、绝对 checkout 路径或未跟踪的比较器源文件。

## 实现边界

1. 将已验证的网站来源选择及项目/workspace 快照实现整理入当前树，保留最新 QA 和
   全部 **97 fixture / 10 示例**。修正 catalog pin/digest，生成 FEATURES 索引。
2. Go LICENSE 从实际选用工具链定位；支持标准 GOROOT 与 Homebrew libexec 布局。
   找不到时失败，不硬编码本机版本、不从不相关工具链复制。
3. 提供仓内验证入口、完整 AST 对照工具及有意义的正反控制。输出位置、Go/Node/
   Chromium 与既有 Playwright 可配置；不自动安装工具或下载依赖。缓存是环境前提，
   不能承载未声明的源代码或 oracle。
4. 隔离生成物和运行日志；所有阶段失败保留命令、输出、状态与部分摘要。只有必要阶段
   全部成功才能标记整体通过；不沿用之前任务的成功状态或复用未经身份校验的输出。

## 不可降低的验收

- 七阶段：WASM 构建、web 构建、native 对照、web race、web vet、真实浏览器 QA、
  catalog QA。保留现有 UI/Worker 错误恢复、XSS、移动布局、超时/中断及协议边界。
- 13 个来源诊断在 LF/CRLF 各两次的 **52 次原生观察**；browser 原矩阵不删减。
- 33 组旧/新成功输出分别记录 Go 字节、差异文件和完整 parsed AST；语义标量、子字段
  顺序与 token presence 参与比较，仅忽略数值位置、注释及 parser caches。
  不能将旧有 6 组排版差异重写为字节相等，也不能只比较标识符或粗略 token。
- AST oracle 自验：operator、channel direction、type alias、ellipsis 四个负控，
  可选 trailing comma 正控。语义差异必须失败；字节差异可保留并另证结构/行为。
- 5 组双版本原生运行检查成功退出、stdout/stderr 相同；不声称 33 组全部运行。
- CLI/WASM 逐字节差分与旧/新 compiler AST 对照分开记账；native-only 类型拒绝不
  冒称 WASM 完成最终 Go 类型检查，UI 换行规范化不冒称 UI CRLF 验证。

## 交付门槛

先提交本合同，再实现和独立审查。在独立干净 worktree 对准确工具提交实际执行仓内
入口、完整 Mac `go test -race -count=1 -timeout 30m ./...`、全树 vet 和完整 design
检查；工具新增测试随全树运行。记录工具版本、依赖前提、所有输入 hash、失败与修复。
最终输出一组短命令、无任务目录依赖证据及 A04 下一批边界。纯报告追加不冒称重跑源码。
普通分支 push 后核对本地/fetch/ls-remote；不改 main/tag、不部署、不新增或触发 workflow，
未实际执行的 Linux/托管 CI 不报通过。

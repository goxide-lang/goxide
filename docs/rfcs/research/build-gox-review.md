# build.gox 独立对抗评审

2026-10-04，独立 agent `adversarial_review` 只读检查 [RFC](build-gox-proposed.md)、fixture 和当前基线相关生产源码；未写生产文件，未运行第三方安装代码。以下记录其具体发现和作者修订，不等于设计获用户批准。

| 严重性 | 独立发现 | 已完成修订 |
| --- | --- | --- |
| 中 | 图在 trust gate 之后才 resolve host closure，但授权 subject 需要完整闭包 | 调整为静态解析 target/host 闭包→cycle/capability→快照与摘要→授权；明确编译消费同一快照，避免 live-path 内容换位 |
| 中 | 目录 fixture 实际只 flat iterdir、检测 symlink 存在，结果却称递归和拒绝 | 改为自编递归 inventory；验证 nested content 变化；显式抛出 ValueError 并断言 symlink 拒绝。仍标为 model，不称 OS 安全证明 |
| 中 | “撤销立即失效”未定义 in-flight 并发语义 | 新增 policy epoch，所有新执行/cache 使用查当前策略；publish 和下游 view 绑定前复查；明确请求取消不能保证消灭 unconfined daemon 或回滚副作用 |
| 低 | subject 列举遗漏 compiler/frontend identity | 明确纳入授权 subject，与 host cache 规则一致 |
| 低 | 0.5s partial timeout / 固定 descendant 时序可能因 CI 负载假失败 | partial 文件和 spawn ready marker 握手；独立复核又指出固定 child sleep 可能假阳性，已改为父进程 cancel+Wait 后才写 release，child 读 release 才写 survivor marker；宽松就绪期限，仍只测试合作 fixture 机制 |

评审未发现伪 sandbox 主张；抽查确认 host-only ModuleOptions、fresh temp、PackageFingerprint 与 localEnv 现状描述准确。评审强调：普通 Go 执行、cwd/OUT_DIR、离线环境、路径校验都不能提供整个子进程的 OS 隔离；RFC 已保留这些限制。

修订后的最终结果见 [results.json](../../../tests/research/build_gox/results.json)。独立 reviewer 的复核只覆盖上述修正与文档边界；未实现 production runner，因此不存在 runner 全功能或跨平台安全测试通过结论。

独立复核确认原五项问题已回应，无新增 RFC 阻碍；其补充的 child 完成时间歧义也采用 release 握手修正，并重新运行 fixture。

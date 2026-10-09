# 静态路径与 serde owner 迁移审查（2026-10-09）

对应 issues [#3](https://github.com/goxide-lang/goxide/issues/3) 与
[MVP pre-release 总跟踪 #13](https://github.com/goxide-lang/goxide/issues/13)。
本切片不代表整体 MVP 完成。依赖 std 支持库迁移
[std #2](https://github.com/goxide-lang/std/pull/2)、[goxide #12](https://github.com/goxide-lang/goxide/pull/12)。

## 实施范围

`.gox` 直接切换显式 roots + `use` / `pub use`。静态路径统一 `::`，值成员统一 `.`；
拒绝旧 import、静态点号与未绑定的裸 Serialize/Deserialize。serde core 拥有派生，
JSON 只是格式层。use metadata v3 要求重新生成，不保留旧恢复层。

目录 `mod` 只表示选中的本地子包；普通 Go 文件和互操作保持 Go 规则。生成的
类型卫生保护、原函数身份、变量存储和初始化依赖仍保留。完整协议见
[静态路径合同](../design/static-paths.md)、[依赖配置](../design/dependency-roots.md)。

## 独立审查与修复

审查者使用实现会话之外的实际 CLI 输入发现并复验了以下问题：

- 跨文件公开 namespace 再次 use、分组 metadata 及惰性子项加载。
- 生成别名与其他源文件声明碰撞、namespace 与类型冲突。
- 单文件 mod 错认外部模块、手工 API roots 省略来源后伪装 serde owner。
- 经重导出才发现的本地子包漏编译，及其实际包循环。
- 推断 match 分支内类型静态路径拒绝、旧点号误接受及来源证据丢失。

上述反例已修复并复审关闭。最终固定 13 例中，8 例通过真实生成 Go 构建运行，
5 例非法操作符拒绝并定位原源码；另有 9 个独立生成模块及 5 组遮蔽案例通过。
最终构建和探针运行期间，生产源码摘要未变化。此结论针对已记录范围，不声称穷尽输入。

## 本地验证

真实 Go 1.27.1。按改动和失败案例定向复验，没有本地重复运行全仓 race。
原生 Go 验收源码逐段核对保留；新的 fixture helper 只序列化显式 roots，不转换旧源码。

- macros 全包；matchexpr 全包；enums、propagate、sourcemap 全包通过。
- 静态路径、serde owner、生成 alias、文件隔离、源码位置正反例通过。
- 独立 serde/enum/Default/Clone/Debug/equality 模块、宏/Go-result、项目及 host
  构建脚本验收通过相应定向运行；生成程序另行执行 race/vet。
- 单文件外部恢复及两个引用 metadata 大组最后联跑通过（36.755s）；保留原源码
  use/member 位置断言，覆盖真实 CLI build/run 与原生 Go 消费。
- enum_trial 双模块、T0 联合业务和显式 static_paths 示例已实际运行。

最终 GitHub Actions 与匹配提交的网站/WASM/浏览器证据记录在交付 PR。
本记录写入时仅完成核心与原生验证；旧网站 dist 不是本轮新语义的验证产物。
未合并、发布或部署；合入顺序为 std #2 → goxide #12 → 本切片 PR。

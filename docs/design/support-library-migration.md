# formatting/runtime 迁入 std

本轮对应 [issue #4](https://github.com/goxide-lang/goxide/issues/4)。项目尚未发布，直接切换支持包归属；旧包不保留转发层，使用者须重新生成 Go 产物。

| 职责 | 唯一生成 import |
| --- | --- |
| 格式化后的写入、短写与原始错误 cause | `github.com/goxide-lang/std/formatting` |
| 私有赋值传播帧、panic token 与卫生性的 comparable 约束 | `github.com/goxide-lang/std/runtime` |
| Option/Result 的共享表示与 ABI | `github.com/goxide-lang/std/sum` |
| 格式无关序列化协议 | `github.com/goxide-lang/serde` |
| JSON 数据格式 | `github.com/goxide-lang/serde/json` |

格式字符串解析、静态参数与协议检查、Go 源码生成继续属于 compiler 的
`internal/printing`。std 的 formatting/runtime 只依赖 Go 标准库，不导入
compiler、serde 或用户生成包；serde 不反向依赖 std。生成应用导入这些公共库，
公共库不导入生成应用，因而不会形成跨模块循环。

删除 compiler 原 `formatting/`、`runtime/` 包及其旧 import 入口，不增加 alias
shim 或复制一份支持库。运行时 API 与行为按原实现迁移：参数先求值、每个写入
操作只调用一次 Write、短写归为 io.ErrShortWrite、原错误 cause 可恢复；赋值失败
只消费自身 pending token，源 defer、直接 recover、普通 panic、panic(nil) 与
Goexit 的行为保持原合同。保护 Go 预声明类型绑定的 `runtime.Comparable` 仍保留，
它是卫生约束，不是旧路径兼容层。

单文件 CLI 的 `--runtime` 是选择匹配工具链的支持依赖清单的配置入口。生成模块
从清单选取 std/serde 的真实固定版本和必要来源设置，不再注入 compiler module
的 require/replace，也不带入 Cobra 等 CLI 依赖。已有用户 Go module 继续使用
自己的清单；这一配置不能覆盖正常 Go 的依赖解析或类型身份。

std 先提交并普通推送，compiler 再用 Go 实际解析所得的 pseudo-version/checksum
固定该提交。不得提交临时相邻 checkout 的 replace。网站导出 archives、资源摘要
与只读依赖浏览都从同一清单生成；新产物不能继续展示 compiler 的旧支持包来源。
最终交付记录列出确切提交、独立生成模块验证、浏览器验证和 CI 状态。历史报告的
通过只适用于它们记录的旧版本，不作为本次验收结果。

# vendor 输入物化本地模块：网站同步 · 2026-10-05

编译器固定f1474551cb7f18c5610880b91b54e7367a2a6a2e，生产源digest c03e0b656ede234d0d11f2a8fbeb0a1204534594d70a69737b7d5781fb66b472。整合上一网站16cbda1的ABI源提取/std校验、浏览器ABI断言与naming审计；不混入旧vendor快照。std ce5bccb、enum8/ref3/use2/sum2不变。

vendor仅Go迁移输入，物化普通local-modules，显式local replace；长期vendor构建与vendor独立拆出方案取消。默认local核心和只读来源候选已支持，交互一次选择、remote参数与差异/修改确认仍未实现。--offline只表述已核验的本地材料/缓存，不扩大为用户脚本网络隔离。

本地构建实际运行10单文件、10目录negative及全部工程快照。供应源码快照断言目标无vendor目录，按goxide-import.json映射展示真实go.mod/源码/provenance；原文件与权限核验保持。单模块本地源码编辑后locked输出changed bytes；不再描述为vendor不可变字节拒绝。workspace供应分支真实验证增强宏与字节/权限保留；其本地模块拆出由专项CLI测试验证，不假称该快照单独执行拆出。

首次构建与11文件隐私/命名审计通过，新增provenance移除临时根前缀，无/tmp或/workspace路径泄漏。本地catalog26卡、10live/10negative、快照、390px、同源GET、清单故障和损坏WASM闭锁通过。独立只读审查无blocking。

定向race命令：`go test -race -count=1 -timeout=15m ./cmd/goxide ./internal/project -run 'Test(Supplied|Vendor|OldVendor|NativeWorkspace.*(Supplied|Vendor|CopiedLock)|Naming|CLIProcessContract)'`。最终结果、源提交绑定重建、全浏览器与Pages/12文件hash记录在发布环境localmodules-release.json。上游完整race/vet/design证据见docs/reviews/vendor-local-modules-20261004.md；本轮不声称全树复跑。没有main/tag/release修改。

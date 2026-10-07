# A06 单文件外部恢复：精简交付记录

已知输入 `go.mod` 的单文件 consumer 现在恢复已有外部 enum、具体 sum 及已登记的
Default/Clone/Debug/Serialize/Deserialize/PartialEq/Eq，并按同一依赖图真正 build/run。
单文件与目录入口共享 canonical identity 恢复，仍只选择指定文件；不复制 std 类型、
不新增手写协议签名，不绕过 Go internal/private 可见性或 artifact 校验。

## 提交与范围

| 身份 | 完整提交 |
| --- | --- |
| A04 基线 | `e7c10e9fe7bf3854e7dc119b916b620868638bff` |
| 实施前合同 | `f217324113d30fc88a6e8466afdab780465262cc` |
| 编译器实现 | `2a48b3ddd795922e467d60370869f75577d79364` |
| compiler pin | `ed346d320f584f85ca5fa4f3a4a7ceab526ba38f` |
| 首次完整验证工具 | `81b95e7e86cee2ca498ad07bef8e9d40be99b565` |
| 最终完整验证工具 | `8ba3174bd43481c396e75be5a95aa8e622456271` |

125 个编译器生产输入在实现、pin 和两个工具提交之间逐字节一致。
完整门槛对应 `8ba3174`；之后的精简报告与产物检查提交不冒称当时执行的 HEAD。
摘要、工具版本及日志哈希见[精简验证摘要](single-file-external-recovery-summary.json)。

## 失败、修正与验证

第一次完整运行的 vet、design 和网站七阶段通过，race 失败于旧
`TestSourceMappingRealCompilerCLI` 夹具：为原生 Go 对照创建在输入旁的 `go.mod`
使 CLI 正确选择已知 module 模式，原模块内 binary 输出目标随后触发输出保护。

`8ba3174` 仅修正夹具：将 Go oracle 放到独立 module，保留 standalone 的完整原始
Go 诊断断言；增加 configured 子项，使用外部输出并检查真实错误行、列、完整消息
和两个重复 literal 的准确源位置。生产保护未放宽，第一次失败记录仍在本地保留。

随后在该提交的干净独立检出重新运行全部门槛：

| 门槛 | 实际结果 |
| --- | --- |
| Mac 全树 race | 619 项顶层 / 1451 项子测试通过，零失败、零测试跳过；18 个测试包、5 个无测试包 |
| 全树 vet / design | 通过；design 包含 13 个 Go 机制命令及预期拒绝 |
| 网站 CLI/WASM | 七阶段全部通过；105 fixture / 11 示例，原 97 / 10 输入保持 |
| 旧/新行为对照 | 33 对完整 Go AST 相同，27 对 Go 字节相同；5 对原生程序行为相同 |
| 独立审查 | 实现与最终结果复核无剩余阻断 |

正式命令使用新的外层 Go build cache；个别旧测试有自身缓存配置，不宣称所有嵌套
构建均为冷缓存。仅有 Mac 本机证据，没有运行 Linux/托管 CI 或部署网站。

## 复现入口与证据处理

复用仓内[联合验收](../../tests/acceptance/single_file_external_test.go)、
[源位置验收](../../tests/acceptance/source_mapping_test.go)及
[网站验证入口](../../website/validation/README.md)，不再复制测试源或原始产物。
在具备所需工具、完整固定依赖与可写输出目录的干净检出中运行：

```sh
env -u GOWORK GOTOOLCHAIN=local GOPROXY=off go test -race -count=1 -timeout 30m ./...
env -u GOWORK GOTOOLCHAIN=local GOPROXY=off go vet ./...
python3 -B scripts/check_design.py
python3 -B website/validation/run.py --help
```

网站完整参数按上述 README 配置；帮助命令本身不是验证。输出写仓外或已忽略的
目录，原始日志、截图、大型运行记录不进入本交付。仓内摘要不是自带依赖的离线包。

旧本地分支 `feat/single-file-external-recovery` 的
`fd3e7f74c95c64df636253227b1c07cb916b8ea9` 及全部原始证据保留。本干净分支从
`8ba3174` 建立，不以删除原始记录掩盖首次失败，也不改写已发布祖先中的历史证据。
新增 ignore 与[产物检查](../../scripts/check_review_artifacts.py)共同约束后续入库：
暂存后、提交前使用 `python3 -B scripts/check_review_artifacts.py`，不要整目录 force-add。

本次精简交付另通过 29 项真实 Git 防回归测试、8 项独立对象图检查和完整 design
入口。该检查覆盖强制暂存、先提交再删除、侧分支合并与浅历史边界；不替代前述
准确 `8ba3174` 上的编译器完整门槛，也不宣称能够识别故意伪装的任意数据。

## 保留边界

不可导入 internal-origin 的增强操作、无源分发、通用 trait/声明加载、手写
Clone/Debug/serde、单文件 TOML/workspace target/features、tags/test 等余项继续留账。
浏览器仍只加载随附 export data，不宣称任意 module 加载。公开 std/serde ABI 不变；
本片不代表 A06 或整个 MVP 全部完成。

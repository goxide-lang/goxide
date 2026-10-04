# 宿主构建脚本与 target 布局联合验收

2026-10-04，Linux/amd64，指定 Go 1.27.1（`/workspace/goxide-environment/go/bin/go`），本地临时 fixture。没有执行第三方依赖脚本、安装器或下载工具链。当前范围以 [RFC 0023](../rfcs/0023-build-scripts.md) 为准。

## 历史与整合点

- 研究分支 `research/build-gox-rfc`，提交 `4c54b12290fffd5d593e674a403306a60b3c9708`。完整 [最初RFC](https://github.com/hxsf/goxide/blob/4c54b12290fffd5d593e674a403306a60b3c9708/docs/rfcs/research/build-gox-proposed.md)、[Cargo差距对照](https://github.com/hxsf/goxide/blob/4c54b12290fffd5d593e674a403306a60b3c9708/docs/rfcs/research/build-gox-cargo-gap.md)、[可重复OS机制fixture](https://github.com/hxsf/goxide/tree/4c54b12290fffd5d593e674a403306a60b3c9708/tests/research/build_gox) 独立保留。研究基于最新 macro 分支，不是旧 main。
- 用户确认后，runner 分支 `feat/build-script-generation`，提交 `94621c1`，基于项目工具链 `f1beade68ee6dd0899e4337423998392e3e320cc`。它单独保存了尚未迁移 target 的实现历史。
- target 布局独立分支 `feat/target-layout`，提交 `8b7bd954149b5588ab21432cda824f112f121885`；与 runner 在同一项目基线上开发。
- 最终整合分支 `feat/build-script-target-layout`。真实解决 CLI project 管线、frontend ModuleOptions 和 RFC 0022 冲突；没有覆盖 layout 所有权、lease、修改二进制拒覆盖、旧bundle保留或扫描隔离规则。未 merge 到公共分支，未打 tag。

接口边界：manifest Config 放在不依赖前端的 `internal/buildconfig`，避免 project/frontend/runner 导入环。runner 使用 `Layout.ScriptCache` 的包级空间，跨bin共享；core 完整键负责具体配置隔离。`Unit.ScriptOut`/逐target `ScriptCache` 暂未用于runner执行。信任记录位于用户store，CLI拒绝当前已知源码/helper/产物内的store；不是cache owner或manifest中可提交的授权。

## 独立审阅与修复

独立审阅者未参与生产实现，仅使用自编 `/tmp` fixture；发现后修复并复核：

| 问题 | 修复与回归 |
| --- | --- |
| host lowering内部Go查询脱离timeout/context | ModuleOptions.Context → importer go list/go env；遍历/包边界取消检查；取消测试 |
| 注释/字符串造成helper伪导入循环 | ImportsOnly真实imports、host文件选择；注释互引、关闭文件、pub use回归 |
| 重叠helper module全部前缀匹配造成假环 | 最长匹配，a → b → a/sub真实三helper测试 |
| 生成 `.go.map.json` 被source-map覆盖 | core与frontend保留该后缀，冲突拒绝 |
| monorepo module未绑定祖先Git身份 | 向祖先查找.git，改变remote身份失效，HEAD改变仍稳定 |
| script/helper快照混入自有target | 仅ownership验证成功的布局排除；显式以布局作input拒绝；外来marker拒绝；重复nested target缓存测试 |
| helper目录可被用作授权store | helper根纳入CheckLocation；trust-project/plan/build先拒绝，不创建grants |

限定复核未发现新的明显错误。这不是安全认证；同UID恶意代码、外部副作用、隐式网络/文件读取、动态工具依赖及全部后代进程终止均不受保证。

## 可重复验收

自编测试位置：

- [core tests](../../internal/buildscript/buildscript_test.go)：冻结字节/配置、内容/目录/env/tool键、真实Go/goxide helper、拒绝路径与链接、声明输出、失败/超时/缓存发布、授权回调、clean、host-only。
- [CLI tests](../../cmd/goxide/buildscript_test.go)：schema/embed、两bin共享、feature变化、root/helper/tool/依赖包许可、撤销及更新、超时恢复、clean保留信任/源码。
- [联合布局tests](../../cmd/goxide/buildscript_layout_test.go)：默认target可运行二进制、旧bundle独立重编、CLI/env优先级、跨bin缓存、指定布局clean、修改binary/陌生目录/symlink拒绝、nested target、helper信任位置。
- [布局cache tests](../../internal/project/layout_scripts_test.go)、[信任store tests](../../internal/scripttrust/store_test.go)、[生成前端tests](../../internal/frontend/generated_module_test.go)。

```sh
export GO_BIN=/workspace/goxide-environment/go/bin/go
export GOCACHE=/workspace/goxide-environment/cache
export GOTOOLCHAIN=local
python3 website/build.py
"$GO_BIN" test -race -tags goxideweb ./...
"$GO_BIN" vet -tags goxideweb ./...
python3 scripts/check_design.py --docs-only
```

网页构建只为 `goxideweb` embed 准备本地资源与WASM，不发布站点。干净checkout若直接加goxideweb标签，缺少dist会在setup阶段失败；需先按上述步骤构建。它不代表普通无标签测试依赖网页资源。

专项结果：runner全树普通测试通过（acceptance 263.642s）；联合CLI全脚本race通过（132.890s）；两项联合review回归race通过（9.449s）；core最终race通过（25.2s）。完整联合测试结果记录在下方；这些时长是本机观察，不是性能承诺。实验只在本机host执行，未将Go ELF机制或路径中的host/target字段当作goxide交叉编译支持。

[手动试用示例](../../examples/build_script/README.md) 已复制到临时目录完成 plan→trust→生成Go/embed→cache hit→schema修改rerun→独立target build→clean→rerun验证；信任保留，源码无生成目录。使用的生成器只导入标准库。

最终联合结果：`go test -race -tags goxideweb ./...` 通过（cmd/goxide 204.547s，frontend 51.956s，acceptance 316.768s）；`go vet -tags goxideweb ./...` 通过；网页资源与真实WASM构建通过（5个示例）。随后将信任位置检查扩大到图中所有已知源码节点，联合布局/信任身份回归再跑race通过（31.097s）。文档检查通过：76个Markdown、929个本地链接/锚点、23个RFC结构。没有执行浏览器UI测试；本次没有修改网页交互。

# 宏 + 独立 enum：最小双模块试用

本例从 [t0_joint](../t0_joint/) 的订单、配置和条件日志宏缩减而来：JSON 输入 → 闭合 enum/match → Result `?`/错误链 → 回执。移除了 Budget/读写引用部分，不使用 `&T`、`mut&` 等新指针语法；helper 调用中的 `&value` 是普通 Go 取址。

**生产代码候选：`55ef3b3020b3984a8ae360ca469fc13fee345bd2`。** 本次交付只增补示例和文档，不改变编译器/运行时语义。已实跑 Go **1.27.1 linux/amd64**（race detector 可用）；脚本要求真实 Go 1.27.x，不自动下载工具链。实际 checkout SHA 会由脚本打印。目录/多模块实现是有限子集，不代表整个 MVP 完成。

## 一次跑通

从仓库根目录执行。其他机器请把 `GO_BIN` 改为安装的真实编译器路径；不要仅凭程序名判断，某些环境的 `/usr/bin/go` 是同名游戏。

```sh
export GO_BIN=/workspace/goxide-environment/go/bin/go
work="$(mktemp -d)"
bash examples/enum_trial/try.sh "$work"
```

[try.sh](try.sh) 是完整、已实跑的构建说明。它依次：

1. 用 `go build -o "$work/hgo" ./cmd/hgo` 构建 CLI。
2. 转译独立的 [model 模块](model/go.mod)，保存生成 Go 和协议 metadata。
3. 复制 [app 模块](app/go.mod) 到临时输入目录，将其 `replace` 指向**已生成的 model**，再转译消费者。不会修改仓库的 go.mod。
4. 在两个生成模块执行 `go test -race -count=1 -v ./...`、`go vet ./...`，构建并运行 `orders`。

产物保留在 `$work`；重复尝试请使用新的空目录。生成模块通过本地 `replace` 依赖本仓库的支持库，**不是自包含发布包**。宏在 app 自己的源码包 [logkit](app/logkit/events.hgo) 中导入；此例不声称跨独立模块的预编译宏 ABI 已实现。

默认输出：

```text
accepted 42 units=3 retry=-1 port=8080
Snapshot { Data: Samples { Items: Option::None } }
Snapshot { Data: Samples { Items: Option::Some(nil) } }
Snapshot { Data: Samples { Items: Option::Some([]) } }
equals none/some(nil)=false some(nil)/some(empty)=false
clone source=[1] copy=[9] equals=false
```

`Snapshot` 在消费者派生 Default/Clone/Debug/PartialEq/Eq，其字段协议来自单独生成的 model 模块。`clone source=[1] copy=[9]` 表示修改复制后的 slice 没有改变源值；普通 Go 赋值本身没有改成深复制。None、Some(nil)、Some(empty) 是不同状态，`.equals` 保留其区别。

## 输入正常与错误路径

以下命令均已对上面的实际二进制执行：

```sh
"$work/orders" '{"Name":"7","Retry":null}'
"$work/orders" '{"Name":"7","Retry":2,"Settings":{"Port":90}}'
"$work/orders" '{"Name":"42","Mode":{"Batch":9}}'
"$work/orders" '{"Name":"bad"}'
"$work/orders" '{"Name":"1","Mode":{"Unknown":0}}'
"$work/orders" '{'
```

| 输入 | 可观察结果 |
| --- | --- |
| 缺省 Mode、Retry=null | `accepted 7 units=1 retry=-1 port=8080`，退出 0 |
| Retry=2、显式 Settings | `accepted 7 units=1 retry=2 port=90`，退出 0 |
| Batch=9 | `order rejected: capacity exceeded`，退出 1 |
| Name="bad" | `order rejected: strconv.Atoi: … invalid syntax`，退出 1 |
| Unknown 变体 | `order rejected: UnknownVariant …`，退出 1 |
| 截断 JSON | `order rejected: Syntax: unexpected end of JSON input`，退出 1 |

错误命令预期非零退出；若放入 `set -e` 脚本，应分别处理预期失败。错误写 stderr，不打印成功回执。测试还断言失败不触发成功日志宏，以及 `errors.Is`/`errors.As` 能穿过 Failure 的 cause 链。

普通 Option 的缺失/null→None 属于 serde 协议，与 Default 独立。Settings 的 `#[serde(default)]` 才调用其手写 Default；Secret 的 `#[serde(skip)]` 走已登记 Default。Name 是必需字段，enum 未知变体拒绝，不是开放数字枚举。

## 运行与测试模式

上述脚本已经实跑：双模块目录转译、生成 Go 的普通 build/运行、两个生成模块的 race/vet。也单独实跑了以下目录 `hgo run`：

```sh
PATH="$(dirname "$GO_BIN"):$PATH" GOTOOLCHAIN=local GOWORK=off GOPROXY=off \
    "$work/hgo" run "$work/input"
```

此试用的测试入口是脚本中的**生成 Go 模块测试**。直接对普通模式生成的 model 再执行 `hgo test "$work/input"` 已知会因 race 源码视图不同而拒绝 metadata（实测 `dependency source closure changed: github.com/goxide-lang/serde`）。不要把生成 Go 的 `go test -race` 与重新按 race 视图加载源码的 `hgo test` 混为一个模式，也不要绕过 metadata 检查。切换 build tags/race/平台需一致地重新生成依赖；本例不额外扩展该流程。

## 范围和证据

- 生产代码候选 `55ef3b3…` 已通过全树 race（acceptance 282.018s）、vet、设计检查和独立审查；这是该 SHA 的既有证据，不因本次文档更新虚报一次新的全树运行。
- 本例新增 [main_test.go](app/main_test.go) 通过真实生成模块 race/vet，覆盖正常/错误输入、None/null、Default、闭合变体和跨模块协议。当前模型模块没有自身测试文件，其代码由消费者执行测试覆盖。
- [当前支持 / 已知失败 / 待决 / 后续指针](../../README.md) 是统一入口；[完整验收清单](../../docs/t0-acceptance.md) 保留证据与历史。

仍需用户决定的最小事项：公开 enum 参数是否统一入口验证及非法值行为；blank/embedded 字段的派生/serde 规则；Proposed serde adapters 的入口和缺失策略。试用成功不代表这些已经定案。本阶段交付后暂停，不自动开始后续指针工作。

# 宏 + 独立 enum：最小双模块试用

本例从 [t0_joint](../t0_joint/) 的订单、配置和条件日志宏缩减而来：JSON 输入 → 闭合 enum/match → Result `?`/错误链 → 回执。移除了 Budget/读写引用部分，只读协议方法使用 `&Self`；业务调用中的 `&value` 取得原值的只读引用。

脚本要求真实 Go 1.27.x，不自动下载工具链。它在独立输出目录中复制示例，按 TOML 项目和路径依赖联合编译，再分别构建、测试两个生成的 Go module；源码目录保持不变。

## 一次跑通

从仓库根目录执行。其他机器请把 `GO_BIN` 改为安装的真实编译器路径；不要仅凭程序名判断，某些环境的 `/usr/bin/go` 是同名游戏。

```sh
export GO_BIN="$(command -v go)"
"$GO_BIN" mod download # 先取得仓库固定的公开 std/serde 与工具链依赖。
work="$(mktemp -d)"
bash examples/enum_trial/try.sh "$work"
```

[try.sh](try.sh) 执行完整构建流程：

1. 用 `go run ./scripts/build.go -o "$work/goxide"` 构建带语法 SDK 身份的 CLI。
2. 把 [model](model/) 和 [app](app/) 复制到临时输入目录，写入 TOML 清单；model 声明 `config` 库，app 声明到 model 的 path 依赖。
3. 联合转译 app 与 model 源码，生成两个独立 Go module，通过受管相对 `replace` 关联。
4. 在两个生成模块执行 `go test -race -count=1 -v ./...`、`go vet ./...`，构建并运行 `orders`。

产物保留在 `$work`；重复尝试请使用新的空目录。生成模块通过固定版本依赖公开 std/serde，formatting/runtime helper 也来自 std；生成物不依赖编译器模块。原生 Go 示例目录保留 `goxide.roots.toml` 根声明；脚本中的 TOML 项目通过依赖 alias 访问 model 的库入口，并直接用 `serde::json` 选择真实 Go 子包。app 通过 `mod logkit;` 绑定自身的 [日志宏包](app/logkit/events.gox)，不需要逐包映射。

默认输出：

```text
accepted 42 units=3 retry=-1 port=8080
Snapshot { Data: Samples { Items: Option::None } }
Snapshot { Data: Samples { Items: Option::Some(nil) } }
Snapshot { Data: Samples { Items: Option::Some([]) } }
equals none/some(nil)=false some(nil)/some(empty)=false
clone source=[1] copy=[9] equals=false
```

`Snapshot` 在消费者派生 Default/Clone/Debug/PartialEq/Eq，其字段协议来自单独生成的 model 模块。派生由正式 std/serde 过程宏生成普通 impl；原生 Go 通过作者声明的 `SnapshotOps{}.Initial()`、`Copy()` 和 `Inspect()` 调用。`clone source=[1] copy=[9]` 表示修改复制后的 slice 没有改变源值；普通 Go 赋值本身没有改成深复制。None、Some(nil)、Some(empty) 是不同状态，`.equals` 保留其区别。

## 输入正常与错误路径

可对生成的二进制检查以下正常与错误输入：

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

## 测试范围

脚本在两个生成模块执行真实 `go test -race` 与 `go vet`，并构建运行 app。[main_test.go](app/main_test.go) 覆盖正常与错误输入、错误链、闭合变体、None/null、Default 和跨模块协议。model 当前没有独立测试文件，其代码由消费者测试覆盖。

源级路径和公开边界见 [RFC 0028](../../docs/rfcs/0028-crate-roots.md)，其他能力范围见[状态表](../../docs/mvp-gap-audit.md)。

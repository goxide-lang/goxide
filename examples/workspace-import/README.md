# 迁入原生 Go workspace

此例只使用临时本地 Go 模块；要求真实 Go1.27+ 和本分支构建的 `goxide` 在 PATH。增强作者 workspace 的 feature 示例见 [workspace](../workspace/README.md)。

```sh
fixture=$(mktemp -d)
mkdir -p "$fixture/native/app/cmd/web" "$fixture/native/lib"
cat > "$fixture/native/go.work" <<'WORK'
go 1.27
use (
 ./app
 ./lib
)
WORK
cat > "$fixture/native/app/go.mod" <<'MOD'
module example.org/app
go 1.27
MOD
cat > "$fixture/native/lib/go.mod" <<'MOD'
module example.org/lib
go 1.27
MOD
cat > "$fixture/native/lib/lib.go" <<'GO'
package lib
func Message() string { return "hello workspace" }
GO
cat > "$fixture/native/app/main.go" <<'GO'
package main
import ("fmt"; "example.org/lib")
func main() { fmt.Println(lib.Message()) }
GO
cat > "$fixture/native/app/cmd/web/main.go" <<'GO'
package main
import ("fmt"; "example.org/lib")
func main() { fmt.Println("web:", lib.Message()) }
GO

goxide import-go --workspace --plan --offline "$fixture/native"
goxide import-go --workspace --out "$fixture/migrated" --offline "$fixture/native"
goxide build --workspace --locked --offline "$fixture/migrated"
goxide run -p example.org/app --bin app --locked --offline "$fixture/migrated"
goxide run -p example.org/app --bin web --locked --offline "$fixture/migrated"
goxide test -p example.org/app --bin app --locked --offline "$fixture/migrated"
```

plan 会显示 `example.org/app -> example.org/lib (workspace=true)`。输出根清单使用显式成员与 path 模板，app 成员写 `workspace=true`；原来没有 require 的关系不再依赖 Go workspace 隐式补边。原 native 目录不改变。

有 workspace vendor 的原工程使用同样命令，迁入不运行 `go work vendor`；供应文件与修改字节保留，缺包失败。更多政策、配置范围、外部 replace 和 schema4 限制见 [迁入合同](../../docs/design/workspace-import.md)；实际测试见 [验收](https://github.com/goxide-lang/goxide/blob/43f930c2f0fd516c263f5c71b9205279af836393/docs/reviews/workspace-import-20261004.md)。


## 迁入后渐进增强

以上 fixture 执行后，可把 `members/m1/lib.go` 换成 `lib.gox`，保持 `Message()` 的 Go API；例如：

```goxide
package lib
func Message() string {
 if cfg!(feature("fancy")) { return "enhanced workspace" }
 return "plain workspace"
}
```

在该成员 TOML 增加 `[features] fancy=[]`，app 成员增加：

```toml
[features]
default = ["member1/fancy"]
```

```sh
goxide run -p example.org/app --bin app --offline "$fixture/migrated"
goxide run -p example.org/app --bin app --no-default-features --locked --offline "$fixture/migrated"
```

首次清单改变要普通构建更新锁；随后切换feature不改版本锁。可选成员依赖仍使用原有 `optional=true` / `dep:alias`，不会按import自动启用。构建脚本继续先 `goxide scripts plan MEMBERDIR`；根项目需 `trust-project`，依赖脚本需针对 plan 的 exact subject 使用 `allow --subject`。`revoke` 后已有缓存也不可使用，迁入不会授予权限。

非vendor拆出时，复制所需成员源码，显式展开继承并设置新的 path（例如app的member1指向`../lib`），再把根 `goxide.lock` 复制为app的锁。`goxide run --locked --offline APPDIR` 只消费可达记录，保留锁定版本、恢复app自己的go/godebug/replace；不能把旧work覆盖偷偷带入独立项目。workspace vendor 不可直接按此方式拆出，需显式派生转换合同；工具会拒绝，不会删vendor标记来蒙混通过。

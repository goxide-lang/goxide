# 迁入原生 Go workspace

此例只使用临时本地 Go 模块；要求真实 Go1.27+ 和本分支构建的 `hgo` 在 PATH。增强作者 workspace 的 feature 示例见 [workspace](../workspace/README.md)。

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

hgo import-go --workspace --plan --offline "$fixture/native"
hgo import-go --workspace --out "$fixture/migrated" --offline "$fixture/native"
hgo build --workspace --locked --offline "$fixture/migrated"
hgo run -p example.org/app --bin app --locked --offline "$fixture/migrated"
hgo run -p example.org/app --bin web --locked --offline "$fixture/migrated"
hgo test -p example.org/app --bin app --locked --offline "$fixture/migrated"
```

plan 会显示 `example.org/app -> example.org/lib (workspace=true)`。输出根清单使用显式成员与 path 模板，app 成员写 `workspace=true`；原来没有 require 的关系不再依赖 Go workspace 隐式补边。原 native 目录不改变。

有 workspace vendor 的原工程使用同样命令，迁入不运行 `go work vendor`；供应文件与修改字节保留，缺包失败。更多政策、配置范围、外部 replace 和 schema4 限制见 [迁入合同](../../docs/design/workspace-import.md)；实际测试见 [验收](../../docs/reviews/workspace-import-20261004.md)。

# 静态路径、use 与派生拥有者

本次直接切换 `.gox` 源码语法，不保留旧 import、包点号调用或未绑定裸序列化派生的兼容入口。原生 `.go` 及生成 Go 仍使用 Go 语法。依赖的获取、版本和 Go module 身份仍由既有 Go 图负责；源语言解析不根据目录前缀猜依赖树。

## 绑定与运算符

外部根必须由 [依赖映射](dependency-roots.md)提供，再由源文件 `use` 选择。下例假定 `serde` 映射根包，并显式配置 `json` 子 namespace；package path 的目录关系不会自动提供该子项：

```goxide
package model
use serde as wire;
use serde::Serialize;
use serde::json as json;

#[derive(Serialize, wire::Deserialize)]
type Message struct { Text string }

func RoundTrip(value Message) (Message, error) {
    data, err := json::Marshal(&value, SerializeMessage)
    if err != nil { return Message{}, err }
    return json::Unmarshal(data, DeserializeMessage)
}
```

- `use root;` / `use root as alias;` 引入 namespace；`use root::Item as Local;` 引入项目；`use root::*;` 选择有效公开项。绑定都位于包顶层。
- 私有 `use` 仅对当前 `.gox` 文件可见；`pub use` 对本包各文件及增强消费者公开，且携带 canonical 身份和合法 Go 路由。公开类型、常量通过真实 Go alias 输出，函数和变量直接指向原对象，保持存储、defer/recover 和函数身份。
- 包、类型、宏、派生和关联项用 `::`；值字段和接收者方法用 `.`。例如 `time::Time::Unix(t)` 是类型方法表达式，`t.Unix()` 是接收者调用，`time::Local.String()` 是包变量上的方法调用。
- namespace 与局部值按操作符区分：顶层 `use strings;` 后，函数体内的 `strings := value` 与 `strings::Repeat(strings.Text, 2)` 可共存。类型属于静态命名空间；同名的类型声明与可见 namespace 明确冲突，namespace 别名也不能占用 `int`、`string`、`any` 等预声明类型名，不默默选择错误绑定。局部值仍可正常遮蔽显式选择的普通函数项。
- 别名链按声明身份解析；跨文件公开 namespace 可再次 `use`，私有别名不会由此泄漏。不同来源占同名、宏与 Go 对象类别冲突、alias 环均诊断。
- `use root as _;` 保留具体包的初始化依赖；纯分组没有可执行的初始化身份。未选择的 namespace 子项不会被提前加载。

依赖根只是候选 namespace，不自动成为文件绑定。`fmt::Println` 缺少 `use fmt;` 时不能通过全局白名单获得含义；同样不接受 `.gox` 的 `import "fmt"` 或 `fmt.Println` 作为第二套语法。编译器引入的支持依赖独立于用户 namespace，不能凭生成代码反向给源码授予绑定。

## 本地模块与真实来源

目录模式的 `mod child;` 声明当前包路径下、当前构建视图选中的本地子包，`pub mod child;` 可公开它。不能把外部依赖冒充为 `mod`，也不接受 `mod = go import`。单文件模式不选择其他源包，因此拒绝 `mod`；调用已生成或原生 Go 包可使用明确配置的 `use`。

`goxide.toml` 的 `namespaces` 可精确映射当前 module 的原生 Go 包。path/Git 的 module 身份从所选根清单读取，可选 `module` 只作一致性断言。外部 namespace 则记录其预期 module；实际加载通过 Go resolver 提供的 module 身份核对，不用字符串前缀替代证明。toolchain 根也须证明来自工具链。来源不同的包即使取得同一个 package path，也不能冒充 serde owner。

依赖发现先记录显式选择的包，再按已验证重导出的 namespace 选择实际叶子。本地叶子即使通过多层 facade 才可见，也须先完成编译；未使用的分组子项不产生构建边。循环仍按真实包图拒绝。

## serde 的 canonical owner

`Serialize` 和 `Deserialize` 属于 `github.com/goxide-lang/serde` 根包；JSON 位于 `github.com/goxide-lang/serde/json`，只实现数据格式。可改名 namespace、逐项导入或 `pub use` 派生，但 canonical owner、方向及其协议签名必须一致。API 手工提供 roots 也不能省略真实 serde module 身份证明。

`#[derive(Serialize)]` 只有在显式绑定 `use serde::Serialize;` 后合法。未绑定裸名、`json::Serialize`、错误 root、同名普通类型/函数、缺少相应方向协议均拒绝。Serialize-only 与 Deserialize-only 继续独立；不要求为了编码额外提供解码协议。

其他既有编译器派生保持其当前协议，不在本次扩展完整 trait、泛型用户函数或过程宏。为抵御局部类型遮蔽而生成的卫生 alias 保留；这与删除旧支持路径和兼容转发层无关。

## 产物与入口一致性

use metadata 升至 v3，记录 namespace 树与派生身份；旧版本产物须重新生成，不恢复旧字段或旧语法。metadata 仍校验 marker、选中源码和依赖摘要；普通 Go 消费者使用真实 Go API，不依赖虚拟宏/函数/变量导出表。

CLI、目录编译、单文件 API 与 Playground 共用路径绑定实现。Playground 的显式 roots 和每个依赖的真实来源随构建产物固定，多文件私有 use 不跨文件泄漏；依赖浏览器显示其实际源码和 namespace 路径。跨独立模块的预编译宏发布仍未实现，完整 trait 和内联模块也不属于这次范围。

示例：[static_paths](../../examples/static_paths/README.md)、[reexports](../../examples/reexports/README.md)。验证以相应提交的测试及 CI 为准；历史审查文档中的旧拼写保留为当时的记录。

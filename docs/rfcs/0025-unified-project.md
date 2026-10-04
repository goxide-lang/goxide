# RFC 0025：统一项目清单与 Go 工程迁入

## 状态与范围

状态（2026-10-04）：用户已确认统一模型，首片实现已提交；迁移、锁定、纯 TOML 来源及独立复审通过，最新 std/serde 集成后的完整回归仍在收尾。以下是本片规范及明确限制，不是完整 Cargo 兼容声明。

用户已明确决定：迁入 goxide 的第一步就将 Go 依赖配置迁入 `goxide.toml` / `goxide.lock`。混合项目指 `.go` / `.hgo` 共存，不指两套根依赖配置并存。本篇取代 [TOML 迁移设计](../design/toml-manifest.md) 中长期保留根 go.mod 的兼容模式；未迁入且没有 TOML 的普通 Go/单文件工具行为不变。本轮先实现无损子集，原生 Go 仍负责语法、MVS、下载/checksum 与构建，不重造 resolver。

不自动删除、移动或覆盖用户原文件。迁入采用只读计划与独立 staging 工程验证；用户审阅后可选择迁入后的工程，旧工程完整保留。完整 workspace、发布自动化、跨目标编译、共享 enum ABI、借用/所有权不在本篇范围。

## 源级规则

### 唯一权威与字段

```toml
[package]
format = 2
module = "example.org/app"
go = "1.27"
# toolchain = "go1.27.1" # 保留 Go 建议版本；本工具仍 GOTOOLCHAIN=local

[[go.require]]
module = "example.org/lib"
version = "v1.2.0"
indirect = false

[[go.replace]]
module = "example.org/lib"
path = "../lib"
# version 可限定被替换版本；远端替换改用 with / with_version 两字段

[[go.exclude]]
module = "example.org/bad"
version = "v1.0.0"

[[bin]]
name = "app"
path = "cmd/app"
```

format 2 要求显式 module/go；format 1 清单需显式迁移，不能暗取旁边 go.mod。`[features]`、`[dependencies.alias]`、lib/bin/example/build 保持上一版语义；go.require 是原生 Go 最低版本约束，dependencies 是增强依赖请求，两者均在同一 TOML 内交真实 Go 统一选版。相同 module 的多个要求取 MVS，不按 alias 分裂类型身份。replace/exclude 是根政策，不提升依赖自己的政策。保留indirect标记和原require集合，不将间接依赖误作 hgo feature 请求。

本地作者工程一旦有 TOML，根 go.mod/go.sum 都报双配置迁移诊断。原生 Go 依赖无 TOML 时继续使用自己的 go.mod；带 TOML 的 path/Git 作者依赖从 TOML 合成 module 元数据，不能要求给 readonly checkout 写文件。代理下载的 source 发行物有传输用 go.mod：只有经 Go 校验的版本来源允许这一组合，且传输身份/feature无关原生Go约束须与 TOML 相符；该 go.mod 不是第二个作者配置来源。不能用 manifest 标记让任意本地根绕过双配置检查。

### 无损迁入子集

已实现命令：`hgo import-go --plan --bin app=cmd/app ROOT` 只读输出候选 TOML；`hgo import-go --out NEWDIR --bin app=cmd/app ROOT` 在全新独立目录复制源码并产生 TOML/锁，构建所选目标验证。库显式用 `--lib .`，不默认递归发现所有 main。NEW目录必须不在输入工程或任何现有受管布局内部，必须不存在；锁以最终逻辑根身份计算，不含临时staging路径；发布前丢弃staging的target/.hgo产物，不搬移布局owner/信任缓存，最终配置的本地replace指向原目录（首片采用规范绝对路径保义）。成功前不发布目的目录，不运行程序、go generate 或安装脚本。允许普通 Go 编译/cgo 所需编译器行为，目标环境沿用既有 host 限制。

| go.mod 指令 | 首片行为 |
| --- | --- |
| module、go | 映射 package.module/go，交真实 Go 验证；缺少 go 明确要求补充，不擅自选新语言版本 |
| toolchain | 保留 package.toolchain；GOTOOLCHAIN=local，不擅自下载工具链；Go语言最低版本不可满足时明确失败；toolchain只是建议，不将更高建议擅自变成硬要求 |
| require（含 indirect） | 映射 go.require 数组，保留 direct/indirect |
| replace 本地/版本，含 Old 精确版本限定 | 映射 go.replace；迁移 staging 保持指向同一真实本地目录，首片序列化为规范绝对路径，不改原指向 |
| exclude | 映射 go.exclude |
| module Deprecated 声明、retract、tool、godebug、ignore 及未来未知指令 | 首片明确不支持，整个迁移失败，不能静默丢弃发布/工具/运行时语义 |
| go.work/workspace | 明确不支持；根或有效上级workspace、显式GOWORK均诊断，不暗中当单module导入 |

映射由实际 `go mod edit -json` 读取，额外保留/拒绝尚未支持字段；不以不完整手写行解析器吞掉指令。Go 版本及工具链写法由真实 Go 核验。原 go.sum 是历史校验集合，不能重命名为锁：在 staging 用其作已知校验输入，再由实际解析/下载结果形成锁；冲突 checksum 必须失败。无效或缺少离线缓存也失败，保留源工程不变。

### 锁与目录

`goxide.lock` schema 2 保存实际选版计划、module/sums、Git commit、来源/config摘要及生成 Go 所需校验信息。迁移来源的摘要用于解释 provenance，不把所有历史 go.sum 行当已选择版本。初次生成须实际求解验证；--locked 禁止改变，--offline 只读已缓存来源。旧 hgo.lock 或双锁明确迁移错误，不自动改名，更不能丢旧锁后偷偷重新选 Git branch。旧锁迁移尚无安全可验证路径时要求先保存原工程，在新 staging 重新审阅解析结果。

生成 go.mod/go.sum 只放受管 target 的规划/单元目录，临时 staging 验证中的 Go 元数据也不回写作者根。所有 unit 使用稳定的统一选版图和 feature 并集；bin/example/config 仅隔离 artifact，不给同一 Go package 复制新身份。target、内部迁移暂存/备份目录从源扫描排除；不默认排除任意名为 backup 的用户源码，工具采用明确保留的隐藏目录名。

## 转译示例

```hgo
// 导入后可先原封不动保留 main.go，再逐文件改成 .hgo。
package main
import "fmt"
func main() { fmt.Println("mixed source, one manifest") }
```

```go
// target/.../module/main.go：相同 Go 包身份，生成器不更名业务 module。
package main
import "fmt"
func main() { fmt.Println("mixed source, one manifest") }
```

同目录或目录入口的 go:embed 资源随选中源码闭包保留；文件入口原有 embed 限制不变。混合 Go→逐文件hgo 的过程不再次迁移依赖。host build script 仍先完成图/配置预检再核对信任；新锁或迁移命令不等于授权执行脚本。

## 拒绝与边界

| 白/黑例 | 结果 |
| --- | --- |
| 纯 Go module + explicit lib/bin，无不支持指令 | plan只读；staging构建成功后输出TOML+实际锁，旧文件逐字不变 |
| TOML纯根、两个bin和example、混合源码 | target生成Go metadata；按需构建，共享稳定module身份 |
| 本地/Git依赖只有TOML | target规划代理元数据，不写原来源；边界/锁commit保持 |
| 根同时TOML和go.mod/go.sum | 正常build拒绝；import-go首片只接受尚无TOML的原生Go输入，既有双配置须显式人工整理 |
| 目的目录已有TOML/锁或任何内容 | 不覆盖；失败清理仅本次拥有的暂存目录 |
| 未支持指令/workspace/工具链不满足 | 整体拒绝，源文件和已有输出不变 |
| go.sum存在旧未选版本 | 不把它当锁版本；实际Go MVS与校验结果决定 |
| proxy source只有TOML、没有Go传输module元数据 | 不承诺可发现/go get；需专门生成source发行物 |
| proxy传输go.mod与TOML module/约束冲突 | 拒绝，不能静默选择一方 |
| 冷离线、checksum错误、部分构建失败 | 不发布候选工程，不改变原配置 |

## 双向互操作

原生Go作者依赖可直接消费；goxide作者工程的根不再可直接运行go build，使用hgo生成目标。原生Go消费者使用固定配置生成Go发行物（含标准go.mod/go.sum），无需理解TOML或增强锁。source发行物面向hgo增强构建，仍须满足Go proxy的.mod/zip/module path与checksum合同；作者根和发布目录是不同对象，本轮不承诺自动发布命令。

官方机制依据：[Go module reference](https://go.dev/ref/mod)、[go mod edit](https://go.dev/ref/mod#go-mod-edit)。TOML路径仍相对各自清单，Git URL不得当shell；保持Go internal/DAG、初始化、原生变量/函数身份与现有metadata校验。原生Go的多module/workspace不能靠换清单自动消除包环。

## 验收与未定项

实施顺序：先字段与指令映射/只读plan；再project模块元数据适配、target规划、锁schema；接入path/Git纯作者源、proxy传输一致性与host helper；最后staging迁移和文档/fixtures。测试使用真实Go与本地mockproxy/Git，不访问私人第三方：Go→mixed→hgo、localreplace/版本replace、indirect/MVS、offline/locked、Go checksum、未知指令、workspace、双配置、partialfailure/source不变、发布后第一次offline+locked重建及无staging路径残留、版本/feature/target A-B-A、脚本授权、embed、生成Go、race/vet与独立review。

本篇不要求用户重新批准已决定的单一清单模型。首片拒绝未支持指令属于无损边界，不以“部分迁移成功”交付。若后续确需retract/tool/godebug/workspace语义，另列精确映射与测试后启用；不在首片默默补默认值。

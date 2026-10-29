# RFC 0027：Goxide 正式命名与 pre-1.0 迁移

## 状态与范围

已确认（2026-10-04）。语言展示名 **Goxide**，命令、配置和仓库命名 **goxide**，源文件 **.gox**。基于 27d2e274 的 feat/goxide-naming 分阶段落实；最终阶段移除旧命令、后缀、环境变量与生成符号，不提供永久兼容。pre-1.0 允许破坏旧生成产物，但禁止静默读取错误缓存。当时的 hxsf/goxide 仓库地址及 github.com/goxide-lang/std、github.com/goxide-lang/serde 模块路径不因命名迁移而变。此后仓库已转移到 goxide-lang/goxide，编译器 Go module 与内部 import 也统一为 github.com/goxide-lang/goxide。

## 源级规则

- CLI 为 goxide；源文件 main.gox，规范构建入口 build.gox。普通 Go 源仍支持 .go。
- 旧 .hgo 输入必须改名；目录混合新旧后缀、同名 main.gox/main.hgo 均拒绝，先诊断再生成，不覆盖。
- GOXIDE_TARGET_DIR、GOXIDE_TRUST_DIR 取代旧环境变量。CLI flag 优先于新环境变量；设置任何已移除 HGO_* 变量均明确拒绝，即便同时有新变量或 flag，避免迁移时悄悄忽略。
- 作者配置与锁分别使用 goxide.toml、goxide.lock；作者清单直接按当前字段校验。
- 生成公开前缀 Goxide、内部前缀 __goxide；metadata marker、sidecar、缓存目录同步改名并升级 ABI/schema。删除旧受管 target 后重建；所有生产者、消费者和 std 依赖必须同步重新生成，不能只替换生成 Go 文本。

## 转译示例

```goxide
package main
func main() { println("Goxide") }
```

```go
package main
func main() { println("Goxide") }
```

## 拒绝与边界

最终版本不安装 hgo shim，不接受 .hgo/build.hgo，不回退 HGO_*。例如同时设置 HGO_TARGET_DIR=old 和 GOXIDE_TARGET_DIR=new，报迁移错误，要求移除旧变量。旧生成 ABI/metadata 必须重新生成；锁文件既有来源身份、module path、业务序列化字段不重写。历史提交、v0.0.0-mvp-doc 标签和事实性历史记录保留。workspace vendor 拆出未获授权，不实施。

## 双向互操作

Goxide → Go 仍生成普通 Go；Go → Goxide 通过重新生成的 Goxide* 符号调用。std Option/Result 表示合同升级，正式模块路径和用户的业务 API 不改变；serde 无需品牌相关 API 变更。

## 验收与未定项

待办分阶段：规范/分类 → CLI、发现、宏/cfg、source maps、依赖、构建脚本及 ABI → 文档、示例、帮助、补全、WASM/editor/playground → 独立 review、真实 CLI 和全树 race/vet/design 检查 → push 并核验远端。详见 [迁移清单](../design/goxide-naming.md)。官网 owner 按交付合同同步，不在本任务部署。

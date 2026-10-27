# Goxide for VS Code

通过 `goxide lsp` 提供诊断、悬浮信息、定义跳转、基础补全和文档符号。语言分析在编译器中进行，扩展负责 stdio 连接、编辑器事件和只读生成代码预览。

需要 VS Code 1.91 或更新版本，以及带 `lsp` 子命令的 Goxide。Go 后端使用原版 `gopls`；当前服务端契约以 v0.24.0 验证。不自动安装编译器、Go 或 gopls。

## 安装与启动

在本目录执行：

```sh
npm ci
npm test
npm run package
```

随后在 VS Code 中运行 **Extensions: Install from VSIX**，选择生成的 `goxide-0.0.0.vsix`。打包使用 Node.js 22 或更新版本；扩展运行时使用 VS Code 自带的 Node.js。

将 `goxide` 和 `gopls` 放入扩展宿主的 `PATH`，或设置其可执行文件路径。在 Remote SSH/容器窗口中，应配置远端路径。

- 打开包含 `goxide.toml` 的工作区文件夹，会创建一个项目会话。打开嵌套项目的 `.gox` 时，只检查该文件的祖先目录。
- 目录模式（包括已有 Go module）在文件夹设置中指定 `"goxide.input": "."`。
- 单文件模式指定 `.gox` 路径；未找到项目配置的 `.gox` 也使用单文件会话。关闭最后一个相关文件会回收自动发现的会话。
- 多文件夹工作区为各项目分别启动服务，不向单个服务传入多个工作区根。项目源码树必须互不重叠；重叠的后一个项目会跳过并写入日志。

只在可信、磁盘文件工作区中启动编译器。未保存的编辑由标准增量 LSP 文档同步发送，位置编码为 UTF-16。

## Go 扩展协调

默认只为 `.gox` 注册编辑器能力。启用的 Go/Go Nightly 扩展会触发降级：Goxide 使用 `--gopls off`，保留自身的 `.gox` 源码能力，外部 Go 导航和 Go 后端诊断暂不可用。不会另开第二个 gopls，也不会修改用户的 Go 设置。

需要同项目 `.go` 与 `.gox` 共享投影时：

1. 对 Go 扩展选择 **Disable (Workspace)**。
2. 在该项目的源码文件中运行 **Goxide: Enable Managed Go for This Project**。
3. 如 VS Code 要求重新加载，完成重新加载；也可运行 **Goxide: Restart Language Server**。

该命令只写当前工作区文件夹的 `goxide.enableManagedGo`。只有 `goxide.toml` 项目或通过 `goxide.input` 显式选择的目录会接管 `.go`；单文件会话不接管相邻 Go 文件。普通 Go 项目不会被自动加入 Goxide。需要同时保留另一项目的 Go 扩展时，可使用独立 VS Code 窗口。

## 配置

配置均支持工作区文件夹作用域。路径不经过 shell；相对路径按工作区文件夹解释。

| 设置 | 默认值 | 含义 |
| --- | --- | --- |
| `goxide.enabled` | `true` | 是否启动该文件夹的语言服务 |
| `goxide.executable` | `goxide` | 编译器可执行文件 |
| `goxide.gopls` | `gopls` | 原版 gopls 可执行文件；`off` 禁用 Go 后端 |
| `goxide.input` | `""` | 目录、`goxide.toml` 或单个 `.gox`；留空时按上述规则发现 |
| `goxide.bin` | `""` | 选择项目 binary，与 `example`、`lib` 互斥 |
| `goxide.example` | `""` | 选择项目 example，与 `bin`、`lib` 互斥 |
| `goxide.lib` | `false` | 选择项目 library，与 `bin`、`example` 互斥 |
| `goxide.features` | `[]` | 启用的 feature 名称 |
| `goxide.tags` | `[]` | Go build tags |
| `goxide.noDefaultFeatures` | `false` | 禁用项目默认 features |
| `goxide.target` | `""` | 目标 `GOOS/GOARCH`，如 `linux/amd64` |
| `goxide.enableManagedGo` | `false` | 显式允许受管项目接管 `.go`，仍检查 Go 扩展冲突 |

例如：

```json
{
  "goxide.executable": "../../bin/goxide",
  "goxide.input": ".",
  "goxide.lib": true,
  "goxide.features": ["fast"],
  "goxide.target": "linux/amd64",
  "goxide.enableManagedGo": true
}
```

只有 library 的项目可由服务端自动选择 library；包含多个目标的项目应显式配置 `bin`、`example` 或 `lib`。同时选择多个目标会在启动前报错。

配置变化会先发送 `workspace/didChangeConfiguration`，再重启服务以应用新的 CLI 参数。项目文件的新增、删除、重命名和未打开文件的磁盘变化通过 `workspace/didChangeWatchedFiles` 发送；保存已同步的源码不会额外触发项目重载。事件监听排除项目 `target/`、`.git/` 和 `node_modules/`，不在按键路径扫描文件树。

## 生成代码预览

在源码上运行 **Goxide: Show Generated Go**。结果使用 `goxide-generated:` 虚拟文档，只读，不写回源码或磁盘生成物。重新运行该命令会刷新预览；导航和诊断引用同一 scheme 时也可直接打开。

协议为自定义请求 `goxide/generatedSource`，参数 `{ "uri": "file:///.../source.gox" }`，响应 `{ "uri": "goxide-generated:...", "content": "..." }`。后续请求也可传入返回的预览 URI。无可用生成物时显示错误，不用旧产物冒充当前源码。

服务日志在 **Output → Goxide**。编译器或 gopls 不可用时按服务端诊断处理；修改路径后运行重启命令。

开发校验使用 `npm test`、`npm run check` 和 `npm run package`。测试覆盖参数传递、配置 schema、项目选择器、Go 扩展冲突、文件与配置事件、只读预览请求和会话释放；不替代真实 VS Code 窗口中的交互验收。

在已有 VS Code 的 macOS/Linux 主机可运行 `npm run test:host`。它使用临时用户数据与扩展目录启动真实 VS Code，连接受控 stdio 服务，验证五项能力、UTF-16、Go 文件选择器、预览和关闭/重启流程；不下载 Electron，也不修改用户配置。非默认安装路径通过 `VSCODE_EXECUTABLE` 指向 VS Code 可执行文件。该测试验证客户端协议接线，编译器行为仍由服务端端到端测试覆盖。

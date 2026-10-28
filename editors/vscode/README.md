# Goxide for VS Code

通过 `goxide lsp` 提供诊断、悬浮信息、定义跳转、补全、签名提示、文档符号、引用查找、重命名、格式化和快速修复。重命名会重新分析候选源码、检查生成 Go，并验证原有源码绑定未改变。扩展还提供 Run、Test Explorer 和基于 Delve 的调试入口。

需要 VS Code 1.91 或更新版本、带 `lsp` 和 `dev` 子命令的 Goxide，以及编译器要求的 Go 工具链。Go 后端使用原版 `gopls`；当前服务端契约以 v0.24.0 验证。调试另外需要外部安装的官方 Delve（`dlv`），不依赖 Go 扩展提供调试器。VSIX 不内置 Goxide、gopls 或 Delve。

## 安装与启动

在本目录执行：

```sh
npm ci
npm test
npm run package
```

随后在 VS Code 中运行 **Extensions: Install from VSIX**，选择生成的 `goxide-0.0.0.vsix`。打包使用 Node.js 22 或更新版本；扩展运行时使用 VS Code 自带的 Node.js。

将 `goxide` 和需要调试时使用的 `dlv` 放入扩展宿主的 `PATH`，或设置其可执行文件路径。扩展不会下载这两个工具。在 Remote SSH/容器窗口中，扩展与工具运行在远端宿主，应配置远端路径。

默认 `goxide.gopls` 为 `gopls`，优先复用宿主 `PATH` 中的可执行文件。缺少时显示可取消进度，按[官方安装方式](https://go.dev/gopls/advanced#installing-unreleased-versions)执行 `go install golang.org/x/tools/gopls@v0.24.0`，将二进制保存到扩展宿主 `globalStorage` 的 `tools/gopls-v0.24.0/<platform>-<arch>/`。安装需要宿主 `PATH` 中有 `go`，使用 Go 的正常模块和构建缓存、代理及工具链设置；安装进程通过 `go env GOHOSTOS GOHOSTARCH` 选择 Go 工具链自身的宿主目标，并设置私有 `GOBIN`，支持 Go 与扩展进程使用不同架构的模拟运行组合。不受项目交叉编译目标影响，不修改全局 Go 或 VS Code 配置。Windows 使用 `gopls.exe`，macOS 与 Linux 使用 `gopls`。

自定义 gopls 命令/路径会直接使用，`off` 或启用的 Go 扩展会跳过安装。多个项目共享一次工具准备；失败或取消后保留 Goxide 源码服务，错误见 **Output → Goxide**，运行 **Goxide: Restart Language Server** 可重试。成功安装的固定版本会复用，安装中间产物会清理。

- 打开包含 `goxide.toml` 的工作区文件夹，会创建一个项目会话。打开嵌套项目的 `.gox` 时，只检查该文件的祖先目录。
- 目录模式（包括已有 Go module）在文件夹设置中指定 `"goxide.input": "."`。
- 单文件模式指定 `.gox` 路径；未找到项目配置的 `.gox` 也使用单文件会话。关闭最后一个相关文件会回收自动发现的会话。
- 多文件夹工作区为各项目分别启动服务，不向单个服务传入多个工作区根。项目源码树必须互不重叠；重叠的后一个项目会跳过并写入日志。

只在可信、磁盘文件工作区中启动编译器。未保存的编辑由标准增量 LSP 文档同步发送，位置编码为 UTF-16。

## Go 扩展协调

默认只为 `.gox` 注册语言服务。启用的 Go/Go Nightly 扩展会触发降级：Goxide 使用 `--gopls off`，保留自身的 `.gox` 源码能力，外部 Go 导航与 Go 后端诊断暂不可用；需要原生 Go 引用参与的重命名也不提供。不会另开第二个 gopls，也不会自动禁用 Go 扩展或修改其设置。Run、Test Explorer 和 Goxide 调试入口独立使用编译器与 Delve。

需要同项目 `.go` 与 `.gox` 共享投影时：

1. 对 Go 扩展选择 **Disable (Workspace)**。
2. 在该项目的源码文件中运行 **Goxide: Enable Managed Go for This Project**。
3. 如 VS Code 要求重新加载，完成重新加载；也可运行 **Goxide: Restart Language Server**。

该命令只写当前工作区文件夹的 `goxide.enableManagedGo`。只有 `goxide.toml` 项目或通过 `goxide.input` 显式选择的目录会接管 `.go`；单文件会话不接管相邻 Go 文件。普通 Go 项目不会被自动加入 Goxide。需要同时保留另一项目的 Go 扩展时，可使用独立 VS Code 窗口。

## 配置

配置均支持工作区文件夹作用域。路径不经过 shell；相对路径按工作区文件夹解释。

| 设置 | 默认值 | 含义 |
| --- | --- | --- |
| `goxide.enabled` | `true` | 是否启用该文件夹的 Goxide 服务与开发命令 |
| `goxide.executable` | `goxide` | 编译器可执行文件 |
| `goxide.gopls` | `gopls` | 默认优先用 `PATH`，缺少时私有安装 v0.24.0；也可指定命令/路径，`off` 禁用 Go 后端 |
| `goxide.dlv` | `dlv` | 原版 Delve 可执行文件，用于 Goxide 调试 |
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
  "goxide.bin": "app",
  "goxide.features": ["fast"],
  "goxide.enableManagedGo": true
}
```

只有 library 的项目可由服务端自动选择 library；包含多个目标的项目应显式配置 `bin`、`example` 或 `lib`。同时选择多个目标会在启动前报错。本地运行、测试和调试应选择宿主平台；Test/Debug 会拒绝与宿主不同的 `GOOS/GOARCH`。

配置变化会先发送 `workspace/didChangeConfiguration`，再重启服务以应用新的 CLI 参数。项目文件的新增、删除、重命名和未打开文件的磁盘变化通过 `workspace/didChangeWatchedFiles` 发送；保存已同步的源码不会额外触发项目重载。事件监听排除项目 `target/`、`.git/` 和 `node_modules/`，不在按键路径扫描文件树。

## 运行与测试

在命令面板使用以下入口；`.gox` 编辑器标题栏也有 Run 和 Debug 按钮。

| 命令 | 行为 |
| --- | --- |
| **Goxide: Run** | 为当前项目执行 `goxide run`，输出显示在 VS Code 任务终端 |
| **Goxide: Run Tests** | 发现各项目测试，并将运行结果写入 Test Explorer |
| **Goxide: Debug** | 调试当前项目的 binary 或 example |
| **Goxide: Select Project Target** | 为开发命令选择本次扩展会话使用的 binary、example 或 library |

没有显式配置目标时，开发命令会选择唯一可用目标，或显示目标选择列表。library 可用于测试，不能直接 Run。目标选择只保存在内存中；要同时改变语言服务使用的目标，请设置 `goxide.bin`、`goxide.example` 或 `goxide.lib`。修改这些设置会覆盖之前的会话选择。

Run、测试发现、测试运行和 Debug 会保存所选输入范围内的未保存文档；保存失败会停止操作。Test Explorer 按项目、包、测试分组，发现所选根模块中的普通测试、fuzz 种子测试和带 Output 的可执行示例，不包含依赖模块的测试。可以运行所选测试或整个分组，也可以使用 Debug profile；一次调试一个测试，多选时会要求选择。

每次 Run/Debug 测试前都会读取最新配置并重新发现，再按测试身份匹配原来的选择。更改 features、tags 或目标后，旧叶子不会继续使用旧参数；测试已移除或发现失败时，不会执行旧发现结果。通过、失败、跳过与输出会进入 Test Explorer，可定位的失败链接指向原始 `.gox` 或 `.go` 行。Debug profile 只有收到所选测试的明确 PASS 才显示通过；例如 `TestMain` 未调用 `m.Run` 时，未执行的测试显示为跳过。

测试运行使用私有 Go 生成视图，不把临时生成文件写回源码目录。取消测试会终止其进程树：POSIX 上先发送 SIGTERM，最多等待 1 秒后升级 SIGKILL，并清理父进程退出后仍存活的后台子进程；Windows 使用 `taskkill /T /F`。调试测试的取消通过 VS Code 停止对应调试会话；适配器取消准备与构建，先请求 Delve 断开，再有界清理确认属于该会话的调试进程，覆盖独立进程组中的目标。

## 调试

设置 `goxide.dlv` 或保证 `dlv` 在扩展宿主的 `PATH` 中。Goxide 准备带原始源码行号的私有调试二进制，再转发原版 Delve 的 DAP；适配器由 `goxide dev debug` 启动。可以在 `.gox` 和项目中的 `.go` 文件上设置断点。

以下 `.vscode/launch.json` 示例调试名为 `app` 的 binary：

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug Goxide app",
      "type": "goxide",
      "request": "launch",
      "input": "${workspaceFolder}",
      "bin": "app",
      "args": [],
      "cwd": "${workspaceFolder}"
    }
  ]
}
```

省略 `input` 时使用当前 Goxide 项目。配置可覆盖 `bin`/`example`/`lib`、`features`、`tags`、`noDefaultFeatures`、`target`，并支持 `args`、`cwd`、`env`、`stopOnEntry` 和单次会话的 `dlv` 路径；显式目标优先于会话选择。调试测试通常使用 Test Explorer，也可设置 `"test": true`、`package`（发现结果中的完整包路径）和 `run`（测试名正则）。**Run Without Debugging**（noDebug）仍使用同一 DAP 适配器并需要 Delve；普通 **Goxide: Run** 使用任务终端。

官方 Delve v1.27.2 已通过真实 VS Code noDebug、Test Explorer Debug profile，以及 macOS arm64 和 Linux arm64 目标上的跨包 `.gox` 断点、原始调用栈、单步和局部变量验收。macOS 使用稳定版 VS Code 1.143.0、Go 1.27.1 和隔离的用户数据及扩展目录完成，Developer mode 保持关闭。调试目标需要宿主允许进程调试；扩展不会自动修改系统调试权限。

## 生成代码预览

在源码上运行 **Goxide: Show Generated Go**。结果使用 `goxide-generated:` 虚拟文档，只读，不写回源码或磁盘生成物。重新运行该命令会刷新预览；导航和诊断引用同一 scheme 时也可直接打开。

协议为自定义请求 `goxide/generatedSource`，参数 `{ "uri": "file:///.../source.gox" }`，响应 `{ "uri": "goxide-generated:...", "content": "..." }`。后续请求也可传入返回的预览 URI。无可用生成物时显示错误，不用旧产物冒充当前源码。

服务日志在 **Output → Goxide**。编译器或 gopls 不可用时按服务端诊断处理；修改路径后运行重启命令。

开发校验使用 `npm test`、`npm run check` 和 `npm run package`。测试覆盖参数传递、配置 schema、项目选择器、Go 扩展冲突、gopls 私有安装与失败/取消重试、文件与配置事件、只读预览、会话释放、过期测试选择、进程树取消和调试测试结果；不替代真实 VS Code 窗口中的交互验收。

在已有 VS Code 的 macOS/Linux 主机可运行 `npm run test:host`。它使用临时用户数据与扩展目录启动真实 VS Code，连接受控 stdio 服务，验证五项能力、UTF-16、Go 文件选择器、预览和关闭/重启流程；不下载 Electron，也不修改用户配置。非默认安装路径通过 `VSCODE_EXECUTABLE` 指向 VS Code 可执行文件。该测试验证客户端协议接线，编译器行为仍由服务端端到端测试覆盖。

开发命令的真实 Host 验收使用已构建的编译器和外部 Delve：

```sh
GOXIDE_TEST_COMPILER=/absolute/path/to/goxide \
GOXIDE_TEST_DLV=/absolute/path/to/dlv \
npm run test:development
```

该测试在隔离的 VS Code 窗口中禁用 Go 扩展，实际运行多包程序、发现并执行测试、核对原始 `.gox` 失败行，并通过 Delve noDebug 验证 DAP 与 Debug profile 的结果，包括 `TestMain` 跳过测试的情况。它还通过真实编辑器执行缺失方法修复、签名提示、跨文件引用与重命名、两次格式化，保存后生成 Go 并运行。省略 `GOXIDE_TEST_DLV` 时跳过调试部分；它不验证断点命中。

编辑闭环使用真实 `gopls`，可通过 `GOXIDE_TEST_GOPLS` 指定路径。在允许进程调试的环境中，使用同一组编译器与 Delve 路径执行 `npm run test:debug`，验证跨包源码断点、调用栈路径与行号、单步后的局部变量、程序输出和退出；该测试不使用 noDebug。Host 驱动要求收到实际 suite 的成功标记，超时会清理自己启动的进程组。

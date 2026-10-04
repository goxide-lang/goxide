# build.gox 机制研究 fixture

仅验证自编 Go 程序、OS 机制和明确标注的简单模型，**不是 production runner/sandbox**。详见 [研究 RFC](../../../docs/rfcs/research/build-gox-proposed.md)。

```sh
python3 tests/research/build_gox/run.py
```

默认 Go `/workspace/goxide-environment/go/bin/go`，严格要求 Go 1.27.1；可用 `BUILD_GOX_RESEARCH_GO` 覆盖。需要 Linux/POSIX Python 3、loopback 可用。首次构建采用独立临时 GOCACHE，可能耗时约一分钟。全部源码临时复制后编译，不执行第三方安装/下载脚本，不执行生成的 ARM64 binary。仅自编 child 对自建 loopback server 发请求。unshare/bwrap 若失败原样记录，不提权重试。

每次使用独立临时目录，正常退出或 Python 异常时清理；强制杀死整个 harness 可能留下 `build-gox-research-*` 临时目录。timeout 测试只杀自己新建的进程组，CommandContext 反例的 child 完成一次 marker 写入即退出。

[results.json](results.json) 是本机一次运行的结构化证据；`observed` 是真实机制，`model` 只证明 harness 中给出的模型，`capability` 表示该环境是否能启动隔离工具。结果不证明未来 CLI 有信任门、path 校验、process-tree 隔离或缓存实现。测试 scope 未覆盖完整 hgo 前端交叉编译。

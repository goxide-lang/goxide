# trait 表示与双路径研究

对应 [RFC 0004](../../../docs/rfcs/research/0004-trait-lowering-paths.md)。这是独立机制研究，不实现 goxide、不修改当前 D05/D06，不把基准加入日常文档检查。

```sh
python3 tests/research/trait_paths/run.py \
  --go /absolute/path/to/go \
  --out /tmp/trait-paths-results --count 5 --benchtime 200ms
```

需要 Go 1.27+ 与 Python 3.10+。脚本固定本地工具链、关闭模块下载与外部 workspace/GOFLAGS，复制 `probe` 到临时模块；默认使用该临时目录的缓存，也可用 GOCACHE 指定已有本地缓存。不把 go.mod 写进设计仓库。所有独立输入、命令与结果保存在输出目录及 environment.json 指向的临时模块。

- plain/enhanced：同一组测试，build tag 只改变 marker 注入；检查错误链、HTTP Flush、WriterTo、自动转发绕过自定义 Read、动态身份与尺寸。
- bench：21 个基准，各 5 次，默认每次 200ms；GOMAXPROCS=1，未禁止内联。Local 不含重复适配；Retain 是全局保留每次适配的结果；Copy 每次适配同一 target 后执行真实 io.Copy。字典分静态局部与可更换全局字段，不将两者混称直接调用。
- compiler/dispatch：`-gcflags=-m=2` 与真实多态循环反汇编；保留原始输出，不能只凭 ns/op 猜是否内联。
- publish：一个独立 Go 根项目导入 adapter，分别验证 marker+原依赖（预期缺方法失败）、marker+增强依赖（成功）、本地 wrapper+原依赖（成功）。这不验证标准库 overlay 或完整发布驱动。

[记录结果](results/environment.json)、[原始基准](results/bench.log)、[统计](results/summary.json)是本轮机器证据，不是跨版本保证。一次 summary 使用同一原始日志的中位数/min/max；不去掉慢样本、不做速度比营销。日志中的“escapes”是对应生成形状，不意味着每次调用必然新分配；结合分配基准解释。

使用 `unsafe.Sizeof` 只测表示尺寸，不使用 unsafe.Pointer 或绕过内存权限。Defined 普通类型转换只对实验具体 struct 有效，不证明任意泛型/原生类型都能这样适配。所有微基准目标是极小方法或内存 I/O，不代表网络、磁盘、业务吞吐，也不证明不同可观察行为可以互换。

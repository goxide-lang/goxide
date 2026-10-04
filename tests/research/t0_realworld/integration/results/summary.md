# Optimized benchmarks

Go 1.27.1 linux/amd64, Intel Xeon Platinum 8573C, GOMAXPROCS=1, 200ms × 3, 2026-10-04. Sequential original/direct runs on a shared host; allocation counts are stronger evidence than exact timing ratios. No race or disabled optimization flags. Values are medians; raw samples adjacent.

| Artifact | Case | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| original | BenchmarkBatchSum/hgo/1 | 31.630 | 16 | 2 |
| original | BenchmarkBatchSum/native/1 | 2.971 | 0 | 0 |
| original | BenchmarkBatchSum/hgo/32 | 474.000 | 264 | 33 |
| original | BenchmarkBatchSum/native/32 | 25.700 | 0 | 0 |
| original | BenchmarkBatchSum/hgo/1024 | 14663.000 | 8200 | 1025 |
| original | BenchmarkBatchSum/native/1024 | 781.900 | 0 | 0 |
| original | BenchmarkDispatch/hgo | 3.875 | 0 | 0 |
| original | BenchmarkDispatch/native-same-abi | 1.190 | 0 | 0 |
| direct | BenchmarkBatchSum/hgo/1 | 17.360 | 8 | 1 |
| direct | BenchmarkBatchSum/native/1 | 4.324 | 0 | 0 |
| direct | BenchmarkBatchSum/hgo/32 | 79.970 | 8 | 1 |
| direct | BenchmarkBatchSum/native/32 | 35.070 | 0 | 0 |
| direct | BenchmarkBatchSum/hgo/1024 | 2054.000 | 8 | 1 |
| direct | BenchmarkBatchSum/native/1024 | 806.400 | 0 | 0 |
| direct | BenchmarkDispatch/hgo | 2.409 | 0 | 0 |
| direct | BenchmarkDispatch/native-same-abi | 1.388 | 0 | 0 |

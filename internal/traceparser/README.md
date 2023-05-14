## Benchmark results

```
goos: windows
goarch: amd64
pkg: github.com/getsentry/sentry-go/internal/trace
cpu: 12th Gen Intel(R) Core(TM) i7-12700K
BenchmarkEqualBytes-20          44979196                27.10 ns/op
BenchmarkStringEqual-20         64171465                18.40 ns/op
BenchmarkEqualPrefix-20         39841033                30.62 ns/op
BenchmarkFullParse-20             747458              1512 ns/op              1286 MiB/s            1024 B/op          6 allocs/op
BenchmarkSplitOnly-20            2126996               545.7 ns/op            3564 MiB/s             128 B/op          1 allocs/op
```

## Benchmark results

```
goos: windows
goarch: amd64
pkg: github.com/getsentry/sentry-go/internal/trace
cpu: 12th Gen Intel(R) Core(TM) i7-12700K
BenchmarkEqualBytes-20          44328697                26.13 ns/op
BenchmarkStringEqual-20         64680960                17.56 ns/op
BenchmarkEqualPrefix-20         39894544                28.96 ns/op
BenchmarkFullParse-20             745479              1565 ns/op              1243 MiB/s            1264 B/op         11 allocs/op
BenchmarkSplitOnly-20            1946575               622.8 ns/op            3122 MiB/s             368 B/op          6 allocs/op
```

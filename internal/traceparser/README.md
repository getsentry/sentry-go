## Benchmark results

```
goos: windows
goarch: amd64
pkg: github.com/getsentry/sentry-go/internal/trace
cpu: 12th Gen Intel(R) Core(TM) i7-12700K
BenchmarkEqualBytes-20          42332671                25.99 ns/op
BenchmarkStringEqual-20         70265427                17.02 ns/op
BenchmarkEqualPrefix-20         42128026                30.14 ns/op
BenchmarkFullParse-20             738534              1501 ns/op        1358.56 MB/s        1024 B/op          6 allocs/op
BenchmarkSplitOnly-20            2298318               524.6 ns/op      3886.65 MB/s         128 B/op          1 allocs/op
```

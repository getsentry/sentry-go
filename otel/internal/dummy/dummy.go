package dummy

// This package is intentionally left empty.
// Reason: the otel module currenty requires go>=1.18. All files in the module have '//go:build go1.18' guards, so
// with go1.17 "go test" might fail with the error: "go: warning: "./..." matched no packages; no packages to test".
// As a workaround, we added this empty "dummy" package, which is the only package without the compiler version restrictions,
// so at least the compiler doesn't complain that there are no packages to test.
//
// This file and package can be removed when we drop support for 1.17.

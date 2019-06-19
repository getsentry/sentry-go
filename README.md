<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry SDK for Go

[![Build Status](https://travis-ci.com/getsentry/sentry-go.svg?branch=master)](https://travis-ci.com/getsentry/sentry-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/getsentry/sentry-go)](https://goreportcard.com/report/github.com/getsentry/sentry-go)

`sentry-go` provides a Sentry client implementation for the Go programming language. This is the next line of the Go SDK for [Sentry](https://sentry.io/), intended to replace the `raven-go` package.

> Looking for the old `raven-go` SDK documentation? See the Legacy client section [here](https://docs.sentry.io/clients/go/).
> If you want to start using sentry-go instead, check out the [migration guide](https://docs.sentry.io/platforms/go/migration/).

## Requirements

We verify this package against N-2 recent versions of Go compiler. As of June 2019, those versions are:

* 1.10
* 1.11
* 1.12

## Installation

`sentry-go` can be installed like any other Go library through `go get`:

```bash
$ go get github.com/getsentry/sentry-go
```

Or, if you are already using Go Modules, specify a version number as well:

```bash
$ go get github.com/getsentry/sentry-go@0.1
```

## Configuration

To use `sentry-go`, you’ll need to import the `sentry-go` package and initialize it with the client options that will include your DSN. If you specify the `SENTRY_DSN` environment variable, you can omit this value from options and it will be picked up automatically for you. The release and environment can also be specified in the environment variables `SENTRY_RELEASE` and `SENTRY_ENVIRONMENT` respectively.

More on this in [Configuration](https://docs.sentry.io/platforms/go/config/) section.

```go
package main

import (
    "fmt"
    "os"

    "github.com/getsentry/sentry-go"
)

func main() {
  err := sentry.Init(sentry.ClientOptions{
    Dsn: "___DSN___",
  })

  if err != nil {
    fmt.Printf("Sentry initialization failed: %v\n", err)
  }
  
  f, err := os.Open("filename.ext")
  if err != nil {
    sentry.CaptureException(err)
  }
}
```

For more detailed information about how to get the most out of `sentry-go` there is additional documentation available:

- [Configuration](https://docs.sentry.io/platforms/go/config.md)
- [Error Reporting](https://docs.sentry.io/error-reporting/quickstart.md?platform=go)
- [Enriching Error Data](https://docs.sentry.io/enriching-error-data/context.md?platform=go)
- [Integrations](https://docs.sentry.io/platforms/go/integrations.md)
  - [net/http](https://docs.sentry.io/platforms/go/http.md)
  - [echo](https://docs.sentry.io/platforms/go/echo.md)
  - [gin](https://docs.sentry.io/platforms/go/gin.md)
  - [iris](https://docs.sentry.io/platforms/go/iris.md)
  - [martini](https://docs.sentry.io/platforms/go/martini.md)
  - [negroni](https://docs.sentry.io/platforms/go/negroni.md)

## Resources:

- [Bug Tracker](https://github.com/getsentry/sentry-go/issues)
- [GitHub Project](https://github.com/getsentry/sentry-go)
- [Godocs](https://godoc.org/github.com/getsentry/sentry-go)
- [@getsentry](https://twitter.com/getsentry) on Twitter for updates

## License

Licensed under the BSD license, see `LICENSE`

## Community

Want to join our Sentry's `community-golang` channel, get involved and help us improve the SDK?

Do not hesistate to shoot me up an email at [kamil@sentry.io](mailto:kamil@sentry.io) for Slack invite!
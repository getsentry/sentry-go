# Changelog

## 0.0.1-beta.3

- feat: `Gin` framework support with `sentrygin` package
- feat: `Martini` framework support with `sentrymartini` package
- feat: `Negroni` framework support with `sentrynegroni` package
- feat: Add `Hub.Clone()` for easier frameworks integration
- feat: Return `EventID` from `Recovery` methods
- feat: Add `NewScope` and `NewEvent` functions and use them in the whole codebase
- feat: Add `AddEventProcessor` to the `Client`
- ref: Try to read source files from the root directory, based on the filename as well, to make it work on AWS Lambda
- ref: Remove `gocertifi` dependence and document how to provide your own certificates
- ref: **[breaking]** Remove `Decorate` and `DecorateFunc` methods in favor of `sentryhttp` package
- ref: **[breaking]** Allow for integrations to live on the client, by passing client instance in `SetupOnce` method
- ref: **[breaking]** Remove `GetIntegration` from the `Hub`
- ref: **[breaking]** Remove `GlobalEventProcessors` getter from the public API
- fix: Operate on requests body copy instead of the original

## 0.0.1-beta.2

- feat: Add `AttachStacktrace` client option to include stacktrace for messages
- feat: Add `BufferSize` client option to configure transport buffer size
- feat: Add `SetRequest` method on a `Scope` to control `Request` context data
- feat: Add `FromHTTPRequest` for `Request` type for easier extraction
- ref: Extract `Request` information more accurately
- fix: Attach `ServerName`, `Release`, `Dist`, `Environment` options to the event
- fix: Don't log events dropped due to full transport buffer as sent
- fix: Don't panic and create an appropriate event when called `CaptureException` or `Recover` with `nil` value

## 0.0.1-beta

- Initial release

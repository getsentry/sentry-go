# Changelog

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

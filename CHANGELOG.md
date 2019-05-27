# Changelog

## 0.0.1-beta.2

- feat: Allow for configuring transport buffer size throught `BufferSize` client option
- fix: Don't log events dropped due to full transport buffer as sent
- fix: Don't panic and create an appropriate event when called `CaptureException` or `Recover` with `nil` value

## 0.0.1-beta

- Initial release

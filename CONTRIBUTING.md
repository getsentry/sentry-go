# Contributing to sentry-go

Hey, thank you if you're reading this, we welcome your contribution!

## Sending a Pull Request

Please help us save time when reviewing your PR by following this simple
process:

1. Is your PR a simple typo fix? Read no further, **click that green "Create
   pull request" button**!

2. For more complex PRs that involve behavior changes or new APIs, please
   consider [opening an **issue**][new-issue] describing the problem you're
   trying to solve if there's not one already.

   A PR is often one specific solution to a problem and sometimes talking about
   the problem unfolds new possible solutions. Remember we will be responsible
   for maintaining the changes later.

3. Fixing a bug and changing a behavior? Please add automated tests to prevent
   future regression.

4. Practice writing good commit messages. We have [commit
   guidelines][commit-guide].

5. We have [guidelines for PR submitters][pr-guide]. A short summary:

   - Good PR descriptions are very helpful and most of the time they include
     **why** something is done and why done in this particular way. Also list
     other possible solutions that were considered and discarded.
   - Be your own first reviewer. Make sure your code compiles and passes the
     existing tests.
   - Keep a PR to one change. Unrelated fixes and behavior changes go in a
     follow-up.

[new-issue]: https://github.com/getsentry/sentry-go/issues/new/choose
[commit-guide]: https://develop.sentry.dev/code-review/#commit-guidelines
[pr-guide]: https://develop.sentry.dev/code-review/#guidelines-for-submitters

Please also read through our [SDK Development docs](https://develop.sentry.dev/sdk/).
It contains information about SDK features, expected payloads and best practices for
contributing to Sentry SDKs.

## AI Use

You are welcome to use whatever tools you prefer for making a contribution. However, any changes you propose have to be reviewed and tested by you, a human, before you submit a pull request for the Sentry team to review. Pull requests that we consider low effort will be closed without review. For example, we won't review visibly AI-generated PRs from an agent instructed to look for and "fix" open issues in the repo.

## Security

Do not open public issues or pull requests for known or suspected security vulnerabilities. Follow the private reporting process in our [security policy](.github/SECURITY.md).

## Community

The public-facing channels for support and development of Sentry SDKs can be found on [Discord](https://discord.gg/Ww9hbqr).

## Guidelines

The SDK runs inside its users' applications. The following guidelines apply to every change.

- Preserve host-application safety. The SDK must not panic, and must not block or perform expensive work on the caller's goroutine.
- Treat values passed to the SDK as shared with the caller. Callers may modify them after the call, so copy what the SDK retains. A shallow copy of maps or `any` values is not sufficient.
- Preserve compliance with the [SDK Development docs](https://develop.sentry.dev/sdk/). Where the specification is silent, align with the behavior of other Sentry SDKs before introducing new behavior.
- Be conservative on hot paths. Avoid unnecessary allocations, reflection, serialization, and blocking in the capture, log, and metric paths. Compare benchmarks with `benchstat` when changing them.
- Do not send data that may contain PII unless the user has opted in. When in doubt, omit it.
- Do not emit inaccurate telemetry. If a value cannot be measured correctly, skip it.
- Keep each integration in its own module, modeled on an existing integration, and register it in `go.work` and `.craft.yml`.
- Prefer minimal, concise changes over broad refactors or speculative cleanups.

## Testing

- Write tests with `internal/sentrytest` and exercise the public API.
- Prefer table-driven tests. Add a case to an existing table before writing a new test function.
- Compare values with `cmp.Diff`. Do not use testify in new tests. It is being phased out so that the dependency can be removed.
- For a bug fix, add a failing test that reproduces the bug before changing production code.
- Tests must pass with the race detector enabled, because the SDK is used concurrently.

## Verification

Use `make` to format, lint, vet, and test changes.

## Changelog

[`craft`](https://github.com/getsentry/craft) generates `CHANGELOG.md` from merged PRs, so don't edit it by hand.

- The PR title becomes the changelog entry, and its [conventional commit](https://develop.sentry.dev/engineering-practices/commit-messages/#type) type (`feat:`, `fix:`, `ref:`, ...) decides the section and the version bump. Write the title for users reading the changelog.
- To use different text, add a `### Changelog Entry` section to the PR description. It replaces the title and can hold several bullets. See [custom changelog entries](https://getsentry.github.io/craft/configuration/#custom-changelog-entries-from-pr-descriptions).

## Versioning Policy

This project follows [semver](https://semver.org/), with the following additions:

- All modules in this repository are released together and share the same version number.

- We follow Go's [release policy](https://go.dev/doc/devel/release#policy) and support the two most recent Go releases. Raising the minimum Go version is not considered a breaking change.

- Security fixes, including dependency updates that address a vulnerability, are not considered breaking.

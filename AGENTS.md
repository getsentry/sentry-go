# Go SDK

You MUST read and follow `CONTRIBUTING.md` before starting any task. The rules below apply in addition to it.

## Public API

- Treat everything exported outside `internal/`, in any module, as public API under semantic versioning. Do not break it unless explicitly asked.
- Question every addition to the public API, including requested ones. Additions are easy to make and hard to remove, so export only what users need.
- Prefer the simplest API that leaves room to grow, so that later changes do not have to break it.
- Do not add dependencies to the root module or raise the Go version unless asked.

## Code

- Write comments only for intent, invariants, and non-obvious constraints. Do not add comments that restate the code.
- Keep doc comments to one or two lines that describe what the identifier does.
- Do not add nil checks to tests. A panic already fails the test.

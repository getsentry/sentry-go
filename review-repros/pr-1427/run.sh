#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd -P)
repo=$(git -C "$script_dir" rev-parse --show-toplevel)
base=$(git -C "$repo" rev-parse "${1:-0fdec1ff5455ea9f208eccb57f74d4c9ff747ce5}^{commit}")
# Pin the reviewed SDK commit: HEAD in a draft PR also contains the repro files.
head=$(git -C "$repo" rev-parse "${2:-4fd76f13b03771b0e3daf4e0b0a965a971b99b57}^{commit}")
: "${TMPDIR:?Set TMPDIR to a temporary directory}"
output=$(mktemp -d "$TMPDIR/sentry-pr-1427.XXXXXX")
export GOWORK=off
# Override externally if another Go 1.25+ toolchain is preferred.
export GOTOOLCHAIN=${GOTOOLCHAIN:-go1.25.0}
export REPRO_ITERATIONS=${REPRO_ITERATIONS:-1000}

printf 'Base: %s\nHead: %s\nLogs and exported sources: %s\n' "$base" "$head" "$output"
go version
status=0
for label in base head; do
  if [[ "$label" == base ]]; then
    revision=$base
    tags=legacy
  else
    revision=$head
    tags=
  fi
  sdk="$output/$label/sdk"
  tests="$output/$label/tests"
  mkdir -p "$sdk" "$tests"
  git -C "$repo" archive "$revision" | tar -x -C "$sdk"
  cp "$script_dir/go.mod" "$script_dir/go.sum" "$script_dir/"*_test.go "$tests/"
  (cd "$tests" && go mod edit "-replace=github.com/getsentry/sentry-go=$sdk")
  for test_name in TestFlushDeadline TestImmediateClose; do
    printf '\n=== %s %s ===\n' "$label" "$test_name"
    # Run separately so a shutdown panic cannot hide the flush result.
    set +e
    (cd "$tests" && go test -mod=readonly -tags="$tags" -race -count=1 -timeout 180s -run "^$test_name$" -v .) 2>&1 | tee "$output/$label/$test_name.log"
    result=${PIPESTATUS[0]}
    set -e
    printf '%s %s exit=%s\n' "$label" "$test_name" "$result" | tee -a "$output/summary.txt"
    if [[ "$result" != 0 ]]; then
      status=1
    fi
  done
done
printf '\nSummary: %s/summary.txt\n' "$output"
# A reproduced regression is a failing test, so nonzero is expected on this PR.
exit "$status"

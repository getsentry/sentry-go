## Testing

```bash
$ go test
```

### Watch mode

Use: https://github.com/cespare/reflex

```bash
$ reflex -g '*.go' -d "none" -- sh -c 'printf "\n"; go test'
```

### With data race detection

```bash
$ go test -race
```

### Coverage
```bash
$ go test -race -coverprofile=coverage.txt -covermode=atomic && go tool cover -html coverage.txt
```

## Linting

```bash
$ golangci-lint run
```

## Release

_make sure to use correct `vX.X.X` version format_

1. Update changelog with new version in `vX.X.X` format title and list of changes
2. Commit with `vX.X.X` commit message and push to `master`
3. Let `craft` do the rest

```bash
$ craft prepare vX.X.X
$ craft publish vX.X.X --skip-status-check
```
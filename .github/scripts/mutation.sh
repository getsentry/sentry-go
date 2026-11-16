#!/bin/sh
# Mutation tests the Go code changed since a base ref, one workspace module at a
# time, and lists the mutants on changed lines that no test catches.
#
# Usage: mutation.sh [base-ref] [results-dir]
# With results-dir, each module's gremlins JSON results go to
# results-dir/<module>/gremlins.json, for mutation-report.go.
set -eu
# The exclude patterns below contain glob characters.
set -f

base=${1:-origin/master}
results=${2:-}
root=$(pwd)
case $results in
'' | /*) ;;
*) results=$root/$results ;;
esac

work=$(go work edit -json)
modules=$(jq -er '.Use[].DiskPath | ltrimstr("./")' <<JSON
$work
JSON
)

# gremlins matches diff paths against paths relative to the module it runs in,
# and only marks the first run of changed lines in each hunk, so diff with no
# context to give every change its own hunk.
export GIT_CONFIG_COUNT=2 \
	GIT_CONFIG_KEY_0=diff.relative GIT_CONFIG_VALUE_0=true \
	GIT_CONFIG_KEY_1=diff.context GIT_CONFIG_VALUE_1=0

# Files never mutated, relative to the module. gremlins replaces, rather than
# extends, the exclude-files in .gremlins.yaml when any are passed as flags.
excludes='^\.github/ ^internal/testutils/ ^internal/sentrytest/ (^|/)mocks?\.go$ mock_transport\.go$'

# nested prints the modules nested in module $1, relative to it, one per line.
nested() {
	for other in $modules; do
		if [ "$1" = . ] && [ "$other" != . ]; then
			printf '%s\n' "$other"
		else
			case $other in
			"$1"/*) printf '%s\n' "${other#"$1"/}" ;;
			*) ;;
			esac
		fi
	done
}

for mod in $modules; do
	# A module directory also holds the modules nested in it; leave those to their own run.
	inner=$(nested "$mod")

	set -- "$mod/*.go"
	for n in $inner; do
		set -- "$@" ":(exclude)$mod/$n/"
	done
	# gremlins treats an empty diff as "everything changed".
	changed=0
	git diff --quiet --merge-base "$base" -- "$@" || changed=$?
	case $changed in
	0) continue ;;
	1) ;;
	*) exit "$changed" ;;
	esac

	set --
	for x in $excludes; do
		set -- "$@" --exclude-files "$x"
	done
	for n in $inner; do
		set -- "$@" --exclude-files "^$n/"
	done
	if [ -n "$results" ]; then
		mkdir -p "$results/$mod"
		set -- "$@" --output "$results/$mod/gremlins.json"
	fi

	printf '>>> %s\n' "$mod"
	(cd "$mod" && go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash \
		--config "$root/.gremlins.yaml" --diff "$base" "$@" .)
done

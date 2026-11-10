#!/usr/bin/env bash
# Mutation tests the Go code changed since a base ref, one workspace module at a
# time, and lists the mutants on changed lines that no test catches.
set -euo pipefail

base=${1:-origin/master}
root=$(pwd)
gremlins=(go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --config "$root/.gremlins.yaml" --diff "$base")
modules=$(go work edit -json | jq -r '.Use[].DiskPath | ltrimstr("./")')

# gremlins matches diff paths against paths relative to the module it runs in,
# and only marks the first run of changed lines in each hunk, so diff with no
# context to give every change its own hunk.
export GIT_CONFIG_COUNT=2 \
	GIT_CONFIG_KEY_0=diff.relative GIT_CONFIG_VALUE_0=true \
	GIT_CONFIG_KEY_1=diff.context GIT_CONFIG_VALUE_1=0

for mod in $modules; do
	# A module directory also holds the modules nested in it; leave those to their own run.
	excludes=()
	pathspecs=("$mod/*.go")
	for other in $modules; do
		if [[ $mod == . && $other != . ]]; then
			nested=$other
		elif [[ $other == "$mod"/* ]]; then
			nested=${other#"$mod"/}
		else
			continue
		fi
		excludes+=(--exclude-files "^$nested/")
		pathspecs+=(":(exclude)$mod/$nested/")
	done

	# gremlins treats an empty diff as "everything changed".
	if git diff --quiet --merge-base "$base" -- "${pathspecs[@]}"; then
		continue
	fi
	echo ">>> $mod"
	(cd "$mod" && "${gremlins[@]}" ${excludes[@]+"${excludes[@]}"} .)
done

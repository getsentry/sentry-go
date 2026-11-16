#!/bin/sh
# Turns the gremlins results written by mutation.sh into a Markdown report of
# the surviving mutants, with what each one means for the tests.
#
# Usage: mutation-report.sh <results-dir> <report-file>
# Run from the repository root. GITHUB_REPOSITORY and GITHUB_SHA, when set, turn
# locations into links, and the report is also added to GITHUB_STEP_SUMMARY.
# Exits 0 and leaves no report file when no module changed.
set -eu

results=$1
report=$2
tab=$(printf '\t')

if [ ! -d "$results" ]; then
	exit 0
fi
files=$(find "$results" -name gremlins.json)
if [ -z "$files" ]; then
	exit 0
fi

rows=
killed=0
for f in $files; do
	rel=${f#"$results"/}
	case $rel in
	gremlins.json) prefix= ;;
	*) prefix=${rel%gremlins.json} ;;
	esac
	found=$(jq -r --arg prefix "$prefix" --arg tab "$tab" '
		.files[] | .file_name as $file | .mutations[]
		| select(.status == "LIVED" or .status == "NOT COVERED")
		| [$prefix + $file, .line, .column, .type, .status] | join($tab)' "$f")
	if [ -n "$found" ]; then
		rows="$rows$found
"
	fi
	count=$(jq -er '[.files[].mutations[] | select(.status == "KILLED" or .status == "TIMED OUT")] | length' "$f")
	killed=$((killed + count))
done

# hint prints what a test must do to catch a mutant of type $1.
hint() {
	case $1 in
	CONDITIONALS_BOUNDARY) printf 'Swaps < with <=, or > with >=. Add a case with a value exactly at the boundary.' ;;
	CONDITIONALS_NEGATION) printf 'Negates the comparison. Add a case where the branch taken changes what the test asserts.' ;;
	ARITHMETIC_BASE) printf 'Swaps + with -, or * with /. Assert the exact computed value.' ;;
	INCREMENT_DECREMENT) printf 'Swaps ++ with --. Assert the resulting count or index.' ;;
	INVERT_NEGATIVES) printf 'Drops a unary minus. Assert the sign of the result.' ;;
	INVERT_LOGICAL) printf 'Swaps && with &#124;&#124;. Add a case where only one operand is true.' ;;
	INVERT_LOOPCTRL) printf 'Swaps break with continue. Assert on the iterations after this one.' ;;
	*) printf 'Add a case whose assertion fails under this mutation.' ;;
	esac
}

table=
lived=0
while IFS=$tab read -r path line column type status; do
	[ -n "$path" ] || continue
	lived=$((lived + 1))
	code=$(sed -n "${line}p" "$path")
	code=$(printf '%s' "$code" | sed 's/^[[:space:]]*//; s/|/\\|/g')
	location="$path:$line:$column"
	if [ -n "${GITHUB_REPOSITORY:-}" ] && [ -n "${GITHUB_SHA:-}" ]; then
		location="[$location](https://github.com/$GITHUB_REPOSITORY/blob/$GITHUB_SHA/$path#L$line)"
	fi
	if [ "$status" = "NOT COVERED" ]; then
		todo='No test runs this line. Add one that does and asserts its effect.'
	else
		todo=$(hint "$type")
	fi
	table="$table| $location | \`$code\` | $type | $status | $todo |
"
done <<ROWS
$rows
ROWS

{
	printf '<!-- mutation-report -->\n## Mutation testing\n\n'
	if [ "$lived" -eq 0 ]; then
		printf 'All %s mutants on the changed lines are caught by the tests.\n' "$killed"
	else
		printf '**%s of %s mutants on the changed lines survive.** ' "$lived" $((lived + killed))
		cat <<'TEXT'
Each one is a small change to the code that every test still passes with, so the tests do not pin down that line. Change or add tests until each mutant fails them.

`LIVED`: tests run the line but none assert on what it changes. `NOT COVERED`: no test runs the line.

| Location | Code | Mutation | Status | What the tests need |
| --- | --- | --- | --- | --- |
TEXT
		printf '%s' "$table"
		cat <<'TEXT'

Rerun locally with `make mutation MUTATION_BASE=origin/<base-branch>`.
TEXT
	fi
} >"$report"

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	cat "$report" >>"$GITHUB_STEP_SUMMARY"
fi

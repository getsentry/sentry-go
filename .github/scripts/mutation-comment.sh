#!/bin/sh
# Posts the report from mutation-report.go as a pull request comment, updating
# the earlier one instead of adding another. A clean report is only posted when
# it replaces an earlier report, so a PR without survivors gets no comment.
#
# Usage: mutation-comment.sh <report-file>
# Needs GH_TOKEN, GITHUB_REPOSITORY, and PR_NUMBER.
set -eu

report=$1

if [ ! -f "$report" ]; then
	exit 0
fi

ids=$(gh api --paginate "repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments" \
	--jq '.[] | select(.user.login == "github-actions[bot]" and (.body | startswith("<!-- mutation-report -->"))) | .id')
id=$(printf '%s\n' "$ids" | sed -n 1p)

# mutation-report.go marks reports that list surviving mutants.
survivors=0
grep -qF '<!-- mutation-report:survivors -->' "$report" || survivors=$?
if [ "$survivors" -gt 1 ]; then
	exit "$survivors"
fi

if [ -n "$id" ]; then
	gh api --method PATCH "repos/$GITHUB_REPOSITORY/issues/comments/$id" -F "body=@$report" --silent
elif [ "$survivors" -eq 0 ]; then
	gh api --method POST "repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments" -F "body=@$report" --silent
fi

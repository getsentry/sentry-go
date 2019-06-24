#!/usr/bin/env bash
set -eux

SCRIPT_DIR="$( dirname "$0" )"
cd $SCRIPT_DIR/..

function replace() {
    ! grep "$2" $3
    perl -i -pe "s/$1/$2/g" $3
    grep "$2" $3  # verify that replacement was successful
}

if [ "$#" -eq 1 ]; then
    OLD_VERSION=""
    NEW_VERSION="${1}"
elif [ "$#" -eq 2 ]; then
    OLD_VERSION="${1}"
    NEW_VERSION="${2}"
else
    echo "Illegal number of parameters"
    exit 1
fi

replace "const Version = \"[\w.-]+\"" "const Version = \"$NEW_VERSION\"" ./sentry.go

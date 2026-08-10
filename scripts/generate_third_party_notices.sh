#!/bin/sh
set -eu

output="${1:-THIRD_PARTY_NOTICES.txt}"

{
    echo "TermCall third-party Go module inventory"
    echo "Generated from go.mod/go.sum; see each module for license terms."
    echo
    go list -m -f '{{if not .Main}}{{.Path}} {{.Version}}{{end}}' all | LC_ALL=C sort
} >"$output"

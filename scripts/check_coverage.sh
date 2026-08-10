#!/usr/bin/env sh
set -eu

minimum="${1:-70}"

awk -v minimum="$minimum" '
    /coverage: [0-9.]+% of statements/ {
        coverage = $0
        sub(/^.*coverage: /, "", coverage)
        sub(/%.*/, "", coverage)
        found++
        if ((coverage + 0) < (minimum + 0)) {
            print "coverage below " minimum "%: " $0 > "/dev/stderr"
            failed = 1
        }
    }
    END {
        if (found == 0) {
            print "no package coverage results found" > "/dev/stderr"
            exit 1
        }
        if (failed) {
            exit 1
        }
    }
'

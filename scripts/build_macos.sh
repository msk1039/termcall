#!/bin/sh
set -eu

# Some Nix shells advertise C.UTF-8 even when that locale is unavailable.
# Keep the build subprocesses quiet without changing the user's shell.
export LC_ALL=C

# Nix can put a non-Apple clang first on PATH. Native mediadevices builds need
# Apple's SDK so the linker can resolve system libraries such as libresolv.
if ! command -v xcrun >/dev/null 2>&1; then
    echo "xcrun was not found; install Apple's Xcode Command Line Tools first" >&2
    exit 1
fi

sdkroot="$(xcrun --sdk macosx --show-sdk-path)"
cc="$(xcrun --sdk macosx --find clang)"
output="${1:-termcall}"

exec env SDKROOT="$sdkroot" CC="$cc" go build -o "$output" ./cmd/termcall

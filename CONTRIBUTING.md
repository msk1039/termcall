# Contributing to TermCall

Thank you for helping improve TermCall. Please discuss large behavior or
protocol changes in an issue before investing in an implementation.

## Development setup

Use Go `1.26.5`. On macOS, use the build helper because a Nix-provided compiler
may not find the Apple SDK:

```sh
./scripts/build_macos.sh termcall
```

Before opening a pull request, run:

```sh
go mod tidy -diff
gofmt -w cmd internal
go vet ./...
go test ./...
```

Native media behavior needs manual testing on supported hardware. Linux
cross-builds are receive-only and do not validate camera or microphone capture.

## Pull requests

- Keep a pull request focused on one problem.
- Add or update tests for behavior changes and security regressions.
- Do not add credentials, recordings, IP addresses, generated binaries, or
  machine-specific files.
- Explain user-visible changes and any compatibility impact.
- Follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

By submitting a contribution, you agree that it is licensed under the
Apache-2.0 license in this repository.

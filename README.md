# TermCall

TermCall is an experimental terminal video-call client. It uses WebRTC for
peer-to-peer media and renders video as ANSI/half-block art in the terminal.
The companion `termcall-server` process provides signaling and TURN relay
services for self-hosted deployments.

> [!WARNING]
> TermCall is pre-release software. It is not suitable for emergency,
> production, or confidential communication. The first planned GitHub
> prerelease is `v0.1.0-rc.1`; this repository does not claim a stable release.

## Status and support

- macOS Apple Silicon is the native client release target, including camera and
  microphone support.
- Linux client artifacts are **receive-only** (`CGO_ENABLED=0`); they do not
  provide native camera or microphone capture.
- Windows builds are not verified or published.
- Local voice-activity display is not a measurement yet and remains inactive.
- Rooms are small full-mesh calls (five peers by default), not a conferencing
  service.

Please read [SECURITY.md](SECURITY.md) before reporting a vulnerability, and
[SUPPORT.md](SUPPORT.md) for non-security help.

## Build from source

The client needs Go `1.26.5`, CGO, and a native media toolchain.

### macOS Apple Silicon

Install Xcode Command Line Tools, then build with the provided helper. It
selects Apple's SDK and compiler even when a Nix compiler is first on `PATH`.

```sh
git clone https://github.com/msk1039/termcall.git
cd termcall
./scripts/build_macos.sh termcall
./termcall
```

## Join a call

Start the interactive join form:

```sh
./termcall
```

Or provide a generated room UUID and username directly:

```sh
# First participant creates a room and receives its UUID in the title bar.
./termcall -username alice -room new

# Other participants join that exact UUID.
./termcall -username bob -room YOUR_ROOM_UUID
```

The camera and microphone are off by default. Press `v` to request camera
access, `m` to request microphone access, `s` for stats, `n` to change the
renderer, and `q` to quit.

## Self-host the server

The supported deployment is a Linux host with Docker Engine, a public IPv4
address, and a TLS reverse proxy for signaling. Copy the environment template,
set a high-entropy TURN secret, and start the hardened non-root container:

```sh
cp .env.example .env
# Edit TERMCALL_PUBLIC_IP and TERMCALL_TURN_SECRET in .env.
docker compose up -d --build
```

Open the WebSocket/TURN ports and relay range configured in `.env` in both the
host firewall and cloud firewall. The provided server binds signaling to
loopback by default, so place a TLS reverse proxy in front of it and give
clients a `wss://` URL:

```sh
./termcall -username alice -room new \
  -server wss://call.example.com/ws
```

For a trusted short-lived development or LAN deployment only, clients may use
plaintext WebSockets with explicit opt-in:

```sh
./termcall -username alice -room new \
  -server ws://SERVER_IP:8080/ws \
  -allow-insecure-ws
```

Plaintext WebSockets expose signaling metadata and must not be used on an
untrusted network. TURN authentication and firewall policy still need proper
configuration.

## Releases and verification

Tagged prereleases build these assets in GitHub Actions:

- macOS ARM64 native client archive;
- Linux AMD64 receive-only client archive;
- Linux AMD64 server archive;
- SHA-256 checksums, SPDX SBOM, and generated third-party notice inventory.

The release workflow produces GitHub artifact attestations for the checksum
manifest and publishes `ghcr.io/msk1039/termcall-server` with a digest recorded
in the release assets. Prefer the immutable `@sha256:...` image reference over
a mutable tag.

To verify a downloaded archive, compare it with `CHECKSUMS.txt`; where GitHub
CLI is available, also verify the build attestation:

```sh
gh attestation verify termcall_*.tar.gz \
  --repo msk1039/termcall
```

## Contributing

Contributions are welcome once you have read [CONTRIBUTING.md](CONTRIBUTING.md)
and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). By contributing, you agree that
your contribution is licensed under [Apache-2.0](LICENSE).

## License

Copyright 2026 TermCall contributors. Licensed under Apache-2.0; see
[LICENSE](LICENSE), [NOTICE](NOTICE), and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

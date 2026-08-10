# TermCall Stage 0-3 Hardening Design

## Scope

TermCall remains a terminal WebRTC calling application with a long-running Go
signaling/TURN server and a human-operated CLI client. This hardening pass makes
the existing five-peer mesh safe enough for an open-source prerelease: resolve
known source-provenance issues, protect TURN and signaling, define bounded wire
contracts, repair lifecycle/concurrency faults, and add meaningful automated
verification. The server remains self-hostable. Native camera and microphone
support remains available on supported CGO builds, while CGO-disabled builds are
explicitly receive-only. Accounts, recording, an SFU, large rooms, Kubernetes,
and a web client are explicit non-goals for this pass.

## Architecture

- `cmd/termcall` and `cmd/termcall-server`: composition, flags, signals, and
  process lifecycle only.
- `internal/config`: validated server configuration loaded from flags and the
  environment.
- `internal/protocol`: versioned, bounded signaling and video-frame contracts.
- `internal/signaling`: WebSocket sessions, admission handshake, and room state.
- `internal/turn`: embedded Pion TURN lifecycle, ephemeral authentication,
  quotas, and peer-address policy.
- `internal/rtc`: client mesh state, ICE ordering, and peer ownership.
- `internal/capture` and `internal/playback`: native media adapters behind
  consumer-owned interfaces where tests need substitutes.
- `internal/tui`: presentation and explicit media-consent state.

Import direction is `cmd -> config/signaling/turn/rtc/tui`, with `signaling` and
`rtc` depending on `protocol`. Protocol has no application-package dependencies.
Interfaces remain at the consuming package: `rtc.SignalTransport` abstracts the
signaling client; media-source abstractions live at the call composition boundary.
No public `pkg/` API is introduced.

## Concurrency model

Each WebSocket connection has one reader and one writer. A peer send queue has a
bounded capacity; failure to enqueue a critical signaling message closes that
slow session rather than silently desynchronizing room state. Room membership
changes and empty-room removal are atomic under the room manager. RTC peer fields
are protected by the peer mutex, and WebRTC callbacks never mutate shared peer
fields outside that ownership. A parent context controls client/server goroutines;
shutdown snapshots shared objects under locks and closes them after releasing the
locks.

## Core contracts

```go
const ProtocolVersion = 1

type JoinRequest struct {
    Version  int
    RoomID   string
    Username string
}

type JoinAccepted struct {
    Version     int
    RoomID      string
    PeerID      string
    Peers       []PeerInfo
    ICEServers  []ICEServer
}

type ErrorPayload struct {
    Code    string
    Message string
}

type ServerConfig struct {
    HTTPAddress       string
    PublicIP         net.IP
    TURNPort         uint16
    RelayPortMin     uint16
    RelayPortMax     uint16
    MaxRoomSize      int
    TURNSharedSecret string
    AllowedOrigins   []string
}
```

Signaling messages have a 64 KiB transport limit plus smaller field limits.
Display names are printable, control-free, and at most 32 runes. Room tokens and
peer IDs are canonical UUIDv4 values with 122 random bits. Only the literal room
request `new` creates a server-generated room; an unknown UUID is rejected rather
than created. A join completes only after an explicit `join_accepted` response.

Video frames remain the existing keyframe/delta representation for compatibility
within this prerelease, but raw ANSI fallback is removed. Decoding happens only
after a payload-size check and enforces maximum dimensions, cell count, render
mode, character values, color values, and change count. Invalid packets return an
error without mutating decoder state.

TURN uses Pion's time-windowed REST credentials. The shared secret never crosses
the server boundary. The server rejects expired credentials, enforces allocation
quotas, and denies loopback, link-local, multicast, unspecified, and private peer
addresses by default.

## Alternatives and decisions

- **Frames:** a new binary codec would be smaller, while protobuf would provide
  schema tooling. A bounded version of the current codec is selected to keep this
  pass reviewable; a binary v2 can follow after profiling.
- **TURN:** coturn is operationally mature, but adds another service. Embedded Pion
  remains the default because it already supports REST credentials, quota hooks,
  and permission hooks; the deployment documentation can add coturn later.
- **RTC synchronization:** a single mesh event loop would minimize locks but is a
  large rewrite. This pass makes lock ownership explicit and adds regression/race
  tests, leaving an event-loop refactor for later.
- **Licensing:** GPL licensing would preserve the copied globe. The selected path
  removes the derived implementation/assets and targets Apache-2.0 for the
  independently authored project.

## Dependencies

| Need | Choice | Alternatives | Reason |
| --- | --- | --- | --- |
| WebSocket | `github.com/coder/websocket` | Gorilla, stdlib-only | Maintained continuation of the current API |
| WebRTC/TURN | Pion | external coturn, libwebrtc | Existing Go-native architecture and testability |
| Config | standard library | Viper, envconfig | Small fixed configuration surface |
| Rate limiting | `golang.org/x/time/rate` | custom token bucket, Redis | Audited in-process primitive; distributed quotas are out of scope |

## Risks

1. Native camera/audio behavior differs by OS and cannot be covered by pure-Go CI.
2. TURN permission filtering must not block legitimate public ICE relays.
3. Signaling protocol changes intentionally break compatibility with the unsafe
   client/server pair and therefore require an atomic deployment.
4. WebRTC callbacks can expose ordering and shutdown bugs that unit tests miss.
5. Self-hosters must operate TLS termination, certificate renewal, firewall
   rules, secret rotation, and rollback procedures outside the Go process.

## Test strategy and release gates

- Table-driven unit tests cover config validation, sanitization, room admission,
  atomic cleanup, TURN credentials/policy/quota, and every frame-parser boundary.
- Fuzz tests cover frame decoding and signaling JSON without large allocations or
  panics.
- Integration tests use real in-process WebSockets for accepted/rejected joins,
  peer snapshots, leave behavior, message limits, and shutdown.
- RTC tests use a fake `SignalTransport` for lifecycle/error propagation, replay
  protection, pending-candidate bounds, candidate policy, and frame rate limits.
  Optional deployment tests exercise two-client WSS admission/leave behavior and
  authenticated TURN allocation; full two-peer media and forced-relay calls
  remain manual release gates.
- Required checks are `gofmt`, `go mod tidy -diff`, `go vet`, `go test`, race tests,
  staticcheck, gosec, govulncheck, secret scanning, and native client build jobs.
- Core packages (`protocol`, `ascii`, `signaling`, `config`, and TURN policy/auth)
  target at least 70 percent statement coverage. Hardware adapters are reported
  separately rather than inflating or blocking that target.

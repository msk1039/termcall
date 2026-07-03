# TermCall

TermCall is a terminal video call client. Start it, pick a username and a room, and you're connected to anyone else in that room - video comes through as colored ASCII art in your terminal instead of a normal video window.

It's WebRTC under the hood: a signaling server introduces peers, then audio and video stream directly between them (full mesh, no media server in the middle). Frames get downsampled and drawn with half-block characters and ANSI color, so it works in any truecolor terminal.

The features that set TermCall apart:

- Peer-to-peer video calls rendered as colored ASCII / half-block art, right in the terminal
- Full-mesh WebRTC: audio and video flow directly between peers once they're introduced
- A TUI join screen with a live spinning ASCII globe behind the form
- Three render modes — ASCII, Color256, HalfBlock (HalfBlock by default)
- Live per-peer stats: bandwidth, packet loss, and ping
- Mic and camera toggles, mute state, and voice-activity bars
- One base16 color theme, consistent across the whole UI

## Install

There are a couple of ways to install TermCall.

### Precompiled binaries

Precompiled binaries for released versions are available on the [releases page](https://github.com/msk1039/termcall/releases) of the GitHub repo. Download the binary for your platform, make it executable, and run it:

```bash
chmod +x termcall
./termcall
```

### Building from source

To build TermCall from source you need:

- **Go 1.25+** (the version in `go.mod` or greater).
- **CGO** enabled (required for camera and microphone access via `mediadevices`).

#### macOS

Install the Xcode command line tools for CGO:

```bash
xcode-select --install
```

#### Windows

Building on Windows requires a 64-bit GCC toolchain for CGO.

**Option 1: TDM-GCC (recommended)**
1. Download [TDM-GCC 64-bit](https://jmeubank.github.io/tdm-gcc/download/) — pick the `tdm64` version, not 32-bit.
2. Run the installer and keep the option that adds it to your PATH.
3. Restart your terminal.

**Option 2: MSYS2**
1. Install [MSYS2](https://www.msys2.org/).
2. In the **MSYS2 UCRT64** terminal run:
   ```bash
   pacman -S mingw-w64-ucrt-x86_64-gcc
   ```
3. Add `C:\msys64\ucrt64\bin` to your system PATH and restart your terminal.

> If you hit `cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`, you're on a 32-bit `gcc`. Remove any old 32-bit MinGW from your PATH, install a 64-bit compiler (TDM-GCC 64), and make sure it's first in PATH. Verify with `gcc -v` — it should say `Target: x86_64...`.

Then clone and build:

```bash
git clone https://github.com/msk1039/termcall.git
cd termcall
go mod tidy
go build -o termcall ./cmd/termcall
./termcall
```

Or just run it directly without producing a binary:

```bash
go run ./cmd/termcall
```

## Usage

There are two ways to start a call.

**1. The join form (default).** Run the binary with no flags:

```bash
./termcall
```

You'll get a join screen — type a username, room ID, and server URL (pre-filled with a default), move between fields with ↑/↓, and press enter to join.

**2. Flags.** Skip the form by passing a username and a room. The server URL is optional and defaults to `ws://13.127.137.230:8080/ws`:

```bash
./termcall -username alice -room my-room
./termcall -username alice -room my-room -server ws://localhost:8080/ws
```

If you pass some flags but not both `-username` and `-room`, TermCall will ask you to pass all the arguments and print the usage.

Once you're in a call:

- **[V]** Toggle camera
- **[M]** Toggle microphone
- **[S]** Toggle stats overlay (bandwidth, loss, ping)
- **[N]** Cycle render mode (ASCII → Color256 → HalfBlock; defaults to HalfBlock)
- **[Q]** Quit

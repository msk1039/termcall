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

## Self-hosting the server

TermCall needs a signaling + TURN server to pair peers and relay media when direct connections fail. The `termcall-server` binary runs both in one process, and you can host it on any Linux VPS with a public IP.

### Build the server

On your VPS (or locally, then copy the binary over):

```bash
git clone https://github.com/msk1039/termcall.git
cd termcall
go mod tidy
go build -o termcall-server ./cmd/termcall-server
```

The server has no CGO dependency, so it builds with plain `go build` — no C toolchain required.

### Run with Docker (Linux VMs)

For hosting on a Linux VM, the included Docker setup is the easiest path. It uses `network_mode: host` so the TURN server can bind its full relay port range (49152–65535) directly to the host — no need to map thousands of UDP ports.

> Docker Desktop on macOS/Windows isn't supported here; `network_mode: host` only behaves correctly on Linux.

1. Clone and configure `.env`:
   ```bash
   git clone https://github.com/msk1039/termcall.git
   cd termcall
   cp .env.example .env
   curl -s ifconfig.me   # your server's public IP
   ```
   Edit `.env`: set `TERMCALL_PUBLIC_IP` to the address above and change `TERMCALL_TURN_SECRET` to a random string. The full variable reference is in Configure `.env` below.

2. Open the firewall ports (and the cloud firewall, if any):
   ```bash
   sudo ./scripts/setup_firewall.sh
   ```

3. Build and start the server:
   ```bash
   docker compose up -d --build
   ```

4. Verify it's running:
   ```bash
   docker ps
   docker compose logs
   ```

To secure the WebSocket signaling channel with TLS (recommended for production), see [docs/tls-setup.md](docs/tls-setup.md).

### Configure `.env`

Copy the template and set your public IP:

```bash
cp .env.example .env
curl -s ifconfig.me   # your server's public IP
```

Edit `.env` and set `TERMCALL_PUBLIC_IP` to that address. Only that field is required — ports, the relay range, max room size, and the TURN secret all have defaults you can leave as-is or tweak.

### Open the firewall ports

By default the server uses a WebSocket port (`8080/tcp`), a TURN port (`3478/tcp+udp`), and a UDP relay range (`49152-65535/udp`). Open them with the included script, which reads your `.env`:

```bash
sudo ./scripts/setup_firewall.sh
```

> If your VPS has a cloud firewall too (AWS Security Groups, Hetzner, DigitalOcean, etc.), open the same ports there — the script only configures the OS-level firewall.

> On restrictive networks (some mobile carriers, corporate Wi-Fi), TURN on port 443 reaches clients that block 3478. Set `TERMCALL_TURN_PORT=443` in `.env` if you run into that.

### Run the server

```bash
./termcall-server
```

The startup log lists the WebSocket port, TURN port, relay range, public IP, and max room size. Run it under `tmux` or `systemd` so it stays up.

### Point clients at it

Enter the server URL in the join form, or pass it with `-server`:

```bash
./termcall -username alice -room my-room -server ws://YOUR.VPS.IP:8080/ws
```

Anyone who joins the same room on your server gets connected.

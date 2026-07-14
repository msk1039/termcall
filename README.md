# TermCall

TermCall is a terminal video call client. Start it, pick a username and a room, and you're connected to anyone else in that room - video comes through as colored ASCII art in your terminal instead of a normal video window.

It's WebRTC under the hood: a signaling server introduces peers, then audio and video stream directly between them (full mesh, no media server in the middle). Frames get downsampled and drawn with half-block characters and ANSI color, so it works in any truecolor terminal.

https://github.com/user-attachments/assets/e4fcb05c-7089-4a93-8465-0f8415fcb790

## Table of Contents

- [Features](#features)
- [Install](#install)
  - [Precompiled binaries](#precompiled-binaries)
  - [Building the client from source](#building-the-client-from-source)
- [Usage](#usage)
- [Self-hosting the server](#self-hosting-the-server)
  - [Run with Docker (Linux VMs)](#run-with-docker-linux-vms)
  - [Configure `.env`](#configure-env)
  - [Open the firewall ports](#open-the-firewall-ports)
  - [Point clients at it](#point-clients-at-it)

## Features

- Reliable NAT traversal: when direct peer-to-peer connections fail (symmetric NATs, restrictive firewalls, hotel/corporate Wi-Fi), a bundled self-hostable TURN server relays packets so calls still connect when peers can't reach each other directly
- A self-hostable backend server: `termcall-server` runs the signaling server and TURN relay in one process — host it yourself on any Linux VPS with a single `docker compose up -d` command, no C toolchain or manual binary build required
- Public test server available: skip self-hosting and try TermCall right away at `ws://termcall-server.m4yank.com:8080/ws` — the maintainer runs a live instance for anyone to join a room and see it work
- Peer-to-peer video calls rendered as colored ASCII / half-block art, right in the terminal
- Full-mesh WebRTC: audio and video flow directly between peers once they're introduced
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

### Building the client from source

To build TermCall from source you need:

- **Go 1.25+** (the version in `go.mod` or greater).
- **CGO** enabled (required for camera and microphone access via `mediadevices`).
- A working C compiler toolchain — see the platform-specific guide below.

<details>
<summary>macOS</summary>

**Prerequisites**

- **Xcode Command Line Tools** — provides the C compiler CGO links against. Install via `xcode-select --install` from the Terminal, or from Apple's developer downloads.

**Build and run**

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

</details>

<details>
<summary>Windows</summary>

**Prerequisites**

You need a 64-bit GCC toolchain for CGO. Pick one of the following:

- **TDM-GCC 64-bit** (recommended) — download the `tdm64` installer from https://jmeubank.github.io/tdm-gcc/download/ (not the 32-bit version), run it, and keep the option that adds it to your PATH.
- **MSYS2** — install from https://www.msys2.org/, then in the **MSYS2 UCRT64** terminal run:
  ```bash
  pacman -S mingw-w64-ucrt-x86_64-gcc
  ```
  and add `C:\msys64\ucrt64\bin` to your system PATH.

Verify with `gcc -v` — the `Target:` line should say `x86_64...`.

> If you hit `cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`, you're on a 32-bit `gcc`. Remove any old 32-bit MinGW from your PATH, install a 64-bit compiler (TDM-GCC 64), and make sure it's first in PATH.

**Build and run**

From PowerShell (the env vars are required so `go build` uses CGO and targets amd64):

```powershell
git clone https://github.com/msk1039/termcall.git
cd termcall
$env:GOARCH="amd64"
$env:CGO_ENABLED=1
go mod tidy
go build -o termcall.exe ./cmd/termcall
./termcall.exe
```

</details>

## Usage

There are two ways to start a call.

**1. The join form (default).** Run the binary with no flags:

```bash
./termcall
```

You'll get a join screen — type a username, room ID, and server URL (pre-filled with a default), move between fields with ↑/↓, and press enter to join.
<img width="1440" height="873" alt="Screenshot 2026-07-15 at 2 32 28 AM" src="https://github.com/user-attachments/assets/088ff2d4-c0fd-455b-89be-7407a1d8cbb8" />


**2. Flags.** Skip the form by passing a username, room name and backend server url

```bash
./termcall -username alice -room my-room
./termcall -username alice -room my-room -server ws://localhost:8080/ws
```

Once you're in a call:

- **[V]** Toggle camera
- **[M]** Toggle microphone
- **[S]** Toggle stats overlay (bandwidth, loss, ping)
- **[N]** Cycle render mode (ASCII → Color256 → HalfBlock; defaults to HalfBlock)
- **[Q]** Quit

## Self-hosting the server

TermCall needs a signaling + TURN server to pair peers and relay media when direct connections fail. The `termcall-server` binary runs both in one process, and you can host it on any Linux VPS with a public IP.

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
   Edit `.env`: set `TERMCALL_PUBLIC_IP` to the address above and change `TERMCALL_TURN_SECRET` to a random string. The full variable reference is in [Configure `.env`](#configure-env) below.

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

### Point clients at it

Enter the server URL in the join form, or pass it with `-server`:

```bash
./termcall -username alice -room my-room -server ws://YOUR.VPS.IP:8080/ws
```

Anyone who joins the same room on your server gets connected.

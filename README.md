# TermCall

A fully-featured WebRTC Video Calling App that runs directly inside your terminal! Built with Go, Pion, and Bubbletea.

## Prerequisites

- **Go 1.20+** must be installed.
- **CGO** must be enabled (required for camera/microphone access via `mediadevices`).

### macOS

You need to have Xcode command line tools installed for CGO:
```bash
xcode-select --install
```

### Windows

Building on Windows requires a **64-bit GCC compiler toolchain** for CGO. 

**Option 1: TDM-GCC (Recommended & Easiest)**
1. Download [TDM-GCC 64-bit](https://jmeubank.github.io/tdm-gcc/download/). Make sure you pick the `tdm64` version, not the 32-bit version!
2. Run the installer and keep the option checked that adds it to your system PATH.
3. Restart your terminal / PowerShell.

**Option 2: MSYS2**
1. Download and install [MSYS2](https://www.msys2.org/).
2. Open the **MSYS2 UCRT64** terminal and run:
   ```bash
   pacman -S mingw-w64-ucrt-x86_64-gcc
   ```
3. Add the MSYS2 `C:\msys64\ucrt64\bin` folder to your Windows system PATH.
4. Restart your terminal.

> **Troubleshooting `cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`**: 
> If you get this error, your system is using a 32-bit version of `gcc`. You must remove any old 32-bit MinGW installations from your Environment Variables (System PATH), install a 64-bit compiler (like TDM-GCC 64), and ensure it is the first `gcc` in your PATH. You can verify your compiler by running `gcc -v`. It should say `Target: x86_64...`.

## Installation & Running

1. Clone the repository and navigate into it.
2. Download dependencies:
   ```bash
   go mod tidy
   ```
3. Run the application:
   ```bash
   go run ./cmd/termcall
   ```

### Command Line Arguments

If you want to skip the join form and jump straight into a room:
```bash
go run ./cmd/termcall -room "my-secret-room" -username "Alice"
```

## Usage
- **[V]** Toggle Video
- **[M]** Toggle Microphone
- **[S]** Toggle Nerd Stats Overlay
- **[N]** Cycle Render Modes (ASCII -> Color256 -> HalfBlock)
- **[T]** Cycle UI Themes (Ayu, Catppuccin, Midnight, Daylight)
- **[Q]** Quit

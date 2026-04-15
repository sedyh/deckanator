# Deckanator

The only controller-friendly Minecraft launcher for Steam Deck.

<img width="1392" height="944" alt="Screenshot 2026-04-15 at 06 06 10" src="https://github.com/user-attachments/assets/dcd30f6c-7f7a-4180-844c-7c2d6fc5309e" />

## Install on Steam Deck

Open a terminal in Desktop Mode (Konsole) and run:

```bash
curl -fsSL https://github.com/sedyh/deckanator/releases/latest/download/install.sh | bash
```

Or install a specific version:

```bash
curl -fsSL https://github.com/sedyh/deckanator/releases/download/v1.0.0/install.sh | bash -s v1.0.0
```

After install, add to Steam: **Games -> Add a Non-Steam Game -> Browse** and select `~/.local/share/deckanator/Deckanator`.

## Update

Re-run the install command - it overwrites the existing binary.

## Build

Requires [Go](https://go.dev) and [Wails](https://wails.io).

```bash
# Install tools
go install tool

# macOS
task build:mac

# Linux / Steam Deck
task build:linux
```

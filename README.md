# Deckanator

A controller-friendly Minecraft launcher for Steam Deck.

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

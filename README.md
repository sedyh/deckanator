# Deckanator

The only controller-friendly Minecraft launcher for Steam Deck.

<img width="1392" height="944" alt="Screenshot 2026-04-15 at 06 06 10" src="https://github.com/user-attachments/assets/dcd30f6c-7f7a-4180-844c-7c2d6fc5309e" />

## Install on Steam Deck

Open a terminal in Desktop Mode (Konsole) and run:

```bash
curl -fsSL https://github.com/sedyh/deckanator/releases/latest/download/install.sh | bash
```

The installer will:
- Download and install the Deckanator binary to `~/.local/share/deckanator/`
- Create a `.desktop` entry
- Add Deckanator to Steam with artwork (shortcuts.vdf)

Restart Steam after install to see Deckanator in your library.

## Update

Re-run the install command - existing Steam artwork is preserved.

## Steam Artwork

Artwork files are in `cmd/installer/assets/`:

| File | Size | Usage |
|------|------|-------|
| `grid.png` | 460x215 | Library horizontal capsule |
| `poster.png` | 600x900 | Library vertical capsule |
| `hero.png` | 1920x620 | Game detail hero banner |
| `icon.png` | any | Icon / logo |

Replace these files before building to use custom artwork.

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

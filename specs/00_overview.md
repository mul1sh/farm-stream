# 00 — Project Overview & Architecture

## Purpose

`peerflix-go` is a full-feature Go port of [peerflix](https://github.com/mafintosh/peerflix) — a streaming torrent client that allows immediate media playback from magnet links or `.torrent` files by downloading pieces sequentially and serving them over a local HTTP server.

## Goals

1. **Feature parity** with the original Node.js peerflix (v0.39.0)
2. **Single static binary** — no runtime dependencies (Node.js, npm, etc.)
3. **Cross-platform** — macOS, Linux, Windows
4. **Idiomatic Go** — leverage Go's concurrency model, stdlib HTTP, and strong typing
5. **Extensible** — clean interfaces that allow embedding as a library

## Non-Goals

- GUI / web UI (out of scope for v1; the HTTP stats endpoint enables future UIs)
- AirPlay support (low priority, macOS-only, complex protocol)
- Windows registry lookups for player paths (use PATH lookup instead)

## High-Level Architecture

```
┌─────────────────────────────────────────────────────┐
│                   CLI (main.go)                     │
│          Flag parsing, orchestration, signals       │
├──────────┬──────────┬───────────┬───────────────────┤
│          │          │           │                    │
│  Engine  │  Server  │  Player   │  TUI              │
│          │          │           │                    │
│ Wraps    │ HTTP     │ Launches  │ Live terminal     │
│ anacrolix│ streaming│ VLC/mpv/  │ dashboard with    │
│ /torrent │ with     │ IINA etc  │ speed, peers,     │
│          │ Range    │           │ progress           │
│          │ support  │           │                    │
├──────────┴──────────┴───────────┴───────────────────┤
│              anacrolix/torrent (BitTorrent)          │
└─────────────────────────────────────────────────────┘
```

## Component Map

| Component | File(s) | Spec | Description |
|-----------|---------|------|-------------|
| CLI | `main.go` | `02_cli.md` | Entry point, flag parsing, orchestration |
| Engine | `engine.go` | `01_torrent_engine.md` | Torrent client wrapper |
| HTTP Server | `server.go` | `03_http_server.md` | Streaming server with Range support |
| Player | `player.go` | `04_player_integration.md` | Media player auto-launch |
| TUI | `tui.go` | `05_tui_dashboard.md` | Terminal dashboard |
| File Selection | (in `engine.go`) | `06_file_selection.md` | File listing, selection, filtering |
| Stats | (in `server.go` + `tui.go`) | `07_stats_reporting.md` | Swarm stats, JSON endpoint |
| Blocklist | `blocklist.go` | `08_blocklist.md` | Peer blocklist parsing |
| Cleanup | (in `main.go`) | `09_cleanup_exit.md` | Signal handling, temp file removal |
| Config | `config.go` | `10_configuration.md` | Defaults, rc file, env vars |

## Dependencies

| Dependency | Purpose | Replaces (Node.js) |
|---|---|---|
| `github.com/anacrolix/torrent` | BitTorrent protocol, peer management, piece downloading | `torrent-stream` |
| `github.com/spf13/cobra` | CLI framework with subcommands and flags | `optimist` + `rc` |
| `golang.org/x/term` | Raw terminal mode for keyboard input | `keypress` |
| Go stdlib `net/http` | HTTP server with Range support | `http` + `range-parser` + `pump` |
| Go stdlib `mime` | MIME type detection | `mime` npm package |
| Go stdlib `os/exec` | Player process launching | `child_process` |
| Go stdlib `encoding/json` | JSON stats endpoint | built-in |

## Data Flow

```
User provides magnet link or .torrent path
         │
         ▼
┌─────────────────────┐
│  Parse torrent      │  (anacrolix/torrent parses magnet URI or .torrent file)
│  metadata           │
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  Connect to swarm   │  (DHT, trackers, PEX → discover peers)
│  Download metadata  │  (if magnet, fetch info dict from peers)
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  Select file         │  (largest by default, or by --index)
│  Create Reader       │  (anacrolix Reader with read-ahead)
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  Start HTTP server   │  (listen on --port, default 8888)
│  Serve Reader via    │  (http.ServeContent handles Range)
│  HTTP Range          │
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  Launch player       │  (if --vlc, --mpv, --iina)
│  OR print URL        │  (user opens manually)
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  TUI dashboard       │  (refresh every 500ms)
│  Show stats until    │  (Ctrl+C or player exit)
│  exit                │
└─────────────────────┘
```

## Build & Distribution

- `go build -o peerflix-go .` produces a single binary
- `Makefile` targets for `darwin/amd64`, `darwin/arm64`, `linux/amd64`
- Binary name: `peerflix-go` (or `pfx` as a short alias)

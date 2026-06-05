# 02 — Command-Line Interface

## Overview

The CLI is the entry point for `peerflix-go`. It parses command-line flags, initializes components, orchestrates the startup sequence, and handles shutdown signals. It mirrors the original peerflix CLI interface while adding Go-idiomatic improvements.

## Usage

```
peerflix-go <magnet-link-or-torrent> [flags]

Stream video from a torrent magnet link or .torrent file.
The torrent is downloaded sequentially and served over a local HTTP server
for immediate playback in any media player.

Examples:
  peerflix-go "magnet:?xt=urn:btih:..." --vlc
  peerflix-go movie.torrent --mpv --port 9000
  peerflix-go "magnet:?xt=urn:btih:..." --list
  peerflix-go "magnet:?xt=urn:btih:..." --index 2 --iina
```

## Flags

### Server Flags
| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--port` | `-p` | int | `8888` | HTTP server listen port |
| `--hostname` | `-h` | string | `""` (all interfaces) | Bind address for the HTTP server |

### File Selection Flags
| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--index` | `-i` | int | `-1` (largest) | Stream file at this index |
| `--list` | `-l` | bool | `false` | List files with indices and exit |
| `--all` | `-a` | bool | `false` | Download all files (serve as M3U playlist) |

### Player Flags
| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--vlc` | `-v` | bool | `false` | Auto-open in VLC |
| `--mpv` | `-k` | bool | `false` | Auto-open in mpv |
| `--iina` | | bool | `false` | Auto-open in IINA (macOS) |
| `--mplayer` | `-m` | bool | `false` | Auto-open in mplayer |
| `--smplayer` | `-g` | bool | `false` | Auto-open in smplayer |
| `--no-quit` | `-n` | bool | `false` | Don't exit when player closes |
| `--not-on-top` | `-d` | bool | `false` | Don't float video on top |
| `--subtitles` | `-t` | string | `""` | Path to subtitles file (.srt, .ass) |

### Network Flags
| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--connections` | `-c` | int | `100` | Max connected peers |
| `--peer` | `-e` | string[] | `[]` | Add peer manually (ip:port), repeatable |
| `--peer-port` | `-x` | int | `0` (random) | Peer listening port |
| `--blocklist` | `-b` | string | `""` | Path to blocklist file |
| `--no-dht` | | bool | `false` | Disable DHT |

### Storage Flags
| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--path` | `-f` | string | OS temp dir | Download/buffer directory |
| `--remove` | `-r` | bool | `false` | Remove downloaded files on exit |

### Output Flags
| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--quiet` | `-q` | bool | `false` | Minimal output (just print server URL) |
| `--version` | | bool | `false` | Print version and exit |

### Hook Flags
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--on-downloaded` | string | `""` | Command to run when download completes |
| `--on-listening` | string | `""` | Command to run when server starts |

## Startup Sequence

```go
func main() {
    // 1. Parse flags
    flags := parseFlags()

    // 2. Validate input
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }

    // 3. Handle --version
    if flags.Version {
        fmt.Println("peerflix-go v1.0.0")
        os.Exit(0)
    }

    // 4. Create engine
    engine, err := NewEngine(&EngineConfig{
        DownloadDir:    flags.Path,
        MaxConnections: flags.Connections,
        PeerPort:       flags.PeerPort,
        BlocklistPath:  flags.Blocklist,
    })

    // 5. Add torrent (magnet or file)
    engine.AddTorrent(flags.Args[0])

    // 6. Wait for metadata (show "fetching metadata from N peers" for magnets)
    <-engine.WaitReady()

    // 7. Handle --list mode
    if flags.List {
        listFiles(engine)
        // If interactive TTY, prompt for selection, then continue
        // If piped, print and exit
    }

    // 8. Select file
    selected := engine.SelectFile(flags.Index) // or SelectLargest if -1

    // 9. Start HTTP server
    server := NewStreamServer(engine, selected, &ServerConfig{
        Port:     flags.Port,
        Hostname: flags.Hostname,
    })
    server.Start()

    // 10. Run --on-listening hook
    if flags.OnListening != "" {
        exec.Command("sh", "-c", flags.OnListening+" "+server.URL()).Start()
    }

    // 11. Launch player (if requested)
    if flags.PlayerName() != "" {
        LaunchPlayer(flags, server.URL())
    }

    // 12. Start TUI (unless --quiet)
    if !flags.Quiet {
        tui := NewTUI(engine, server, selected)
        go tui.Run()
    } else {
        fmt.Printf("server is listening on %s\n", server.URL())
    }

    // 13. Wait for exit signal
    waitForExit(engine, server, flags)
}
```

## Signal Handling

```go
func waitForExit(engine *Engine, server *StreamServer, flags *Flags) {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

    select {
    case <-sigCh:
        fmt.Println("\npeerflix-go is exiting...")
    case <-playerExitCh:  // player process exited
        if !flags.NoQuit {
            fmt.Println("\nPlayer closed, exiting...")
        } else {
            // Keep running, wait for another signal
            <-sigCh
        }
    }

    server.Close()
    engine.Close(flags.Remove)
}
```

## Interactive File Selection (--list with TTY)

When `--list` is used and stdin is a TTY:

1. Display files sorted alphabetically with sizes
2. Use arrow keys / numbers to select
3. Re-invoke the streaming flow with the selected index

When stdin is NOT a TTY (piped):

1. Print files with indices and sizes
2. Exit with code 0

```
$ peerflix-go "magnet:..." --list

Files in torrent:
  0 : Movie.Name.2024.1080p.mkv    (2.1 GB)
  1 : Movie.Name.2024.720p.mkv     (1.3 GB)
  2 : Subtitles.srt                 (45 KB)
  3 : NFO.nfo                       (1.2 KB)

Select file [0-3]:
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Clean exit |
| 1 | Invalid arguments / missing torrent |
| 2 | Torrent parse error (invalid magnet or corrupt .torrent) |
| 3 | Network error (can't bind port, etc.) |

## Flag Parsing Library

Use `spf13/cobra` for:
- Structured flag definitions with short/long forms
- Automatic help generation
- Positional argument validation
- Shell completion support

Alternative: plain `flag` package if we want zero dependencies beyond `anacrolix/torrent`. Decision: use `cobra` since we're already pulling in a large dependency tree with `anacrolix/torrent`.

## Configuration File Support

Support an optional `~/.config/peerflix-go/config.json` for default values:

```json
{
  "port": 8888,
  "connections": 100,
  "path": "/tmp/peerflix-go",
  "player": "iina",
  "remove": true
}
```

CLI flags override config file values. See `10_configuration.md` for details.

# 05 — Terminal UI Dashboard

## Overview

The TUI (Terminal UI) provides a live-updating dashboard in the terminal showing download progress, peer connections, speeds, and interactive controls. It refreshes every 500ms and supports keyboard input for pause/resume and opening the download directory.

This is a direct port of the `draw()` function and keyboard handling in `app.js` lines 346-445.

## Responsibilities

1. Clear and redraw the terminal every 500ms
2. Display streaming file info (name, size)
3. Display download/upload speeds and progress
4. Display peer list with per-peer stats
5. Handle keyboard input (space = pause, Ctrl+L = open dir, Ctrl+C = exit)
6. Adapt to terminal height (truncate peer list if needed)

## Interface

```go
type TUI struct {
    engine    *Engine
    server    *StreamServer
    selected  *SelectedFile
    paused    bool
    pausedAt  time.Time
    timePaused time.Duration
    startTime time.Time
    ticker    *time.Ticker
    done      chan struct{}
}

func NewTUI(engine *Engine, server *StreamServer, selected *SelectedFile) *TUI
func (t *TUI) Run()   // blocking — run in goroutine
func (t *TUI) Stop()
```

## Display Layout

```
┌──────────────────────────────────────────────────────────────────────┐
│ open vlc and enter http://192.168.1.100:8888/ as the network address│
│                                                                      │
│ info streaming Movie.Name.2024.1080p.mkv (2.1 GB) - 5.2 MB/s       │
│      from 15/42 peers                                                │
│ info path /tmp/peerflix-go/abc123                                    │
│ info downloaded 524.3 MB (24%) and uploaded 10.5 MB in 102s         │
│      with 3 hotswaps                                                 │
│ info verified 1024 pieces and received 2 invalid pieces             │
│ info peer queue size is 27                                           │
│ ──────────────────────────────────────────────────────────────────── │
│ Press SPACE to pause download or CTRL+L to open download location   │
│                                                                      │
│ 192.168.1.50:51234    12.3 MB    2.1 MB/s                           │
│ 10.0.0.100:6881       8.5 MB     1.5 MB/s                           │
│ 172.16.0.42:51413     3.2 MB     800 KB/s   choked                  │
│ 203.0.113.10:44123    1.1 MB     200 KB/s                           │
│ ... and 38 more                                                      │
│ ──────────────────────────────────────────────────────────────────── │
└──────────────────────────────────────────────────────────────────────┘
```

## Color Scheme

Using ANSI escape codes (matching your existing `vast/` color system):

| Element | Color | ANSI |
|---------|-------|------|
| "info" label | Yellow | `\033[33m` |
| "streaming", "path", etc. | Green | `\033[32m` |
| File name, speed values | Bold White | `\033[1;37m` |
| Peer addresses | Magenta | `\033[35m` |
| Peer speed | Cyan | `\033[36m` |
| "choked" tag | Gray/Dim | `\033[2m` |
| Paused indicator | Yellow bold | `\033[1;33m` |
| Separators | Dim | `\033[2m` |

## Draw Function

```go
func (t *TUI) draw() {
    stats := t.engine.Stats()

    // Calculate runtime excluding paused time
    currentPause := time.Duration(0)
    if t.paused {
        currentPause = time.Since(t.pausedAt)
    }
    runtime := int(time.Since(t.startTime).Seconds()) -
               int(t.timePaused.Seconds()) -
               int(currentPause.Seconds())

    // Get terminal dimensions
    _, height := terminalSize()
    linesRemaining := height

    // Clear screen
    fmt.Print("\033[H\033[2J")

    // Line 1: Player instruction or URL
    if t.playerName != "" {
        fmt.Printf("%sstreaming to %s%s %s%s\n", green, bold, t.playerName, reset, "")
    } else {
        url := t.server.URL()
        fmt.Printf("%sopen %svlc%s %sand enter %s%s%s %sas the network address%s\n",
            green, bold, reset, green, bold, url, reset, green, reset)
    }
    fmt.Println()

    // Line 2: File info + speed
    fmt.Printf("%sinfo%s %sstreaming%s %s%s (%s)%s %s-%s %s%s/s%s %sfrom%s %s%d/%d%s %speers%s\n",
        yellow, reset,
        green, reset,
        bold, t.selected.Name, formatBytes(t.selected.Length), reset,
        green, reset,
        bold, formatBytes(int64(stats.DownloadSpeed)), reset,
        green, reset,
        bold, stats.ActivePeers, stats.TotalPeers, reset,
        green, reset)

    // Line 3: Download path
    fmt.Printf("%sinfo%s %spath%s %s%s%s\n",
        yellow, reset, green, reset, cyan, t.engine.config.DownloadDir, reset)

    // Line 4: Progress
    pct := int(stats.Progress * 100)
    fmt.Printf("%sinfo%s %sdownloaded%s %s%s%s (%d%%) %sand uploaded%s %s%s%s %sin%s %s%ds%s\n",
        yellow, reset,
        green, reset,
        bold, formatBytes(stats.Downloaded), reset,
        pct,
        green, reset,
        bold, formatBytes(stats.Uploaded), reset,
        green, reset,
        bold, runtime, reset)

    // Line 5: Pieces
    fmt.Printf("%sinfo%s %sverified%s %s%d%s %spieces%s\n",
        yellow, reset,
        green, reset,
        bold, stats.PiecesComplete, reset,
        green, reset)

    // Line 6: Peer queue
    fmt.Printf("%sinfo%s %speer queue size is%s %s%d%s\n",
        yellow, reset,
        green, reset,
        bold, stats.TotalPeers - stats.ActivePeers, reset)

    // Separator
    fmt.Printf("%s%s%s\n", dim, strings.Repeat("─", 76), reset)

    // Interactive hint
    if t.paused {
        fmt.Printf("%sPAUSED%s %sPress SPACE to continue download or CTRL+L to open download location%s\n",
            yellowBold, reset, green, reset)
    } else {
        fmt.Printf("%sPress SPACE to pause download or CTRL+L to open download location%s\n",
            green, reset)
    }

    fmt.Println()
    linesRemaining -= 11

    // Peer list
    listed := 0
    for _, peer := range stats.PeerStats {
        if linesRemaining - listed <= 4 {
            break
        }
        tags := ""
        if peer.Choked {
            tags = dim + "choked" + reset
        }
        fmt.Printf("%s%-25s%s %10s %s%10s/s%s %s\n",
            magenta, peer.Address, reset,
            formatBytes(peer.Downloaded),
            cyan, formatBytes(int64(peer.DownloadSpeed)), reset,
            tags)
        listed++
    }

    if len(stats.PeerStats) > listed {
        fmt.Printf("%s%s%s\n", dim, strings.Repeat("─", 76), reset)
        fmt.Printf("... and %d more\n", len(stats.PeerStats)-listed)
    }

    fmt.Printf("%s%s%s\n", dim, strings.Repeat("─", 76), reset)
}
```

## Keyboard Input

Use `golang.org/x/term` to put stdin into raw mode:

```go
func (t *TUI) handleInput() {
    oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
    if err != nil {
        return  // not a TTY, skip keyboard handling
    }
    defer term.Restore(int(os.Stdin.Fd()), oldState)

    buf := make([]byte, 3)
    for {
        n, err := os.Stdin.Read(buf)
        if err != nil || n == 0 {
            continue
        }

        switch {
        case buf[0] == ' ':
            // Toggle pause
            t.togglePause()

        case buf[0] == 3:  // Ctrl+C
            // Send SIGINT to self
            p, _ := os.FindProcess(os.Getpid())
            p.Signal(syscall.SIGINT)
            return

        case buf[0] == 12:  // Ctrl+L
            // Open download directory
            t.openDownloadDir()
        }
    }
}
```

## Pause / Resume

```go
func (t *TUI) togglePause() {
    if t.paused {
        // Resume
        t.paused = false
        t.timePaused += time.Since(t.pausedAt)
        // Re-select the file to resume download
        t.selected.Reader.SetReadahead(5 * 1024 * 1024)
    } else {
        // Pause
        t.paused = true
        t.pausedAt = time.Now()
        // Set readahead to 0 to stop downloading
        t.selected.Reader.SetReadahead(0)
    }
}
```

## Open Download Directory

```go
func (t *TUI) openDownloadDir() {
    var cmd string
    switch runtime.GOOS {
    case "darwin":
        cmd = "open"
    case "linux":
        cmd = "xdg-open"
    case "windows":
        cmd = "explorer"
    }
    exec.Command(cmd, t.engine.config.DownloadDir).Start()
}
```

## Terminal Size Detection

```go
func terminalSize() (width, height int) {
    w, h, err := term.GetSize(int(os.Stdout.Fd()))
    if err != nil {
        return 80, 24  // default fallback
    }
    return w, h
}
```

## Refresh Loop

```go
func (t *TUI) Run() {
    t.startTime = time.Now()
    t.ticker = time.NewTicker(500 * time.Millisecond)
    defer t.ticker.Stop()

    // Start keyboard handler in background
    go t.handleInput()

    // Initial draw
    t.draw()

    for {
        select {
        case <-t.ticker.C:
            t.draw()
        case <-t.done:
            return
        }
    }
}
```

## Magnet Metadata Phase

Before metadata is available (magnet links), show a simpler screen:

```
fetching torrent metadata from 12 peers
```

Update as peers connect. Once metadata arrives, switch to the full dashboard.

```go
func (t *TUI) showMetadataFetch() {
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            stats := t.engine.Stats()
            fmt.Print("\033[H\033[2J")
            fmt.Printf("%sfetching torrent metadata from%s %s%d%s %speers%s\n",
                green, reset, bold, stats.TotalPeers, reset, green, reset)
        case <-t.engine.WaitReady():
            return
        }
    }
}
```

## Non-Interactive Mode

If stdout is not a TTY (piped output), disable:
- Screen clearing
- Raw keyboard input
- Color codes

Instead, print one-line periodic updates:

```
[10s] 2.1 MB/s ↓ | 15 peers | 24% complete
[20s] 3.4 MB/s ↓ | 22 peers | 31% complete
```

Detect with:
```go
interactive := term.IsTerminal(int(os.Stdout.Fd()))
```

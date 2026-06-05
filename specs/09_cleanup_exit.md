# 09 — Cleanup & Exit Handling

## Overview

Cleanup ensures that peerflix-go shuts down gracefully: closing peer connections, stopping the HTTP server, optionally removing downloaded files, and restoring the terminal state. This is a port of the exit handling in `app.js` lines 470-494.

## Responsibilities

1. Handle OS signals (SIGINT, SIGTERM)
2. Handle player process exit
3. Close the HTTP server gracefully
4. Close the torrent engine (drop torrent, close client)
5. Optionally remove downloaded files (`--remove`)
6. Restore terminal state (if raw mode was enabled)
7. Print exit status message

## Signal Handling

```go
func setupSignals() <-chan os.Signal {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    return sigCh
}
```

## Exit Flow

```go
func shutdown(engine *Engine, server *StreamServer, tui *TUI, flags *Flags) {
    // 1. Print status
    fmt.Println()
    fmt.Printf("%sinfo%s %speerflix-go is exiting...%s\n", yellow, reset, green, reset)

    // 2. Stop TUI (restores terminal)
    if tui != nil {
        tui.Stop()
    }

    // 3. Close HTTP server (graceful with 5s timeout)
    if server != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        server.httpServer.Shutdown(ctx)
    }

    // 4. Close engine
    if engine != nil {
        if flags.Remove {
            // Remove downloaded data
            fmt.Printf("%sinfo%s %sremoving downloaded files...%s\n",
                yellow, reset, green, reset)
            engine.Close(true)  // true = remove files
        } else {
            engine.Close(false) // false = keep files
        }
    }

    // 5. Print final stats
    if engine != nil {
        stats := engine.Stats()
        fmt.Printf("%sinfo%s %sdownloaded %s, uploaded %s%s\n",
            yellow, reset, green,
            formatBytes(stats.Downloaded),
            formatBytes(stats.Uploaded),
            reset)
    }
}
```

## Exit Triggers

### Trigger 1: SIGINT / SIGTERM

User presses Ctrl+C or system sends termination signal.

```go
select {
case sig := <-sigCh:
    log.Printf("Received %s", sig)
    shutdown(engine, server, tui, flags)
    os.Exit(0)
}
```

### Trigger 2: Player Exit

When a player was launched and it exits:

```go
select {
case <-playerExitCh:
    if !flags.NoQuit {
        shutdown(engine, server, tui, flags)
        os.Exit(0)
    }
    // If --no-quit, keep running and wait for signal
}
```

### Trigger 3: TUI Ctrl+C

The TUI's keyboard handler sends SIGINT to self:

```go
case buf[0] == 3: // Ctrl+C
    p, _ := os.FindProcess(os.Getpid())
    p.Signal(syscall.SIGINT)
```

This is caught by the signal handler (Trigger 1).

## Terminal Restoration

If the TUI put stdin into raw mode, it MUST be restored on exit:

```go
type TUI struct {
    oldTermState *term.State
    // ...
}

func (t *TUI) Stop() {
    // Stop the refresh ticker
    if t.ticker != nil {
        t.ticker.Stop()
    }
    close(t.done)

    // Restore terminal
    if t.oldTermState != nil {
        term.Restore(int(os.Stdin.Fd()), t.oldTermState)
    }

    // Clear screen one final time
    fmt.Print("\033[H\033[2J")
}
```

**Critical:** If the program panics or exits abnormally, the terminal may be left in raw mode. Use `defer` to guard:

```go
func (t *TUI) Run() {
    oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
    if err == nil {
        t.oldTermState = oldState
        defer term.Restore(int(os.Stdin.Fd()), oldState)
    }
    // ...
}
```

## File Removal (`--remove`)

When `--remove` is specified:

```go
func (e *Engine) Close(remove bool) {
    // Drop the torrent (stops all peer activity)
    if e.torrent != nil {
        e.torrent.Drop()
    }

    // Close the client
    if e.client != nil {
        e.client.Close()
    }

    // Remove data directory
    if remove && e.config.DownloadDir != "" {
        err := os.RemoveAll(e.config.DownloadDir)
        if err != nil {
            fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n",
                e.config.DownloadDir, err)
        }
    }
}
```

**Safety checks:**
- Never `RemoveAll` on `/`, `/tmp`, or `$HOME`
- Only remove the specific torrent data directory, not a user-provided parent
- If the download dir is a user-specified path (not auto-generated), only remove the torrent subfolder

```go
func safePath(dir string) bool {
    abs, _ := filepath.Abs(dir)
    home, _ := os.UserHomeDir()

    dangerous := []string{"/", "/tmp", home, "/var", "/etc", "/usr"}
    for _, d := range dangerous {
        if abs == d {
            return false
        }
    }
    return true
}
```

## Graceful HTTP Shutdown

Use `http.Server.Shutdown` to let in-flight requests complete:

```go
func (s *StreamServer) Close() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // This will:
    // 1. Stop accepting new connections
    // 2. Wait for active requests to complete (up to 5s)
    // 3. Close all idle connections immediately
    return s.httpServer.Shutdown(ctx)
}
```

If a player is still streaming when shutdown is called, the 5-second timeout allows the last chunk to be sent before forcefully closing.

## Combined Exit Orchestration

```go
func waitForExit(engine *Engine, server *StreamServer, tui *TUI,
                 playerExit <-chan struct{}, flags *Flags) {

    sigCh := setupSignals()

    for {
        select {
        case <-sigCh:
            shutdown(engine, server, tui, flags)
            return

        case <-playerExit:
            if flags.NoQuit {
                // Player exited but --no-quit is set
                // Reset playerExit to nil so we don't re-trigger
                playerExit = nil
                continue
            }
            shutdown(engine, server, tui, flags)
            return
        }
    }
}
```

## Temporary Directory Management

Default download directory:

```go
func defaultDownloadDir() string {
    dir := filepath.Join(os.TempDir(), "peerflix-go")
    os.MkdirAll(dir, 0755)
    return dir
}
```

Each torrent gets a subdirectory based on its info hash:

```go
func torrentDataDir(baseDir string, infoHash string) string {
    dir := filepath.Join(baseDir, infoHash[:12])
    os.MkdirAll(dir, 0755)
    return dir
}
```

## Process Cleanup on Crash

Register a panic handler to restore terminal:

```go
func main() {
    defer func() {
        if r := recover(); r != nil {
            // Attempt terminal restoration
            term.Restore(int(os.Stdin.Fd()), savedTermState)
            fmt.Fprintf(os.Stderr, "panic: %v\n", r)
            os.Exit(1)
        }
    }()

    // ... normal execution
}
```

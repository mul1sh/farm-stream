# 04 — Player Integration

## Overview

Player integration handles auto-detection and launching of media players with the streaming URL. The player receives the local HTTP URL and plays the torrent as if it were a standard web video stream. When the player exits, peerflix-go optionally exits too.

This is a direct port of the player-launching logic in `app.js` lines 244-338.

## Responsibilities

1. Detect available media players on the system
2. Launch the requested player with the stream URL
3. Pass player-specific flags (on-top, subtitles, quiet mode)
4. Monitor the player process for exit
5. Signal peerflix-go to exit when the player closes (unless `--no-quit`)

## Supported Players

| Player | Flag | macOS | Linux | Windows |
|--------|------|-------|-------|---------|
| IINA | `--iina` | ✓ | — | — |
| VLC | `--vlc` | ✓ | ✓ | ✓ |
| mpv | `--mpv` | ✓ | ✓ | ✓ |
| mplayer | `--mplayer` | ✓ | ✓ | ✓ |
| smplayer | `--smplayer` | ✓ | ✓ | ✓ |
| OMX | `--omx` | — | ✓ (RPi) | — |

## Interface

```go
// PlayerConfig holds settings for launching a media player.
type PlayerConfig struct {
    Name       string   // player name: "vlc", "mpv", "iina", etc.
    StreamURL  string   // HTTP URL to stream from
    Subtitles  string   // path to subtitles file (optional)
    OnTop      bool     // float video window on top (default true)
    ExtraArgs  []string // additional args passed through
}

// LaunchPlayer starts the media player and returns a channel
// that is closed when the player process exits.
func LaunchPlayer(config *PlayerConfig) (<-chan struct{}, error)
```

## Player Detection

For each player, check if it's available:

```go
func findPlayer(name string) (string, error) {
    // 1. Check PATH
    path, err := exec.LookPath(name)
    if err == nil {
        return path, nil
    }

    // 2. Check well-known locations per platform
    for _, p := range knownPaths(name) {
        if _, err := os.Stat(p); err == nil {
            return p, nil
        }
    }

    return "", fmt.Errorf("player %q not found", name)
}
```

### Well-Known Paths

**macOS:**
```go
var macPaths = map[string][]string{
    "iina": {
        "/Applications/IINA.app/Contents/MacOS/iina-cli",
    },
    "vlc": {
        "/Applications/VLC.app/Contents/MacOS/VLC",
        os.Getenv("HOME") + "/Applications/VLC.app/Contents/MacOS/VLC",
    },
    "mpv": {
        "/opt/homebrew/bin/mpv",
        "/usr/local/bin/mpv",
    },
}
```

**Linux:**
```go
var linuxPaths = map[string][]string{
    "vlc":     {"/usr/bin/vlc"},
    "mpv":     {"/usr/bin/mpv"},
    "mplayer": {"/usr/bin/mplayer"},
    "omxplayer": {"/usr/bin/omxplayer"},
}
```

## Player Command Construction

### IINA (macOS only)

```go
func iinaArgs(config *PlayerConfig) []string {
    args := []string{"--no-stdin", config.StreamURL}
    if config.OnTop {
        args = append(args, "--pip")  // picture-in-picture
    }
    if config.Subtitles != "" {
        args = append(args, "--sub-file="+config.Subtitles)
    }
    return append(args, config.ExtraArgs...)
}
```

Executable: `iina-cli` (not `iina`) — the CLI companion tool.

### VLC

```go
func vlcArgs(config *PlayerConfig) []string {
    args := []string{
        "-q",                    // quiet
        "--play-and-exit",       // exit when done
    }
    if config.OnTop {
        args = append(args, "--video-on-top")
    }
    if config.Subtitles != "" {
        args = append(args, "--sub-file="+config.Subtitles)
    }
    // Set window title to filename
    filename := filepath.Base(config.StreamURL)
    args = append(args, fmt.Sprintf("--meta-title=%s", filename))
    args = append(args, config.StreamURL)
    return append(args, config.ExtraArgs...)
}
```

### mpv

```go
func mpvArgs(config *PlayerConfig) []string {
    args := []string{
        "--really-quiet",
        "--loop=no",
    }
    if config.OnTop {
        args = append(args, "--ontop")
    }
    if config.Subtitles != "" {
        args = append(args, "--sub-file="+config.Subtitles)
    }
    args = append(args, config.StreamURL)
    return append(args, config.ExtraArgs...)
}
```

### mplayer

```go
func mplayerArgs(config *PlayerConfig) []string {
    args := []string{
        "-really-quiet",
        "-noidx",
        "-loop", "0",
    }
    if config.OnTop {
        args = append(args, "-ontop")
    }
    if config.Subtitles != "" {
        args = append(args, "-sub", config.Subtitles)
    }
    args = append(args, config.StreamURL)
    return append(args, config.ExtraArgs...)
}
```

### smplayer

```go
func smplayerArgs(config *PlayerConfig) []string {
    args := []string{}
    if config.OnTop {
        args = append(args, "-ontop")
    }
    if config.Subtitles != "" {
        args = append(args, "-sub", config.Subtitles)
    }
    args = append(args, config.StreamURL)
    return append(args, config.ExtraArgs...)
}
```

### OMX (Raspberry Pi)

```go
func omxArgs(config *PlayerConfig, useJack bool) []string {
    output := "hdmi"
    if useJack {
        output = "local"
    }
    args := []string{"-r", "-o", output}
    if config.Subtitles != "" {
        args = append(args, "--subtitles", config.Subtitles)
    }
    args = append(args, config.StreamURL)
    return append(args, config.ExtraArgs...)
}
```

## Process Lifecycle

```go
func LaunchPlayer(config *PlayerConfig) (<-chan struct{}, error) {
    playerPath, err := findPlayer(config.Name)
    if err != nil {
        return nil, err
    }

    args := buildArgs(config)
    cmd := exec.Command(playerPath, args...)

    // Don't pipe stdout/stderr — let player show its own window
    cmd.Stdout = nil
    cmd.Stderr = nil
    cmd.Stdin = nil

    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("failed to start %s: %w", config.Name, err)
    }

    exitCh := make(chan struct{})
    go func() {
        cmd.Wait()
        close(exitCh)
    }()

    return exitCh, nil
}
```

## Extra Args Passthrough

Arguments after `--` are passed directly to the player:

```
peerflix-go "magnet:..." --vlc -- --fullscreen --no-video-title-show
```

The `--fullscreen --no-video-title-show` part is captured in `config.ExtraArgs` and appended to the player's argument list.

## Auto-Detection Mode

If no player flag is specified but the user hasn't used `--quiet`:
- Print the URL for manual use
- Don't auto-launch anything

```
open vlc and enter http://localhost:8888/ as the network address
```

## macOS `open` Command

For IINA on macOS, we can alternatively use:
```
open -a IINA --args --no-stdin http://localhost:8888/
```

But prefer the `iina-cli` binary for better control and exit detection.

## Error Handling

- If the player binary is not found, print a helpful error:
  ```
  ✗ VLC not found. Install it or specify the path.
    macOS: brew install --cask vlc
    Linux: sudo apt install vlc
  ```
- If the player crashes immediately (exit code != 0 within 2s of launch), warn but don't exit peerflix-go
- If the player exits normally, signal the exit channel

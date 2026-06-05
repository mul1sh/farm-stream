# 10 — Configuration

## Overview

Configuration management handles default values, config file loading, environment variables, and CLI flag precedence. This is a port of the `rc` (runtime configuration) module used in `app.js` line 21.

## Responsibilities

1. Define sensible defaults for all settings
2. Load optional config file (`~/.config/peerflix-go/config.json`)
3. Read environment variable overrides
4. Merge with CLI flags (flags take highest precedence)

## Precedence Order (highest wins)

```
CLI Flags  >  Environment Variables  >  Config File  >  Defaults
```

## Default Values

```go
var Defaults = Config{
    Port:           8888,
    Hostname:       "",     // all interfaces
    Connections:    100,
    PeerPort:       0,      // random
    DownloadDir:    "",     // will resolve to os.TempDir()/peerflix-go
    Index:          -1,     // auto-select largest
    Remove:         false,
    Quiet:          false,
    OnTop:          true,
    ReadAhead:      5 * 1024 * 1024,  // 5MB
    Seed:           false,
    NoDHT:          false,
}
```

## Config File

### Location

```
~/.config/peerflix-go/config.json
```

On macOS: `~/Library/Application Support/peerflix-go/config.json` (follow XDG on Linux).

Actually, keep it simple — use `~/.config/peerflix-go/config.json` on all platforms for consistency:

```go
func configPath() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".config", "peerflix-go", "config.json")
}
```

### Format

```json
{
  "port": 8888,
  "connections": 100,
  "download_dir": "/tmp/peerflix-go",
  "player": "iina",
  "remove": true,
  "on_top": true,
  "quiet": false,
  "peer_port": 6881,
  "no_dht": false,
  "seed": false,
  "blocklist": "",
  "subtitles": "",
  "read_ahead_mb": 5
}
```

All fields are optional. Missing fields use defaults.

### Loading

```go
type Config struct {
    Port         int    `json:"port"`
    Hostname     string `json:"hostname"`
    Connections  int    `json:"connections"`
    PeerPort     int    `json:"peer_port"`
    DownloadDir  string `json:"download_dir"`
    Player       string `json:"player"`
    Index        int    `json:"index"`
    Remove       bool   `json:"remove"`
    Quiet        bool   `json:"quiet"`
    OnTop        bool   `json:"on_top"`
    NoDHT        bool   `json:"no_dht"`
    Seed         bool   `json:"seed"`
    Blocklist    string `json:"blocklist"`
    Subtitles    string `json:"subtitles"`
    ReadAheadMB  int    `json:"read_ahead_mb"`
    NoQuit       bool   `json:"no_quit"`
}

func LoadConfig() Config {
    cfg := Defaults

    // Try loading config file
    path := configPath()
    data, err := os.ReadFile(path)
    if err == nil {
        json.Unmarshal(data, &cfg)
    }

    // Environment variable overrides
    if v := os.Getenv("PEERFLIX_PORT"); v != "" {
        cfg.Port, _ = strconv.Atoi(v)
    }
    if v := os.Getenv("PEERFLIX_CONNECTIONS"); v != "" {
        cfg.Connections, _ = strconv.Atoi(v)
    }
    if v := os.Getenv("PEERFLIX_DOWNLOAD_DIR"); v != "" {
        cfg.DownloadDir = v
    }
    if v := os.Getenv("PEERFLIX_PLAYER"); v != "" {
        cfg.Player = v
    }

    return cfg
}
```

## Environment Variables

| Variable | Maps to | Example |
|----------|---------|---------|
| `PEERFLIX_PORT` | `--port` | `PEERFLIX_PORT=9000` |
| `PEERFLIX_CONNECTIONS` | `--connections` | `PEERFLIX_CONNECTIONS=50` |
| `PEERFLIX_DOWNLOAD_DIR` | `--path` | `PEERFLIX_DOWNLOAD_DIR=/data/torrents` |
| `PEERFLIX_PLAYER` | `--vlc`/`--mpv`/etc. | `PEERFLIX_PLAYER=mpv` |
| `PEERFLIX_PEER_PORT` | `--peer-port` | `PEERFLIX_PEER_PORT=6881` |
| `PEERFLIX_BLOCKLIST` | `--blocklist` | `PEERFLIX_BLOCKLIST=/etc/blocklist.txt` |

## Merging with CLI Flags

```go
func mergeFlags(cfg Config, flags *pflag.FlagSet) Config {
    // Only override config values if the flag was explicitly set
    if flags.Changed("port") {
        cfg.Port, _ = flags.GetInt("port")
    }
    if flags.Changed("connections") {
        cfg.Connections, _ = flags.GetInt("connections")
    }
    if flags.Changed("path") {
        cfg.DownloadDir, _ = flags.GetString("path")
    }
    // ... etc for all flags

    // Player flags are mutually exclusive booleans
    if v, _ := flags.GetBool("vlc"); v {
        cfg.Player = "vlc"
    }
    if v, _ := flags.GetBool("mpv"); v {
        cfg.Player = "mpv"
    }
    if v, _ := flags.GetBool("iina"); v {
        cfg.Player = "iina"
    }

    return cfg
}
```

## Download Directory Resolution

```go
func resolveDownloadDir(cfg Config) string {
    if cfg.DownloadDir != "" {
        return cfg.DownloadDir
    }

    // Default: OS temp dir / peerflix-go
    dir := filepath.Join(os.TempDir(), "peerflix-go")
    os.MkdirAll(dir, 0755)
    return dir
}
```

## Config Init Command

Optionally support creating a default config file:

```
peerflix-go config init
```

This creates `~/.config/peerflix-go/config.json` with defaults and comments:

```go
func initConfig() {
    path := configPath()
    dir := filepath.Dir(path)
    os.MkdirAll(dir, 0755)

    cfg := Defaults
    data, _ := json.MarshalIndent(cfg, "", "  ")
    os.WriteFile(path, data, 0644)

    fmt.Printf("Config written to %s\n", path)
}
```

## Validation

After merging all config sources:

```go
func (c Config) Validate() error {
    if c.Port < 0 || c.Port > 65535 {
        return fmt.Errorf("port must be 0-65535, got %d", c.Port)
    }
    if c.Connections < 1 {
        return fmt.Errorf("connections must be >= 1, got %d", c.Connections)
    }
    if c.PeerPort < 0 || c.PeerPort > 65535 {
        return fmt.Errorf("peer-port must be 0-65535, got %d", c.PeerPort)
    }
    if c.ReadAheadMB < 0 {
        return fmt.Errorf("read-ahead must be >= 0, got %d", c.ReadAheadMB)
    }
    if c.Blocklist != "" {
        if _, err := os.Stat(c.Blocklist); err != nil {
            return fmt.Errorf("blocklist file not found: %s", c.Blocklist)
        }
    }
    return nil
}
```

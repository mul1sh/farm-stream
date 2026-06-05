# 07 — Stats & Reporting

## Overview

Stats reporting provides real-time telemetry about the torrent swarm, download/upload progress, and per-peer connection details. Stats are consumed by two components: the TUI dashboard (spec `05`) and the HTTP JSON endpoint (spec `03`).

This is a port of the stats logic in `index.js` lines 65-96 and `app.js` lines 132-149, 392-442.

## Responsibilities

1. Track download/upload byte counters
2. Calculate download/upload speeds (bytes/sec)
3. Track piece verification (valid + invalid pieces)
4. Track per-peer statistics
5. Track hotswaps (peer replacements)
6. Provide a thread-safe stats snapshot

## Data Structures

### Swarm-Level Stats

```go
type EngineStats struct {
    // Transfer totals
    Downloaded    int64   // total bytes downloaded
    Uploaded      int64   // total bytes uploaded

    // Speeds (computed as rolling average)
    DownloadSpeed float64 // bytes/sec
    UploadSpeed   float64 // bytes/sec

    // Progress
    TotalLength    int64   // total size of selected file(s)
    Progress       float64 // 0.0 - 1.0

    // Pieces
    PiecesTotal    int
    PiecesComplete int
    PiecesInvalid  int     // received invalid/corrupt pieces

    // Peers
    TotalPeers  int         // all connected peers
    ActivePeers int         // peers not choking us
    QueuedPeers int         // peers waiting to connect
    PeerStats   []PeerStat  // per-peer details

    // Metadata
    Hotswaps  int    // number of peer replacements
    Name      string // torrent name
    InfoHash  string // torrent info hash (hex)
}
```

### Per-Peer Stats

```go
type PeerStat struct {
    Address       string  // ip:port
    Downloaded    int64   // bytes downloaded from this peer
    DownloadSpeed float64 // bytes/sec from this peer
    Uploaded      int64   // bytes uploaded to this peer
    UploadSpeed   float64 // bytes/sec to this peer
    Choked        bool    // peer is choking us (not sending data)
    Interested    bool    // we are interested in this peer's data
    Client        string  // peer client identification string
}
```

## Speed Calculation

The `anacrolix/torrent` library provides cumulative byte counters via `torrent.Stats()`. To compute speeds, sample at regular intervals and calculate deltas:

```go
type SpeedTracker struct {
    mu           sync.Mutex
    lastSample   time.Time
    lastDownload int64
    lastUpload   int64
    downloadSpeed float64
    uploadSpeed   float64
}

func (st *SpeedTracker) Update(downloaded, uploaded int64) {
    st.mu.Lock()
    defer st.mu.Unlock()

    now := time.Now()
    elapsed := now.Sub(st.lastSample).Seconds()

    if elapsed > 0 && st.lastSample != (time.Time{}) {
        st.downloadSpeed = float64(downloaded - st.lastDownload) / elapsed
        st.uploadSpeed = float64(uploaded - st.lastUpload) / elapsed
    }

    st.lastSample = now
    st.lastDownload = downloaded
    st.lastUpload = uploaded
}
```

Update the speed tracker in the TUI refresh loop (every 500ms).

## Stats Collection from anacrolix/torrent

```go
func (e *Engine) Stats() EngineStats {
    t := e.torrent
    if t == nil {
        return EngineStats{}
    }

    tStats := t.Stats()

    // Collect peer stats
    peers := make([]PeerStat, 0)
    activePeers := 0
    for _, conn := range tStats.ConnStats {
        // ... build per-peer stats
    }

    // Alternative: use t.PeerConns() for detailed per-peer info
    for _, pc := range t.PeerConns() {
        ps := PeerStat{
            Address:       pc.RemoteAddr.String(),
            Downloaded:    pc.Stats().BytesReadData.Int64(),
            Uploaded:      pc.Stats().BytesWrittenData.Int64(),
            Choked:        pc.PeerChoking(),
            Interested:    pc.Interested(),
            Client:        pc.PeerClientName.Load().(string),
        }
        if !ps.Choked {
            activePeers++
        }
        peers = append(peers, ps)
    }

    // Sort peers by download speed (fastest first)
    sort.Slice(peers, func(i, j int) bool {
        return peers[i].DownloadSpeed > peers[j].DownloadSpeed
    })

    // Piece stats
    info := t.Info()
    piecesComplete := 0
    piecesTotal := 0
    if info != nil {
        piecesTotal = t.NumPieces()
        for i := 0; i < piecesTotal; i++ {
            if t.Piece(i).State().Complete {
                piecesComplete++
            }
        }
    }

    // Progress
    var progress float64
    totalLength := t.Length()
    bytesComplete := t.BytesCompleted()
    if totalLength > 0 {
        progress = float64(bytesComplete) / float64(totalLength)
    }

    return EngineStats{
        Downloaded:     tStats.BytesReadData.Int64(),
        Uploaded:       tStats.BytesWrittenData.Int64(),
        DownloadSpeed:  e.speedTracker.downloadSpeed,
        UploadSpeed:    e.speedTracker.uploadSpeed,
        TotalLength:    totalLength,
        Progress:       progress,
        PiecesTotal:    piecesTotal,
        PiecesComplete: piecesComplete,
        TotalPeers:     len(peers),
        ActivePeers:    activePeers,
        PeerStats:      peers,
        Name:           t.Name(),
        InfoHash:       t.InfoHash().HexString(),
    }
}
```

## JSON Endpoint Response

The `/.json` endpoint returns stats formatted for external consumers (matches peerflix format):

```go
type JSONStats struct {
    TotalLength   int64       `json:"totalLength"`
    Downloaded    int64       `json:"downloaded"`
    Uploaded      int64       `json:"uploaded"`
    DownloadSpeed int         `json:"downloadSpeed"`
    UploadSpeed   int         `json:"uploadSpeed"`
    TotalPeers    int         `json:"totalPeers"`
    ActivePeers   int         `json:"activePeers"`
    Progress      float64     `json:"progress"`
    Files         []JSONFile  `json:"files"`
}

type JSONFile struct {
    Name   string `json:"name"`
    URL    string `json:"url"`
    Length int64  `json:"length"`
}
```

## Byte Formatting

```go
func formatBytes(b int64) string {
    const (
        KB = 1024
        MB = KB * 1024
        GB = MB * 1024
        TB = GB * 1024
    )

    switch {
    case b >= TB:
        return fmt.Sprintf("%.1f TB", float64(b)/float64(TB))
    case b >= GB:
        return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
    case b >= MB:
        return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
    case b >= KB:
        return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
    default:
        return fmt.Sprintf("%d B", b)
    }
}
```

## Thread Safety

- `EngineStats` is a value type (returned by copy) — safe to read from any goroutine
- The `SpeedTracker` uses a mutex for concurrent access
- `t.PeerConns()` in `anacrolix/torrent` is thread-safe
- The TUI and JSON endpoint may call `Stats()` concurrently — both get independent snapshots

## Hotswap Tracking

A "hotswap" occurs when the engine replaces a slow peer with a faster one. Track via torrent events:

```go
// anacrolix/torrent doesn't expose hotswap events directly.
// Approximate by tracking peer churn: new peers replacing disconnected ones
// while actively downloading the same pieces.

// For v1, omit hotswap tracking or set to 0.
// The stat exists for peerflix compatibility but is non-critical.
```

## On-Downloaded Hook

When the download completes, fire the `--on-downloaded` hook:

```go
func (e *Engine) watchCompletion(hook string) {
    go func() {
        // Poll until complete
        for {
            time.Sleep(time.Second)
            if e.torrent.BytesMissing() == 0 {
                if hook != "" {
                    exec.Command("sh", "-c", hook).Start()
                }
                return
            }
        }
    }()
}
```

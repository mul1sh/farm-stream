# 01 — Torrent Engine

## Overview

The torrent engine is the core component that wraps `anacrolix/torrent` to handle BitTorrent protocol operations: parsing torrents, connecting to the swarm, downloading pieces in streaming order, and exposing file data through Go's `io.Reader`/`io.ReadSeeker` interfaces.

## Responsibilities

1. Initialize the `anacrolix/torrent` client with configuration
2. Accept magnet URIs or `.torrent` file paths as input
3. Resolve torrent metadata (wait for info dict if magnet)
4. Select and prioritize files for download
5. Provide `io.ReadSeeker` for each file (for HTTP serving)
6. Report swarm statistics (speed, peers, progress)
7. Apply peer blocklists
8. Clean up on shutdown (close connections, optionally remove data)

## Interface

```go
// Engine wraps the anacrolix/torrent client for streaming.
type Engine struct {
    client    *torrent.Client
    torrent   *torrent.Torrent
    config    *EngineConfig
    ready     chan struct{}        // closed when metadata is available
    startTime time.Time
}

type EngineConfig struct {
    // Download directory for torrent data
    DownloadDir string

    // Maximum number of peer connections
    MaxConnections int

    // Specific peers to connect to (ip:port)
    Peers []string

    // Blocklist file path (optional)
    BlocklistPath string

    // Peer listening port (0 = random)
    PeerPort int

    // Seed after download completes
    Seed bool

    // Disable DHT
    NoDHT bool

    // Disable UPnP port mapping
    NoUPnP bool
}
```

## Key Functions

### `NewEngine(config *EngineConfig) (*Engine, error)`

Initialize the `anacrolix/torrent` client:
- Set `torrent.ClientConfig.DataDir` to `config.DownloadDir`
- Set `torrent.ClientConfig.EstablishedConnsPerTorrent` to `config.MaxConnections`
- If `config.PeerPort > 0`, set `ListenPort`
- If `config.BlocklistPath != ""`, load and apply blocklist (see spec `08_blocklist.md`)
- If `config.NoDHT`, disable DHT
- Create and return the `Engine`

### `(e *Engine) AddTorrent(uri string) error`

Accept either a magnet URI or a file path:

```go
if strings.HasPrefix(uri, "magnet:") {
    t, err := e.client.AddMagnet(uri)
    // ...
} else {
    mi, err := metainfo.LoadFromFile(uri)
    t, err := e.client.AddTorrent(mi)
    // ...
}
```

After adding:
1. Block until metadata is available: `<-t.GotInfo()`
2. Close the `e.ready` channel to signal downstream components
3. Store `e.torrent = t`

### `(e *Engine) Files() []FileEntry`

Return all files in the torrent with metadata:

```go
type FileEntry struct {
    Index  int
    Name   string    // relative path within torrent
    Path   string    // full display path
    Length int64     // size in bytes
}
```

Map from `e.torrent.Files()`.

### `(e *Engine) SelectFile(index int) (*SelectedFile, error)`

Select a file for streaming:

```go
type SelectedFile struct {
    FileEntry
    Reader torrent.Reader   // io.ReadSeeker for HTTP serving
}
```

1. Get the `torrent.File` at `index`
2. Call `file.Download()` to prioritize all pieces for this file
3. Create a `Reader` via `file.NewReader()`
4. Set read-ahead: `reader.SetReadahead(5 * 1024 * 1024)` (5MB default)
5. The reader implements `io.ReadSeeker` — this is what the HTTP server uses

### `(e *Engine) SelectLargest() (*SelectedFile, error)`

Convenience: find the file with the largest `Length` and call `SelectFile`.

### `(e *Engine) SelectAll()`

Select all files for download (for `--all` mode):

```go
for _, f := range e.torrent.Files() {
    f.Download()
}
```

### `(e *Engine) Stats() EngineStats`

Return current swarm statistics:

```go
type EngineStats struct {
    // Download/Upload
    Downloaded     int64
    Uploaded       int64
    DownloadSpeed  float64   // bytes/sec (computed from torrent stats)
    UploadSpeed    float64   // bytes/sec

    // Peers
    TotalPeers     int
    ActivePeers    int       // not choked
    PeerStats      []PeerStat

    // Progress
    TotalLength    int64
    Completed      int64
    Progress       float64   // 0.0 - 1.0
    PiecesTotal    int
    PiecesComplete int

    // Metadata
    Name           string
    InfoHash       string
}

type PeerStat struct {
    Address       string
    Downloaded    int64
    DownloadSpeed float64
    Choked        bool
    Client        string   // peer's client name (e.g. "qBittorrent 4.5")
}
```

For computing speeds, use `e.torrent.Stats()` which provides `ConnStats` with `BytesReadData` and `BytesWrittenData`. Calculate deltas over time intervals for speed.

### `(e *Engine) WaitReady() <-chan struct{}`

Returns the `ready` channel — consumers block on this until metadata is resolved.

### `(e *Engine) Close(remove bool)`

Graceful shutdown:
1. Drop the torrent: `e.torrent.Drop()`
2. Close the client: `e.client.Close()`
3. If `remove`, delete the download directory: `os.RemoveAll(e.config.DownloadDir)`

## Piece Prioritization

`anacrolix/torrent` handles this automatically via the `Reader`:
- When `Read()` is called, the library prioritizes the pieces covering the requested byte range
- Read-ahead buffers upcoming pieces automatically
- Seeking (`Seek()`) re-prioritizes from the new position

**No manual sequential download logic is needed** — this is the key advantage over porting `torrent-stream`'s piece selection algorithm.

## Connection to Peers

Manual peer addition (from `--peer` flag):

```go
func (e *Engine) AddPeer(addr string) {
    host, portStr, _ := net.SplitHostPort(addr)
    port, _ := strconv.Atoi(portStr)
    e.torrent.AddPeers([]torrent.PeerInfo{{
        Addr: torrent.PeerRemoteAddr(net.JoinHostPort(host, portStr)),
    }})
}
```

## Error Handling

- If magnet metadata times out (no peers with info), log and keep trying
- If `.torrent` file doesn't exist or is corrupt, return immediately with error
- If download directory is not writable, fail fast

## Threading Model

- The `anacrolix/torrent` client manages its own goroutines for peer connections
- `AddTorrent` blocks in a separate goroutine waiting for metadata, signals via channel
- `Stats()` is safe to call from any goroutine (reads atomic counters)
- `Close()` is called once from the main goroutine on shutdown

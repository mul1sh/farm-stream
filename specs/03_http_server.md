# 03 — HTTP Streaming Server

## Overview

The HTTP streaming server is the bridge between the BitTorrent engine and the media player. It serves torrent file data over HTTP with full Range request support, enabling media players to seek, pause, and resume playback as if streaming from a normal web server.

This is a direct port of `index.js`'s `createServer` function.

## Responsibilities

1. Serve the selected video file with HTTP Range support
2. Serve individual files by index (`/0`, `/1`, `/2`, ...)
3. Serve files by name (`/Movie.Name.2024.mkv`)
4. Provide a JSON stats endpoint (`/.json`)
5. Provide an M3U playlist endpoint (`/.m3u`)
6. Handle CORS preflight requests
7. Set DLNA-compatible headers for smart TV / casting compatibility
8. Manage socket timeouts for long-lived streaming connections

## Interface

```go
type StreamServer struct {
    engine     *Engine
    selected   *SelectedFile
    config     *ServerConfig
    httpServer *http.Server
    listener   net.Listener
}

type ServerConfig struct {
    Port     int     // HTTP port (default 8888, 0 = random)
    Hostname string  // Bind address (default "" = all interfaces)
    Filter   func(FileEntry) bool  // File filter for playlist/stats (default: all)
}
```

## Endpoints

### `GET /` — Stream Selected File

Default endpoint. Serves the primary selected file.

- Redirects internally to `/<selected_index>`
- This is the URL given to media players

### `GET /<index>` — Stream File by Index

Serves the file at the given index from the torrent's file list.

**Headers set:**
```
Accept-Ranges: bytes
Content-Type: video/x-matroska  (or detected MIME type)
Content-Length: <file_length>
transferMode.dlna.org: Streaming
contentFeatures.dlna.org: DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000
```

**Without Range header:**
- Status: `200 OK`
- `Content-Length`: full file length
- Body: stream entire file from beginning

**With Range header:**
- Status: `206 Partial Content`
- Parse the Range header (e.g., `bytes=1000-2000`)
- `Content-Length`: `end - start + 1`
- `Content-Range`: `bytes start-end/total`
- Body: stream from `start` to `end`

**HEAD requests:** Return headers only, no body.

### `GET /<filename>` — Stream File by Name

Match the URL path against file names in the torrent. If matched, serve that file (same as by-index serving).

```go
for i, file := range engine.Files() {
    if urlPath == "/"+file.Name {
        serveFile(w, r, i)
        return
    }
}
```

### `GET /.json` — Swarm Statistics

Returns JSON with current swarm state:

```json
{
  "totalLength": 2147483648,
  "downloaded": 524288000,
  "uploaded": 10485760,
  "downloadSpeed": 5242880,
  "uploadSpeed": 1048576,
  "totalPeers": 42,
  "activePeers": 15,
  "progress": 0.244,
  "files": [
    {
      "name": "Movie.Name.2024.1080p.mkv",
      "url": "http://192.168.1.100:8888/0",
      "length": 2147483648
    },
    {
      "name": "Subtitles.srt",
      "url": "http://192.168.1.100:8888/1",
      "length": 45056
    }
  ]
}
```

Response headers:
```
Content-Type: application/json; charset=utf-8
Content-Length: <json_length>
```

### `GET /.m3u` — M3U Playlist

Returns an M3U playlist containing all (filtered) files:

```
#EXTM3U
#EXTINF:-1,Movie.Name.2024.1080p.mkv
http://192.168.1.100:8888/0
#EXTINF:-1,Subtitles.srt
http://192.168.1.100:8888/1
```

Response headers:
```
Content-Type: application/x-mpegurl; charset=utf-8
Content-Length: <playlist_length>
```

### `GET /favicon.ico` — 404

Return `404 Not Found` immediately.

## CORS Support

Handle `OPTIONS` preflight requests:

```go
if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Headers") != "" {
    w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
    w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
    w.Header().Set("Access-Control-Max-Age", "1728000")
    w.WriteHeader(200)
    return
}

// For all other requests with an Origin header:
if origin := r.Header.Get("Origin"); origin != "" {
    w.Header().Set("Access-Control-Allow-Origin", origin)
}
```

## Implementation: Serving with Range Support

Use `http.ServeContent` which handles Range requests automatically:

```go
func (s *StreamServer) serveFile(w http.ResponseWriter, r *http.Request, index int) {
    file, reader := s.engine.GetFileReader(index)

    // Set streaming headers
    w.Header().Set("Accept-Ranges", "bytes")
    w.Header().Set("Content-Type", detectMIME(file.Name))
    w.Header().Set("transferMode.dlna.org", "Streaming")
    w.Header().Set("contentFeatures.dlna.org",
        "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000")

    // http.ServeContent handles:
    // - Range header parsing
    // - 206 Partial Content responses
    // - Content-Range headers
    // - HEAD requests
    // - If-Modified-Since / If-Range
    http.ServeContent(w, r, file.Name, time.Time{}, reader)
}
```

> **Note:** `http.ServeContent` requires an `io.ReadSeeker`. The `anacrolix/torrent` `Reader` implements this interface. When the player seeks (e.g., user jumps to 50% in VLC), `Seek()` is called, and `anacrolix/torrent` reprioritizes pieces from the new position.

## Socket Timeout

Set a long timeout on connections to prevent drops during slow downloads:

```go
server.ConnState = func(conn net.Conn, state http.ConnState) {
    if state == http.StateNew {
        conn.SetDeadline(time.Now().Add(10 * time.Hour))
    }
}
```

Original peerflix uses `36000000ms` (10 hours).

## MIME Type Detection

Use Go's `mime` package with fallback:

```go
func detectMIME(filename string) string {
    ext := filepath.Ext(filename)
    mimeType := mime.TypeByExtension(ext)
    if mimeType == "" {
        mimeType = "application/octet-stream"
    }
    return mimeType
}
```

Common video types:
| Extension | MIME Type |
|-----------|-----------|
| `.mkv` | `video/x-matroska` |
| `.mp4` | `video/mp4` |
| `.avi` | `video/x-msvideo` |
| `.mov` | `video/quicktime` |
| `.webm` | `video/webm` |
| `.srt` | `text/plain` |

## Server Lifecycle

### Start

```go
func (s *StreamServer) Start() error {
    mux := http.NewServeMux()
    mux.HandleFunc("/", s.handleRequest)

    s.httpServer = &http.Server{Handler: mux}
    addr := fmt.Sprintf("%s:%d", s.config.Hostname, s.config.Port)

    var err error
    s.listener, err = net.Listen("tcp", addr)
    if err != nil {
        // If port is taken, try random port
        s.listener, err = net.Listen("tcp", s.config.Hostname+":0")
    }

    go s.httpServer.Serve(s.listener)
    return nil
}
```

### URL

```go
func (s *StreamServer) URL() string {
    addr := s.listener.Addr().(*net.TCPAddr)
    host := s.config.Hostname
    if host == "" {
        host = getLocalIP()  // detect LAN IP for other devices
    }
    return fmt.Sprintf("http://%s:%d/", host, addr.Port)
}
```

### Close

```go
func (s *StreamServer) Close() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    return s.httpServer.Shutdown(ctx)
}
```

## Port Conflict Handling

Match peerflix behavior: if the requested port is in use, fall back to a random port:

```go
s.listener, err = net.Listen("tcp", addr)
if err != nil {
    // Port taken, use random
    s.listener, err = net.Listen("tcp", s.config.Hostname+":0")
    if err != nil {
        return fmt.Errorf("cannot listen: %w", err)
    }
}
```

## Concurrent Request Handling

Go's `http.Server` handles concurrent requests in separate goroutines by default. Each request creates a new `Read` on the torrent reader. The `anacrolix/torrent` library is thread-safe and handles concurrent reads from multiple goroutines.

**Important:** Each concurrent request (e.g., player + browser checking `.json`) should get its own `Reader` or the readers must be properly synchronized. For the main video stream, use a single reader per file. For stats/playlist endpoints, no reader is needed.

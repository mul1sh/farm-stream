# 🌾 farm-stream

**Peer-to-peer agricultural data streaming powered by BitTorrent.**

Stream drone sensor feeds — video, multispectral imagery, weather telemetry, air quality data — in real-time to multiple ground stations without overloading the drone's uplink. When live streaming fails, package saved data and distribute it offline.

Built in Go. Single binary. Zero dependencies.

---

## Why BitTorrent for Agriculture?

Traditional drone data pipelines use a **hub-and-spoke model**: the drone uploads to one server, and every consumer downloads from that server. This creates three problems:

| Problem | Hub-and-Spoke | farm-stream (P2P) |
|---------|--------------|-------------------|
| **Bandwidth bottleneck** | Drone uplink saturated when multiple stations consume the feed | Each consumer shares data with others — the more consumers, the *faster* it gets |
| **Single point of failure** | Central server goes down = everyone loses access | No central server. Data lives on every peer |
| **Latency to remote farms** | Far-flung stations wait for slow server round-trips | Peers connect directly, often on the same LAN |

### The P2P Advantage

```
Traditional:                          farm-stream:

  Drone ──────► Server               Drone ──► Station A
                  │                              │ ▲
                  ├──► Station A                  │ │
                  ├──► Station B        Station B ◄─┘
                  └──► Station C        Station C ◄──► Station B
                                        
  Bandwidth: 1x                       Bandwidth: Nx (scales with peers)
  Failure: catastrophic               Failure: resilient
```

**More ground stations = faster distribution**, not slower. BitTorrent's swarm protocol turns every consumer into a redistributor.

---

## Key Advantages

### 🚀 Real-Time Streaming with HTTP Server
Stream data the moment pieces arrive — no waiting for full downloads. farm-stream includes a built-in HTTP server with full **Range request support** (RFC 7233), so media players can seek, pause, and resume as if watching from a normal web server.

```
🌐 Server: http://192.168.100.6:8888/        ← stream URL
   Stats:  http://192.168.100.6:8888/.json    ← live stats (JSON)
   M3U:    http://192.168.100.6:8888/.m3u     ← playlist for players
   Status: http://192.168.100.6:8888/status   ← active connections
```

**Every connected client gets its own independent reader** — multiple agronomists can watch the same drone feed simultaneously at different positions without seek contention. The torrent engine intelligently prioritizes pieces needed by all active readers.

### 🎬 Auto-Launch Media Players
farm-stream auto-detects and launches your media player:

```bash
farm-stream "magnet:..."               # Auto-detects VLC → mpv → IINA
farm-stream "magnet:..." --player=mpv  # Force a specific player
farm-stream "magnet:..." --no-player   # Headless/server mode
```

Supported players:
| Player | macOS | Linux |
|--------|-------|-------|
| **VLC** | ✅ `open -a VLC` | ✅ `vlc` |
| **mpv** | ✅ `mpv` | ✅ `mpv` |
| **IINA** | ✅ `open -a IINA` | — |

### 🔄 Reconnection Resilience
When a client disconnects (network drop, app crash, user pause) and reconnects:

1. Media player sends `Range: bytes=45000000-` to resume from where it left off
2. Server creates a **new reader**, seeks to byte 45M
3. Responds with `206 Partial Content`
4. Client resumes seamlessly — zero bytes re-transferred

Torrent pieces persist on disk between connections. This works for all data types:

| Data Type | On Reconnect |
|-----------|-------------|
| **Video (H.264/H.265)** | Player sends Range header, resumes mid-stream |
| **Telemetry (JSON/CSV)** | Tiny files — re-downloads in milliseconds |
| **Imagery (GeoTIFF)** | Large files — Range header resumes partial download |

### 📡 Multi-Format Drone Data
Supports all agricultural drone output formats as opaque byte streams:

| Format | Use Case |
|--------|----------|
| H.264 / H.265 | Live video feed from drone cameras |
| GeoTIFF / TIFF | NDVI, multispectral crop health imagery |
| CSV | Sensor telemetry — temperature, humidity, soil moisture |
| JSON | Structured weather data, air quality samples |
| ROS .bag | Raw robotic sensor recordings |

Format-specific processing (NDVI analysis, telemetry parsing) is handled by downstream packages — the engine just moves bytes reliably.

### 🔌 Offline Recovery (SSD Workflow)
When live streaming fails — storms, hawk attacks on the drone, fiber optic cable faults — drone data is saved to onboard SSD. Later:

```bash
# At base station: package the SSD data
farm-stream create /mnt/ssd/drone-data/flight-2024-06-05/

# Seed to all ground stations
farm-stream seed /mnt/ssd/drone-data/flight-2024-06-05.torrent
```

Ground stations pull the data via P2P. No centralized upload needed.

### 🏗️ Library-First Architecture
The engine is a proper Go package — import it into your own applications:

```go
import "github.com/mul1sh/farm-stream/engine"

e, _ := engine.New(&engine.Config{
    DownloadDir:    "/data/farm-feeds",
    MaxConnections: 100,
})
defer e.Close(false)

e.AddTorrent("magnet:?xt=urn:btih:...")
<-e.WaitReady()

// selected.Reader implements io.ReadSeeker
// Serve it via HTTP, pipe it to a decoder, write it to disk
selected, _ := e.SelectLargest()
```

### 🛡️ Resilient by Design
- **No single point of failure** — data is distributed across all peers
- **Automatic piece verification** — SHA-1 hash checking prevents corruption
- **Resumable transfers** — interrupted downloads pick up where they left off
- **NAT traversal** — DHT + UPnP for peer discovery behind firewalls
- **DLNA headers** — smart TVs and casting devices can consume the stream
- **CORS support** — browser-based dashboards can poll stats from any origin

### ⚡ Performance
- Written in Go — compiled, concurrent, low memory footprint
- `anacrolix/torrent` handles hundreds of peer connections efficiently
- Per-client readers with independent seek positions
- 5MB read-ahead buffer for smooth streaming
- 10-hour socket timeout for long-lived connections

---

## Install

### From source
```bash
go install github.com/mul1sh/farm-stream/cmd/farm-stream@latest
```

### From binary
Download from [Releases](https://github.com/mul1sh/farm-stream/releases) and add to your PATH.

---

## Usage

### Stream a torrent (auto-launches VLC)
```bash
farm-stream "magnet:?xt=urn:btih:..."                  # Stream + open VLC
farm-stream "magnet:?xt=urn:btih:..." --list            # List files only
farm-stream "magnet:?xt=urn:btih:..." --index=0         # Stream specific file
farm-stream movie.torrent                               # From .torrent file
farm-stream "magnet:..." --player=mpv                   # Use mpv instead
farm-stream "magnet:..." --no-player                    # Server only, no player
farm-stream "magnet:..." --port=9090                    # Custom HTTP port
farm-stream "magnet:..." --path=/data/downloads         # Custom download dir
```

### Create a torrent from drone data
```bash
farm-stream create /mnt/ssd/drone-data/flight-001/
farm-stream create /mnt/ssd/flight-001/ --comment="Nakuru farm scan — 2024-06-05"
farm-stream create /mnt/ssd/flight-001/ --private       # Disable DHT (private swarm)
```

### Seed saved data to ground stations
```bash
farm-stream seed /mnt/ssd/drone-data/flight-001.torrent
```

### All flags
```
--list, -l            List files in torrent and exit
--index=N, -i N       Stream file at index N (default: largest)
--port=N, -p N        HTTP server port (default: 8888)
--path=DIR, -f DIR    Download directory (default: temp dir)
--connections=N       Max peer connections (default: 100)
--player=NAME         Player to launch: vlc, mpv, iina, or auto (default: auto)
--no-player           Don't launch a media player
--quiet, -q           Minimal output
```

---

## Video Streaming Walkthrough

Here's what happens when you stream a video:

```bash
$ farm-stream "magnet:?xt=urn:btih:84E0E1F4..."
```

```
⚡ Adding torrent...
⏳ Fetching torrent metadata from peers...
✓ Torrent: Citadel.S02E05.Heirlooms.480p.x264-mSD.mkv
▶ Streaming: Citadel.S02E05... (89.1 MB)
  Download dir: /tmp/farm-stream

🌐 Server: http://192.168.100.6:8888/
   Stats:  http://192.168.100.6:8888/.json
   M3U:    http://192.168.100.6:8888/.m3u
   Status: http://192.168.100.6:8888/status

🎬 Launched vlc (pid 16759)

⚡ ↓ 247 KB/s  ↑ 0 B/s  peers: 1/1  clients: 1  progress: 1.5%  pieces: 16/1038
```

**What's happening under the hood:**

1. **Metadata resolution** — farm-stream contacts trackers/DHT to find peers and download the torrent's file list
2. **File selection** — the largest file is auto-selected (or use `--index=N`)
3. **HTTP server starts** — binds to port 8888 (or random if taken), serves the file with Range support
4. **VLC launches** — auto-detected and pointed at `http://192.168.100.6:8888/`
5. **Piece prioritization** — the engine prioritizes sequential pieces from VLC's read position for smooth playback
6. **Stats loop** — shows download speed, peers, connected clients, and progress

### Multi-Client Streaming

Multiple clients can stream simultaneously:

```bash
# Terminal: farm-stream is running on port 8888

# Client 1: VLC on the same machine (auto-launched)
# Client 2: mpv on another machine on the LAN
mpv http://192.168.100.6:8888/

# Client 3: Browser dashboard polling stats
curl http://192.168.100.6:8888/.json

# Client 4: Another VLC on a tablet
# Open VLC → Network Stream → http://192.168.100.6:8888/
```

All four clients get **independent readers** — seeking in one player doesn't affect the others.

### HTTP API

| Endpoint | Response | Use |
|----------|----------|-----|
| `GET /` | Video stream (200/206) | Point media players here |
| `GET /0`, `/1`, ... | Stream file by index | Multi-file torrents |
| `GET /.json` | Torrent stats as JSON | Monitoring dashboards |
| `GET /.m3u` | M3U playlist | Load all files into a player |
| `GET /status` | Server health + active connections | Operations monitoring |
| `HEAD /` | Headers only, no body | Player pre-flight checks |
| `OPTIONS /` | CORS preflight response | Browser-based clients |

---

## Architecture

```
farm-stream/
├── engine/                  # Core torrent engine (importable Go package)
│   ├── engine.go            # Client wrapper, file selection, torrent creation
│   ├── server.go            # HTTP streaming server with Range support
│   ├── types.go             # Config, data types, speed tracker
│   ├── utils.go             # Byte formatting, media/drone file detection
│   ├── engine_test.go       # Engine unit tests (11 tests)
│   └── server_test.go       # Server tests (11 tests)
├── cmd/
│   └── farm-stream/
│       └── main.go          # CLI entry point (stream/create/seed + player launch)
├── go.mod
└── go.sum
```

### Engine API

| Method | Description |
|--------|-------------|
| `engine.New(config)` | Create a new torrent engine |
| `e.AddTorrent(uri)` | Add torrent by magnet URI or .torrent file |
| `e.WaitReady()` | Channel that closes when metadata is available |
| `e.Files()` | List all files in the torrent |
| `e.SelectFile(index)` | Select a file for streaming (returns `io.ReadSeeker`) |
| `e.SelectLargest()` | Auto-select the largest file |
| `e.NewFileReader(index)` | Create an independent reader (for concurrent HTTP clients) |
| `e.Stats()` | Get download/upload speed, peers, progress |
| `e.Close(remove)` | Shutdown, optionally delete data |
| `engine.CreateTorrent(path, opts)` | Create .torrent from file/directory |
| `e.SeedTorrent(path)` | Load and seed an existing torrent |
| `engine.NewStreamServer(e, cfg, idx)` | Create HTTP streaming server |

---

## Roadmap

- [x] Torrent engine with streaming support
- [x] Torrent creation (producer side)
- [x] Seeding for offline data recovery
- [x] HTTP streaming server with Range support
- [x] Media player auto-launch (VLC, mpv, IINA)
- [x] Multi-client concurrent streaming
- [x] Connection tracking and monitoring endpoints
- [ ] TUI dashboard with live stats
- [ ] Peer blocklist support
- [ ] Drone data processing packages (NDVI, telemetry, weather)
- [ ] WebRTC peer transport for browser-based consumers

---

## Use Cases

### 🌽 Crop Monitoring
Stream multispectral drone imagery to agronomists' stations in real-time. Multiple field officers can view the same NDVI scan simultaneously without competing for bandwidth.

### 🌤️ Weather Station Network
Distribute hyperlocal weather data from drone-mounted sensors across a network of farm stations. Each station that receives data helps relay it to others.

### 🌿 Air Quality Sampling
Drone-collected air quality samples (particulate matter, CO2, humidity) streamed to environmental monitoring dashboards across the farm network.

### ☕ Plantation Management
Coffee, tea, and other plantation operators can monitor crop health across hundreds of hectares, with drone data distributed to multiple oversight stations.

---

## License

MIT

---

*Built for Kenyan farms. Works everywhere.*

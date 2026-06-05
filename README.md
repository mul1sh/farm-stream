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

### 🚀 Real-Time Streaming
Stream data the moment pieces arrive — no waiting for full downloads. The engine prioritizes sequential pieces for smooth playback of video feeds and progressive loading of imagery.

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

### ⚡ Performance
- Written in Go — compiled, concurrent, low memory footprint
- `anacrolix/torrent` handles hundreds of peer connections efficiently
- Automatic piece prioritization via `SetResponsive()` — no manual sequential logic
- 5MB read-ahead buffer for smooth streaming

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

### Stream a torrent
```bash
farm-stream "magnet:?xt=urn:btih:..." --list          # List files
farm-stream "magnet:?xt=urn:btih:..." --index=0        # Stream specific file
farm-stream movie.torrent                              # Auto-select largest file
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

### Flags
```
--list, -l          List files in torrent and exit
--index=N, -i N     Stream file at index N (default: largest)
--port=N, -p N      HTTP server port (default: 8888)
--path=DIR, -f DIR  Download directory (default: temp dir)
--connections=N     Max peer connections (default: 100)
--quiet, -q         Minimal output
```

---

## Architecture

```
farm-stream/
├── engine/                  # Core torrent engine (importable Go package)
│   ├── engine.go            # Client wrapper, file selection, torrent creation
│   ├── types.go             # Config, data types, speed tracker
│   ├── utils.go             # Byte formatting, media/drone file detection
│   └── engine_test.go       # Unit tests
├── cmd/
│   └── farm-stream/
│       └── main.go          # CLI entry point (3 modes: stream/create/seed)
└── specs/                   # 12 design specification documents
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
| `e.Stats()` | Get download/upload speed, peers, progress |
| `e.Close(remove)` | Shutdown, optionally delete data |
| `engine.CreateTorrent(path, opts)` | Create .torrent from file/directory |
| `e.SeedTorrent(path)` | Load and seed an existing torrent |

---

## Roadmap

- [x] Torrent engine with streaming support
- [x] Torrent creation (producer side)
- [x] Seeding for offline data recovery
- [ ] HTTP streaming server with Range support
- [ ] Media player auto-launch (VLC, mpv, IINA)
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

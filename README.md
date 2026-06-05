# farm-stream 🌾

Agricultural data streaming via BitTorrent. Built in Go.

Stream drone sensor feeds (video, NDVI imagery, weather telemetry, air quality) in real-time via P2P, or package and distribute saved data from SSD when live streaming isn't possible.

## Install

```bash
go install github.com/mulish/farm-stream/cmd/farm-stream@latest
```

## Usage

### Stream a torrent
```bash
farm-stream "magnet:?xt=urn:btih:..." --list
farm-stream "magnet:?xt=urn:btih:..." --index=0
farm-stream movie.torrent
```

### Create a torrent from drone data
```bash
farm-stream create /mnt/ssd/drone-data/flight-001/
farm-stream create /mnt/ssd/drone-data/flight-001/ --comment="Farm scan — 2024-06-05"
```

### Seed saved data to ground stations
```bash
farm-stream seed /mnt/ssd/drone-data/flight-001.torrent
```

## Architecture

```
farm-stream/
├── engine/           # Core torrent engine (importable package)
│   ├── engine.go     # Client wrapper, file selection, stats
│   ├── types.go      # Config, data types, speed tracker
│   ├── utils.go      # Byte formatting, media/drone detection
│   └── engine_test.go
├── cmd/
│   └── farm-stream/
│       └── main.go   # CLI entry point
└── specs/            # Design specifications
```

## Using as a Library

```go
import "github.com/mulish/farm-stream/engine"

e, _ := engine.New(&engine.Config{
    DownloadDir:    "/tmp/farm-stream",
    MaxConnections: 100,
})
defer e.Close(false)

e.AddTorrent("magnet:?xt=urn:btih:...")
<-e.WaitReady()

selected, _ := e.SelectLargest()
// selected.Reader implements io.ReadSeeker
```

## License

MIT

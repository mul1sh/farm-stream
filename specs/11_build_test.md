# 11 — Build, Test & Distribution

## Overview

Build configuration, test strategy, cross-compilation targets, and distribution for the peerflix-go binary.

## Project Initialization

```bash
cd peerflix-go
go mod init github.com/mulish/peerflix-go
go get github.com/anacrolix/torrent
go get github.com/spf13/cobra
go get golang.org/x/term
```

## Directory Structure

```
peerflix-go/
├── specs/                  # These specification documents
│   ├── 00_overview.md
│   ├── 01_torrent_engine.md
│   ├── ...
│   └── 11_build_test.md
├── go.mod
├── go.sum
├── main.go                 # CLI entry point, cobra root command
├── engine.go               # Torrent engine (wraps anacrolix/torrent)
├── server.go               # HTTP streaming server
├── player.go               # Media player detection & launch
├── tui.go                  # Terminal dashboard
├── blocklist.go            # Blocklist parser
├── config.go               # Configuration loading & merging
├── utils.go                # Helpers (formatBytes, MIME, colors)
├── engine_test.go          # Engine unit tests
├── server_test.go          # HTTP server tests
├── player_test.go          # Player detection tests
├── blocklist_test.go       # Blocklist parser tests
├── config_test.go          # Config loading tests
├── Makefile                # Build targets
└── README.md               # Usage documentation
```

## Makefile

```makefile
APP_NAME := peerflix-go
VERSION  := 1.0.0
LDFLAGS  := -ldflags "-s -w -X main.Version=$(VERSION)"

.PHONY: build clean test install

build:
	go build $(LDFLAGS) -o $(APP_NAME) .

build-all: build-darwin-arm64 build-darwin-amd64 build-linux-amd64

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(APP_NAME)-darwin-arm64 .

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(APP_NAME)-darwin-amd64 .

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(APP_NAME)-linux-amd64 .

install: build
	cp $(APP_NAME) /usr/local/bin/

test:
	go test -v ./...

test-race:
	go test -race -v ./...

clean:
	rm -f $(APP_NAME) $(APP_NAME)-*

lint:
	golangci-lint run ./...
```

## Test Strategy

### Unit Tests

#### `engine_test.go`
- Test magnet URI detection (magnet vs file path)
- Test file entry sorting (largest file selection)
- Test stats snapshot returns valid data
- Test media file extension detection

#### `server_test.go`
- Test HTTP 200 response for root path
- Test HTTP 206 with Range header
- Test Content-Range header formatting
- Test CORS OPTIONS preflight
- Test `.json` endpoint returns valid JSON
- Test `.m3u` endpoint returns valid playlist
- Test `/favicon.ico` returns 404
- Test out-of-range file index returns 404
- Test DLNA headers are present

```go
func TestRangeRequest(t *testing.T) {
    // Create a mock ReadSeeker with known content
    content := bytes.Repeat([]byte("A"), 1000)
    reader := bytes.NewReader(content)

    // Create test server
    handler := createFileHandler(reader, "test.mkv", int64(len(content)))
    srv := httptest.NewServer(handler)
    defer srv.Close()

    // Request Range: bytes=100-199
    req, _ := http.NewRequest("GET", srv.URL, nil)
    req.Header.Set("Range", "bytes=100-199")
    resp, _ := http.DefaultClient.Do(req)

    assert.Equal(t, 206, resp.StatusCode)
    assert.Equal(t, "bytes 100-199/1000", resp.Header.Get("Content-Range"))
    assert.Equal(t, "100", resp.Header.Get("Content-Length"))

    body, _ := io.ReadAll(resp.Body)
    assert.Equal(t, 100, len(body))
}
```

#### `player_test.go`
- Test player argument construction for each player
- Test VLC args include `--play-and-exit`
- Test mpv args include `--really-quiet`
- Test subtitle flag injection
- Test extra args passthrough
- Test on-top flag handling

#### `blocklist_test.go`
- Test parsing valid blocklist lines
- Test skipping comments
- Test skipping malformed lines
- Test IPv4 range matching
- Test empty blocklist
- Test gzipped blocklist (future)

#### `config_test.go`
- Test default values
- Test JSON config loading
- Test environment variable overrides
- Test flag precedence over config file
- Test validation errors

### Integration Tests

- Use a well-known, small, well-seeded test torrent (e.g., a Linux ISO)
- Start engine, wait for metadata, verify file list is populated
- Start HTTP server, make a GET request, verify response headers
- Verify Range request returns correct byte range

### Race Detection

Run all tests with `-race` flag to detect data races:
```bash
go test -race ./...
```

## Build Tags

No build tags needed for v1. Future considerations:
- `//go:build !windows` for OMX player (Linux/RPi only)
- `//go:build darwin` for IINA support

## Version Embedding

```go
// main.go
var Version = "dev"  // overridden by -ldflags at build time

// In cobra root command:
rootCmd.Version = Version
```

## Binary Size Optimization

- Use `-ldflags "-s -w"` to strip debug symbols (~30% smaller)
- Expected binary size: ~15-25 MB (anacrolix/torrent is a large dependency)
- UPX compression can reduce to ~5-8 MB if needed

## CI/CD (Future)

GitHub Actions workflow for:
1. Run tests on push
2. Build binaries for all platforms on tag
3. Create GitHub release with binaries

## README.md

```markdown
# peerflix-go

Streaming torrent client for the command line. Written in Go.

## Install

### From binary
Download from [Releases](releases) and place in your PATH.

### From source
\`\`\`bash
go install github.com/mulish/peerflix-go@latest
\`\`\`

## Usage
\`\`\`bash
peerflix-go "magnet:?xt=urn:btih:..." --mpv
peerflix-go movie.torrent --vlc --port 9000
peerflix-go "magnet:..." --list
\`\`\`

## Options
[auto-generated by cobra]
```

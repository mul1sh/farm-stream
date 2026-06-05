package engine

import (
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

// ── Configuration ───────────────────────────────────────────────────────────

// Config holds all settings for the torrent engine.
type Config struct {
	// DownloadDir is where torrent data is stored.
	DownloadDir string

	// MaxConnections is the maximum number of peer connections per torrent.
	MaxConnections int

	// Peers is a list of initial peers to connect to (ip:port).
	Peers []string

	// BlocklistPath is an optional path to a peer blocklist file.
	BlocklistPath string

	// PeerPort is the port to listen for incoming peer connections (0 = random).
	PeerPort int

	// Seed controls whether to continue seeding after download completes.
	Seed bool

	// NoDHT disables the DHT for peer discovery.
	NoDHT bool

	// NoUPnP disables UPnP port mapping.
	NoUPnP bool
}

// ── Data Types ──────────────────────────────────────────────────────────────

// FileEntry represents a single file within a torrent.
type FileEntry struct {
	Index   int
	Name    string // file basename
	Path    string // full path within torrent
	Length  int64
	IsMedia bool
}

// SelectedFile is a file that has been selected for streaming.
type SelectedFile struct {
	FileEntry
	Reader torrent.Reader
}

// EngineStats holds a snapshot of current engine statistics.
type EngineStats struct {
	// Transfer totals
	Downloaded int64
	Uploaded   int64

	// Speeds (bytes/sec)
	DownloadSpeed float64
	UploadSpeed   float64

	// Progress
	TotalLength    int64
	Completed      int64
	Progress       float64 // 0.0 - 1.0
	PiecesTotal    int
	PiecesComplete int

	// Peers
	TotalPeers  int
	ActivePeers int
	PeerStats   []PeerStat

	// Metadata
	Name     string
	InfoHash string
}

// PeerStat holds statistics for a single peer connection.
type PeerStat struct {
	Address       string
	Downloaded    int64
	DownloadSpeed float64
	Choked        bool
	Client        string
}

// CreateOpts holds options for creating a new torrent file.
type CreateOpts struct {
	// PieceLength in bytes (default: 256KB).
	PieceLength int64

	// Trackers is a list of tracker announce URLs.
	Trackers []string

	// Comment is an optional description embedded in the torrent.
	Comment string

	// Private marks the torrent as private (disables DHT/PEX).
	Private bool

	// CreatedBy is the creator string embedded in the torrent.
	CreatedBy string
}

// ── Speed Tracker ───────────────────────────────────────────────────────────

// SpeedTracker computes download/upload speeds by sampling byte deltas.
type SpeedTracker struct {
	mu            sync.Mutex
	lastSample    time.Time
	lastDownload  int64
	lastUpload    int64
	downloadSpeed float64
	uploadSpeed   float64
}

// Update samples current byte counters and recomputes speeds.
func (st *SpeedTracker) Update(downloaded, uploaded int64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(st.lastSample).Seconds()

	if elapsed > 0 && !st.lastSample.IsZero() {
		st.downloadSpeed = float64(downloaded-st.lastDownload) / elapsed
		st.uploadSpeed = float64(uploaded-st.lastUpload) / elapsed

		// Clamp to zero if negative (can happen on counter reset)
		if st.downloadSpeed < 0 {
			st.downloadSpeed = 0
		}
		if st.uploadSpeed < 0 {
			st.uploadSpeed = 0
		}
	}

	st.lastSample = now
	st.lastDownload = downloaded
	st.lastUpload = uploaded
}

// Speeds returns the current download and upload speeds in bytes/sec.
func (st *SpeedTracker) Speeds() (down, up float64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.downloadSpeed, st.uploadSpeed
}

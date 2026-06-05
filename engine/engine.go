// Package engine provides a streaming BitTorrent client built on top of
// anacrolix/torrent. It is designed for real-time data distribution from
// agricultural drones (video feeds, NDVI imagery, weather telemetry,
// air quality samples) as well as general-purpose media streaming.
//
// The engine supports both consuming (downloading/streaming) and producing
// (creating/seeding) torrents, enabling an offline recovery workflow for
// drone data saved to SSD when live streaming fails.
//
// Usage:
//
//	e, err := engine.New(&engine.Config{
//	    DownloadDir:    "/tmp/farm-stream",
//	    MaxConnections: 100,
//	})
//	if err != nil { log.Fatal(err) }
//	defer e.Close(false)
//
//	e.AddTorrent("magnet:?xt=urn:btih:...")
//	<-e.WaitReady()
//
//	selected, _ := e.SelectLargest()
//	// selected.Reader implements io.ReadSeeker — serve via HTTP
package engine

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// peerAddr implements net.Addr for manual peer addition.
type peerAddr string

func (a peerAddr) Network() string { return "tcp" }
func (a peerAddr) String() string  { return string(a) }

// ── Engine ───────────────────────────────────────────────────────────────────

// Engine wraps the anacrolix/torrent client for streaming.
type Engine struct {
	Client       *torrent.Client
	Torrent      *torrent.Torrent
	Cfg          *Config
	ready        chan struct{}
	readyOnce    sync.Once
	StartTime    time.Time
	SpeedTracker *SpeedTracker
}

// New creates a new torrent engine with the given configuration.
func New(config *Config) (*Engine, error) {
	cfg := torrent.NewDefaultClientConfig()

	// Download directory
	if config.DownloadDir != "" {
		if err := os.MkdirAll(config.DownloadDir, 0755); err != nil {
			return nil, fmt.Errorf("cannot create download dir: %w", err)
		}
		cfg.DataDir = config.DownloadDir
	}

	// Connection limits
	if config.MaxConnections > 0 {
		cfg.EstablishedConnsPerTorrent = config.MaxConnections
	}

	// Peer listening port
	if config.PeerPort > 0 {
		cfg.SetListenAddr(fmt.Sprintf(":%d", config.PeerPort))
	}

	// DHT
	if config.NoDHT {
		cfg.NoDHT = true
	}

	// UPnP
	if config.NoUPnP {
		cfg.NoDefaultPortForwarding = true
	}

	// Create client
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot create torrent client: %w", err)
	}

	return &Engine{
		Client:       client,
		Cfg:          config,
		ready:        make(chan struct{}),
		StartTime:    time.Now(),
		SpeedTracker: &SpeedTracker{},
	}, nil
}

// AddTorrent adds a torrent by magnet URI or .torrent file path.
// It spawns a goroutine to wait for metadata and signals via the ready channel.
func (e *Engine) AddTorrent(uri string) error {
	var t *torrent.Torrent
	var err error

	if strings.HasPrefix(uri, "magnet:") {
		t, err = e.Client.AddMagnet(uri)
		if err != nil {
			return fmt.Errorf("invalid magnet URI: %w", err)
		}
	} else {
		// Treat as .torrent file path
		t, err = e.Client.AddTorrentFromFile(uri)
		if err != nil {
			return fmt.Errorf("cannot load torrent file: %w", err)
		}
	}

	e.Torrent = t

	// Add manual peers if configured
	for _, peer := range e.Cfg.Peers {
		e.AddPeer(peer)
	}

	// Wait for metadata in background, then signal ready
	go func() {
		<-t.GotInfo()
		e.readyOnce.Do(func() { close(e.ready) })
	}()

	return nil
}

// WaitReady returns a channel that is closed when torrent metadata is available.
func (e *Engine) WaitReady() <-chan struct{} {
	return e.ready
}

// Files returns all files in the torrent. Blocks until metadata is available.
func (e *Engine) Files() []FileEntry {
	<-e.ready

	files := e.Torrent.Files()
	entries := make([]FileEntry, len(files))

	for i, f := range files {
		name := filepath.Base(f.DisplayPath())
		entries[i] = FileEntry{
			Index:   i,
			Name:    name,
			Path:    f.Path(),
			Length:  f.Length(),
			IsMedia: IsMediaFile(name),
		}
	}

	return entries
}

// SelectFile selects a specific file by index for streaming.
func (e *Engine) SelectFile(index int) (*SelectedFile, error) {
	<-e.ready

	files := e.Torrent.Files()
	if index < 0 || index >= len(files) {
		return nil, fmt.Errorf("file index %d out of range [0, %d)", index, len(files))
	}

	file := files[index]
	file.Download() // Prioritize this file's pieces

	reader := file.NewReader()
	reader.SetResponsive()               // Prioritize current read position
	reader.SetReadahead(5 * 1024 * 1024) // 5MB read-ahead

	name := filepath.Base(file.DisplayPath())

	return &SelectedFile{
		FileEntry: FileEntry{
			Index:   index,
			Name:    name,
			Path:    file.Path(),
			Length:  file.Length(),
			IsMedia: IsMediaFile(name),
		},
		Reader: reader,
	}, nil
}

// NewFileReader creates an independent reader for the file at the given index.
// Each reader has its own seek position — safe for concurrent HTTP clients.
// The caller is responsible for closing the returned reader.
func (e *Engine) NewFileReader(index int) (torrent.Reader, error) {
	<-e.ready

	files := e.Torrent.Files()
	if index < 0 || index >= len(files) {
		return nil, fmt.Errorf("file index %d out of range [0, %d)", index, len(files))
	}

	file := files[index]
	file.Download()

	reader := file.NewReader()
	reader.SetResponsive()
	reader.SetReadahead(5 * 1024 * 1024)

	return reader, nil
}

// FileCount returns the number of files in the torrent. Blocks until metadata is available.
func (e *Engine) FileCount() int {
	<-e.ready
	return len(e.Torrent.Files())
}

// SelectLargest selects the largest file in the torrent for streaming.
func (e *Engine) SelectLargest() (*SelectedFile, error) {
	files := e.Files()
	if len(files) == 0 {
		return nil, fmt.Errorf("torrent contains no files")
	}

	largest := 0
	for i, f := range files {
		if f.Length > files[largest].Length {
			largest = i
		}
	}

	return e.SelectFile(largest)
}

// SelectAll marks all files in the torrent for download.
func (e *Engine) SelectAll() {
	<-e.ready
	for _, f := range e.Torrent.Files() {
		f.Download()
	}
}

// AddPeer manually adds a peer by address (ip:port).
func (e *Engine) AddPeer(addr string) {
	if e.Torrent == nil {
		return
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return
	}

	e.Torrent.AddPeers([]torrent.PeerInfo{{
		Addr: peerAddr(net.JoinHostPort(host, portStr)),
	}})
}

// Stats returns a snapshot of current engine statistics.
func (e *Engine) Stats() EngineStats {
	if e.Torrent == nil {
		return EngineStats{}
	}

	t := e.Torrent
	tStats := t.Stats()

	// Update speed tracker
	downloaded := tStats.BytesReadData.Int64()
	uploaded := tStats.BytesWrittenData.Int64()
	e.SpeedTracker.Update(downloaded, uploaded)
	dlSpeed, ulSpeed := e.SpeedTracker.Speeds()

	// Collect per-peer stats
	peers := make([]PeerStat, 0)
	activePeers := 0

	for _, pc := range t.PeerConns() {
		peerStats := pc.Peer.Stats()

		ps := PeerStat{
			Address:    pc.RemoteAddr.String(),
			Downloaded: peerStats.BytesReadData.Int64(),
			Choked:     false,
		}

		// Approximate active peers by checking download rate
		// (PeerChoking is unexported in anacrolix/torrent)
		if pc.Peer.DownloadRate() > 0 {
			activePeers++
		} else {
			ps.Choked = true
		}

		peers = append(peers, ps)
	}

	// Piece stats
	piecesTotal := t.NumPieces()
	piecesComplete := 0
	for i := 0; i < piecesTotal; i++ {
		if t.Piece(i).State().Complete {
			piecesComplete++
		}
	}

	// Progress
	totalLength := t.Length()
	bytesComplete := t.BytesCompleted()
	var progress float64
	if totalLength > 0 {
		progress = float64(bytesComplete) / float64(totalLength)
	}

	return EngineStats{
		Downloaded:     downloaded,
		Uploaded:       uploaded,
		DownloadSpeed:  dlSpeed,
		UploadSpeed:    ulSpeed,
		TotalLength:    totalLength,
		Completed:      bytesComplete,
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

// Close shuts down the engine. If remove is true, downloaded data is deleted.
func (e *Engine) Close(remove bool) {
	if e.Torrent != nil {
		e.Torrent.Drop()
	}
	if e.Client != nil {
		e.Client.Close()
	}
	if remove && e.Cfg.DownloadDir != "" {
		if SafePath(e.Cfg.DownloadDir) {
			os.RemoveAll(e.Cfg.DownloadDir)
		}
	}
}

// SafePath returns false for dangerous paths that should never be deleted.
func SafePath(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	home, _ := os.UserHomeDir()

	dangerous := []string{"/", "/tmp", home, "/var", "/etc", "/usr", "/bin", "/sbin"}
	for _, d := range dangerous {
		if abs == d {
			return false
		}
	}
	return true
}

// ── Torrent Creation (Producer Side) ────────────────────────────────────────

// CreateTorrent creates a .torrent file from a file or directory.
// This is used for the offline SSD recovery workflow: drone data saved to disk
// is packaged into a torrent for P2P distribution to ground stations.
//
// Returns the path to the created .torrent file.
func CreateTorrent(path string, opts CreateOpts) (string, error) {
	// Validate input path
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot access path: %w", err)
	}

	// Default piece length: 256KB
	pieceLength := opts.PieceLength
	if pieceLength <= 0 {
		pieceLength = 256 * 1024
	}

	// Build metainfo
	mi := metainfo.MetaInfo{
		CreatedBy: opts.CreatedBy,
		Comment:   opts.Comment,
	}
	if mi.CreatedBy == "" {
		mi.CreatedBy = "farm-stream"
	}

	// Set announce / tracker URLs
	if len(opts.Trackers) > 0 {
		mi.Announce = opts.Trackers[0]
		if len(opts.Trackers) > 1 {
			for _, tr := range opts.Trackers {
				mi.AnnounceList = append(mi.AnnounceList, []string{tr})
			}
		}
	}

	// Build the info dict
	builder := metainfo.Info{
		PieceLength: pieceLength,
	}

	if err = builder.BuildFromFilePath(path); err != nil {
		return "", fmt.Errorf("cannot build torrent info: %w", err)
	}

	if opts.Private {
		priv := true
		builder.Private = &priv
	}

	mi.InfoBytes, err = bencode.Marshal(builder)
	if err != nil {
		return "", fmt.Errorf("cannot encode info dict: %w", err)
	}

	// Write .torrent file
	torrentPath := path + ".torrent"
	if info.IsDir() {
		torrentPath = filepath.Join(filepath.Dir(path), filepath.Base(path)+".torrent")
	}

	f, err := os.Create(torrentPath)
	if err != nil {
		return "", fmt.Errorf("cannot create torrent file: %w", err)
	}
	defer f.Close()

	if err := mi.Write(f); err != nil {
		return "", fmt.Errorf("cannot write torrent file: %w", err)
	}

	return torrentPath, nil
}

// SeedTorrent loads a .torrent file and starts seeding it.
// The data must already exist at the path specified in the torrent.
func (e *Engine) SeedTorrent(torrentPath string) error {
	t, err := e.Client.AddTorrentFromFile(torrentPath)
	if err != nil {
		return fmt.Errorf("cannot load torrent for seeding: %w", err)
	}

	e.Torrent = t

	// Wait for info, then verify and start seeding
	go func() {
		<-t.GotInfo()

		// Verify existing data
		t.VerifyData()

		// Mark all pieces for upload
		t.AllowDataUpload()

		e.readyOnce.Do(func() { close(e.ready) })
	}()

	return nil
}

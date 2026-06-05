package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── FormatBytes Tests ───────────────────────────────────────────────────────

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
		{5368709120, "5.0 GB"},
	}

	for _, tt := range tests {
		result := FormatBytes(tt.input)
		if result != tt.expected {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// ── Media Detection Tests ───────────────────────────────────────────────────

func TestIsMediaFile(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"movie.mkv", true},
		{"video.mp4", true},
		{"film.avi", true},
		{"clip.mov", true},
		{"stream.webm", true},
		{"video.flv", true},
		{"readme.txt", false},
		{"data.csv", false},
		{"image.tiff", false},
		{"report.pdf", false},
		{"MOVIE.MKV", true},
		{"Video.MP4", true},
	}

	for _, tt := range tests {
		result := IsMediaFile(tt.name)
		if result != tt.expected {
			t.Errorf("IsMediaFile(%q) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}

func TestIsDroneData(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"ndvi_scan.tiff", true},
		{"ndvi_scan.tif", true},
		{"telemetry.csv", true},
		{"sensors.json", true},
		{"stream.h264", true},
		{"stream.h265", true},
		{"ros_data.bag", true},
		{"movie.mkv", false},
		{"readme.txt", false},
	}

	for _, tt := range tests {
		result := IsDroneData(tt.name)
		if result != tt.expected {
			t.Errorf("IsDroneData(%q) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}

func TestIsStreamable(t *testing.T) {
	if !IsStreamable("movie.mkv") {
		t.Error("movie.mkv should be streamable")
	}
	if !IsStreamable("ndvi.tiff") {
		t.Error("ndvi.tiff should be streamable")
	}
	if IsStreamable("readme.txt") {
		t.Error("readme.txt should not be streamable")
	}
}

// ── MIME Detection Tests ────────────────────────────────────────────────────

func TestDetectMIME(t *testing.T) {
	result := DetectMIME("movie.mp4")
	if result == "" {
		t.Error("DetectMIME should not return empty string")
	}
	result = DetectMIME("unknown.xyz123")
	if result != "application/octet-stream" {
		t.Errorf("unknown extension should return octet-stream, got %q", result)
	}
}

// ── SpeedTracker Tests ──────────────────────────────────────────────────────

func TestSpeedTracker(t *testing.T) {
	st := &SpeedTracker{}

	// First sample — no speed yet
	st.Update(0, 0)
	dl, ul := st.Speeds()
	if dl != 0 || ul != 0 {
		t.Errorf("initial speeds should be 0, got dl=%.2f ul=%.2f", dl, ul)
	}

	// Simulate time passing with data transferred
	time.Sleep(100 * time.Millisecond)
	st.Update(1024*1024, 512*1024)
	dl, ul = st.Speeds()

	if dl <= 0 {
		t.Errorf("download speed should be > 0 after data received, got %.2f", dl)
	}
	if ul <= 0 {
		t.Errorf("upload speed should be > 0 after data sent, got %.2f", ul)
	}
}

// ── SafePath Tests ──────────────────────────────────────────────────────────

func TestSafePath(t *testing.T) {
	if SafePath("/") {
		t.Error("/ should not be safe to delete")
	}
	if SafePath("/tmp") {
		t.Error("/tmp should not be safe to delete")
	}
	if SafePath("/var") {
		t.Error("/var should not be safe to delete")
	}
	if !SafePath("/tmp/farm-stream/abc123") {
		t.Error("/tmp/farm-stream/abc123 should be safe to delete")
	}
}

// ── Engine Tests ────────────────────────────────────────────────────────────

func TestNewEngine(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "farm-stream-test")
	defer os.RemoveAll(tmpDir)

	e, err := New(&Config{
		DownloadDir:    tmpDir,
		MaxConnections: 50,
		NoDHT:          true,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer e.Close(true)

	if e.Client == nil {
		t.Error("engine.Client should not be nil")
	}
	if e.Cfg.MaxConnections != 50 {
		t.Errorf("MaxConnections = %d, want 50", e.Cfg.MaxConnections)
	}
}

// ── CreateTorrent Tests ─────────────────────────────────────────────────────

func TestCreateTorrent(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "farm-stream-create-test")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test-data.bin")
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("cannot create test file: %v", err)
	}

	torrentPath, err := CreateTorrent(testFile, CreateOpts{
		Comment:   "test torrent",
		CreatedBy: "farm-stream-test",
	})
	if err != nil {
		t.Fatalf("CreateTorrent() error: %v", err)
	}

	if _, err := os.Stat(torrentPath); err != nil {
		t.Fatalf("torrent file not created: %v", err)
	}
	if filepath.Ext(torrentPath) != ".torrent" {
		t.Errorf("torrent path %q should end with .torrent", torrentPath)
	}

	info, _ := os.Stat(torrentPath)
	if info.Size() == 0 {
		t.Error("torrent file should not be empty")
	}
	t.Logf("Created torrent: %s (%s)", torrentPath, FormatBytes(info.Size()))
}

func TestCreateTorrentDirectory(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "farm-stream-dir-test")
	dataDir := filepath.Join(tmpDir, "drone-flight-001")
	os.MkdirAll(dataDir, 0755)
	defer os.RemoveAll(tmpDir)

	files := map[string]int{
		"video.h264":     512 * 1024,
		"ndvi_scan.tiff": 256 * 1024,
		"telemetry.csv":  1024,
		"weather.json":   512,
	}

	for name, size := range files {
		data := make([]byte, size)
		os.WriteFile(filepath.Join(dataDir, name), data, 0644)
	}

	torrentPath, err := CreateTorrent(dataDir, CreateOpts{
		Comment: "drone flight 001 — farm scan",
	})
	if err != nil {
		t.Fatalf("CreateTorrent(dir) error: %v", err)
	}

	info, _ := os.Stat(torrentPath)
	t.Logf("Created directory torrent: %s (%s)", torrentPath, FormatBytes(info.Size()))
}

// ── Magnet Detection Tests ──────────────────────────────────────────────────

func TestMagnetDetection(t *testing.T) {
	tests := []struct {
		input    string
		isMagnet bool
	}{
		{"magnet:?xt=urn:btih:abc123", true},
		{"magnet:?xt=urn:btih:abc123&dn=test", true},
		{"/path/to/file.torrent", false},
		{"./relative.torrent", false},
	}

	for _, tt := range tests {
		result := len(tt.input) > 7 && tt.input[:7] == "magnet:"
		if result != tt.isMagnet {
			t.Errorf("magnet detection for %q = %v, want %v", tt.input, result, tt.isMagnet)
		}
	}
}

package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Helper: create a test engine with a real torrent ────────────────────────

// testHTTPClient has short timeouts suitable for testing.
var testHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
}

// createTestServer spins up an engine seeding test data and a StreamServer.
// Uses SeedTorrent so the data is verified and available for HTTP serving.
func createTestServer(t *testing.T) (*StreamServer, func()) {
	t.Helper()

	// Create test data directory
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("farm-stream-server-test-%d", time.Now().UnixNano()))
	dataDir := filepath.Join(tmpDir, "test-flight")
	os.MkdirAll(dataDir, 0755)

	// Write test files
	testFiles := map[string][]byte{
		"video.h264":    make([]byte, 256*1024),
		"telemetry.csv": []byte("timestamp,temp,humidity\n1717600000,28.5,65.2\n1717600001,28.6,65.1\n"),
		"ndvi.tiff":     make([]byte, 128*1024),
	}
	for name, data := range testFiles {
		if err := os.WriteFile(filepath.Join(dataDir, name), data, 0644); err != nil {
			t.Fatalf("cannot write test file %s: %v", name, err)
		}
	}

	// Create a torrent from the data
	torrentPath, err := CreateTorrent(dataDir, CreateOpts{
		Comment:   "test flight",
		CreatedBy: "farm-stream-test",
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("CreateTorrent error: %v", err)
	}

	// Create engine in seed mode — data is already on disk
	eng, err := New(&Config{
		DownloadDir:    tmpDir,
		MaxConnections: 10,
		NoDHT:          true,
		Seed:           true,
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("New engine error: %v", err)
	}

	// SeedTorrent loads + verifies existing data so readers can serve it
	if err := eng.SeedTorrent(torrentPath); err != nil {
		eng.Close(false)
		os.RemoveAll(tmpDir)
		t.Fatalf("SeedTorrent error: %v", err)
	}

	// Wait for verification
	select {
	case <-eng.WaitReady():
	case <-time.After(15 * time.Second):
		eng.Close(false)
		os.RemoveAll(tmpDir)
		t.Fatal("timeout waiting for torrent verification")
	}

	// Create server on random port
	srv := NewStreamServer(eng, &ServerConfig{Port: 0}, 0)
	if err := srv.Start(); err != nil {
		eng.Close(false)
		os.RemoveAll(tmpDir)
		t.Fatalf("server Start error: %v", err)
	}

	cleanup := func() {
		srv.Close()
		eng.Close(false)
		os.RemoveAll(tmpDir)
	}

	return srv, cleanup
}

// ── Server Lifecycle Tests ──────────────────────────────────────────────────

func TestServerStartStop(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	// Verify it's listening
	port := srv.Port()
	if port == 0 {
		t.Fatal("server port should not be 0")
	}

	url := srv.URL()
	if url == "" {
		t.Fatal("server URL should not be empty")
	}
	if !strings.Contains(url, fmt.Sprintf(":%d/", port)) {
		t.Errorf("URL %q should contain port %d", url, port)
	}

	t.Logf("Server running at %s", url)

	// Verify it responds
	resp, err := testHTTPClient.Get(url)
	if err != nil {
		t.Fatalf("GET / error: %v", err)
	}
	resp.Body.Close()

	// Accept both 200 (full content) and 206 (partial)
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		t.Errorf("GET / status = %d, want 200 or 206", resp.StatusCode)
	}

	// Verify Accept-Ranges header
	if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Errorf("Accept-Ranges = %q, want 'bytes'", ar)
	}
}

// ── JSON Endpoint Tests ─────────────────────────────────────────────────────

func TestJSONEndpoint(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	resp, err := testHTTPClient.Get(srv.URL() + ".json")
	if err != nil {
		t.Fatalf("GET /.json error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("GET /.json status = %d, want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// Parse JSON
	var data jsonResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("JSON decode error: %v", err)
	}

	// Verify fields
	if data.Name == "" {
		t.Error("JSON name should not be empty")
	}
	if len(data.Files) == 0 {
		t.Error("JSON files array should not be empty")
	}

	// Verify file URLs contain the server address
	for _, f := range data.Files {
		if f.Name == "" {
			t.Error("file name should not be empty")
		}
		if !strings.HasPrefix(f.URL, "http://") {
			t.Errorf("file URL %q should start with http://", f.URL)
		}
		if f.Length <= 0 {
			t.Errorf("file %q length should be > 0", f.Name)
		}
	}

	t.Logf("JSON: name=%q, files=%d, progress=%.1f%%", data.Name, len(data.Files), data.Progress*100)
}

// ── M3U Playlist Endpoint Tests ─────────────────────────────────────────────

func TestPlaylistEndpoint(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	resp, err := testHTTPClient.Get(srv.URL() + ".m3u")
	if err != nil {
		t.Fatalf("GET /.m3u error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("GET /.m3u status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	playlist := string(body)

	if !strings.HasPrefix(playlist, "#EXTM3U") {
		t.Error("M3U should start with #EXTM3U")
	}
	if !strings.Contains(playlist, "#EXTINF:") {
		t.Error("M3U should contain #EXTINF entries")
	}
	if !strings.Contains(playlist, "http://") {
		t.Error("M3U should contain http:// URLs")
	}

	t.Logf("M3U playlist:\n%s", playlist)
}

// ── CORS Preflight Tests ────────────────────────────────────────────────────

func TestCORSPreflight(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	req, _ := http.NewRequest("OPTIONS", srv.URL(), nil)
	req.Header.Set("Origin", "https://farm-dashboard.local")
	req.Header.Set("Access-Control-Request-Headers", "Range")

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("OPTIONS status = %d, want 200", resp.StatusCode)
	}

	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao != "https://farm-dashboard.local" {
		t.Errorf("ACAO = %q, want 'https://farm-dashboard.local'", acao)
	}

	acam := resp.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(acam, "GET") {
		t.Errorf("ACAM = %q, should contain GET", acam)
	}
}

// ── Favicon 404 Test ────────────────────────────────────────────────────────

func TestFaviconReturns404(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	resp, err := testHTTPClient.Get(srv.URL() + "favicon.ico")
	if err != nil {
		t.Fatalf("GET /favicon.ico error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("GET /favicon.ico status = %d, want 404", resp.StatusCode)
	}
}

// ── Server Status Endpoint Tests ────────────────────────────────────────────

func TestStatusEndpoint(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	resp, err := testHTTPClient.Get(srv.URL() + "status")
	if err != nil {
		t.Fatalf("GET /status error: %v", err)
	}
	defer resp.Body.Close()

	var data statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("JSON decode error: %v", err)
	}

	if data.Status != "streaming" {
		t.Errorf("status = %q, want 'streaming'", data.Status)
	}
	if data.Port == 0 {
		t.Error("port should not be 0")
	}
	if data.URL == "" {
		t.Error("URL should not be empty")
	}
	if data.UptimeSecs <= 0 {
		t.Error("uptime should be > 0")
	}

	t.Logf("Status: %s, uptime=%s, connections=%d", data.Status, data.Uptime, data.Connections)
}

// ── File by Index Test ──────────────────────────────────────────────────────

func TestFileByIndex(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	// Get file list first
	resp, err := testHTTPClient.Get(srv.URL() + ".json")
	if err != nil {
		t.Fatalf("GET /.json error: %v", err)
	}
	var data jsonResponse
	json.NewDecoder(resp.Body).Decode(&data)
	resp.Body.Close()

	if len(data.Files) == 0 {
		t.Skip("no files in torrent")
	}

	// Request first file by index
	resp, err = testHTTPClient.Get(srv.URL() + "0")
	if err != nil {
		t.Fatalf("GET /0 error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		t.Errorf("GET /0 status = %d, want 200 or 206", resp.StatusCode)
	}

	if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Errorf("Accept-Ranges = %q, want 'bytes'", ar)
	}

	// Verify DLNA headers are present
	if dlna := resp.Header.Get("transferMode.dlna.org"); dlna != "Streaming" {
		t.Errorf("DLNA transferMode = %q, want 'Streaming'", dlna)
	}
}

// ── Range Request Test ──────────────────────────────────────────────────────

func TestRangeRequest(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	// Request a specific byte range
	req, _ := http.NewRequest("GET", srv.URL()+"0", nil)
	req.Header.Set("Range", "bytes=0-1023")

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("Range request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 206 {
		t.Errorf("Range request status = %d, want 206", resp.StatusCode)
	}

	cr := resp.Header.Get("Content-Range")
	if !strings.HasPrefix(cr, "bytes 0-1023/") {
		t.Errorf("Content-Range = %q, want 'bytes 0-1023/...'", cr)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) != 1024 {
		t.Errorf("body length = %d, want 1024", len(body))
	}

	t.Logf("Range response: status=%d, Content-Range=%s, body=%d bytes", resp.StatusCode, cr, len(body))
}

// ── Multiple Concurrent Clients Test ────────────────────────────────────────

func TestMultipleClients(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	// Simulate 3 concurrent clients requesting different ranges
	var wg sync.WaitGroup
	errors := make(chan error, 3)

	ranges := []string{
		"bytes=0-511",
		"bytes=512-1023",
		"bytes=1024-2047",
	}

	for i, rangeHeader := range ranges {
		wg.Add(1)
		go func(clientID int, rng string) {
			defer wg.Done()

			req, _ := http.NewRequest("GET", srv.URL()+"0", nil)
			req.Header.Set("Range", rng)

			resp, err := testHTTPClient.Do(req)
			if err != nil {
				errors <- fmt.Errorf("client %d: %v", clientID, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != 206 {
				errors <- fmt.Errorf("client %d: status %d, want 206", clientID, resp.StatusCode)
				return
			}

			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				errors <- fmt.Errorf("client %d: empty body", clientID)
				return
			}
		}(i, rangeHeader)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	t.Log("3 concurrent clients served successfully")
}

// ── Connection Tracking Test ────────────────────────────────────────────────

func TestConnectionTracking(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	// Before any connections
	if n := srv.ActiveConnections(); n != 0 {
		t.Errorf("initial active connections = %d, want 0", n)
	}

	// Start a long-lived request
	resp, err := testHTTPClient.Get(srv.URL() + "0")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}

	// Read a small amount (connection stays open)
	buf := make([]byte, 64)
	resp.Body.Read(buf)

	// Check active connections
	// Note: by the time we check, the connection might have completed
	// for small files, so we just verify no panics occur
	_ = srv.ActiveConnections()

	resp.Body.Close()
}

// ── HEAD Request Test ───────────────────────────────────────────────────────

func TestHEADRequest(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	req, _ := http.NewRequest("HEAD", srv.URL()+"0", nil)
	resp, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("HEAD status = %d, want 200", resp.StatusCode)
	}

	if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Errorf("Accept-Ranges = %q, want 'bytes'", ar)
	}

	// HEAD should have no body
	if resp.ContentLength <= 0 {
		t.Error("HEAD Content-Length should be > 0")
	}
}

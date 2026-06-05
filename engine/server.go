package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Server Configuration ────────────────────────────────────────────────────

// ServerConfig holds settings for the HTTP streaming server.
type ServerConfig struct {
	// Port is the HTTP port to listen on (default 8888, 0 = random).
	Port int

	// Hostname is the bind address ("" = all interfaces).
	Hostname string
}

// ── Stream Server ───────────────────────────────────────────────────────────

// StreamServer serves torrent file data over HTTP with Range support.
// It creates a new torrent.Reader per HTTP request, allowing multiple clients
// to stream simultaneously at different positions without seek contention.
type StreamServer struct {
	engine     *Engine
	cfg        *ServerConfig
	httpServer *http.Server
	listener   net.Listener
	selected   int // index of the default file for GET /
	startTime  time.Time

	// Connection tracking
	mu          sync.Mutex
	activeConns map[string]connInfo
}

type connInfo struct {
	ConnectedAt time.Time
	Path        string
	RemoteAddr  string
}

// NewStreamServer creates a new HTTP streaming server.
func NewStreamServer(engine *Engine, cfg *ServerConfig, selectedIndex int) *StreamServer {
	if cfg.Port == 0 {
		cfg.Port = 8888
	}
	return &StreamServer{
		engine:      engine,
		cfg:         cfg,
		selected:    selectedIndex,
		startTime:   time.Now(),
		activeConns: make(map[string]connInfo),
	}
}

// Start binds to the configured port and begins serving HTTP requests.
// If the port is taken, it falls back to a random available port.
func (s *StreamServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.httpServer = &http.Server{
		Handler: mux,
		// 10-hour timeout for long-lived streaming connections.
		// Matches original peerflix behavior (36000000ms).
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       10 * time.Hour,
		ConnState: func(conn net.Conn, state http.ConnState) {
			if state == http.StateNew {
				conn.SetDeadline(time.Now().Add(10 * time.Hour))
			}
		},
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.Hostname, s.cfg.Port)

	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		// Port taken — fall back to random
		s.listener, err = net.Listen("tcp", s.cfg.Hostname+":0")
		if err != nil {
			return fmt.Errorf("cannot listen: %w", err)
		}
	}

	go s.httpServer.Serve(s.listener)
	return nil
}

// URL returns the LAN-accessible URL of the server.
func (s *StreamServer) URL() string {
	addr := s.listener.Addr().(*net.TCPAddr)
	host := s.cfg.Hostname
	if host == "" {
		host = getLocalIP()
	}
	return fmt.Sprintf("http://%s:%d/", host, addr.Port)
}

// Port returns the actual port the server is listening on.
func (s *StreamServer) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// Close gracefully shuts down the server with a 5-second drain period.
func (s *StreamServer) Close() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// ActiveConnections returns the number of currently active streaming connections.
func (s *StreamServer) ActiveConnections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.activeConns)
}

// ── Request Router ──────────────────────────────────────────────────────────

func (s *StreamServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// CORS preflight
	if r.Method == "OPTIONS" {
		s.handleCORS(w, r)
		return
	}

	// Set CORS headers on all responses
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}

	switch {
	case path == "/favicon.ico":
		http.NotFound(w, r)

	case path == "/.json":
		s.handleJSON(w, r)

	case path == "/.m3u":
		s.handlePlaylist(w, r)

	case path == "/status":
		s.handleStatus(w, r)

	case path == "/":
		// Serve the default selected file
		s.handleFile(w, r, s.selected)

	default:
		// Try to match by index: /0, /1, /2, ...
		trimmed := strings.TrimPrefix(path, "/")
		if index, err := strconv.Atoi(trimmed); err == nil {
			s.handleFile(w, r, index)
			return
		}

		// Try to match by filename
		files := s.engine.Files()
		for _, f := range files {
			if "/"+f.Name == path {
				s.handleFile(w, r, f.Index)
				return
			}
		}

		http.NotFound(w, r)
	}
}

// ── File Streaming Handler ──────────────────────────────────────────────────

func (s *StreamServer) handleFile(w http.ResponseWriter, r *http.Request, index int) {
	files := s.engine.Files()
	if index < 0 || index >= len(files) {
		http.NotFound(w, r)
		return
	}

	file := files[index]

	// Create a per-client reader — each client gets independent seek position
	reader, err := s.engine.NewFileReader(index)
	if err != nil {
		http.Error(w, "file not available", http.StatusServiceUnavailable)
		return
	}
	defer reader.Close()

	// Track this connection
	connID := fmt.Sprintf("%s-%d", r.RemoteAddr, time.Now().UnixNano())
	s.trackConnect(connID, r.RemoteAddr, r.URL.Path)
	defer s.trackDisconnect(connID)

	// Set streaming headers
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", DetectMIME(file.Name))

	// DLNA headers for smart TV / casting compatibility
	w.Header().Set("transferMode.dlna.org", "Streaming")
	w.Header().Set("contentFeatures.dlna.org",
		"DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000")

	// http.ServeContent handles:
	// - Range header parsing → 206 Partial Content
	// - Content-Range response headers
	// - HEAD requests (no body)
	// - If-Modified-Since / If-Range
	// On client disconnect, r.Context() is cancelled, reader.Close() runs via defer
	http.ServeContent(w, r, file.Name, time.Time{}, reader)
}

// ── JSON Stats Endpoint ─────────────────────────────────────────────────────

type jsonResponse struct {
	Name           string     `json:"name"`
	InfoHash       string     `json:"infoHash"`
	TotalLength    int64      `json:"totalLength"`
	Downloaded     int64      `json:"downloaded"`
	Uploaded       int64      `json:"uploaded"`
	DownloadSpeed  float64    `json:"downloadSpeed"`
	UploadSpeed    float64    `json:"uploadSpeed"`
	TotalPeers     int        `json:"totalPeers"`
	ActivePeers    int        `json:"activePeers"`
	Progress       float64    `json:"progress"`
	PiecesTotal    int        `json:"piecesTotal"`
	PiecesComplete int        `json:"piecesComplete"`
	Connections    int        `json:"activeConnections"`
	Files          []jsonFile `json:"files"`
}

type jsonFile struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Length int64  `json:"length"`
}

func (s *StreamServer) handleJSON(w http.ResponseWriter, r *http.Request) {
	stats := s.engine.Stats()
	baseURL := s.URL()

	files := s.engine.Files()
	jsonFiles := make([]jsonFile, len(files))
	for i, f := range files {
		jsonFiles[i] = jsonFile{
			Name:   f.Name,
			URL:    fmt.Sprintf("%s%d", baseURL, f.Index),
			Length: f.Length,
		}
	}

	resp := jsonResponse{
		Name:           stats.Name,
		InfoHash:       stats.InfoHash,
		TotalLength:    stats.TotalLength,
		Downloaded:     stats.Downloaded,
		Uploaded:       stats.Uploaded,
		DownloadSpeed:  stats.DownloadSpeed,
		UploadSpeed:    stats.UploadSpeed,
		TotalPeers:     stats.TotalPeers,
		ActivePeers:    stats.ActivePeers,
		Progress:       stats.Progress,
		PiecesTotal:    stats.PiecesTotal,
		PiecesComplete: stats.PiecesComplete,
		Connections:    s.ActiveConnections(),
		Files:          jsonFiles,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

// ── M3U Playlist Endpoint ───────────────────────────────────────────────────

func (s *StreamServer) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	baseURL := s.URL()
	files := s.engine.Files()

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, f := range files {
		b.WriteString(fmt.Sprintf("#EXTINF:-1,%s\n", f.Name))
		b.WriteString(fmt.Sprintf("%s%d\n", baseURL, f.Index))
	}

	w.Header().Set("Content-Type", "application/x-mpegurl; charset=utf-8")
	w.Write([]byte(b.String()))
}

// ── Server Status Endpoint ──────────────────────────────────────────────────

type statusResponse struct {
	Status      string           `json:"status"`
	Uptime      string           `json:"uptime"`
	UptimeSecs  float64          `json:"uptimeSeconds"`
	Port        int              `json:"port"`
	URL         string           `json:"url"`
	Connections int              `json:"activeConnections"`
	Clients     []statusClient   `json:"clients"`
}

type statusClient struct {
	RemoteAddr  string `json:"remoteAddr"`
	Path        string `json:"path"`
	ConnectedAt string `json:"connectedAt"`
	Duration    string `json:"duration"`
}

func (s *StreamServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	clients := make([]statusClient, 0, len(s.activeConns))
	for _, c := range s.activeConns {
		clients = append(clients, statusClient{
			RemoteAddr:  c.RemoteAddr,
			Path:        c.Path,
			ConnectedAt: c.ConnectedAt.Format(time.RFC3339),
			Duration:    time.Since(c.ConnectedAt).Round(time.Second).String(),
		})
	}
	s.mu.Unlock()

	uptime := time.Since(s.startTime)
	resp := statusResponse{
		Status:      "streaming",
		Uptime:      uptime.Round(time.Second).String(),
		UptimeSecs:  uptime.Seconds(),
		Port:        s.Port(),
		URL:         s.URL(),
		Connections: len(clients),
		Clients:     clients,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

// ── CORS Handler ────────────────────────────────────────────────────────────

func (s *StreamServer) handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
	w.Header().Set("Access-Control-Max-Age", "1728000")
	w.WriteHeader(http.StatusOK)
}

// ── Connection Tracking ─────────────────────────────────────────────────────

func (s *StreamServer) trackConnect(id, remoteAddr, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeConns[id] = connInfo{
		ConnectedAt: time.Now(),
		Path:        path,
		RemoteAddr:  remoteAddr,
	}
}

func (s *StreamServer) trackDisconnect(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeConns, id)
}

// ── LAN IP Detection ────────────────────────────────────────────────────────

// getLocalIP returns the preferred outbound LAN IP address.
func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}

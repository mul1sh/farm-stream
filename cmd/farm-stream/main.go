package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mulish/farm-stream/engine"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// ── ANSI Colors ─────────────────────────────────────────────────────────────

const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Red       = "\033[31m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Blue      = "\033[34m"
	Magenta   = "\033[35m"
	Cyan      = "\033[36m"
	BoldRed   = "\033[1;31m"
	BoldGreen = "\033[1;32m"
	BoldCyan  = "\033[1;34m"
	BoldWhite = "\033[1;37m"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	arg := os.Args[1]

	switch {
	case arg == "--version" || arg == "-V":
		fmt.Printf("farm-stream %s\n", Version)
		os.Exit(0)
	case arg == "--help" || arg == "-h":
		printUsage()
		os.Exit(0)
	case arg == "create":
		if len(os.Args) < 3 {
			fmt.Printf("%s✗ Usage: farm-stream create <file-or-directory>%s\n", Red, Reset)
			os.Exit(1)
		}
		handleCreate(os.Args[2:])
	case arg == "seed":
		if len(os.Args) < 3 {
			fmt.Printf("%s✗ Usage: farm-stream seed <torrent-file>%s\n", Red, Reset)
			os.Exit(1)
		}
		handleSeed(os.Args[2:])
	default:
		handleStream(arg, os.Args[2:])
	}
}

func printUsage() {
	fmt.Printf(`%sfarm-stream%s — Agricultural data streaming via BitTorrent

%sUsage:%s
  farm-stream <magnet-link-or-torrent> [flags]     Stream a torrent
  farm-stream create <file-or-directory>            Create a .torrent file
  farm-stream seed <torrent-file>                   Seed an existing torrent

%sStream Flags:%s
  --list, -l          List files in torrent and exit
  --index=N, -i N     Stream file at index N (default: largest)
  --port=N, -p N      HTTP server port (default: 8888)
  --path=DIR, -f DIR  Download directory (default: temp dir)
  --connections=N     Max peer connections (default: 100)
  --quiet, -q         Minimal output

%sExamples:%s
  farm-stream "magnet:?xt=urn:btih:..." --list
  farm-stream "magnet:?xt=urn:btih:..." --index=0
  farm-stream movie.torrent
  farm-stream create /mnt/ssd/drone-data/flight-2024-06-05/
  farm-stream seed /mnt/ssd/drone-data/flight-2024-06-05.torrent

`, BoldCyan, Reset, Bold, Reset, Bold, Reset, Bold, Reset)
}

// ── Stream Mode ─────────────────────────────────────────────────────────────

func handleStream(uri string, args []string) {
	listMode := false
	fileIndex := -1
	downloadDir := filepath.Join(os.TempDir(), "farm-stream")
	maxConns := 100
	port := 8888
	quiet := false

	for _, arg := range args {
		switch {
		case arg == "--list" || arg == "-l":
			listMode = true
		case strings.HasPrefix(arg, "--index="):
			fmt.Sscanf(strings.TrimPrefix(arg, "--index="), "%d", &fileIndex)
		case strings.HasPrefix(arg, "-i"):
			if len(arg) > 2 {
				fmt.Sscanf(arg[2:], "%d", &fileIndex)
			}
		case strings.HasPrefix(arg, "--port="):
			fmt.Sscanf(strings.TrimPrefix(arg, "--port="), "%d", &port)
		case strings.HasPrefix(arg, "-p"):
			if len(arg) > 2 {
				fmt.Sscanf(arg[2:], "%d", &port)
			}
		case strings.HasPrefix(arg, "--path="):
			downloadDir = strings.TrimPrefix(arg, "--path=")
		case strings.HasPrefix(arg, "-f"):
			if len(arg) > 2 {
				downloadDir = arg[2:]
			}
		case strings.HasPrefix(arg, "--connections="):
			fmt.Sscanf(strings.TrimPrefix(arg, "--connections="), "%d", &maxConns)
		case arg == "--quiet" || arg == "-q":
			quiet = true
		}
	}

	e, err := engine.New(&engine.Config{
		DownloadDir:    downloadDir,
		MaxConnections: maxConns,
	})
	if err != nil {
		fmt.Printf("%s✗ Engine error: %v%s\n", Red, err, Reset)
		os.Exit(1)
	}

	if !quiet {
		fmt.Printf("%s⚡ Adding torrent...%s\n", Yellow, Reset)
	}

	if err := e.AddTorrent(uri); err != nil {
		fmt.Printf("%s✗ Torrent error: %v%s\n", Red, err, Reset)
		e.Close(false)
		os.Exit(2)
	}

	if strings.HasPrefix(uri, "magnet:") && !quiet {
		fmt.Printf("%s⏳ Fetching torrent metadata from peers...%s\n", Yellow, Reset)
	}

	select {
	case <-e.WaitReady():
	case <-time.After(2 * time.Minute):
		fmt.Printf("%s✗ Timeout: could not resolve torrent metadata after 2 minutes%s\n", Red, Reset)
		e.Close(false)
		os.Exit(2)
	}

	if !quiet {
		fmt.Printf("%s✓ Torrent: %s%s%s\n", BoldGreen, Bold, e.Torrent.Name(), Reset)
	}

	// List mode
	if listMode {
		files := e.Files()
		fmt.Printf("\n%sFiles in torrent (%d):%s\n\n", BoldCyan, len(files), Reset)
		for _, f := range files {
			icon := "  "
			if f.IsMedia {
				icon = "▶ "
			} else if engine.IsDroneData(f.Name) {
				icon = "📡"
			}
			fmt.Printf("  %s %s%d%s : %s%s%s : %s%s%s\n",
				icon,
				Bold, f.Index, Reset,
				Magenta, f.Name, Reset,
				Blue, engine.FormatBytes(f.Length), Reset)
		}
		fmt.Println()
		e.Close(false)
		return
	}

	// Select file
	var selected *engine.SelectedFile
	if fileIndex >= 0 {
		selected, err = e.SelectFile(fileIndex)
	} else {
		selected, err = e.SelectLargest()
	}
	if err != nil {
		fmt.Printf("%s✗ File selection error: %v%s\n", Red, err, Reset)
		e.Close(false)
		os.Exit(1)
	}

	if !quiet {
		fmt.Printf("%s▶ Streaming: %s%s%s (%s)\n",
			Green, Bold, selected.Name, Reset, engine.FormatBytes(selected.Length))
		fmt.Printf("%s  Download dir: %s%s\n", Dim, e.Cfg.DownloadDir, Reset)
	}

	// Start HTTP server
	srv := engine.NewStreamServer(e, &engine.ServerConfig{Port: port}, selected.Index)
	if err := srv.Start(); err != nil {
		fmt.Printf("%s✗ Server error: %v%s\n", Red, err, Reset)
		e.Close(false)
		os.Exit(1)
	}
	defer srv.Close()

	if !quiet {
		fmt.Printf("\n%s🌐 Server:%s %s%s%s\n", BoldCyan, Reset, Bold, srv.URL(), Reset)
		fmt.Printf("%s   Stats:%s  %s%s.json%s\n", Dim, Reset, Dim, srv.URL(), Reset)
		fmt.Printf("%s   M3U:%s    %s%s.m3u%s\n", Dim, Reset, Dim, srv.URL(), Reset)
		fmt.Printf("%s   Status:%s %s%sstatus%s\n\n", Dim, Reset, Dim, srv.URL(), Reset)
	}

	// Stats loop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := e.Stats()
			conns := srv.ActiveConnections()
			if !quiet {
				fmt.Printf("\r%s⚡%s ↓ %s%s/s%s  ↑ %s%s/s%s  peers: %s%d/%d%s  clients: %s%d%s  progress: %s%.1f%%%s  pieces: %d/%d     ",
					Yellow, Reset,
					BoldGreen, engine.FormatBytes(int64(stats.DownloadSpeed)), Reset,
					Cyan, engine.FormatBytes(int64(stats.UploadSpeed)), Reset,
					Bold, stats.ActivePeers, stats.TotalPeers, Reset,
					Magenta, conns, Reset,
					BoldGreen, stats.Progress*100, Reset,
					stats.PiecesComplete, stats.PiecesTotal,
				)
			}
		case <-sigCh:
			fmt.Printf("\n\n%s⏏ Shutting down...%s\n", Yellow, Reset)
			stats := e.Stats()
			fmt.Printf("  Downloaded: %s  Uploaded: %s\n",
				engine.FormatBytes(stats.Downloaded), engine.FormatBytes(stats.Uploaded))
			fmt.Printf("  Server connections served: %d\n", srv.ActiveConnections())
			srv.Close()
			e.Close(false)
			fmt.Printf("%s✓ Done.%s\n", BoldGreen, Reset)
			return
		}
	}
}

// ── Create Mode ─────────────────────────────────────────────────────────────

func handleCreate(args []string) {
	path := args[0]

	opts := engine.CreateOpts{}
	for _, arg := range args[1:] {
		switch {
		case strings.HasPrefix(arg, "--tracker="):
			opts.Trackers = append(opts.Trackers, strings.TrimPrefix(arg, "--tracker="))
		case strings.HasPrefix(arg, "--comment="):
			opts.Comment = strings.TrimPrefix(arg, "--comment=")
		case arg == "--private":
			opts.Private = true
		case strings.HasPrefix(arg, "--piece-length="):
			fmt.Sscanf(strings.TrimPrefix(arg, "--piece-length="), "%d", &opts.PieceLength)
		}
	}

	fmt.Printf("%s📦 Creating torrent from: %s%s%s\n", BoldCyan, Bold, path, Reset)

	torrentPath, err := engine.CreateTorrent(path, opts)
	if err != nil {
		fmt.Printf("%s✗ Error: %v%s\n", Red, err, Reset)
		os.Exit(1)
	}

	fmt.Printf("%s✓ Torrent created: %s%s%s\n", BoldGreen, Bold, torrentPath, Reset)

	info, _ := os.Stat(torrentPath)
	if info != nil {
		fmt.Printf("  Size: %s\n", engine.FormatBytes(info.Size()))
	}
	if opts.Comment != "" {
		fmt.Printf("  Comment: %s\n", opts.Comment)
	}
	if opts.Private {
		fmt.Printf("  Private: yes\n")
	}
	fmt.Printf("\n  Seed with: %sfarm-stream seed %s%s\n\n", Bold, torrentPath, Reset)
}

// ── Seed Mode ───────────────────────────────────────────────────────────────

func handleSeed(args []string) {
	torrentPath := args[0]

	downloadDir := filepath.Dir(torrentPath)
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "--path=") {
			downloadDir = strings.TrimPrefix(arg, "--path=")
		}
	}

	e, err := engine.New(&engine.Config{
		DownloadDir:    downloadDir,
		MaxConnections: 100,
		Seed:           true,
	})
	if err != nil {
		fmt.Printf("%s✗ Engine error: %v%s\n", Red, err, Reset)
		os.Exit(1)
	}

	fmt.Printf("%s🌱 Seeding: %s%s%s\n", BoldCyan, Bold, torrentPath, Reset)

	if err := e.SeedTorrent(torrentPath); err != nil {
		fmt.Printf("%s✗ Seed error: %v%s\n", Red, err, Reset)
		e.Close(false)
		os.Exit(1)
	}

	fmt.Printf("%s⏳ Verifying data...%s\n", Yellow, Reset)
	<-e.WaitReady()
	fmt.Printf("%s✓ Verification complete. Seeding...%s\n\n", BoldGreen, Reset)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := e.Stats()
			fmt.Printf("\r%s🌱%s ↑ %s%s/s%s  uploaded: %s%s%s  peers: %s%d%s     ",
				Green, Reset,
				Cyan, engine.FormatBytes(int64(stats.UploadSpeed)), Reset,
				Bold, engine.FormatBytes(stats.Uploaded), Reset,
				Bold, stats.TotalPeers, Reset,
			)
		case <-sigCh:
			fmt.Printf("\n\n%s⏏ Stopping seed...%s\n", Yellow, Reset)
			stats := e.Stats()
			fmt.Printf("  Total uploaded: %s\n", engine.FormatBytes(stats.Uploaded))
			e.Close(false)
			fmt.Printf("%s✓ Done.%s\n", BoldGreen, Reset)
			return
		}
	}
}

# 06 — File Selection & Filtering

## Overview

Torrents often contain multiple files (video, subtitles, NFOs, samples, etc.). File selection determines which file(s) to stream, and filtering controls which files appear in playlists and stats.

This is a port of the file selection logic in `index.js` lines 36-48 and `app.js` lines 151-188.

## Responsibilities

1. List all files in a torrent with metadata
2. Auto-select the largest file (default behavior)
3. Select a specific file by index
4. Select all files (for M3U playlist mode)
5. Interactive file chooser in TTY mode
6. Filter files for playlist/stats output

## File Listing

### Data Structure

```go
type FileEntry struct {
    Index    int     // position in torrent file list
    Name     string  // file basename (e.g., "Movie.mkv")
    Path     string  // full path within torrent (e.g., "MovieDir/Movie.mkv")
    Length   int64   // size in bytes
    IsMedia  bool    // detected as media file by extension
}
```

### Media Detection

Classify files by extension to prioritize media files:

```go
var mediaExtensions = map[string]bool{
    ".mkv":  true,
    ".mp4":  true,
    ".avi":  true,
    ".mov":  true,
    ".wmv":  true,
    ".flv":  true,
    ".webm": true,
    ".m4v":  true,
    ".mpg":  true,
    ".mpeg": true,
    ".ts":   true,
    ".vob":  true,
    ".divx": true,
    ".ogv":  true,
    ".3gp":  true,
}

var subtitleExtensions = map[string]bool{
    ".srt": true,
    ".ass": true,
    ".ssa": true,
    ".sub": true,
    ".vtt": true,
}

func isMediaFile(name string) bool {
    ext := strings.ToLower(filepath.Ext(name))
    return mediaExtensions[ext]
}
```

## Selection Modes

### Mode 1: Auto-Select Largest (Default)

When no `--index` is specified, select the largest file:

```go
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
```

This matches peerflix's behavior in `index.js` line 38-41:
```javascript
index = e.files.reduce(function (a, b) {
    return a.length > b.length ? a : b
})
```

### Mode 2: Select by Index (`--index N`)

User specifies the exact file index:

```go
func (e *Engine) SelectFile(index int) (*SelectedFile, error) {
    files := e.torrent.Files()
    if index < 0 || index >= len(files) {
        return nil, fmt.Errorf("index %d out of range [0, %d)", index, len(files))
    }

    file := files[index]
    file.Download()  // prioritize this file's pieces

    reader := file.NewReader()
    reader.SetReadahead(5 * 1024 * 1024)  // 5MB read-ahead
    reader.SetResponsive()  // prioritize current read position

    return &SelectedFile{
        FileEntry: FileEntry{
            Index:  index,
            Name:   file.DisplayPath(),
            Path:   file.Path(),
            Length: file.Length(),
        },
        Reader: reader,
    }, nil
}
```

### Mode 3: Select All (`--all`)

Download and serve all files as a playlist:

```go
func (e *Engine) SelectAll() {
    for _, f := range e.torrent.Files() {
        f.Download()
    }
}
```

In this mode, the HTTP server:
- Serves `/.m3u` as the primary URL (given to player)
- Each file is accessible at `/<index>`
- The player reads the M3U and plays files in order

### Mode 4: Interactive Selection (`--list` with TTY)

When `--list` is used and stdin is a TTY:

```go
func interactiveFileSelect(engine *Engine) (int, error) {
    files := engine.Files()

    // Sort alphabetically for display
    sorted := make([]FileEntry, len(files))
    copy(sorted, files)
    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i].Path < sorted[j].Path
    })

    // Display
    fmt.Println("\nFiles in torrent:\n")
    for _, f := range sorted {
        sizeStr := formatBytes(f.Length)
        icon := "  "
        if f.IsMedia {
            icon = "▶ "
        }
        fmt.Printf("  %s%s%d%s : %s%s%s : %s%s%s\n",
            icon, bold, f.Index, reset,
            magenta, f.Name, reset,
            blue, sizeStr, reset)
    }

    // Prompt
    fmt.Printf("\nSelect file [0-%d]: ", len(files)-1)

    var choice int
    fmt.Scan(&choice)

    if choice < 0 || choice >= len(files) {
        return 0, fmt.Errorf("invalid selection: %d", choice)
    }

    return choice, nil
}
```

### Mode 5: Non-Interactive List (`--list` without TTY)

Print files and exit:

```go
func listFiles(engine *Engine) {
    files := engine.Files()
    for _, f := range files {
        fmt.Printf("%s%d%s : %s%s%s : %s%s%s\n",
            bold, f.Index, reset,
            magenta, f.Name, reset,
            blue, formatBytes(f.Length), reset)
    }
}
```

## File Filtering

Filters control which files appear in playlist and stats endpoints.

Default filter: include all files.

```go
// DefaultFilter includes all files
func DefaultFilter(f FileEntry) bool {
    return true
}

// MediaFilter includes only media files
func MediaFilter(f FileEntry) bool {
    return f.IsMedia
}
```

The filter is passed to `ServerConfig` and applied in `.m3u` and `.json` endpoints:

```go
filteredFiles := make([]FileEntry, 0)
for _, f := range engine.Files() {
    if config.Filter(f) {
        filteredFiles = append(filteredFiles, f)
    }
}
```

## Edge Cases

1. **Empty torrent**: Return error "torrent contains no files"
2. **Single file**: Auto-select it regardless of `--index`
3. **Index out of range**: Return error with valid range
4. **All files are tiny (no video)**: Select largest anyway, warn user
5. **Torrent with directory structure**: `Name` uses basename, `Path` uses full relative path

## Read-Ahead Tuning

The read-ahead value determines how many bytes ahead of the current read position are pre-fetched from the swarm:

| Scenario | Read-Ahead | Rationale |
|----------|-----------|-----------|
| Default | 5 MB | Good balance for HD video |
| Paused | 0 | Stop downloading |
| 4K video | 10 MB | Higher bitrate needs more buffer |
| Slow connection | 2 MB | Don't waste bandwidth on future pieces |

For v1, use a fixed 5MB. Future versions could auto-tune based on bitrate.

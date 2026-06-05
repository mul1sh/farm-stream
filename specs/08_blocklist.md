# 08 — Peer Blocklist

## Overview

Blocklists prevent connections to known bad peers (e.g., monitoring nodes, known anti-piracy trackers). This is a port of the `parseBlocklist` function in `index.js` lines 10-24.

## Responsibilities

1. Parse blocklist files in standard P2P blocklist format
2. Apply blocklist to the torrent engine configuration
3. Support the common eMule/PeerGuardian text format

## Blocklist Format

The standard format (used by PeerGuardian, eMule, and most blocklist providers):

```
# Comment lines start with #
Some Description:1.2.3.4-1.2.3.255
Another Range:10.0.0.0-10.0.0.255
IPv6 Example:2001:db8::1-2001:db8::ffff
```

Each non-comment line has the format:
```
<description>:<start_ip>-<end_ip>
```

## Interface

```go
type BlocklistEntry struct {
    Start net.IP
    End   net.IP
}

// ParseBlocklist reads a blocklist file and returns IP ranges.
func ParseBlocklist(filename string) ([]BlocklistEntry, error)

// IsBlocked checks if an IP falls within any blocked range.
func IsBlocked(ip net.IP, blocklist []BlocklistEntry) bool
```

## Implementation

### Parsing

```go
func ParseBlocklist(filename string) ([]BlocklistEntry, error) {
    data, err := os.ReadFile(filename)
    if err != nil {
        return nil, fmt.Errorf("cannot read blocklist: %w", err)
    }

    var blocklist []BlocklistEntry
    // Regex matching the original peerflix pattern:
    // /^\s*[^#].*?\s*:\s*([a-f0-9.:]+?)\s*-\s*([a-f0-9.:]+?)\s*$/
    re := regexp.MustCompile(`^\s*[^#].*?\s*:\s*([a-f0-9.:]+?)\s*-\s*([a-f0-9.:]+?)\s*$`)

    scanner := bufio.NewScanner(bytes.NewReader(data))
    lineNum := 0
    for scanner.Scan() {
        lineNum++
        line := scanner.Text()

        matches := re.FindStringSubmatch(line)
        if matches == nil {
            continue
        }

        startIP := net.ParseIP(matches[1])
        endIP := net.ParseIP(matches[2])

        if startIP == nil || endIP == nil {
            continue  // skip malformed lines silently
        }

        blocklist = append(blocklist, BlocklistEntry{
            Start: startIP,
            End:   endIP,
        })
    }

    return blocklist, nil
}
```

### Checking

```go
func IsBlocked(ip net.IP, blocklist []BlocklistEntry) bool {
    for _, entry := range blocklist {
        if bytesCompare(ip, entry.Start) >= 0 && bytesCompare(ip, entry.End) <= 0 {
            return true
        }
    }
    return false
}

func bytesCompare(a, b net.IP) int {
    // Normalize to 16-byte representation
    a = a.To16()
    b = b.To16()
    return bytes.Compare(a, b)
}
```

## Integration with anacrolix/torrent

`anacrolix/torrent` supports IP blocklists via `ClientConfig.IPBlocklist`:

```go
import "github.com/anacrolix/iplist"

func applyBlocklist(cfg *torrent.ClientConfig, path string) error {
    entries, err := ParseBlocklist(path)
    if err != nil {
        return err
    }

    // Convert to anacrolix iplist format
    ranges := make([]iplist.Range, len(entries))
    for i, e := range entries {
        ranges[i] = iplist.Range{
            First: e.Start,
            Last:  e.End,
        }
    }

    cfg.IPBlocklist = iplist.New(ranges)
    return nil
}
```

Alternatively, `anacrolix/torrent` can load blocklists directly:

```go
import "github.com/anacrolix/iplist"

func applyBlocklistDirect(cfg *torrent.ClientConfig, path string) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close()

    bl, err := iplist.NewFromReader(f)
    if err != nil {
        return err
    }

    cfg.IPBlocklist = bl
    return nil
}
```

Check if `anacrolix/iplist` can parse the standard format natively — if so, use it directly instead of custom parsing.

## Future: Gzipped Blocklists

The original peerflix has a TODO for gzipped blocklist support. Implement in Go:

```go
func ParseBlocklist(filename string) ([]BlocklistEntry, error) {
    f, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer f.Close()

    var reader io.Reader = f

    // Detect gzip
    if strings.HasSuffix(filename, ".gz") {
        gzReader, err := gzip.NewReader(f)
        if err != nil {
            return nil, fmt.Errorf("cannot decompress blocklist: %w", err)
        }
        defer gzReader.Close()
        reader = gzReader
    }

    return parseBlocklistReader(reader)
}
```

## Performance

For large blocklists (100K+ entries), linear scan in `IsBlocked` is too slow. However, since we delegate to `anacrolix/iplist`, which uses a sorted list with binary search, performance is O(log n) per check.

## CLI Usage

```
peerflix-go "magnet:..." --blocklist /path/to/blocklist.txt
peerflix-go "magnet:..." -b /path/to/blocklist.p2p.gz
```

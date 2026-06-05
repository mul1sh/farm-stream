package engine

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

// ── Byte Formatting ─────────────────────────────────────────────────────────

const (
	kB = 1024
	mB = kB * 1024
	gB = mB * 1024
	tB = gB * 1024
)

// FormatBytes returns a human-readable byte size string.
func FormatBytes(b int64) string {
	switch {
	case b >= tB:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(tB))
	case b >= gB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gB))
	case b >= mB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mB))
	case b >= kB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ── Media File Detection ────────────────────────────────────────────────────

// mediaExtensions lists known video/media file extensions.
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

// droneDataExtensions lists data formats produced by agricultural drones.
// Processing of these formats is deferred to future sprint packages.
var droneDataExtensions = map[string]bool{
	".tiff":    true, // GeoTIFF — NDVI, multispectral imagery
	".tif":     true,
	".geotiff": true,
	".csv":     true, // Sensor telemetry (temperature, humidity, air quality)
	".json":    true, // Structured sensor data
	".h264":    true, // Raw video stream
	".h265":    true,
	".bag":     true, // ROS bag files (robotic sensor data)
}

// IsMediaFile returns true if the file has a known video extension.
func IsMediaFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return mediaExtensions[ext]
}

// IsDroneData returns true if the file has a known drone data extension.
func IsDroneData(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return droneDataExtensions[ext]
}

// IsStreamable returns true if the file is either media or drone data.
func IsStreamable(name string) bool {
	return IsMediaFile(name) || IsDroneData(name)
}

// ── MIME Detection ──────────────────────────────────────────────────────────

// DetectMIME returns the MIME type for a filename based on its extension.
func DetectMIME(filename string) string {
	ext := filepath.Ext(filename)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return mimeType
}

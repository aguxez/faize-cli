//go:build darwin

package vm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SyncClipboardToDir reads the macOS clipboard and writes contents to the specified directory.
// It checks for image (PNG) data first, then falls back to text content.
// Files written:
//   - clipboard-image: PNG image data (if clipboard contains an image)
//   - clipboard-text: text content (always attempted)
//   - clipboard-meta: content type + timestamp metadata
func SyncClipboardToDir(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create clipboard dir: %w", err)
	}

	// Remove stale image file before sync so the VM can't serve old data
	os.Remove(filepath.Join(dir, "clipboard-image"))

	hasImage := syncClipboardImage(dir)
	hasText := syncClipboardText(dir)

	// Write metadata
	contentType := "none"
	if hasImage {
		contentType = "image/png"
	} else if hasText {
		contentType = "text/plain"
	}
	meta := fmt.Sprintf("%s\n%d\n", contentType, time.Now().UnixNano())
	metaPath := filepath.Join(dir, "clipboard-meta")
	if err := os.WriteFile(metaPath, []byte(meta), 0644); err != nil {
		return fmt.Errorf("failed to write clipboard meta: %w", err)
	}

	return nil
}

// syncClipboardImage attempts to read image data from the macOS clipboard.
// Uses NSImage to load any image format (PNG, TIFF, JPEG, etc.) from the
// pasteboard, writes as TIFF, then converts to PNG via sips.
// The script is piped via stdin (not -e) to avoid osascript parse issues
// with multi-line scripts passed as command-line arguments.
// Returns true if image data was found and written successfully.
func syncClipboardImage(dir string) bool {
	imgPath := filepath.Join(dir, "clipboard-image")
	const tempTiff = "/tmp/faize_clipboard.tiff"

	script := `use framework "AppKit"
set pb to current application's NSPasteboard's generalPasteboard()
set img to current application's NSImage's alloc()'s initWithPasteboard:pb
if img is missing value then
	error "no image"
end if
set tiffData to img's TIFFRepresentation
tiffData's writeToFile:"/tmp/faize_clipboard.tiff" atomically:true
return "/tmp/faize_clipboard.tiff"
`

	cmd := exec.Command("osascript")
	cmd.Stdin = strings.NewReader(script)
	if err := cmd.Run(); err != nil {
		return false
	}
	defer os.Remove(tempTiff)

	// Convert TIFF to PNG using sips (built into macOS)
	sipsCmd := exec.Command("sips", "-s", "format", "png", tempTiff, "--out", imgPath)
	if sipsErr := sipsCmd.Run(); sipsErr != nil {
		return false
	}

	if info, err := os.Stat(imgPath); err != nil || info.Size() == 0 {
		return false
	}

	return true
}

// syncClipboardText reads text content from the macOS clipboard.
// Returns true if text was found and written successfully.
func syncClipboardText(dir string) bool {
	cmd := exec.Command("pbpaste")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return false
	}

	textPath := filepath.Join(dir, "clipboard-text")
	if err := os.WriteFile(textPath, output, 0644); err != nil {
		return false
	}

	return true
}

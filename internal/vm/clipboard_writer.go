//go:build darwin

package vm

import (
	"fmt"
	"io"
	"os"
)

// ClipboardWriter wraps an io.Writer to detect Ctrl+V (0x16) keypresses
// in the input stream and sync the host clipboard to VirtioFS.
// All bytes are always forwarded to the underlying writer.
//
// ClipboardWriter is not safe for concurrent use from multiple goroutines.
type ClipboardWriter struct {
	w            io.Writer
	clipboardDir string
}

// NewClipboardWriter creates a ClipboardWriter that syncs clipboard on Ctrl+V.
func NewClipboardWriter(w io.Writer, clipboardDir string) *ClipboardWriter {
	return &ClipboardWriter{
		w:            w,
		clipboardDir: clipboardDir,
	}
}

// Write processes input bytes, triggering clipboard sync when 0x16 is detected.
// All bytes (including 0x16) are forwarded to the underlying writer.
func (c *ClipboardWriter) Write(p []byte) (n int, err error) {
	for _, b := range p {
		if b == 0x16 && c.clipboardDir != "" {
			if err := SyncClipboardToDir(c.clipboardDir); err != nil {
				fmt.Fprintf(os.Stderr, "[clipboard] sync error: %v\r\n", err)
			}
			break // only need to sync once per Write call
		}
	}
	return c.w.Write(p)
}

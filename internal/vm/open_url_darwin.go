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

// watchOpenURL polls the bootstrap directory for URL open requests from the VM guest.
// The guest writes a URL to "open-url" in the bootstrap dir; this function reads it,
// validates it (https-only), opens it in the host browser, and removes the file as
// acknowledgment. Runs until the done channel is closed.
func watchOpenURL(done <-chan struct{}, bootstrapDir string) {
	if bootstrapDir == "" {
		return
	}

	urlFile := filepath.Join(bootstrapDir, "open-url")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			data, err := os.ReadFile(urlFile)
			if err != nil {
				continue // file doesn't exist yet, normal
			}

			url := strings.TrimSpace(string(data))
			if url == "" {
				_ = os.Remove(urlFile)
				continue
			}

			// Remove file first to acknowledge receipt to guest
			_ = os.Remove(urlFile)

			if !isURLAllowed(url) {
				fmt.Fprintf(os.Stderr, "[faize] Blocked URL open request (not https): %s\r\n", url)
				continue
			}

			fmt.Fprintf(os.Stderr, "[faize] Opening in browser: %s\r\n", url)
			_ = exec.Command("open", url).Start()
		}
	}
}

// isURLAllowed validates that a URL uses the https scheme.
// Blocks file://, javascript:, http://, and all other schemes.
func isURLAllowed(url string) bool {
	return strings.HasPrefix(url, "https://")
}

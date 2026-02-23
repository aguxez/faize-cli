//go:build darwin

package vm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// parseOAuthRedirect extracts the localhost port from an OAuth authorization URL's
// redirect_uri parameter. Returns the port and true if redirect_uri is
// http://localhost:<port>/..., otherwise returns ("", false).
func parseOAuthRedirect(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}

	redirectURI := u.Query().Get("redirect_uri")
	if redirectURI == "" {
		return "", false
	}

	r, err := url.Parse(redirectURI)
	if err != nil {
		return "", false
	}

	if r.Scheme != "http" {
		return "", false
	}

	host := r.Hostname()
	port := r.Port()
	if host != "localhost" || port == "" {
		return "", false
	}

	n, err := strconv.Atoi(port)
	if err != nil || n < 1024 || n > 65535 {
		return "", false
	}

	return port, true
}

// startOAuthRelay starts an HTTP server on 127.0.0.1:<port> that captures a single
// OAuth callback request, writes the full reconstructed URL to bootstrapDir/auth-callback,
// and responds with a success page. Shuts down after one request, on done channel close,
// or after a 5-minute timeout.
func startOAuthRelay(done <-chan struct{}, bootstrapDir string, port string) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	handled := make(chan struct{})
	var once sync.Once

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fired := false
		once.Do(func() { fired = true })
		if !fired {
			http.Error(w, "already handled", http.StatusGone)
			return
		}

		reconstructed := "http://localhost:" + port + r.URL.RequestURI()

		callbackFile := filepath.Join(bootstrapDir, "auth-callback")
		_ = os.WriteFile(callbackFile, []byte(reconstructed), 0o600)

		debugLog("OAuth callback received, relaying to VM")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!DOCTYPE html><html><body><p>Authentication successful. You can close this tab.</p></body></html>")

		close(handled)
	})

	srv := &http.Server{Handler: mux}

	go func() {
		select {
		case <-handled:
		case <-done:
		case <-time.After(5 * time.Minute):
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	go func() { _ = srv.Serve(ln) }()
	return nil
}

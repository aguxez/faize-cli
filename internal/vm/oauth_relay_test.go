//go:build darwin

package vm

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestParseOAuthRedirect(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantPort  string
		wantMatch bool
	}{
		{
			name:      "standard OAuth URL",
			rawURL:    "https://auth.example.com/authorize?client_id=abc&redirect_uri=http%3A%2F%2Flocalhost%3A38449%2Fcallback&state=xyz",
			wantPort:  "38449",
			wantMatch: true,
		},
		{
			name:      "different port",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A12345%2Fcallback",
			wantPort:  "12345",
			wantMatch: true,
		},
		{
			name:      "redirect_uri with path and query",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Foauth%2Fcallback%3Ffoo%3Dbar",
			wantPort:  "8080",
			wantMatch: true,
		},
		{
			name:      "no redirect_uri param",
			rawURL:    "https://auth.example.com/authorize?client_id=abc",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "non-localhost redirect",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Fexample.com%3A8080%2Fcallback",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "HTTPS redirect_uri",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=https%3A%2F%2Flocalhost%3A8080%2Fcallback",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "localhost without port",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%2Fcallback",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "empty URL",
			rawURL:    "",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "malformed URL",
			rawURL:    "://not-a-url",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "127.0.0.1 instead of localhost",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2F127.0.0.1%3A8080%2Fcallback",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "privileged port",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A80%2Fcallback",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "port zero",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A0%2Fcallback",
			wantPort:  "",
			wantMatch: false,
		},
		{
			name:      "port overflow",
			rawURL:    "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A99999%2Fcallback",
			wantPort:  "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort, gotMatch := parseOAuthRedirect(tt.rawURL)
			if gotMatch != tt.wantMatch {
				t.Errorf("parseOAuthRedirect(%q) match = %v, want %v", tt.rawURL, gotMatch, tt.wantMatch)
			}
			if gotPort != tt.wantPort {
				t.Errorf("parseOAuthRedirect(%q) port = %q, want %q", tt.rawURL, gotPort, tt.wantPort)
			}
		})
	}
}

func TestStartOAuthRelay(t *testing.T) {
	// Pick a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	tmpDir := t.TempDir()
	done := make(chan struct{})
	defer close(done)

	portStr := fmt.Sprintf("%d", port)
	if err := startOAuthRelay(done, tmpDir, portStr); err != nil {
		t.Fatalf("startOAuthRelay: %v", err)
	}

	// Hit the relay
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?code=abc", port))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Check the callback file was written
	data, err := os.ReadFile(filepath.Join(tmpDir, "auth-callback"))
	if err != nil {
		t.Fatalf("read auth-callback: %v", err)
	}

	want := "http://localhost:" + portStr + "/callback?code=abc"
	if string(data) != want {
		t.Errorf("auth-callback = %q, want %q", string(data), want)
	}
}

func TestStartOAuthRelayPortConflict(t *testing.T) {
	// Bind a port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	portStr := fmt.Sprintf("%d", port)

	done := make(chan struct{})
	defer close(done)

	// Should fail because port is already bound
	if err := startOAuthRelay(done, t.TempDir(), portStr); err == nil {
		t.Error("expected error for occupied port, got nil")
	}
}

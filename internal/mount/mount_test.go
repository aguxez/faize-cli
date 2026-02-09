package mount

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	tests := []struct {
		name     string
		spec     string
		want     *Mount
		wantErr  bool
		errMatch string
	}{
		{
			name: "simple path with tilde",
			spec: "~/.npmrc",
			want: &Mount{
				Source:   filepath.Clean(filepath.Join(homeDir, ".npmrc")),
				Target:   filepath.Clean(filepath.Join(homeDir, ".npmrc")),
				ReadOnly: true,
			},
		},
		{
			name: "path with rw flag",
			spec: "~/.cache/pip:rw",
			want: &Mount{
				Source:   filepath.Clean(filepath.Join(homeDir, ".cache/pip")),
				Target:   filepath.Clean(filepath.Join(homeDir, ".cache/pip")),
				ReadOnly: false,
			},
		},
		{
			name: "path with ro flag",
			spec: "~/.npmrc:ro",
			want: &Mount{
				Source:   filepath.Clean(filepath.Join(homeDir, ".npmrc")),
				Target:   filepath.Clean(filepath.Join(homeDir, ".npmrc")),
				ReadOnly: true,
			},
		},
		{
			name: "explicit source and target with ro",
			spec: "/host/path:/guest/path:ro",
			want: &Mount{
				Source:   "/host/path",
				Target:   "/guest/path",
				ReadOnly: true,
			},
		},
		{
			name: "explicit source and target with rw",
			spec: "/host/path:/guest/path:rw",
			want: &Mount{
				Source:   "/host/path",
				Target:   "/guest/path",
				ReadOnly: false,
			},
		},
		{
			name: "source and target without mode",
			spec: "/host/path:/guest/path",
			want: &Mount{
				Source:   "/host/path",
				Target:   "/guest/path",
				ReadOnly: true,
			},
		},
		{
			name: "absolute path defaults to read-only",
			spec: "/etc/hosts",
			want: &Mount{
				Source:   "/etc/hosts",
				Target:   "/etc/hosts",
				ReadOnly: true,
			},
		},
		{
			name:     "empty spec",
			spec:     "",
			wantErr:  true,
			errMatch: "cannot be empty",
		},
		{
			name:     "invalid mode",
			spec:     "/path:/target:invalid",
			wantErr:  true,
			errMatch: "invalid mode",
		},
		{
			name:     "too many colons",
			spec:     "/path:/target:ro:extra",
			wantErr:  true,
			errMatch: "too many colons",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error but got none")
					return
				}
				if tt.errMatch != "" && !contains(err.Error(), tt.errMatch) {
					t.Errorf("Parse() error = %v, want error containing %q", err, tt.errMatch)
				}
				return
			}

			if err != nil {
				t.Errorf("Parse() unexpected error = %v", err)
				return
			}

			if got.Source != tt.want.Source {
				t.Errorf("Parse() Source = %v, want %v", got.Source, tt.want.Source)
			}
			if got.Target != tt.want.Target {
				t.Errorf("Parse() Target = %v, want %v", got.Target, tt.want.Target)
			}
			if got.ReadOnly != tt.want.ReadOnly {
				t.Errorf("Parse() ReadOnly = %v, want %v", got.ReadOnly, tt.want.ReadOnly)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "tilde expansion",
			path: "~/.npmrc",
			want: filepath.Clean(filepath.Join(homeDir, ".npmrc")),
		},
		{
			name: "absolute path",
			path: "/etc/hosts",
			want: "/etc/hosts",
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandPath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expandPath() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("expandPath() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("expandPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

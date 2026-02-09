package mount

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewValidator(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	// Helper to resolve path through symlinks (for expected values)
	resolvePath := func(path string) string {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return filepath.Clean(path)
		}
		return resolved
	}

	sshPath := resolvePath(filepath.Join(homeDir, ".ssh"))
	etcPath := resolvePath("/etc")

	tests := []struct {
		name         string
		blockedPaths []string
		wantPaths    []string
		wantErr      bool
	}{
		{
			name:         "single path with tilde",
			blockedPaths: []string{"~/.ssh"},
			wantPaths:    []string{sshPath},
		},
		{
			name:         "multiple paths",
			blockedPaths: []string{"~/.ssh", "/etc"},
			wantPaths: []string{
				sshPath,
				etcPath,
			},
		},
		{
			name:         "empty paths are skipped",
			blockedPaths: []string{"~/.ssh", "", "/etc"},
			wantPaths: []string{
				sshPath,
				etcPath,
			},
		},
		{
			name:         "empty list",
			blockedPaths: []string{},
			wantPaths:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewValidator(tt.blockedPaths)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewValidator() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("NewValidator() unexpected error = %v", err)
				return
			}

			if len(got.blockedPaths) != len(tt.wantPaths) {
				t.Errorf("NewValidator() blocked paths count = %d, want %d", len(got.blockedPaths), len(tt.wantPaths))
				return
			}

			for i, want := range tt.wantPaths {
				if got.blockedPaths[i] != want {
					t.Errorf("NewValidator() blocked path[%d] = %v, want %v", i, got.blockedPaths[i], want)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	sshPath := filepath.Clean(filepath.Join(homeDir, ".ssh"))
	sshKeyPath := filepath.Clean(filepath.Join(homeDir, ".ssh/id_rsa"))
	npmrcPath := filepath.Clean(filepath.Join(homeDir, ".npmrc"))

	validator, err := NewValidator([]string{"~/.ssh", "/etc"})
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name     string
		mount    *Mount
		wantErr  bool
		errMatch string
	}{
		{
			name: "allowed path",
			mount: &Mount{
				Source:   npmrcPath,
				Target:   npmrcPath,
				ReadOnly: true,
			},
			wantErr: false,
		},
		{
			name: "blocked path exact match",
			mount: &Mount{
				Source:   sshPath,
				Target:   sshPath,
				ReadOnly: true,
			},
			wantErr:  true,
			errMatch: "protected path",
		},
		{
			name: "blocked path subdirectory",
			mount: &Mount{
				Source:   sshKeyPath,
				Target:   sshKeyPath,
				ReadOnly: true,
			},
			wantErr:  true,
			errMatch: "protected path",
		},
		{
			name: "blocked system path",
			mount: &Mount{
				Source:   "/etc/hosts",
				Target:   "/etc/hosts",
				ReadOnly: true,
			},
			wantErr:  true,
			errMatch: "protected path",
		},
		{
			name:     "nil mount",
			mount:    nil,
			wantErr:  true,
			errMatch: "cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.mount)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
					return
				}
				if tt.errMatch != "" && !contains(err.Error(), tt.errMatch) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMatch)
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestIsUnderOrEqual(t *testing.T) {
	tests := []struct {
		name     string
		testPath string
		basePath string
		want     bool
	}{
		{
			name:     "exact match",
			testPath: "/home/user/.ssh",
			basePath: "/home/user/.ssh",
			want:     true,
		},
		{
			name:     "subdirectory",
			testPath: "/home/user/.ssh/id_rsa",
			basePath: "/home/user/.ssh",
			want:     true,
		},
		{
			name:     "not under - similar prefix",
			testPath: "/home/user/.sshrc",
			basePath: "/home/user/.ssh",
			want:     false,
		},
		{
			name:     "completely different",
			testPath: "/var/log",
			basePath: "/home/user/.ssh",
			want:     false,
		},
		{
			name:     "parent directory",
			testPath: "/home/user",
			basePath: "/home/user/.ssh",
			want:     false,
		},
		{
			name:     "nested subdirectory",
			testPath: "/home/user/.ssh/config.d/personal",
			basePath: "/home/user/.ssh",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUnderOrEqual(tt.testPath, tt.basePath)
			if got != tt.want {
				t.Errorf("isUnderOrEqual(%q, %q) = %v, want %v", tt.testPath, tt.basePath, got, tt.want)
			}
		})
	}
}

func TestValidateSymlinkBypass(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir, err := os.MkdirTemp("", "validator-symlink-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a "blocked" directory to simulate a protected path
	blockedDir := filepath.Join(tmpDir, "blocked-secrets")
	if err := os.MkdirAll(blockedDir, 0755); err != nil {
		t.Fatalf("Failed to create blocked directory: %v", err)
	}

	// Create a symlink that points to the blocked directory
	symlinkPath := filepath.Join(tmpDir, "innocent-looking-link")
	if err := os.Symlink(blockedDir, symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Create validator with the blocked directory
	validator, err := NewValidator([]string{blockedDir})
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name     string
		mount    *Mount
		wantErr  bool
		errMatch string
	}{
		{
			name: "symlink to blocked path should be blocked",
			mount: &Mount{
				Source:   symlinkPath,
				Target:   "/mnt/data",
				ReadOnly: true,
			},
			wantErr:  true,
			errMatch: "resolves to protected path",
		},
		{
			name: "direct blocked path should be blocked",
			mount: &Mount{
				Source:   blockedDir,
				Target:   "/mnt/data",
				ReadOnly: true,
			},
			wantErr:  true,
			errMatch: "protected path",
		},
		{
			name: "allowed path should pass",
			mount: &Mount{
				Source:   tmpDir,
				Target:   "/mnt/data",
				ReadOnly: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.mount)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
					return
				}
				if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMatch)
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Load config without a config file present
	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check defaults
	assert.Equal(t, 2, cfg.Defaults.CPUs)
	assert.Equal(t, "4GB", cfg.Defaults.Memory)
	assert.Equal(t, "2h", cfg.Defaults.Timeout)
	assert.Contains(t, cfg.Networks, "npm")
	assert.Contains(t, cfg.Networks, "pypi")
	assert.Contains(t, cfg.Networks, "github")
	assert.Contains(t, cfg.Networks, "anthropic")

	// Check blocked paths (SECURITY CRITICAL)
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.ssh"))
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.aws"))
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.config/gcloud"))
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.gnupg"))
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.password-store"))
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.mozilla"))
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.config/google-chrome"))
	assert.Contains(t, cfg.BlockedPaths, expandPath("~/.docker/config.json"))

	// Platform-specific blocked paths
	switch runtime.GOOS {
	case "darwin":
		assert.Contains(t, cfg.BlockedPaths, expandPath("~/Library/Keychains"))
	case "linux":
		assert.Contains(t, cfg.BlockedPaths, expandPath("~/.local/share/keyrings"))
	}

	// Credential persistence defaults to false (opt-in)
	assert.False(t, cfg.Claude.ShouldPersistCredentials())
}

func TestExpandPaths(t *testing.T) {
	home, err := homedir.Dir()
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "simple path",
			input:    []string{"~/.ssh"},
			expected: []string{filepath.Join(home, ".ssh")},
		},
		{
			name:     "path with read-only mount",
			input:    []string{"~/.gitconfig:ro"},
			expected: []string{filepath.Join(home, ".gitconfig:ro")},
		},
		{
			name:     "path with read-write mount",
			input:    []string{"~/project:rw"},
			expected: []string{filepath.Join(home, "project:rw")},
		},
		{
			name:     "multiple paths with mixed mount options",
			input:    []string{"~/.ssh", "~/.gitconfig:ro", "~/code:rw"},
			expected: []string{
				filepath.Join(home, ".ssh"),
				filepath.Join(home, ".gitconfig:ro"),
				filepath.Join(home, "code:rw"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPaths(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigDir(t *testing.T) {
	home, err := homedir.Dir()
	require.NoError(t, err)

	configDir, err := ConfigDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".faize"), configDir)
}

func TestEnsureConfigDir(t *testing.T) {
	// Just verify that EnsureConfigDir doesn't error
	// We can't easily test the actual directory creation without mocking
	// homedir, which uses a cached home directory value
	err := EnsureConfigDir()
	require.NoError(t, err)

	// Verify the config directory now exists in the real home
	configDir, err := ConfigDir()
	require.NoError(t, err)

	stat, err := os.Stat(configDir)
	require.NoError(t, err)
	assert.True(t, stat.IsDir())
}

func TestShouldMountGitContext(t *testing.T) {
	// Default (nil) should return true
	c := &Claude{}
	assert.True(t, c.ShouldMountGitContext())

	// Explicitly true
	trueVal := true
	c = &Claude{GitContext: &trueVal}
	assert.True(t, c.ShouldMountGitContext())

	// Explicitly false
	falseVal := false
	c = &Claude{GitContext: &falseVal}
	assert.False(t, c.ShouldMountGitContext())
}

// Helper function to expand a single path for test assertions
func expandPath(path string) string {
	expanded, err := homedir.Expand(path)
	if err != nil {
		return path
	}

	// Handle mount syntax
	if len(path) > 3 {
		if path[len(path)-3:] == ":ro" || path[len(path)-3:] == ":rw" {
			mountOpts := path[len(path)-3:]
			basePath, _ := homedir.Expand(path[:len(path)-3])
			return basePath + mountOpts
		}
	}

	return expanded
}

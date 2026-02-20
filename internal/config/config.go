package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mitchellh/go-homedir"
	"gopkg.in/yaml.v3"
)

// HardcodedBlockedPaths are security-critical paths that CANNOT be overridden by user config.
// These paths contain credentials and secrets that should never be mounted into containers.
var HardcodedBlockedPaths = []string{
	"~/.ssh",
	"~/.aws",
	"~/.config/gcloud",
	"~/.gnupg",
	"~/.password-store",
	"~/.docker/config.json",
}

// Config represents the Faize CLI configuration
type Config struct {
	Defaults     Defaults `yaml:"defaults"`
	Networks     []string `yaml:"networks"`
	BlockedPaths []string `yaml:"blocked_paths"`
	Claude       Claude   `yaml:"claude"`
}

// Defaults contains default values for sandbox execution
type Defaults struct {
	CPUs    int    `yaml:"cpus"`
	Memory  string `yaml:"memory"`
	Timeout string `yaml:"timeout"`
}

// Claude contains Claude-specific configuration
type Claude struct {
	AutoMounts         []string `yaml:"auto_mounts"`
	PersistCredentials *bool    `yaml:"persist_credentials"`
	ExtraDeps          []string `yaml:"extra_deps"`
	GitContext         *bool    `yaml:"git_context"`
}

// ShouldPersistCredentials returns whether credential persistence is enabled.
// Defaults to false when not explicitly set.
func (c *Claude) ShouldPersistCredentials() bool {
	if c.PersistCredentials == nil {
		return false
	}
	return *c.PersistCredentials
}

// ShouldMountGitContext returns whether automatic .git directory mounting is enabled.
// Defaults to true when not explicitly set.
func (c *Claude) ShouldMountGitContext() bool {
	if c.GitContext == nil {
		return true
	}
	return *c.GitContext
}

// Load loads the configuration from ~/.faize/config.yaml or returns defaults
func Load() (*Config, error) {
	home, err := homedir.Dir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, ".faize", "config.yaml")

	var cfg Config
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// Config file not found — use defaults
	} else {
		if err := yaml.Unmarshal(bytes.TrimSpace(data), &cfg); err != nil {
			return nil, err
		}
	}

	applyDefaults(&cfg)
	cfg.BlockedPaths = expandPaths(cfg.BlockedPaths)
	cfg.Claude.AutoMounts = expandPaths(cfg.Claude.AutoMounts)
	cfg.BlockedPaths = mergeBlockedPaths(cfg.BlockedPaths, expandPaths(HardcodedBlockedPaths))

	return &cfg, nil
}

// defaultBlockedPaths returns the default list of security-critical blocked paths.
func defaultBlockedPaths() []string {
	paths := []string{
		"~/.ssh",
		"~/.aws",
		"~/.config/gcloud",
		"~/.gnupg",
		"~/.password-store",
		"~/.mozilla",
		"~/.config/google-chrome",
		"~/.docker",
		"~/.netrc",
		"~/.npmrc",
		"~/.pypirc",
		"~/.m2/settings.xml",
		"~/.gradle/gradle.properties",
		"~/.kube",
		"~/.config/gh",
		"~/.config/hub",
		"~/.azure",
	}

	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, "~/Library/Keychains")
	case "linux":
		paths = append(paths, "~/.local/share/keyrings")
	}

	return paths
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.Defaults.CPUs == 0 {
		cfg.Defaults.CPUs = 2
	}
	if cfg.Defaults.Memory == "" {
		cfg.Defaults.Memory = "4GB"
	}
	if cfg.Defaults.Timeout == "" {
		cfg.Defaults.Timeout = "2h"
	}
	if len(cfg.Networks) == 0 {
		cfg.Networks = []string{"npm", "pypi", "github", "anthropic"}
	}
	if len(cfg.BlockedPaths) == 0 {
		cfg.BlockedPaths = defaultBlockedPaths()
	}
}

// expandPaths expands ~ in paths to home directory
func expandPaths(paths []string) []string {
	expanded := make([]string, len(paths))
	for i, path := range paths {
		// Handle mount syntax (path:ro or path:rw)
		mountOpts := ""
		if colonIdx := len(path) - 3; colonIdx > 0 &&
			(path[colonIdx:] == ":ro" || path[colonIdx:] == ":rw") {
			mountOpts = path[colonIdx:]
			path = path[:colonIdx]
		}

		expandedPath, err := homedir.Expand(path)
		if err != nil {
			// If expansion fails, use original path
			expanded[i] = paths[i]
			continue
		}

		// Re-attach mount options if present
		if mountOpts != "" {
			expandedPath += mountOpts
		}

		expanded[i] = expandedPath
	}
	return expanded
}

// ConfigDir returns the Faize configuration directory path
func ConfigDir() (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".faize"), nil
}

// EnsureConfigDir creates the config directory if it doesn't exist
func EnsureConfigDir() error {
	configDir, err := ConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(configDir, 0755)
}

// mergeBlockedPaths merges two lists of blocked paths, removing duplicates.
// The hardcoded paths are always included regardless of user config.
func mergeBlockedPaths(userPaths, hardcodedPaths []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(userPaths)+len(hardcodedPaths))

	// Add hardcoded paths first (they take priority)
	for _, path := range hardcodedPaths {
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}

	// Add user paths that aren't duplicates
	for _, path := range userPaths {
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}

	return result
}

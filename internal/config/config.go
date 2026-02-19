package config

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
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
	Defaults     Defaults `mapstructure:"defaults"`
	Networks     []string `mapstructure:"networks"`
	BlockedPaths []string `mapstructure:"blocked_paths"`
	Claude       Claude   `mapstructure:"claude"`
}

// Defaults contains default values for sandbox execution
type Defaults struct {
	CPUs    int    `mapstructure:"cpus"`
	Memory  string `mapstructure:"memory"`
	Timeout string `mapstructure:"timeout"`
}

// Claude contains Claude-specific configuration
type Claude struct {
	AutoMounts         []string `mapstructure:"auto_mounts"`
	PersistCredentials *bool    `mapstructure:"persist_credentials"`
	ExtraDeps          []string `mapstructure:"extra_deps"`
	GitContext         *bool    `mapstructure:"git_context"`
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
	// Expand home directory
	home, err := homedir.Dir()
	if err != nil {
		return nil, err
	}

	// Set up viper
	configDir := filepath.Join(home, ".faize")
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configDir)

	// Set defaults
	setDefaults()

	// Try to read config file, but don't fail if it doesn't exist
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file was found but another error occurred
			return nil, err
		}
		// Config file not found, use defaults
	}

	// Unmarshal into config struct
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Expand ~ in paths
	cfg.BlockedPaths = expandPaths(cfg.BlockedPaths)
	cfg.Claude.AutoMounts = expandPaths(cfg.Claude.AutoMounts)

	// Merge hardcoded blocked paths (security-critical, cannot be overridden)
	cfg.BlockedPaths = mergeBlockedPaths(cfg.BlockedPaths, expandPaths(HardcodedBlockedPaths))

	return &cfg, nil
}

// setDefaults sets default configuration values
func setDefaults() {
	// Defaults
	viper.SetDefault("defaults.cpus", 2)
	viper.SetDefault("defaults.memory", "4GB")
	viper.SetDefault("defaults.timeout", "2h")
	viper.SetDefault("networks", []string{"npm", "pypi", "github", "anthropic"})

	// Blocked paths (SECURITY CRITICAL)
	blockedPaths := []string{
		"~/.ssh",
		"~/.aws",
		"~/.config/gcloud",
		"~/.gnupg",
		"~/.password-store",
		"~/.mozilla",
		"~/.config/google-chrome",
		"~/.docker",
		// Additional credential stores
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

	// Add platform-specific blocked paths
	switch runtime.GOOS {
	case "darwin":
		blockedPaths = append(blockedPaths, "~/Library/Keychains")
	case "linux":
		blockedPaths = append(blockedPaths, "~/.local/share/keyrings")
	}

	viper.SetDefault("blocked_paths", blockedPaths)

	// Claude-specific defaults
	viper.SetDefault("claude.auto_mounts", []string{})
	viper.SetDefault("claude.persist_credentials", false)
	viper.SetDefault("claude.extra_deps", []string{})
	viper.SetDefault("claude.git_context", true)
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

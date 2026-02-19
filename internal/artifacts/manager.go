package artifacts

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
)

const (
	// BaseURL is the GitHub releases URL for artifacts
	BaseURL = "https://github.com/faize-ai/faize/releases/download"
	// Version is the artifact version to download
	Version = "v0.1.0"

	// Fallback kernel: build from source using scripts/build-kernel.sh when primary download fails
	// The custom kernel has virtio support required for Apple Virtualization.framework
)

// Manager handles artifact download and storage at ~/.faize/artifacts/
type Manager struct {
	dir string
}

// NewManager creates a new artifact manager
func NewManager() (*Manager, error) {
	home, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	dir := filepath.Join(home, ".faize", "artifacts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create artifacts directory: %w", err)
	}

	return &Manager{dir: dir}, nil
}

// EnsureArtifacts downloads kernel and rootfs if missing
func (m *Manager) EnsureArtifacts() error {
	if err := m.ensureKernel(); err != nil {
		return fmt.Errorf("failed to ensure kernel: %w", err)
	}

	if err := m.ensureRootfs(); err != nil {
		return fmt.Errorf("failed to ensure rootfs: %w", err)
	}

	return nil
}

// KernelPath returns the path to the vmlinux kernel
func (m *Manager) KernelPath() string {
	return filepath.Join(m.dir, "vmlinux")
}

// RootfsPath returns the path to the rootfs.img
func (m *Manager) RootfsPath() string {
	return filepath.Join(m.dir, "rootfs.img")
}

// Dir returns the artifacts directory
func (m *Manager) Dir() string {
	return m.dir
}

// FaizeDir returns the base ~/.faize directory (parent of artifacts dir)
func (m *Manager) FaizeDir() string {
	return filepath.Dir(m.dir)
}

// SessionDir returns the path to ~/.faize/sessions/{id}/
func (m *Manager) SessionDir(id string) string {
	return filepath.Join(m.FaizeDir(), "sessions", id)
}

func (m *Manager) ensureKernel() error {
	path := m.KernelPath()
	if _, err := os.Stat(path); err == nil {
		return nil // Already exists
	}

	// Try our own release first
	url := fmt.Sprintf("%s/%s/vmlinux", BaseURL, Version)
	err := m.download(url, path, "vmlinux kernel")
	if err == nil {
		return nil
	}

	// Fallback: build kernel from source with virtio support
	fmt.Printf("Primary kernel unavailable, building from source...\n")
	if err := m.buildKernel(path); err != nil {
		return fmt.Errorf("failed to get kernel from any source: %w", err)
	}

	return nil
}

func (m *Manager) ensureRootfs() error {
	path := m.RootfsPath()
	if _, err := os.Stat(path); err == nil {
		return nil // Already exists
	}

	// Try downloading from GitHub releases first
	url := fmt.Sprintf("%s/%s/rootfs.img", BaseURL, Version)
	fmt.Printf("Attempting to download rootfs from GitHub releases...\n")
	err := m.download(url, path, "rootfs image")

	// If download fails with 404, try building locally
	if err != nil && strings.Contains(err.Error(), "HTTP 404") {
		fmt.Printf("Rootfs not found in releases, attempting to build locally...\n")
		return m.BuildRootfs()
	}

	return err
}

func (m *Manager) download(url, destPath, name string) error {
	fmt.Printf("Downloading %s...\n", name)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: HTTP %d", name, resp.StatusCode)
	}

	// Create temp file for atomic write
	tmpPath := destPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	// Copy with progress
	written, err := io.Copy(file, resp.Body)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close %s: %w", name, err)
	}

	// Rename to final path (atomic)
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize %s: %w", name, err)
	}

	fmt.Printf("Downloaded %s (%d bytes)\n", name, written)
	return nil
}

// Clean removes all artifacts
func (m *Manager) Clean() error {
	if err := os.RemoveAll(m.dir); err != nil {
		return fmt.Errorf("failed to clean artifacts: %w", err)
	}
	return os.MkdirAll(m.dir, 0755)
}

// buildKernel builds the kernel using scripts/build-kernel.sh
// This produces an uncompressed ARM64 Image that Apple Virtualization.framework requires
func (m *Manager) buildKernel(destPath string) error {
	scriptPath, err := m.findKernelBuildScript()
	if err != nil {
		return fmt.Errorf("failed to find build-kernel.sh: %w", err)
	}

	fmt.Printf("Building kernel with virtio support (this may take 5-10 minutes on first run)...\n")
	fmt.Printf("Using build script: %s\n", scriptPath)

	// build-kernel.sh <version> <workdir> <output>
	// Use empty string for workdir to let the script use a temp directory
	cmd := exec.Command("bash", scriptPath, "6.6.10", "", destPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build kernel: %w", err)
	}

	// Verify the kernel was built
	if info, err := os.Stat(destPath); err == nil {
		fmt.Printf("Kernel built successfully (%d bytes)\n", info.Size())
	}

	return nil
}

// findKernelBuildScript locates the build-kernel.sh script
// Looks in ../scripts/ relative to the binary, or scripts/ relative to repo root
func (m *Manager) findKernelBuildScript() (string, error) {
	// Try relative to binary (for installed/distributed binaries)
	execPath, err := os.Executable()
	if err == nil {
		scriptPath := filepath.Join(filepath.Dir(execPath), "..", "scripts", "build-kernel.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}
	}

	// Try relative to repo root (for development)
	wd, err := os.Getwd()
	if err == nil {
		// Try from current working directory
		scriptPath := filepath.Join(wd, "cli", "scripts", "build-kernel.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}

		// Try going up from working directory to find scripts
		scriptPath = filepath.Join(wd, "scripts", "build-kernel.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}
	}

	return "", fmt.Errorf("build-kernel.sh not found in expected locations")
}

// BuildRootfs builds the rootfs locally using build-rootfs.sh script
func (m *Manager) BuildRootfs() error {
	// Find the build-rootfs.sh script
	scriptPath, err := m.findBuildScript()
	if err != nil {
		return fmt.Errorf("failed to find build-rootfs.sh script: %w", err)
	}

	fmt.Printf("Building rootfs using: %s\n", scriptPath)

	// Execute the script with rootfs path as argument
	cmd := exec.Command("bash", scriptPath, m.RootfsPath())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build rootfs: %w", err)
	}

	fmt.Printf("Rootfs built successfully at: %s\n", m.RootfsPath())
	return nil
}

// ClaudeRootfsPath returns the path to the claude-rootfs.img
func (m *Manager) ClaudeRootfsPath() string {
	return filepath.Join(m.dir, "claude-rootfs.img")
}

// ToolchainDir returns the path to ~/.faize/toolchain/
func (m *Manager) ToolchainDir() string {
	return filepath.Join(m.FaizeDir(), "toolchain")
}

// EnsureClaudeRootfs ensures kernel and claude-rootfs.img exist
func (m *Manager) EnsureClaudeRootfs() error {
	// Ensure kernel exists (shared with regular rootfs)
	if err := m.ensureKernel(); err != nil {
		return fmt.Errorf("failed to ensure kernel: %w", err)
	}

	path := m.ClaudeRootfsPath()
	if _, err := os.Stat(path); err == nil {
		return nil // Already exists
	}

	// Try to build locally
	return m.BuildClaudeRootfs()
}

// BuildClaudeRootfs builds claude rootfs using build-claude-rootfs.sh
func (m *Manager) BuildClaudeRootfs() error {
	return m.BuildClaudeRootfsWithDeps(nil)
}

// BuildClaudeRootfsWithDeps builds claude rootfs with extra dependencies baked in
func (m *Manager) BuildClaudeRootfsWithDeps(extraDeps []string) error {
	scriptPath, err := m.findClaudeBuildScript()
	if err != nil {
		return fmt.Errorf("failed to find build-claude-rootfs.sh script: %w", err)
	}

	fmt.Printf("Building Claude rootfs using: %s\n", scriptPath)

	cmd := exec.Command("bash", scriptPath, m.ClaudeRootfsPath())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Pass extra dependencies via environment variable
	if len(extraDeps) > 0 {
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("EXTRA_DEPS=%s", strings.Join(extraDeps, " ")))
		fmt.Printf("Extra dependencies: %v\n", extraDeps)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build claude rootfs: %w", err)
	}

	fmt.Printf("Claude rootfs built successfully at: %s\n", m.ClaudeRootfsPath())
	return nil
}

// findClaudeBuildScript locates the build-claude-rootfs.sh script
// Looks in ../scripts/ relative to the binary, or cli/scripts/ relative to repo root
func (m *Manager) findClaudeBuildScript() (string, error) {
	// Try relative to binary (for installed/distributed binaries)
	execPath, err := os.Executable()
	if err == nil {
		scriptPath := filepath.Join(filepath.Dir(execPath), "..", "scripts", "build-claude-rootfs.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}
	}

	// Try relative to repo root (for development)
	// Assume we're in cli/internal/artifacts, so go up to repo root
	wd, err := os.Getwd()
	if err == nil {
		// Try from current working directory
		scriptPath := filepath.Join(wd, "cli", "scripts", "build-claude-rootfs.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}

		// Try going up from working directory to find cli/scripts
		scriptPath = filepath.Join(wd, "scripts", "build-claude-rootfs.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}
	}

	return "", fmt.Errorf("build-claude-rootfs.sh not found in expected locations")
}

// EnsureToolchainDir ensures toolchain directory exists
func (m *Manager) EnsureToolchainDir() error {
	dir := m.ToolchainDir()
	return os.MkdirAll(dir, 0755)
}

// CredentialsDir returns the path to ~/.faize/credentials/
func (m *Manager) CredentialsDir() string {
	return filepath.Join(m.FaizeDir(), "credentials")
}

// EnsureCredentialsDir ensures credentials directory exists with restrictive permissions
func (m *Manager) EnsureCredentialsDir() error {
	dir := m.CredentialsDir()
	return os.MkdirAll(dir, 0700)
}

// findBuildScript locates the build-rootfs.sh script
// Looks in ../scripts/ relative to the binary, or cli/scripts/ relative to repo root
func (m *Manager) findBuildScript() (string, error) {
	// Try relative to binary (for installed/distributed binaries)
	execPath, err := os.Executable()
	if err == nil {
		scriptPath := filepath.Join(filepath.Dir(execPath), "..", "scripts", "build-rootfs.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}
	}

	// Try relative to repo root (for development)
	// Assume we're in cli/internal/artifacts, so go up to repo root
	wd, err := os.Getwd()
	if err == nil {
		// Try from current working directory
		scriptPath := filepath.Join(wd, "cli", "scripts", "build-rootfs.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}

		// Try going up from working directory to find cli/scripts
		scriptPath = filepath.Join(wd, "scripts", "build-rootfs.sh")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, nil
		}
	}

	return "", fmt.Errorf("build-rootfs.sh not found in expected locations")
}

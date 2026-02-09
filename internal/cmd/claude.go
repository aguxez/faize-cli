package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/faize-ai/faize/internal/config"
	"github.com/faize-ai/faize/internal/mount"
	"github.com/faize-ai/faize/internal/network"
	"github.com/faize-ai/faize/internal/session"
	"github.com/faize-ai/faize/internal/vm"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
)

var (
	claudeProjectDir string
	claudeMounts     []string
	claudeCPUs       int
	claudeMemory     string
	claudeTimeout    string
	claudeDebug      bool
)

var claudeCmd = &cobra.Command{
	Use:   "claude [flags]",
	Short: "Run a Claude-optimized Faize environment",
	Long: `Run a Claude-optimized Faize environment with preset configurations.

This command automatically:
  - Uses claude-rootfs.img instead of regular rootfs.img
  - Mounts ~/.claude as read-only to /mnt/host-claude
  - Mounts ~/.faize/toolchain as read-write to /opt/toolchain
  - Sets up your project directory at /workspace
  - Configures network access for Claude-specific domains
  - Enables ClaudeMode for optimized VM configuration

Example:
  faize claude --project ~/code/myapp
  faize claude -p ~/code/myapp --cpus 4 --memory 8GB`,
	RunE: runClaude,
}

func init() {
	// Required flags
	claudeCmd.Flags().StringVarP(&claudeProjectDir, "project", "p", "", "project directory to mount (required)")
	claudeCmd.MarkFlagRequired("project")

	// Optional flags
	claudeCmd.Flags().StringArrayVarP(&claudeMounts, "mount", "m", []string{}, "additional mount paths (repeatable)")
	claudeCmd.Flags().IntVar(&claudeCPUs, "cpus", 0, "number of CPUs (default from config)")
	claudeCmd.Flags().StringVar(&claudeMemory, "memory", "", "memory limit (e.g., 4GB)")
	claudeCmd.Flags().StringVarP(&claudeTimeout, "timeout", "t", "", "session timeout (e.g., 2h)")
	claudeCmd.Flags().BoolVar(&claudeDebug, "debug", false, "enable debug logging")
}

func runClaude(cmd *cobra.Command, args []string) error {
	// Set debug env var for subpackages
	if claudeDebug {
		os.Setenv("FAIZE_DEBUG", "1")
		debug = true // Set global debug flag
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	Debug("Config loaded successfully")

	// Get home directory for Claude paths
	home, err := homedir.Dir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	claudeDir := filepath.Join(home, ".claude")
	toolchainDir := filepath.Join(home, ".faize", "toolchain")

	// Verify ~/.claude exists
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return fmt.Errorf("~/.claude directory not found - please ensure Claude Code is installed")
	}

	// Ensure ~/.faize/toolchain exists
	if err := os.MkdirAll(toolchainDir, 0755); err != nil {
		return fmt.Errorf("failed to create toolchain directory: %w", err)
	}

	// Apply defaults from config if not specified
	if claudeCPUs == 0 {
		claudeCPUs = cfg.Defaults.CPUs
	}
	if claudeMemory == "" {
		claudeMemory = cfg.Defaults.Memory
	}
	if claudeTimeout == "" {
		claudeTimeout = cfg.Defaults.Timeout
	}

	// Use Claude-specific network defaults
	claudeNetworks := cfg.Claude.Network
	if len(claudeNetworks) == 0 {
		// Fallback to hardcoded Claude defaults if not in config
		claudeNetworks = []string{"anthropic", "npm", "github", "bun"}
	}

	// Parse timeout duration
	timeoutDuration, err := time.ParseDuration(claudeTimeout)
	if err != nil {
		return fmt.Errorf("invalid timeout format '%s': %w", claudeTimeout, err)
	}

	// Parse project directory
	projectMount, err := mount.Parse(claudeProjectDir)
	if err != nil {
		return fmt.Errorf("invalid project path: %w", err)
	}

	// Create mount validator with blocked paths
	validator, err := mount.NewValidator(cfg.BlockedPaths)
	if err != nil {
		return fmt.Errorf("failed to create mount validator: %w", err)
	}

	// Build mount list:
	// 1. Project mount (rw)
	// 2. ~/.claude mount (ro) -> /mnt/host-claude
	// 3. ~/.faize/toolchain mount (rw) -> /opt/toolchain
	// 4. Auto mounts from config (Claude-specific)
	// 5. User-specified mounts
	allMountSpecs := []string{
		claudeProjectDir + ":rw",
		claudeDir + ":/mnt/host-claude:ro",
		toolchainDir + ":/opt/toolchain:rw",
	}
	allMountSpecs = append(allMountSpecs, cfg.Claude.AutoMounts...)
	allMountSpecs = append(allMountSpecs, claudeMounts...)

	// Parse and validate all mounts
	var parsedMounts []session.VMMount
	for i, spec := range allMountSpecs {
		m, err := mount.Parse(spec)
		if err != nil {
			return fmt.Errorf("invalid mount '%s': %w", spec, err)
		}

		// Validate against blocked paths (except for ~/.claude which is explicitly allowed)
		if m.Source != claudeDir {
			if err := validator.Validate(m); err != nil {
				return fmt.Errorf("mount validation failed: %w", err)
			}
		}

		parsedMounts = append(parsedMounts, session.VMMount{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
			Tag:      fmt.Sprintf("mount%d", i),
		})
	}

	// Parse network policy
	policy := network.Parse(claudeNetworks)
	fmt.Printf("Network policy: ")
	if policy.AllowAll {
		fmt.Println("allow all traffic")
	} else if policy.Blocked {
		fmt.Println("no network access")
	} else {
		fmt.Printf("allowed domains: %v\n", policy.Domains)
	}

	// Create VM configuration with Claude mode enabled
	vmConfig := &vm.Config{
		ProjectDir:    projectMount.Source,
		Mounts:        parsedMounts,
		Network:       claudeNetworks,
		CPUs:          claudeCPUs,
		Memory:        claudeMemory,
		Timeout:       timeoutDuration,
		ClaudeMode:    true,
		HostClaudeDir: claudeDir,
		ToolchainDir:  toolchainDir,
	}

	// Print configuration
	fmt.Println("\nClaude session configuration:")
	fmt.Printf("  Mode: Claude-optimized\n")
	fmt.Printf("  Project: %s\n", vmConfig.ProjectDir)
	fmt.Printf("  Claude dir: %s (ro)\n", claudeDir)
	fmt.Printf("  Toolchain: %s (rw)\n", toolchainDir)
	fmt.Printf("  CPUs: %d\n", vmConfig.CPUs)
	fmt.Printf("  Memory: %s\n", vmConfig.Memory)
	fmt.Printf("  Timeout: %s\n", vmConfig.Timeout)
	fmt.Printf("  Mounts: %d configured\n", len(vmConfig.Mounts))
	for _, m := range vmConfig.Mounts {
		mode := "rw"
		if m.ReadOnly {
			mode = "ro"
		}
		fmt.Printf("    %s -> %s (%s)\n", m.Source, m.Target, mode)
	}

	// Try to create VZManager (macOS only), fall back to stub
	Debug("Creating VM manager...")
	var manager vm.Manager
	vzManager, err := vm.NewVZManager()
	if err != nil {
		// Fall back to stub manager for non-macOS or missing entitlements
		fmt.Printf("\nNote: %v\n", err)
		fmt.Println("Using stub manager for validation only.")
		manager = vm.NewStubManager()
	} else {
		manager = vzManager
		Debug("VZManager created successfully")
	}

	Debug("Creating VM session...")
	sess, err := manager.Create(vmConfig)
	if err != nil {
		if err == vm.ErrVMNotImplemented {
			fmt.Println("\n[Phase 1] VM support not yet implemented.")
			fmt.Println("Configuration validated successfully. VM creation will be available in Phase 2.")
			return nil
		}
		return fmt.Errorf("failed to create VM session: %w", err)
	}

	// Start the session
	Debug("Starting VM session %s...", sess.ID)
	if err := manager.Start(sess); err != nil {
		return fmt.Errorf("failed to start VM session: %w", err)
	}
	Debug("VM started successfully")

	fmt.Printf("\nSession started: %s\n", sess.ID)

	// Attach to console
	fmt.Println("Attaching to console... (Ctrl+C to detach)")
	if err := manager.Attach(sess.ID); err != nil {
		return fmt.Errorf("failed to attach to console: %w", err)
	}

	return nil
}

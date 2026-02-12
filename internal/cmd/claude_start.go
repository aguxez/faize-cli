package cmd

import (
	"errors"
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
	startProjectDir string
	startMounts     []string
	startCPUs       int
	startMemory     string
	startTimeout    string
	startDebug      bool
)

var claudeStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a new Claude Code session",
	Long: `Start a new Claude Code VM session.

This command automatically:
  - Uses claude-rootfs.img for the VM
  - Mounts ~/.claude as read-only to /mnt/host-claude
  - Mounts ~/.faize/toolchain as read-write to /opt/toolchain
  - Sets up your project directory at /workspace
  - Configures network access for Claude-specific domains

Examples:
  faize claude start --project ~/code/myapp
  faize claude start -p ~/code/myapp --cpus 4 --memory 8GB`,
	RunE: runClaudeStart,
}

func init() {
	claudeStartCmd.Flags().StringVarP(&startProjectDir, "project", "p", "", "project directory to mount (required)")
	claudeStartCmd.MarkFlagRequired("project")

	claudeStartCmd.Flags().StringArrayVarP(&startMounts, "mount", "m", []string{}, "additional mount paths (repeatable)")
	claudeStartCmd.Flags().IntVar(&startCPUs, "cpus", 0, "number of CPUs (default from config)")
	claudeStartCmd.Flags().StringVar(&startMemory, "memory", "", "memory limit (e.g., 4GB)")
	claudeStartCmd.Flags().StringVarP(&startTimeout, "timeout", "t", "", "session timeout (e.g., 2h)")
	claudeStartCmd.Flags().BoolVar(&startDebug, "debug", false, "enable debug logging")

	claudeCmd.AddCommand(claudeStartCmd)
}

func runClaudeStart(cmd *cobra.Command, args []string) error {
	// Set debug env var for subpackages
	if startDebug {
		os.Setenv("FAIZE_DEBUG", "1")
		debug = true
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
	if startCPUs == 0 {
		startCPUs = cfg.Defaults.CPUs
	}
	if startMemory == "" {
		startMemory = cfg.Defaults.Memory
	}
	if startTimeout == "" {
		startTimeout = cfg.Defaults.Timeout
	}

	// Use Claude-specific network defaults
	claudeNetworks := cfg.Claude.Network
	if len(claudeNetworks) == 0 {
		claudeNetworks = []string{"anthropic", "npm", "github", "bun"}
	}

	// Parse timeout duration
	timeoutDuration, err := time.ParseDuration(startTimeout)
	if err != nil {
		return fmt.Errorf("invalid timeout format '%s': %w", startTimeout, err)
	}

	// Parse project directory
	projectMount, err := mount.Parse(startProjectDir)
	if err != nil {
		return fmt.Errorf("invalid project path: %w", err)
	}

	// Create mount validator with blocked paths
	validator, err := mount.NewValidator(cfg.BlockedPaths)
	if err != nil {
		return fmt.Errorf("failed to create mount validator: %w", err)
	}

	// Build mount list
	allMountSpecs := []string{
		startProjectDir + ":rw",
		claudeDir + ":/mnt/host-claude:ro",
		toolchainDir + ":/opt/toolchain:rw",
	}
	allMountSpecs = append(allMountSpecs, cfg.Claude.AutoMounts...)
	allMountSpecs = append(allMountSpecs, startMounts...)

	// Parse and validate all mounts
	var parsedMounts []session.VMMount
	for i, spec := range allMountSpecs {
		m, err := mount.Parse(spec)
		if err != nil {
			return fmt.Errorf("invalid mount '%s': %w", spec, err)
		}

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

	// Create VM configuration
	vmConfig := &vm.Config{
		ProjectDir:    projectMount.Source,
		Mounts:        parsedMounts,
		Network:       claudeNetworks,
		CPUs:          startCPUs,
		Memory:        startMemory,
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

	// Create VM manager
	Debug("Creating VM manager...")
	var manager vm.Manager
	vzManager, err := vm.NewVZManager()
	if err != nil {
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

	// Always attach to console after starting
	fmt.Println("Attaching to console... (Ctrl+C to detach)")
	err = manager.Attach(sess.ID)
	if errors.Is(err, vm.ErrUserDetach) {
		fmt.Println("\nDetached from session")
		fmt.Printf("Session %s still running. Reattach with: faize claude attach %s\n", sess.ID, sess.ID)
		return nil
	}
	return err
}

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/faize-ai/faize/internal/config"
	"github.com/faize-ai/faize/internal/git"
	"github.com/faize-ai/faize/internal/mount"
	"github.com/faize-ai/faize/internal/network"
	"github.com/faize-ai/faize/internal/session"
	"github.com/faize-ai/faize/internal/vm"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
)

var (
	startProjectDir        string
	startMounts            []string
	startCPUs              int
	startMemory            string
	startTimeout           string
	startDebug             bool
	startPersistCreds      bool
	startNoGitContext      bool
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
  faize claude start                              # uses current directory
  faize claude start --project ~/code/myapp
  faize claude start -p ~/code/myapp --cpus 4 --memory 8GB`,
	RunE: runClaudeStart,
}

func init() {
	claudeStartCmd.Flags().StringVarP(&startProjectDir, "project", "p", "", "project directory to mount (default: current directory)")

	claudeStartCmd.Flags().StringArrayVarP(&startMounts, "mount", "m", []string{}, "additional mount paths (repeatable)")
	claudeStartCmd.Flags().IntVar(&startCPUs, "cpus", 0, "number of CPUs (default from config)")
	claudeStartCmd.Flags().StringVar(&startMemory, "memory", "", "memory limit (e.g., 4GB)")
	claudeStartCmd.Flags().StringVarP(&startTimeout, "timeout", "t", "", "session timeout (e.g., 2h)")
	claudeStartCmd.Flags().BoolVar(&startDebug, "debug", false, "enable debug logging")
	claudeStartCmd.Flags().BoolVar(&startPersistCreds, "persist-credentials", false, "persist Claude credentials across sessions")
	claudeStartCmd.Flags().BoolVar(&startNoGitContext, "no-git-context", false, "disable automatic .git directory mounting from git root")

	claudeCmd.AddCommand(claudeStartCmd)
}

func runClaudeStart(cmd *cobra.Command, args []string) error {
	// Set debug env var for subpackages
	if startDebug {
		os.Setenv("FAIZE_DEBUG", "1")
		debug = true
	}

	// Default project directory to current working directory
	if startProjectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		startProjectDir = cwd
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

	// Determine credential persistence
	persistCreds := cfg.Claude.ShouldPersistCredentials() || startPersistCreds
	var credentialsDir string
	if persistCreds {
		credentialsDir = filepath.Join(home, ".faize", "credentials")
		if err := os.MkdirAll(credentialsDir, 0700); err != nil {
			return fmt.Errorf("failed to create credentials directory: %w", err)
		}
		// No need to pre-create empty files - copy logic handles missing files gracefully
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

	// Use network config
	claudeNetworks := cfg.Networks
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

	// Auto-detect git root for monorepo support
	if !startNoGitContext && cfg.Claude.ShouldMountGitContext() {
		gitRoot := git.FindRoot(startProjectDir)
		if gitRoot != "" && gitRoot != startProjectDir {
			gitDirPath := filepath.Join(gitRoot, ".git")
			if info, err := os.Stat(gitDirPath); err == nil && info.IsDir() {
				allMountSpecs = append(allMountSpecs, gitDirPath+":"+gitDirPath+":ro")
				Debug("Git root detected: %s (mounting .git read-only)", gitRoot)
			}
		}
	}

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
	if policy.AllowAll {
		Debug("Network policy: allow all traffic")
	} else if policy.Blocked {
		Debug("Network policy: no network access")
	} else {
		Debug("Network policy: allowed domains: %v", policy.Domains)
	}

	// Create VM configuration
	vmConfig := &vm.Config{
		ProjectDir:    projectMount.Source,
		Mounts:        parsedMounts,
		Network:       claudeNetworks,
		NetworkPolicy: policy,
		CPUs:          startCPUs,
		Memory:        startMemory,
		Timeout:       timeoutDuration,
		ClaudeMode:     true,
		HostClaudeDir:  claudeDir,
		ToolchainDir:   toolchainDir,
		CredentialsDir: credentialsDir,
		ExtraDeps:      cfg.Claude.ExtraDeps,
	}

	// Print configuration (debug only)
	Debug("Claude session configuration:")
	Debug("  Mode: Claude-optimized")
	Debug("  Project: %s", vmConfig.ProjectDir)
	Debug("  Claude dir: %s (ro)", claudeDir)
	Debug("  Toolchain: %s (rw)", toolchainDir)
	if credentialsDir != "" {
		Debug("  Credentials: %s (rw)", credentialsDir)
	}
	Debug("  CPUs: %d", vmConfig.CPUs)
	Debug("  Memory: %s", vmConfig.Memory)
	Debug("  Timeout: %s", vmConfig.Timeout)
	Debug("  Mounts: %d configured", len(vmConfig.Mounts))
	for _, m := range vmConfig.Mounts {
		mode := "rw"
		if m.ReadOnly {
			mode = "ro"
		}
		Debug("    %s -> %s (%s)", m.Source, m.Target, mode)
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

	// Ensure session is stopped when we exit (detach, VM stop, error, signal)
	defer func() {
		fmt.Printf("\nStopping session %s...\n", sess.ID)
		if stopErr := manager.Stop(sess.ID); stopErr != nil {
			Debug("Failed to stop session: %v", stopErr)
		}
	}()

	projectName := filepath.Base(vmConfig.ProjectDir)
	fmt.Printf("\nSession %s | %s | %d CPUs, %s | %s timeout\n",
		sess.ID, projectName, vmConfig.CPUs, vmConfig.Memory, vmConfig.Timeout)

	// Attach to console — session stops when we return
	fmt.Println("Attaching to console... (~. to detach)")
	if err := manager.Attach(sess.ID); err != nil && !errors.Is(err, vm.ErrUserDetach) {
		return fmt.Errorf("console error: %w", err)
	}
	return nil
}

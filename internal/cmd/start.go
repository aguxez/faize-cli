package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/faize-ai/faize/internal/changeset"
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
	startProjectDir   string
	startMounts       []string
	startTimeout      string
	startPersistCreds bool
	startNoGitContext bool
	startClaude       bool
	startNoDiff       bool
)

var startCmd = &cobra.Command{
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
  faize start                              # uses current directory
  faize start --project ~/code/myapp
  faize start -p ~/code/myapp`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().StringVarP(&startProjectDir, "project", "p", "", "project directory to mount (default: current directory)")
	startCmd.Flags().StringArrayVarP(&startMounts, "mount", "m", []string{}, "additional mount paths (repeatable)")
	startCmd.Flags().StringVarP(&startTimeout, "timeout", "t", "", "session timeout (e.g., 2h)")
	startCmd.Flags().BoolVar(&startPersistCreds, "persist-credentials", false, "persist Claude credentials across sessions")
	startCmd.Flags().BoolVar(&startNoGitContext, "no-git-context", false, "disable automatic .git directory mounting from git root")
	startCmd.Flags().BoolVar(&startClaude, "claude", true, "use Claude Code mode")
	startCmd.Flags().BoolVar(&startNoDiff, "no-diff", false, "disable change tracking and summary")

	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// Set debug env var for subpackages
	if debug {
		_ = os.Setenv("FAIZE_DEBUG", "1")
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

	// Read CPUs and memory directly from config
	cpus := cfg.Resources.CPUs
	memory := cfg.Resources.Memory

	if startTimeout == "" {
		startTimeout = cfg.Timeout
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
		if len(policy.Domains) > 0 {
			Debug("Network policy: allowed domains: %v", policy.Domains)
		}
		if len(policy.Wildcards) > 0 {
			Debug("Network policy: allowed wildcards: %v", policy.Wildcards)
		}
	}

	// Create VM configuration
	vmConfig := &vm.Config{
		ProjectDir:     projectMount.Source,
		Mounts:         parsedMounts,
		Network:        claudeNetworks,
		NetworkPolicy:  policy,
		CPUs:           cpus,
		Memory:         memory,
		Timeout:        timeoutDuration,
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

	// Timeout enforcement: stop the VM when the timeout expires
	var timedOut atomic.Bool
	if timeoutDuration > 0 {
		timer := time.AfterFunc(timeoutDuration, func() {
			timedOut.Store(true)
			fmt.Printf("\nSession timeout (%s) reached. Stopping...\n", timeoutDuration)
			_ = manager.Stop(sess.ID)
		})
		defer timer.Stop()
	}

	// Take pre-snapshots of rw mounts for change tracking
	type mountSnapshot struct {
		source string
		target string
		tag    string
		snap   changeset.Snapshot
	}
	var preSnapshots []mountSnapshot
	showDiff := cfg.Claude.ShouldShowDiff() && !startNoDiff
	if showDiff {
		for _, m := range parsedMounts {
			if m.ReadOnly {
				continue
			}
			Debug("Taking pre-snapshot of %s", m.Source)
			snap, err := changeset.Take(m.Source)
			if err != nil {
				Debug("Failed to snapshot %s: %v", m.Source, err)
				continue
			}
			preSnapshots = append(preSnapshots, mountSnapshot{
				source: m.Source,
				target: m.Target,
				tag:    m.Tag,
				snap:   snap,
			})
		}
	}

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
	attachErr := manager.Attach(sess.ID)
	if attachErr != nil && !errors.Is(attachErr, vm.ErrUserDetach) {
		return fmt.Errorf("console error: %w", attachErr)
	}

	// Determine exit reason and persist session metadata
	exitReason := "normal"
	if timedOut.Load() {
		exitReason = "timeout"
	} else if errors.Is(attachErr, vm.ErrUserDetach) {
		exitReason = "detach"
	}
	now := time.Now()
	sess.Timeout = startTimeout
	sess.StoppedAt = &now
	sess.ExitReason = exitReason
	sess.Status = "stopped"
	store, storeErr := session.NewStore()
	if storeErr == nil {
		if saveErr := store.Save(sess); saveErr != nil {
			Debug("Failed to save session: %v", saveErr)
		}
	}

	// Post-session change tracking
	if showDiff && len(preSnapshots) > 0 {
		var mountChanges []changeset.MountChanges
		for _, pre := range preSnapshots {
			Debug("Taking post-snapshot of %s", pre.source)
			postSnap, err := changeset.Take(pre.source)
			if err != nil {
				Debug("Failed to post-snapshot %s: %v", pre.source, err)
				continue
			}
			changes := changeset.Diff(pre.snap, postSnap)
			changes = changeset.FilterNoise(changes, pre.snap, postSnap)
			if len(changes) > 0 {
				mountChanges = append(mountChanges, changeset.MountChanges{
					Source:  pre.source,
					Target:  pre.target,
					Changes: changes,
				})
			}
		}

		// Read guest-side changes from bootstrap dir
		bootstrapDir := filepath.Join(home, ".faize", "sessions", sess.ID, "bootstrap")
		guestChanges, _ := changeset.ParseGuestChanges(filepath.Join(bootstrapDir, "guest-changes.txt"))

		// Read network + DNS logs from bootstrap dir
		networkEvents, netErr := changeset.CollectNetworkEvents(bootstrapDir)
		if netErr != nil {
			Debug("Failed to collect network events: %v", netErr)
		}

		cs := &changeset.SessionChangeset{
			SessionID:     sess.ID,
			MountChanges:  mountChanges,
			GuestChanges:  guestChanges,
			NetworkEvents: networkEvents,
		}

		// Display summary
		changeset.PrintSummary(os.Stdout, cs)

		// Save for later viewing with `faize diff`
		changesetPath := filepath.Join(bootstrapDir, "changeset.json")
		if err := os.MkdirAll(bootstrapDir, 0755); err == nil {
			if saveErr := changeset.SaveChangeset(changesetPath, cs); saveErr != nil {
				Debug("Failed to save changeset: %v", saveErr)
			}
		}
	}

	return nil
}

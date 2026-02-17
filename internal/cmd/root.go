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
	"github.com/spf13/cobra"
)

var (
	cfgFile     string
	projectDir  string
	mounts      []string
	networks    []string
	cpus        int
	memory      string
	timeout     string
	debug       bool
	minimalTest bool
)

// Debug prints a message if debug mode is enabled
func Debug(format string, args ...interface{}) {
	if debug {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

var rootCmd = &cobra.Command{
	Use:   "faize [flags]",
	Short: "Faize - AI development environments",
	Long: `Faize provides isolated, reproducible development environments for AI agents.

Create a sandboxed VM with network restrictions and controlled file access:
  faize                                           # uses current directory
  faize --project ~/code/myapp --mount ~/.npmrc

List running sessions:
  faize ps

Stop a session:
  faize stop <session-id>`,
	RunE: runRoot,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.faize/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")

	// Local flags for the root command
	rootCmd.Flags().StringVarP(&projectDir, "project", "p", "", "project directory to mount (default: current directory)")
	rootCmd.Flags().StringArrayVarP(&mounts, "mount", "m", []string{}, "additional mount paths (repeatable)")
	rootCmd.Flags().StringArrayVarP(&networks, "network", "n", []string{}, "network access policies (e.g., npm, pypi, github, all, none)")
	rootCmd.Flags().IntVar(&cpus, "cpus", 0, "number of CPUs (default from config)")
	rootCmd.Flags().StringVar(&memory, "memory", "", "memory limit (e.g., 4GB)")
	rootCmd.Flags().StringVarP(&timeout, "timeout", "t", "", "session timeout (e.g., 2h)")
	rootCmd.Flags().BoolVar(&minimalTest, "minimal-test", false, "minimal test mode: 1 CPU, 512MB RAM, no mounts, no network")
}

func initConfig() {
	// Config is loaded on-demand in runRoot
}

func runRoot(cmd *cobra.Command, args []string) error {
	// If no project specified, default to current directory
	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		projectDir = cwd
	}

	// Set debug env var for subpackages
	if debug {
		os.Setenv("FAIZE_DEBUG", "1")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	Debug("Config loaded successfully")

	// Minimal test mode overrides
	if minimalTest {
		fmt.Println("Running in minimal test mode...")
		cpus = 1
		memory = "512MB"
		timeout = "5m"
		networks = []string{"none"}
		mounts = []string{} // No additional mounts
	}

	// Apply defaults from config if not specified
	if cpus == 0 {
		cpus = cfg.Defaults.CPUs
	}
	if memory == "" {
		memory = cfg.Defaults.Memory
	}
	if timeout == "" {
		timeout = cfg.Defaults.Timeout
	}
	if len(networks) == 0 {
		networks = cfg.Defaults.Network
	}

	// Parse timeout duration
	timeoutDuration, err := time.ParseDuration(timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout format '%s': %w", timeout, err)
	}

	// Expand project directory
	projectMount, err := mount.Parse(projectDir)
	if err != nil {
		return fmt.Errorf("invalid project path: %w", err)
	}

	// Create mount validator with blocked paths
	validator, err := mount.NewValidator(cfg.BlockedPaths)
	if err != nil {
		return fmt.Errorf("failed to create mount validator: %w", err)
	}

	// Collect all mounts: project + auto mounts + user mounts
	allMountSpecs := []string{projectDir + ":rw"}
	allMountSpecs = append(allMountSpecs, cfg.AutoMounts...)
	allMountSpecs = append(allMountSpecs, mounts...)

	// Parse and validate all mounts
	var parsedMounts []session.VMMount
	for i, spec := range allMountSpecs {
		m, err := mount.Parse(spec)
		if err != nil {
			return fmt.Errorf("invalid mount '%s': %w", spec, err)
		}

		// Validate against blocked paths
		if err := validator.Validate(m); err != nil {
			return fmt.Errorf("mount validation failed: %w", err)
		}

		parsedMounts = append(parsedMounts, session.VMMount{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
			Tag:      fmt.Sprintf("mount%d", i),
		})
	}

	// Parse network policy
	policy := network.Parse(networks)
	if policy.AllowAll {
		Debug("Network policy: allow all traffic")
	} else if policy.Blocked {
		Debug("Network policy: no network access")
	} else {
		Debug("Network policy: allowed domains: %v", policy.Domains)
	}

	// Create VM configuration
	vmConfig := &vm.Config{
		ProjectDir: projectMount.Source,
		Mounts:     parsedMounts,
		Network:    networks,
		CPUs:       cpus,
		Memory:     memory,
		Timeout:    timeoutDuration,
	}

	// Print configuration (debug only)
	Debug("Session configuration:")
	Debug("  Project: %s", vmConfig.ProjectDir)
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

	projectName := filepath.Base(vmConfig.ProjectDir)
	fmt.Printf("\nSession %s | %s | %d CPUs, %s | %s timeout\n",
		sess.ID, projectName, vmConfig.CPUs, vmConfig.Memory, vmConfig.Timeout)

	// Attach to console
	fmt.Println("Attaching to console... (Ctrl+C to detach)")
	if err := manager.Attach(sess.ID); err != nil {
		return fmt.Errorf("failed to attach to console: %w", err)
	}

	return nil
}

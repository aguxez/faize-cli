package cmd

import (
	"fmt"

	"github.com/faize-ai/faize/internal/artifacts"
	"github.com/faize-ai/faize/internal/config"
	"github.com/spf13/cobra"
)

var claudeRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild Claude rootfs with extra dependencies",
	Long: `Rebuild the Claude rootfs image with extra dependencies from config.

This command reads claude.extra_deps from ~/.faize/config.yaml and bakes
those packages into the rootfs image at build time. This is more reliable
than installing packages at runtime since the rootfs has full apk support
during the build process.

Example config (~/.faize/config.yaml):
  claude:
    extra_deps:
      - go
      - rust
      - postgresql-client

After updating extra_deps, run this command to rebuild the rootfs:
  faize claude rebuild

Then start a new session:
  faize start`,
	RunE: runClaudeRebuild,
}

func init() {
	claudeCmd.AddCommand(claudeRebuildCmd)
}

func runClaudeRebuild(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create artifact manager
	manager, err := artifacts.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create artifact manager: %w", err)
	}

	extraDeps := cfg.Claude.ExtraDeps
	if len(extraDeps) == 0 {
		fmt.Println("No extra dependencies configured in ~/.faize/config.yaml")
		fmt.Println("Add packages under claude.extra_deps to bake them into the rootfs.")
		fmt.Println("\nRebuilding rootfs with default packages...")
	} else {
		fmt.Printf("Extra dependencies from config: %v\n", extraDeps)
		fmt.Println("\nRebuilding rootfs with extra packages...")
	}

	// Build rootfs with extra dependencies
	if err := manager.BuildClaudeRootfsWithDeps(extraDeps); err != nil {
		return fmt.Errorf("failed to rebuild rootfs: %w", err)
	}

	fmt.Println("\nRootfs rebuilt successfully!")
	fmt.Println("Start a new session with: faize start")

	return nil
}

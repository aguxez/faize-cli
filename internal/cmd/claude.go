package cmd

import (
	"github.com/spf13/cobra"
)

var claudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Manage Claude Code VM images",
	Long: `Manage Claude Code VM images and toolchain.

Commands:
  rebuild  Rebuild rootfs with extra dependencies from config

Examples:
  faize claude rebuild`,
}

func init() {
	rootCmd.AddCommand(claudeCmd)
}

package cmd

import (
	"github.com/spf13/cobra"
)

var claudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Manage Claude Code VM sessions",
	Long: `Manage Claude Code in isolated VM environments.

Commands:
  start    Start a new Claude Code session
  attach   Attach to a running session
  rebuild  Rebuild rootfs with extra dependencies from config

Examples:
  faize claude start --project ~/code/myapp
  faize claude attach abc123
  faize claude start -p ~/code/myapp --attach
  faize claude rebuild`,
}

func init() {
	rootCmd.AddCommand(claudeCmd)
}

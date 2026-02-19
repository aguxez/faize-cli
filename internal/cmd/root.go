package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	debug   bool
)

// Debug prints a message if debug mode is enabled
func Debug(format string, args ...interface{}) {
	if debug {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

var rootCmd = &cobra.Command{
	Use:   "faize",
	Short: "Faize - AI development environments",
	Long: `Faize provides isolated, reproducible development environments for AI agents.

Start a Claude Code session:
  faize start
  faize start --project ~/code/myapp

List running sessions:
  faize ps

Manage sessions:
  faize kill <session-id>
  faize prune`,
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
}

func initConfig() {
	// Config is loaded on-demand in subcommands
}

package cmd

import (
	"fmt"

	"github.com/faize-ai/faize/internal/vm"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <session-id>",
	Short: "Stop a running VM session",
	Long:  `Stop a running Faize VM session by its ID.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	// Try VZManager first, fall back to stub
	var manager vm.Manager
	vzManager, err := vm.NewVZManager()
	if err != nil {
		manager = vm.NewStubManager()
	} else {
		manager = vzManager
	}

	if err := manager.Stop(sessionID); err != nil {
		if err == vm.ErrVMNotImplemented {
			fmt.Println("[Phase 1] VM support not yet implemented.")
			fmt.Printf("Would stop session: %s\n", sessionID)
			return nil
		}
		return fmt.Errorf("failed to stop session %s: %w", sessionID, err)
	}

	fmt.Printf("Session %s stopped.\n", sessionID)
	return nil
}

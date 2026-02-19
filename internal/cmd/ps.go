package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/faize-ai/faize/internal/vm"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running VM sessions",
	Long:  `List all running Faize VM sessions with their status and details.`,
	RunE:  runPs,
}

func init() {
	rootCmd.AddCommand(psCmd)
}

func runPs(cmd *cobra.Command, args []string) error {
	// Try VZManager first, fall back to stub
	var manager vm.Manager
	vzManager, err := vm.NewVZManager()
	if err != nil {
		manager = vm.NewStubManager()
	} else {
		manager = vzManager
	}

	sessions, err := manager.List()
	if err != nil {
		if err == vm.ErrVMNotImplemented {
			fmt.Println("[Phase 1] VM support not yet implemented.")
			fmt.Println("No sessions to display.")
			return nil
		}
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No running sessions.")
		return nil
	}

	// Create tabwriter for aligned output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tPROJECT\tSTATUS\tSTARTED")
	_, _ = fmt.Fprintln(w, "--\t-------\t------\t-------")

	for _, session := range sessions {
		started := session.StartedAt.Format("2006-01-02 15:04:05")
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			session.ID,
			session.ProjectDir,
			session.Status,
			started,
		)
	}

	_ = w.Flush()
	return nil
}

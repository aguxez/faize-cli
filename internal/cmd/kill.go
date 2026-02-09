package cmd

import (
	"fmt"

	"github.com/faize-ai/faize/internal/session"
	"github.com/faize-ai/faize/internal/vm"
	"github.com/spf13/cobra"
)

var killForce bool

var killCmd = &cobra.Command{
	Use:   "kill",
	Short: "Remove VM sessions",
	Long: `Remove VM sessions from the system.

By default, only removes sessions with status "created" (not yet started).
Use --force to also stop and remove running sessions.

Note: Stopped sessions are handled by 'faize prune'.`,
	RunE: runKill,
}

func init() {
	rootCmd.AddCommand(killCmd)
	killCmd.Flags().BoolVarP(&killForce, "force", "f", false, "also stop and remove running sessions")
}

func runKill(cmd *cobra.Command, args []string) error {
	store, err := session.NewStore()
	if err != nil {
		return fmt.Errorf("failed to access session store: %w", err)
	}

	sessions, err := store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	// Create VM manager for stopping running sessions
	var manager vm.Manager
	vzManager, err := vm.NewVZManager()
	if err != nil {
		manager = vm.NewStubManager()
	} else {
		manager = vzManager
	}

	removedCount := 0
	skippedRunning := 0

	for _, sess := range sessions {
		switch sess.Status {
		case "created":
			// Always remove sessions that haven't started
			if err := store.Delete(sess.ID); err != nil {
				fmt.Printf("Warning: failed to delete session %s: %v\n", sess.ID, err)
			} else {
				fmt.Printf("Removed session: %s (created)\n", sess.ID)
				removedCount++
			}

		case "running":
			if killForce {
				// Stop the VM first
				if err := manager.Stop(sess.ID); err != nil {
					if err != vm.ErrVMNotImplemented {
						fmt.Printf("Warning: failed to stop session %s: %v\n", sess.ID, err)
					}
					// Continue to delete session metadata even if stop fails
				}
				// Delete the session
				if err := store.Delete(sess.ID); err != nil {
					fmt.Printf("Warning: failed to delete session %s: %v\n", sess.ID, err)
				} else {
					fmt.Printf("Stopped and removed session: %s (running)\n", sess.ID)
					removedCount++
				}
			} else {
				skippedRunning++
			}

		case "stopped":
			// Stopped sessions are handled by prune, skip them
			continue
		}
	}

	if skippedRunning > 0 {
		fmt.Printf("Skipped %d running session(s). Use --force to remove them.\n", skippedRunning)
	}

	if removedCount == 0 {
		fmt.Println("No sessions to remove.")
	} else {
		fmt.Printf("Removed %d session(s).\n", removedCount)
	}

	return nil
}

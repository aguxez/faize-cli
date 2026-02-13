package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/faize-ai/faize/internal/vm"
	"github.com/spf13/cobra"
)

var claudeAttachCmd = &cobra.Command{
	Use:   "attach <session-id>",
	Short: "Attach to a running Claude Code session",
	Long: `Attach to an existing running Claude Code VM session.

The session ID can be a partial match (prefix).

Examples:
  faize claude attach abc123
  faize claude attach abc  # partial match`,
	Args: cobra.ExactArgs(1),
	RunE: runClaudeAttach,
}

func init() {
	claudeCmd.AddCommand(claudeAttachCmd)
}

func runClaudeAttach(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	// Create VM manager
	manager, err := vm.NewVZManager()
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}

	// List sessions to find the target
	sessions, err := manager.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	// Find session by ID or prefix
	var targetID string
	for _, sess := range sessions {
		if sess.ID == sessionID || strings.HasPrefix(sess.ID, sessionID) {
			if sess.Status != "running" {
				return fmt.Errorf("session %s is not running (status: %s)", sess.ID, sess.Status)
			}
			targetID = sess.ID
			break
		}
	}

	if targetID == "" {
		return fmt.Errorf("session %s not found", sessionID)
	}

	fmt.Printf("Attaching to session %s... (~. to detach)\n", targetID)

	err = manager.Attach(targetID)
	if errors.Is(err, vm.ErrUserDetach) {
		fmt.Println("\nDetached from session")
		fmt.Printf("Session %s still running. Reattach with: faize claude attach %s\n", targetID, targetID)
		return nil
	}
	return err
}

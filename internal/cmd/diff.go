package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/faize-ai/faize/internal/changeset"
	"github.com/faize-ai/faize/internal/session"
	"github.com/spf13/cobra"
)

var diffJSON bool

var diffCmd = &cobra.Command{
	Use:   "diff [session-id]",
	Short: "Show changes from a session",
	Long: `Show file changes made during a faize session.

If no session-id is given, shows changes from the most recent session.

Examples:
  faize diff
  faize diff abc123
  faize diff --json`,
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().BoolVar(&diffJSON, "json", false, "output in JSON format")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	store, err := session.NewStore()
	if err != nil {
		return fmt.Errorf("failed to open session store: %w", err)
	}

	var sessionID string
	if len(args) > 0 {
		sessionID = args[0]
	} else {
		// Find most recent session
		sessionID, err = findMostRecentSession(store)
		if err != nil {
			return err
		}
	}

	// Look for changeset.json in session's bootstrap dir
	bootstrapDir := filepath.Join(store.Dir(), sessionID, "bootstrap")
	changesetPath := filepath.Join(bootstrapDir, "changeset.json")

	cs, err := changeset.LoadChangeset(changesetPath)
	if err != nil {
		return fmt.Errorf("no changeset found for session %s: %w", sessionID, err)
	}

	if diffJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cs)
	}

	// Filter noise paths from older saved changesets
	for i := range cs.MountChanges {
		cs.MountChanges[i].Changes = changeset.FilterPaths(cs.MountChanges[i].Changes)
	}
	changeset.PrintSummary(os.Stdout, cs)
	return nil
}

// findMostRecentSession returns the ID of the most recently started session.
func findMostRecentSession(store *session.Store) (string, error) {
	sessions, err := store.List()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions found")
	}

	// Sort by StartedAt descending
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})

	return sessions[0].ID, nil
}

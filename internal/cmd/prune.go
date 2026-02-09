package cmd

import (
	"fmt"

	"github.com/faize-ai/faize/internal/artifacts"
	"github.com/faize-ai/faize/internal/session"
	"github.com/spf13/cobra"
)

var (
	pruneAll      bool
	pruneArtifacts bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Clean up VM images and caches",
	Long: `Clean up unused VM images and caches to free up disk space.

This command removes:
  - Stopped VM sessions
  - Unused base images (with --artifacts)
  - Build caches`,
	RunE: runPrune,
}

func init() {
	rootCmd.AddCommand(pruneCmd)
	pruneCmd.Flags().BoolVarP(&pruneAll, "all", "a", false, "remove all sessions (including running)")
	pruneCmd.Flags().BoolVar(&pruneArtifacts, "artifacts", false, "also remove downloaded artifacts (kernel, rootfs)")
}

func runPrune(cmd *cobra.Command, args []string) error {
	fmt.Println("Cleaning up VM sessions and caches...")

	// Clean up sessions
	store, err := session.NewStore()
	if err != nil {
		return fmt.Errorf("failed to access session store: %w", err)
	}

	sessions, err := store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	removedCount := 0
	for _, sess := range sessions {
		if pruneAll || sess.Status == "stopped" {
			if err := store.Delete(sess.ID); err != nil {
				fmt.Printf("Warning: failed to delete session %s: %v\n", sess.ID, err)
			} else {
				fmt.Printf("Removed session: %s\n", sess.ID)
				removedCount++
			}
		}
	}

	if removedCount == 0 {
		fmt.Println("No sessions to remove.")
	} else {
		fmt.Printf("Removed %d session(s).\n", removedCount)
	}

	// Optionally clean artifacts
	if pruneArtifacts {
		fmt.Println("\nCleaning up artifacts...")
		artifactMgr, err := artifacts.NewManager()
		if err != nil {
			return fmt.Errorf("failed to access artifact manager: %w", err)
		}

		if err := artifactMgr.Clean(); err != nil {
			return fmt.Errorf("failed to clean artifacts: %w", err)
		}
		fmt.Println("Artifacts removed.")
	}

	return nil
}

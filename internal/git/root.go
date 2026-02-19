package git

import (
	"os/exec"
	"strings"
)

// FindRoot returns the git repository root for the given directory,
// or an empty string if the directory is not inside a git repository.
func FindRoot(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

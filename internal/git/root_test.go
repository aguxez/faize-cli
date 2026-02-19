package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo initializes a git repository in dir, setting minimal config
// to avoid pollution from the global git config.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	cmd := exec.Command("git", "init", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git init failed: %s", out)

	for _, args := range [][]string{
		{"git", "-C", dir, "config", "user.email", "test@example.com"},
		{"git", "-C", dir, "config", "user.name", "Test User"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git config failed: %s", out)
	}
}

func TestFindRoot_ReturnsRepoRoot(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	got := FindRoot(dir)
	assert.Equal(t, dir, got)
}

func TestFindRoot_Subdir_ReturnsRepoRoot(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	subdir := filepath.Join(dir, "some", "nested", "subdir")
	err := os.MkdirAll(subdir, 0o755)
	require.NoError(t, err)

	got := FindRoot(subdir)
	assert.Equal(t, dir, got)
}

func TestFindRoot_NonGitDir_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	// Ensure it is NOT a git repo by not calling initGitRepo.

	got := FindRoot(dir)
	assert.Equal(t, "", got)
}

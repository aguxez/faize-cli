package changeset

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTake_BasicFiles(t *testing.T) {
	// Create temp dir with a few files, verify snapshot entries
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "file2.go"), []byte("package main"), 0644)
	_ = os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "subdir", "nested.txt"), []byte("nested"), 0644)

	snap, err := Take(dir)
	require.NoError(t, err)
	assert.Len(t, snap, 4) // file1, file2, subdir, subdir/nested
	assert.Equal(t, int64(5), snap["file1.txt"].Size)
	assert.True(t, snap["subdir"].IsDir)
	assert.Equal(t, int64(6), snap["subdir/nested.txt"].Size)
}

func TestTake_SkipsGitContents(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	_ = os.MkdirAll(filepath.Join(gitDir, "objects"), 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	snap, err := Take(dir)
	require.NoError(t, err)
	// Should have .git dir entry but NOT its children
	assert.Contains(t, snap, ".git")
	assert.True(t, snap[".git"].IsDir)
	assert.NotContains(t, snap, ".git/HEAD")
	assert.NotContains(t, snap, ".git/objects")
	assert.Contains(t, snap, "main.go")
}

func TestTake_SummarizesNodeModules(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules")
	_ = os.MkdirAll(nmDir, 0755)
	// Create a few fake packages
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(filepath.Join(nmDir, fmt.Sprintf("pkg%d", i)), []byte("x"), 0644)
	}

	snap, err := Take(dir)
	require.NoError(t, err)
	assert.Contains(t, snap, "node_modules")
	assert.True(t, snap["node_modules"].IsDir)
	assert.Equal(t, 5, snap["node_modules"].ChildCount)
	// Should NOT contain children
	assert.NotContains(t, snap, "node_modules/pkg0")
}

func TestTake_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	snap, err := Take(dir)
	require.NoError(t, err)
	assert.Empty(t, snap)
}

func TestDiff_Created(t *testing.T) {
	before := Snapshot{}
	after := Snapshot{
		"new.txt": FileEntry{Path: "new.txt", Size: 100},
	}
	changes := Diff(before, after)
	assert.Len(t, changes, 1)
	assert.Equal(t, "created", changes[0].Type)
	assert.Equal(t, "new.txt", changes[0].Path)
	assert.Equal(t, int64(100), changes[0].NewSize)
}

func TestDiff_Deleted(t *testing.T) {
	before := Snapshot{
		"old.txt": FileEntry{Path: "old.txt", Size: 50},
	}
	after := Snapshot{}
	changes := Diff(before, after)
	assert.Len(t, changes, 1)
	assert.Equal(t, "deleted", changes[0].Type)
	assert.Equal(t, int64(50), changes[0].OldSize)
}

func TestDiff_Modified(t *testing.T) {
	now := time.Now()
	before := Snapshot{
		"file.txt": FileEntry{Path: "file.txt", Size: 100, ModTime: now},
	}
	after := Snapshot{
		"file.txt": FileEntry{Path: "file.txt", Size: 200, ModTime: now.Add(time.Second)},
	}
	changes := Diff(before, after)
	assert.Len(t, changes, 1)
	assert.Equal(t, "modified", changes[0].Type)
	assert.Equal(t, int64(100), changes[0].OldSize)
	assert.Equal(t, int64(200), changes[0].NewSize)
}

func TestDiff_NoChanges(t *testing.T) {
	now := time.Now()
	snap := Snapshot{
		"file.txt": FileEntry{Path: "file.txt", Size: 100, ModTime: now},
	}
	changes := Diff(snap, snap)
	assert.Empty(t, changes)
}

func TestDiff_SortedOutput(t *testing.T) {
	before := Snapshot{}
	after := Snapshot{
		"z.txt": FileEntry{Path: "z.txt", Size: 1},
		"a.txt": FileEntry{Path: "a.txt", Size: 2},
		"m.txt": FileEntry{Path: "m.txt", Size: 3},
	}
	changes := Diff(before, after)
	assert.Len(t, changes, 3)
	assert.Equal(t, "a.txt", changes[0].Path)
	assert.Equal(t, "m.txt", changes[1].Path)
	assert.Equal(t, "z.txt", changes[2].Path)
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	now := time.Now().Truncate(time.Millisecond) // JSON loses sub-ms precision
	original := Snapshot{
		"file.txt": FileEntry{Path: "file.txt", Size: 42, ModTime: now, Mode: 0644},
	}
	require.NoError(t, original.Save(path))

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, original["file.txt"].Size, loaded["file.txt"].Size)
	assert.Equal(t, original["file.txt"].Path, loaded["file.txt"].Path)
}

func TestParseGuestChanges_MissingFile(t *testing.T) {
	lines, err := ParseGuestChanges("/nonexistent/path")
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestParseGuestChanges_WithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guest-changes.txt")
	content := "/etc/resolv.conf\n/home/claude/.cache/pip/something\n\n/usr/bin/newpkg\n"
	_ = os.WriteFile(path, []byte(content), 0644)

	lines, err := ParseGuestChanges(path)
	require.NoError(t, err)
	assert.Len(t, lines, 3)
	assert.Equal(t, "/etc/resolv.conf", lines[0])
}

func TestFilterNoise_RemovesDirectories(t *testing.T) {
	before := Snapshot{}
	after := Snapshot{
		"internal/cmd":          FileEntry{Path: "internal/cmd", IsDir: true},
		"internal/cmd/start.go": FileEntry{Path: "internal/cmd/start.go", Size: 100},
	}
	changes := []Change{
		{Path: "internal/cmd", Type: "modified"},
		{Path: "internal/cmd/start.go", Type: "modified", NewSize: 100},
	}
	filtered := FilterNoise(changes, before, after)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "internal/cmd/start.go", filtered[0].Path)
}

func TestFilterNoise_RemovesIgnoredPrefixes(t *testing.T) {
	before := Snapshot{}
	after := Snapshot{
		".git/HEAD":              FileEntry{Path: ".git/HEAD", Size: 40},
		".omc/state.json":       FileEntry{Path: ".omc/state.json", Size: 200},
		".claude/settings.json": FileEntry{Path: ".claude/settings.json", Size: 50},
		"main.go":               FileEntry{Path: "main.go", Size: 300},
	}
	changes := []Change{
		{Path: ".git/HEAD", Type: "modified"},
		{Path: ".omc/state.json", Type: "created"},
		{Path: ".claude/settings.json", Type: "modified"},
		{Path: "main.go", Type: "modified"},
	}
	filtered := FilterNoise(changes, before, after)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "main.go", filtered[0].Path)
}

func TestFilterNoise_KeepsRegularFiles(t *testing.T) {
	before := Snapshot{
		"old.go": FileEntry{Path: "old.go", Size: 50},
	}
	after := Snapshot{
		"old.go": FileEntry{Path: "old.go", Size: 100},
		"new.go": FileEntry{Path: "new.go", Size: 200},
	}
	changes := Diff(before, after)
	filtered := FilterNoise(changes, before, after)
	assert.Len(t, filtered, 2)
}

func TestFilterPaths_RemovesIgnoredPrefixes(t *testing.T) {
	changes := []Change{
		{Path: ".git/HEAD", Type: "modified"},
		{Path: ".omc/notepad.md", Type: "created"},
		{Path: "src/main.go", Type: "modified"},
	}
	filtered := FilterPaths(changes)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "src/main.go", filtered[0].Path)
}

func TestFilterNoise_EmptyInput(t *testing.T) {
	filtered := FilterNoise(nil, Snapshot{}, Snapshot{})
	assert.Nil(t, filtered)
}

func TestFilterPaths_ExactPrefixMatch(t *testing.T) {
	// ".github" should NOT be filtered (doesn't match ".git" prefix)
	changes := []Change{
		{Path: ".github/workflows/ci.yml", Type: "created"},
		{Path: ".gitignore", Type: "modified"},
		{Path: ".git/HEAD", Type: "modified"},
	}
	filtered := FilterPaths(changes)
	assert.Len(t, filtered, 2)
	assert.Equal(t, ".github/workflows/ci.yml", filtered[0].Path)
	assert.Equal(t, ".gitignore", filtered[1].Path)
}

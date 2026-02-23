package changeset

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileEntry records a single file's metadata at snapshot time.
type FileEntry struct {
	Path    string      `json:"path"`
	Size    int64       `json:"size"`
	ModTime time.Time   `json:"mod_time"`
	Mode    os.FileMode `json:"mode"`
	IsDir   bool        `json:"is_dir"`
	// For summarized directories (node_modules, etc): count of children
	ChildCount int `json:"child_count,omitempty"`
}

// Snapshot is a map of relative paths to FileEntry.
type Snapshot map[string]FileEntry

// Take walks a directory and returns a Snapshot.
// - Uses filepath.WalkDir for efficiency
// - Skips .git directory contents (records .git dir entry itself only)
// - For node_modules or any dir with >500 direct children: records dir entry + child count, doesn't recurse
// - All paths are relative to root
func Take(root string) (Snapshot, error) {
	snap := make(Snapshot)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Skip the root itself
		if rel == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		entry := FileEntry{
			Path:    rel,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Mode:    info.Mode(),
			IsDir:   d.IsDir(),
		}

		// Handle .git: record dir entry, skip contents
		if d.IsDir() && d.Name() == ".git" {
			snap[rel] = entry
			return filepath.SkipDir
		}

		// For directories, check child count before deciding to recurse
		if d.IsDir() {
			children, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			childCount := len(children)
			entry.ChildCount = childCount

			// Summarize large dirs (node_modules or >500 direct children)
			if d.Name() == "node_modules" || childCount > 500 {
				snap[rel] = entry
				return filepath.SkipDir
			}
		}

		snap[rel] = entry
		return nil
	})
	if err != nil {
		return nil, err
	}

	return snap, nil
}

// Change represents a single file change.
type Change struct {
	Path    string `json:"path"`              // relative to mount root
	Type    string `json:"type"`              // "created", "modified", "deleted"
	OldSize int64  `json:"old_size,omitempty"`
	NewSize int64  `json:"new_size,omitempty"`
}

// Diff compares two snapshots and returns changes.
// - Files in after but not before = "created"
// - Files in before but not after = "deleted"
// - Files in both but with different size or modtime = "modified"
func Diff(before, after Snapshot) []Change {
	var changes []Change

	// Check for created and modified
	for path, afterEntry := range after {
		beforeEntry, exists := before[path]
		if !exists {
			changes = append(changes, Change{
				Path:    path,
				Type:    "created",
				NewSize: afterEntry.Size,
			})
			continue
		}
		if beforeEntry.Size != afterEntry.Size || !beforeEntry.ModTime.Equal(afterEntry.ModTime) {
			changes = append(changes, Change{
				Path:    path,
				Type:    "modified",
				OldSize: beforeEntry.Size,
				NewSize: afterEntry.Size,
			})
		}
	}

	// Check for deleted
	for path, beforeEntry := range before {
		if _, exists := after[path]; !exists {
			changes = append(changes, Change{
				Path:    path,
				Type:    "deleted",
				OldSize: beforeEntry.Size,
			})
		}
	}

	// Sort by path for deterministic output
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})

	return changes
}

// MountChanges groups changes by mount source.
type MountChanges struct {
	Source  string   `json:"source"`  // host path
	Target  string   `json:"target"`  // guest path
	Changes []Change `json:"changes"`
}

// SessionChangeset is the complete changeset for a session.
type SessionChangeset struct {
	SessionID    string         `json:"session_id"`
	MountChanges []MountChanges `json:"mount_changes"`
	GuestChanges []string       `json:"guest_changes"` // lines from guest-changes.txt
}

// Save persists a snapshot to JSON file.
func (s Snapshot) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load reads a snapshot from JSON file.
func Load(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return snap, nil
}

// SaveChangeset saves a SessionChangeset to JSON.
func SaveChangeset(path string, cs *SessionChangeset) error {
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadChangeset loads a SessionChangeset from JSON.
func LoadChangeset(path string) (*SessionChangeset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cs SessionChangeset
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// ParseGuestChanges reads guest-changes.txt and returns the lines.
// Returns empty slice and nil error if the file doesn't exist.
func ParseGuestChanges(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if lines == nil {
		return []string{}, nil
	}
	return lines, nil
}

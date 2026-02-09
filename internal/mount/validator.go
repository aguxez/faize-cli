package mount

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
)

// Validator validates mount paths against a list of blocked paths
type Validator struct {
	blockedPaths []string // Expanded absolute paths
}

// NewValidator creates a new Validator with the given blocked paths.
// Each blocked path is expanded and normalized to an absolute path.
// Symlinks in blocked paths are also resolved to ensure consistent comparison.
func NewValidator(blockedPaths []string) (*Validator, error) {
	expanded := make([]string, 0, len(blockedPaths))

	for _, path := range blockedPaths {
		if path == "" {
			continue
		}

		// Expand ~ to home directory
		expandedPath, err := homedir.Expand(path)
		if err != nil {
			return nil, fmt.Errorf("failed to expand blocked path '%s': %w", path, err)
		}

		// Convert to absolute path
		absPath, err := filepath.Abs(expandedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to convert blocked path '%s' to absolute: %w", path, err)
		}

		// Resolve symlinks in blocked paths for consistent comparison
		// (e.g., /etc -> /private/etc on macOS)
		realPath, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			// Path may not exist yet, use the cleaned absolute path
			realPath = filepath.Clean(absPath)
		}

		expanded = append(expanded, realPath)
	}

	return &Validator{
		blockedPaths: expanded,
	}, nil
}

// Validate checks if the mount's source path is under or equal to any blocked path.
// Returns an error if the mount is blocked.
func (v *Validator) Validate(m *Mount) error {
	if m == nil {
		return fmt.Errorf("mount cannot be nil")
	}

	// First, expand ~ and convert to absolute path
	sourcePath, err := homedir.Expand(m.Source)
	if err != nil {
		sourcePath = m.Source
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		sourcePath = filepath.Clean(m.Source)
	}

	// Resolve symlinks to get the real path
	realPath, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		// Path doesn't exist or can't be resolved - use the absolute path
		// This handles paths that don't exist yet
		realPath = sourcePath
	}

	// Check against each blocked path
	for _, blocked := range v.blockedPaths {
		if isUnderOrEqual(realPath, blocked) {
			if realPath != sourcePath {
				return fmt.Errorf("mount blocked: %s resolves to protected path %s", m.Source, blocked)
			}
			return fmt.Errorf("mount blocked: %s is a protected path", blocked)
		}
	}

	return nil
}

// isUnderOrEqual returns true if testPath is under or equal to basePath.
// This handles path prefixes correctly:
//   - "/home/user/.ssh" is under "/home/user/.ssh" (equal)
//   - "/home/user/.ssh/id_rsa" is under "/home/user/.ssh"
//   - "/home/user/.sshrc" is NOT under "/home/user/.ssh"
func isUnderOrEqual(testPath, basePath string) bool {
	// Exact match
	if testPath == basePath {
		return true
	}

	// Check if testPath is under basePath
	// Ensure basePath ends with separator for proper prefix matching
	baseWithSep := basePath
	if !strings.HasSuffix(baseWithSep, string(filepath.Separator)) {
		baseWithSep += string(filepath.Separator)
	}

	return strings.HasPrefix(testPath, baseWithSep)
}

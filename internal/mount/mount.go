package mount

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
)

// Mount represents a host path to be mounted into the guest container
type Mount struct {
	Source   string // Host path (expanded absolute path)
	Target   string // Guest path (defaults to same as source)
	ReadOnly bool   // Default true
}

// Parse parses a mount specification string into a Mount struct.
//
// Formats:
//   - "~/.npmrc" -> Mount{Source: expanded path, Target: expanded path, ReadOnly: true}
//   - "~/.cache/pip:rw" -> Mount{Source: expanded path, Target: expanded path, ReadOnly: false}
//   - "/path:/guest/path:ro" -> Mount{Source: "/path", Target: "/guest/path", ReadOnly: true}
//   - "/path:/guest/path:rw" -> Mount{Source: "/path", Target: "/guest/path", ReadOnly: false}
//
// Default behavior:
//   - Target defaults to Source if not specified
//   - ReadOnly defaults to true unless ":rw" is specified
func Parse(spec string) (*Mount, error) {
	if spec == "" {
		return nil, fmt.Errorf("mount specification cannot be empty")
	}

	parts := strings.Split(spec, ":")

	mount := &Mount{
		ReadOnly: true, // Default to read-only
	}

	// Parse source path (required)
	sourcePath, err := expandPath(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid source path: %w", err)
	}
	mount.Source = sourcePath

	// Parse optional target and mode
	switch len(parts) {
	case 1:
		// Only source specified: "/path"
		mount.Target = mount.Source
	case 2:
		// Two parts: either "/path:mode" or "/path:/target"
		if parts[1] == "ro" || parts[1] == "rw" {
			// Format: "/path:mode"
			mount.Target = mount.Source
			mount.ReadOnly = (parts[1] == "ro")
		} else {
			// Format: "/path:/target"
			targetPath, err := expandPath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid target path: %w", err)
			}
			mount.Target = targetPath
		}
	case 3:
		// Three parts: "/path:/target:mode"
		targetPath, err := expandPath(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid target path: %w", err)
		}
		mount.Target = targetPath

		switch parts[2] {
		case "ro":
			mount.ReadOnly = true
		case "rw":
			mount.ReadOnly = false
		default:
			return nil, fmt.Errorf("invalid mode '%s': must be 'ro' or 'rw'", parts[2])
		}
	default:
		return nil, fmt.Errorf("invalid mount specification: too many colons")
	}

	return mount, nil
}

// expandPath expands ~ to home directory and returns an absolute path
func expandPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Expand ~ to home directory
	expanded, err := homedir.Expand(path)
	if err != nil {
		return "", fmt.Errorf("failed to expand path: %w", err)
	}

	// Convert to absolute path and normalize
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("failed to convert to absolute path: %w", err)
	}

	// Clean the path (resolve .., ., remove duplicate slashes)
	return filepath.Clean(abs), nil
}

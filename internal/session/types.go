package session

import "time"

// VMMount represents a VirtioFS mount between host and guest
type VMMount struct {
	Source   string `json:"source"`    // Host path
	Target   string `json:"target"`    // Guest path
	ReadOnly bool   `json:"read_only"` // Whether mount is read-only
	Tag      string `json:"tag"`       // VirtioFS mount tag
}

// Session represents a VM session with its configuration
type Session struct {
	ID         string     `json:"id"`
	ProjectDir string     `json:"project_dir"`
	Mounts     []VMMount  `json:"mounts"`
	Network    []string   `json:"network"`
	CPUs       int        `json:"cpus"`
	Memory     string     `json:"memory"`
	Status     string     `json:"status"` // "created", "running", "stopped"
	StartedAt  time.Time  `json:"started_at"`
	ClaudeMode bool       `json:"claude_mode"`       // Whether using Claude rootfs
	Timeout    string     `json:"timeout,omitempty"` // e.g., "2h" - human-readable timeout
	StoppedAt  *time.Time `json:"stopped_at,omitempty"`
	ExitReason string     `json:"exit_reason,omitempty"` // "normal" | "timeout" | "detach" | "killed"
}

package vm

import (
	"time"

	"github.com/faize-ai/faize/internal/network"
	"github.com/faize-ai/faize/internal/session"
)

type Config struct {
	ProjectDir    string
	Mounts        []session.VMMount
	Network       []string
	NetworkPolicy *network.Policy
	CPUs          int
	Memory        string
	Timeout       time.Duration
	ClaudeMode    bool
	HostClaudeDir string
	ToolchainDir  string
}

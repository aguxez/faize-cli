package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionSerialization(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	stopped := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	t.Run("serializes all fields including new ones", func(t *testing.T) {
		s := Session{
			ID:         "test-session-1",
			ProjectDir: "/home/user/project",
			Mounts: []VMMount{
				{Source: "/host/path", Target: "/guest/path", ReadOnly: true, Tag: "mount0"},
			},
			Network:    []string{"npm", "github"},
			CPUs:       2,
			Memory:     "4GB",
			Status:     "stopped",
			StartedAt:  now,
			ClaudeMode: true,
			Timeout:    "2h",
			StoppedAt:  &stopped,
			ExitReason: "timeout",
		}

		data, err := json.Marshal(s)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, "test-session-1", m["id"])
		assert.Equal(t, "2h", m["timeout"])
		assert.Equal(t, "timeout", m["exit_reason"])
		assert.NotEmpty(t, m["stopped_at"])
	})

	t.Run("deserializes new fields from JSON", func(t *testing.T) {
		input := `{
			"id": "test-session-2",
			"project_dir": "/tmp/proj",
			"mounts": null,
			"network": null,
			"cpus": 4,
			"memory": "8GB",
			"status": "stopped",
			"started_at": "2024-01-15T10:00:00Z",
			"claude_mode": false,
			"timeout": "1h",
			"stopped_at": "2024-01-15T11:00:00Z",
			"exit_reason": "normal"
		}`

		var s Session
		require.NoError(t, json.Unmarshal([]byte(input), &s))

		assert.Equal(t, "test-session-2", s.ID)
		assert.Equal(t, "1h", s.Timeout)
		assert.Equal(t, "normal", s.ExitReason)
		require.NotNil(t, s.StoppedAt)
		assert.Equal(t, time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC), *s.StoppedAt)
	})

	t.Run("omitempty omits zero-value new fields", func(t *testing.T) {
		s := Session{
			ID:         "test-session-3",
			ProjectDir: "/tmp/proj",
			CPUs:       2,
			Memory:     "4GB",
			Status:     "running",
			StartedAt:  now,
		}

		data, err := json.Marshal(s)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.NotContains(t, m, "timeout")
		assert.NotContains(t, m, "stopped_at")
		assert.NotContains(t, m, "exit_reason")
	})
}

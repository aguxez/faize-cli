package network

import (
	"sort"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		wantAll  bool
		wantBlocked bool
		wantDomains []string
	}{
		{
			name:        "empty specs defaults to blocked",
			input:       []string{},
			wantAll:     false,
			wantBlocked: true,
			wantDomains: []string{},
		},
		{
			name:        "all allows everything",
			input:       []string{"all"},
			wantAll:     true,
			wantBlocked: false,
			wantDomains: []string{},
		},
		{
			name:        "none blocks network",
			input:       []string{"none"},
			wantAll:     false,
			wantBlocked: true,
			wantDomains: []string{},
		},
		{
			name:        "single preset npm",
			input:       []string{"npm"},
			wantAll:     false,
			wantBlocked: false,
			wantDomains: []string{"registry.npmjs.org", "npmjs.com"},
		},
		{
			name:    "multiple presets",
			input:   []string{"npm", "github"},
			wantAll: false,
			wantBlocked: false,
			wantDomains: []string{"registry.npmjs.org", "npmjs.com", "github.com", "api.github.com", "raw.githubusercontent.com"},
		},
		{
			name:    "preset with literal domain",
			input:   []string{"npm", "custom.example.com"},
			wantAll: false,
			wantBlocked: false,
			wantDomains: []string{"registry.npmjs.org", "npmjs.com", "custom.example.com"},
		},
		{
			name:    "case insensitive",
			input:   []string{"NPM", "GitHub"},
			wantAll: false,
			wantBlocked: false,
			wantDomains: []string{"registry.npmjs.org", "npmjs.com", "github.com", "api.github.com", "raw.githubusercontent.com"},
		},
		{
			name:    "duplicate domains removed",
			input:   []string{"npm", "npm"},
			wantAll: false,
			wantBlocked: false,
			wantDomains: []string{"registry.npmjs.org", "npmjs.com"},
		},
		{
			name:    "all overrides other specs",
			input:   []string{"npm", "all"},
			wantAll: true,
			wantBlocked: false,
			wantDomains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := Parse(tt.input)

			if policy.AllowAll != tt.wantAll {
				t.Errorf("AllowAll = %v, want %v", policy.AllowAll, tt.wantAll)
			}

			if policy.Blocked != tt.wantBlocked {
				t.Errorf("Blocked = %v, want %v", policy.Blocked, tt.wantBlocked)
			}

			// Sort for comparison
			sort.Strings(policy.Domains)
			sort.Strings(tt.wantDomains)

			if len(policy.Domains) != len(tt.wantDomains) {
				t.Errorf("Domains length = %d, want %d. Got: %v", len(policy.Domains), len(tt.wantDomains), policy.Domains)
				return
			}

			for i, domain := range policy.Domains {
				if domain != tt.wantDomains[i] {
					t.Errorf("Domains[%d] = %s, want %s", i, domain, tt.wantDomains[i])
				}
			}
		})
	}
}

func TestPresetsExist(t *testing.T) {
	expectedPresets := []string{"npm", "pypi", "github", "anthropic", "openai"}

	for _, preset := range expectedPresets {
		if _, ok := Presets[preset]; !ok {
			t.Errorf("Preset %q not found", preset)
		}
	}
}

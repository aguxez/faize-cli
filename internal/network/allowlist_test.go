package network

import (
	"sort"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		input         []string
		wantAll       bool
		wantBlocked   bool
		wantDomains   []string
		wantWildcards []string
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
			name:        "all overrides other specs",
			input:       []string{"npm", "all"},
			wantAll:     true,
			wantBlocked: false,
			wantDomains: []string{},
		},
		// Wildcard test cases
		{
			name:          "simple wildcard",
			input:         []string{"*.example.com"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{},
			wantWildcards: []string{"*.example.com"},
		},
		{
			name:          "mixed domains and wildcards",
			input:         []string{"npm", "*.example.com", "custom.org"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{"registry.npmjs.org", "npmjs.com", "custom.org"},
			wantWildcards: []string{"*.example.com"},
		},
		{
			name:          "preset with wildcard",
			input:         []string{"github", "*.internal.company.com"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{"github.com", "api.github.com", "raw.githubusercontent.com"},
			wantWildcards: []string{"*.internal.company.com"},
		},
		{
			name:          "duplicate wildcards removed",
			input:         []string{"*.example.com", "*.example.com"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{},
			wantWildcards: []string{"*.example.com"},
		},
		{
			name:          "all overrides wildcards",
			input:         []string{"*.example.com", "all"},
			wantAll:       true,
			wantBlocked:   false,
			wantDomains:   []string{},
			wantWildcards: []string{},
		},
		{
			name:          "invalid TLD wildcard rejected",
			input:         []string{"*.com"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{},
			wantWildcards: []string{},
		},
		{
			name:          "invalid recursive wildcard rejected",
			input:         []string{"**.example.com"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{"**.example.com"}, // Doesn't start with *., treated as literal
			wantWildcards: []string{},
		},
		{
			name:          "invalid mid-level wildcard rejected",
			input:         []string{"sub.*.example.com"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{"sub.*.example.com"}, // Treated as literal (doesn't start with *.)
			wantWildcards: []string{},
		},
		{
			name:          "multiple wildcards",
			input:         []string{"*.foo.com", "*.bar.org"},
			wantAll:       false,
			wantBlocked:   false,
			wantDomains:   []string{},
			wantWildcards: []string{"*.foo.com", "*.bar.org"},
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

			// Check wildcards
			sort.Strings(policy.Wildcards)
			sort.Strings(tt.wantWildcards)

			if len(policy.Wildcards) != len(tt.wantWildcards) {
				t.Errorf("Wildcards length = %d, want %d. Got: %v", len(policy.Wildcards), len(tt.wantWildcards), policy.Wildcards)
				return
			}

			for i, wildcard := range policy.Wildcards {
				if wildcard != tt.wantWildcards[i] {
					t.Errorf("Wildcards[%d] = %s, want %s", i, wildcard, tt.wantWildcards[i])
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

func TestIsWildcard(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"*.example.com", true},
		{"*.foo.bar.com", true},
		{"example.com", false},
		{"sub.example.com", false},
		{"sub.*.example.com", false}, // Doesn't start with *.
		{"**example.com", false},     // Doesn't have the dot
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsWildcard(tt.input)
			if got != tt.want {
				t.Errorf("IsWildcard(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateWildcard(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		// Valid patterns
		{"*.example.com", false},
		{"*.foo.bar.com", false},
		{"*.sub.example.org", false},

		// Invalid: TLD wildcards
		{"*.com", true},
		{"*.org", true},
		{"*.io", true},

		// Invalid: recursive wildcards
		{"**.example.com", true},

		// Invalid: mid-level wildcards
		{"*.*.example.com", true},

		// Invalid: not a wildcard
		{"example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateWildcard(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWildcard(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestExtractBaseDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"*.example.com", "example.com"},
		{"*.foo.bar.com", "foo.bar.com"},
		{"*.sub.example.org", "sub.example.org"},
		{"example.com", "example.com"}, // No-op for non-wildcards
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExtractBaseDomain(tt.input)
			if got != tt.want {
				t.Errorf("ExtractBaseDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

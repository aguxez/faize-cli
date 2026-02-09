package network

import (
	"strings"
)

// Preset domain groups
var Presets = map[string][]string{
	"npm":       {"registry.npmjs.org", "npmjs.com"},
	"pypi":      {"pypi.org", "files.pythonhosted.org"},
	"github":    {"github.com", "api.github.com", "raw.githubusercontent.com"},
	"anthropic": {"api.anthropic.com", "anthropic.com"},
	"openai":    {"api.openai.com", "openai.com"},
}

// Special values
const (
	NetworkAll  = "all"  // Allow all traffic
	NetworkNone = "none" // No network access
)

// Policy represents network access permissions
type Policy struct {
	AllowAll bool     // Allow all traffic
	Blocked  bool     // No network access
	Domains  []string // Allowed domains
}

// Parse converts network specs like "npm,pypi,github" into a Policy.
// Examples:
//   - Parse([]string{"npm", "pypi"}) -> Policy{Domains: ["registry.npmjs.org", "npmjs.com", "pypi.org", ...]}
//   - Parse([]string{"all"}) -> Policy{AllowAll: true}
//   - Parse([]string{"none"}) -> Policy{Blocked: true}
func Parse(specs []string) *Policy {
	policy := &Policy{
		Domains: []string{},
	}

	if len(specs) == 0 {
		policy.Blocked = true
		return policy
	}

	// First pass: check for special values "all" or "none"
	// These take precedence regardless of position
	for _, spec := range specs {
		spec = strings.TrimSpace(strings.ToLower(spec))

		if spec == NetworkAll {
			return &Policy{
				AllowAll: true,
				Domains:  []string{},
			}
		}
		if spec == NetworkNone {
			return &Policy{
				Blocked: true,
				Domains: []string{},
			}
		}
	}

	// Second pass: process presets and domains
	for _, spec := range specs {
		spec = strings.TrimSpace(strings.ToLower(spec))

		// Check if it's a preset
		if presetDomains, ok := Presets[spec]; ok {
			policy.Domains = append(policy.Domains, presetDomains...)
		} else {
			// Treat as a literal domain
			policy.Domains = append(policy.Domains, spec)
		}
	}

	// Remove duplicates
	policy.Domains = deduplicateDomains(policy.Domains)

	return policy
}

// deduplicateDomains removes duplicate domains from a slice
func deduplicateDomains(domains []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, domain := range domains {
		if !seen[domain] {
			seen[domain] = true
			result = append(result, domain)
		}
	}

	return result
}

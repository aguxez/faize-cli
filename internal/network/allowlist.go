package network

import (
	"fmt"
	"strings"
)

// Preset domain groups
var Presets = map[string][]string{
	"npm":       {"registry.npmjs.org", "npmjs.com"},
	"pypi":      {"pypi.org", "files.pythonhosted.org"},
	"github":    {"github.com", "api.github.com", "raw.githubusercontent.com"},
	"anthropic": {"api.anthropic.com", "anthropic.com"},
	"openai":    {"api.openai.com", "openai.com"},
	"bun":       {"bun.sh", "registry.npmjs.org"},
}

// Special values
const (
	NetworkAll  = "all"  // Allow all traffic
	NetworkNone = "none" // No network access
)

// Policy represents network access permissions
type Policy struct {
	AllowAll  bool     // Allow all traffic
	Blocked   bool     // No network access
	Domains   []string // Allowed literal domains
	Wildcards []string // Allowed wildcard patterns (*.example.com)
}

// IsWildcard returns true if the domain is a wildcard pattern (*.example.com)
func IsWildcard(domain string) bool {
	return strings.HasPrefix(domain, "*.")
}

// ValidateWildcard validates a wildcard pattern and returns an error if invalid.
// Valid: *.example.com (leading single-level wildcard)
// Invalid: *.com (TLD wildcard), **.example.com (recursive), sub.*.example.com (mid-level)
func ValidateWildcard(pattern string) error {
	if !IsWildcard(pattern) {
		return fmt.Errorf("not a wildcard pattern: %s", pattern)
	}

	// Remove the *. prefix to get the base domain
	baseDomain := strings.TrimPrefix(pattern, "*.")

	// Check for recursive wildcards (**)
	if strings.Contains(pattern, "**") {
		return fmt.Errorf("recursive wildcards not supported: %s", pattern)
	}

	// Check for mid-level wildcards (sub.*.example.com)
	if strings.Contains(baseDomain, "*") {
		return fmt.Errorf("mid-level wildcards not supported: %s", pattern)
	}

	// Check for TLD-only wildcards (*.com, *.org, etc.)
	// Base domain must have at least one dot (e.g., example.com, not just "com")
	if !strings.Contains(baseDomain, ".") {
		return fmt.Errorf("TLD wildcards not allowed: %s", pattern)
	}

	// Validate base domain isn't empty
	if baseDomain == "" {
		return fmt.Errorf("invalid wildcard pattern: %s", pattern)
	}

	return nil
}

// ExtractBaseDomain returns the base domain from a wildcard pattern.
// e.g., *.example.com -> example.com
func ExtractBaseDomain(pattern string) string {
	return strings.TrimPrefix(pattern, "*.")
}

// Parse converts network specs like "npm,pypi,github" into a Policy.
// Examples:
//   - Parse([]string{"npm", "pypi"}) -> Policy{Domains: ["registry.npmjs.org", "npmjs.com", "pypi.org", ...]}
//   - Parse([]string{"all"}) -> Policy{AllowAll: true}
//   - Parse([]string{"none"}) -> Policy{Blocked: true}
//   - Parse([]string{"*.example.com"}) -> Policy{Wildcards: ["*.example.com"]}
func Parse(specs []string) *Policy {
	policy := &Policy{
		Domains:   []string{},
		Wildcards: []string{},
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
				AllowAll:  true,
				Domains:   []string{},
				Wildcards: []string{},
			}
		}
		if spec == NetworkNone {
			return &Policy{
				Blocked:   true,
				Domains:   []string{},
				Wildcards: []string{},
			}
		}
	}

	// Second pass: process presets, wildcards, and domains
	for _, spec := range specs {
		spec = strings.TrimSpace(strings.ToLower(spec))

		// Check if it's a preset
		if presetDomains, ok := Presets[spec]; ok {
			policy.Domains = append(policy.Domains, presetDomains...)
		} else if IsWildcard(spec) {
			// Validate and add wildcard pattern
			if err := ValidateWildcard(spec); err == nil {
				policy.Wildcards = append(policy.Wildcards, spec)
			}
			// Invalid wildcards are silently ignored (or could log a warning)
		} else {
			// Treat as a literal domain
			policy.Domains = append(policy.Domains, spec)
		}
	}

	// Remove duplicates
	policy.Domains = deduplicateDomains(policy.Domains)
	policy.Wildcards = deduplicateDomains(policy.Wildcards)

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

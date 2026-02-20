package guest

import (
	"strings"
	"testing"

	"github.com/faize-ai/faize/internal/network"
	"github.com/faize-ai/faize/internal/session"
)

func TestGenerateClaudeInitScript_DNSForcedWithAllowlist(t *testing.T) {
	// This test verifies that when a domain allowlist is applied,
	// /etc/resolv.conf is forced to use 8.8.8.8/1.1.1.1 BEFORE iptables rules.
	// This prevents the bug where DHCP sets a different DNS server that iptables then blocks.

	tests := []struct {
		name           string
		policy         *network.Policy
		wantDNSForced  bool
		wantIPTables   bool
	}{
		{
			name: "domain allowlist forces DNS",
			policy: &network.Policy{
				Domains: []string{"api.anthropic.com", "github.com"},
			},
			wantDNSForced: true,
			wantIPTables:  true,
		},
		{
			name: "wildcard allowlist forces DNS",
			policy: &network.Policy{
				Wildcards: []string{"*.example.com"},
			},
			wantDNSForced: true,
			wantIPTables:  true,
		},
		{
			name: "mixed domains and wildcards forces DNS",
			policy: &network.Policy{
				Domains:   []string{"api.anthropic.com"},
				Wildcards: []string{"*.internal.company.com"},
			},
			wantDNSForced: true,
			wantIPTables:  true,
		},
		{
			name: "blocked policy does not force DNS",
			policy: &network.Policy{
				Blocked: true,
			},
			wantDNSForced: false,
			wantIPTables:  true, // Still has iptables rules for blocking
		},
		{
			name: "allow all does not force DNS",
			policy: &network.Policy{
				AllowAll: true,
			},
			wantDNSForced: false,
			wantIPTables:  false,
		},
		{
			name:          "nil policy does not force DNS",
			policy:        nil,
			wantDNSForced: false,
			wantIPTables:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := GenerateClaudeInitScript(
				[]session.VMMount{},
				"/workspace",
				tt.policy,
				false,
				nil,
			)

			// Check for DNS forcing in the allowlist section (the specific pattern that fixes the DHCP/iptables mismatch)
			// This must appear BEFORE the iptables rules when allowlist is active
			// We look for the allowlist-specific DNS force, not the generic DHCP fallback
			allowlistDNSMarker := "# Force DNS to use public resolvers (iptables will only allow these)"
			dnsForceIndex := strings.Index(script, allowlistDNSMarker)
			hasDNSForce := dnsForceIndex != -1

			// Check for iptables OUTPUT policy
			iptablesPattern := "iptables -P OUTPUT DROP"
			iptablesIndex := strings.Index(script, iptablesPattern)
			hasIPTables := iptablesIndex != -1

			if hasDNSForce != tt.wantDNSForced {
				t.Errorf("DNS force = %v, want %v", hasDNSForce, tt.wantDNSForced)
			}

			if hasIPTables != tt.wantIPTables {
				t.Errorf("iptables rules = %v, want %v", hasIPTables, tt.wantIPTables)
			}

			// Critical check: DNS forcing must come BEFORE iptables rules
			// This is the actual regression test for the DNS/iptables mismatch bug
			if tt.wantDNSForced && hasDNSForce && hasIPTables {
				if dnsForceIndex > iptablesIndex {
					t.Errorf("DNS forcing appears AFTER iptables rules - this will cause DNS to fail!\n"+
						"DNS force at index %d, iptables at index %d", dnsForceIndex, iptablesIndex)
				}
			}

			// Verify DNS rules allow the forced resolvers
			if tt.wantDNSForced {
				if !strings.Contains(script, "iptables -A OUTPUT -p udp -d 8.8.8.8 --dport 53 -j ACCEPT") {
					t.Error("Missing iptables rule to allow DNS to 8.8.8.8")
				}
				if !strings.Contains(script, "iptables -A OUTPUT -p udp -d 1.1.1.1 --dport 53 -j ACCEPT") {
					t.Error("Missing iptables rule to allow DNS to 1.1.1.1")
				}
			}
		})
	}
}

func TestGenerateClaudeInitScript_WildcardSNIRules(t *testing.T) {
	// Test that wildcard domains generate correct SNI matching rules

	policy := &network.Policy{
		Wildcards: []string{"*.example.com", "*.internal.company.org"},
	}

	script := GenerateClaudeInitScript(
		[]session.VMMount{},
		"/workspace",
		policy,
		false,
		nil,
	)

	// Check for SNI matching rules (iptables string module)
	expectedPatterns := []string{
		// SNI matching for subdomains
		`iptables -A OUTPUT -p tcp --dport 443 -m string --string '.example.com' --algo bm -j ACCEPT`,
		`iptables -A OUTPUT -p tcp --dport 443 -m string --string 'example.com' --algo bm -j ACCEPT`,
		`iptables -A OUTPUT -p tcp --dport 443 -m string --string '.internal.company.org' --algo bm -j ACCEPT`,
		`iptables -A OUTPUT -p tcp --dport 443 -m string --string 'internal.company.org' --algo bm -j ACCEPT`,
		// Base domain IP resolution as fallback
		`nslookup 'example.com'`,
		`nslookup 'internal.company.org'`,
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(script, pattern) {
			t.Errorf("Missing expected pattern: %s", pattern)
		}
	}

	// Verify wildcard comment markers
	if !strings.Contains(script, "# Wildcard: *.example.com") {
		t.Error("Missing wildcard comment marker for *.example.com")
	}
	if !strings.Contains(script, "# Wildcard: *.internal.company.org") {
		t.Error("Missing wildcard comment marker for *.internal.company.org")
	}
}

func TestGenerateClaudeInitScript_MixedDomainsAndWildcards(t *testing.T) {
	// Test that mixed literal domains and wildcards both work correctly

	policy := &network.Policy{
		Domains:   []string{"api.anthropic.com", "github.com"},
		Wildcards: []string{"*.example.com"},
	}

	script := GenerateClaudeInitScript(
		[]session.VMMount{},
		"/workspace",
		policy,
		false,
		nil,
	)

	// Should have literal domain resolution
	if !strings.Contains(script, "ALLOWED_DOMAINS='api.anthropic.com github.com'") {
		t.Error("Missing literal domains in ALLOWED_DOMAINS")
	}

	// Should have wildcard SNI rules
	if !strings.Contains(script, `--string '.example.com'`) {
		t.Error("Missing SNI rule for wildcard *.example.com")
	}

	// Should have wildcard section marker
	if !strings.Contains(script, "# === Wildcard Domains (SNI matching) ===") {
		t.Error("Missing wildcard section marker")
	}
}

func TestGenerateInitScript(t *testing.T) {
	// Basic test for the non-Claude init script
	mounts := []session.VMMount{
		{Source: "/host/path", Target: "/guest/path", ReadOnly: false, Tag: "mount0"},
	}

	script := GenerateInitScript(mounts, "/workspace")

	if !strings.Contains(script, "#!/bin/sh") {
		t.Error("Missing shebang")
	}
	if !strings.Contains(script, "mount -t virtiofs 'mount0' '/guest/path' -o rw") {
		t.Error("Missing mount command")
	}
	if !strings.Contains(script, "cd '/workspace'") {
		t.Error("Missing cd to workspace")
	}
}

func TestGenerateRCLocal(t *testing.T) {
	mounts := []session.VMMount{
		{Source: "/host/path", Target: "/guest/path", ReadOnly: true, Tag: "mount0"},
	}

	script := GenerateRCLocal(mounts)

	if !strings.Contains(script, "#!/bin/sh") {
		t.Error("Missing shebang")
	}
	if !strings.Contains(script, "mount -t virtiofs 'mount0' '/guest/path' -o ro || true") {
		t.Error("Missing mount command with correct options")
	}
	if !strings.Contains(script, "exit 0") {
		t.Error("Missing exit 0")
	}
}

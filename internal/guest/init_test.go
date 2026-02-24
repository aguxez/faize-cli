package guest

import (
	"strings"
	"testing"

	"github.com/faize-ai/faize/internal/network"
	"github.com/faize-ai/faize/internal/session"
)

func TestGenerateClaudeInitScript_DNSForcedWithAllowlist(t *testing.T) {
	// This test verifies that when network restrictions are active,
	// dnsmasq is configured as a local DNS forwarder BEFORE iptables rules.
	// dnsmasq forwards to 8.8.8.8/1.1.1.1 which iptables allows through.

	tests := []struct {
		name          string
		policy        *network.Policy
		wantDNSForced bool
		wantIPTables  bool
	}{
		{
			name: "domain allowlist forces DNS via dnsmasq",
			policy: &network.Policy{
				Domains: []string{"api.anthropic.com", "github.com"},
			},
			wantDNSForced: true,
			wantIPTables:  true,
		},
		{
			name: "wildcard allowlist forces DNS via dnsmasq",
			policy: &network.Policy{
				Wildcards: []string{"*.example.com"},
			},
			wantDNSForced: true,
			wantIPTables:  true,
		},
		{
			name: "mixed domains and wildcards forces DNS via dnsmasq",
			policy: &network.Policy{
				Domains:   []string{"api.anthropic.com"},
				Wildcards: []string{"*.internal.company.com"},
			},
			wantDNSForced: true,
			wantIPTables:  true,
		},
		{
			name: "blocked policy forces DNS via dnsmasq",
			policy: &network.Policy{
				Blocked: true,
			},
			wantDNSForced: true,
			wantIPTables:  true,
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

			// Check for dnsmasq DNS forcing (replaces the old direct resolv.conf forcing)
			dnsmasqMarker := "# Configure dnsmasq as logging DNS forwarder"
			dnsForceIndex := strings.Index(script, dnsmasqMarker)
			hasDNSForce := dnsForceIndex != -1

			// Check for iptables OUTPUT policy
			iptablesPattern := "iptables -P OUTPUT DROP"
			iptablesIndex := strings.Index(script, iptablesPattern)
			hasIPTables := iptablesIndex != -1

			if hasDNSForce != tt.wantDNSForced {
				t.Errorf("DNS force (dnsmasq) = %v, want %v", hasDNSForce, tt.wantDNSForced)
			}

			if hasIPTables != tt.wantIPTables {
				t.Errorf("iptables rules = %v, want %v", hasIPTables, tt.wantIPTables)
			}

			// Critical check: dnsmasq setup must come BEFORE iptables rules
			if tt.wantDNSForced && hasDNSForce && hasIPTables {
				if dnsForceIndex > iptablesIndex {
					t.Errorf("dnsmasq setup appears AFTER iptables rules - DNS queries will be blocked!\n"+
						"dnsmasq at index %d, iptables at index %d", dnsForceIndex, iptablesIndex)
				}
			}

			// Verify iptables allows DNS to upstream resolvers (for domain/wildcard policies)
			if tt.wantDNSForced && hasIPTables && !tt.policy.Blocked {
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

func TestGenerateClaudeInitScript_NetworkLogRules(t *testing.T) {
	tests := []struct {
		name           string
		policy         *network.Policy
		wantNetLog     bool // FAIZE_NET LOG rule
		wantDenyLog    bool // FAIZE_DENY LOG rule
		wantDmesgWatch bool // background dmesg watcher
	}{
		{
			name: "domain allowlist has both LOG rules and watcher",
			policy: &network.Policy{
				Domains: []string{"api.anthropic.com"},
			},
			wantNetLog:     true,
			wantDenyLog:    true,
			wantDmesgWatch: true,
		},
		{
			name: "blocked policy has deny LOG and watcher",
			policy: &network.Policy{
				Blocked: true,
			},
			wantNetLog:     false, // blocked doesn't need NEW connection logging
			wantDenyLog:    true,
			wantDmesgWatch: true,
		},
		{
			name: "allow all has no LOG rules and no watcher",
			policy: &network.Policy{
				AllowAll: true,
			},
			wantNetLog:     false,
			wantDenyLog:    false,
			wantDmesgWatch: false,
		},
		{
			name:           "nil policy has no LOG rules and no watcher",
			policy:         nil,
			wantNetLog:     false,
			wantDenyLog:    false,
			wantDmesgWatch: false,
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

			hasNetLog := strings.Contains(script, "FAIZE_NET: ")
			hasDenyLog := strings.Contains(script, "FAIZE_DENY: ")
			hasDmesgWatch := strings.Contains(script, "dmesg -c") && strings.Contains(script, "NETLOG_PID")

			if hasNetLog != tt.wantNetLog {
				t.Errorf("FAIZE_NET LOG rule = %v, want %v", hasNetLog, tt.wantNetLog)
			}
			if hasDenyLog != tt.wantDenyLog {
				t.Errorf("FAIZE_DENY LOG rule = %v, want %v", hasDenyLog, tt.wantDenyLog)
			}
			if hasDmesgWatch != tt.wantDmesgWatch {
				t.Errorf("dmesg watcher = %v, want %v", hasDmesgWatch, tt.wantDmesgWatch)
			}
		})
	}
}

func TestGenerateClaudeInitScript_DnsmasqSetup(t *testing.T) {
	tests := []struct {
		name          string
		policy        *network.Policy
		wantDnsmasq   bool
		wantLocalhost bool // resolv.conf points to 127.0.0.1
	}{
		{
			name: "domain allowlist configures dnsmasq",
			policy: &network.Policy{
				Domains: []string{"api.anthropic.com", "github.com"},
			},
			wantDnsmasq:   true,
			wantLocalhost: true,
		},
		{
			name: "wildcard allowlist configures dnsmasq",
			policy: &network.Policy{
				Wildcards: []string{"*.example.com"},
			},
			wantDnsmasq:   true,
			wantLocalhost: true,
		},
		{
			name: "blocked policy configures dnsmasq",
			policy: &network.Policy{
				Blocked: true,
			},
			wantDnsmasq:   true,
			wantLocalhost: true,
		},
		{
			name: "allow all does NOT configure dnsmasq",
			policy: &network.Policy{
				AllowAll: true,
			},
			wantDnsmasq:   false,
			wantLocalhost: false,
		},
		{
			name:          "nil policy does NOT configure dnsmasq",
			policy:        nil,
			wantDnsmasq:   false,
			wantLocalhost: false,
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

			hasDnsmasqConfig := strings.Contains(script, "cat > /etc/dnsmasq.conf")
			hasDnsmasqStart := strings.Contains(script, "dnsmasq\n")
			hasLocalhost := strings.Contains(script, "echo 'nameserver 127.0.0.1' > /etc/resolv.conf")
			hasDnsmasqKill := strings.Contains(script, "DNSMASQ_RUNNING=1")
			hasLogQueries := strings.Contains(script, "log-queries")
			hasDNSLogFacility := strings.Contains(script, "log-facility=/mnt/bootstrap/dns.log")

			if hasDnsmasqConfig != tt.wantDnsmasq {
				t.Errorf("dnsmasq config = %v, want %v", hasDnsmasqConfig, tt.wantDnsmasq)
			}
			if hasDnsmasqStart != tt.wantDnsmasq {
				t.Errorf("dnsmasq start = %v, want %v", hasDnsmasqStart, tt.wantDnsmasq)
			}
			if hasLocalhost != tt.wantLocalhost {
				t.Errorf("resolv.conf localhost = %v, want %v", hasLocalhost, tt.wantLocalhost)
			}

			// Cleanup handler should have conditional dnsmasq kill only when dnsmasq is configured
			if hasDnsmasqKill != tt.wantDnsmasq {
				t.Errorf("dnsmasq kill in cleanup = %v, want %v", hasDnsmasqKill, tt.wantDnsmasq)
			}

			if tt.wantDnsmasq {
				if !hasLogQueries {
					t.Error("Missing log-queries in dnsmasq config")
				}
				if !hasDNSLogFacility {
					t.Error("Missing log-facility in dnsmasq config")
				}
			}
		})
	}
}

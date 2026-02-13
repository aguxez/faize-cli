package guest

import (
	"fmt"
	"strings"

	"github.com/faize-ai/faize/internal/network"
	"github.com/faize-ai/faize/internal/session"
)

// GenerateInitScript generates the bootstrap init script executed by the rootfs /init.
// This script is written to /mnt/bootstrap/init.sh and called after the rootfs /init
// has already mounted proc/sys/dev and the faize-bootstrap VirtioFS share.
func GenerateInitScript(mounts []session.VMMount, workDir string) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Faize bootstrap init script\n")
	sb.WriteString("# Called by rootfs /init after mounting faize-bootstrap VirtioFS share\n")
	sb.WriteString("set -e\n\n")

	// Mount VirtioFS shares (proc/sys/dev already mounted by rootfs /init)
	sb.WriteString("# Mount VirtioFS shares\n")
	for i, mount := range mounts {
		tag := mount.Tag
		if tag == "" {
			tag = fmt.Sprintf("mount%d", i)
		}

		// Create mount point
		sb.WriteString(fmt.Sprintf("mkdir -p %s\n", mount.Target))

		// Mount options
		opts := "rw"
		if mount.ReadOnly {
			opts = "ro"
		}

		sb.WriteString(fmt.Sprintf("mount -t virtiofs %s %s -o %s\n", tag, mount.Target, opts))
	}

	sb.WriteString("\n")

	// Set system time from host
	sb.WriteString("# Set system time from host\n")
	sb.WriteString("if [ -f /mnt/bootstrap/hosttime ]; then\n")
	sb.WriteString("  HOSTTIME=$(cat /mnt/bootstrap/hosttime)\n")
	sb.WriteString("  date -s \"@$HOSTTIME\" >/dev/null 2>&1 && echo \"Clock synced from host\" || echo \"Clock sync failed\"\n")
	sb.WriteString("fi\n\n")

	// Change to working directory
	if workDir != "" {
		sb.WriteString(fmt.Sprintf("# Change to project directory\n"))
		sb.WriteString(fmt.Sprintf("cd %s\n\n", workDir))
	}

	// Start shell
	sb.WriteString("# Start interactive shell\n")
	sb.WriteString("exec setsid /bin/sh </dev/console >/dev/console 2>&1\n")

	return sb.String()
}

// GenerateRCLocal generates /etc/rc.local content for Alpine
func GenerateRCLocal(mounts []session.VMMount) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Faize rc.local - mount VirtioFS shares at boot\n\n")

	for i, mount := range mounts {
		tag := mount.Tag
		if tag == "" {
			tag = fmt.Sprintf("mount%d", i)
		}

		sb.WriteString(fmt.Sprintf("mkdir -p %s\n", mount.Target))

		opts := "rw"
		if mount.ReadOnly {
			opts = "ro"
		}

		sb.WriteString(fmt.Sprintf("mount -t virtiofs %s %s -o %s || true\n", tag, mount.Target, opts))
	}

	sb.WriteString("\nexit 0\n")

	return sb.String()
}

// GenerateClaudeInitScript generates the bootstrap init script for Claude mode.
// This script mounts VirtioFS shares, sets up Claude configuration, and launches Claude Code CLI.
// Bun and Claude are pre-installed in the rootfs at /usr/local/bin.
func GenerateClaudeInitScript(mounts []session.VMMount, projectDir string, policy *network.Policy) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Faize Claude mode init script (non-root)\n")
	sb.WriteString("set -e\n\n")

	// Add signal handler for graceful shutdown
	sb.WriteString("# Signal handler for graceful shutdown\n")
	sb.WriteString("cleanup() {\n")
	sb.WriteString("  echo 'Shutting down...'\n")
	sb.WriteString("  # Kill child processes gracefully\n")
	sb.WriteString("  kill -TERM $(jobs -p) 2>/dev/null || true\n")
	sb.WriteString("  wait\n")
	sb.WriteString("  # Sync filesystems\n")
	sb.WriteString("  sync\n")
	sb.WriteString("  # Power off\n")
	sb.WriteString("  poweroff -f\n")
	sb.WriteString("}\n\n")
	sb.WriteString("trap cleanup TERM INT\n\n")

	// Mount VirtioFS shares
	sb.WriteString("# Mount VirtioFS shares\n")
	for i, mount := range mounts {
		tag := mount.Tag
		if tag == "" {
			tag = fmt.Sprintf("mount%d", i)
		}
		sb.WriteString(fmt.Sprintf("mkdir -p %s\n", mount.Target))
		opts := "rw"
		if mount.ReadOnly {
			opts = "ro"
		}
		sb.WriteString(fmt.Sprintf("mount -t virtiofs %s %s -o %s\n", tag, mount.Target, opts))
	}
	sb.WriteString("\n")

	// Mount devpts for PTY support (required by script command)
	sb.WriteString("# Mount devpts for PTY support\n")
	sb.WriteString("mkdir -p /dev/pts\n")
	sb.WriteString("mount -t devpts devpts /dev/pts -o gid=5,mode=620\n\n")

	// Set system time from host
	sb.WriteString("# Set system time from host\n")
	sb.WriteString("if [ -f /mnt/bootstrap/hosttime ]; then\n")
	sb.WriteString("  HOSTTIME=$(cat /mnt/bootstrap/hosttime)\n")
	sb.WriteString("  date -s \"@$HOSTTIME\" >/dev/null 2>&1 && echo \"Clock synced from host\" || echo \"Clock sync failed\"\n")
	sb.WriteString("fi\n\n")

	// Set terminal size from host (makes URLs clickable by preventing line wrapping)
	sb.WriteString("# Set terminal size from host\n")
	sb.WriteString("if [ -f /mnt/bootstrap/termsize ]; then\n")
	sb.WriteString("  TERMSIZE=$(cat /mnt/bootstrap/termsize 2>/dev/null) || true\n")
	sb.WriteString("  COLS=$(echo $TERMSIZE | cut -d' ' -f1)\n")
	sb.WriteString("  ROWS=$(echo $TERMSIZE | cut -d' ' -f2)\n")
	sb.WriteString("  [ -n \"$COLS\" ] && [ -n \"$ROWS\" ] && stty cols $COLS rows $ROWS 2>/dev/null && echo \"Terminal size: ${COLS}x${ROWS}\" || true\n")
	sb.WriteString("fi\n\n")

	// Configure network via DHCP
	sb.WriteString("# Configure network\n")
	sb.WriteString("echo 'Setting up network...'\n")

	// Bring up loopback
	sb.WriteString("ifconfig lo 127.0.0.1 up\n")

	// Find and bring up the network interface
	sb.WriteString("IFACE=$(ls /sys/class/net | grep -v lo | head -1)\n")
	sb.WriteString("if [ -n \"$IFACE\" ]; then\n")
	sb.WriteString("  echo \"Found interface: $IFACE\"\n")
	sb.WriteString("  ifconfig $IFACE up\n")
	sb.WriteString("  \n")
	sb.WriteString("  # Run DHCP client (busybox udhcpc)\n")
	sb.WriteString("  echo 'Running DHCP...'\n")
	sb.WriteString("  udhcpc -i $IFACE -n -q -t 10 2>/dev/null && echo 'DHCP successful' || echo 'DHCP failed'\n")
	sb.WriteString("  \n")
	sb.WriteString("  # Show assigned IP\n")
	sb.WriteString("  ifconfig $IFACE | grep 'inet addr' || ifconfig $IFACE | grep 'inet '\n")
	sb.WriteString("fi\n\n")

	// Ensure DNS is configured (DHCP may or may not set this)
	sb.WriteString("# Ensure DNS configuration\n")
	sb.WriteString("grep -q nameserver /etc/resolv.conf 2>/dev/null || echo 'nameserver 8.8.8.8' > /etc/resolv.conf\n")
	sb.WriteString("grep -q '8.8.8.8\\|1.1.1.1' /etc/resolv.conf || {\n")
	sb.WriteString("  echo 'nameserver 8.8.8.8' > /etc/resolv.conf\n")
	sb.WriteString("  echo 'nameserver 1.1.1.1' >> /etc/resolv.conf\n")
	sb.WriteString("}\n\n")

	// Test connectivity
	sb.WriteString("# Test network connectivity\n")
	sb.WriteString("echo 'Testing connectivity...'\n")
	sb.WriteString("wget -q --spider http://api.anthropic.com 2>/dev/null && echo 'Network OK' || echo 'Network check failed (may still work)'\n\n")

	// Apply network policy if specified
	if policy != nil && !policy.AllowAll {
		if policy.Blocked {
			// Block all outbound traffic (IPv4 only - IPv6 disabled in kernel)
			sb.WriteString("# === Network Policy: BLOCKED ===\n")
			sb.WriteString("echo 'Applying network policy: blocked'\n")
			sb.WriteString("iptables -P OUTPUT DROP\n")
			sb.WriteString("iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT\n")
			sb.WriteString("iptables -A OUTPUT -o lo -j ACCEPT\n")
			sb.WriteString("echo 'Network blocked (loopback only)'\n\n")
		} else if len(policy.Domains) > 0 {
			// Domain-based allowlist
			sb.WriteString("# === Network Policy: Domain Allowlist ===\n")
			sb.WriteString("echo 'Applying network policy: domain allowlist'\n\n")
			sb.WriteString("# Default: drop all outbound except established connections\n")
			sb.WriteString("iptables -P OUTPUT DROP\n")
			sb.WriteString("iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT\n")
			sb.WriteString("iptables -A OUTPUT -o lo -j ACCEPT\n\n")
			sb.WriteString("# Allow DNS queries (required for resolution)\n")
			sb.WriteString("iptables -A OUTPUT -p udp --dport 53 -j ACCEPT\n")
			sb.WriteString("iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT\n\n")
			sb.WriteString("# Resolve and allow specific domains\n")
			domainsStr := strings.Join(policy.Domains, " ")
			sb.WriteString(fmt.Sprintf("ALLOWED_DOMAINS=\"%s\"\n", domainsStr))
			sb.WriteString("\n")
			sb.WriteString("# Brief wait for DNS to stabilize after DHCP\n")
			sb.WriteString("sleep 1\n\n")
			sb.WriteString("FAIZE_DEBUG=0\n")
			sb.WriteString("[ -f /mnt/bootstrap/debug ] && FAIZE_DEBUG=1\n\n")
			sb.WriteString("for domain in $ALLOWED_DOMAINS; do\n")
			sb.WriteString("  [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Resolving $domain...\"\n")
			sb.WriteString("  # Use temp file to avoid subshell issues with pipe\n")
			sb.WriteString("  nslookup \"$domain\" 2>/dev/null | awk 'NR>2 && /^Address:/ {print $2}' > /tmp/ips_$$ || true\n")
			sb.WriteString("  while read ip; do\n")
			sb.WriteString("    # Skip IPv6 addresses (kernel has IPv6 disabled)\n")
			sb.WriteString("    if [ -n \"$ip\" ] && ! echo \"$ip\" | grep -q ':'; then\n")
			sb.WriteString("      [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"  Allowing $ip ($domain)\"\n")
			sb.WriteString("      iptables -A OUTPUT -d \"$ip\" -j ACCEPT 2>/dev/null || echo \"  Failed to add rule for $ip\"\n")
			sb.WriteString("    fi\n")
			sb.WriteString("  done < /tmp/ips_$$\n")
			sb.WriteString("  rm -f /tmp/ips_$$\n")
			sb.WriteString("done\n\n")
			sb.WriteString("# Show applied rules (debug only)\n")
			sb.WriteString("if [ \"$FAIZE_DEBUG\" = \"1\" ]; then\n")
			sb.WriteString("  echo '=== iptables OUTPUT rules ==='\n")
			sb.WriteString("  iptables -L OUTPUT -n 2>/dev/null | head -20 || echo 'Failed to list iptables rules'\n")
			sb.WriteString("fi\n\n")
			sb.WriteString("echo 'Network policy applied'\n\n")
		}
	}

	// Fix ownership for writable directories
	sb.WriteString("# Fix ownership for claude user\n")
	sb.WriteString("chown -R claude:claude /home/claude 2>/dev/null || true\n")
	sb.WriteString("chown -R claude:claude /opt/toolchain 2>/dev/null || true\n")
	if projectDir != "" {
		sb.WriteString(fmt.Sprintf("chown -R claude:claude %s 2>/dev/null || true\n", projectDir))
	}
	sb.WriteString("\n")

	// Create Claude config directory
	sb.WriteString("# Create Claude configuration directory\n")
	sb.WriteString("mkdir -p /home/claude/.claude\n")
	sb.WriteString("chown claude:claude /home/claude/.claude\n\n")

	// Symlink read-only configuration files
	sb.WriteString("# Symlink read-only Claude configuration files\n")
	readOnlyFiles := []string{"CLAUDE.md", "keybindings.json"}
	for _, file := range readOnlyFiles {
		sb.WriteString(fmt.Sprintf("if [ -e /mnt/host-claude/%s ]; then\n", file))
		sb.WriteString(fmt.Sprintf("  ln -sf /mnt/host-claude/%s /home/claude/.claude/%s\n", file, file))
		sb.WriteString("fi\n")
	}
	sb.WriteString("\n")

	// Copy settings.json (Claude may need to modify it) - only if not already present
	sb.WriteString("# Copy settings.json (may need modifications) - only if not already present\n")
	sb.WriteString("if [ -f /mnt/host-claude/settings.json ] && [ ! -e /home/claude/.claude/settings.json ]; then\n")
	sb.WriteString("  cp /mnt/host-claude/settings.json /home/claude/.claude/settings.json\n")
	sb.WriteString("  chown claude:claude /home/claude/.claude/settings.json\n")
	sb.WriteString("fi\n\n")

	// Create writable directories and copy contents from host
	sb.WriteString("# Create writable directories with host content\n")
	writableDirs := []string{"skills", "plugins"}
	for _, dir := range writableDirs {
		sb.WriteString(fmt.Sprintf("mkdir -p /home/claude/.claude/%s\n", dir))
		sb.WriteString(fmt.Sprintf("if [ -d /mnt/host-claude/%s ]; then\n", dir))
		sb.WriteString(fmt.Sprintf("  cp -r /mnt/host-claude/%s/. /home/claude/.claude/%s/ 2>/dev/null || true\n", dir, dir))
		sb.WriteString("fi\n")
		sb.WriteString(fmt.Sprintf("chown -R claude:claude /home/claude/.claude/%s\n", dir))
	}
	sb.WriteString("\n")

	// Change to project directory
	if projectDir != "" {
		sb.WriteString(fmt.Sprintf("cd %s\n\n", projectDir))
	} else {
		sb.WriteString("cd /workspace\n\n")
	}

	// Launch Claude CLI as non-root user with PTY allocation via script command
	// The script command allocates a PTY which Claude/Ink requires for raw mode
	sb.WriteString("# Launch Claude CLI as non-root user with PTY allocation via script command\n")
	sb.WriteString("# The script command allocates a PTY which Claude/Ink requires for raw mode\n")
	sb.WriteString("# Disable exit-on-error for the script command to prevent kernel panic if it fails\n")
	sb.WriteString("set +e\n")
	sb.WriteString("script -q -c \"su -s /bin/sh claude -c 'export HOME=/home/claude && export PATH=/usr/local/bin:/usr/bin:/bin && cd \\${PWD} && exec claude'\" /dev/null\n")
	sb.WriteString("CLAUDE_EXIT=$?\n")
	sb.WriteString("set -e\n\n")
	sb.WriteString("echo \"Claude exited with code: $CLAUDE_EXIT\"\n\n")
	sb.WriteString("# Shutdown gracefully\n")
	sb.WriteString("cleanup\n")

	return sb.String()
}

// DefaultShellRC returns default shell RC content
func DefaultShellRC(workDir string) string {
	var sb strings.Builder

	sb.WriteString("# Faize shell configuration\n")
	sb.WriteString("export PS1='faize:\\w\\$ '\n")
	sb.WriteString("export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin\n")

	if workDir != "" {
		sb.WriteString(fmt.Sprintf("cd %s 2>/dev/null || true\n", workDir))
	}

	return sb.String()
}

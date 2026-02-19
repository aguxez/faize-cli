package guest

import (
	"fmt"
	"strings"

	"github.com/faize-ai/faize/internal/network"
	"github.com/faize-ai/faize/internal/session"
)

// shellQuote wraps a string in single quotes with proper escaping for shell interpolation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

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
		fmt.Fprintf(&sb, "mkdir -p %s\n", shellQuote(mount.Target))

		// Mount options
		opts := "rw"
		if mount.ReadOnly {
			opts = "ro"
		}

		fmt.Fprintf(&sb, "mount -t virtiofs %s %s -o %s\n", shellQuote(tag), shellQuote(mount.Target), opts)
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
		sb.WriteString("# Change to project directory\n")
		fmt.Fprintf(&sb, "cd %s\n\n", shellQuote(workDir))
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

		fmt.Fprintf(&sb, "mkdir -p %s\n", shellQuote(mount.Target))

		opts := "rw"
		if mount.ReadOnly {
			opts = "ro"
		}

		fmt.Fprintf(&sb, "mount -t virtiofs %s %s -o %s || true\n", shellQuote(tag), shellQuote(mount.Target), opts)
	}

	sb.WriteString("\nexit 0\n")

	return sb.String()
}

// GenerateClaudeInitScript generates the bootstrap init script for Claude mode.
// This script mounts VirtioFS shares, sets up Claude configuration, and launches Claude Code CLI.
// Bun and Claude are pre-installed in the rootfs at /usr/local/bin.
func GenerateClaudeInitScript(mounts []session.VMMount, projectDir string, policy *network.Policy, persistCredentials bool, extraDeps []string) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Faize Claude mode init script (non-root)\n")
	sb.WriteString("set -e\n\n")

	// Debug mode detection
	sb.WriteString("# Debug mode detection\n")
	sb.WriteString("FAIZE_DEBUG=0\n")
	sb.WriteString("[ -f /mnt/bootstrap/debug ] && FAIZE_DEBUG=1\n\n")

	// Add signal handler for graceful shutdown
	sb.WriteString("# Signal handler for graceful shutdown\n")
	sb.WriteString("cleanup() {\n")
	sb.WriteString("  echo 'Shutting down...'\n")
	sb.WriteString("  # Kill resize watcher if running\n")
	sb.WriteString("  [ -n \"$RESIZE_WATCHER_PID\" ] && kill $RESIZE_WATCHER_PID 2>/dev/null || true\n")
	sb.WriteString("  # Kill child processes gracefully\n")
	sb.WriteString("  kill -TERM $(jobs -p) 2>/dev/null || true\n")
	sb.WriteString("  wait\n")

	if persistCredentials {
		sb.WriteString("  # Persist credential files to host\n")
		sb.WriteString("  if [ -d /mnt/host-credentials ]; then\n")
		sb.WriteString("    if [ -s /home/claude/.claude/.credentials.json ]; then\n")
		sb.WriteString("      cp /home/claude/.claude/.credentials.json /mnt/host-credentials/.credentials.json\n")
		sb.WriteString("      [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Persisted .credentials.json to host\"\n")
		sb.WriteString("    fi\n")
		sb.WriteString("    if [ -s /home/claude/.claude.json ]; then\n")
		sb.WriteString("      cp /home/claude/.claude.json /mnt/host-credentials/claude.json\n")
		sb.WriteString("      [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Persisted .claude.json to host\"\n")
		sb.WriteString("    fi\n")
		sb.WriteString("    sync\n")
		sb.WriteString("  fi\n")
	}

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
		fmt.Fprintf(&sb, "mkdir -p %s\n", shellQuote(mount.Target))
		opts := "rw"
		if mount.ReadOnly {
			opts = "ro"
		}
		fmt.Fprintf(&sb, "mount -t virtiofs %s %s -o %s\n", shellQuote(tag), shellQuote(mount.Target), opts)
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
	sb.WriteString("  if date -s \"@$HOSTTIME\" >/dev/null 2>&1; then\n")
	sb.WriteString("    [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Clock synced from host\"\n")
	sb.WriteString("  else\n")
	sb.WriteString("    echo \"Clock sync failed\"\n")
	sb.WriteString("  fi\n")
	sb.WriteString("fi\n\n")

	// Set terminal size from host (makes URLs clickable by preventing line wrapping)
	sb.WriteString("# Set terminal size from host\n")
	sb.WriteString("if [ -f /mnt/bootstrap/termsize ]; then\n")
	sb.WriteString("  TERMSIZE=$(cat /mnt/bootstrap/termsize 2>/dev/null) || true\n")
	sb.WriteString("  COLS=$(echo $TERMSIZE | cut -d' ' -f1)\n")
	sb.WriteString("  ROWS=$(echo $TERMSIZE | cut -d' ' -f2)\n")
	sb.WriteString("  if [ -n \"$COLS\" ] && [ -n \"$ROWS\" ]; then\n")
	sb.WriteString("    stty cols $COLS rows $ROWS 2>/dev/null || true\n")
	sb.WriteString("    [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Terminal size: ${COLS}x${ROWS}\"\n")
	sb.WriteString("  fi\n")
	sb.WriteString("fi\n\n")

	// Configure network via DHCP
	sb.WriteString("# Configure network\n")
	sb.WriteString("[ \"$FAIZE_DEBUG\" = \"1\" ] && echo 'Setting up network...'\n")

	// Bring up loopback
	sb.WriteString("ifconfig lo 127.0.0.1 up\n")

	// Find and bring up the network interface
	sb.WriteString("IFACE=$(ls /sys/class/net | grep -v lo | head -1)\n")
	sb.WriteString("if [ -n \"$IFACE\" ]; then\n")
	sb.WriteString("  [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Found interface: $IFACE\"\n")
	sb.WriteString("  ifconfig $IFACE up\n")
	sb.WriteString("  \n")
	sb.WriteString("  # Run DHCP client (busybox udhcpc)\n")
	sb.WriteString("  [ \"$FAIZE_DEBUG\" = \"1\" ] && echo 'Running DHCP...'\n")
	sb.WriteString("  if udhcpc -i $IFACE -n -q -t 10 2>/dev/null; then\n")
	sb.WriteString("    [ \"$FAIZE_DEBUG\" = \"1\" ] && echo 'DHCP successful'\n")
	sb.WriteString("  else\n")
	sb.WriteString("    echo 'DHCP failed'\n")
	sb.WriteString("  fi\n")
	sb.WriteString("  \n")
	sb.WriteString("  # Show assigned IP\n")
	sb.WriteString("  if [ \"$FAIZE_DEBUG\" = \"1\" ]; then\n")
	sb.WriteString("    ifconfig $IFACE | grep 'inet addr' || ifconfig $IFACE | grep 'inet ' || true\n")
	sb.WriteString("  fi\n")
	sb.WriteString("fi\n\n")

	// Ensure DNS is configured (only inject public DNS if DHCP didn't provide any)
	sb.WriteString("# Ensure DNS configuration (only inject public DNS if DHCP didn't provide any)\n")
	sb.WriteString("if ! grep -q nameserver /etc/resolv.conf 2>/dev/null; then\n")
	sb.WriteString("  echo 'nameserver 8.8.8.8' > /etc/resolv.conf\n")
	sb.WriteString("  echo 'nameserver 1.1.1.1' >> /etc/resolv.conf\n")
	sb.WriteString("fi\n\n")

	// Test connectivity (with DNS stabilization delay and retries)
	sb.WriteString("# Brief wait for network/DNS to stabilize after DHCP\n")
	sb.WriteString("sleep 2\n\n")
	sb.WriteString("# Test network connectivity (with retries)\n")
	sb.WriteString("[ \"$FAIZE_DEBUG\" = \"1\" ] && echo 'Testing connectivity...'\n")
	sb.WriteString("if wget -q --spider --timeout=3 https://api.anthropic.com 2>/dev/null || \\\n")
	sb.WriteString("   { sleep 1 && wget -q --spider --timeout=3 https://api.anthropic.com 2>/dev/null; } || \\\n")
	sb.WriteString("   { sleep 2 && wget -q --spider --timeout=3 https://api.anthropic.com 2>/dev/null; }; then\n")
	sb.WriteString("  [ \"$FAIZE_DEBUG\" = \"1\" ] && echo 'Network OK'\n")
	sb.WriteString("else\n")
	sb.WriteString("  echo 'Network check failed (may still work)'\n")
	sb.WriteString("fi\n\n")

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
			sb.WriteString("[ \"$FAIZE_DEBUG\" = \"1\" ] && echo 'Applying network policy: domain allowlist'\n\n")
			sb.WriteString("# Default: drop all outbound except established connections\n")
			sb.WriteString("iptables -P OUTPUT DROP\n")
			sb.WriteString("iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT\n")
			sb.WriteString("iptables -A OUTPUT -o lo -j ACCEPT\n\n")
			sb.WriteString("# Allow DNS queries only to known resolvers\n")
			sb.WriteString("iptables -A OUTPUT -p udp -d 8.8.8.8 --dport 53 -j ACCEPT\n")
			sb.WriteString("iptables -A OUTPUT -p udp -d 1.1.1.1 --dport 53 -j ACCEPT\n")
			sb.WriteString("iptables -A OUTPUT -p tcp -d 8.8.8.8 --dport 53 -j ACCEPT\n")
			sb.WriteString("iptables -A OUTPUT -p tcp -d 1.1.1.1 --dport 53 -j ACCEPT\n\n")
			sb.WriteString("# Resolve and allow specific domains\n")
			domainsStr := strings.Join(policy.Domains, " ")
			sb.WriteString(fmt.Sprintf("ALLOWED_DOMAINS=%s\n", shellQuote(domainsStr)))
			sb.WriteString("\n")
			sb.WriteString("# FAIZE_DEBUG already set at top of script\n")
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
			sb.WriteString("[ \"$FAIZE_DEBUG\" = \"1\" ] && echo 'Network policy applied'\n\n")
		}
	}

	// Fix ownership for writable directories
	sb.WriteString("# Fix ownership for claude user\n")
	sb.WriteString("chown -R claude:claude /home/claude 2>/dev/null || true\n")
	sb.WriteString("chown -R claude:claude /opt/toolchain 2>/dev/null || true\n")
	if projectDir != "" {
		sb.WriteString(fmt.Sprintf("chown -R claude:claude %s 2>/dev/null || true\n", shellQuote(projectDir)))
	}
	sb.WriteString("\n")

	// Mark project directory as safe for git (VirtioFS mounts have different ownership)
	safeDir := projectDir
	if safeDir == "" {
		safeDir = "/workspace"
	}
	sb.WriteString(fmt.Sprintf("git config --system --add safe.directory %s\n\n", shellQuote(safeDir)))

	// Install clipboard bridge shims (xclip/xsel)
	// These scripts read clipboard data from VirtioFS, synced by the host on Ctrl+V
	sb.WriteString("# Install clipboard bridge shims\n")

	// xclip shim
	sb.WriteString("cat > /usr/local/bin/xclip << 'XCLIP_EOF'\n")
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("CLIP_DIR=\"/mnt/bootstrap/clipboard\"\n")
	sb.WriteString("# Parse arguments\n")
	sb.WriteString("OUTPUT_MODE=0\n")
	sb.WriteString("TARGET_TYPE=\"\"\n")
	sb.WriteString("SELECTION=\"\"\n")
	sb.WriteString("while [ $# -gt 0 ]; do\n")
	sb.WriteString("  case \"$1\" in\n")
	sb.WriteString("    -o|-out) OUTPUT_MODE=1 ;;\n")
	sb.WriteString("    -t) shift; TARGET_TYPE=\"$1\" ;;\n")
	sb.WriteString("    -selection) shift; SELECTION=\"$1\" ;;\n")
	sb.WriteString("    -i|-in) ;;\n")
	sb.WriteString("    *) ;;\n")
	sb.WriteString("  esac\n")
	sb.WriteString("  shift\n")
	sb.WriteString("done\n")
	sb.WriteString("if [ \"$OUTPUT_MODE\" = \"1\" ]; then\n")
	sb.WriteString("  # Output mode: read from VirtioFS\n")
	sb.WriteString("  if [ \"$TARGET_TYPE\" = \"TARGETS\" ]; then\n")
	sb.WriteString("    # Report available clipboard types\n")
	sb.WriteString("    [ -f \"$CLIP_DIR/clipboard-image\" ] && printf 'image/png\\n'\n")
	sb.WriteString("    [ -f \"$CLIP_DIR/clipboard-text\" ] && printf 'UTF8_STRING\\ntext/plain\\n'\n")
	sb.WriteString("  elif [ \"$TARGET_TYPE\" = \"image/png\" ] && [ -f \"$CLIP_DIR/clipboard-image\" ]; then\n")
	sb.WriteString("    cat \"$CLIP_DIR/clipboard-image\"\n")
	sb.WriteString("  elif [ -f \"$CLIP_DIR/clipboard-text\" ]; then\n")
	sb.WriteString("    cat \"$CLIP_DIR/clipboard-text\"\n")
	sb.WriteString("  fi\n")
	sb.WriteString("else\n")
	sb.WriteString("  # Input mode: write stdin to VirtioFS\n")
	sb.WriteString("  mkdir -p \"$CLIP_DIR\"\n")
	sb.WriteString("  cat > \"$CLIP_DIR/clipboard-text\"\n")
	sb.WriteString("fi\n")
	sb.WriteString("XCLIP_EOF\n")
	sb.WriteString("chmod +x /usr/local/bin/xclip\n\n")

	// xsel shim
	sb.WriteString("cat > /usr/local/bin/xsel << 'XSEL_EOF'\n")
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("CLIP_DIR=\"/mnt/bootstrap/clipboard\"\n")
	sb.WriteString("# Parse arguments\n")
	sb.WriteString("OUTPUT_MODE=0\n")
	sb.WriteString("while [ $# -gt 0 ]; do\n")
	sb.WriteString("  case \"$1\" in\n")
	sb.WriteString("    -o|--output) OUTPUT_MODE=1 ;;\n")
	sb.WriteString("    -i|--input) ;;\n")
	sb.WriteString("    -b|--clipboard) ;;\n")
	sb.WriteString("    *) ;;\n")
	sb.WriteString("  esac\n")
	sb.WriteString("  shift\n")
	sb.WriteString("done\n")
	sb.WriteString("if [ \"$OUTPUT_MODE\" = \"1\" ]; then\n")
	sb.WriteString("  if [ -f \"$CLIP_DIR/clipboard-text\" ]; then\n")
	sb.WriteString("    cat \"$CLIP_DIR/clipboard-text\"\n")
	sb.WriteString("  fi\n")
	sb.WriteString("else\n")
	sb.WriteString("  mkdir -p \"$CLIP_DIR\"\n")
	sb.WriteString("  cat > \"$CLIP_DIR/clipboard-text\"\n")
	sb.WriteString("fi\n")
	sb.WriteString("XSEL_EOF\n")
	sb.WriteString("chmod +x /usr/local/bin/xsel\n\n")

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

	if persistCredentials {
		sb.WriteString("# Mount credentials VirtioFS share\n")
		sb.WriteString("mkdir -p /mnt/host-credentials\n")
		sb.WriteString("mount -t virtiofs credentials /mnt/host-credentials -o rw\n\n")

		sb.WriteString("# Copy persisted credentials from host (if they exist and have content)\n")
		sb.WriteString("if [ -d /mnt/host-credentials ]; then\n")
		sb.WriteString("  if [ -s /mnt/host-credentials/.credentials.json ]; then\n")
		sb.WriteString("    cp /mnt/host-credentials/.credentials.json /home/claude/.claude/.credentials.json\n")
		sb.WriteString("    chown claude:claude /home/claude/.claude/.credentials.json\n")
		sb.WriteString("    [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Restored .credentials.json from host\"\n")
		sb.WriteString("  fi\n")
		sb.WriteString("  if [ -s /mnt/host-credentials/claude.json ]; then\n")
		sb.WriteString("    cp /mnt/host-credentials/claude.json /home/claude/.claude.json\n")
		sb.WriteString("    chown claude:claude /home/claude/.claude.json\n")
		sb.WriteString("    [ \"$FAIZE_DEBUG\" = \"1\" ] && echo \"Restored .claude.json from host\"\n")
		sb.WriteString("  fi\n")
		sb.WriteString("fi\n\n")
	}

	// Rewrite hardcoded host paths in plugin config files
	// Plugins store absolute paths like /Users/<user>/.claude/... which don't exist in VM
	sb.WriteString("# Rewrite host paths in plugin configs to VM paths\n")

	sb.WriteString("for jsonfile in /home/claude/.claude/plugins/*.json; do\n")
	sb.WriteString("  [ -f \"$jsonfile\" ] || continue\n")
	sb.WriteString("  # Replace macOS home paths: /Users/<user>/.claude/ -> /home/claude/.claude/\n")
	sb.WriteString("  sed -i 's|/Users/[^/]*/.claude/|/home/claude/.claude/|g' \"$jsonfile\"\n")
	sb.WriteString("  # Replace Linux home paths: /home/<user>/.claude/ -> /home/claude/.claude/\n")
	sb.WriteString("  sed -i 's|/home/[^\"]*.claude/|/home/claude/.claude/|g' \"$jsonfile\"\n")
	sb.WriteString("done\n")

	// Rewrite projectPath to point to VM workspace
	vmWorkspace := projectDir
	if vmWorkspace == "" {
		vmWorkspace = "/workspace"
	}
	sb.WriteString("# Rewrite projectPath to VM workspace\n")
	sb.WriteString("if [ -f /home/claude/.claude/plugins/installed_plugins.json ]; then\n")
	sb.WriteString(fmt.Sprintf("  sed -i 's|\"projectPath\": \"[^\"]*\"|\"projectPath\": \"%s\"|g' /home/claude/.claude/plugins/installed_plugins.json\n", strings.ReplaceAll(vmWorkspace, "'", "")))
	sb.WriteString("fi\n")

	// Verify the rewrite worked (debug only)
	sb.WriteString("if [ \"$FAIZE_DEBUG\" = \"1\" ]; then\n")
	sb.WriteString("  echo 'Verifying path rewrite...'\n")
	sb.WriteString("  grep -o 'installLocation.*' /home/claude/.claude/plugins/known_marketplaces.json 2>/dev/null | head -2 || echo 'known_marketplaces.json missing'\n")
	sb.WriteString("fi\n")
	sb.WriteString("\n")

	// Change to project directory
	if projectDir != "" {
		sb.WriteString(fmt.Sprintf("cd %s\n\n", shellQuote(projectDir)))
	} else {
		sb.WriteString("cd /workspace\n\n")
	}

	// Test npm registry connectivity (debug only - helps diagnose auto-update failures)
	sb.WriteString("# Test npm registry connectivity (debug only)\n")
	sb.WriteString("if [ \"$FAIZE_DEBUG\" = \"1\" ]; then\n")
	sb.WriteString("  if wget -q --spider --timeout=3 https://registry.npmjs.org 2>/dev/null; then\n")
	sb.WriteString("    echo 'npm registry OK'\n")
	sb.WriteString("  else\n")
	sb.WriteString("    echo 'npm registry FAILED'\n")
	sb.WriteString("  fi\n")
	sb.WriteString("fi\n\n")

	// Background terminal resize watcher — polls VirtioFS termsize file and
	// resizes PTYs when the host terminal dimensions change.
	sb.WriteString("# Background terminal resize watcher\n")
	sb.WriteString("(\n")
	sb.WriteString("  LAST_SIZE=\"\"\n")
	sb.WriteString("  while true; do\n")
	sb.WriteString("    if [ -f /mnt/bootstrap/termsize ]; then\n")
	sb.WriteString("      NEW_SIZE=$(cat /mnt/bootstrap/termsize 2>/dev/null) || true\n")
	sb.WriteString("      if [ -n \"$NEW_SIZE\" ] && [ \"$NEW_SIZE\" != \"$LAST_SIZE\" ]; then\n")
	sb.WriteString("        LAST_SIZE=\"$NEW_SIZE\"\n")
	sb.WriteString("        COLS=$(echo $NEW_SIZE | cut -d' ' -f1)\n")
	sb.WriteString("        ROWS=$(echo $NEW_SIZE | cut -d' ' -f2)\n")
	sb.WriteString("        if [ -n \"$COLS\" ] && [ -n \"$ROWS\" ]; then\n")
	sb.WriteString("          # Resize only the first PTY slave (created by script)\n")
	sb.WriteString("          # stty TIOCSWINSZ ioctl triggers SIGWINCH to the PTY's\n")
	sb.WriteString("          # foreground process group automatically\n")
	sb.WriteString("          PTY=$(ls /dev/pts/[0-9]* 2>/dev/null | head -1) || true\n")
	sb.WriteString("          if [ -n \"$PTY\" ]; then\n")
	sb.WriteString("            stty -F \"$PTY\" cols $COLS rows $ROWS 2>/dev/null || true\n")
	sb.WriteString("          fi\n")
	sb.WriteString("        fi\n")
	sb.WriteString("      fi\n")
	sb.WriteString("    fi\n")
	sb.WriteString("    sleep 1\n")
	sb.WriteString("  done\n")
	sb.WriteString(") &\n")
	sb.WriteString("RESIZE_WATCHER_PID=$!\n\n")

	// Launch Claude CLI as non-root user with PTY allocation via script command
	// The script command allocates a PTY which Claude/Ink requires for raw mode
	sb.WriteString("# Launch Claude CLI as non-root user with PTY allocation via script command\n")
	sb.WriteString("# The script command allocates a PTY which Claude/Ink requires for raw mode\n")
	sb.WriteString("# Disable exit-on-error for the script command to prevent kernel panic if it fails\n")
	sb.WriteString("set +e\n")
	sb.WriteString("script -q -c \"su -s /bin/sh claude -c 'export HOME=/home/claude && export PATH=/usr/local/bin:/usr/bin:/bin && export GIT_DISCOVERY_ACROSS_FILESYSTEM=1 && cd \\${PWD} && exec claude'\" /dev/null\n")
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
		sb.WriteString(fmt.Sprintf("cd %s 2>/dev/null || true\n", shellQuote(workDir)))
	}

	return sb.String()
}

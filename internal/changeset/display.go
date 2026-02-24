package changeset

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

const maxDisplayChanges = 20

// PrintSummary prints a human-readable change summary to the writer.
func PrintSummary(w io.Writer, cs *SessionChangeset) {
	if cs == nil {
		return
	}

	totalChanges := 0
	for _, mc := range cs.MountChanges {
		totalChanges += len(mc.Changes)
	}

	if totalChanges == 0 && len(cs.NetworkEvents) == 0 {
		_, _ = fmt.Fprintln(w, "\nNo changes detected.")
		return
	}

	_, _ = fmt.Fprintln(w, "\nSession Changes")
	_, _ = fmt.Fprintln(w, strings.Repeat("─", 40))

	// Print mount changes
	for _, mc := range cs.MountChanges {
		if len(mc.Changes) == 0 {
			continue
		}
		// Determine label based on mount target
		label := mountLabel(mc.Target)
		_, _ = fmt.Fprintf(w, "\n%s (%s → %s):\n", label, mc.Source, mc.Target)
		printChanges(w, mc.Changes)
	}

	// Print network activity summary
	if len(cs.NetworkEvents) > 0 {
		printNetworkSummary(w, cs.NetworkEvents)
	}
}

// mountLabel returns a human-friendly label based on the guest mount target
func mountLabel(target string) string {
	switch {
	case strings.HasPrefix(target, "/opt/toolchain"):
		return "Toolchain"
	case strings.HasPrefix(target, "/mnt/host-claude"):
		return "Claude Config"
	default:
		return "Project"
	}
}

// printChanges prints individual file changes, summarizing if >maxDisplayChanges
func printChanges(w io.Writer, changes []Change) {
	if len(changes) > maxDisplayChanges {
		// Show top 5 of each type, then summary
		created, modified, deleted := categorize(changes)
		shown := 0
		for _, c := range created {
			if shown >= 5 {
				break
			}
			printChange(w, c)
			shown++
		}
		for _, c := range modified {
			if shown >= 5 {
				break
			}
			printChange(w, c)
			shown++
		}
		for _, c := range deleted {
			if shown >= 5 {
				break
			}
			printChange(w, c)
			shown++
		}
		_, _ = fmt.Fprintf(w, "  (%d changes total: %d created, %d modified, %d deleted)\n",
			len(changes), len(created), len(modified), len(deleted))
		return
	}
	for _, c := range changes {
		printChange(w, c)
	}
}

// printChange prints a single change line
func printChange(w io.Writer, c Change) {
	switch c.Type {
	case "created":
		_, _ = fmt.Fprintf(w, "  + %-50s (%s)\n", c.Path, formatSize(c.NewSize))
	case "modified":
		_, _ = fmt.Fprintf(w, "  ~ %-50s (%s → %s)\n", c.Path, formatSize(c.OldSize), formatSize(c.NewSize))
	case "deleted":
		_, _ = fmt.Fprintf(w, "  - %s\n", c.Path)
	}
}

// categorize splits changes into created/modified/deleted slices
func categorize(changes []Change) (created, modified, deleted []Change) {
	for _, c := range changes {
		switch c.Type {
		case "created":
			created = append(created, c)
		case "modified":
			modified = append(modified, c)
		case "deleted":
			deleted = append(deleted, c)
		}
	}
	return
}

// formatSize returns a human-readable file size
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// printNetworkSummary prints a summary of network events grouped by action type.
func printNetworkSummary(w io.Writer, events []NetworkEvent) {
	_, _ = fmt.Fprintln(w, "\nNetwork activity")
	_, _ = fmt.Fprintln(w, strings.Repeat("─", 40))

	// Separate by type
	var dnsEvents, conns, denies []NetworkEvent
	for _, e := range events {
		switch e.Action {
		case "DNS":
			dnsEvents = append(dnsEvents, e)
		case "DENY":
			denies = append(denies, e)
		default:
			conns = append(conns, e)
		}
	}

	// DNS queries — show domain names
	if len(dnsEvents) > 0 {
		domains := make([]string, 0, len(dnsEvents))
		for _, e := range dnsEvents {
			domains = append(domains, e.Domain)
		}
		display := strings.Join(domains, ", ")
		if len(domains) > 5 {
			display = strings.Join(domains[:5], ", ") + fmt.Sprintf(", +%d more", len(domains)-5)
		}
		_, _ = fmt.Fprintf(w, "  DNS queries: %d (%s)\n", len(dnsEvents), display)
	}

	// Non-DNS connections — show domain when available, fall back to IP
	var nonDNSConns []NetworkEvent
	for _, e := range conns {
		if e.DstPort != 53 {
			nonDNSConns = append(nonDNSConns, e)
		}
	}
	if len(nonDNSConns) > 0 {
		connDests := make(map[string]bool)
		for _, e := range nonDNSConns {
			host := e.DstIP
			if e.Domain != "" {
				host = e.Domain
			}
			connDests[fmt.Sprintf("%s:%d", host, e.DstPort)] = true
		}
		destList := make([]string, 0, len(connDests))
		for dest := range connDests {
			destList = append(destList, dest)
		}
		sort.Strings(destList)
		display := strings.Join(destList, ", ")
		if len(destList) > 5 {
			display = strings.Join(destList[:5], ", ") + fmt.Sprintf(" (+%d more)", len(destList)-5)
		}
		_, _ = fmt.Fprintf(w, "  Connections: %d (%s)\n", len(connDests), display)
	}

	// Denied connections — same domain annotation
	if len(denies) > 0 {
		denyDests := make(map[string]bool)
		for _, e := range denies {
			host := e.DstIP
			if e.Domain != "" {
				host = e.Domain
			}
			denyDests[fmt.Sprintf("%s:%d", host, e.DstPort)] = true
		}
		destList := make([]string, 0, len(denyDests))
		for dest := range denyDests {
			destList = append(destList, dest)
		}
		sort.Strings(destList)
		_, _ = fmt.Fprintf(w, "  Denied: %d (%s)\n", len(denyDests), strings.Join(destList, ", "))
	}
}

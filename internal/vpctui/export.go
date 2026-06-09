package vpctui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Export for case notes
//
// exportMarkdown renders a VPC snapshot plus its findings as a self-contained
// Markdown report suitable for pasting into a support case or runbook. The
// rendering is pure (timestamp injected) so it is fully unit-testable.
// ---------------------------------------------------------------------------

// exportMarkdown builds the Markdown report for a VPC.
func exportMarkdown(snap vpcSnapshot, findings []Finding, region string, generatedAt time.Time) string {
	var b strings.Builder

	title := "VPC Report: " + snap.VPCID
	if region != "" {
		title += " (" + region + ")"
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString("_Generated " + generatedAt.UTC().Format("2006-01-02 15:04:05 UTC") + "_\n\n")

	// Summary counts.
	b.WriteString("## Summary\n\n| Resource | Count |\n|---|---|\n")
	for _, row := range [][2]string{
		{"Subnets", itoa(len(snap.Subnets))},
		{"Security groups", itoa(len(snap.SecurityGroups))},
		{"Route tables", itoa(len(snap.RouteTables))},
		{"Internet gateways", itoa(len(snap.InternetGateways))},
		{"NAT gateways", itoa(len(snap.NatGateways))},
		{"Network ACLs", itoa(len(snap.NetworkACLs))},
		{"VPC endpoints", itoa(len(snap.Endpoints))},
		{"Peering connections", itoa(len(snap.Peerings))},
		{"Network interfaces", itoa(len(snap.NetworkInterfaces))},
	} {
		b.WriteString("| " + row[0] + " | " + row[1] + " |\n")
	}
	b.WriteString("\n")

	// Findings, grouped by severity.
	crit, warn, info := countBySeverity(findings)
	b.WriteString(fmt.Sprintf("## Findings (%d critical, %d warning, %d info)\n\n", crit, warn, info))
	if len(findings) == 0 {
		b.WriteString("No issues detected. ✓\n\n")
	} else {
		writeFindingGroup(&b, "Critical", SevCritical, findings)
		writeFindingGroup(&b, "Warning", SevWarning, findings)
		writeFindingGroup(&b, "Info", SevInfo, findings)
	}

	// Resource inventory.
	if len(snap.Subnets) > 0 {
		b.WriteString("## Subnets\n\n| ID | CIDR | AZ | Available IPs | Public |\n|---|---|---|---|---|\n")
		for _, s := range snap.Subnets {
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %d | %s |\n",
				s.ID, s.CIDR, s.AZ, s.AvailableIPs, boolStr(s.IsPublic)))
		}
		b.WriteString("\n")
	}

	if len(snap.SecurityGroups) > 0 {
		b.WriteString("## Security groups\n\n| ID | Name | Inbound rules | Outbound rules |\n|---|---|---|---|\n")
		for _, sg := range snap.SecurityGroups {
			in, eg := 0, 0
			for _, r := range sg.Rules {
				if strings.EqualFold(r.Direction, "Inbound") {
					in++
				} else {
					eg++
				}
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %d | %d |\n", sg.ID, orDash(sg.Name), in, eg))
		}
		b.WriteString("\n")
	}

	if len(snap.RouteTables) > 0 {
		b.WriteString("## Route tables\n\n| ID | Name | Routes | Associations | Main |\n|---|---|---|---|---|\n")
		for _, rt := range snap.RouteTables {
			b.WriteString(fmt.Sprintf("| %s | %s | %d | %d | %s |\n",
				rt.ID, orDash(rt.Name), len(rt.Routes), len(rt.Associations), boolStr(rt.IsMain)))
		}
		b.WriteString("\n")
	}

	if len(snap.NatGateways) > 0 {
		b.WriteString("## NAT gateways\n\n| ID | State | Subnet | Public IP |\n|---|---|---|---|\n")
		for _, n := range snap.NatGateways {
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", n.ID, n.State, n.SubnetID, orDash(n.PublicIP)))
		}
		b.WriteString("\n")
	}

	if len(snap.Endpoints) > 0 {
		b.WriteString("## VPC endpoints\n\n| ID | Service | Type | State |\n|---|---|---|---|\n")
		for _, e := range snap.Endpoints {
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", e.ID, e.ServiceName, e.Type, e.State))
		}
		b.WriteString("\n")
	}

	if len(snap.NetworkInterfaces) > 0 {
		b.WriteString("## Network interfaces\n\n| ID | Type | Private IP | Public IP | Attached To |\n|---|---|---|---|---|\n")
		for _, e := range snap.NetworkInterfaces {
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				e.ID, e.Type, orDash(e.PrivateIP), orDash(e.PublicIP), orDash(e.AttachedTo)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func writeFindingGroup(b *strings.Builder, label string, sev Severity, findings []Finding) {
	var group []Finding
	for _, f := range findings {
		if f.Severity == sev {
			group = append(group, f)
		}
	}
	if len(group) == 0 {
		return
	}
	b.WriteString("### " + label + "\n\n")
	for _, f := range group {
		b.WriteString(fmt.Sprintf("- **%s** (`%s`) — %s\n", f.Title, f.Resource, f.Detail))
		if f.Fix != "" {
			b.WriteString("  - Fix: " + f.Fix + "\n")
		}
	}
	b.WriteString("\n")
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// ---------------------------------------------------------------------------
// File output
// ---------------------------------------------------------------------------

// exportDir returns the directory where reports are written, creating it.
func exportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".aws_explorer", "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// writeExport writes the report to a timestamped file and returns its path.
func writeExport(snap vpcSnapshot, findings []Finding, region string, now time.Time) (string, error) {
	dir, err := exportDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.md", snap.VPCID, now.Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(exportMarkdown(snap, findings, region, now)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

package emrtui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/downloads"
	"github.com/ryandam9/aws_explorer/internal/findings"
)

// The describe view's "s" key writes the full cluster description to a
// sectioned Markdown file in the downloads directory — a shareable report of
// everything the describe gathered: overview, configuration/OS, S3 connector,
// applications, node groups, EC2 instances, networking (security groups,
// routes, NACL), configuration classifications, and the best-effort notes.

// mdCell makes a value safe inside a Markdown table cell: pipes are escaped,
// newlines flattened, and an empty value reads as an em dash.
func mdCell(s string) string {
	s = strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s))
	if s == "" {
		return "—"
	}
	return strings.ReplaceAll(s, "|", "\\|")
}

// mdKVTable renders label/value pairs as a two-column Markdown table.
func mdKVTable(b *strings.Builder, rows [][2]string) {
	b.WriteString("| | |\n|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| **%s** | %s |\n", mdCell(r[0]), mdCell(r[1]))
	}
}

// mdTable renders a header + rows as a Markdown table.
func mdTable(b *strings.Builder, header []string, rows [][]string) {
	for i, h := range header {
		if i > 0 {
			b.WriteString(" | ")
		} else {
			b.WriteString("| ")
		}
		b.WriteString(mdCell(h))
	}
	b.WriteString(" |\n|")
	b.WriteString(strings.Repeat("---|", len(header)))
	b.WriteString("\n")
	for _, row := range rows {
		for i, c := range row {
			if i > 0 {
				b.WriteString(" | ")
			} else {
				b.WriteString("| ")
			}
			b.WriteString(mdCell(c))
		}
		b.WriteString(" |\n")
	}
}

// clusterMarkdown renders the full describe result as a Markdown document.
// Pure over its inputs (now is injected) so it is fixture-testable.
func clusterMarkdown(d ClusterDescription, now time.Time) string {
	cl := d.Cluster
	var b strings.Builder

	fmt.Fprintf(&b, "# EMR cluster %s (%s)\n\n", mdCell(cl.Name), mdCell(cl.ID))
	fmt.Fprintf(&b, "_Exported by aws_explorer on %s_\n\n", now.Format("2006-01-02 15:04:05 MST"))

	b.WriteString("## Overview\n\n")
	mdKVTable(&b, [][2]string{
		{"Name", cl.Name},
		{"Cluster ID", cl.ID},
		{"State", stateLabel(cl.State)},
		{"State reason", cl.StateReason},
		{"Region", cl.Region},
		{"Created", shortTime(cl.Created)},
		{"Normalized instance hours", instanceHours(cl.InstanceHours)},
		{"Master DNS", cl.MasterDNS},
		{"ARN", cl.ARN},
	})

	b.WriteString("\n## Configuration & OS\n\n")
	mdKVTable(&b, [][2]string{
		{"Release", d.ReleaseLabel},
		{"Operating system", osLabel(d)},
		{"OS release label", d.OSReleaseLabel},
		{"Architecture", architectureLabel(d.Groups)},
		{"Custom AMI", d.CustomAmiID},
		{"Instance collection", d.InstanceCollection},
		{"Auto-terminate", boolLabel(cl.AutoTerminate)},
		{"Termination protection", triStateLabel(d.TerminationProt)},
		{"Scale-down behavior", d.ScaleDownBehavior},
		{"EBS root volume", gibLabel(d.EbsRootVolumeGiB)},
		{"Log URI", cl.LogURI},
		{"Security configuration", cl.SecurityConfig},
		{"Service role", cl.ServiceRole},
		{"Instance profile", cl.InstanceProfile},
		{"EC2 key pair", cl.KeyName},
	})

	b.WriteString("\n## S3 connector\n\n")
	v := findings.DeriveS3Connector(findings.S3ConnectorInput{
		ReleaseLabel:    d.ReleaseLabel,
		Classifications: classificationMap(d.Configurations),
	})
	mdKVTable(&b, [][2]string{
		{"Effective", connectorEffectiveLabel(v)},
		{"Release default", connectorDefaultLabel(v)},
		{"Consistent View", consistentViewLabel(v)},
		{"S3 encryption", v.Encryption},
	})

	b.WriteString("\n## Applications\n\n")
	if len(d.Applications) == 0 {
		b.WriteString("_No applications reported._\n")
	} else {
		rows := make([][]string, 0, len(d.Applications))
		for _, a := range d.Applications {
			rows = append(rows, []string{a.Name, a.Version})
		}
		mdTable(&b, []string{"Application", "Version"}, rows)
	}

	b.WriteString("\n## Compute, memory & storage\n\n")
	if len(d.Groups) == 0 {
		b.WriteString("_No instance groups reported._\n")
	} else {
		rows := make([][]string, 0, len(d.Groups))
		for _, g := range d.Groups {
			rows = append(rows, []string{
				g.Role, g.Name, g.InstanceType, g.Market,
				fmt.Sprintf("%d / %d", g.Running, g.Requested),
				vcpuLabel(g), memoryLabel(g), g.State, ebsLabel(g.EBSVolumes),
			})
		}
		mdTable(&b, []string{"Role", "Name", "Instance type", "Market", "Running / requested", "vCPU", "Memory", "State", "EBS storage"}, rows)
	}

	b.WriteString("\n## EC2 instances\n\n")
	if len(d.Instances) == 0 {
		b.WriteString("_No instances reported._\n")
	} else {
		rows := make([][]string, 0, len(d.Instances))
		for _, in := range d.Instances {
			rows = append(rows, []string{in.EC2ID, in.Type, in.Market, in.State, in.Group, in.PrivateDNS, in.PublicDNS})
		}
		mdTable(&b, []string{"EC2 ID", "Type", "Market", "State", "Group", "Private DNS", "Public DNS"}, rows)
	}

	writeNetworkMarkdown(&b, d.Network)

	if len(d.Configurations) > 0 {
		b.WriteString("\n## Configurations\n")
		for _, c := range d.Configurations {
			fmt.Fprintf(&b, "\n### %s\n\n", mdCell(c.Classification))
			if len(c.Properties) == 0 {
				b.WriteString("_No properties._\n")
				continue
			}
			keys := make([]string, 0, len(c.Properties))
			for k := range c.Properties {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{k, c.Properties[k]})
			}
			mdTable(&b, []string{"Property", "Value"}, rows)
		}
	}

	if len(d.Notes) > 0 {
		b.WriteString("\n## Notes\n\n")
		b.WriteString("Best-effort gaps — these sections could not be fully gathered:\n\n")
		for _, n := range d.Notes {
			fmt.Fprintf(&b, "- ⚠ %s\n", n)
		}
	}

	return b.String()
}

// writeNetworkMarkdown renders the networking section with sub-tables for
// security groups, routes and the network ACL.
func writeNetworkMarkdown(b *strings.Builder, n NetworkInfo) {
	b.WriteString("\n## Networking\n\n")
	mdKVTable(b, [][2]string{
		{"VPC", n.VPCID},
		{"Subnet", n.SubnetID},
		{"Subnet CIDR", n.CIDR},
		{"Availability zone", n.AZ},
		{"Public IP on launch", triStateLabel(n.MapPublicIP)},
	})

	if len(n.SecurityGroups) > 0 {
		b.WriteString("\n### Security groups\n")
		for _, sg := range n.SecurityGroups {
			fmt.Fprintf(b, "\n**%s** — %s _[%s]_\n\n", mdCell(sg.ID), mdCell(sg.Name), mdCell(sg.Kind))
			if !sg.Known {
				b.WriteString("_Rules unavailable (DescribeSecurityGroups denied or the group was not returned)._\n")
				continue
			}
			if len(sg.Rules) == 0 {
				b.WriteString("_No rules._\n")
				continue
			}
			rows := make([][]string, 0, len(sg.Rules))
			for _, r := range sg.Rules {
				rows = append(rows, []string{r.Direction, r.Protocol, r.Ports, r.Source})
			}
			mdTable(b, []string{"Direction", "Protocol", "Ports", "Source"}, rows)
		}
	}

	if n.RouteTableID != "" || len(n.Routes) > 0 {
		fmt.Fprintf(b, "\n### Route table %s\n\n", mdCell(n.RouteTableID))
		if len(n.Routes) == 0 {
			b.WriteString("_No routes._\n")
		} else {
			rows := make([][]string, 0, len(n.Routes))
			for _, r := range n.Routes {
				rows = append(rows, []string{r.Destination, r.Target, r.State})
			}
			mdTable(b, []string{"Destination", "Target", "State"}, rows)
		}
	}

	if n.NaclID != "" || len(n.NaclEntries) > 0 {
		fmt.Fprintf(b, "\n### Network ACL %s\n\n", mdCell(n.NaclID))
		if len(n.NaclEntries) == 0 {
			b.WriteString("_No rules._\n")
		} else {
			rows := make([][]string, 0, len(n.NaclEntries))
			for _, e := range n.NaclEntries {
				num := "*"
				if e.RuleNumber != 32767 {
					num = itoa(int(e.RuleNumber))
				}
				rows = append(rows, []string{e.Direction, num, e.Protocol, e.Ports, e.CIDR, e.Action})
			}
			mdTable(b, []string{"Direction", "Rule #", "Protocol", "Ports", "CIDR", "Action"}, rows)
		}
	}

	if n.Note != "" {
		fmt.Fprintf(b, "\n> ⚠ %s\n", n.Note)
	}
}

// exportClusterMarkdown writes the describe report to the downloads directory
// and returns the path.
func exportClusterMarkdown(d ClusterDescription) (string, error) {
	dir, err := downloads.Dir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("emr-%s-%s.md", sanitizeFile(d.Cluster.ID), time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(clusterMarkdown(d, time.Now())), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// sanitizeFile keeps a cluster ID filesystem-friendly.
func sanitizeFile(s string) string {
	if s == "" {
		return "cluster"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, s)
}

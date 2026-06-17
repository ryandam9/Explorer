package emrtui

import (
	"fmt"
	"sort"
	"strings"
)

// descSection is one titled block of the cluster-describe report. Body is the
// already-assembled plain text (no styling) so the CLI twin and the TUI overlay
// share one layout — the TUI only adds heading colour to Title.
type descSection struct {
	Title string
	Body  string
}

// descKV renders a "label value" line with a stable label column, matching the
// detail overlay's alignment. An empty value reads as a muted em dash.
func descKV(label, value string) string {
	if strings.TrimSpace(value) == "" {
		value = "—"
	}
	return fmt.Sprintf("  %-20s %s", label, value)
}

// sections builds the ordered describe report. Pure over the description, so it
// is fixture-tested and reused verbatim by both the TUI overlay and the CLI
// twin.
func (d ClusterDescription) sections() []descSection {
	var out []descSection

	// Overview.
	cl := d.Cluster
	var ov strings.Builder
	ov.WriteString(descKV("Name", cl.Name) + "\n")
	ov.WriteString(descKV("Cluster ID", cl.ID) + "\n")
	ov.WriteString(descKV("State", stateLabel(cl.State)) + "\n")
	if cl.StateReason != "" {
		ov.WriteString(descKV("State reason", cl.StateReason) + "\n")
	}
	ov.WriteString(descKV("Region", cl.Region) + "\n")
	ov.WriteString(descKV("Created", shortTime(cl.Created)) + "\n")
	ov.WriteString(descKV("Normalized hrs", instanceHours(cl.InstanceHours)) + "\n")
	ov.WriteString(descKV("Master DNS", cl.MasterDNS) + "\n")
	ov.WriteString(descKV("ARN", cl.ARN))
	out = append(out, descSection{Title: "Overview", Body: ov.String()})

	// Configuration & OS.
	var cfg strings.Builder
	cfg.WriteString(descKV("Release", d.ReleaseLabel) + "\n")
	cfg.WriteString(descKV("Operating system", osLabel(d)) + "\n")
	if d.OSReleaseLabel != "" {
		cfg.WriteString(descKV("OS release label", d.OSReleaseLabel) + "\n")
	}
	cfg.WriteString(descKV("Architecture", architectureLabel(d.Groups)) + "\n")
	if d.CustomAmiID != "" {
		cfg.WriteString(descKV("Custom AMI", d.CustomAmiID) + "\n")
	}
	cfg.WriteString(descKV("Auto-terminate", boolLabel(cl.AutoTerminate)) + "\n")
	cfg.WriteString(descKV("Termination prot.", triStateLabel(d.TerminationProt)) + "\n")
	if d.ScaleDownBehavior != "" {
		cfg.WriteString(descKV("Scale-down", d.ScaleDownBehavior) + "\n")
	}
	cfg.WriteString(descKV("EBS root volume", gibLabel(d.EbsRootVolumeGiB)) + "\n")
	cfg.WriteString(descKV("Log URI", cl.LogURI) + "\n")
	cfg.WriteString(descKV("Security config", cl.SecurityConfig) + "\n")
	cfg.WriteString(descKV("Service role", cl.ServiceRole) + "\n")
	cfg.WriteString(descKV("Instance profile", cl.InstanceProfile) + "\n")
	cfg.WriteString(descKV("EC2 key", cl.KeyName))
	out = append(out, descSection{Title: "Configuration & OS", Body: cfg.String()})

	// Services (applications).
	out = append(out, descSection{Title: "Services", Body: servicesBody(d.Applications)})

	// Compute, memory & storage (per node group).
	out = append(out, descSection{Title: "Compute, memory & storage", Body: groupsBody(d.Groups)})

	// Running instances.
	out = append(out, descSection{Title: "EC2 instances", Body: instancesBody(d.Instances)})

	// Networking.
	out = append(out, descSection{Title: "Networking", Body: networkBody(d.Network)})

	// Configurations (classifications) — last, can be long.
	if len(d.Configurations) > 0 {
		out = append(out, descSection{Title: "Configurations", Body: configurationsBody(d.Configurations)})
	}

	// Notes (best-effort gaps).
	if len(d.Notes) > 0 {
		var nb strings.Builder
		for i, n := range d.Notes {
			if i > 0 {
				nb.WriteString("\n")
			}
			nb.WriteString("  ⚠ " + n)
		}
		out = append(out, descSection{Title: "Notes", Body: nb.String()})
	}
	return out
}

// osLabel describes the cluster OS. EMR runs Amazon Linux; the release label
// pins the AMI generation, and a custom AMI overrides the base image.
func osLabel(d ClusterDescription) string {
	base := "Amazon Linux"
	if d.OSReleaseLabel != "" {
		base += " " + d.OSReleaseLabel
	} else if d.ReleaseLabel != "" {
		base += " (" + d.ReleaseLabel + ")"
	}
	if d.CustomAmiID != "" {
		base += " · custom AMI"
	}
	return base
}

// architectureLabel reports the processor architecture resolved from the node
// groups (they share one architecture in practice).
func architectureLabel(groups []NodeGroup) string {
	for _, g := range groups {
		if g.SpecsKnown && g.Architecture != "" {
			return g.Architecture
		}
	}
	return ""
}

// servicesBody lists the installed applications and versions.
func servicesBody(apps []AppInfo) string {
	if len(apps) == 0 {
		return "  (no applications reported)"
	}
	var b strings.Builder
	for i, a := range apps {
		if i > 0 {
			b.WriteString("\n")
		}
		v := a.Version
		if v == "" {
			v = "—"
		}
		b.WriteString(fmt.Sprintf("  %-18s %s", a.Name, v))
	}
	return b.String()
}

// groupsBody renders each instance group/fleet with its memory, vCPU, count and
// EBS storage. Memory and vCPU are per-instance; storage is per-instance EBS.
func groupsBody(groups []NodeGroup) string {
	if len(groups) == 0 {
		return "  (no instance groups reported)"
	}
	var b strings.Builder
	for i, g := range groups {
		if i > 0 {
			b.WriteString("\n\n")
		}
		role := g.Role
		if role == "" {
			role = "?"
		}
		b.WriteString(fmt.Sprintf("  %s — %s", role, dashIfEmpty(g.InstanceType)))
		if g.Market != "" {
			b.WriteString(" (" + g.Market + ")")
		}
		b.WriteString("\n")
		b.WriteString(descKV("  count", fmt.Sprintf("%d running / %d requested", g.Running, g.Requested)) + "\n")
		b.WriteString(descKV("  memory", memoryLabel(g)) + "\n")
		b.WriteString(descKV("  vCPU", vcpuLabel(g)) + "\n")
		if g.State != "" {
			b.WriteString(descKV("  state", g.State) + "\n")
		}
		b.WriteString(descKV("  EBS storage", ebsLabel(g.EBSVolumes)))
	}
	return b.String()
}

// memoryLabel renders a group's per-instance memory, or "—" when the EC2 spec
// lookup was denied (distinguishing unknown from a real value).
func memoryLabel(g NodeGroup) string {
	if !g.SpecsKnown || g.MemoryMiB <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f GiB", float64(g.MemoryMiB)/1024)
}

func vcpuLabel(g NodeGroup) string {
	if !g.SpecsKnown || g.VCPUs <= 0 {
		return "—"
	}
	return itoa(int(g.VCPUs))
}

// ebsLabel summarizes a group's EBS volumes ("2 × gp3 100 GiB"), or "EBS-only
// root / instance store" when none are attached.
func ebsLabel(vols []EBSVolume) string {
	if len(vols) == 0 {
		return "none (root + instance store only)"
	}
	parts := make([]string, 0, len(vols))
	for _, v := range vols {
		s := fmt.Sprintf("%s %d GiB", dashIfEmpty(v.VolumeType), v.SizeGiB)
		if v.Iops > 0 {
			s += fmt.Sprintf(" (%d IOPS)", v.Iops)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// instancesBody lists the running EC2 instances grouped by their market/type.
func instancesBody(instances []Instance) string {
	if len(instances) == 0 {
		return "  (no instances reported)"
	}
	var b strings.Builder
	for i, in := range instances {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("  %-19s %-12s %-10s %s",
			dashIfEmpty(in.EC2ID), dashIfEmpty(in.Type), dashIfEmpty(in.State), dashIfEmpty(in.PrivateDNS)))
	}
	return b.String()
}

// networkBody renders the VPC networking: subnet, security-group rules, routes
// and network ACL.
func networkBody(n NetworkInfo) string {
	var b strings.Builder
	b.WriteString(descKV("VPC", n.VPCID) + "\n")
	b.WriteString(descKV("Subnet", n.SubnetID) + "\n")
	b.WriteString(descKV("Subnet CIDR", n.CIDR) + "\n")
	b.WriteString(descKV("Availability zone", n.AZ) + "\n")
	b.WriteString(descKV("Public IP on launch", triStateLabel(n.MapPublicIP)))

	// Security groups.
	if len(n.SecurityGroups) > 0 {
		b.WriteString("\n\n  Security groups:")
		for _, sg := range n.SecurityGroups {
			name := sg.Name
			if name == "" {
				name = "—"
			}
			b.WriteString(fmt.Sprintf("\n    %s  %s  [%s]", sg.ID, name, sg.Kind))
			if !sg.Known {
				b.WriteString("  (rules unavailable)")
				continue
			}
			if len(sg.Rules) == 0 {
				b.WriteString("\n      (no rules)")
				continue
			}
			for _, r := range sg.Rules {
				b.WriteString(fmt.Sprintf("\n      %-8s %-4s %-9s %s", r.Direction, r.Protocol, r.Ports, r.Source))
			}
		}
	}

	// Routes.
	if n.RouteTableID != "" || len(n.Routes) > 0 {
		b.WriteString("\n\n  Route table " + dashIfEmpty(n.RouteTableID) + ":")
		if len(n.Routes) == 0 {
			b.WriteString("\n    (no routes)")
		}
		for _, r := range n.Routes {
			b.WriteString(fmt.Sprintf("\n    %-20s → %-22s %s", dashIfEmpty(r.Destination), dashIfEmpty(r.Target), r.State))
		}
	}

	// Network ACL.
	if n.NaclID != "" || len(n.NaclEntries) > 0 {
		b.WriteString("\n\n  Network ACL " + dashIfEmpty(n.NaclID) + ":")
		if len(n.NaclEntries) == 0 {
			b.WriteString("\n    (no rules)")
		}
		for _, e := range n.NaclEntries {
			num := "*"
			if e.RuleNumber != 32767 {
				num = itoa(int(e.RuleNumber))
			}
			b.WriteString(fmt.Sprintf("\n    %-8s %-5s %-4s %-9s %-18s %s",
				e.Direction, num, e.Protocol, e.Ports, dashIfEmpty(e.CIDR), e.Action))
		}
	}

	if n.Note != "" {
		b.WriteString("\n\n  ⚠ " + n.Note)
	}
	return b.String()
}

// configurationsBody renders the configuration classifications and their
// properties.
func configurationsBody(cfgs []ConfigClassification) string {
	var b strings.Builder
	for i, c := range cfgs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  " + c.Classification)
		keys := make([]string, 0, len(c.Properties))
		for k := range c.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("\n    %s = %s", k, c.Properties[k]))
		}
	}
	return b.String()
}

// triStateLabel renders a *bool posture fact: yes / no / unknown. A nil pointer
// is a denied or unreported fact, not a definite "no" (the tool's tri-state
// discipline).
func triStateLabel(b *bool) string {
	if b == nil {
		return "unknown"
	}
	if *b {
		return "yes"
	}
	return "no"
}

// gibLabel renders a GiB size, or "—" when zero/unknown.
func gibLabel(gib int32) string {
	if gib <= 0 {
		return "—"
	}
	return itoa(int(gib)) + " GiB"
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

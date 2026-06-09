package vpctui

import "strings"

// ---------------------------------------------------------------------------
// DNS / VPC attribute analysis
//
// "DNS doesn't resolve in my VPC" is almost always one of: enableDnsSupport
// off, enableDnsHostnames off, or a custom DHCP option set that bypasses the
// Amazon Route 53 Resolver. VPCDNSInfo captures those attributes and dnsNotes
// turns them into plain-English observations.
// ---------------------------------------------------------------------------

// VPCDNSInfo holds the DNS-relevant attributes of a VPC.
type VPCDNSInfo struct {
	VPCID              string
	EnableDnsSupport   bool
	EnableDnsHostnames bool
	DhcpOptionsID      string
	DomainNameServers  []string
	DomainName         string
}

// dnsNote is a single observation about a VPC's DNS configuration.
type dnsNote struct {
	Severity Severity
	Text     string
}

// usesCustomDNS reports whether the DHCP option set points at DNS servers other
// than the Amazon-provided resolver.
func usesCustomDNS(servers []string) bool {
	for _, s := range servers {
		if s != "" && !strings.EqualFold(strings.TrimSpace(s), "AmazonProvidedDNS") {
			return true
		}
	}
	return false
}

// dnsNotes returns observations about a VPC's DNS configuration, most severe
// first. It always returns at least one note.
func dnsNotes(info VPCDNSInfo) []dnsNote {
	var notes []dnsNote

	if !info.EnableDnsSupport {
		notes = append(notes, dnsNote{SevCritical,
			"DNS resolution (enableDnsSupport) is disabled — the Amazon DNS server will not answer queries, so most name resolution in this VPC will fail."})
	}
	if !info.EnableDnsHostnames {
		notes = append(notes, dnsNote{SevWarning,
			"DNS hostnames (enableDnsHostnames) are disabled — instances are not assigned public DNS names, and the private DNS of interface VPC endpoints will not resolve."})
	}
	if usesCustomDNS(info.DomainNameServers) {
		notes = append(notes, dnsNote{SevInfo,
			"This VPC uses custom DNS servers (" + strings.Join(info.DomainNameServers, ", ") +
				"); the Amazon Route 53 Resolver is bypassed, so VPC endpoint private DNS and private hosted zones may not resolve unless those servers forward to it."})
	}

	if len(notes) == 0 {
		notes = append(notes, dnsNote{SevInfo,
			"DNS resolution and hostnames are enabled with the Amazon Route 53 Resolver."})
	}
	return notes
}

package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func TestUsesCustomDNS(t *testing.T) {
	if usesCustomDNS([]string{"AmazonProvidedDNS"}) {
		t.Error("AmazonProvidedDNS should not count as custom")
	}
	if usesCustomDNS(nil) {
		t.Error("empty should not count as custom")
	}
	if !usesCustomDNS([]string{"10.0.0.2"}) {
		t.Error("an explicit server should count as custom")
	}
	if !usesCustomDNS([]string{"AmazonProvidedDNS", "10.0.0.2"}) {
		t.Error("a mixed list with a custom server should count as custom")
	}
}

func TestDNSNotes(t *testing.T) {
	// Healthy VPC -> single positive note.
	ok := dnsNotes(VPCDNSInfo{EnableDnsSupport: true, EnableDnsHostnames: true, DomainNameServers: []string{"AmazonProvidedDNS"}})
	if len(ok) != 1 || ok[0].Severity != SevInfo {
		t.Errorf("healthy VPC should yield one info note, got %+v", ok)
	}

	// DNS support off -> critical.
	off := dnsNotes(VPCDNSInfo{EnableDnsSupport: false, EnableDnsHostnames: true})
	if findNote(off, SevCritical) == nil {
		t.Errorf("disabled DNS support should be critical, got %+v", off)
	}

	// Hostnames off -> warning.
	hn := dnsNotes(VPCDNSInfo{EnableDnsSupport: true, EnableDnsHostnames: false})
	if findNote(hn, SevWarning) == nil {
		t.Errorf("disabled hostnames should warn, got %+v", hn)
	}

	// Custom DNS -> info note mentioning the servers.
	custom := dnsNotes(VPCDNSInfo{EnableDnsSupport: true, EnableDnsHostnames: true, DomainNameServers: []string{"10.0.0.2"}})
	var sawCustom bool
	for _, n := range custom {
		if strings.Contains(n.Text, "custom DNS servers") && strings.Contains(n.Text, "10.0.0.2") {
			sawCustom = true
		}
	}
	if !sawCustom {
		t.Errorf("custom DNS should produce a note, got %+v", custom)
	}
}

func findNote(notes []dnsNote, sev Severity) *dnsNote {
	for i := range notes {
		if notes[i].Severity == sev {
			return &notes[i]
		}
	}
	return nil
}

func TestRenderDNS(t *testing.T) {
	m := &Model{dnsInfo: VPCDNSInfo{
		VPCID: "vpc-1", EnableDnsSupport: true, EnableDnsHostnames: false,
		DhcpOptionsID: "dopt-1", DomainNameServers: []string{"AmazonProvidedDNS"}, DomainName: "ec2.internal",
	}}
	m.dnsVP = viewport.New(80, 20)
	out := ansi.Strip(m.renderDNS())
	for _, want := range []string{"DNS resolution", "Enabled", "DNS hostnames", "Disabled", "dopt-1", "ec2.internal", "Notes"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDNS missing %q:\n%s", want, out)
		}
	}
}

func TestViewDNSOverlay(t *testing.T) {
	m := &Model{width: 100, height: 30, dnsInfo: VPCDNSInfo{VPCID: "vpc-1", EnableDnsSupport: true, EnableDnsHostnames: true}}
	m.dnsVP = viewport.New(80, 20)
	m.dnsVP.SetContent(m.renderDNS())
	out := ansi.Strip(m.viewDNSOverlay("bg"))
	if !strings.Contains(out, "DNS & VPC attributes: vpc-1") {
		t.Errorf("overlay should show the title, got:\n%s", out)
	}
}

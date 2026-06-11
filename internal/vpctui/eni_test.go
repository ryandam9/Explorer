package vpctui

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/display"
)

func TestENIToMap(t *testing.T) {
	m := eniToMap(ENIInfo{
		ID:              "eni-0a1b",
		Type:            "interface",
		Status:          "in-use",
		PrivateIP:       "10.0.1.5",
		PublicIP:        "", // -> "-"
		SubnetID:        "subnet-1",
		AZ:              "ap-southeast-2a",
		AttachedTo:      "i-0123",
		SecurityGroups:  []string{"sg-1", "sg-2"},
		SourceDestCheck: true,
		Description:     "primary interface",
		VPCID:           "vpc-1",
	})

	want := map[string]string{
		"eni_id":            "eni-0a1b",
		"type":              "interface",
		"status":            "in-use",
		"private_ip":        "10.0.1.5",
		"public_ip":         "-",
		"attached_to":       "i-0123",
		"security_groups":   "sg-1, sg-2",
		"source_dest_check": "Yes",
		"subnet_id":         "subnet-1",
		"az":                "ap-southeast-2a",
		"description":       "primary interface",
		"vpc_id":            "vpc-1",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("eniToMap[%q] = %q, want %q", k, m[k], v)
		}
	}
}

func TestENIToMapServiceManaged(t *testing.T) {
	// A service-managed ENI (e.g. NAT/Lambda) has no instance attachment.
	m := eniToMap(ENIInfo{ID: "eni-nat", Type: "nat_gateway", Status: "in-use", AttachedTo: "-"})
	if m["attached_to"] != "-" {
		t.Errorf("expected '-' for unattached ENI, got %q", m["attached_to"])
	}
	if m["security_groups"] != "-" {
		t.Errorf("expected '-' for empty SG list, got %q", m["security_groups"])
	}
}

func TestNetworkInterfacesResourceTypeWired(t *testing.T) {
	if got := rtKey(rtNetworkInterfaces); got != "network_interfaces" {
		t.Errorf("rtKey = %q, want network_interfaces", got)
	}
	if got := rtLabel(rtNetworkInterfaces); got != "Network Interfaces" {
		t.Errorf("rtLabel = %q, want Network Interfaces", got)
	}

	fields, ok := display.VPCFields["network_interfaces"]
	if !ok {
		t.Fatal("network_interfaces not registered in display.VPCFields")
	}
	// Default columns should lead with the ENI ID.
	cols := display.Columns(display.ResolveColumns(fields, nil))
	if len(cols) < 2 || cols[1].Title != "ENI ID" {
		t.Errorf("expected ENI ID as first data column, got %+v", cols)
	}

	// The sidebar must expose the new resource under Network.
	found := false
	for _, item := range buildSidebarItems() {
		if !item.isHeader && item.rt == rtNetworkInterfaces {
			found = true
		}
	}
	if !found {
		t.Error("Network Interfaces missing from the sidebar")
	}
}

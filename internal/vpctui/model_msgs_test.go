package vpctui

import "testing"

// A reply that was started for one VPC must not land in the model after the
// user navigates to a different VPC (or back to the list).
func TestStaleResourceReplyDropped(t *testing.T) {
	vpcB := VPCInfo{ID: "vpc-b"}
	m := &Model{
		selectedVPC:     &vpcB,
		resourceMaps:    map[resourceType][]map[string]string{},
		resourceLoading: true,
	}

	m.Update(resourcesLoadedMsg{vpcID: "vpc-a", rt: rtSecurityGroups,
		maps: []map[string]string{{"sg_id": "sg-stale"}}})
	if _, ok := m.resourceMaps[rtSecurityGroups]; ok {
		t.Error("reply for vpc-a should be dropped while browsing vpc-b")
	}
	if !m.resourceLoading {
		t.Error("a stale reply must not clear the loading state")
	}

	m.Update(resourcesLoadedMsg{vpcID: "vpc-b", rt: rtSecurityGroups,
		maps: []map[string]string{{"sg_id": "sg-fresh"}}})
	if got := m.resourceMaps[rtSecurityGroups]; len(got) != 1 || got[0]["sg_id"] != "sg-fresh" {
		t.Errorf("reply for the current VPC should be stored, got %+v", got)
	}
}

func TestStaleFindingsReplyDropped(t *testing.T) {
	m := &Model{findingsLoading: true} // selectedVPC nil: user went back to the list
	m.Update(findingsLoadedMsg{vpcID: "vpc-a", findings: []Finding{{Title: "x"}}})
	if len(m.findings) != 0 {
		t.Error("findings for a deselected VPC should be dropped")
	}
}

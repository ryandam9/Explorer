package findings

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// sarifDoc mirrors just enough of the SARIF shape to assert on output.
type sarifDoc struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []struct {
		Tool struct {
			Driver struct {
				Name            string `json:"name"`
				SemanticVersion string `json:"semanticVersion"`
				Rules           []struct {
					ID               string `json:"id"`
					Name             string `json:"name"`
					ShortDescription struct {
						Text string `json:"text"`
					} `json:"shortDescription"`
					DefaultConfiguration struct {
						Level string `json:"level"`
					} `json:"defaultConfiguration"`
				} `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Results []struct {
			RuleID    string `json:"ruleId"`
			RuleIndex int    `json:"ruleIndex"`
			Level     string `json:"level"`
			Message   struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
				} `json:"physicalLocation"`
				LogicalLocations []struct {
					FullyQualifiedName string `json:"fullyQualifiedName"`
				} `json:"logicalLocations"`
			} `json:"locations"`
			Properties map[string]any `json:"properties"`
		} `json:"results"`
	} `json:"runs"`
}

func renderSARIFDoc(t *testing.T, fs []Finding) sarifDoc {
	t.Helper()
	var buf bytes.Buffer
	if err := RenderSARIF(&buf, fs, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	var doc sarifDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}
	return doc
}

func TestRenderSARIF(t *testing.T) {
	fs := []Finding{
		{ID: CheckUnattachedVolume, Severity: SevWarning, Service: "ec2", Region: "us-east-1",
			Resource: "vol-0abc", Title: "Unattached EBS volume (gp2, 1024 GiB)",
			Detail: "still bills", Fix: "delete it", EstMonthlyUSD: 102.40,
			ARN: "arn:aws:ec2:us-east-1:1:volume/vol-0abc"},
		{ID: CheckUnattachedVolume, Severity: SevWarning, Service: "ec2", Region: "eu-west-1",
			Resource: "vol-0def", Title: "Unattached EBS volume (gp3, 10 GiB)", EstMonthlyUSD: 0.80},
		{ID: CheckIdleNATGateway, Severity: SevWarning, Service: "ec2", Region: "us-east-1",
			Resource: "nat-01 (spare)", Title: "NAT gateway not referenced by any route", EstMonthlyUSD: 32.85},
	}
	doc := renderSARIFDoc(t, fs)

	if doc.Version != "2.1.0" || !strings.Contains(doc.Schema, "sarif-2.1.0") {
		t.Errorf("schema/version = %q / %q", doc.Schema, doc.Version)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}
	run := doc.Runs[0]
	if run.Tool.Driver.Name != "aws_explorer" || run.Tool.Driver.SemanticVersion != "1.2.3" {
		t.Errorf("driver = %q %q", run.Tool.Driver.Name, run.Tool.Driver.SemanticVersion)
	}

	// Two distinct check IDs → two rules, deduplicated, with registry metadata.
	if len(run.Tool.Driver.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(run.Tool.Driver.Rules))
	}
	r0 := run.Tool.Driver.Rules[0]
	if r0.ID != CheckUnattachedVolume || r0.Name != "UnattachedEBSVolume" {
		t.Errorf("rule 0 = %q/%q", r0.ID, r0.Name)
	}
	if r0.ShortDescription.Text == "" || r0.DefaultConfiguration.Level != "warning" {
		t.Errorf("rule 0 metadata = %+v", r0)
	}

	if len(run.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(run.Results))
	}
	res0 := run.Results[0]
	if res0.RuleID != CheckUnattachedVolume || res0.RuleIndex != 0 || res0.Level != "warning" {
		t.Errorf("result 0 = %+v", res0)
	}
	if !strings.Contains(res0.Message.Text, "still bills") || !strings.Contains(res0.Message.Text, "Fix: delete it") {
		t.Errorf("message = %q", res0.Message.Text)
	}
	if got := res0.Locations[0].LogicalLocations[0].FullyQualifiedName; got != fs[0].ARN {
		t.Errorf("logical location = %q", got)
	}
	if got := res0.Properties["estMonthlyUSD"]; got != 102.40 {
		t.Errorf("estMonthlyUSD = %v", got)
	}
	// The NAT result references the second rule and sanitizes its URI.
	res2 := run.Results[2]
	if res2.RuleIndex != 1 {
		t.Errorf("NAT ruleIndex = %d, want 1", res2.RuleIndex)
	}
	if got := res2.Locations[0].PhysicalLocation.ArtifactLocation.URI; got != "aws/ec2/us-east-1/nat-01" {
		t.Errorf("artifact URI = %q", got)
	}
}

func TestRenderSARIFEmpty(t *testing.T) {
	doc := renderSARIFDoc(t, nil)
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}
	if doc.Runs[0].Results == nil || len(doc.Runs[0].Results) != 0 {
		t.Errorf("empty findings should yield an empty (non-null) results array")
	}
}

func TestRenderSARIFUnknownCheckID(t *testing.T) {
	// A finding with an unregistered ID still gets a synthesized rule.
	doc := renderSARIFDoc(t, []Finding{
		{ID: "X-FUTURE-001", Severity: SevCritical, Service: "s3", Region: "us-east-1",
			Resource: "bkt", Title: "Some future check"},
	})
	rules := doc.Runs[0].Tool.Driver.Rules
	if len(rules) != 1 || rules[0].ID != "X-FUTURE-001" {
		t.Fatalf("rules = %+v", rules)
	}
	if rules[0].ShortDescription.Text != "Some future check" {
		t.Errorf("synthesized description = %q", rules[0].ShortDescription.Text)
	}
	if doc.Runs[0].Results[0].Level != "error" {
		t.Errorf("critical should map to error, got %q", doc.Runs[0].Results[0].Level)
	}
}

func TestArtifactURI(t *testing.T) {
	cases := map[Finding]string{
		{Service: "ec2", Region: "us-east-1", Resource: "vol-1"}:          "aws/ec2/us-east-1/vol-1",
		{Service: "ec2", Region: "us-east-1", Resource: "nat-01 (spare)"}: "aws/ec2/us-east-1/nat-01",
		{Service: "ec2", Region: "us-east-1", Resource: ""}:               "aws/ec2/us-east-1/unknown",
	}
	for f, want := range cases {
		if got := artifactURI(f); got != want {
			t.Errorf("artifactURI(%q) = %q, want %q", f.Resource, got, want)
		}
	}
}

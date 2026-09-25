package lambdatui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func sectionTitled(secs []section, title string) (section, bool) {
	for _, s := range secs {
		if s.Title == title {
			return s, true
		}
	}
	return section{}, false
}

func TestFunctionSections(t *testing.T) {
	d := FunctionDetail{
		Name: "orders", Region: "us-east-1", ARN: "arn:fn:orders",
		Runtime: "python3.12", PackageType: "Zip", Handler: "app.handler",
		MemoryMB: 256, TimeoutSec: 30, Role: "arn:role",
		VpcID: "vpc-1", SubnetIDs: []string{"subnet-a"}, SecurityGroupIDs: []string{"sg-1"},
		Layers:    []string{"arn:layer:1"},
		EnvKeys:   []string{"DB_HOST", "API_BASE"},
		DLQTarget: "arn:aws:sqs:us-east-1:1:dl",
		Tags:      map[string]string{"team": "payments"},
	}
	secs := d.sections()

	// Each requested concept is its own panel.
	for _, want := range []string{"Configuration", "Triggers", "Versions & aliases", "Function URL", "Async invocation",
		"Permissions", "Networking", "Layers (1)", "Code package"} {
		if _, ok := sectionTitled(secs, want); !ok {
			t.Errorf("missing section %q (got %d sections)", want, len(secs))
		}
	}
	// The dead-letter queue is part of the async-invocation picture.
	if async, _ := sectionTitled(secs, "Async invocation"); !strings.Contains(async.Body, "sqs:dl") {
		t.Errorf("async panel should show the DLQ: %q", async.Body)
	}

	// Environment panel lists keys, with a count in the title, and never values.
	env, ok := sectionTitled(secs, "Environment (2)")
	if !ok {
		t.Fatalf("missing Environment section; titles=%v", titlesOf(secs))
	}
	if !strings.Contains(env.Body, "DB_HOST") || !strings.Contains(env.Body, "API_BASE") {
		t.Errorf("env body missing keys: %q", env.Body)
	}

	// Tags panel renders key=value with a count.
	tags, ok := sectionTitled(secs, "Tags (1)")
	if !ok {
		t.Fatalf("missing Tags section; titles=%v", titlesOf(secs))
	}
	if !strings.Contains(tags.Body, "team = payments") {
		t.Errorf("tags body = %q", tags.Body)
	}

	// VPC panel shows the attachment.
	vpc, _ := sectionTitled(secs, "Networking")
	if !strings.Contains(vpc.Body, "vpc-1") || !strings.Contains(vpc.Body, "sg-1") {
		t.Errorf("vpc body = %q", vpc.Body)
	}
}

func titlesOf(secs []section) []string {
	out := make([]string, len(secs))
	for i, s := range secs {
		out[i] = s.Title
	}
	return out
}

func TestResourcePolicyBody(t *testing.T) {
	// A real policy is pretty-printed (indented, multi-line) and syntax-
	// highlighted; strip the ANSI to assert on the text.
	pol := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"lambda:InvokeFunction"}]}`
	body := ansi.Strip(resourcePolicyBody(FunctionDetail{ResourcePolicy: pol}))
	if !strings.Contains(body, "\"Version\": \"2012-10-17\"") || !strings.Contains(body, "lambda:InvokeFunction") {
		t.Errorf("policy not pretty-printed:\n%s", body)
	}
	if !strings.Contains(body, "\n") {
		t.Errorf("expected multi-line JSON, got %q", body)
	}

	// No policy → an explanatory note, not blank.
	if got := resourcePolicyBody(FunctionDetail{}); !strings.Contains(got, "no resource-based policy") {
		t.Errorf("empty policy = %q", got)
	}

	// A read error (e.g. access denied) is surfaced and kept distinct from "none".
	denied := resourcePolicyBody(FunctionDetail{ResourcePolicyErr: "Access denied: not permitted to read the resource policy (lambda:GetPolicy)."})
	if !strings.Contains(denied, "Access denied") {
		t.Errorf("denied policy = %q", denied)
	}
}

func TestIndentJSONNonJSON(t *testing.T) {
	// Non-JSON input is shown as-is (trimmed), never dropped.
	if got := indentJSON("  not json  "); got != "  not json" {
		t.Errorf("non-JSON indentJSON = %q", got)
	}
}

func TestVPCBodyNotAttached(t *testing.T) {
	body := vpcBody(FunctionDetail{})
	if !strings.Contains(body, "Not attached to a VPC") {
		t.Errorf("expected not-attached note, got %q", body)
	}
}

func TestReservedConcurrencyLabel(t *testing.T) {
	if got := reservedConcurrencyLabel(nil); !strings.Contains(got, "unreserved") {
		t.Errorf("nil = %q", got)
	}
	zero := int32(0)
	if got := reservedConcurrencyLabel(&zero); !strings.Contains(got, "throttled") {
		t.Errorf("zero = %q", got)
	}
	five := int32(5)
	if got := reservedConcurrencyLabel(&five); got != "5" {
		t.Errorf("five = %q", got)
	}
}

func TestEnvBodyEmpty(t *testing.T) {
	if got := envBody(FunctionDetail{}); !strings.Contains(got, "no environment variables") {
		t.Errorf("empty env = %q", got)
	}
}

func TestLayerSections(t *testing.T) {
	secs := layerSections(Layer{Name: "deps", LatestVersion: 7, Runtimes: []string{"python3.12"}})
	if _, ok := sectionTitled(secs, "Compatibility"); !ok {
		t.Errorf("layer sections missing Compatibility: %v", titlesOf(secs))
	}
}

func TestEventSourceSections(t *testing.T) {
	secs := eventSourceSections(EventSource{FunctionName: "orders", SourceLabel: "sqs:q", State: "Enabled", BatchSize: 10, LastModified: time.Now()})
	if _, ok := sectionTitled(secs, "Processing"); !ok {
		t.Errorf("event-source sections missing Processing: %v", titlesOf(secs))
	}
}

func TestSplitWidth(t *testing.T) {
	w := splitWidth(100, 3)
	if len(w) != 3 || w[0] != 32 || w[0]+w[1]+w[2]+2*panelGap != 100 {
		t.Errorf("splitWidth(100,3) = %v", w)
	}
}

func TestDetailColCount(t *testing.T) {
	if got := detailColCount(9, 180); got != 3 {
		t.Errorf("wide = %d, want 3", got)
	}
	if got := detailColCount(9, 120); got != 2 {
		t.Errorf("medium = %d, want 2", got)
	}
	if got := detailColCount(9, 80); got != 1 {
		t.Errorf("narrow = %d, want 1", got)
	}
	// Never more columns than sections.
	if got := detailColCount(2, 180); got != 2 {
		t.Errorf("few sections = %d, want 2", got)
	}
}

func TestEnvKeysAreSortedKeysOnly(t *testing.T) {
	// The env panel must carry keys only — never values.
	keys := sortedMapKeys(map[string]string{"SECRET": "shh", "REGION": "x"})
	if len(keys) != 2 || keys[0] != "REGION" || keys[1] != "SECRET" {
		t.Fatalf("keys = %v", keys)
	}
	for _, k := range keys {
		if k == "shh" || k == "x" {
			t.Errorf("a value leaked into the keys: %v", keys)
		}
	}
}

func TestCardColCount(t *testing.T) {
	for _, c := range []struct{ w, want int }{{190, 3}, {120, 2}, {98, 2}, {80, 1}} {
		if got := cardColCount(3, c.w); got != c.want {
			t.Errorf("cardColCount(3, %d) = %d, want %d", c.w, got, c.want)
		}
	}
}

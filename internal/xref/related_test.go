package xref

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestRelated_ShowAllPaths(t *testing.T) {
	// Diamond: A reaches D via two distinct two-hop paths (A→B→D and A→C→D).
	a := "arn:aws:iam::1:role/a"
	b := "arn:aws:iam::1:role/b"
	c := "arn:aws:iam::1:role/c"
	d := "arn:aws:iam::1:role/d"
	// Both routes reach D via the *same* final relationship ("p"); only the
	// upstream path differs. Shortest mode collapses them (resource + immediate
	// relationship match); --show-paths all keeps both (full path differs).
	edges := []Edge{
		{From: Reference{Service: "iam", Type: "role", ID: a, Via: "x"}, Target: b},
		{From: Reference{Service: "iam", Type: "role", ID: a, Via: "y"}, Target: c},
		{From: Reference{Service: "iam", Type: "role", ID: b, Via: "p"}, Target: d},
		{From: Reference{Service: "iam", Type: "role", ID: c, Via: "p"}, Target: d},
	}
	fwd, rev := BuildForwardIndex(edges), BuildIndex(edges)
	countD := func(links []Link) int {
		n := 0
		for _, l := range links {
			if l.ID == d {
				n++
			}
		}
		return n
	}

	shortest := Related(a, fwd, rev, 2, false)
	if got := countD(shortest.Uses); got != 1 {
		t.Errorf("shortest: D should appear once, got %d: %+v", got, shortest.Uses)
	}
	if shortest.AllPaths {
		t.Errorf("shortest result should not set AllPaths")
	}

	all := Related(a, fwd, rev, 2, true)
	if got := countD(all.Uses); got != 2 {
		t.Errorf("all-paths: D should appear twice, got %d: %+v", got, all.Uses)
	}
	if !all.AllPaths {
		t.Errorf("all-paths result should set AllPaths")
	}
}

func TestRenderRelated_CaveatPrintedOnce(t *testing.T) {
	res := relatedOver(roleARN, 1)
	var sb strings.Builder
	if err := RenderRelated(&sb, res, "table", false, true, true, false); err != nil {
		t.Fatalf("table: %v", err)
	}
	if got := strings.Count(sb.String(), relatedCaveat); got != 1 {
		t.Errorf("caveat should print exactly once when both directions shown, got %d:\n%s", got, sb.String())
	}

	// Single-direction output still prints it exactly once.
	sb.Reset()
	if err := RenderRelated(&sb, res, "table", false, true, false, false); err != nil {
		t.Fatalf("table uses-only: %v", err)
	}
	if got := strings.Count(sb.String(), relatedCaveat); got != 1 {
		t.Errorf("uses-only: caveat count = %d, want 1", got)
	}
}

func TestAmbiguousCandidates(t *testing.T) {
	roleA := "arn:aws:iam::111111111111:role/team-a/app"
	roleB := "arn:aws:iam::111111111111:role/team-b/app"
	edges := []Edge{
		{From: Reference{Service: "lambda", ID: lambdaARN, Via: "execution role"}, Target: roleA},
		{From: Reference{Service: "ecs", ID: tdARN, Via: "ECS task role"}, Target: roleB},
		{From: Reference{Service: "iam", ID: roleA, Via: "trust policy principal"}, Target: srcRole},
	}

	// Bare name "app" collides across two distinct role ARNs.
	got := AmbiguousCandidates("app", edges)
	if len(got) != 2 || got[0] != roleA || got[1] != roleB {
		t.Errorf("AmbiguousCandidates(app) = %v, want [%s %s]", got, roleA, roleB)
	}

	// A full ARN is never ambiguous.
	if got := AmbiguousCandidates(roleA, edges); got != nil {
		t.Errorf("full ARN should not be ambiguous, got %v", got)
	}

	// A name matching only one resource is unambiguous.
	if got := AmbiguousCandidates("source", edges); got != nil {
		t.Errorf("unique name should not be ambiguous, got %v", got)
	}

	// Empty input is a no-op.
	if got := AmbiguousCandidates("", edges); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}

func TestClassifyShortID(t *testing.T) {
	cases := []struct {
		id           string
		service, typ string
		ok           bool
	}{
		{"sg-0abc", "ec2", "security-group", true},
		{"subnet-0abc", "ec2", "subnet", true},
		{"vpc-0abc", "ec2", "vpc", true},
		{"eni-0abc", "ec2", "network-interface", true},
		{"vol-0abc", "ec2", "volume", true},
		{"vpce-0abc", "ec2", "vpc-endpoint", true},
		{"i-0abc", "ec2", "instance", true},
		{"igw-0abc", "ec2", "internet-gateway", true},
		{"/aws/lambda/checkout", "logs", "log-group", true},
		{"my-subnet-group", "", "", false},  // RDS subnet-group name: unclassified
		{"web.example.com.", "", "", false}, // DNS name: unclassified
	}
	for _, c := range cases {
		svc, typ, ok := classifyShortID(c.id)
		if svc != c.service || typ != c.typ || ok != c.ok {
			t.Errorf("classifyShortID(%q) = (%q,%q,%v), want (%q,%q,%v)", c.id, svc, typ, ok, c.service, c.typ, c.ok)
		}
	}
}

func TestTargetReference_InheritsRegionForShortIDs(t *testing.T) {
	from := Reference{Service: "lambda", Type: "function", Region: "ap-southeast-4", ID: lambdaARN, Via: "VPC subnet"}

	// A regional EC2 short-id target inherits the referencing resource's region.
	got := targetReference(Edge{From: from, Target: "subnet-0abc123"})
	if got.Region != "ap-southeast-4" || got.Service != "ec2" || got.Type != "subnet" {
		t.Errorf("subnet target = %+v, want region ap-southeast-4 / ec2 / subnet", got)
	}

	// A bare name (e.g. a DB subnet-group) stays honestly region-less.
	got = targetReference(Edge{From: Reference{Region: "ap-southeast-4", Via: "DB subnet group"}, Target: "prod-subnets"})
	if got.Region != "" || got.Service != "" {
		t.Errorf("name target should stay unclassified/region-less, got %+v", got)
	}

	// An ARN target keeps its own region, not the source's.
	got = targetReference(Edge{From: from, Target: "arn:aws:kms:us-east-1:111:key/abc"})
	if got.Region != "us-east-1" {
		t.Errorf("ARN target region = %q, want us-east-1", got.Region)
	}
}

func TestRelatedResult_WithCollectionStatus(t *testing.T) {
	base := relatedOver(roleARN, 1)
	if base.Partial || base.Errors != nil {
		t.Fatalf("fresh result should not be partial: %+v", base)
	}
	withErrs := base.WithCollectionStatus([]model.ExploreError{
		{Service: "iam", Region: "global", Code: "Throttling", Message: "rate exceeded"},
	})
	if !withErrs.Partial || len(withErrs.Errors) != 1 {
		t.Errorf("expected partial with 1 error, got %+v", withErrs)
	}
	// No errors → not partial.
	if got := base.WithCollectionStatus(nil); got.Partial {
		t.Errorf("nil errors must not mark partial: %+v", got)
	}
}

func TestRenderRelated_JSONCarriesPartialStatus(t *testing.T) {
	res := relatedOver(roleARN, 1).WithCollectionStatus([]model.ExploreError{
		{Service: "iam", Region: "global", Code: "Throttling", Message: "rate exceeded"},
	})
	var sb strings.Builder
	if err := RenderRelated(&sb, res, "json", false, true, true, res.Partial); err != nil {
		t.Fatalf("json: %v", err)
	}
	out := sb.String()
	for _, want := range []string{`"partial": true`, `"errors"`, "rate exceeded"} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q:\n%s", want, out)
		}
	}

	// A clean result must report partial:false and omit the errors array.
	sb.Reset()
	clean := relatedOver(roleARN, 1).WithCollectionStatus(nil)
	if err := RenderRelated(&sb, clean, "json", false, true, true, clean.Partial); err != nil {
		t.Fatalf("json clean: %v", err)
	}
	if !strings.Contains(sb.String(), `"partial": false`) || strings.Contains(sb.String(), `"errors"`) {
		t.Errorf("clean json should be partial:false with no errors array:\n%s", sb.String())
	}
}

const (
	lambdaARN = "arn:aws:lambda:us-east-1:111111111111:function:checkout"
	roleARN   = "arn:aws:iam::111111111111:role/app"
	srcRole   = "arn:aws:iam::111111111111:role/source"
	tdARN     = "arn:aws:ecs:us-east-1:111111111111:task-definition/web:3"
)

func sampleEdges() []Edge {
	return []Edge{
		{From: Reference{Service: "lambda", Type: "function", Region: "us-east-1", ID: lambdaARN, Name: "checkout", Via: "execution role"}, Target: roleARN},
		{From: Reference{Service: "iam", Type: "role", Region: "global", ID: roleARN, Name: "app", Via: "trust policy principal"}, Target: srcRole},
		{From: Reference{Service: "ecs", Type: "task-definition", Region: "us-east-1", ID: tdARN, Name: "web", Via: "ECS task role"}, Target: roleARN},
	}
}

func relatedOver(input string, depth int) RelatedResult {
	edges := sampleEdges()
	return Related(input, BuildForwardIndex(edges), BuildIndex(edges), depth, false)
}

func TestRelated_BothDirections(t *testing.T) {
	res := relatedOver(roleARN, 1)

	// Uses: the role's trust policy names the source role.
	if len(res.Uses) != 1 {
		t.Fatalf("uses: want 1, got %d: %+v", len(res.Uses), res.Uses)
	}
	if res.Uses[0].ID != srcRole || res.Uses[0].Service != "iam" || res.Uses[0].Type != "role" {
		t.Errorf("uses[0] = %+v", res.Uses[0])
	}
	if res.Uses[0].Via != "trust policy principal" {
		t.Errorf("uses[0].Via = %q", res.Uses[0].Via)
	}

	// Used by: the Lambda (execution role) and the ECS task definition (task role).
	if len(res.UsedBy) != 2 {
		t.Fatalf("usedby: want 2, got %d: %+v", len(res.UsedBy), res.UsedBy)
	}
	vias := res.UsedBy[0].Via + "," + res.UsedBy[1].Via
	if !strings.Contains(vias, "execution role") || !strings.Contains(vias, "ECS task role") {
		t.Errorf("usedby vias = %q", vias)
	}
}

func TestRelated_ForwardDerivesTargetFields(t *testing.T) {
	// The forward link is built from the target ARN string alone.
	res := relatedOver(lambdaARN, 1)
	if len(res.Uses) != 1 {
		t.Fatalf("uses: want 1, got %d", len(res.Uses))
	}
	u := res.Uses[0]
	if u.Service != "iam" || u.Type != "role" || u.Name != "app" || u.ID != roleARN {
		t.Errorf("derived target = %+v", u)
	}
	// Lambda is referenced by nothing here.
	if len(res.UsedBy) != 0 {
		t.Errorf("usedby: want 0, got %d", len(res.UsedBy))
	}
}

func TestRelated_MultiHopForward(t *testing.T) {
	res := relatedOver(lambdaARN, 2)
	// hop1: role/app (execution role); hop2: role/source via the chained path.
	if len(res.Uses) != 2 {
		t.Fatalf("uses: want 2, got %d: %+v", len(res.Uses), res.Uses)
	}
	var hop2 *Link
	for i := range res.Uses {
		if res.Uses[i].ID == srcRole {
			hop2 = &res.Uses[i]
		}
	}
	if hop2 == nil {
		t.Fatalf("source role not reached at depth 2: %+v", res.Uses)
	}
	if hop2.Depth != 2 {
		t.Errorf("hop2 depth = %d, want 2", hop2.Depth)
	}
	if hop2.Path != "execution role ▸ trust policy principal" {
		t.Errorf("hop2 path = %q", hop2.Path)
	}
}

func TestRelated_CycleGuard(t *testing.T) {
	a := "arn:aws:iam::1:role/a"
	b := "arn:aws:iam::1:role/b"
	edges := []Edge{
		{From: Reference{Service: "iam", Type: "role", ID: a, Name: "a", Via: "trust policy principal"}, Target: b},
		{From: Reference{Service: "iam", Type: "role", ID: b, Name: "b", Via: "trust policy principal"}, Target: a},
	}
	res := Related(a, BuildForwardIndex(edges), BuildIndex(edges), 5, false)
	// Must terminate and must NOT list the queried resource (a) back to itself
	// via the cycle (#389): only b is related to a here.
	if len(res.Uses) != 1 {
		t.Fatalf("uses: want 1 (b only; a is the start), got %d: %+v", len(res.Uses), res.Uses)
	}
	if res.Uses[0].ID != b {
		t.Errorf("uses[0] = %q, want %q", res.Uses[0].ID, b)
	}
	for _, l := range res.Uses {
		if l.ID == a {
			t.Errorf("start node a must not appear as its own related row: %+v", l)
		}
	}
}

func TestRelated_EmptyIsScoped(t *testing.T) {
	res := relatedOver("arn:aws:iam::111111111111:role/lonely", 1)
	if len(res.Uses) != 0 || len(res.UsedBy) != 0 {
		t.Fatalf("lonely role should have no links: %+v", res)
	}
	// Recognized as an IAM role → reverse CheckedTypes populated.
	if len(res.CheckedTypes) == 0 {
		t.Errorf("CheckedTypes must scope the empty 'used by' answer")
	}
}

func TestArnHelpers(t *testing.T) {
	cases := map[string]struct{ service, typ, region string }{
		lambdaARN:                          {"lambda", "function", "us-east-1"},
		roleARN:                            {"iam", "role", ""},
		tdARN:                              {"ecs", "task-definition", "us-east-1"},
		"arn:aws:sqs:eu-west-1:1:my-queue": {"sqs", "", "eu-west-1"}, // no type segment
	}
	for arn, want := range cases {
		if got := arnService(arn); got != want.service {
			t.Errorf("arnService(%q) = %q, want %q", arn, got, want.service)
		}
		if got := arnResourceType(arn); got != want.typ {
			t.Errorf("arnResourceType(%q) = %q, want %q", arn, got, want.typ)
		}
		if got := arnRegion(arn); got != want.region {
			t.Errorf("arnRegion(%q) = %q, want %q", arn, got, want.region)
		}
	}
}

func TestRenderRelated_Table(t *testing.T) {
	res := relatedOver(roleARN, 1)
	var sb strings.Builder
	if err := RenderRelated(&sb, res, "table", false, true, true, false); err != nil {
		t.Fatalf("table: %v", err)
	}
	out := sb.String()
	for _, want := range []string{"Uses (depends on) →", "Used by ←", "execution role", relatedCaveat, "Reference types checked"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	// Direction filter: only the forward section.
	sb.Reset()
	if err := RenderRelated(&sb, res, "table", false, true, false, false); err != nil {
		t.Fatalf("table uses-only: %v", err)
	}
	if strings.Contains(sb.String(), "Used by ←") {
		t.Errorf("uses-only output should not contain the used-by section:\n%s", sb.String())
	}
}

func TestRenderRelated_PartialFlagsEmptySection(t *testing.T) {
	empty := RelatedResult{Target: Classify("arn:aws:iam::1:role/lonely"), Depth: 1}

	var sb strings.Builder
	if err := RenderRelated(&sb, empty, "table", false, true, true, true); err != nil {
		t.Fatalf("table: %v", err)
	}
	if !strings.Contains(sb.String(), "result may be incomplete") {
		t.Errorf("partial empty section should flag incompleteness:\n%s", sb.String())
	}

	sb.Reset()
	if err := RenderRelated(&sb, empty, "table", false, true, true, false); err != nil {
		t.Fatalf("table: %v", err)
	}
	if strings.Contains(sb.String(), "result may be incomplete") {
		t.Errorf("non-partial empty section must not claim incompleteness:\n%s", sb.String())
	}
}

func TestRenderRelated_UnknownTargetNote(t *testing.T) {
	res := RelatedResult{Target: Classify("vpc-0475013d0d9249369"), Depth: 1}
	var sb strings.Builder
	if err := RenderRelated(&sb, res, "table", false, true, true, false); err != nil {
		t.Fatalf("table: %v", err)
	}
	if !strings.Contains(sb.String(), "aws_explorer vpc") {
		t.Errorf("vpc- target should point at the vpc command:\n%s", sb.String())
	}
}

func TestRenderRelated_JSONAndNDJSON(t *testing.T) {
	res := relatedOver(roleARN, 1)

	var sb strings.Builder
	if err := RenderRelated(&sb, res, "json", false, true, true, false); err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, want := range []string{`"uses"`, `"used_by"`, `"checked_types"`, srcRole} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("json missing %q:\n%s", want, sb.String())
		}
	}

	sb.Reset()
	if err := RenderRelated(&sb, res, "ndjson", false, true, true, false); err != nil {
		t.Fatalf("ndjson: %v", err)
	}
	if !strings.Contains(sb.String(), `"direction":"uses"`) || !strings.Contains(sb.String(), `"direction":"used_by"`) {
		t.Errorf("ndjson missing direction tags:\n%s", sb.String())
	}
}

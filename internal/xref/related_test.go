package xref

import (
	"strings"
	"testing"
)

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
	return Related(input, BuildForwardIndex(edges), BuildIndex(edges), depth)
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
	res := Related(a, BuildForwardIndex(edges), BuildIndex(edges), 5)
	// Must terminate; b at depth 1, a at depth 2 — and no further (a already visited).
	if len(res.Uses) != 2 {
		t.Fatalf("uses: want 2 (b, then a), got %d: %+v", len(res.Uses), res.Uses)
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

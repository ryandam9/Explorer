package xref

import (
	"bytes"
	"strings"
	"testing"
)

func treeFixture() RelatedResult {
	return RelatedResult{
		Target: Target{Input: "arn:aws:iam::123456789012:role/app-role",
			ARN: "arn:aws:iam::123456789012:role/app-role", Kind: KindIAMRole},
		Depth: 2,
		Uses: []Link{
			{Reference: Reference{Service: "kms", Type: "key", ID: "abcd", Name: "alias/app",
				Region: "ap-southeast-2", Via: "role policy"}, Depth: 1, Path: "role policy"},
			{Reference: Reference{Service: "s3", Type: "bucket", ID: "app-data",
				Region: "ap-southeast-2", Via: "bucket policy"}, Depth: 2,
				Path: "role policy → bucket policy"},
		},
		UsedBy: []Link{
			{Reference: Reference{Service: "lambda", Type: "function", ID: "checkout",
				Region: "ap-southeast-2", Via: "execution role"}, Depth: 1},
		},
		CheckedTypes: []string{"Lambda execution roles"},
	}
}

// The tree exists to show the hop structure a table can only put in a column,
// so the hops must be branches — and named, since "2" alone means nothing
// without a column header above it.
func TestRelatedTreeGroupsByHop(t *testing.T) {
	var b bytes.Buffer
	if err := relatedTree(&b, treeFixture(), true, true, false); err != nil {
		t.Fatalf("relatedTree: %v", err)
	}
	out := b.String()

	for _, want := range []string{
		"arn:aws:iam::123456789012:role/app-role  (IAM role)", // the root is the target
		"Depends on →  (2)", // each direction counts its links
		"Used by ←  (1)",
		"1 hop away  (1)", // …and groups them by distance, in words
		"2 hops away  (1)",
		"kms key  alias/app  [ap-southeast-2]  — role policy",
		"s3 bucket  app-data",
		"lambda function  checkout",
		"├──", "└──", // drawn as a tree, not an indented list
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tree missing %q:\n%s", want, out)
		}
	}

	// The honesty notes the table carries are not dropped by the new format.
	if !strings.Contains(out, relatedCaveat) {
		t.Error("the caveat about which link types were checked is missing")
	}
	if !strings.Contains(out, "Lambda execution roles") {
		t.Error("the searched-for link kinds are missing")
	}
}

// One hop deep, there is nothing to group — the links hang straight off the
// direction rather than under a pointless "1 hop away".
func TestRelatedTreeSkipsHopGroupsAtDepthOne(t *testing.T) {
	res := treeFixture()
	res.Depth = 1
	res.Uses = res.Uses[:1]
	res.UsedBy = nil

	var b bytes.Buffer
	if err := relatedTree(&b, res, true, false, false); err != nil {
		t.Fatalf("relatedTree: %v", err)
	}
	if strings.Contains(b.String(), "hop away") {
		t.Errorf("a single-hop query should not show hop groups:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "kms key  alias/app") {
		t.Errorf("the link is missing:\n%s", b.String())
	}
}

// An empty side says whether the scan was clean, exactly as the table does —
// "nothing found" and "nothing found, but the scan hit errors" are different
// answers (§6a/§8).
func TestRelatedTreeDistinguishesEmptyFromPartial(t *testing.T) {
	empty := RelatedResult{Target: Target{Input: "sg-x", Kind: KindSecurityGroup}, Depth: 1}

	var clean, partial bytes.Buffer
	_ = relatedTree(&clean, empty, true, true, false)
	_ = relatedTree(&partial, empty, true, true, true)

	if !strings.Contains(clean.String(), "(nothing found)") {
		t.Errorf("clean empty result:\n%s", clean.String())
	}
	if strings.Contains(clean.String(), "may be incomplete") {
		t.Error("a clean scan must not claim it may be incomplete")
	}
	if !strings.Contains(partial.String(), "may be incomplete") {
		t.Errorf("a partial scan must say so:\n%s", partial.String())
	}
}

// --show-paths swaps the single relationship for the whole chain, as in the
// table, so two routes to the same resource are distinguishable.
func TestRelatedTreeShowsPaths(t *testing.T) {
	res := treeFixture()
	res.AllPaths = true
	var b bytes.Buffer
	_ = relatedTree(&b, res, true, false, false)
	if !strings.Contains(b.String(), "role policy → bucket policy") {
		t.Errorf("the full path is missing:\n%s", b.String())
	}
}

// Only the directions asked for are rendered.
func TestRelatedTreeHonoursDirection(t *testing.T) {
	var b bytes.Buffer
	_ = relatedTree(&b, treeFixture(), false, true, false)
	out := b.String()
	if strings.Contains(out, "Depends on") {
		t.Errorf("--direction usedby still printed the uses side:\n%s", out)
	}
	if !strings.Contains(out, "Used by") {
		t.Errorf("the requested direction is missing:\n%s", out)
	}
}

// where-used groups by service, which is what "can I delete this?" is really
// asking: which parts of the estate would notice.
func TestWhereUsedTreeGroupsByService(t *testing.T) {
	var b bytes.Buffer
	err := whereUsedTree(&b, Result{
		Target: Target{Input: "sg-0abc123", Kind: KindSecurityGroup},
		References: []Reference{
			{Service: "ec2", Type: "instance", ID: "i-0123", Region: "ap-southeast-2", Via: "attached SG"},
			{Service: "rds", Type: "db-instance", ID: "orders", Region: "ap-southeast-2", Via: "vpc security group"},
			{Service: "ec2", Type: "eni", ID: "eni-9", Region: "ap-southeast-2", Via: "attached SG"},
		},
		CheckedTypes: []string{"EC2 instances"},
	})
	if err != nil {
		t.Fatalf("whereUsedTree: %v", err)
	}
	out := b.String()

	for _, want := range []string{
		"sg-0abc123", "ec2  (2)", "rds  (1)",
		"instance  i-0123", "eni  eni-9", "db-instance  orders",
		"EC2 instances", // the checked-link note survives
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tree missing %q:\n%s", want, out)
		}
	}
	// Services are ordered, so two runs of the same data read the same.
	if strings.Index(out, "ec2  (2)") > strings.Index(out, "rds  (1)") {
		t.Errorf("services should be in a stable order:\n%s", out)
	}
}

func TestWhereUsedTreeEmpty(t *testing.T) {
	var b bytes.Buffer
	_ = whereUsedTree(&b, Result{Target: Target{Input: "sg-x", Kind: KindSecurityGroup}})
	if !strings.Contains(b.String(), "Not referenced by anything") {
		t.Errorf("an unreferenced target should say so:\n%s", b.String())
	}
}

// The tree goes to stdout, which is as often piped as read, so it carries no
// colour — only the box-drawing characters.
func TestTreeOutputCarriesNoANSI(t *testing.T) {
	var b bytes.Buffer
	_ = relatedTree(&b, treeFixture(), true, true, false)
	if strings.Contains(b.String(), "\x1b[") {
		t.Errorf("tree output contains ANSI escapes:\n%q", b.String())
	}
}

// Both commands reach the new renderer through their normal entry point.
func TestTreeFormatIsRoutable(t *testing.T) {
	var b bytes.Buffer
	if err := RenderRelated(&b, treeFixture(), "tree", false, true, true, false); err != nil {
		t.Fatalf("RenderRelated tree: %v", err)
	}
	if !strings.Contains(b.String(), "├──") {
		t.Errorf("-o tree did not render a tree:\n%s", b.String())
	}

	b.Reset()
	if err := Render(&b, Result{Target: Target{Input: "sg-1", Kind: KindSecurityGroup},
		References: []Reference{{Service: "ec2", Type: "instance", ID: "i-1"}}}, "tree", false); err != nil {
		t.Fatalf("Render tree: %v", err)
	}
	if !strings.Contains(b.String(), "└──") {
		t.Errorf("-o tree did not render a tree:\n%s", b.String())
	}
}

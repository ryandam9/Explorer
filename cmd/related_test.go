package cmd

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/xref"
)

func TestRelatedTUIFlagError(t *testing.T) {
	cases := []struct {
		name                  string
		tui, depth, direction bool
		wantErr               bool
	}{
		{"non-tui ignores both", false, true, true, false},
		{"tui, no flags set", true, false, false, false},
		{"tui with --depth", true, true, false, true},
		{"tui with --direction", true, false, true, true},
		{"tui with both", true, true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := relatedTUIFlagError(c.tui, c.depth, c.direction)
			if (err != nil) != c.wantErr {
				t.Errorf("relatedTUIFlagError(%v,%v,%v) err=%v, wantErr=%v",
					c.tui, c.depth, c.direction, err, c.wantErr)
			}
		})
	}
}

func TestArnRegionField(t *testing.T) {
	cases := map[string]string{
		"arn:aws:lambda:us-east-2:111:function:f": "us-east-2",
		"arn:aws:iam::111:role/app":               "", // global → empty region field
		"sg-0abc123":                              "", // not an ARN
		"my-role":                                 "",
	}
	for in, want := range cases {
		if got := arnRegionField(in); got != want {
			t.Errorf("arnRegionField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTimeoutHint(t *testing.T) {
	deadline := []model.ExploreError{{Service: "logs", Region: "sa-east-1", Code: "CollectionError", Message: "operation error CloudWatch Logs: DescribeLogGroups, context deadline exceeded"}}
	denied := []model.ExploreError{{Service: "s3", Region: "us-east-1", Code: "AccessDenied", Message: "not authorized"}}

	// Timeout present, multi-region → full hint incl. the -r suggestion.
	h := timeoutHint(deadline, "us-east-2", 18)
	for _, want := range []string{"timed out", "-r us-east-2", "--scan eventing", "--debug-scan"} {
		if !strings.Contains(h, want) {
			t.Errorf("hint missing %q:\n%s", want, h)
		}
	}

	// Single-region scan → no -r suggestion (already scoped).
	if h := timeoutHint(deadline, "us-east-2", 1); strings.Contains(h, "-r ") {
		t.Errorf("single-region hint should omit -r:\n%s", h)
	}

	// No timeout errors → no hint.
	if h := timeoutHint(denied, "us-east-1", 18); h != "" {
		t.Errorf("non-timeout errors should produce no hint, got:\n%s", h)
	}
	if h := timeoutHint(nil, "", 18); h != "" {
		t.Errorf("no errors should produce no hint, got:\n%s", h)
	}
}

func TestExplainScan(t *testing.T) {
	// A KMS key lists its encryption-relationship reference types.
	var sb strings.Builder
	kms := "arn:aws:kms:us-east-1:111:key/abc"
	if err := explainScan(&sb, kms, xref.Classify(kms)); err != nil {
		t.Fatalf("explainScan: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "kms-key") || !strings.Contains(out, "EBS volume encryption") {
		t.Errorf("KMS explain missing expected content:\n%s", out)
	}

	// An unrecognized target explains it has no scoped list.
	sb.Reset()
	vpc := "vpc-0475013d0d9249369"
	if err := explainScan(&sb, vpc, xref.Classify(vpc)); err != nil {
		t.Fatalf("explainScan unknown: %v", err)
	}
	if !strings.Contains(sb.String(), "raw graph links") {
		t.Errorf("unknown explain should note no scoped list:\n%s", sb.String())
	}
}

func TestRelatedOutputFormat(t *testing.T) {
	// --format unset → fall back to -o.
	if got, err := relatedOutputFormat("json", ""); err != nil || got != "json" {
		t.Errorf("unset --format should use -o: got %q, %v", got, err)
	}
	// --format graph dialects override -o.
	for _, f := range []string{"dot", "mermaid", "DOT", "Mermaid"} {
		got, err := relatedOutputFormat("table", f)
		if err != nil || (got != "dot" && got != "mermaid") {
			t.Errorf("relatedOutputFormat(table, %q) = %q, %v", f, got, err)
		}
	}
	// Invalid --format is rejected.
	if _, err := relatedOutputFormat("table", "png"); err == nil {
		t.Errorf("invalid --format should error")
	}
}

func TestParseDepth(t *testing.T) {
	cases := []struct {
		in      int
		want    int
		wantErr bool
	}{
		{0, 1, false},  // floored to one hop
		{-3, 1, false}, // floored
		{1, 1, false},
		{relatedMaxDepth, relatedMaxDepth, false},
		{relatedMaxDepth + 1, 0, true}, // too deep
	}
	for _, c := range cases {
		got, err := parseDepth(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseDepth(%d) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
		if err == nil && got != c.want {
			t.Errorf("parseDepth(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseShowPaths(t *testing.T) {
	cases := []struct {
		in      string
		wantAll bool
		wantErr bool
	}{
		{"", false, false},
		{"shortest", false, false},
		{"all", true, false},
		{"ALL", true, false},
		{"bogus", false, true},
	}
	for _, c := range cases {
		all, err := parseShowPaths(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseShowPaths(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
		if err == nil && all != c.wantAll {
			t.Errorf("parseShowPaths(%q) = %v, want %v", c.in, all, c.wantAll)
		}
	}
}

func TestParseDirection(t *testing.T) {
	cases := []struct {
		in                   string
		wantUses, wantUsedBy bool
		wantErr              bool
	}{
		{"", true, true, false},
		{"both", true, true, false},
		{"uses", true, false, false},
		{"forward", true, false, false},
		{"usedby", false, true, false},
		{"used-by", false, true, false},
		{"reverse", false, true, false},
		{"USES", true, false, false}, // case-insensitive
		{"bogus", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			uses, usedBy, err := parseDirection(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseDirection(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			}
			if err != nil {
				return
			}
			if uses != c.wantUses || usedBy != c.wantUsedBy {
				t.Errorf("parseDirection(%q) = (%v,%v), want (%v,%v)", c.in, uses, usedBy, c.wantUses, c.wantUsedBy)
			}
		})
	}
}

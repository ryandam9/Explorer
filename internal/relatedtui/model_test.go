package relatedtui

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/xref"
)

const (
	lamARN  = "arn:aws:lambda:us-east-1:111:function:f"
	roleARN = "arn:aws:iam::111:role/app"
	ecsARN  = "arn:aws:ecs:us-east-1:111:task-definition/web:1"
)

func newTestModel(center string) *m {
	edges := []xref.Edge{
		{From: xref.Reference{Service: "lambda", Type: "function", Region: "us-east-1", ID: lamARN, Name: "f", Via: "execution role"}, Target: roleARN},
		{From: xref.Reference{Service: "ecs", Type: "task-definition", Region: "us-east-1", ID: ecsARN, Name: "web", Via: "ECS task role"}, Target: roleARN},
	}
	return &m{
		stack:     []string{center},
		fwd:       xref.BuildForwardIndex(edges),
		rev:       xref.BuildIndex(edges),
		usesTbl:   newResTable(),
		usedByTbl: newResTable(),
	}
}

func TestRecompute(t *testing.T) {
	mm := newTestModel(lamARN)
	mm.recompute()
	if len(mm.result.Uses) != 1 || mm.result.Uses[0].Service != "iam" {
		t.Fatalf("lambda.Uses = %+v", mm.result.Uses)
	}
	if len(mm.result.UsedBy) != 0 {
		t.Errorf("lambda.UsedBy = %+v", mm.result.UsedBy)
	}
}

func TestFilterLinks(t *testing.T) {
	links := []xref.Link{
		{Reference: xref.Reference{Service: "lambda", Type: "function", Name: "checkout", Via: "execution role"}},
		{Reference: xref.Reference{Service: "iam", Type: "role", Name: "app", Via: "trust policy principal"}},
		{Reference: xref.Reference{Service: "ec2", Type: "subnet", ID: "subnet-0abc", Region: "us-east-1", Via: "subnet"}},
	}
	cases := []struct {
		q    string
		want int
	}{
		{"", 3},            // empty → all
		{"lambda", 1},      // service
		{"ROLE", 2},        // case-insensitive: "role" in type + in "execution role"
		{"subnet-0abc", 1}, // ID match
		{"us-east-1", 1},   // region match
		{"zzz", 0},         // no match
	}
	for _, c := range cases {
		if got := filterLinks(links, c.q); len(got) != c.want {
			t.Errorf("filterLinks(%q) = %d rows, want %d", c.q, len(got), c.want)
		}
	}
}

func TestApplyFilter_AlignsSelection(t *testing.T) {
	mm := newTestModel(roleARN)
	mm.recompute() // role is used by lambda + ecs

	mm.focus = paneUsedBy
	mm.filter = "ecs"
	mm.applyFilter()

	if len(mm.viewUsedBy) != 1 {
		t.Fatalf("filtered used-by = %d, want 1", len(mm.viewUsedBy))
	}
	// selected() must index the filtered view, not the raw result.
	l, ok := mm.selected()
	if !ok || l.Service != "ecs" {
		t.Errorf("selected after filter = %+v (ok=%v), want the ecs row", l, ok)
	}
}

func TestDescendAndBack(t *testing.T) {
	mm := newTestModel(lamARN)
	mm.recompute()

	// Focus the Uses pane (cursor 0 = the role) and drill in.
	mm.focus = paneUses
	mm.descend()
	if mm.current() != roleARN {
		t.Fatalf("after descend, current = %q, want role", mm.current())
	}
	// The role is used by both the Lambda and the ECS task definition.
	if len(mm.result.UsedBy) != 2 {
		t.Fatalf("role.UsedBy = %+v", mm.result.UsedBy)
	}
	if len(mm.result.Uses) != 0 {
		t.Errorf("role.Uses = %+v", mm.result.Uses)
	}

	// Breadcrumb back to the Lambda.
	mm.back()
	if mm.current() != lamARN {
		t.Errorf("after back, current = %q, want lambda", mm.current())
	}
}

func TestBack_AtRootIsNoop(t *testing.T) {
	mm := newTestModel(lamARN)
	mm.recompute()
	mm.back()
	if len(mm.stack) != 1 || mm.current() != lamARN {
		t.Errorf("back at root should be a no-op, stack=%v", mm.stack)
	}
}

func TestDescend_EmptyRowNoop(t *testing.T) {
	mm := newTestModel(roleARN)
	mm.recompute()
	// Centered on the role, the Uses pane is empty; descending does nothing.
	mm.focus = paneUses
	mm.descend()
	if len(mm.stack) != 1 {
		t.Errorf("descend on empty pane should be a no-op, stack=%v", mm.stack)
	}
}

func TestLinkRows(t *testing.T) {
	rows := linkRows([]xref.Link{
		{Reference: xref.Reference{Service: "iam", Type: "role", ID: roleARN, Name: "app", Via: "execution role"}},
		{Reference: xref.Reference{Service: "ec2", Type: "subnet", ID: "subnet-1", Via: "subnet"}}, // no name → ID
	})
	if rows[0][0] != "1" || rows[0][1] != "iam" || rows[0][3] != "app" || rows[0][5] != "execution role" {
		t.Errorf("row0 = %v", rows[0])
	}
	if rows[1][3] != "subnet-1" {
		t.Errorf("row1 name should fall back to ID, got %q", rows[1][3])
	}
}

func TestUsedByEmpty_Scoped(t *testing.T) {
	mm := newTestModel("arn:aws:iam::111:role/lonely")
	mm.recompute()
	msg := mm.usedByEmpty()
	// IAM role is a recognized kind → the scoped message lists checked types.
	if msg == "" || !contains(msg, "Not referenced by anything checked") {
		t.Errorf("usedByEmpty = %q", msg)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

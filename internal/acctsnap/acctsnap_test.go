package acctsnap

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func res(service, typ, region, id, name, arn, state string, tags map[string]string) model.Resource {
	return model.Resource{
		Service: service, Type: typ, Region: region,
		ID: id, Name: name, ARN: arn, State: state, Tags: tags,
	}
}

func baseResources() []model.Resource {
	return []model.Resource{
		res("ec2", "instance", "us-east-1", "i-0abc", "web-3",
			"arn:aws:ec2:us-east-1:123:instance/i-0abc", "running", map[string]string{"Env": "prod"}),
		res("s3", "bucket", "", "old-logs-bucket", "old-logs-bucket",
			"arn:aws:s3:::old-logs-bucket", "", nil),
		res("ec2", "security-group", "us-east-1", "sg-noarn", "no-arn-sg", "", "", nil),
	}
}

func TestEntryKey_ARNlessNoIDFoldsName(t *testing.T) {
	// Two distinct ARN-less, ID-less resources of the same type/region must not
	// collapse to the same key (which would hide one in the diff).
	a := res("iam", "tag", "global", "", "team=payments", "", "", nil)
	b := res("iam", "tag", "global", "", "team=billing", "", "", nil)
	if entryKey(a) == entryKey(b) {
		t.Errorf("distinct ARN-less/ID-less resources share key %q", entryKey(a))
	}

	// A diff between two snapshots with two such resources must see both, not
	// silently merge them.
	snap := New([]model.Resource{a, b}, "123", []string{"global"})
	if len(snap.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (both ARN-less resources kept)", len(snap.Entries))
	}
	// ARN-bearing keys are still the ARN.
	withARN := res("ec2", "instance", "us-east-1", "i-1", "x", "arn:aws:ec2:us-east-1:1:instance/i-1", "", nil)
	if entryKey(withARN) != "arn:aws:ec2:us-east-1:1:instance/i-1" {
		t.Errorf("ARN key = %q", entryKey(withARN))
	}
}

func TestNew_DeterministicOrder(t *testing.T) {
	a := New(baseResources(), "123", []string{"us-east-1"})
	rev := baseResources()
	rev[0], rev[2] = rev[2], rev[0]
	b := New(rev, "123", []string{"us-east-1"})
	if !reflect.DeepEqual(a.Entries, b.Entries) {
		t.Errorf("entries depend on input order:\n%v\n%v", a.Entries, b.Entries)
	}
}

func TestDiff_NoChanges(t *testing.T) {
	old := New(baseResources(), "123", []string{"us-east-1"})
	neu := New(baseResources(), "123", []string{"us-east-1"})
	if changes := Diff(old, neu); len(changes) != 0 {
		t.Errorf("expected empty diff, got %v", changes)
	}
}

func TestDiff_AddedRemovedModified(t *testing.T) {
	old := New(baseResources(), "123", []string{"us-east-1"})

	newRes := baseResources()[:2] // sg-noarn removed
	// i-0abc stopped and retagged
	newRes[0].State = "stopped"
	newRes[0].Tags = map[string]string{"Env": "dev"}
	// a new lambda appears
	newRes = append(newRes, res("lambda", "function", "us-east-1", "new-payments-fn", "new-payments-fn",
		"arn:aws:lambda:us-east-1:123:function:new-payments-fn", "", nil))
	neu := New(newRes, "123", []string{"us-east-1"})

	changes := Diff(old, neu)
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3: %+v", len(changes), changes)
	}

	byKind := map[string]Change{}
	for _, c := range changes {
		byKind[c.Kind] = c
	}
	if c := byKind[KindAdded]; c.Type != "lambda/function" || c.ID != "new-payments-fn" {
		t.Errorf("added = %+v", c)
	}
	if c := byKind[KindRemoved]; c.ID != "sg-noarn" {
		t.Errorf("removed = %+v", c)
	}
	mod := byKind[KindModified]
	if mod.ID != "i-0abc" {
		t.Fatalf("modified = %+v", mod)
	}
	wantDeltas := []string{"state: running → stopped", "tag Env: prod → dev"}
	if !reflect.DeepEqual(mod.Deltas, wantDeltas) {
		t.Errorf("deltas = %v, want %v", mod.Deltas, wantDeltas)
	}
}

func TestDiff_TagAddedAndRemoved(t *testing.T) {
	oldRes := []model.Resource{res("ec2", "instance", "us-east-1", "i-1", "a",
		"arn:1", "running", map[string]string{"Drop": "x"})}
	newRes := []model.Resource{res("ec2", "instance", "us-east-1", "i-1", "a",
		"arn:1", "running", map[string]string{"Add": "y"})}
	changes := Diff(New(oldRes, "", nil), New(newRes, "", nil))
	if len(changes) != 1 {
		t.Fatalf("changes = %+v", changes)
	}
	want := []string{"tag Add: (none) → y", "tag Drop: x → (none)"}
	if !reflect.DeepEqual(changes[0].Deltas, want) {
		t.Errorf("deltas = %v, want %v", changes[0].Deltas, want)
	}
}

func TestScopeKey(t *testing.T) {
	if k := ScopeKey([]string{"us-east-1"}); k != "us-east-1" {
		t.Errorf("single region key = %q", k)
	}
	// Order-insensitive.
	if ScopeKey([]string{"b", "a"}) != ScopeKey([]string{"a", "b"}) {
		t.Error("scope key depends on region order")
	}
	if k := ScopeKey(nil); k != "default" {
		t.Errorf("empty scope key = %q", k)
	}
	// Long scopes hash to a short, stable name.
	var many []string
	for r := 'a'; r <= 'z'; r++ {
		many = append(many, "region-"+string(r)+"-northeast-9")
	}
	k := ScopeKey(many)
	if len(k) > 80 || !strings.HasPrefix(k, "26-regions-") {
		t.Errorf("long scope key = %q", k)
	}
	if k != ScopeKey(many) {
		t.Error("hashed scope key not stable")
	}
}

// tempHome points os.UserHomeDir at a fresh temp dir on every OS: HOME covers
// Unix, USERPROFILE is what os.UserHomeDir reads on Windows. Without the latter
// these tests would write into the runner's real profile on Windows.
func tempHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tempHome(t)

	snap := New(baseResources(), "123456789012", []string{"us-east-1"})
	path, err := Save(snap)
	if err != nil {
		t.Fatal(err)
	}
	// filepath.ToSlash normalizes the OS separator so the assertion holds on
	// Windows (backslashes) as well as Unix.
	if !strings.Contains(filepath.ToSlash(path), "account-snapshots/123456789012/us-east-1.json") {
		t.Errorf("path = %q", path)
	}

	loaded, ok, err := Load("123456789012", []string{"us-east-1"})
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(loaded.Entries, snap.Entries) {
		t.Errorf("round trip mismatch")
	}

	// Different scope: no baseline, but the saved scope is listed.
	if _, ok, _ := Load("123456789012", []string{"eu-west-1"}); ok {
		t.Error("found a baseline for the wrong scope")
	}
	if scopes := Scopes("123456789012"); !reflect.DeepEqual(scopes, []string{"us-east-1"}) {
		t.Errorf("Scopes = %v", scopes)
	}
}

func TestLoad_NoBaseline(t *testing.T) {
	tempHome(t)
	if _, ok, err := Load("999", []string{"us-east-1"}); ok || err != nil {
		t.Errorf("ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestRender_Table(t *testing.T) {
	baseline := Snapshot{TakenAt: time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)}
	changes := []Change{
		{Kind: KindAdded, Type: "lambda/function", ID: "new-payments-fn", Region: "us-east-1"},
		{Kind: KindRemoved, Type: "s3/bucket", ID: "old-logs-bucket"},
		{Kind: KindModified, Type: "ec2/instance", ID: "i-0abc", Name: "web-3", Region: "us-east-1",
			Deltas: []string{"state: running → stopped"}},
	}
	var buf bytes.Buffer
	if err := Render(&buf, NewReport(baseline, changes), "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Changes since baseline 2026-06-11 09:00 UTC — 1 added, 1 removed, 1 modified",
		"+ lambda/function",
		"- s3/bucket",
		"~ ec2/instance",
		"i-0abc (web-3)",
		"global", // bucket has no region
		"state: running → stopped",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestRender_JSONEmptyChanges(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, NewReport(Snapshot{TakenAt: time.Now()}, nil), "json", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"changes": []`) {
		t.Errorf("json = %s", buf.String())
	}
}

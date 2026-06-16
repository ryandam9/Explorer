package gluetui

import (
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDetailTitleFor(t *testing.T) {
	if got := detailTitleFor("crawler", "c1"); got != "Crawler — c1" {
		t.Errorf("crawler title = %q", got)
	}
	if got := detailTitleFor("bogus", "x"); got != "Detail — x" {
		t.Errorf("unknown type title = %q, want generic", got)
	}
}

func TestCrawlerTargets(t *testing.T) {
	if got := crawlerTargets(nil); got != nil {
		t.Errorf("nil targets = %v, want nil", got)
	}
	tg := &gluetypes.CrawlerTargets{
		S3Targets:      []gluetypes.S3Target{{Path: aws.String("s3://b/p")}},
		JdbcTargets:    []gluetypes.JdbcTarget{{Path: aws.String("db/schema/%")}},
		CatalogTargets: []gluetypes.CatalogTarget{{DatabaseName: aws.String("sales")}},
	}
	got := crawlerTargets(tg)
	want := []string{"s3: s3://b/p", "jdbc: db/schema/%", "catalog: sales"}
	if len(got) != len(want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDetailBodyRendering checks each DetailRow shape renders distinctly: an
// aligned key/value line, a blank value as an em dash, a section header and an
// indented bullet.
func TestDetailBodyRendering(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.detail = ResourceDetail{Rows: []DetailRow{
		{Label: "State", Value: "READY"},
		{Label: "Description", Value: ""},
		{},
		{Label: "Targets", Section: true},
		{Value: "s3: s3://bucket/prefix"},
	}}
	out := mm.detailBody()
	for _, want := range []string{"State", "READY", "Description", "—", "Targets", "  s3: s3://bucket/prefix"} {
		if !strings.Contains(out, want) {
			t.Errorf("detailBody missing %q:\n%s", want, out)
		}
	}
}

func TestDetailBodyLoadingAndError(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.detailLoading = true
	if !strings.Contains(mm.detailBody(), "Loading") {
		t.Errorf("loading body = %q", mm.detailBody())
	}
	mm.detailLoading = false
	mm.detailErr = errors.New("AccessDenied")
	if !strings.Contains(mm.detailBody(), "AccessDenied") {
		t.Errorf("error body = %q", mm.detailBody())
	}
}

// TestGlueEnterOpensDetailOnNonJobTab guards issue #238: Enter on a crawler/
// trigger/etc row opens the on-demand detail overlay, and any key closes it.
func TestGlueEnterOpensDetailOnNonJobTab(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.tab = tabCrawlers
	mm.rebuild()

	cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !mm.detailActive {
		t.Fatal("Enter on a crawler should open the detail overlay")
	}
	if !mm.detailLoading {
		t.Error("the detail overlay should start in a loading state")
	}
	if mm.detailTitle != "Crawler — catalog-crawler" {
		t.Errorf("detailTitle = %q", mm.detailTitle)
	}
	if len(cmds) == 0 {
		t.Error("Enter should issue a detail-load command")
	}

	// Any key closes the overlay (q still quits, tested by the guard order).
	mm.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.detailActive {
		t.Error("a key should close the detail overlay")
	}
}

// TestGlueEnterOnJobsTabKeepsRuns ensures the new detail path does not steal
// Enter on the Jobs tab, which still drills into the run history.
func TestGlueEnterOnJobsTabKeepsRuns(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.tab = tabJobs
	mm.rebuild()
	mm.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if mm.detailActive {
		t.Error("Enter on the Jobs tab must not open the resource-detail overlay")
	}
	if !mm.runsActive {
		t.Error("Enter on the Jobs tab should open the run history")
	}
}

// TestGlueDetailOverlayRenders checks the overlay composes over the dashboard
// without panicking or overflowing the terminal width.
func TestGlueDetailOverlayRenders(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.tab = tabConnections
	mm.rebuild()
	mm.detailActive = true
	mm.detailTitle = "Connection — db"
	mm.detail = ResourceDetail{Rows: []DetailRow{{Label: "Type", Value: "JDBC"}}}
	out := mm.View()
	if !strings.Contains(out, "Connection — db") || !strings.Contains(out, "JDBC") {
		t.Errorf("detail overlay not rendered:\n%s", out)
	}
}

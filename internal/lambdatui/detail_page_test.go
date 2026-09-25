package lambdatui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func detailTestModel(width, height int) *m {
	d := FunctionDetail{
		Name: "orders", Region: "us-east-1", ARN: "arn:aws:lambda:us-east-1:111122223333:function:orders",
		Runtime: "python3.12", PackageType: "Zip", Handler: "app.handler", Description: "Processes orders",
		MemoryMB: 256, TimeoutSec: 30, Role: "arn:aws:iam::111122223333:role/orders", State: "Active",
		LastUpdateStatus: "Successful", LastModified: time.Now().Add(-26 * 24 * time.Hour),
		EnvKeys:        []string{"DB_HOST"},
		ResourcePolicy: `{"Statement":[{"Effect":"Allow","Principal":{"Service":"events.amazonaws.com"},"Action":"lambda:InvokeFunction","Condition":{"ArnLike":{"AWS:SourceArn":"arn:aws:events:us-east-1:111122223333:rule/nightly"}}}]}`,
		Async:          &AsyncInfo{Default: true},
	}
	daily := make([]float64, usageDays)
	for i := range daily {
		daily[i] = math.NaN()
	}
	daily[28], daily[29] = 3, 4
	mm := &m{width: width, height: height, regions: []string{"us-east-1"}, detailActive: true,
		detailTitle: "Function — orders", detailFunc: d,
		usage: map[string]*FunctionUsage{"us-east-1/orders": {UsageKnown: true, Invocations30d: 7, Daily: daily,
			LastInvoked: time.Now().UTC(), LogKnown: true, LogExists: true, StoredBytes: 2 << 20}},
		inv: Inventory{Functions: []Function{{Name: "orders", Region: "us-east-1", Runtime: "python3.12", PackageType: "Zip",
			Architectures: []string{"x86_64"}, LogGroup: "/aws/lambda/orders", State: "Active"}}},
	}
	mm.setDetailSections(d.sections())
	return mm
}

// The page fits the terminal exactly at any width, with the header band, the
// three summary cards, the sections, and empty sections folded together.
func TestDetailPageLayout(t *testing.T) {
	for _, w := range []int{80, 130, 190} {
		mm := detailTestModel(w, 40)
		view := mm.View()
		lines := strings.Split(view, "\n")
		if len(lines) != 40 {
			t.Errorf("width %d: frame is %d lines, want 40", w, len(lines))
		}
		for i, l := range lines {
			if lw := ansi.StringWidth(l); lw > w {
				t.Fatalf("width %d: line %d is %d wide: %q", w, i, lw, l)
			}
		}
		page := ansi.Strip(mm.buildDetailPage(mm.detailPageWidth()).content)
		for _, want := range []string{"λ orders", "✓ Active", "python3.12 · x86_64 · 256 MB · 30s timeout", "Processes orders",
			"(26 days ago)", "╭─ Health", "╭─ Usage · 30 days", "╭─ Findings", "7 invocations",
			"Log retention", "never expires", "could run on arm64", "╭─ Not configured", "· no layers", "· no function URL",
			"EventBridge rule — nightly"} {
			if !strings.Contains(page, want) {
				t.Errorf("width %d: page missing %q", w, want)
			}
		}
		if strings.Contains(page, "╭─ Layers") {
			t.Errorf("width %d: an empty section should fold into Not configured", w)
		}
	}
}

// Tab walks the panels and scrolls the focused one into view; y copies it.
func TestDetailPageFocusScroll(t *testing.T) {
	mm := detailTestModel(80, 20) // one column: the page is long
	page := mm.buildDetailPage(mm.detailPageWidth())
	if len(page.tops) < 8 {
		t.Fatalf("expected cards + sections to be focusable, got %d", len(page.tops))
	}
	for i := 1; i < len(page.tops); i++ {
		mm.Update(key("tab"))
		top := page.tops[mm.detailFocus]
		if top < mm.detailOffset || top >= mm.detailOffset+mm.detailPageHeight() {
			t.Fatalf("panel %d (top %d) not in view at offset %d", mm.detailFocus, top, mm.detailOffset)
		}
	}
	mm.Update(key("tab")) // wraps to the first panel
	if mm.detailFocus != 0 {
		t.Errorf("Tab past the last panel should wrap, focus %d", mm.detailFocus)
	}
	mm.Update(key("G"))
	total := len(strings.Split(page.content, "\n"))
	if mm.detailOffset != total-mm.detailPageHeight() {
		t.Errorf("G should scroll to the end: offset %d, want %d", mm.detailOffset, total-mm.detailPageHeight())
	}
	if !strings.Contains(mm.focusedPanelText(), "Active") {
		t.Errorf("focused panel text = %q", mm.focusedPanelText())
	}
}

func TestAgeLabel(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{{time.Hour, "today"}, {30 * time.Hour, "yesterday"}, {26 * 24 * time.Hour, "26 days ago"},
		{120 * 24 * time.Hour, "4 months ago"}, {3 * 365 * 24 * time.Hour, "3 years ago"}} {
		if got := ageLabel(now.Add(-c.ago), now); got != c.want {
			t.Errorf("ageLabel(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

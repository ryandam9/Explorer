package billtui

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/billing"
)

func line(service, usage string, amount float64) billing.Line {
	return billing.Line{Service: service, UsageType: usage, Amount: amount}
}

func TestDiffBills(t *testing.T) {
	prev := &billing.Bill{Lines: []billing.Line{
		line("EC2", "BoxUsage", 5.0),
		line("S3", "Storage", 1.0),
	}}
	next := &billing.Bill{Lines: []billing.Line{
		line("EC2", "BoxUsage", 7.5),    // moved up by 2.5
		line("S3", "Storage", 1.0),      // unchanged → absent
		line("Lambda", "Duration", 0.3), // new → full amount
	}}

	d := diffBills(prev, next)
	if got := d[line("EC2", "BoxUsage", 0).Key()]; got != 2.5 {
		t.Errorf("EC2 delta = %v, want 2.5", got)
	}
	if _, ok := d[line("S3", "Storage", 0).Key()]; ok {
		t.Error("unchanged line should not appear in deltas")
	}
	if got := d[line("Lambda", "Duration", 0).Key()]; got != 0.3 {
		t.Errorf("new line delta = %v, want 0.3 (full amount)", got)
	}
}

func TestDiffBills_NilPrev(t *testing.T) {
	// First fetch has no prior bill: no deltas, no panic.
	if d := diffBills(nil, &billing.Bill{Lines: []billing.Line{line("EC2", "X", 1)}}); d != nil {
		t.Errorf("diff against nil prev = %v, want nil", d)
	}
}

func TestFormatDelta(t *testing.T) {
	cases := []struct {
		d        float64
		currency string
		want     string
	}{
		{0, "USD", ""},
		{2.5, "USD", "+$2.50"},
		{-1.0, "USD", "-$1.00"},
	}
	for _, c := range cases {
		if got := formatDelta(c.d, c.currency); got != c.want {
			t.Errorf("formatDelta(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFilterLines(t *testing.T) {
	lines := []billing.Line{
		line("Amazon EC2", "BoxUsage:t3.micro", 5),
		line("Amazon S3", "TimedStorage", 1),
		{Service: "Amazon EC2", UsageType: "EBS", Unit: "GB-Mo", Amount: 8},
	}
	if got := filterLines(lines, ""); len(got) != 3 {
		t.Errorf("empty filter kept %d, want all 3", len(got))
	}
	if got := filterLines(lines, "ec2"); len(got) != 2 {
		t.Errorf("'ec2' matched %d lines, want 2", len(got))
	}
	if got := filterLines(lines, "gb-mo"); len(got) != 1 {
		t.Errorf("unit filter matched %d lines, want 1", len(got))
	}
	if got := filterLines(lines, "nomatch"); len(got) != 0 {
		t.Errorf("non-matching filter kept %d, want 0", len(got))
	}
}

func TestSortVisible(t *testing.T) {
	m := &Model{
		visible: []billing.Line{
			line("B-service", "u", 1.0),
			line("A-service", "u", 9.0),
		},
	}
	// Sort by SERVICE (col 0) ascending.
	m.sortCol, m.sortAsc = 0, true
	m.sortVisible()
	if m.visible[0].Service != "A-service" {
		t.Errorf("service-asc first = %q, want A-service", m.visible[0].Service)
	}

	// Sort by COST (col 4) descending (the default direction for that column).
	m.sortCol, m.sortAsc = 4, false
	m.sortVisible()
	if m.visible[0].Amount != 9.0 {
		t.Errorf("cost-desc first amount = %v, want 9.0", m.visible[0].Amount)
	}
}

func TestSortVisible_NaturalOrderUntouched(t *testing.T) {
	orig := []billing.Line{line("Z", "u", 1), line("A", "u", 2)}
	m := &Model{visible: append([]billing.Line(nil), orig...), sortCol: -1}
	m.sortVisible()
	for i := range orig {
		if m.visible[i] != orig[i] {
			t.Fatalf("col -1 should preserve incoming order; changed at %d", i)
		}
	}
}

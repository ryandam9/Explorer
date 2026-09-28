package billtui

import (
	"sort"

	"github.com/ryandam9/aws_explorer/internal/billing"
)

// Cost Explorer returns one line per (service, usage type), so a real bill
// arrives as hundreds of rows — most of them usage types that accrued usage
// but no cost. The summary folds those into one row per service, which is the
// shape you want when the question is "what am I paying for?" rather than
// "which usage type is this charge".

// ServiceTotal is one service's cost rolled up across its usage types.
type ServiceTotal struct {
	Service string
	Amount  float64
	Lines   int // usage-type rows folded into this total
	Zero    int // how many of those carried no cost at all
}

// summarizeByService folds bill lines into one row per service, ordered by
// cost descending with ties broken by name so the order is stable across
// refreshes. Zero-cost lines are counted, not dropped: a service that billed
// nothing this period is still a fact about the bill, and its line count is
// what explains the long tail in the detailed view.
func summarizeByService(lines []billing.Line) []ServiceTotal {
	byService := make(map[string]*ServiceTotal)
	for _, l := range lines {
		t, ok := byService[l.Service]
		if !ok {
			t = &ServiceTotal{Service: l.Service}
			byService[l.Service] = t
		}
		t.Amount += l.Amount
		t.Lines++
		if l.Amount == 0 {
			t.Zero++
		}
	}

	out := make([]ServiceTotal, 0, len(byService))
	for _, t := range byService {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Amount != out[j].Amount {
			return out[i].Amount > out[j].Amount
		}
		return out[i].Service < out[j].Service
	})
	return out
}

// totalOf sums the rolled-up rows. The summary's own total is used rather
// than bill.Total so the footer always describes the rows on screen: with a
// filter applied those differ, and showing the whole-bill total under a
// filtered table would misreport what is displayed.
func totalOf(totals []ServiceTotal) float64 {
	var sum float64
	for _, t := range totals {
		sum += t.Amount
	}
	return sum
}

// shareOf returns amount as a percentage of total. A zero total yields 0
// rather than NaN — an empty or all-free bill must render as "0.0%", not as
// a broken cell.
func shareOf(amount, total float64) float64 {
	if total == 0 {
		return 0
	}
	return amount / total * 100
}

// dropZeroCost removes the lines that accrued no cost. It is the detailed
// view's answer to the same noise the summary folds away, for when you want
// the usage-type detail but only for what you are actually paying for.
func dropZeroCost(lines []billing.Line) []billing.Line {
	out := make([]billing.Line, 0, len(lines))
	for _, l := range lines {
		if l.Amount != 0 {
			out = append(out, l)
		}
	}
	return out
}

// countZeroCost reports how many lines carry no cost, so the UI can say what
// hiding them would remove (and what it did remove) instead of silently
// shortening the table.
func countZeroCost(lines []billing.Line) int {
	n := 0
	for _, l := range lines {
		if l.Amount == 0 {
			n++
		}
	}
	return n
}

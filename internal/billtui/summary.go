package billtui

import (
	"fmt"
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

// costsNothing reports whether an amount is zero *as the table prints it*.
//
// Testing amount == 0 is not the same thing and is the wrong test here: Cost
// Explorer returns full precision, so a service billing a ten-thousandth of a
// cent is not zero, prints as "$0.0000", and would otherwise survive a filter
// whose whole promise was that rows reading zero would be gone. The filter is
// therefore defined by the rendering — hide what displays as nothing — so
// what you see and what is hidden can never disagree, at whatever precision
// the amount happens to be shown.
//
// It looks for a non-zero digit rather than comparing against a formatted
// zero, because the two are not formatted alike: "$0.00" has two decimals and
// "$0.0000" has four, and a sub-cent credit carries a minus sign as well.
func costsNothing(amount float64, currency string) bool {
	for _, r := range billing.FormatAmount(amount, currency) {
		if r >= '1' && r <= '9' {
			return false
		}
	}
	return true
}

// dropZeroCost removes the lines that print as costing nothing. It is the
// detailed view's answer to the same noise the summary folds away, for when
// you want the usage-type detail but only for what you are actually paying
// for.
func dropZeroCost(lines []billing.Line, currency string) []billing.Line {
	out := make([]billing.Line, 0, len(lines))
	for _, l := range lines {
		if !costsNothing(l.Amount, currency) {
			out = append(out, l)
		}
	}
	return out
}

// countZeroCost reports how many lines print as costing nothing, so the UI can
// say what hiding them would remove (and what it did remove) instead of
// silently shortening the table.
func countZeroCost(lines []billing.Line, currency string) int {
	n := 0
	for _, l := range lines {
		if costsNothing(l.Amount, currency) {
			n++
		}
	}
	return n
}

// plural renders a count with the right noun ("1 free line", "3 free lines"),
// so the footer reads as a sentence rather than as a debug counter.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

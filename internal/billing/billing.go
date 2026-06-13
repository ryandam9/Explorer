// Package billing fetches the account's bill from the AWS Cost Explorer API
// (`aws_explorer bill`). The bill is the month-to-date unblended cost grouped
// by service and usage type, each line carrying the usage quantity and unit,
// with a grand total — the same numbers the Billing console shows, not the
// static list-price estimates the audit linter uses.
//
// Cost Explorer is a paid API: AWS bills every request (GetCostAndUsage,
// GetCostAndUsageWithResources) at $0.01. Callers that poll should surface
// that to the user.
package billing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/smithy-go"
	"github.com/dustin/go-humanize"
)

const (
	dateFmt     = "2006-01-02"
	metricCost  = "UnblendedCost"
	metricUsage = "UsageQuantity"
)

// resourceWindowDays is how far back Cost Explorer keeps resource-level data.
const resourceWindowDays = 14

// API is the Cost Explorer surface the package uses, separated for test fakes.
type API interface {
	GetCostAndUsage(ctx context.Context, in *costexplorer.GetCostAndUsageInput, opts ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
	GetCostAndUsageWithResources(ctx context.Context, in *costexplorer.GetCostAndUsageWithResourcesInput, opts ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageWithResourcesOutput, error)
}

// NewClient builds a Cost Explorer client. The service is global; the caller
// should pass a config pinned to us-east-1.
func NewClient(cfg aws.Config) *costexplorer.Client {
	return costexplorer.NewFromConfig(cfg)
}

// Line is one bill line: a (service, usage type) pair with its accrued usage
// and cost over the period.
type Line struct {
	Service   string  `json:"service"`
	UsageType string  `json:"usageType"`
	Quantity  float64 `json:"quantity"`
	Unit      string  `json:"unit,omitempty"`
	Amount    float64 `json:"amount"`
}

// Key identifies a line across refreshes (delta tracking in the TUI).
func (l Line) Key() string { return l.Service + "\x00" + l.UsageType }

// Bill is the account's cost for [Start, End), End exclusive, sorted by
// amount descending. Estimated is set when the period includes days AWS has
// not finalized yet (i.e. any current-month query).
type Bill struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Currency  string    `json:"currency"`
	Estimated bool      `json:"estimated"`
	Lines     []Line    `json:"lines"`
	Total     float64   `json:"total"`
}

// ResourceCost is one resource's cost within a service (resource-level
// drill-down). Resource is the ID or ARN exactly as Cost Explorer reports it.
type ResourceCost struct {
	Resource string  `json:"resource"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit,omitempty"`
	Amount   float64 `json:"amount"`
}

// ErrResourceDataDisabled marks the per-resource breakdown being unavailable
// because the account has not opted in to resource-level data.
var ErrResourceDataDisabled = errors.New(
	"resource-level data is not enabled for this account — turn on " +
		"\"Daily granularity resource-level data\" under Billing → Cost Management Preferences")

// Fetch retrieves the bill for [start, end) grouped by service and usage
// type. Lines that accrued neither cost nor usage are dropped.
func Fetch(ctx context.Context, api API, start, end time.Time) (*Bill, error) {
	bill := &Bill{Start: start, End: end, Currency: "USD"}
	agg := map[string]*Line{}

	var token *string
	for {
		out, err := api.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
			TimePeriod: &cetypes.DateInterval{
				Start: aws.String(start.Format(dateFmt)),
				End:   aws.String(end.Format(dateFmt)),
			},
			Granularity: cetypes.GranularityMonthly,
			Metrics:     []string{metricCost, metricUsage},
			GroupBy: []cetypes.GroupDefinition{
				{Type: cetypes.GroupDefinitionTypeDimension, Key: aws.String("SERVICE")},
				{Type: cetypes.GroupDefinitionTypeDimension, Key: aws.String("USAGE_TYPE")},
			},
			NextPageToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("cost explorer GetCostAndUsage: %w", err)
		}

		for _, rbt := range out.ResultsByTime {
			if rbt.Estimated {
				bill.Estimated = true
			}
			for _, g := range rbt.Groups {
				if len(g.Keys) < 2 {
					continue
				}
				key := g.Keys[0] + "\x00" + g.Keys[1]
				line, ok := agg[key]
				if !ok {
					line = &Line{Service: g.Keys[0], UsageType: g.Keys[1]}
					agg[key] = line
				}
				amt, unit := metric(g.Metrics, metricCost)
				qty, qunit := metric(g.Metrics, metricUsage)
				line.Amount += amt
				line.Quantity += qty
				if line.Unit == "" {
					line.Unit = qunit
				}
				if unit != "" {
					bill.Currency = unit
				}
			}
		}

		if out.NextPageToken == nil || *out.NextPageToken == "" {
			break
		}
		token = out.NextPageToken
	}

	for _, l := range agg {
		if l.Amount == 0 && l.Quantity == 0 {
			continue
		}
		bill.Lines = append(bill.Lines, *l)
		bill.Total += l.Amount
	}
	SortLines(bill.Lines)
	return bill, nil
}

// FetchResources retrieves the per-resource cost for one service over the
// trailing resource-data window (Cost Explorer keeps resource-level data for
// 14 days only, and only when the account opted in). It returns the window
// start alongside the rows so callers can label the shorter period.
func FetchResources(ctx context.Context, api API, service string, now time.Time) ([]ResourceCost, time.Time, error) {
	now = now.UTC()
	end := now.AddDate(0, 0, 1).Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -resourceWindowDays)

	agg := map[string]*ResourceCost{}
	var token *string
	for {
		out, err := api.GetCostAndUsageWithResources(ctx, &costexplorer.GetCostAndUsageWithResourcesInput{
			TimePeriod: &cetypes.DateInterval{
				Start: aws.String(start.Format(dateFmt)),
				End:   aws.String(end.Format(dateFmt)),
			},
			Granularity: cetypes.GranularityDaily,
			Metrics:     []string{metricCost, metricUsage},
			Filter: &cetypes.Expression{
				Dimensions: &cetypes.DimensionValues{
					Key:    cetypes.DimensionService,
					Values: []string{service},
				},
			},
			GroupBy: []cetypes.GroupDefinition{
				{Type: cetypes.GroupDefinitionTypeDimension, Key: aws.String("RESOURCE_ID")},
			},
			NextPageToken: token,
		})
		if err != nil {
			if isResourceDataDisabled(err) {
				return nil, start, fmt.Errorf("%w (%v)", ErrResourceDataDisabled, err)
			}
			return nil, start, fmt.Errorf("cost explorer GetCostAndUsageWithResources: %w", err)
		}

		for _, rbt := range out.ResultsByTime {
			for _, g := range rbt.Groups {
				if len(g.Keys) < 1 {
					continue
				}
				rc, ok := agg[g.Keys[0]]
				if !ok {
					rc = &ResourceCost{Resource: g.Keys[0]}
					agg[g.Keys[0]] = rc
				}
				amt, _ := metric(g.Metrics, metricCost)
				qty, qunit := metric(g.Metrics, metricUsage)
				rc.Amount += amt
				rc.Quantity += qty
				if rc.Unit == "" {
					rc.Unit = qunit
				}
			}
		}

		if out.NextPageToken == nil || *out.NextPageToken == "" {
			break
		}
		token = out.NextPageToken
	}

	rows := make([]ResourceCost, 0, len(agg))
	for _, rc := range agg {
		if rc.Amount == 0 && rc.Quantity == 0 {
			continue
		}
		rows = append(rows, *rc)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Amount != rows[j].Amount {
			return rows[i].Amount > rows[j].Amount
		}
		return rows[i].Resource < rows[j].Resource
	})
	return rows, start, nil
}

// isResourceDataDisabled spots the ValidationException Cost Explorer returns
// when the account never opted in to resource-level data.
func isResourceDataDisabled(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.ErrorCode() != "ValidationException" && apiErr.ErrorCode() != "DataUnavailableException" {
		return false
	}
	msg := strings.ToLower(apiErr.ErrorMessage())
	return strings.Contains(msg, "resource") || strings.Contains(msg, "not enabled")
}

// metric parses one metric value from a Cost Explorer group, returning 0 for
// missing or unparseable amounts.
func metric(metrics map[string]cetypes.MetricValue, name string) (float64, string) {
	mv, ok := metrics[name]
	if !ok {
		return 0, ""
	}
	var amount float64
	if mv.Amount != nil {
		amount, _ = strconv.ParseFloat(*mv.Amount, 64)
	}
	var unit string
	if mv.Unit != nil {
		unit = *mv.Unit
	}
	return amount, unit
}

// SortLines orders lines by amount descending, then service and usage type,
// so the most expensive items lead and ties are deterministic.
func SortLines(lines []Line) {
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Amount != lines[j].Amount {
			return lines[i].Amount > lines[j].Amount
		}
		if lines[i].Service != lines[j].Service {
			return lines[i].Service < lines[j].Service
		}
		return lines[i].UsageType < lines[j].UsageType
	})
}

// MonthToDate returns the current billing period [first of month, tomorrow)
// in UTC. The exclusive end of tomorrow makes today's partial (estimated)
// charges part of the answer.
func MonthToDate(now time.Time) (start, end time.Time) {
	now = now.UTC()
	start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return start, end
}

// ParseMonth turns "YYYY-MM" into a billing period. Past months cover the
// whole month; the current month clamps to month-to-date (Cost Explorer
// rejects end dates beyond tomorrow); future months are an error.
func ParseMonth(s string, now time.Time) (start, end time.Time, err error) {
	t, err := time.Parse("2006-01", strings.TrimSpace(s))
	if err != nil {
		return start, end, fmt.Errorf("invalid --month %q (use YYYY-MM, e.g. 2026-05)", s)
	}
	now = now.UTC()
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	if start.After(now) {
		return start, end, fmt.Errorf("--month %s is in the future", s)
	}
	end = start.AddDate(0, 1, 0)
	if tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC); end.After(tomorrow) {
		end = tomorrow
	}
	return start, end, nil
}

// PeriodLabel names a billing period for headers: "June 2026 (month-to-date)"
// while the month is still accruing, "May 2026" once it is closed.
func PeriodLabel(start, end, now time.Time) string {
	label := start.Format("January 2006")
	if !end.Before(now.UTC()) {
		label += " (month-to-date)"
	}
	return label
}

// FormatAmount renders a cost for display: "$1,234.56" for USD (the Cost
// Explorer default), "-$0.42" for credits, "1,234.56 EUR" otherwise.
func FormatAmount(amount float64, currency string) string {
	abs := amount
	sign := ""
	if amount < 0 {
		abs = -amount
		sign = "-"
	}
	n := humanize.FormatFloat("#,###.##", abs)
	if currency == "" || currency == "USD" {
		return sign + "$" + n
	}
	return sign + n + " " + currency
}

// FormatQty renders a usage quantity with up to four significant decimals
// ("120", "0.0231", "1,440.5").
func FormatQty(q float64) string {
	return humanize.CommafWithDigits(q, 4)
}

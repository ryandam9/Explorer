package billing

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/smithy-go"
)

// fakeAPI serves canned Cost Explorer responses, optionally across two pages,
// and records the inputs it was called with.
type fakeAPI struct {
	pages   []*costexplorer.GetCostAndUsageOutput
	calls   int
	lastIn  *costexplorer.GetCostAndUsageInput
	resOut  *costexplorer.GetCostAndUsageWithResourcesOutput
	resErr  error
	resIn   *costexplorer.GetCostAndUsageWithResourcesInput
	costErr error
}

func (f *fakeAPI) GetCostAndUsage(_ context.Context, in *costexplorer.GetCostAndUsageInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	if f.costErr != nil {
		return nil, f.costErr
	}
	f.lastIn = in
	out := f.pages[f.calls]
	f.calls++
	return out, nil
}

func (f *fakeAPI) GetCostAndUsageWithResources(_ context.Context, in *costexplorer.GetCostAndUsageWithResourcesInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageWithResourcesOutput, error) {
	f.resIn = in
	if f.resErr != nil {
		return nil, f.resErr
	}
	return f.resOut, nil
}

func metricVal(amount, unit string) cetypes.MetricValue {
	return cetypes.MetricValue{Amount: aws.String(amount), Unit: aws.String(unit)}
}

func group(service, usageType, cost, usage, usageUnit string) cetypes.Group {
	return cetypes.Group{
		Keys: []string{service, usageType},
		Metrics: map[string]cetypes.MetricValue{
			metricCost:  metricVal(cost, "USD"),
			metricUsage: metricVal(usage, usageUnit),
		},
	}
}

func TestFetch_AggregatesAndSorts(t *testing.T) {
	api := &fakeAPI{pages: []*costexplorer.GetCostAndUsageOutput{{
		ResultsByTime: []cetypes.ResultByTime{{
			Estimated: true,
			Groups: []cetypes.Group{
				group("Amazon EC2", "BoxUsage:t3.micro", "1.50", "744", "Hrs"),
				group("Amazon S3", "TimedStorage-ByteHrs", "0.25", "10", "GB-Mo"),
				group("Amazon EC2", "EBS:VolumeUsage.gp3", "8.00", "100", "GB-Mo"),
				// Zero-cost, zero-usage line is dropped.
				group("AWS KMS", "KMS-Keys", "0", "0", ""),
			},
		}},
	}}}

	bill, err := Fetch(context.Background(), api, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bill.Estimated {
		t.Error("expected Estimated=true for a partial month")
	}
	if len(bill.Lines) != 3 {
		t.Fatalf("got %d lines, want 3 (zero line dropped): %+v", len(bill.Lines), bill.Lines)
	}
	// Sorted by amount descending: EBS 8.00, EC2 box 1.50, S3 0.25.
	if bill.Lines[0].UsageType != "EBS:VolumeUsage.gp3" {
		t.Errorf("first line = %q, want the $8 EBS line", bill.Lines[0].UsageType)
	}
	if got := bill.Total; got != 9.75 {
		t.Errorf("total = %v, want 9.75", got)
	}
	if bill.Lines[0].Quantity != 100 || bill.Lines[0].Unit != "GB-Mo" {
		t.Errorf("usage = %v %q, want 100 GB-Mo", bill.Lines[0].Quantity, bill.Lines[0].Unit)
	}
	if bill.Currency != "USD" {
		t.Errorf("currency = %q, want USD", bill.Currency)
	}
}

func TestFetch_Paginates(t *testing.T) {
	api := &fakeAPI{pages: []*costexplorer.GetCostAndUsageOutput{
		{
			NextPageToken: aws.String("page2"),
			ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{
				group("Amazon EC2", "BoxUsage", "1.00", "1", "Hrs"),
			}}},
		},
		{
			ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{
				// Same line continues on page 2; amounts accumulate.
				group("Amazon EC2", "BoxUsage", "2.00", "2", "Hrs"),
			}}},
		},
	}}

	bill, err := Fetch(context.Background(), api, time.Now().AddDate(0, 0, -1), time.Now())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if api.calls != 2 {
		t.Errorf("called %d times, want 2 (pagination)", api.calls)
	}
	if len(bill.Lines) != 1 || bill.Lines[0].Amount != 3.00 || bill.Lines[0].Quantity != 3 {
		t.Errorf("accumulated line = %+v, want amount 3 qty 3", bill.Lines)
	}
}

func TestFetch_PropagatesError(t *testing.T) {
	api := &fakeAPI{costErr: errors.New("boom")}
	if _, err := Fetch(context.Background(), api, time.Now().AddDate(0, 0, -1), time.Now()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchResources_FiltersAndSorts(t *testing.T) {
	api := &fakeAPI{resOut: &costexplorer.GetCostAndUsageWithResourcesOutput{
		ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{
			{Keys: []string{"i-0aaa"}, Metrics: map[string]cetypes.MetricValue{
				metricCost: metricVal("0.50", "USD"), metricUsage: metricVal("12", "Hrs")}},
			{Keys: []string{"i-0bbb"}, Metrics: map[string]cetypes.MetricValue{
				metricCost: metricVal("3.00", "USD"), metricUsage: metricVal("72", "Hrs")}},
			{Keys: []string{"i-0ccc"}, Metrics: map[string]cetypes.MetricValue{
				metricCost: metricVal("0", "USD"), metricUsage: metricVal("0", "")}},
		}}},
	}}

	rows, start, err := FetchResources(context.Background(), api, "Amazon EC2", time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchResources: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (zero row dropped)", len(rows))
	}
	if rows[0].Resource != "i-0bbb" {
		t.Errorf("first row = %q, want the costliest i-0bbb", rows[0].Resource)
	}
	// Window starts 14 days before tomorrow (2026-06-14) → 2026-05-31.
	if want := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC); !start.Equal(want) {
		t.Errorf("window start = %s, want %s", start.Format(dateFmt), want.Format(dateFmt))
	}
	if f, ok := api.resIn.Filter.Dimensions, true; ok && f.Values[0] != "Amazon EC2" {
		t.Errorf("filter value = %q, want service name", f.Values[0])
	}
}

func TestFetchResources_DisabledMapsToSentinel(t *testing.T) {
	api := &fakeAPI{resErr: &smithy.GenericAPIError{
		Code:    "ValidationException",
		Message: "resource-level data is not enabled",
	}}
	_, _, err := FetchResources(context.Background(), api, "Amazon EC2", time.Now())
	if !errors.Is(err, ErrResourceDataDisabled) {
		t.Errorf("err = %v, want ErrResourceDataDisabled", err)
	}
}

func TestMonthToDate(t *testing.T) {
	now := time.Date(2026, 6, 13, 15, 4, 5, 0, time.UTC)
	start, end := MonthToDate(now)
	if !start.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("start = %s, want 2026-06-01", start.Format(dateFmt))
	}
	if !end.Equal(time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("end = %s, want 2026-06-14 (tomorrow, exclusive)", end.Format(dateFmt))
	}
}

func TestParseMonth(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)

	t.Run("past month covers the whole month", func(t *testing.T) {
		start, end, err := ParseMonth("2026-05", now)
		if err != nil {
			t.Fatal(err)
		}
		if !start.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("got %s → %s, want full May", start.Format(dateFmt), end.Format(dateFmt))
		}
	})

	t.Run("current month clamps to tomorrow", func(t *testing.T) {
		_, end, err := ParseMonth("2026-06", now)
		if err != nil {
			t.Fatal(err)
		}
		if !end.Equal(time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("end = %s, want clamp to 2026-06-14", end.Format(dateFmt))
		}
	})

	t.Run("future month errors", func(t *testing.T) {
		if _, _, err := ParseMonth("2026-07", now); err == nil {
			t.Error("expected error for a future month")
		}
	})

	t.Run("garbage errors", func(t *testing.T) {
		if _, _, err := ParseMonth("nope", now); err == nil {
			t.Error("expected error for an unparseable month")
		}
	})
}

func TestPeriodLabel(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	mtdStart, mtdEnd := MonthToDate(now)
	if got := PeriodLabel(mtdStart, mtdEnd, now); !strings.Contains(got, "month-to-date") {
		t.Errorf("current period label = %q, want month-to-date marker", got)
	}
	mayStart, mayEnd, _ := ParseMonth("2026-05", now)
	if got := PeriodLabel(mayStart, mayEnd, now); got != "May 2026" {
		t.Errorf("closed period label = %q, want %q", got, "May 2026")
	}
}

func TestFormatAmount(t *testing.T) {
	cases := []struct {
		amount   float64
		currency string
		want     string
	}{
		{1234.5, "USD", "$1,234.50"},
		{0.42, "USD", "$0.42"},
		{0, "USD", "$0.00"},
		{-0.42, "USD", "-$0.42"},
		{1234.56, "EUR", "1,234.56 EUR"},
		{5, "", "$5.00"},
	}
	for _, c := range cases {
		if got := FormatAmount(c.amount, c.currency); got != c.want {
			t.Errorf("FormatAmount(%v, %q) = %q, want %q", c.amount, c.currency, got, c.want)
		}
	}
}

func TestRender_TableHasTotal(t *testing.T) {
	bill := &Bill{
		Start:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
		Currency:  "USD",
		Estimated: true,
		Lines: []Line{
			{Service: "Amazon EC2", UsageType: "BoxUsage", Quantity: 744, Unit: "Hrs", Amount: 8.0},
		},
		Total: 8.0,
	}
	var buf bytes.Buffer
	if err := Render(&buf, bill, "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "TOTAL (estimated)") {
		t.Errorf("table output missing estimated total:\n%s", out)
	}
	if !strings.Contains(out, "$8.00") {
		t.Errorf("table output missing amount:\n%s", out)
	}
}

func TestRender_CSVRoundTrips(t *testing.T) {
	bill := &Bill{Currency: "USD", Lines: []Line{
		{Service: "Amazon S3", UsageType: "Requests", Quantity: 1000, Unit: "Count", Amount: 0.4},
	}}
	var buf bytes.Buffer
	if err := Render(&buf, bill, "csv", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "Service,UsageType,Usage,Unit,Cost,Currency") {
		t.Errorf("csv missing header:\n%s", out)
	}
	if !strings.Contains(out, "Amazon S3,Requests,1000,Count,0.4,USD") {
		t.Errorf("csv missing data row:\n%s", out)
	}
}

func TestRender_JSONEmptyLinesIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, &Bill{Currency: "USD"}, "json", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"lines": []`) {
		t.Errorf("empty bill should encode lines as [], got:\n%s", buf.String())
	}
}

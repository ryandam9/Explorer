package emrtui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

// maxHBaseTables caps how many tables the browser enriches, so a cluster with
// thousands of tables can't stall the UI.
const maxHBaseTables = 300

// hbaseEnrichParallel bounds concurrent per-table REST calls.
const hbaseEnrichParallel = 8

// HBaseTable is one table's posture, flattened for the HBase browser (AXE-041),
// read from the HBase REST server (default port 8080).
type HBaseTable struct {
	Namespace string
	Name      string // bare table name (within the namespace)
	Qualified string // "ns:table", or just "table" for the default namespace
	Regions   int
	Online    int      // regions with an assigned location
	Families  []string // column families
	State     string   // derived: ENABLED / DISABLED / PARTIAL / "—"

	// Row count, populated only after an explicit, confirmed scan (the `c`
	// action). Counted distinguishes "not counted yet" from "counted 0".
	RowCount    int
	Counted     bool
	CountCapped bool
}

// --- HBase REST wire types (JSON via Accept: application/json) --------------

type hbaseNamespaces struct {
	Namespace []string `json:"Namespace"`
}

type hbaseTableList struct {
	Table []struct {
		Name string `json:"name"`
	} `json:"table"`
}

type hbaseTableInfo struct {
	Name   string `json:"name"`
	Region []struct {
		Name     string `json:"name"`
		Location string `json:"location"`
	} `json:"Region"`
}

type hbaseTableSchema struct {
	Name         string `json:"name"`
	ColumnSchema []struct {
		Name string `json:"name"`
	} `json:"ColumnSchema"`
}

// parseNamespaces maps a GET /namespaces payload. Pure.
func parseNamespaces(body []byte) ([]string, error) {
	var ns hbaseNamespaces
	if err := json.Unmarshal(body, &ns); err != nil {
		return nil, err
	}
	return ns.Namespace, nil
}

// parseTableList maps a GET /namespaces/<ns>/tables payload to bare table names.
// Pure.
func parseTableList(body []byte) ([]string, error) {
	var tl hbaseTableList
	if err := json.Unmarshal(body, &tl); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(tl.Table))
	for _, t := range tl.Table {
		if t.Name != "" {
			out = append(out, t.Name)
		}
	}
	return out, nil
}

// parseRegions maps a GET /<table>/regions payload to (total, online). A region
// counts as online when it has an assigned location (server). Pure.
func parseRegions(body []byte) (total, online int, err error) {
	var ti hbaseTableInfo
	if err := json.Unmarshal(body, &ti); err != nil {
		return 0, 0, err
	}
	total = len(ti.Region)
	for _, r := range ti.Region {
		if r.Location != "" {
			online++
		}
	}
	return total, online, nil
}

// parseSchema maps a GET /<table>/schema payload to its column-family names.
// Pure.
func parseSchema(body []byte) ([]string, error) {
	var ts hbaseTableSchema
	if err := json.Unmarshal(body, &ts); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ts.ColumnSchema))
	for _, c := range ts.ColumnSchema {
		if c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out, nil
}

// deriveTableState turns region counts into a coarse, REST-derivable status:
// a table whose regions are all unassigned is effectively DISABLED; all assigned
// is ENABLED; a mix is PARTIAL (regions in transition / being moved).
func deriveTableState(total, online int) string {
	switch {
	case total == 0:
		return "—"
	case online == 0:
		return "DISABLED"
	case online == total:
		return "ENABLED"
	default:
		return "PARTIAL"
	}
}

// qualify builds the REST table name from a namespace and bare name. The default
// namespace is addressed by the bare name; others as "ns:name".
func qualify(ns, name string) string {
	if ns == "" || ns == "default" {
		return name
	}
	return ns + ":" + name
}

// FetchHBase lists namespaces and tables from a cluster's HBase REST server and
// enriches each table with its region counts and column families. Returns an
// ErrUnreachable-wrapped error when the REST server can't be reached, so callers
// render the connect helper.
func FetchHBase(ctx context.Context, d *emrconn.Dialer, masterDNS string) ([]HBaseTable, error) {
	if d == nil {
		return nil, emrconn.ErrDisabled
	}
	if masterDNS == "" {
		return nil, fmt.Errorf("%w: cluster has no primary-node DNS (not running?)", emrconn.ErrUnreachable)
	}

	nsBody, err := d.GetRaw(ctx, emrconn.ServiceHBase, masterDNS, "/namespaces")
	if err != nil {
		return nil, err
	}
	namespaces, err := parseNamespaces(nsBody)
	if err != nil {
		return nil, err
	}

	// List tables per namespace (cheap), then enrich up to the cap.
	var tables []HBaseTable
	for _, ns := range namespaces {
		tlBody, terr := d.GetRaw(ctx, emrconn.ServiceHBase, masterDNS, "/namespaces/"+ns+"/tables")
		if terr != nil {
			// A namespace listing failure is non-fatal; skip it.
			continue
		}
		names, perr := parseTableList(tlBody)
		if perr != nil {
			continue
		}
		for _, name := range names {
			tables = append(tables, HBaseTable{Namespace: ns, Name: name, Qualified: qualify(ns, name)})
			if len(tables) >= maxHBaseTables {
				break
			}
		}
		if len(tables) >= maxHBaseTables {
			break
		}
	}

	enrichHBaseTables(ctx, d, masterDNS, tables)
	sortHBaseTables(tables)
	return tables, nil
}

// enrichHBaseTables fills each table's region counts, families and derived state
// concurrently. Each goroutine writes its own slice index; a per-table failure
// leaves that table with zero counts (best-effort).
func enrichHBaseTables(ctx context.Context, d *emrconn.Dialer, host string, tables []HBaseTable) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, hbaseEnrichParallel)
	for i := range tables {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			t := &tables[i]
			if body, err := d.GetRaw(ctx, emrconn.ServiceHBase, host, "/"+t.Qualified+"/regions"); err == nil {
				t.Regions, t.Online, _ = parseRegions(body)
			}
			if body, err := d.GetRaw(ctx, emrconn.ServiceHBase, host, "/"+t.Qualified+"/schema"); err == nil {
				t.Families, _ = parseSchema(body)
			}
			t.State = deriveTableState(t.Regions, t.Online)
		}(i)
	}
	wg.Wait()
}

// scannerBatch is how many rows each scanner page returns; scannerCap bounds a
// count so an enormous table can't run unbounded.
const (
	scannerBatch = 1000
	scannerCap   = 5_000_000
)

// scannerSpec is the HBase REST scanner creation body. FirstKeyOnlyFilter
// returns just one cell per row (KeyOnlyFilter then drops its value), so the
// scan transfers as little as possible while still counting every row.
func scannerSpec() []byte {
	return []byte(`{"batch":` + itoa(scannerBatch) +
		`,"caching":` + itoa(scannerBatch) +
		`,"filter":"{\"type\":\"FirstKeyOnlyFilter\"}"}`)
}

// countRowsInBatch counts the rows in one HBase REST CellSet page. Pure.
func countRowsInBatch(body []byte) int {
	var cs struct {
		Row []json.RawMessage `json:"Row"`
	}
	if err := json.Unmarshal(body, &cs); err != nil {
		return 0
	}
	return len(cs.Row)
}

// CountHBaseRows performs an exact row count of a table by opening a REST
// scanner and paging through it. This reads the whole table — it is read-only
// but NOT free — so callers must gate it behind explicit user confirmation. It
// stops at scannerCap (returning capped=true) and respects the context.
func CountHBaseRows(ctx context.Context, d *emrconn.Dialer, masterDNS, qualified string) (count int, capped bool, err error) {
	if d == nil {
		return 0, false, emrconn.ErrDisabled
	}
	if masterDNS == "" {
		return 0, false, fmt.Errorf("%w: cluster has no primary-node DNS", emrconn.ErrUnreachable)
	}

	// Create the scanner.
	resp, err := d.Request(ctx, "PUT", emrconn.ServiceHBase, masterDNS, "/"+qualified+"/scanner/", scannerSpec(), "application/json")
	if err != nil {
		return 0, false, err
	}
	if resp.Status != 201 || resp.Location == "" {
		return 0, false, fmt.Errorf("%w: scanner create returned HTTP %d", emrconn.ErrUnreachable, resp.Status)
	}
	scannerPath := emrconn.PathOf(resp.Location)
	// Best-effort cleanup of the server-side scanner. Failure can't be acted on
	// here, but log it rather than drop it silently (§6a) — a leaked scanner is
	// worth a trace.
	defer func() {
		if _, err := d.Request(context.Background(), "DELETE", emrconn.ServiceHBase, masterDNS, scannerPath, nil, ""); err != nil {
			slog.Warn("HBase scanner cleanup failed", "scanner", scannerPath, "error", err.Error())
		}
	}()

	// Page through until 204 (exhausted), the cap, or context cancellation.
	for {
		select {
		case <-ctx.Done():
			return count, false, ctx.Err()
		default:
		}
		page, perr := d.Request(ctx, "GET", emrconn.ServiceHBase, masterDNS, scannerPath, nil, "")
		if perr != nil {
			return count, false, perr
		}
		if page.Status == 204 { // No more rows.
			return count, false, nil
		}
		if page.Status != 200 {
			return count, false, fmt.Errorf("%w: scanner page returned HTTP %d", emrconn.ErrUnreachable, page.Status)
		}
		n := countRowsInBatch(page.Body)
		if n == 0 {
			return count, false, nil
		}
		count += n
		if count >= scannerCap {
			return count, true, nil
		}
	}
}

func sortHBaseTables(tables []HBaseTable) {
	sort.SliceStable(tables, func(i, j int) bool {
		if tables[i].Namespace != tables[j].Namespace {
			return tables[i].Namespace < tables[j].Namespace
		}
		return tables[i].Name < tables[j].Name
	})
}

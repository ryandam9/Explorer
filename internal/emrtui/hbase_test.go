package emrtui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

func TestParseNamespaces(t *testing.T) {
	ns, err := parseNamespaces([]byte(`{"Namespace":["default","hbase","staging"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ns, []string{"default", "hbase", "staging"}) {
		t.Errorf("got %v", ns)
	}
}

func TestParseTableList(t *testing.T) {
	names, err := parseTableList([]byte(`{"table":[{"name":"orders"},{"name":"customers"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"orders", "customers"}) {
		t.Errorf("got %v", names)
	}
}

func TestParseRegions(t *testing.T) {
	// Two regions assigned, one not (disabled/in-transition).
	body := []byte(`{"name":"orders","Region":[
		{"name":"orders,,1.abc","location":"ip-10-0-0-5.ec2.internal,16020,1"},
		{"name":"orders,k,2.def","location":"ip-10-0-0-6.ec2.internal,16020,1"},
		{"name":"orders,z,3.ghi","location":""}
	]}`)
	total, online, err := parseRegions(body)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || online != 2 {
		t.Errorf("total=%d online=%d, want 3/2", total, online)
	}
}

func TestParseSchema(t *testing.T) {
	fams, err := parseSchema([]byte(`{"name":"orders","ColumnSchema":[{"name":"cf"},{"name":"meta"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fams, []string{"cf", "meta"}) {
		t.Errorf("got %v", fams)
	}
}

func TestDeriveTableState(t *testing.T) {
	cases := []struct {
		total, online int
		want          string
	}{
		{0, 0, "—"},
		{4, 0, "DISABLED"},
		{24, 24, "ENABLED"},
		{24, 22, "PARTIAL"},
	}
	for _, c := range cases {
		if got := deriveTableState(c.total, c.online); got != c.want {
			t.Errorf("deriveTableState(%d,%d) = %q, want %q", c.total, c.online, got, c.want)
		}
	}
}

func TestQualify(t *testing.T) {
	if got := qualify("default", "orders"); got != "orders" {
		t.Errorf("default qualify = %q, want orders", got)
	}
	if got := qualify("staging", "orders"); got != "staging:orders" {
		t.Errorf("ns qualify = %q, want staging:orders", got)
	}
	if got := qualify("", "t"); got != "t" {
		t.Errorf("empty ns qualify = %q, want t", got)
	}
}

func TestFetchHBase_NilDialerIsDisabled(t *testing.T) {
	_, err := FetchHBase(context.Background(), nil, "host")
	if !emrconn.IsUnreachable(err) {
		t.Errorf("nil dialer should be unreachable, got %v", err)
	}
}

func TestSortHBaseTables(t *testing.T) {
	tables := []HBaseTable{
		{Namespace: "staging", Name: "a"},
		{Namespace: "default", Name: "z"},
		{Namespace: "default", Name: "a"},
	}
	sortHBaseTables(tables)
	if tables[0].Namespace != "default" || tables[0].Name != "a" || tables[2].Namespace != "staging" {
		t.Errorf("unexpected order: %+v", tables)
	}
}

func TestScannerSpec(t *testing.T) {
	spec := string(scannerSpec())
	for _, want := range []string{"batch", "caching", "FirstKeyOnlyFilter", "1000"} {
		if !contains(spec, want) {
			t.Errorf("scanner spec missing %q: %s", want, spec)
		}
	}
}

func TestCountRowsInBatch(t *testing.T) {
	body := []byte(`{"Row":[{"key":"a"},{"key":"b"},{"key":"c"}]}`)
	if got := countRowsInBatch(body); got != 3 {
		t.Errorf("countRowsInBatch = %d, want 3", got)
	}
	if got := countRowsInBatch([]byte(`{}`)); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	if got := countRowsInBatch([]byte(`not json`)); got != 0 {
		t.Errorf("bad json = %d, want 0", got)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// A table whose REST reads fail must come back unknown ("—"), not as a real
// "0 regions, no families"; a table whose reads succeed is known.
func TestEnrichHBaseTables_FailureIsUnknownNotZero(t *testing.T) {
	get := func(path string) ([]byte, error) {
		switch path {
		case "/good/regions":
			return []byte(`{"name":"good","Region":[{"name":"r1","location":"h:1"}]}`), nil
		case "/good/schema":
			return []byte(`{"name":"good","ColumnSchema":[{"name":"cf"}]}`), nil
		case "/garbled/regions", "/garbled/schema":
			return []byte(`not json`), nil
		default:
			return nil, errors.New("connection refused")
		}
	}
	tables := []HBaseTable{{Qualified: "good"}, {Qualified: "down"}, {Qualified: "garbled"}}
	enrichHBaseTablesWith(get, tables)

	good := tables[0]
	if !good.RegionsKnown || !good.FamiliesKnown || good.Regions != 1 || good.State != "ENABLED" {
		t.Errorf("good table = %+v, want known, 1 region, ENABLED", good)
	}
	for _, tb := range tables[1:] {
		if tb.RegionsKnown || tb.FamiliesKnown || tb.State != "—" {
			t.Errorf("%s = %+v, want unknown with state —", tb.Qualified, tb)
		}
		row := hbaseRow(tb)
		if row[3] != "—" || row[4] != "—" || row[6] != "—" {
			t.Errorf("%s row = %v, want — for regions/online/families", tb.Qualified, row)
		}
	}
}

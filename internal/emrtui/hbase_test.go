package emrtui

import (
	"context"
	"reflect"
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

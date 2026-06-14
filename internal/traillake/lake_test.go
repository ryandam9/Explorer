package traillake

import (
	"strings"
	"testing"
	"time"
)

func TestStoreID(t *testing.T) {
	cases := map[string]string{
		"arn:aws:cloudtrail:us-east-1:123456789012:eventdatastore/abcd-1234": "abcd-1234",
		"abcd-1234": "abcd-1234",
	}
	for in, want := range cases {
		if got := storeID(in); got != want {
			t.Errorf("storeID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseResultRows(t *testing.T) {
	// CloudTrail Lake returns each row as an ordered list of single-key maps.
	rows := [][]map[string]string{
		{{"eventName": "RunInstances"}, {"events": "12"}},
		{{"eventName": "DeleteBucket"}, {"events": "3"}},
	}
	cols, out := parseResultRows(rows)
	if strings.Join(cols, ",") != "eventName,events" {
		t.Errorf("columns = %v, want [eventName events]", cols)
	}
	if len(out) != 2 || out[0][0] != "RunInstances" || out[0][1] != "12" || out[1][1] != "3" {
		t.Errorf("rows = %v", out)
	}
}

func TestParseResultRowsEmpty(t *testing.T) {
	cols, out := parseResultRows(nil)
	if cols != nil || out != nil {
		t.Errorf("empty input should give nil/nil, got %v / %v", cols, out)
	}
}

func TestWhereClause(t *testing.T) {
	if w := whereClause(Preset{}); w != "" {
		t.Errorf("empty preset should give no WHERE, got %q", w)
	}

	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	w := whereClause(Preset{Since: since, Principal: "alice", ErrorsOnly: true})
	for _, want := range []string{
		"WHERE",
		"eventTime > '2026-06-01 00:00:00'",
		"userIdentity.arn LIKE '%alice%'",
		"errorCode IS NOT NULL",
		" AND ",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("where clause %q missing %q", w, want)
		}
	}
}

func TestWhereClauseEscapesQuotes(t *testing.T) {
	// A single quote in operator input must not break out of the literal.
	w := whereClause(Preset{EventName: "Evil'Name"})
	if !strings.Contains(w, "'Evil''Name'") {
		t.Errorf("single quote not escaped: %q", w)
	}
}

func TestRecentSQL(t *testing.T) {
	sql := RecentSQL("eds-1", Preset{Limit: 10, EventName: "RunInstances"})
	for _, want := range []string{
		"SELECT eventTime, eventName",
		"FROM eds-1",
		"eventName = 'RunInstances'",
		"ORDER BY eventTime DESC",
		"LIMIT 10",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("RecentSQL missing %q:\n%s", want, sql)
		}
	}
}

func TestAggregateSQL(t *testing.T) {
	if sql := TopPrincipalsSQL("eds-1", Preset{}); !strings.Contains(sql, "GROUP BY userIdentity.arn") ||
		!strings.Contains(sql, "COUNT(*) AS events") || !strings.Contains(sql, "LIMIT 50") {
		t.Errorf("TopPrincipalsSQL wrong:\n%s", sql)
	}
	if sql := TopEventsSQL("eds-1", Preset{Limit: 5}); !strings.Contains(sql, "GROUP BY eventName") ||
		!strings.Contains(sql, "LIMIT 5") {
		t.Errorf("TopEventsSQL wrong:\n%s", sql)
	}
}

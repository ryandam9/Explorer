package s3tui

import (
	"reflect"
	"testing"
)

func TestParseLayout(t *testing.T) {
	spec := "# customer record\n" +
		"id,1,5\n" +
		"name, 6 , 10\n" +
		"\n" +
		"amount,16,8\n"
	fields, err := parseLayout(spec)
	if err != nil {
		t.Fatalf("parseLayout: %v", err)
	}
	want := []fixedField{
		{name: "id", start: 1, length: 5},
		{name: "name", start: 6, length: 10},
		{name: "amount", start: 16, length: 8},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("parseLayout = %+v, want %+v", fields, want)
	}
}

func TestParseLayoutErrors(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"comments only":  "# nothing here\n\n",
		"too few fields": "id,1\n",
		"bad start":      "id,x,5\n",
		"zero start":     "id,0,5\n",
		"bad length":     "id,1,-3\n",
		"empty name":     ",1,5\n",
	}
	for name, spec := range cases {
		if _, err := parseLayout(spec); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

func TestBuildFixedRecords(t *testing.T) {
	fields := []fixedField{
		{name: "id", start: 1, length: 3},
		{name: "name", start: 4, length: 6},
		{name: "amt", start: 10, length: 4},
	}
	// total width = 13. Row 2 is short (malformed), row 3 is long (malformed).
	content := "001alice 0100\n" +
		"002bob\n" +
		"003carol 9999EXTRA\n"
	recs, bad := buildFixedRecords(content, fields)

	wantHeader := []string{"!", "id", "name", "amt"}
	if !reflect.DeepEqual(recs[0], wantHeader) {
		t.Fatalf("header = %v, want %v", recs[0], wantHeader)
	}
	if got := recs[1]; !reflect.DeepEqual(got, []string{"", "001", "alice", "0100"}) {
		t.Errorf("row 1 = %v", got)
	}
	// Short line: trailing column is blank, row flagged.
	if got := recs[2]; !reflect.DeepEqual(got, []string{"!", "002", "bob", ""}) {
		t.Errorf("row 2 = %v", got)
	}
	// Long line: data sliced at fixed positions, row flagged.
	if got := recs[3]; !reflect.DeepEqual(got, []string{"!", "003", "carol", "9999"}) {
		t.Errorf("row 3 = %v", got)
	}
	if bad != 2 {
		t.Errorf("badRows = %d, want 2", bad)
	}
}

func TestBuildFixedRecordsStripsBOM(t *testing.T) {
	fields := []fixedField{{name: "a", start: 1, length: 2}}
	recs, _ := buildFixedRecords("\ufeffXY\n", fields)
	if recs[1][1] != "XY" {
		t.Errorf("BOM not stripped: got %q, want %q", recs[1][1], "XY")
	}
}

package emrtui

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigFileFor(t *testing.T) {
	if got := configFileFor("hdfs-site"); got != "/etc/hadoop/conf/hdfs-site.xml" {
		t.Errorf("hdfs-site = %q", got)
	}
	if got := configFileFor("spark-defaults"); got != "/etc/spark/conf/spark-defaults.conf" {
		t.Errorf("spark-defaults = %q", got)
	}
	// Unknown classification falls back, never panics or hides it.
	if got := configFileFor("custom-thing"); got != "custom-thing (classification)" {
		t.Errorf("unknown classification = %q", got)
	}
}

func TestFlattenConfigRows_SortedWithFiles(t *testing.T) {
	cfgs := []ConfigClassification{
		{Classification: "hdfs-site", Properties: map[string]string{"dfs.replication": "2", "dfs.blocksize": "128m"}},
		{Classification: "core-site", Properties: map[string]string{"fs.defaultFS": "hdfs://x"}},
	}
	rows := FlattenConfigRows(cfgs)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// Sorted by classification then key: core-site first, then hdfs-site (blocksize before replication).
	if rows[0].Classification != "core-site" || rows[1].Key != "dfs.blocksize" || rows[2].Key != "dfs.replication" {
		t.Errorf("rows not sorted by classification then key: %+v", rows)
	}
	if rows[1].File != "/etc/hadoop/conf/hdfs-site.xml" {
		t.Errorf("file mapping missing: %+v", rows[1])
	}
}

func TestFilterConfigRows(t *testing.T) {
	rows := FlattenConfigRows([]ConfigClassification{
		{Classification: "hdfs-site", Properties: map[string]string{"a": "1"}},
		{Classification: "core-site", Properties: map[string]string{"b": "2"}},
	})
	got := FilterConfigRows(rows, "hdfs")
	if len(got) != 1 || got[0].Classification != "hdfs-site" {
		t.Errorf("filter hdfs = %+v", got)
	}
	if len(FilterConfigRows(rows, "")) != 2 {
		t.Error("empty filter should keep all rows")
	}
	if len(FilterConfigRows(rows, "nope")) != 0 {
		t.Error("non-matching filter should drop all rows")
	}
}

func TestRenderConfig_CSVNeutralizesInjection(t *testing.T) {
	rows := []ConfigRow{{Classification: "core-site", File: "/etc/hadoop/conf/core-site.xml", Key: "k", Value: "=cmd()"}}
	var buf bytes.Buffer
	if err := RenderConfig(&buf, rows, "csv", false); err != nil {
		t.Fatal(err)
	}
	// A spreadsheet-dangerous value must be prefixed so it isn't run as a formula (§13).
	if strings.Contains(buf.String(), ",=cmd()") {
		t.Errorf("CSV formula injection not neutralized:\n%s", buf.String())
	}
}

func TestRenderConfig_JSONAndNDJSON(t *testing.T) {
	rows := []ConfigRow{
		{Classification: "core-site", File: "/etc/hadoop/conf/core-site.xml", Key: "fs.defaultFS", Value: "hdfs://x"},
	}
	var j bytes.Buffer
	if err := RenderConfig(&j, rows, "json", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j.String(), "\"classification\"") || !strings.Contains(j.String(), "fs.defaultFS") {
		t.Errorf("json output missing fields:\n%s", j.String())
	}
	var nd bytes.Buffer
	if err := RenderConfig(&nd, rows, "ndjson", false); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(nd.String()), "\n"); lines != 0 { // one object → no interior newline
		t.Errorf("ndjson should be one object per line, got %d interior newlines", lines)
	}
}

func TestRenderConfig_TableGroupsByFile(t *testing.T) {
	rows := FlattenConfigRows([]ConfigClassification{
		{Classification: "core-site", Properties: map[string]string{"fs.defaultFS": "hdfs://x"}},
	})
	var buf bytes.Buffer
	if err := RenderConfig(&buf, rows, "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "/etc/hadoop/conf/core-site.xml") || !strings.Contains(out, "fs.defaultFS") {
		t.Errorf("table output missing file header or property:\n%s", out)
	}
}

func TestConfigCountFiles(t *testing.T) {
	rows := FlattenConfigRows([]ConfigClassification{
		{Classification: "core-site", Properties: map[string]string{"a": "1"}},
		{Classification: "hdfs-site", Properties: map[string]string{"b": "2", "c": "3"}},
	})
	if got := configCountFiles(rows); got != 2 {
		t.Errorf("configCountFiles = %d, want 2", got)
	}
}

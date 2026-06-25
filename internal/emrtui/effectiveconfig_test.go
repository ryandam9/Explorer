package emrtui

import (
	"strings"
	"testing"
)

func TestParseEffectiveConfig_JSON(t *testing.T) {
	body := `{"properties":[
	  {"key":"dfs.replication","value":"2","resource":"hdfs-site.xml"},
	  {"key":"fs.defaultFS","value":"hdfs://nn:8020","resource":"core-site.xml"},
	  {"key":"dfs.blocksize","value":"134217728","resource":"hdfs-default.xml"}
	]}`
	rows, err := parseEffectiveConfig([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// Sorted by source (classification) then key: core-site.xml, then the two
	// hdfs-* files (blocksize before replication within hdfs-default? no — sort is
	// by source first: core-site.xml < hdfs-default.xml < hdfs-site.xml).
	if rows[0].Classification != "core-site.xml" || rows[0].File != "core-site.xml" {
		t.Errorf("first row source wrong: %+v", rows[0])
	}
	if rows[1].Classification != "hdfs-default.xml" || rows[2].Classification != "hdfs-site.xml" {
		t.Errorf("rows not sorted by source: %+v", rows)
	}
}

func TestParseEffectiveConfig_XMLFallback(t *testing.T) {
	// A daemon that returns XML (ignoring Accept: application/json) — the last
	// <source> is the effective one.
	body := `<?xml version="1.0"?>
	<configuration>
	  <property><name>fs.defaultFS</name><value>hdfs://nn:8020</value>
	    <source>core-default.xml</source><source>core-site.xml</source></property>
	  <property><name>dfs.replication</name><value>3</value><source>hdfs-site.xml</source></property>
	</configuration>`
	rows, err := parseEffectiveConfig([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	var fsDefault ConfigRow
	for _, r := range rows {
		if r.Key == "fs.defaultFS" {
			fsDefault = r
		}
	}
	if fsDefault.Classification != "core-site.xml" {
		t.Errorf("last source should win: got %q, want core-site.xml", fsDefault.Classification)
	}
	if fsDefault.Value != "hdfs://nn:8020" {
		t.Errorf("value wrong: %q", fsDefault.Value)
	}
}

func TestEffectiveSource(t *testing.T) {
	cases := map[string]string{
		"core-site.xml":             "core-site.xml",
		"":                          "(merged)",
		"programmatically":          "programmatically",
		"/etc/hadoop/conf/yarn.xml": "yarn.xml", // path stripped to basename
		"  hdfs-site.xml  ":         "hdfs-site.xml",
	}
	for in, want := range cases {
		if got := effectiveSource(in); got != want {
			t.Errorf("effectiveSource(%q) = %q, want %q", in, got, want)
		}
	}
}

// The effective rows render through the shared table without the redundant
// "(classification)" suffix (source == file there).
func TestRenderConfig_EffectiveTableNoDupParen(t *testing.T) {
	rows, _ := parseEffectiveConfig([]byte(`{"properties":[{"key":"k","value":"v","resource":"core-site.xml"}]}`))
	var b strings.Builder
	if err := RenderConfig(&b, rows, "table", false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "# core-site.xml") {
		t.Errorf("expected a file header, got:\n%s", out)
	}
	if strings.Contains(out, "(core-site.xml)") {
		t.Errorf("effective header should not repeat the source in parens:\n%s", out)
	}
}

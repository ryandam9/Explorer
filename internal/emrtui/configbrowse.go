package emrtui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/table"
)

// The config browser presents a cluster's EMR configuration classifications as
// the on-disk files they become (core-site.xml, hdfs-site.xml, spark-defaults
// .conf, …) so the setup reads like browsing the cluster's actual config files.
// All data comes from the classifications DescribeCluster already returns — no
// on-cluster access, pure over the API response (fixture-tested).

// configFiles maps an EMR classification to the file it renders to on the
// cluster. Unknown classifications fall back to "<name> (classification)".
var configFiles = map[string]string{
	"core-site":          "/etc/hadoop/conf/core-site.xml",
	"hdfs-site":          "/etc/hadoop/conf/hdfs-site.xml",
	"yarn-site":          "/etc/hadoop/conf/yarn-site.xml",
	"yarn-env":           "/etc/hadoop/conf/yarn-env.sh",
	"mapred-site":        "/etc/hadoop/conf/mapred-site.xml",
	"hadoop-env":         "/etc/hadoop/conf/hadoop-env.sh",
	"capacity-scheduler": "/etc/hadoop/conf/capacity-scheduler.xml",
	"emrfs-site":         "/etc/hadoop/conf/emrfs-site.xml",
	"httpfs-site":        "/etc/hadoop/conf/httpfs-site.xml",
	"container-executor": "/etc/hadoop/conf/container-executor.cfg",
	"spark-defaults":     "/etc/spark/conf/spark-defaults.conf",
	"spark-env":          "/etc/spark/conf/spark-env.sh",
	"spark-hive-site":    "/etc/spark/conf/hive-site.xml",
	"spark-log4j2":       "/etc/spark/conf/log4j2.properties",
	"hive-site":          "/etc/hive/conf/hive-site.xml",
	"hive-env":           "/etc/hive/conf/hive-env.sh",
	"hbase-site":         "/etc/hbase/conf/hbase-site.xml",
	"hbase-env":          "/etc/hbase/conf/hbase-env.sh",
	"tez-site":           "/etc/tez/conf/tez-site.xml",
	"presto-config":      "/etc/presto/conf/config.properties",
	"trino-config":       "/etc/trino/conf/config.properties",
	"oozie-site":         "/etc/oozie/conf/oozie-site.xml",
	"livy-conf":          "/etc/livy/conf/livy.conf",
}

// configFileFor returns the on-disk file an EMR classification renders to.
func configFileFor(classification string) string {
	if f, ok := configFiles[classification]; ok {
		return f
	}
	return classification + " (classification)"
}

// ConfigRow is one configuration property, tagged with its classification and
// the file it lands in — the flat unit the CLI twin and the TUI browser share.
type ConfigRow struct {
	Classification string `json:"classification"`
	File           string `json:"file"`
	Key            string `json:"key"`
	Value          string `json:"value"`
}

// FlattenConfigRows expands classifications to one ConfigRow per property,
// sorted by classification then key for stable, browsable output.
func FlattenConfigRows(cfgs []ConfigClassification) []ConfigRow {
	var rows []ConfigRow
	for _, c := range cfgs {
		for k, v := range c.Properties {
			rows = append(rows, ConfigRow{
				Classification: c.Classification,
				File:           configFileFor(c.Classification),
				Key:            k,
				Value:          v,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Classification != rows[j].Classification {
			return rows[i].Classification < rows[j].Classification
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// FilterConfigRows keeps only rows whose classification matches q
// (case-insensitive substring), so `--classification hdfs` scopes to hdfs-site.
func FilterConfigRows(rows []ConfigRow, q string) []ConfigRow {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return rows
	}
	out := make([]ConfigRow, 0, len(rows))
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Classification), q) {
			out = append(out, r)
		}
	}
	return out
}

// RenderConfig writes a cluster's configuration as table / json / ndjson / csv —
// the CLI twin of the dashboard's config browser.
func RenderConfig(w io.Writer, rows []ConfigRow, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, rows)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Classification", "File", "Key", "Value"})
		}
		for _, r := range rows {
			// Config values are user-influenced text → neutralize CSV formula
			// injection in the spreadsheet-dangerous cells (§13).
			_ = cw.Write(csvexport.SanitizeRow([]string{r.Classification, r.File, r.Key, r.Value}))
		}
		cw.Flush()
		return cw.Error()
	default:
		return renderConfigTable(w, rows, noHeader)
	}
}

// renderConfigTable groups properties under their file for readable browsing.
func renderConfigTable(w io.Writer, rows []ConfigRow, noHeader bool) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "No configuration classifications on this cluster (EMR defaults are in effect).")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	lastFile := ""
	for _, r := range rows {
		if r.File != lastFile {
			if lastFile != "" {
				fmt.Fprintln(tw)
			}
			if noHeader {
				lastFile = r.File
			} else {
				fmt.Fprintf(tw, "# %s  (%s)\n", r.File, r.Classification)
				lastFile = r.File
			}
		}
		fmt.Fprintf(tw, "  %s\t%s\n", r.Key, dash(r.Value))
	}
	return tw.Flush()
}

// configCountFiles counts the distinct files represented in rows (for the
// browser footer).
func configCountFiles(rows []ConfigRow) int {
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.File] = true
	}
	return len(seen)
}

// --- TUI browser table -------------------------------------------------------

func configColumns() []table.Column {
	return []table.Column{
		{Title: "CLASSIFICATION", Width: 18},
		{Title: "KEY", Width: 34},
		{Title: "VALUE", Width: 40},
	}
}

func configTableRow(r ConfigRow) table.Row {
	return table.Row{r.Classification, r.Key, r.Value}
}

func (mm *m) selectedConfigRow() (ConfigRow, bool) {
	i := mm.configTbl.Cursor()
	if i < 0 || i >= len(mm.configRows) {
		return ConfigRow{}, false
	}
	return mm.configRows[i], true
}

package display

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// FieldMeta describes one displayable attribute of a resource type.
type FieldMeta struct {
	Key           string
	Title         string
	Width         int
	DefaultCol    bool // included in table by default
	DefaultDetail bool // included in detail panel by default
}

// ResolveColumns returns the fields to show as table columns.
// When requested is empty the DefaultCol fields are used.
func ResolveColumns(fields []FieldMeta, requested []string) []FieldMeta {
	if len(requested) == 0 {
		var out []FieldMeta
		for _, f := range fields {
			if f.DefaultCol {
				out = append(out, f)
			}
		}
		return out
	}
	return pick(fields, requested)
}

// ResolveDetail returns the fields to show in the detail panel.
// When requested is empty the DefaultDetail fields are used.
func ResolveDetail(fields []FieldMeta, requested []string) []FieldMeta {
	if len(requested) == 0 {
		var out []FieldMeta
		for _, f := range fields {
			if f.DefaultDetail {
				out = append(out, f)
			}
		}
		return out
	}
	return pick(fields, requested)
}

// Columns converts resolved fields to a table.Column slice.
// A leading "#" column is always prepended.
func Columns(fields []FieldMeta) []table.Column {
	cols := make([]table.Column, 0, len(fields)+1)
	cols = append(cols, table.Column{Title: "#", Width: 4})
	for _, f := range fields {
		cols = append(cols, table.Column{Title: f.Title, Width: f.Width})
	}
	return cols
}

// Row builds a table.Row from a resource map.
// Position 0 is left empty for the sequence number (filled later by seqRows).
func Row(fields []FieldMeta, r map[string]string) table.Row {
	row := make(table.Row, len(fields)+1)
	row[0] = ""
	for i, f := range fields {
		v := r[f.Key]
		if v == "" {
			v = "-"
		}
		row[i+1] = v
	}
	return row
}

// Detail builds the detail-panel lines from a resource map.
// The special "tags" key is expanded into sorted key=value lines.
// Keys whose value starts with "\n" are treated as pre-formatted multi-line blocks.
func Detail(fields []FieldMeta, r map[string]string) []string {
	var lines []string
	for _, f := range fields {
		v := r[f.Key]
		switch {
		case f.Key == "tags":
			lines = append(lines, expandTags(v)...)
		case strings.HasPrefix(v, "\n"):
			lines = append(lines, "  "+f.Title+":")
			for _, line := range strings.Split(strings.TrimPrefix(v, "\n"), "\n") {
				lines = append(lines, "  "+line)
			}
		default:
			if v == "" {
				v = "-"
			}
			lines = append(lines, dl(f.Title, v))
		}
	}
	return lines
}

// EncodeTags converts a tag map to the newline-separated "key=value" format
// used as the "tags" value in resource maps.
func EncodeTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(tags))
	for _, k := range keys {
		parts = append(parts, k+"="+tags[k])
	}
	return strings.Join(parts, "\n")
}

// UnknownKeys returns keys in requested that are not registered in fields.
// Used at startup to warn about typos in config.
func UnknownKeys(fields []FieldMeta, requested []string) []string {
	idx := fieldIndex(fields)
	var bad []string
	for _, k := range requested {
		if _, ok := idx[k]; !ok {
			bad = append(bad, k)
		}
	}
	return bad
}

// AvailableKeys returns all registered field keys.
func AvailableKeys(fields []FieldMeta) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.Key
	}
	return out
}

// --- helpers -----------------------------------------------------------------

func pick(fields []FieldMeta, keys []string) []FieldMeta {
	idx := fieldIndex(fields)
	out := make([]FieldMeta, 0, len(keys))
	for _, k := range keys {
		if f, ok := idx[k]; ok {
			out = append(out, f)
		}
	}
	return out
}

func fieldIndex(fields []FieldMeta) map[string]FieldMeta {
	m := make(map[string]FieldMeta, len(fields))
	for _, f := range fields {
		m[f.Key] = f
	}
	return m
}

func dl(key, val string) string {
	return fmt.Sprintf("  %-24s %s", key, val)
}

func expandTags(raw string) []string {
	if raw == "" {
		return []string{dl("Tags", "-")}
	}
	lines := []string{"  Tags:"}
	for _, pair := range strings.Split(raw, "\n") {
		if pair != "" {
			lines = append(lines, "    "+pair)
		}
	}
	return lines
}

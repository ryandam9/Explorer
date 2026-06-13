package s3tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/table"
)

func formatSize(size int64) string {
	switch {
	case size >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
	case size >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func parseSize(value string) int64 {
	parts := strings.Fields(value)
	if len(parts) == 0 || value == "-" {
		return 0
	}
	n, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	unit := "B"
	if len(parts) > 1 {
		unit = strings.ToUpper(parts[1])
	}
	switch unit {
	case "GB", "GIB":
		n *= 1024 * 1024 * 1024
	case "MB", "MIB":
		n *= 1024 * 1024
	case "KB", "KIB":
		n *= 1024
	}
	return int64(n)
}

func parentPrefix(prefix string) string {
	parts := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/") + "/"
}

// breadcrumb joins the bucket and prefix segments into a "bucket / seg / seg/"
// path, truncating from the left when wider than maxW so the trailing
// components stay visible.
func breadcrumb(bucket, prefix string, maxW int) string {
	crumb := bucket
	if prefix != "" {
		segs := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
		segs[len(segs)-1] += "/"
		crumb += " / " + strings.Join(segs, " / ")
	}
	if r := []rune(crumb); maxW > 1 && len(r) > maxW {
		crumb = "…" + string(r[len(r)-maxW+1:])
	}
	return crumb
}

func displayPrefix(prefix string) string {
	if prefix == "" {
		return "<root>"
	}
	return prefix
}

// seqRows returns a new slice of rows where the first element of each row is
// replaced with its 1-based sequence number. The source rows are not modified.
func seqRows(rows []table.Row) []table.Row {
	out := make([]table.Row, len(rows))
	for i, r := range rows {
		nr := make(table.Row, len(r))
		copy(nr, r)
		nr[0] = fmt.Sprintf("%d", i+1)
		out[i] = nr
	}
	return out
}

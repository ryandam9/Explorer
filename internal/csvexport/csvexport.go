// Package csvexport writes the current view of any TUI table to a timestamped
// CSV file, so a screen's contents can be attached to a ticket or shared
// without re-running the scan.
package csvexport

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultDir returns the directory exports are written to
// (~/.aws_explorer/exports), creating it if needed.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".aws_explorer", "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create export directory: %w", err)
	}
	return dir, nil
}

// Write writes header+rows as RFC 4180 CSV to <dir>/<name>-<timestamp>.csv
// and returns the full path. name is sanitized for use in a filename.
func Write(dir, name string, header []string, rows [][]string) (string, error) {
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.csv", sanitize(name), time.Now().Format("20060102-150405")))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(SanitizeRow(header)); err != nil {
		return "", err
	}
	for _, row := range rows {
		if err := w.Write(SanitizeRow(row)); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return path, nil
}

// Sanitize neutralizes CSV formula injection: spreadsheet apps (Excel, Sheets,
// LibreOffice) evaluate a cell whose first character is '=', '+', '-', '@', or
// a leading tab/carriage return as a formula. Since these exports are meant to
// be opened in a spreadsheet, a value beginning with one of those characters
// is prefixed with a single quote so it renders literally. Other values are
// returned unchanged. RFC-4180 quoting (commas/quotes/newlines) is handled by
// encoding/csv and is unaffected.
func Sanitize(field string) string {
	if field == "" {
		return field
	}
	switch field[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + field
	}
	return field
}

// SanitizeRow returns a copy of row with every field passed through Sanitize.
func SanitizeRow(row []string) []string {
	out := make([]string, len(row))
	for i, f := range row {
		out[i] = Sanitize(f)
	}
	return out
}

// sanitize keeps a name filesystem-friendly.
func sanitize(name string) string {
	if name == "" {
		return "export"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)
}

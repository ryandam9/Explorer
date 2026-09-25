package s3tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// layoutModel is a sized S3 model with a full bucket list and object list.
func layoutModel(width, height int) *Model {
	m := &Model{bucket: "b", region: "us-east-2", detailBucket: "b"}
	m.initBucketTable()
	m.initObjectTable()
	var buckets []table.Row
	for i := 0; i < 80; i++ {
		buckets = append(buckets, table.Row{"", fmt.Sprintf("bucket-%d", i), "us-east-1", "2024-01-01"})
		m.objectMaps = append(m.objectMaps, map[string]string{"name": fmt.Sprintf("key-%d", i), "type": "FILE"})
	}
	m.bucketTable.SetRows(seqRows(buckets))
	m.objectTable.SetRows(m.buildObjectRows())
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

// Every S3 screen fills the terminal exactly: the status bar sits on the last
// row (no unused rows under it) and the screen's content ends with its panel's
// bottom border (nothing clipped to make room).
func TestStatusBarPinnedToBottomRow(t *testing.T) {
	screens := []struct {
		name  string
		setup func(m *Model)
	}{
		{"bucket list", func(m *Model) {}},
		{"object list", func(m *Model) { m.state = stateObjectList }},
		{"bucket detail", func(m *Model) { m.state = stateBucketDetail }},
		{"error", func(m *Model) { m.err = errors.New("access denied") }},
	}
	for _, h := range []int{30, 40, 54} {
		for _, sc := range screens {
			m := layoutModel(160, h)
			sc.setup(m)
			m.Update(nil) // refit for the new state
			lines := strings.Split(m.viewString(), "\n")
			if len(lines) != h {
				t.Errorf("%s at height %d: frame has %d rows, want %d", sc.name, h, len(lines), h)
				continue
			}
			last := ansi.Strip(lines[h-1])
			if !strings.Contains(last, "quit") && !strings.Contains(last, "help") {
				t.Errorf("%s at height %d: last row is not the status bar: %q", sc.name, h, last)
			}
			// The row above the status bar is the separator; above it (after
			// any padding) the content must end on a panel's bottom border.
			i := h - 2
			for i > 0 && strings.TrimSpace(ansi.Strip(lines[i])) == "" {
				i--
			}
			if !strings.Contains(ansi.Strip(lines[i]), "╯") {
				t.Errorf("%s at height %d: content is clipped, last content row %q", sc.name, h, ansi.Strip(lines[i]))
			}
		}
	}
}

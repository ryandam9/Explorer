package s3tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Any text preview (e.g. a .txt) can be viewed as a table on demand with "t";
// non-delimited content stays as text and is flagged.
func TestTextPreviewToTableToggle(t *testing.T) {
	delimited := &Model{width: 100, height: 30, showPreview: true, previewKey: "data.txt",
		previewContent: "id,name,amount\n1,alice,100\n2,bob,200\n"}
	delimited.initPreviewViewport(delimited.previewContent, nil)
	delimited.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if delimited.showPreview || !delimited.showCSV {
		t.Fatalf("t should switch delimited text to the table: showPreview=%v showCSV=%v",
			delimited.showPreview, delimited.showCSV)
	}

	prose := &Model{width: 100, height: 30, showPreview: true, previewKey: "notes.txt",
		previewContent: "Just some prose.\nNothing tabular here.\n"}
	prose.initPreviewViewport(prose.previewContent, nil)
	prose.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if prose.showCSV || !prose.showPreview {
		t.Errorf("prose should stay as text: showCSV=%v showPreview=%v", prose.showCSV, prose.showPreview)
	}
	if !prose.previewNotTabular {
		t.Error("prose should set previewNotTabular so the UI can say so")
	}
}

package s3tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// applyArchiveLoaded stores a loaded archive and builds the member table.
func (m *Model) applyArchiveLoaded(msg archiveLoadedMsg) {
	m.archiveLoading = false
	m.archiveErr = msg.err
	if msg.err != nil {
		return
	}
	m.archiveData = msg.data
	m.archiveMembers = msg.members
	m.archiveTruncated = msg.truncated
	m.buildArchiveTable()
}

// buildArchiveTable builds the member-list table (regular files only; the dir
// entries are kept for the count but aren't openable rows).
func (m *Model) buildArchiveTable() {
	cols := []table.Column{
		{Title: "#", Width: 4},
		{Title: "Member", Width: 30},
		{Title: "Size", Width: 12},
	}
	rows := make([]table.Row, 0, len(m.fileMembers()))
	for i, mem := range m.fileMembers() {
		rows = append(rows, table.Row{fmt.Sprintf("%d", i+1), mem.Name, formatSize(mem.Size)})
	}
	m.archiveTable = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1),
	)
	m.layoutArchiveTable()
}

// fileMembers returns just the regular-file members (the openable ones).
func (m *Model) fileMembers() []archiveMember {
	out := make([]archiveMember, 0, len(m.archiveMembers))
	for _, mem := range m.archiveMembers {
		if !mem.Dir {
			out = append(out, mem)
		}
	}
	return out
}

func (m *Model) layoutArchiveTable() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.archiveTable.SetWidth(m.tableViewWidth())
	h := m.height - 9
	if h < 3 {
		h = 3
	}
	m.archiveTable.SetHeight(h)
}

// openArchiveMember extracts the selected member from the in-memory archive and
// shows it as a CSV table or text, remembering to return to the archive list.
func (m *Model) openArchiveMember() {
	files := m.fileMembers()
	idx := m.archiveTable.Cursor()
	if idx < 0 || idx >= len(files) {
		return
	}
	name := files[idx].Name
	content, truncated, err := tarMemberContent(m.archiveData, name, memberPreviewCap)

	m.previewKey = m.archiveKey + " › " + name
	m.previewFromArchive = true
	m.previewLoading = false
	m.showArchive = false

	if err != nil {
		m.previewErr = err
		m.showPreview = true
		m.showCSV = false
		m.initPreviewViewport("", err)
		return
	}
	m.previewErr = nil
	text := decompressedPreview(content, truncated, looksLikeCSV(name))
	m.previewContent = text

	if looksLikeCSV(name) && m.initCSV(text) {
		m.showCSV = true
		m.showPreview = false
		return
	}
	m.showPreview = true
	m.showCSV = false
	m.initPreviewViewport(text, nil)
}

// closeArchive leaves the archive browser, freeing the in-memory archive.
func (m *Model) closeArchive() {
	m.showArchive = false
	m.archiveData = nil
	m.archiveMembers = nil
}

// handleArchiveKey routes a key press in the archive browser; returns true when
// consumed.
func (m *Model) handleArchiveKey(key string) bool {
	switch key {
	case "esc", "q":
		m.closeArchive()
		return true
	case "enter", "p", "right", "l":
		m.openArchiveMember()
		return true
	case "<", ",":
		m.archiveTable.ScrollLeft()
		return true
	case ">", ".":
		m.archiveTable.ScrollRight()
		return true
	}
	return false
}

// archiveView renders the full-screen archive member browser.
func (m *Model) archiveView() string {
	title := ui.PanelTitleStyle().Render("ARCHIVE: " + m.archiveKey)

	if m.archiveLoading {
		return lipgloss.JoinVertical(lipgloss.Left, title, "", m.loadingLine("Downloading and reading archive…"))
	}
	if m.archiveErr != nil {
		return lipgloss.JoinVertical(lipgloss.Left, title, "",
			ui.ErrorStyle().Render("Could not read archive: "+summarizeS3Error(m.archiveErr)))
	}

	files := m.fileMembers()
	if len(files) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title, "", ui.MutedStyle().Render("No files in this archive."))
	}

	info := ui.MutedStyle().Render(m.archiveInfoLine())
	panel := ui.TablePanelStyle(true).Render(m.archiveTable.View())
	scroll := ui.TableScrollIndicator(&m.archiveTable)
	hints := ui.MutedStyle().Render(
		"[↑/↓] select   [Enter] open file   [←/→] columns   [Esc] close")

	parts := []string{title, info, panel}
	if scroll != "" {
		parts = append(parts, scroll)
	}
	parts = append(parts, hints)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) archiveInfoLine() string {
	files := len(m.fileMembers())
	dirs := len(m.archiveMembers) - files
	line := fmt.Sprintf("%d file(s)", files)
	if dirs > 0 {
		line += fmt.Sprintf(" · %d folder(s)", dirs)
	}
	if m.archiveTruncated {
		line += "   ·   archive truncated — some members may be missing"
	}
	return line
}

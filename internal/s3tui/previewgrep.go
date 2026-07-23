package s3tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// Grep filter inside the object preview overlay ("&"), mirroring the CloudWatch
// log viewer's grep (internal/cwtui/logviewer.go): only lines matching the
// typed regex are rendered — the in-preview equivalent of piping through
// grep(1). The pattern live-applies while typed; an invalid in-progress regex
// is flagged but keeps the last valid filter, so a half-typed pattern never
// blanks the screen. Enter keeps the filter applied, Esc (in the input) clears
// it. The filter drops whole *logical* (unwrapped) lines before the display
// wrap, so a long matching line keeps all its wrapped continuations. The "/"
// search then operates on the filtered lines, exactly as in the log viewer.

// previewGrepVisible reports whether the grep bar occupies a line of the
// preview panel: while typing a pattern, and while a filter is applied.
func (m *Model) previewGrepVisible() bool {
	return m.previewGrepActive || m.previewGrepRe != nil
}

// rebuildPreviewLines regenerates the wrapped display lines from the formatted
// source: the grep filter drops non-matching logical lines (matched on plain
// text, so ANSI colouring never hides a match), the survivors are hard-wrapped
// to the viewport, and the search matches are recomputed for the active term —
// the log viewer's rebuild(). The truncation note stays appended whenever the
// preview window cut the object, filter or not, so a cap never hides itself.
func (m *Model) rebuildPreviewLines() {
	m.previewGrepTotal = len(m.previewSrcPlain)
	kept := m.previewSrc
	if m.previewGrepRe != nil {
		kept = make([]string, 0, len(m.previewSrc))
		for i, plain := range m.previewSrcPlain {
			if m.previewGrepRe.MatchString(plain) {
				kept = append(kept, m.previewSrc[i])
			}
		}
	}
	m.previewGrepKept = len(kept)

	display := ""
	if len(kept) > 0 {
		wrapW := max(10, m.previewViewport.Width-previewGutterWidth)
		display = ansi.Hardwrap(strings.Join(kept, "\n"), wrapW, false)
	}
	if m.previewTruncated {
		display += "\n\n… preview truncated …"
	}

	m.previewLines = nil
	m.previewPlain = nil
	if display != "" {
		m.previewLines = strings.Split(display, "\n")
		m.previewPlain = make([]string, len(m.previewLines))
		for i, line := range m.previewLines {
			m.previewPlain[i] = ansi.Strip(line)
		}
	}

	m.previewMatches = computePreviewMatches(m.previewPlain, m.previewSearchTerm)
	if m.previewMatchIdx >= len(m.previewMatches) {
		m.previewMatchIdx = 0
	}
	m.refreshPreviewContent()
}

// setPreviewGrep live-applies the grep pattern (the log viewer's setGrep). An
// empty pattern clears the filter; a pattern that doesn't compile is flagged
// but keeps the last valid filter applied.
func (m *Model) setPreviewGrep(pattern string) {
	if strings.TrimSpace(pattern) == "" {
		m.previewGrepRe, m.previewGrepErr = nil, ""
		m.rebuildPreviewLines()
		return
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		m.previewGrepErr = "invalid regex"
		return
	}
	m.previewGrepRe, m.previewGrepErr = re, ""
	m.rebuildPreviewLines()
}

// startPreviewGrep gives the grep input the keyboard ("&"). A previous pattern
// stays in the input for editing.
func (m *Model) startPreviewGrep() {
	m.previewGrepActive = true
	m.previewGrepInput.CursorEnd()
	m.previewGrepInput.Focus()
	m.syncPreviewViewportHeight()
}

// acceptPreviewGrep (Enter) keeps the filter applied and returns the keyboard
// to the preview; an empty pattern means no filter.
func (m *Model) acceptPreviewGrep() {
	m.previewGrepActive = false
	m.previewGrepInput.Blur()
	m.setPreviewGrep(m.previewGrepInput.Value())
	m.syncPreviewViewportHeight()
}

// cancelPreviewGrep (Esc in the input) clears the filter and the input's text,
// restoring the full content.
func (m *Model) cancelPreviewGrep() {
	m.previewGrepActive = false
	m.previewGrepInput.Blur()
	m.previewGrepInput.SetValue("")
	m.setPreviewGrep("")
	m.syncPreviewViewportHeight()
}

// syncPreviewViewportHeight resizes the viewport for the grep bar's
// visibility — the bar takes one line of the panel while shown, like the log
// viewer's viewerBodyHeight.
func (m *Model) syncPreviewViewportHeight() {
	_, h := m.previewViewportSize()
	m.previewViewport.Height = h
}

// previewGrepLine renders the grep bar shown under the Find line while the
// filter is typed or applied ("" when the bar is hidden) — the same states as
// the log viewer's grep line.
func (m *Model) previewGrepLine() string {
	switch {
	case m.previewGrepActive:
		line := "Grep: " + m.previewGrepInput.View()
		if m.previewGrepErr != "" {
			line += "  " + ui.ErrorStyle().Render("("+m.previewGrepErr+")")
		}
		return line
	case m.previewGrepRe != nil:
		return fmt.Sprintf("Grep: %s  (%d of %d lines, & to edit)",
			lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(m.previewGrepRe.String()),
			m.previewGrepKept, m.previewGrepTotal)
	default:
		return ""
	}
}

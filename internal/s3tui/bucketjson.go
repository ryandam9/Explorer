package s3tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// prettyJSON re-indents a JSON document for readable display. Input that does
// not parse as JSON (e.g. an already-formatted CORS dump or a stray message) is
// returned unchanged so the viewer still shows something useful.
func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// bucketDetailPanelSize is the panel size shared by the bucket detail view and
// the JSON viewer overlaid on it, so the two line up.
func (m *Model) bucketDetailPanelSize() (width, height int) {
	return max(60, m.width-8), max(20, m.height-10)
}

// openBucketJSON sets up the full-screen JSON viewer with pretty-printed,
// hard-wrapped, scrollable content. raw is the document to show; when it is
// empty, emptyMsg explains why (no policy, access denied, …).
func (m *Model) openBucketJSON(title, raw, emptyMsg string) {
	// plain is the text kept for copy (no ANSI); display is syntax-highlighted.
	plain, display := emptyMsg, emptyMsg
	if strings.TrimSpace(raw) != "" {
		plain = prettyJSON(raw)
		display = ui.HighlightLang(plain, "json")
	}

	width, height := m.bucketDetailPanelSize()
	vpW := width - 6 // border + padding + scrollbar gutter
	vpH := height - 8
	if vpW < 10 {
		vpW = 10
	}
	if vpH < 2 {
		vpH = 2
	}
	vp := viewport.New(vpW, vpH)
	vp.SetContent(ansi.Hardwrap(display, vpW, false)) // ANSI-aware so highlight survives

	m.bucketJSONViewport = vp
	m.bucketJSONTitle = title
	m.bucketJSONContent = plain // copy yields clean JSON
	m.bucketJSONNote = ""
	m.showBucketJSON = true
}

// openBucketPolicyJSON shows the bucket policy, or a reason when there is none.
func (m *Model) openBucketPolicyJSON() {
	d := m.selectedBucketDetails
	if d == nil {
		return
	}
	msg := "No bucket policy is set."
	if d.Policy == "Access Denied" {
		msg = "Access denied: not permitted to read the bucket policy (s3:GetBucketPolicy)."
	}
	m.openBucketJSON("BUCKET POLICY: "+m.detailBucket, d.RawPolicy, msg)
}

// openBucketCORSJSON shows the CORS configuration, or a reason when there is none.
func (m *Model) openBucketCORSJSON() {
	d := m.selectedBucketDetails
	if d == nil {
		return
	}
	msg := "No CORS configuration is set."
	if d.CORS == "Access Denied" {
		msg = "Access denied: not permitted to read the CORS configuration (s3:GetBucketCors)."
	}
	m.openBucketJSON("CORS CONFIGURATION: "+m.detailBucket, d.CORSJSON, msg)
}

// copyBucketJSON copies the viewer's content to the clipboard, reporting the
// outcome in the viewer's hint line.
func (m *Model) copyBucketJSON() {
	if strings.TrimSpace(m.bucketJSONContent) == "" {
		m.bucketJSONNote = "Nothing to copy"
		return
	}
	if err := clipboard.WriteAll(m.bucketJSONContent); err != nil {
		m.bucketJSONNote = "Copy failed: " + err.Error()
		return
	}
	m.bucketJSONNote = "Copied to clipboard"
}

// bucketJSONView renders the full-screen JSON viewer overlaid on the bucket
// detail view.
func (m *Model) bucketJSONView() string {
	width, height := m.bucketDetailPanelSize()
	title := ui.PanelTitleStyle().Render(m.bucketJSONTitle)

	bar := ui.VScrollbar(
		m.bucketJSONViewport.Height,
		m.bucketJSONViewport.TotalLineCount(),
		m.bucketJSONViewport.VisibleLineCount(),
		m.bucketJSONViewport.YOffset,
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.bucketJSONViewport.View(), " ", bar)

	hint := ui.MutedStyle().Render("[↑/↓/PgUp/PgDn] Scroll   [y] Copy   [Esc] Back")
	if m.bucketJSONNote != "" {
		hint = ui.SuccessStyle().Render("✓ "+m.bucketJSONNote) + "   " + ui.MutedStyle().Render("[Esc] Back")
	}

	panel := lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width+2).
		MaxHeight(height+2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Foreground(lipgloss.Color(ui.ColorText())).
		Padding(1, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", hint))

	return lipgloss.JoinVertical(lipgloss.Left,
		ui.HeaderStyle().Render("S3 TUI"),
		ui.FeatherRail(max(12, m.width-4)),
		"",
		panel,
		"",
		m.renderStatusBar(),
	)
}

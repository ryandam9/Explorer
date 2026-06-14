// Package debugpane is a reusable debug activity overlay shared by every TUI.
//
// It wraps the in-memory debuglog sink (where scan activity is captured) in a
// compact, scrollable, HUD-style panel toggled with "~". Each TUI embeds a
// Model, forwards input to it while it is visible, refreshes it on spinner
// ticks, and composites it over its frame — so the pane looks and behaves
// identically everywhere without each screen reimplementing it.
package debugpane

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/debuglog"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// Model is the embeddable debug-pane state. The zero value is a closed pane.
type Model struct {
	visible bool
	vp      viewport.Model
}

// Visible reports whether the pane is currently open.
func (m Model) Visible() bool { return m.visible }

// Open (re)builds the pane sized to the terminal and scrolls to the latest
// line. Kept compact (~15 lines) so the live screen stays visible behind it;
// the user scrolls for the rest.
func (m *Model) Open(width, height int) {
	w := width - 20
	if w > 110 {
		w = 110
	}
	if w < 40 {
		w = 40
	}
	h := 15
	if mx := height - 12; h > mx {
		h = mx
	}
	if h < 6 {
		h = 6
	}
	m.vp = viewport.New(w, h)
	m.vp.SetContent(body())
	m.vp.GotoBottom()
	m.visible = true
}

// Close hides the pane.
func (m *Model) Close() { m.visible = false }

// HandleInput processes a message while the pane is open. It returns true when
// the message was a key or mouse event — which the pane consumes (scrolling or
// closing) — so the caller stops processing it. It returns false for every
// other message (scan chunks, completion, spinner ticks) so the caller keeps
// handling those and the scan underneath keeps progressing.
func (m *Model) HandleInput(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", ui.KeyDebug, "q":
			m.visible = false
		case "up", "k", "[":
			m.vp.SetContent(body())
			m.vp.LineUp(3)
		case "down", "j", "]":
			m.vp.SetContent(body())
			m.vp.LineDown(3)
		case "g":
			m.vp.SetContent(body())
			m.vp.GotoTop()
		case "G":
			m.vp.SetContent(body())
			m.vp.GotoBottom()
		}
		// Swallow every key while open so none leaks to the screen beneath.
		return true
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.vp.SetContent(body())
			m.vp.LineUp(3)
		case tea.MouseButtonWheelDown:
			m.vp.SetContent(body())
			m.vp.LineDown(3)
		}
		return true
	}
	return false
}

// Refresh updates the pane with the latest captured activity, tail-following
// only when already at the bottom so scrolling up to read earlier lines is not
// yanked back down. A no-op when the pane is closed; call it on spinner ticks.
func (m *Model) Refresh() {
	if !m.visible {
		return
	}
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(body())
	if atBottom {
		m.vp.GotoBottom()
	}
}

// Overlay composites the pane, centered, over the given full-frame view. Pass
// the TUI's normal rendered frame plus its terminal width/height.
func (m Model) Overlay(base string, width, height int) string {
	if !m.visible {
		return base
	}
	return ui.OverlayCenter(base, m.view(), width, height)
}

func body() string {
	return ui.DebugBody(debuglog.Default.Entries(), debuglog.Default.Dropped())
}

func (m Model) view() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf("DEBUG · TOOL ACTIVITY (%d lines)", debuglog.Default.Len()))
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("↑/↓ scroll · g/G top/bottom · Esc/~ close")
	bar := ui.VScrollbar(m.vp.Height, m.vp.TotalLineCount(), m.vp.VisibleLineCount(), m.vp.YOffset)
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.vp.View(), " ", bar)
	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(0, 1).
		Render(inner)
}

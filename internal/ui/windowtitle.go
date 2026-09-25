package ui

import tea "github.com/charmbracelet/bubbletea"

// Titled is a tea.Model that can name the screen it is currently showing.
type Titled interface {
	tea.Model
	PageTitle() string
}

// WithWindowTitle wraps a model so the terminal window/tab title always names
// the screen being shown (e.g. "VPC Explorer › my-vpc › Subnets"). Every page
// gets a unique, shareable title, which makes "which screen are you on?"
// answerable when several people use or discuss the tool.
//
// Every TUI's program is built around this wrapper, so it is also where the
// finished frame gets the painted background (ui.paintBackground; see Paint).
// Models that don't implement Titled get the painting only.
func WithWindowTitle(m tea.Model) tea.Model {
	if t, ok := m.(Titled); ok {
		return &titledModel{inner: t}
	}
	return &paintedModel{inner: m}
}

type titledModel struct {
	inner Titled
	last  string
	size  tea.WindowSizeMsg
}

// paintedModel paints the frame of a model that has no page title.
type paintedModel struct {
	inner tea.Model
	size  tea.WindowSizeMsg
}

func (p *paintedModel) Init() tea.Cmd { return p.inner.Init() }

func (p *paintedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		p.size = ws
	}
	var cmd tea.Cmd
	p.inner, cmd = p.inner.Update(msg)
	return p, cmd
}

func (p *paintedModel) View() string { return Paint(p.inner.View(), p.size.Width, p.size.Height) }

func (t *titledModel) Init() tea.Cmd {
	t.last = t.inner.PageTitle()
	return tea.Batch(t.inner.Init(), tea.SetWindowTitle(t.last))
}

func (t *titledModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		t.size = ws
	}
	mm, cmd := t.inner.Update(msg)
	inner, ok := mm.(Titled)
	if !ok {
		// The inner model swapped itself for something untitled; stop syncing
		// the title but keep painting its frames.
		return &paintedModel{inner: mm, size: t.size}, cmd
	}
	t.inner = inner
	if title := inner.PageTitle(); title != t.last {
		t.last = title
		cmd = tea.Batch(cmd, tea.SetWindowTitle(title))
	}
	return t, cmd
}

func (t *titledModel) View() string {
	return Paint(t.inner.View(), t.size.Width, t.size.Height)
}

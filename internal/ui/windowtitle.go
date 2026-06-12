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
// answerable when several people use or discuss the tool. Models that don't
// implement Titled are returned unchanged.
func WithWindowTitle(m tea.Model) tea.Model {
	if t, ok := m.(Titled); ok {
		return &titledModel{inner: t}
	}
	return m
}

type titledModel struct {
	inner Titled
	last  string
}

func (t *titledModel) Init() tea.Cmd {
	t.last = t.inner.PageTitle()
	return tea.Batch(t.inner.Init(), tea.SetWindowTitle(t.last))
}

func (t *titledModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, cmd := t.inner.Update(msg)
	inner, ok := mm.(Titled)
	if !ok {
		// The inner model swapped itself for something untitled; stop syncing.
		return mm, cmd
	}
	t.inner = inner
	if title := inner.PageTitle(); title != t.last {
		t.last = title
		cmd = tea.Batch(cmd, tea.SetWindowTitle(title))
	}
	return t, cmd
}

func (t *titledModel) View() string { return t.inner.View() }

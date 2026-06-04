package tui

import (
	"context"
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/aws_explorer/internal/engine"
	"github.com/user/aws_explorer/internal/model"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("#E4E4FF"))

type tuiModel struct {
	ctx       context.Context
	engine    *engine.Engine
	results   []model.Resource
	errors    []model.ExploreError
	loading   bool
	activeTab int
	tables    []table.Model
	chunks    chan model.ResultChunk
	done      bool
	width     int
	height    int
}

type chunkMsg model.ResultChunk
type doneMsg struct{}

// NewModel returns a new Bubble Tea model for the TUI.
func NewModel(ctx context.Context, eng *engine.Engine) tea.Model {
	chunks := make(chan model.ResultChunk, 64)
	return tuiModel{
		ctx:     ctx,
		engine:  eng,
		loading: true,
		chunks:  chunks,
	}
}

func (m tuiModel) Init() tea.Cmd {
	go m.engine.StreamRun(m.ctx, m.chunks)
	return waitForChunk(m.chunks)
}

func waitForChunk(chunks <-chan model.ResultChunk) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-chunks
		if !ok {
			return doneMsg{}
		}
		return chunkMsg(chunk)
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateTableLayout()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			if len(m.tables) > 0 {
				m.activeTab = (m.activeTab + 1) % len(m.tables)
			}
		case "shift+tab":
			if len(m.tables) > 0 {
				m.activeTab = (m.activeTab - 1 + len(m.tables)) % len(m.tables)
			}
		}
	case chunkMsg:
		m.loading = false
		m.results = append(m.results, msg.Resources...)
		m.errors = append(m.errors, msg.Errors...)
		m.buildTables()
		return m, waitForChunk(m.chunks)
	case doneMsg:
		m.loading = false
		m.done = true
	}

	if len(m.tables) > 0 && m.activeTab < len(m.tables) {
		m.tables[m.activeTab], cmd = m.tables[m.activeTab].Update(msg)
	}

	return m, cmd
}

func (m *tuiModel) buildTables() {
	if len(m.results) == 0 {
		return
	}

	tabs := []string{"All"}
	seen := map[string]bool{"All": true}
	for _, r := range m.results {
		if !seen[r.Service] {
			seen[r.Service] = true
			tabs = append(tabs, r.Service)
		}
	}
	sort.Strings(tabs[1:])

	tables := make([]table.Model, 0, len(tabs))
	for _, tab := range tabs {
		var rows []table.Row
		for _, r := range m.results {
			if tab == "All" || r.Service == tab {
				rows = append(rows, table.Row{r.Service, r.Type, r.Region, r.ID, r.Name, r.State})
			}
		}
		t := table.New(
			table.WithColumns(m.columns()),
			table.WithRows(rows),
			table.WithFocused(true),
			table.WithHeight(m.tableHeight()),
		)
		t.SetStyles(tableStyles())
		tables = append(tables, t)
	}

	m.tables = tables
	if m.activeTab >= len(m.tables) {
		m.activeTab = len(m.tables) - 1
	}
}

func (m *tuiModel) updateTableLayout() {
	for i := range m.tables {
		m.tables[i].SetColumns(m.columns())
		m.tables[i].SetHeight(m.tableHeight())
	}
}

func (m tuiModel) columns() []table.Column {
	width := m.width - 8
	if width < 80 {
		width = 80
	}
	return []table.Column{
		{Title: "Service", Width: 10},
		{Title: "Type", Width: 12},
		{Title: "Region", Width: 14},
		{Title: "ID", Width: maxInt(18, width-82)},
		{Title: "Name", Width: 22},
		{Title: "State", Width: 10},
	}
}

func (m tuiModel) tableHeight() int {
	height := m.height - 10
	if height < 8 {
		return 8
	}
	return height
}

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#E4E4FF")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FBEAEB")).
		Background(lipgloss.Color("#3447AA")).
		Bold(false)
	return s
}

func (m tuiModel) View() string {
	if len(m.errors) > 0 && len(m.results) == 0 && m.done {
		return fmt.Sprintf("Error: %v\nPress q to quit.", m.errors[0].Message)
	}
	if m.loading && len(m.results) == 0 {
		return "Loading AWS resources...\n"
	}

	if len(m.tables) == 0 {
		if m.done {
			return "No resources found.\nPress q to quit."
		}
		return "Waiting for AWS resources...\nPress q to quit."
	}

	status := "streaming"
	if m.done {
		status = "complete"
	}
	header := fmt.Sprintf("AWS Resources (%s) | %d resources | %d errors", status, len(m.results), len(m.errors))
	tabs := m.tabView()
	tableView := baseStyle.Render(m.tables[m.activeTab].View())

	return fmt.Sprintf("%s\n%s\n\n%s\n\n[Tab/Shift+Tab] Switch service | [↑/↓] Move | [q] Quit", header, tabs, tableView)
}

func (m tuiModel) tabView() string {
	if len(m.tables) == 0 {
		return ""
	}
	labels := []string{"All"}
	seen := map[string]bool{"All": true}
	for _, r := range m.results {
		if !seen[r.Service] {
			seen[r.Service] = true
			labels = append(labels, r.Service)
		}
	}
	sort.Strings(labels[1:])
	for i, label := range labels {
		if i == m.activeTab {
			labels[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBEAEB")).Background(lipgloss.Color("#3447AA")).Padding(0, 1).Render(label)
		} else {
			labels[i] = lipgloss.NewStyle().Padding(0, 1).Render(label)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, labels...)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

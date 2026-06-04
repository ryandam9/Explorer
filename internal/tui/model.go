package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/aws_explorer/internal/engine"
	"github.com/user/aws_explorer/internal/model"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("#E4E4FF"))

var detailPanelStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#E4E4FF")).
	Padding(0, 1)

var detailKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E4E4FF"))
var detailSectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#6260FF")).Underline(true)

type tuiModel struct {
	ctx          context.Context
	engine       *engine.Engine
	results      []model.Resource
	errors       []model.ExploreError
	loading      bool
	activeTab    int
	tables       []table.Model
	tabNames     []string
	tabResources [][]model.Resource
	chunks       chan model.ResultChunk
	done         bool
	width        int
	height       int
	showDetail   bool
	detail       *model.Resource
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
		case "esc":
			m.showDetail = false
			m.detail = nil
		case "enter":
			if len(m.tables) > 0 && m.activeTab < len(m.tabResources) {
				rows := m.tabResources[m.activeTab]
				cursor := m.tables[m.activeTab].Cursor()
				if cursor >= 0 && cursor < len(rows) {
					r := rows[cursor]
					m.detail = &r
					m.showDetail = true
				}
			}
		case "tab":
			if len(m.tables) > 0 {
				m.activeTab = (m.activeTab + 1) % len(m.tables)
				m.showDetail = false
				m.detail = nil
			}
		case "shift+tab":
			if len(m.tables) > 0 {
				m.activeTab = (m.activeTab - 1 + len(m.tables)) % len(m.tables)
				m.showDetail = false
				m.detail = nil
			}
		}
	case chunkMsg:
		m.loading = false
		m.results = append(m.results, msg.Resources...)
		m.errors = append(m.errors, msg.Errors...)
		m.buildTables()
		m.showDetail = false
		m.detail = nil
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
	tabResources := make([][]model.Resource, 0, len(tabs))

	for _, tab := range tabs {
		var rows []table.Row
		var resources []model.Resource
		for _, r := range m.results {
			if tab == "All" || r.Service == tab {
				rows = append(rows, table.Row{r.Service, r.Type, r.Region, r.ID, r.Name, r.State})
				resources = append(resources, r)
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
		tabResources = append(tabResources, resources)
	}

	m.tables = tables
	m.tabNames = tabs
	m.tabResources = tabResources
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

func (m tuiModel) tableWidth() int {
	if m.showDetail {
		w := (m.width * 55 / 100) - 8
		if w < 60 {
			return 60
		}
		return w
	}
	w := m.width - 8
	if w < 80 {
		return 80
	}
	return w
}

func (m tuiModel) columns() []table.Column {
	width := m.tableWidth()
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

func (m tuiModel) renderDetail(r model.Resource) string {
	var b strings.Builder

	b.WriteString(detailSectionStyle.Render("RESOURCE") + "\n\n")

	fields := []struct{ k, v string }{
		{"Service", r.Service},
		{"Type", r.Type},
		{"Region", r.Region},
		{"Account", r.AccountID},
		{"ID", r.ID},
		{"Name", r.Name},
		{"State", r.State},
	}
	for _, f := range fields {
		if f.v != "" {
			b.WriteString(detailKeyStyle.Render(fmt.Sprintf("%-9s", f.k)) + " " + f.v + "\n")
		}
	}
	if r.ARN != "" {
		b.WriteString(detailKeyStyle.Render("ARN") + "\n  " + r.ARN + "\n")
	}
	if r.CreatedAt != nil {
		b.WriteString(detailKeyStyle.Render("Created") + "  " + r.CreatedAt.Format("2006-01-02 15:04:05") + "\n")
	}

	if len(r.Summary) > 0 {
		b.WriteString("\n" + detailSectionStyle.Render("SUMMARY") + "\n\n")
		keys := make([]string, 0, len(r.Summary))
		for k := range r.Summary {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(detailKeyStyle.Render(fmt.Sprintf("%-20s", k)) + " " + r.Summary[k] + "\n")
		}
	}

	if len(r.Tags) > 0 {
		b.WriteString("\n" + detailSectionStyle.Render("TAGS") + "\n\n")
		keys := make([]string, 0, len(r.Tags))
		for k := range r.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(detailKeyStyle.Render(fmt.Sprintf("%-20s", k)) + " " + r.Tags[k] + "\n")
		}
	}

	return b.String()
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

	var helpLine string
	if m.showDetail {
		helpLine = "[Tab/Shift+Tab] Switch service | [↑/↓] Move | [Enter] Select | [Esc] Close detail | [q] Quit"
	} else {
		helpLine = "[Tab/Shift+Tab] Switch service | [↑/↓] Move | [Enter] Detail | [q] Quit"
	}

	if m.showDetail && m.detail != nil {
		detailWidth := m.width - m.tableWidth() - 12
		if detailWidth < 30 {
			detailWidth = 30
		}
		detailContent := m.renderDetail(*m.detail)
		detailView := detailPanelStyle.Width(detailWidth).Height(m.tableHeight() + 2).Render(detailContent)
		body := lipgloss.JoinHorizontal(lipgloss.Top, tableView, "  ", detailView)
		return fmt.Sprintf("%s\n%s\n\n%s\n\n%s", header, tabs, body, helpLine)
	}

	return fmt.Sprintf("%s\n%s\n\n%s\n\n%s", header, tabs, tableView, helpLine)
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

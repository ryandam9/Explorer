package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
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
	ctx            context.Context
	engine         *engine.Engine
	results        []model.Resource
	errors         []model.ExploreError
	loading        bool
	activeTab      int
	tables         []table.Model
	tabNames       []string
	tabResources   [][]model.Resource
	chunks         chan model.ResultChunk
	done           bool
	width          int
	height         int
	showDetail     bool
	detail         *model.Resource
	detailViewport viewport.Model
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
		if m.showDetail && m.detail != nil {
			m.syncDetailViewport()
		}
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
					m.syncDetailViewport()
				}
			}
		case "[":
			if m.showDetail {
				m.detailViewport.LineUp(3)
				return m, nil
			}
		case "]":
			if m.showDetail {
				m.detailViewport.LineDown(3)
				return m, nil
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

// renderDetail formats resource details for the detail viewport.
// width is the inner display width so long values are wrapped before they
// reach the panel border.
func (m tuiModel) renderDetail(r model.Resource, width int) string {
	var b strings.Builder

	// fieldVal wraps value v at maxW characters, indenting continuation lines.
	fieldVal := func(v string, keyWidth, maxW int) string {
		if maxW <= 0 || len(v) <= maxW {
			return v
		}
		indent := strings.Repeat(" ", keyWidth+1)
		chunks := chunkString(v, maxW)
		return chunks[0] + "\n" + indent + strings.Join(chunks[1:], "\n"+indent)
	}

	const keyW = 9  // width of "%-9s" key field
	valW := width - keyW - 1
	if valW < 10 {
		valW = 10
	}

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
			b.WriteString(detailKeyStyle.Render(fmt.Sprintf("%-9s", f.k)) + " " + fieldVal(f.v, keyW, valW) + "\n")
		}
	}
	if r.ARN != "" {
		arnW := width - 2
		if arnW < 10 {
			arnW = 10
		}
		b.WriteString(detailKeyStyle.Render("ARN") + "\n")
		for _, chunk := range chunkString(r.ARN, arnW) {
			b.WriteString("  " + chunk + "\n")
		}
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
		const sumKeyW = 20
		sumValW := width - sumKeyW - 1
		if sumValW < 10 {
			sumValW = 10
		}
		for _, k := range keys {
			b.WriteString(detailKeyStyle.Render(fmt.Sprintf("%-20s", k)) + " " + fieldVal(r.Summary[k], sumKeyW, sumValW) + "\n")
		}
	}

	if len(r.Tags) > 0 {
		b.WriteString("\n" + detailSectionStyle.Render("TAGS") + "\n\n")
		keys := make([]string, 0, len(r.Tags))
		for k := range r.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		const tagKeyW = 20
		tagValW := width - tagKeyW - 1
		if tagValW < 10 {
			tagValW = 10
		}
		for _, k := range keys {
			b.WriteString(detailKeyStyle.Render(fmt.Sprintf("%-20s", k)) + " " + fieldVal(r.Tags[k], tagKeyW, tagValW) + "\n")
		}
	}

	return b.String()
}

var privilegeErrorStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#FF5555")).
	Padding(0, 1)

var privilegeTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#FF5555"))

var privilegeHintStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#FFAA00"))

func (m tuiModel) renderPrivilegeErrors() string {
	var authErrs, otherErrs []model.ExploreError
	for _, e := range m.errors {
		if e.Code == "AccessDenied" {
			authErrs = append(authErrs, e)
		} else {
			otherErrs = append(otherErrs, e)
		}
	}

	var b strings.Builder

	if len(authErrs) > 0 {
		b.WriteString(privilegeTitleStyle.Render("INSUFFICIENT PRIVILEGES") + "\n\n")
		for _, e := range authErrs {
			b.WriteString(detailKeyStyle.Render(fmt.Sprintf("%-12s", "Service")) +
				" " + strings.ToUpper(e.Service) + " (" + e.Region + ")\n")
			b.WriteString(privilegeHintStyle.Render(e.Message) + "\n\n")
		}
		b.WriteString("Attach the missing permissions to your IAM user or role.\n")
	}

	if len(otherErrs) > 0 {
		if len(authErrs) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Other errors:\n")
		for _, e := range otherErrs {
			b.WriteString(fmt.Sprintf("  [%s|%s] %s: %s\n", e.Service, e.Region, e.Code, e.Message))
		}
	}

	return privilegeErrorStyle.Render(b.String())
}

func (m tuiModel) View() string {
	if len(m.errors) > 0 && len(m.results) == 0 && m.done {
		return m.renderPrivilegeErrors() + "\nPress q to quit."
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
		helpLine = "[Tab/Shift+Tab] Switch service | [↑/↓] Move | [[/]] Scroll detail | [Esc] Close detail | [q] Quit"
	} else {
		helpLine = "[Tab/Shift+Tab] Switch service | [↑/↓] Move | [Enter] Detail | [q] Quit"
	}

	if m.showDetail && m.detail != nil {
		detailWidth := m.width - m.tableWidth() - 12
		if detailWidth < 30 {
			detailWidth = 30
		}
		detailHeight := m.tableHeight() + 2
		detailView := detailPanelStyle.
			Width(detailWidth).
			Height(detailHeight).
			MaxWidth(detailWidth + 2).
			MaxHeight(detailHeight + 2).
			Render(m.detailViewport.View())
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

// chunkString splits s into substrings of at most n characters each,
// which allows long values (like ARNs) to wrap within a fixed-width panel.
func chunkString(s string, n int) []string {
	if n <= 0 || len(s) <= n {
		return []string{s}
	}
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// syncDetailViewport rebuilds the detail viewport to match the current terminal
// size and re-renders the selected resource's detail content into it.
func (m *tuiModel) syncDetailViewport() {
	if m.detail == nil || m.width == 0 {
		return
	}
	detailWidth := m.width - m.tableWidth() - 12
	if detailWidth < 30 {
		detailWidth = 30
	}
	vpWidth := detailWidth - 2 // subtract panel horizontal padding
	vpHeight := m.tableHeight()
	if vpWidth < 10 {
		vpWidth = 10
	}
	if vpHeight < 4 {
		vpHeight = 4
	}
	m.detailViewport = viewport.New(vpWidth, vpHeight)
	m.detailViewport.SetContent(m.renderDetail(*m.detail, vpWidth))
}

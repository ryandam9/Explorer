package gluetui

import (
	"context"
	"log/slog"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

type tab int

const (
	tabJobs tab = iota
	tabCrawlers
	tabTriggers
	tabWorkflows
	tabConnections
	tabDatabases
	tabCount
)

var tabNames = [tabCount]string{"Jobs", "Crawlers", "Triggers", "Workflows", "Connections", "Catalog"}

// runWindow caps how many runs the history view fetches per job.
const runWindow = 20

type m struct {
	ctx        context.Context
	client     *Client
	regions    []string
	allRegions bool
	appCfg     *config.Config

	width, height int

	inv     Inventory
	loading bool
	err     error

	tab tab
	sel [tabCount]int

	filter       textinput.Model
	filterActive bool

	// Run-history sub-view (Enter on a job).
	runsActive  bool
	runsJob     Job
	runs        []JobRun
	runsLoading bool
	runsErr     error
	runsSel     int

	spinner   spinner.Model
	toast     string
	toastExp  time.Time
	showAbout bool
}

type invMsg struct {
	inv Inventory
	err error
}

type runsMsg struct {
	job  Job
	runs []JobRun
	err  error
}

type clearToastMsg struct{}

// NewModel builds the Glue dashboard over one or more regions.
func NewModel(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool, appCfg *config.Config) (tea.Model, error) {
	client, err := NewClient(ctx, awsCfg, regions, allRegions)
	if err != nil {
		return nil, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	f := textinput.New()
	f.Placeholder = "Filter…"
	f.Width = 30

	return &m{
		ctx:        ctx,
		client:     client,
		regions:    client.Regions(),
		allRegions: allRegions,
		appCfg:     appCfg,
		filter:     f,
		spinner:    s,
		loading:    true,
	}, nil
}

func (mm *m) Init() tea.Cmd {
	return tea.Batch(mm.spinner.Tick, mm.loadInventoryCmd())
}

func (mm *m) loadInventoryCmd() tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading Glue inventory", "regions", len(mm.regions))
		inv, err := mm.client.LoadInventory(mm.ctx)
		return invMsg{inv: inv, err: err}
	}
}

func (mm *m) loadRunsCmd(job Job) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading Glue job runs", "job", job.Name, "region", job.Region)
		runs, err := mm.client.JobRuns(mm.ctx, job.Region, job.Name, runWindow)
		return runsMsg{job: job, runs: runs, err: err}
	}
}

func toastCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearToastMsg{} })
}

func (mm *m) setToast(s string) {
	mm.toast = s
	mm.toastExp = time.Now().Add(3 * time.Second)
}

func (mm *m) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		mm.width = msg.Width
		mm.height = msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		mm.spinner, cmd = mm.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case clearToastMsg:
		mm.toast = ""

	case invMsg:
		mm.loading = false
		if msg.err != nil {
			mm.err = msg.err
		} else {
			mm.inv = msg.inv
			mm.clampAll()
		}

	case runsMsg:
		mm.runsLoading = false
		mm.runsErr = msg.err
		mm.runs = msg.runs
		mm.runsSel = 0

	case tea.KeyMsg:
		cmds = append(cmds, mm.handleKey(msg)...)
	}

	return mm, tea.Batch(cmds...)
}

func (mm *m) handleKey(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	// Error screen: Enter/Esc retries, q quits.
	if mm.err != nil {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "enter", "esc":
			mm.err = nil
			mm.loading = true
			cmds = append(cmds, mm.loadInventoryCmd())
		}
		return cmds
	}

	if mm.showAbout {
		mm.showAbout = false
		return cmds
	}

	// Filter input captures keys while active.
	if mm.filterActive {
		switch msg.String() {
		case "enter", "esc":
			if msg.String() == "esc" {
				mm.filter.SetValue("")
			}
			mm.filterActive = false
			mm.filter.Blur()
			mm.clampCurrent()
		default:
			var cmd tea.Cmd
			mm.filter, cmd = mm.filter.Update(msg)
			cmds = append(cmds, cmd)
			mm.clampCurrent()
		}
		return cmds
	}

	// Run-history sub-view.
	if mm.runsActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left", "h":
			mm.runsActive = false
		case "up", "k":
			if mm.runsSel > 0 {
				mm.runsSel--
			}
		case "down", "j":
			if mm.runsSel < len(mm.runs)-1 {
				mm.runsSel++
			}
		case "y":
			if mm.runsSel < len(mm.runs) && mm.runs[mm.runsSel].Error != "" {
				_ = clipboard.WriteAll(mm.runs[mm.runsSel].Error)
				mm.setToast("Copied error message")
				cmds = append(cmds, toastCmd(3*time.Second))
			}
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return cmds
	}

	// Dashboard.
	switch msg.String() {
	case "q", "ctrl+c":
		return []tea.Cmd{tea.Quit}
	case "tab", "right", "l":
		mm.tab = (mm.tab + 1) % tabCount
		mm.resetFilter()
	case "shift+tab", "left", "h":
		mm.tab = (mm.tab + tabCount - 1) % tabCount
		mm.resetFilter()
	case "up", "k":
		if mm.sel[mm.tab] > 0 {
			mm.sel[mm.tab]--
		}
	case "down", "j":
		if mm.sel[mm.tab] < mm.rowCount()-1 {
			mm.sel[mm.tab]++
		}
	case "/":
		mm.filterActive = true
		mm.filter.Focus()
	case "r":
		mm.loading = true
		mm.inv = Inventory{}
		cmds = append(cmds, mm.loadInventoryCmd(), mm.spinner.Tick)
	case "enter":
		if mm.tab == tabJobs {
			if job, ok := mm.selectedJob(); ok {
				mm.runsActive = true
				mm.runsJob = job
				mm.runsLoading = true
				mm.runs = nil
				mm.runsErr = nil
				cmds = append(cmds, mm.loadRunsCmd(job), mm.spinner.Tick)
			}
		}
	case "o":
		mm.openConsole(&cmds)
	case ui.KeyAbout:
		mm.showAbout = true
	}
	return cmds
}

// openConsole copies (and opens, when local) the console URL for the selected
// row.
func (mm *m) openConsole(cmds *[]tea.Cmd) {
	res, ok := mm.selectedResource()
	if !ok {
		return
	}
	url, _ := consolelink.URL(res)
	_ = clipboard.WriteAll(url)
	if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
		mm.setToast("Opened in browser · copied console URL")
	} else {
		mm.setToast("Copied console URL")
	}
	*cmds = append(*cmds, toastCmd(3*time.Second))
}

func (mm *m) resetFilter() {
	mm.filter.SetValue("")
	mm.filterActive = false
	mm.filter.Blur()
	mm.clampCurrent()
}

func (mm *m) clampCurrent() {
	if mm.sel[mm.tab] >= mm.rowCount() {
		mm.sel[mm.tab] = max(0, mm.rowCount()-1)
	}
}

func (mm *m) clampAll() {
	for t := tab(0); t < tabCount; t++ {
		n := len(mm.rowsForTab(t))
		if mm.sel[t] >= n {
			mm.sel[t] = max(0, n-1)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (mm *m) PageTitle() string {
	base := "AWS Glue"
	if mm.runsActive {
		return base + " › " + mm.runsJob.Name + " › runs"
	}
	return base + " › " + tabNames[mm.tab]
}

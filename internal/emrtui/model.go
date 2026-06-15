package emrtui

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

// stepWindow caps how many steps the history view fetches per cluster.
const stepWindow = 50

type m struct {
	ctx        context.Context
	client     *Client
	regions    []string
	allRegions bool
	appCfg     *config.Config
	configPath string

	width, height int

	inv     Inventory
	loading bool
	err     error

	sel int

	filter       textinput.Model
	filterActive bool

	// Step-history sub-view (Enter on a cluster).
	stepsActive  bool
	stepsCluster Cluster
	steps        []Step
	stepsLoading bool
	stepsErr     error
	stepsSel     int

	// Cluster-detail overlay (d on a cluster).
	detailActive  bool
	detailCluster Cluster

	spinner   spinner.Model
	toast     string
	toastExp  time.Time
	showAbout bool
}

type invMsg struct {
	inv Inventory
	err error
}

type stepsMsg struct {
	cluster Cluster
	steps   []Step
	err     error
}

type clearToastMsg struct{}

// NewModel builds the EMR dashboard over one or more regions. configPath is
// passed through to the child s3 process for the log-location jump (AXE-036).
func NewModel(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool, appCfg *config.Config, configPath string) (tea.Model, error) {
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
		configPath: configPath,
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
		slog.Info("Loading EMR inventory", "regions", len(mm.regions))
		inv, err := mm.client.LoadInventory(mm.ctx)
		return invMsg{inv: inv, err: err}
	}
}

func (mm *m) loadStepsCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading EMR steps", "cluster", cl.ID, "region", cl.Region)
		steps, err := mm.client.Steps(mm.ctx, cl.Region, cl.ID, stepWindow)
		return stepsMsg{cluster: cl, steps: steps, err: err}
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

	case s3JumpDoneMsg:
		if msg.err != nil {
			mm.setToast("Could not open S3 logs: " + msg.err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
		}

	case invMsg:
		mm.loading = false
		if msg.err != nil {
			mm.err = msg.err
		} else {
			mm.inv = msg.inv
			mm.clamp()
		}

	case stepsMsg:
		mm.stepsLoading = false
		mm.stepsErr = msg.err
		mm.steps = msg.steps
		mm.stepsSel = 0

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

	// Cluster-detail overlay: any key closes it (q still quits).
	if mm.detailActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		default:
			mm.detailActive = false
		}
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
			mm.clamp()
		default:
			var cmd tea.Cmd
			mm.filter, cmd = mm.filter.Update(msg)
			cmds = append(cmds, cmd)
			mm.clamp()
		}
		return cmds
	}

	// Step-history sub-view.
	if mm.stepsActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left", "h":
			mm.stepsActive = false
		case "up", "k":
			if mm.stepsSel > 0 {
				mm.stepsSel--
			}
		case "down", "j":
			if mm.stepsSel < len(mm.steps)-1 {
				mm.stepsSel++
			}
		case "y":
			if mm.stepsSel < len(mm.steps) && mm.steps[mm.stepsSel].FailureReason != "" {
				_ = clipboard.WriteAll(mm.steps[mm.stepsSel].FailureReason)
				mm.setToast("Copied failure reason")
				cmds = append(cmds, toastCmd(3*time.Second))
			}
		case "L":
			if mm.stepsSel < len(mm.steps) {
				mm.jumpToStepLogs(mm.steps[mm.stepsSel], &cmds)
			}
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return cmds
	}

	// Cluster list.
	switch msg.String() {
	case "q", "ctrl+c":
		return []tea.Cmd{tea.Quit}
	case "up", "k":
		if mm.sel > 0 {
			mm.sel--
		}
	case "down", "j":
		if mm.sel < mm.rowCount()-1 {
			mm.sel++
		}
	case "/":
		mm.filterActive = true
		mm.filter.Focus()
	case "r":
		mm.loading = true
		mm.inv = Inventory{}
		cmds = append(cmds, mm.loadInventoryCmd(), mm.spinner.Tick)
	case "enter", "s":
		if cl, ok := mm.selectedCluster(); ok {
			mm.stepsActive = true
			mm.stepsCluster = cl
			mm.stepsLoading = true
			mm.steps = nil
			mm.stepsErr = nil
			cmds = append(cmds, mm.loadStepsCmd(cl), mm.spinner.Tick)
		}
	case "d":
		if cl, ok := mm.selectedCluster(); ok {
			mm.detailActive = true
			mm.detailCluster = cl
		}
	case "L":
		if cl, ok := mm.selectedCluster(); ok {
			mm.jumpToClusterLogs(cl, &cmds)
		}
	case "o":
		mm.openConsole(&cmds)
	case ui.KeyAbout:
		mm.showAbout = true
	}
	return cmds
}

// openConsole copies (and opens, when local) the console URL for the selected
// cluster.
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

func (mm *m) clamp() {
	if mm.sel >= mm.rowCount() {
		mm.sel = max(0, mm.rowCount()-1)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (mm *m) PageTitle() string {
	base := "Amazon EMR"
	if mm.stepsActive {
		return base + " › " + mm.stepsCluster.Name + " › steps"
	}
	return base + " › Clusters"
}

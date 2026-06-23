// Package relatedtui is the interactive explorer for the related-resources
// query (#345): given a resource, it shows what it uses and what uses it, and
// lets you walk the relationship graph hop by hop. It collects the reference
// edges once (internal/xref) and navigates them in memory — never an AWS call
// on cursor movement (§7).
package relatedtui

import (
	"context"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
	"github.com/ryandam9/aws_explorer/internal/xref"
)

// focusPane identifies which of the two panels has the cursor.
type focusPane int

const (
	paneUses   focusPane = iota // what the centered resource references
	paneUsedBy                  // what references the centered resource
)

type edgesMsg struct {
	edges []xref.Edge
	errs  []model.ExploreError
}

type clearToastMsg struct{}

type m struct {
	ctx        context.Context
	cfg        aws.Config
	regions    []string
	maxConc    int
	timeout    time.Duration
	allRegions bool

	width, height int

	loading bool
	spinner spinner.Model
	partial []model.ExploreError

	fwd map[string][]xref.Edge
	rev map[string][]xref.Reference

	stack  []string // breadcrumb of centered identifiers; last is current
	result xref.RelatedResult

	focus     focusPane
	usesTbl   table.Model
	usedByTbl table.Model

	toast     string
	showHelp  bool
	overlayVP viewport.Model
}

// NewModel builds the explorer centered on input, collecting edges across the
// given region scope on Init.
func NewModel(ctx context.Context, cfg aws.Config, regions []string, maxConc int, timeout time.Duration, allRegions bool, input string) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	return &m{
		ctx:        ctx,
		cfg:        cfg,
		regions:    regions,
		maxConc:    maxConc,
		timeout:    timeout,
		allRegions: allRegions,
		loading:    true,
		spinner:    s,
		stack:      []string{input},
		usesTbl:    newResTable(),
		usedByTbl:  newResTable(),
	}
}

func newResTable() table.Model {
	return table.New(
		table.WithColumns([]table.Column{
			{Title: "#", Width: 4},
			{Title: "Service", Width: 12},
			{Title: "Type", Width: 18},
			{Title: "Resource", Width: 30},
			{Title: "Region", Width: 14},
			{Title: "Via", Width: 26},
		}),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1),
	)
}

func (mm *m) current() string {
	if len(mm.stack) == 0 {
		return ""
	}
	return mm.stack[len(mm.stack)-1]
}

func (mm *m) Init() tea.Cmd {
	return tea.Batch(mm.spinner.Tick, mm.collectCmd())
}

func (mm *m) collectCmd() tea.Cmd {
	return func() tea.Msg {
		// The explorer lets you drill onto any resource — including IAM roles,
		// whose policy edges should be visible — so include the per-role policy
		// sweep. It is collected once at startup and the fan-out is now
		// bounded-concurrent (§7), so this no longer storms.
		edges, errs := xref.Collect(mm.ctx, mm.cfg, mm.regions, mm.maxConc, mm.timeout, true, nil)
		return edgesMsg{edges: edges, errs: errs}
	}
}

func toastCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearToastMsg{} })
}

func (mm *m) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		mm.width, mm.height = msg.Width, msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		mm.spinner, cmd = mm.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case clearToastMsg:
		mm.toast = ""

	case edgesMsg:
		mm.loading = false
		mm.partial = msg.errs
		mm.fwd = xref.BuildForwardIndex(msg.edges)
		mm.rev = xref.BuildIndex(msg.edges)
		mm.recompute()

	case tea.KeyMsg:
		cmds = append(cmds, mm.handleKey(msg)...)
	}
	return mm, tea.Batch(cmds...)
}

// recompute refreshes the result and table rows for the current centered
// resource (in memory — no AWS call).
func (mm *m) recompute() {
	mm.result = xref.Related(mm.current(), mm.fwd, mm.rev, 1, false)
	mm.usesTbl.SetRows(linkRows(mm.result.Uses))
	mm.usesTbl.SetCursor(0)
	mm.usedByTbl.SetRows(linkRows(mm.result.UsedBy))
	mm.usedByTbl.SetCursor(0)
}

func (mm *m) handleKey(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	if mm.showHelp {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "i", "?", "esc", "enter":
			mm.showHelp = false
		case "up", "k":
			mm.overlayVP.LineUp(1)
		case "down", "j":
			mm.overlayVP.LineDown(1)
		case "g", "home":
			mm.overlayVP.GotoTop()
		case "G", "end":
			mm.overlayVP.GotoBottom()
		}
		return cmds
	}

	if mm.loading {
		if s := msg.String(); s == "q" || s == "ctrl+c" {
			return []tea.Cmd{tea.Quit}
		}
		return cmds
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return []tea.Cmd{tea.Quit}
	case "i":
		mm.showHelp = true
		mm.overlayVP.GotoTop()
		return cmds
	case "r":
		mm.loading = true
		return append(cmds, mm.collectCmd(), mm.spinner.Tick)
	case "tab":
		mm.focus = (mm.focus + 1) % 2
		return cmds
	case "shift+tab":
		mm.focus = (mm.focus + 1) % 2
		return cmds
	case "left", "h":
		mm.focus = paneUses
		return cmds
	case "right", "l":
		mm.focus = paneUsedBy
		return cmds
	case "enter":
		mm.descend()
		return cmds
	case "backspace", "esc":
		mm.back()
		return cmds
	case "up", "k":
		mm.active().MoveUp(1)
	case "down", "j":
		mm.active().MoveDown(1)
	case "g", "home":
		mm.active().GotoTop()
	case "G", "end":
		mm.active().GotoBottom()
	case "<", ",":
		mm.active().ScrollLeft()
	case ">", ".":
		mm.active().ScrollRight()
	case "y":
		if r, ok := mm.selected(); ok && r.ID != "" {
			_ = clipboard.WriteAll(r.ID)
			// r.ID isn't always an ARN — don't claim it is.
			if isARN(r.ID) {
				mm.toast = "Copied ARN"
			} else {
				mm.toast = "Copied ID"
			}
			cmds = append(cmds, toastCmd())
		}
	case "o":
		if r, ok := mm.selected(); ok {
			if url, kind, okURL := consoleLinkFor(mm.resourceOf(r)); okURL {
				_ = clipboard.WriteAll(url)
				if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
					mm.toast = "Opened in browser · copied " + kind
				} else {
					mm.toast = "Copied " + kind
				}
			} else {
				mm.toast = "No console link for this resource type"
			}
			cmds = append(cmds, toastCmd())
		}
	}
	return cmds
}

func (mm *m) active() *table.Model {
	if mm.focus == paneUsedBy {
		return &mm.usedByTbl
	}
	return &mm.usesTbl
}

// links returns the link slice backing the focused pane.
func (mm *m) links() []xref.Link {
	if mm.focus == paneUsedBy {
		return mm.result.UsedBy
	}
	return mm.result.Uses
}

func (mm *m) selected() (xref.Link, bool) {
	links := mm.links()
	i := mm.active().Cursor()
	if i < 0 || i >= len(links) {
		return xref.Link{}, false
	}
	return links[i], true
}

// descend re-centers the view on the focused row's resource (pushing the
// current one onto the breadcrumb), so the graph can be walked hop by hop.
func (mm *m) descend() {
	l, ok := mm.selected()
	if !ok || l.ID == "" {
		return
	}
	mm.stack = append(mm.stack, l.ID)
	mm.focus = paneUses
	mm.recompute()
}

// back pops the breadcrumb to the previously centered resource.
func (mm *m) back() {
	if len(mm.stack) <= 1 {
		return
	}
	mm.stack = mm.stack[:len(mm.stack)-1]
	mm.focus = paneUses
	mm.recompute()
}

func (mm *m) resourceOf(l xref.Link) model.Resource {
	r := model.Resource{Service: l.Service, Type: l.Type, Region: l.Region, ID: l.ID, Name: l.Name}
	if isARN(l.ID) {
		r.ARN = l.ID
	}
	return r
}

// consoleLinkFor resolves the best console URL for a row. A deep link is ideal,
// but when the resource type has no deep-link builder we still fall back to an
// ARN search rather than telling the user there's no link at all (#387). kind
// is the noun for the toast ("console URL" vs "console search URL"); ok is false
// only when nothing useful (deep link or ARN search) can be built.
func consoleLinkFor(r model.Resource) (url, kind string, ok bool) {
	u, deep := consolelink.URL(r)
	switch {
	case deep:
		return u, "console URL", true
	case r.ARN != "":
		return u, "console search URL", true
	default:
		return "", "", false
	}
}

func isARN(s string) bool { return len(s) > 4 && s[:4] == "arn:" }

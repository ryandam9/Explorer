package tagstui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// loadTimeout bounds each tag/resource lookup (which fans out across regions).
const loadTimeout = 2 * time.Minute

type pane int

const (
	paneKeys      pane = iota // browse tag keys
	paneValues                // browse the selected key's values
	paneResources             // resources matching the active filter
)

type keysMsg struct {
	keys []string
	errs []model.ExploreError
}

type valuesMsg struct {
	key    string
	values []string
	errs   []model.ExploreError
}

type resourcesMsg struct {
	desc      string
	resources []model.Resource
	errs      []model.ExploreError
}

type clearToastMsg struct{}

type m struct {
	ctx        context.Context
	client     *Client
	regions    []string
	allRegions bool

	width, height int

	pane    pane
	loading bool
	partial []model.ExploreError // non-fatal per-region failures for the current view

	keysTbl   table.Model
	valuesTbl table.Model
	resTbl    table.Model

	keys         []string
	selectedKey  string
	values       []string
	resources    []model.Resource
	filterDesc   string              // human description of the active resource filter (display)
	activeFilter map[string][]string // the active filter itself, for an exact refresh

	valuesCache map[string][]string
	resCache    map[string][]model.Resource

	filter       textinput.Model
	filterActive bool

	spinner   spinner.Model
	toast     string
	showAbout bool
}

// NewModel builds the tags dashboard over the client's resolved region scope.
func NewModel(ctx context.Context, client *Client, allRegions bool) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	f := textinput.New()
	f.Placeholder = "Key=Value, Key2=Value2  (Enter to search)"
	f.CharLimit = 512
	f.Width = 48

	return &m{
		ctx:         ctx,
		client:      client,
		regions:     client.Regions(),
		allRegions:  allRegions,
		pane:        paneKeys,
		loading:     true,
		keysTbl:     newTable([]table.Column{{Title: "#", Width: 4}, {Title: "Tag key", Width: 36}}),
		valuesTbl:   newTable([]table.Column{{Title: "#", Width: 4}, {Title: "Value", Width: 48}}),
		resTbl:      newResourceTable(),
		valuesCache: map[string][]string{},
		resCache:    map[string][]model.Resource{},
		filter:      f,
		spinner:     s,
	}
}

func newTable(cols []table.Column) table.Model {
	return table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1),
	)
}

func newResourceTable() table.Model {
	return newTable([]table.Column{
		{Title: "#", Width: 4},
		{Title: "Service", Width: 12},
		{Title: "Type", Width: 18},
		{Title: "Name", Width: 28},
		{Title: "Region", Width: 14},
		{Title: "ID", Width: 28},
	})
}

func (mm *m) Init() tea.Cmd {
	return tea.Batch(mm.spinner.Tick, mm.loadKeysCmd())
}

func (mm *m) loadKeysCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(mm.ctx, loadTimeout)
		defer cancel()
		keys, errs := mm.client.TagKeys(ctx)
		return keysMsg{keys: keys, errs: errs}
	}
}

func (mm *m) loadValuesCmd(key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(mm.ctx, loadTimeout)
		defer cancel()
		vals, errs := mm.client.TagValues(ctx, key)
		return valuesMsg{key: key, values: vals, errs: errs}
	}
}

func (mm *m) loadResourcesCmd(desc string, filters map[string][]string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(mm.ctx, loadTimeout)
		defer cancel()
		res, errs := mm.client.Resources(ctx, filters)
		return resourcesMsg{desc: desc, resources: res, errs: errs}
	}
}

func toastCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearToastMsg{} })
}

func (mm *m) setToast(s string) { mm.toast = s }

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

	case keysMsg:
		mm.loading = false
		mm.partial = msg.errs
		mm.keys = msg.keys
		mm.keysTbl.SetRows(numbered(msg.keys))
		mm.keysTbl.SetCursor(0)

	case valuesMsg:
		mm.loading = false
		mm.partial = msg.errs
		mm.values = msg.values
		mm.valuesCache[msg.key] = msg.values
		mm.valuesTbl.SetRows(numbered(msg.values))
		mm.valuesTbl.SetCursor(0)

	case resourcesMsg:
		mm.loading = false
		mm.partial = msg.errs
		mm.resources = msg.resources
		mm.resCache[msg.desc] = msg.resources
		mm.resTbl.SetRows(resourceRows(msg.resources))
		mm.resTbl.SetCursor(0)

	case tea.KeyMsg:
		cmds = append(cmds, mm.handleKey(msg)...)
	}

	return mm, tea.Batch(cmds...)
}

func (mm *m) handleKey(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	if mm.showAbout {
		mm.showAbout = false
		return cmds
	}

	// Typed filter entry captures keys while active.
	if mm.filterActive {
		switch msg.String() {
		case "enter":
			expr := strings.TrimSpace(mm.filter.Value())
			mm.filterActive = false
			mm.filter.Blur()
			if filters := parseFilterExpr(expr); len(filters) > 0 {
				mm.openResources(filterDesc(filters), filters, &cmds)
			}
		case "esc":
			mm.filterActive = false
			mm.filter.Blur()
		default:
			var cmd tea.Cmd
			mm.filter, cmd = mm.filter.Update(msg)
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return []tea.Cmd{tea.Quit}
	case "f", "/":
		mm.filterActive = true
		mm.filter.SetValue("")
		mm.filter.Focus()
		return cmds
	case "r":
		mm.refresh(&cmds)
		return cmds
	case "esc", "backspace", "left":
		mm.goBack()
		return cmds
	case "i":
		mm.showAbout = true
		return cmds
	}

	switch mm.pane {
	case paneKeys:
		mm.handleListKey(&mm.keysTbl, msg, func() { mm.openValues(&cmds) })
	case paneValues:
		mm.handleListKey(&mm.valuesTbl, msg, func() { mm.openResourcesFromValue(&cmds) })
	case paneResources:
		mm.handleResourceKey(msg, &cmds)
	}
	return cmds
}

// handleListKey handles navigation common to the single-column list panes; onEnter
// drills into the selection.
func (mm *m) handleListKey(tbl *table.Model, msg tea.KeyMsg, onEnter func()) {
	switch msg.String() {
	case "up", "k":
		tbl.MoveUp(1)
	case "down", "j":
		tbl.MoveDown(1)
	case "g", "home":
		tbl.GotoTop()
	case "G", "end":
		tbl.GotoBottom()
	case "enter", "right", "l":
		onEnter()
	}
}

func (mm *m) handleResourceKey(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		mm.resTbl.MoveUp(1)
	case "down", "j":
		mm.resTbl.MoveDown(1)
	case "g", "home":
		mm.resTbl.GotoTop()
	case "G", "end":
		mm.resTbl.GotoBottom()
	case "<", ",":
		mm.resTbl.ScrollLeft()
	case ">", ".":
		mm.resTbl.ScrollRight()
	case "y":
		if r, ok := mm.selectedResource(); ok && r.ARN != "" {
			_ = clipboard.WriteAll(r.ARN)
			mm.setToast("Copied ARN")
			*cmds = append(*cmds, toastCmd())
		}
	case "o":
		if r, ok := mm.selectedResource(); ok {
			if url, okURL := consolelink.URL(r); okURL {
				_ = clipboard.WriteAll(url)
				if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
					mm.setToast("Opened in browser · copied console URL")
				} else {
					mm.setToast("Copied console URL")
				}
				*cmds = append(*cmds, toastCmd())
			}
		}
	}
}

// openValues drills from the selected tag key into its values (cache-first).
func (mm *m) openValues(cmds *[]tea.Cmd) {
	i := mm.keysTbl.Cursor()
	if i < 0 || i >= len(mm.keys) {
		return
	}
	mm.selectedKey = mm.keys[i]
	mm.pane = paneValues
	if cached, ok := mm.valuesCache[mm.selectedKey]; ok {
		mm.values = cached
		mm.valuesTbl.SetRows(numbered(cached))
		mm.valuesTbl.SetCursor(0)
		mm.partial = nil
		return
	}
	mm.loading = true
	*cmds = append(*cmds, mm.loadValuesCmd(mm.selectedKey), mm.spinner.Tick)
}

// openResourcesFromValue drills from the selected value into matching resources.
func (mm *m) openResourcesFromValue(cmds *[]tea.Cmd) {
	i := mm.valuesTbl.Cursor()
	if i < 0 || i >= len(mm.values) {
		return
	}
	val := mm.values[i]
	mm.openResources(mm.selectedKey+"="+val, map[string][]string{mm.selectedKey: {val}}, cmds)
}

// openResources switches to the resources pane for the given filter (cache-first).
func (mm *m) openResources(desc string, filters map[string][]string, cmds *[]tea.Cmd) {
	mm.filterDesc = desc
	mm.activeFilter = filters
	mm.pane = paneResources
	if cached, ok := mm.resCache[desc]; ok {
		mm.resources = cached
		mm.resTbl.SetRows(resourceRows(cached))
		mm.resTbl.SetCursor(0)
		mm.partial = nil
		return
	}
	mm.loading = true
	*cmds = append(*cmds, mm.loadResourcesCmd(desc, filters), mm.spinner.Tick)
}

// goBack moves one pane up the drill-down (resources → values → keys).
func (mm *m) goBack() {
	switch mm.pane {
	case paneResources:
		// Return to values if we came from a single-key drill, else to keys.
		if mm.selectedKey != "" && strings.HasPrefix(mm.filterDesc, mm.selectedKey+"=") {
			mm.pane = paneValues
		} else {
			mm.pane = paneKeys
		}
		mm.partial = nil
	case paneValues:
		mm.pane = paneKeys
		mm.partial = nil
	}
}

// refresh reloads the current pane from AWS, bypassing the cache.
func (mm *m) refresh(cmds *[]tea.Cmd) {
	mm.loading = true
	switch mm.pane {
	case paneKeys:
		*cmds = append(*cmds, mm.loadKeysCmd(), mm.spinner.Tick)
	case paneValues:
		delete(mm.valuesCache, mm.selectedKey)
		*cmds = append(*cmds, mm.loadValuesCmd(mm.selectedKey), mm.spinner.Tick)
	case paneResources:
		delete(mm.resCache, mm.filterDesc)
		*cmds = append(*cmds, mm.loadResourcesCmd(mm.filterDesc, mm.activeFilter), mm.spinner.Tick)
	}
}

func (mm *m) selectedResource() (model.Resource, bool) {
	i := mm.resTbl.Cursor()
	if i < 0 || i >= len(mm.resources) {
		return model.Resource{}, false
	}
	return mm.resources[i], true
}

func (mm *m) PageTitle() string {
	switch mm.pane {
	case paneValues:
		return "AWS Tags › " + mm.selectedKey
	case paneResources:
		return "AWS Tags › " + mm.filterDesc
	default:
		return "AWS Tags › Keys"
	}
}

// numbered builds 1-based numbered rows for a single-column list.
func numbered(items []string) []table.Row {
	rows := make([]table.Row, len(items))
	for i, s := range items {
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), s}
	}
	return rows
}

func resourceRows(res []model.Resource) []table.Row {
	rows := make([]table.Row, len(res))
	for i, r := range res {
		name := r.Name
		if name == "" {
			name = "—"
		}
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), r.Service, r.Type, name, r.Region, r.ID}
	}
	return rows
}

// parseFilterExpr turns "Key=Value, Key2=Value2, Key3" into a tag-filter map:
// repeated keys accumulate values (OR within a key); a bare key (no "=") means
// "key present with any value" (empty value slice).
func parseFilterExpr(s string) map[string][]string {
	out := map[string][]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, hasEq := strings.Cut(part, "=")
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, exists := out[k]; !exists {
			out[k] = nil
		}
		if hasEq {
			if v = strings.TrimSpace(v); v != "" {
				out[k] = append(out[k], v)
			}
		}
	}
	return out
}

// filterDesc renders a tag-filter map as a stable "k=v|v2, k2" description.
func filterDesc(filters map[string][]string) string {
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if vs := filters[k]; len(vs) > 0 {
			sorted := append([]string(nil), vs...)
			sort.Strings(sorted)
			parts = append(parts, k+"="+strings.Join(sorted, "|"))
		} else {
			parts = append(parts, k)
		}
	}
	return strings.Join(parts, ", ")
}

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
	filterDesc   string                // human description of the active resource filter (display)
	activeGroups []map[string][]string // the active OR-of-AND filter, for an exact refresh
	activeTypes  []string              // active resource-type scope

	valuesCache map[string][]string
	resCache    map[string][]model.Resource

	// Per-key / per-value resource counts, filled progressively by a background
	// pass (best-effort; counts.go). Cached across navigation; countGen/countCh/
	// countCancel manage the in-flight pass.
	keyCounts   map[string]countVal
	valueCounts map[string]map[string]countVal
	countGen    int
	countCh     chan countMsg
	countCancel context.CancelFunc

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
	f.Placeholder = "Key=Value, K2=V2   ·   || to OR groups   ·   type:ec2:instance"
	f.CharLimit = 512
	f.Width = 48

	return &m{
		ctx:         ctx,
		client:      client,
		regions:     client.Regions(),
		allRegions:  allRegions,
		pane:        paneKeys,
		loading:     true,
		keysTbl:     newTable([]table.Column{{Title: "#", Width: 4}, {Title: "Tag key", Width: 36}, {Title: "Resources", Width: 10}}),
		valuesTbl:   newTable([]table.Column{{Title: "#", Width: 4}, {Title: "Value", Width: 44}, {Title: "Resources", Width: 10}}),
		resTbl:      newResourceTable(),
		valuesCache: map[string][]string{},
		resCache:    map[string][]model.Resource{},
		keyCounts:   map[string]countVal{},
		valueCounts: map[string]map[string]countVal{},
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

func (mm *m) loadResourcesCmd(desc string, groups []map[string][]string, resourceTypes []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(mm.ctx, loadTimeout)
		defer cancel()
		res, errs := mm.client.Resources(ctx, groups, resourceTypes)
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
		mm.keyCounts = map[string]countVal{} // fresh load → recount
		mm.keysTbl.SetRows(keyRows(msg.keys, mm.keyCounts))
		mm.keysTbl.SetCursor(0)
		mm.ensureCounts(&cmds)

	case valuesMsg:
		mm.loading = false
		mm.partial = msg.errs
		mm.values = msg.values
		mm.valuesCache[msg.key] = msg.values
		mm.valueCounts[msg.key] = map[string]countVal{} // fresh load → recount
		mm.valuesTbl.SetRows(valueRows(msg.values, mm.valueCounts[msg.key]))
		mm.valuesTbl.SetCursor(0)
		mm.ensureCounts(&cmds)

	case resourcesMsg:
		mm.loading = false
		mm.partial = msg.errs
		mm.resources = msg.resources
		mm.resCache[msg.desc] = msg.resources
		mm.resTbl.SetRows(resourceRows(msg.resources))
		mm.resTbl.SetCursor(0)

	case countMsg:
		if cmd := mm.onCount(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case countsDoneMsg:
		// nothing to do; the pass drained.

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
			if groups, types := ParseQuery(expr); len(groups) > 0 || len(types) > 0 {
				mm.openResources(queryDesc(groups, types), groups, types, &cmds)
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
		mm.goBack(&cmds)
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
		mm.valuesTbl.SetRows(valueRows(cached, mm.valueCounts[mm.selectedKey]))
		mm.valuesTbl.SetCursor(0)
		mm.partial = nil
		mm.ensureCounts(cmds) // resume any counts not finished before
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
	mm.openResources(mm.selectedKey+"="+val, []map[string][]string{{mm.selectedKey: {val}}}, nil, cmds)
}

// openResources switches to the resources pane for the given OR-of-AND filter
// (cache-first).
func (mm *m) openResources(desc string, groups []map[string][]string, resourceTypes []string, cmds *[]tea.Cmd) {
	mm.filterDesc = desc
	mm.activeGroups = groups
	mm.activeTypes = resourceTypes
	mm.pane = paneResources
	if cached, ok := mm.resCache[desc]; ok {
		mm.resources = cached
		mm.resTbl.SetRows(resourceRows(cached))
		mm.resTbl.SetCursor(0)
		mm.partial = nil
		return
	}
	mm.loading = true
	*cmds = append(*cmds, mm.loadResourcesCmd(desc, groups, resourceTypes), mm.spinner.Tick)
}

// goBack moves one pane up the drill-down (resources → values → keys), resuming
// the destination pane's background counts.
func (mm *m) goBack(cmds *[]tea.Cmd) {
	switch mm.pane {
	case paneResources:
		// Return to values if we came from a single-key drill, else to keys.
		if mm.selectedKey != "" && strings.HasPrefix(mm.filterDesc, mm.selectedKey+"=") {
			mm.pane = paneValues
		} else {
			mm.pane = paneKeys
		}
		mm.partial = nil
		mm.ensureCounts(cmds)
	case paneValues:
		mm.pane = paneKeys
		mm.partial = nil
		mm.ensureCounts(cmds)
	}
}

// refresh reloads the current pane from AWS, bypassing the cache.
func (mm *m) refresh(cmds *[]tea.Cmd) {
	mm.loading = true
	mm.cancelCounts() // stop any in-flight counts; the reload restarts them
	switch mm.pane {
	case paneKeys:
		*cmds = append(*cmds, mm.loadKeysCmd(), mm.spinner.Tick)
	case paneValues:
		delete(mm.valuesCache, mm.selectedKey)
		*cmds = append(*cmds, mm.loadValuesCmd(mm.selectedKey), mm.spinner.Tick)
	case paneResources:
		delete(mm.resCache, mm.filterDesc)
		*cmds = append(*cmds, mm.loadResourcesCmd(mm.filterDesc, mm.activeGroups, mm.activeTypes), mm.spinner.Tick)
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

// keyRows / valueRows build the list rows with a trailing resource-count cell
// ("…" until the background count lands; "N+" when a region failed mid-count).
func keyRows(keys []string, counts map[string]countVal) []table.Row {
	rows := make([]table.Row, len(keys))
	for i, k := range keys {
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), k, countCell(counts, k)}
	}
	return rows
}

func valueRows(values []string, counts map[string]countVal) []table.Row {
	rows := make([]table.Row, len(values))
	for i, v := range values {
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), v, countCell(counts, v)}
	}
	return rows
}

func countCell(counts map[string]countVal, item string) string {
	cv, ok := counts[item]
	if !ok {
		return "…"
	}
	if cv.complete {
		return fmt.Sprintf("%d", cv.n)
	}
	return fmt.Sprintf("%d+", cv.n) // a region failed → at least this many
}

// rebuildKeyRows / rebuildValueRows refresh the count column in place, preserving
// the cursor so a count landing doesn't move the user's selection.
func (mm *m) rebuildKeyRows() {
	cur := mm.keysTbl.Cursor()
	mm.keysTbl.SetRows(keyRows(mm.keys, mm.keyCounts))
	mm.keysTbl.SetCursor(clampCursor(cur, len(mm.keys)))
}

func (mm *m) rebuildValueRows() {
	cur := mm.valuesTbl.Cursor()
	mm.valuesTbl.SetRows(valueRows(mm.values, mm.valueCounts[mm.selectedKey]))
	mm.valuesTbl.SetCursor(clampCursor(cur, len(mm.values)))
}

func clampCursor(cur, n int) int {
	if n <= 0 {
		return 0
	}
	if cur < 0 {
		return 0
	}
	if cur >= n {
		return n - 1
	}
	return cur
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

// ParseQuery parses an OR-of-AND query into filter groups plus resource-type
// scopes. Groups are separated by "||"; within a group, comma-separated
// Key=Value / Key terms are ANDed (a repeated key ORs its values; a bare key
// matches any value). A "type:SERVICE:TYPE" term in any group scopes the whole
// query to those resource types (e.g. type:ec2:instance). Exposed for the CLI.
func ParseQuery(expr string) (groups []map[string][]string, resourceTypes []string) {
	seenType := map[string]bool{}
	for _, raw := range strings.Split(expr, "||") {
		var terms []string
		for _, part := range strings.Split(raw, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if t, ok := cutTypePrefix(p); ok {
				if t != "" && !seenType[t] {
					seenType[t] = true
					resourceTypes = append(resourceTypes, t)
				}
				continue
			}
			terms = append(terms, p)
		}
		if g := parseFilterExpr(strings.Join(terms, ",")); len(g) > 0 {
			groups = append(groups, g)
		}
	}
	return groups, resourceTypes
}

// cutTypePrefix returns the resource type from a "type:SERVICE:TYPE" term.
func cutTypePrefix(part string) (string, bool) {
	if len(part) >= 5 && strings.EqualFold(part[:5], "type:") {
		return strings.TrimSpace(part[5:]), true
	}
	return "", false
}

// queryDesc renders an OR-of-AND query (and any type scope) as a stable,
// human-readable description used for the title and the result cache key.
func queryDesc(groups []map[string][]string, resourceTypes []string) string {
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		parts = append(parts, filterDesc(g))
	}
	desc := strings.Join(parts, " || ")
	if len(resourceTypes) > 0 {
		ts := append([]string(nil), resourceTypes...)
		sort.Strings(ts)
		if desc != "" {
			desc += " · "
		}
		desc += "type:" + strings.Join(ts, "|")
	}
	return desc
}

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
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// loadTimeout bounds each tag/resource lookup (which fans out across regions).
const loadTimeout = 2 * time.Minute

// focusCol identifies which of the three always-visible columns has the cursor
// (Miller-columns layout, #333). Keys ▸ Values ▸ Resources are all on screen at
// once; focus moves between them rather than swapping the visible pane.
type focusCol int

const (
	colKeys      focusCol = iota // browse tag keys (column 1)
	colValues                    // browse the selected key's values (column 2)
	colResources                 // resources matching the active filter (column 3)
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

	focus focusCol

	// Per-column loading state and non-fatal per-region failures, so one
	// column refreshing never blanks the others and each column flags its own
	// partial results (§6a).
	loadingKeys, loadingValues, loadingResources bool
	keysErrs, valuesErrs, resErrs                []model.ExploreError

	keysTbl   table.Model
	valuesTbl table.Model
	resTbl    table.Model

	keys          []string
	selectedKey   string
	values        []string
	selectedValue string
	resources     []model.Resource
	filterDesc    string                // human description of the active resource filter (display)
	activeGroups  []map[string][]string // the active OR-of-AND filter, for an exact refresh
	activeTypes   []string              // active resource-type scope

	valuesCache map[string][]string
	resCache    map[string][]model.Resource

	// Per-key / per-value resource counts, filled progressively by a background
	// pass (best-effort; count.go). Cached across navigation; countGen/countCh/
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
	overlayVP viewport.Model // scrolls the help overlay (i)
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
		focus:       colKeys,
		loadingKeys: true,
		keysTbl:     newTable([]table.Column{{Title: "#", Width: 4}, {Title: "Tag key", Width: 22}, {Title: "Res", Width: 6}}),
		valuesTbl:   newTable([]table.Column{{Title: "#", Width: 4}, {Title: "Value", Width: 24}, {Title: "Res", Width: 6}}),
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
		{Title: "Name", Width: 26},
		{Title: "Type", Width: 18},
		{Title: "Region", Width: 14},
		{Title: "ID", Width: 26},
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
		mm.loadingKeys = false
		mm.keysErrs = msg.errs
		mm.keys = msg.keys
		mm.keyCounts = map[string]countVal{} // fresh load → recount
		mm.keysTbl.SetRows(keyRows(msg.keys, mm.keyCounts))
		mm.keysTbl.SetCursor(0)
		mm.ensureCounts(colKeys, &cmds)

	case valuesMsg:
		mm.loadingValues = false
		mm.valuesErrs = msg.errs
		mm.values = msg.values
		mm.valuesCache[msg.key] = msg.values
		mm.valueCounts[msg.key] = map[string]countVal{} // fresh load → recount
		if msg.key == mm.selectedKey {
			mm.valuesTbl.SetRows(valueRows(msg.values, mm.valueCounts[msg.key]))
			mm.valuesTbl.SetCursor(0)
		}
		mm.ensureCounts(colValues, &cmds)

	case resourcesMsg:
		mm.loadingResources = false
		mm.resErrs = msg.errs
		mm.resources = msg.resources
		mm.resCache[msg.desc] = msg.resources
		if msg.desc == mm.filterDesc {
			mm.resTbl.SetRows(resourceRows(msg.resources))
			mm.resTbl.SetCursor(0)
		}

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

	// While the help overlay is open, keys scroll it or close it.
	if mm.showAbout {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "i", "?", "esc", "enter":
			mm.showAbout = false
		case "up", "k":
			mm.overlayVP.LineUp(1)
		case "down", "j":
			mm.overlayVP.LineDown(1)
		case "pgup":
			mm.overlayVP.ViewUp()
		case "pgdown", "pgdn", " ":
			mm.overlayVP.ViewDown()
		case "g", "home":
			mm.overlayVP.GotoTop()
		case "G", "end":
			mm.overlayVP.GotoBottom()
		}
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
	case "i":
		mm.showAbout = true
		mm.overlayVP.GotoTop()
		return cmds
	case "r":
		mm.refresh(&cmds)
		return cmds
	case "tab":
		mm.cycleFocus(1, &cmds)
		return cmds
	case "shift+tab":
		mm.cycleFocus(-1, &cmds)
		return cmds
	case "enter", "right", "l":
		mm.descend(&cmds)
		return cmds
	case "left", "h", "esc", "backspace":
		mm.ascend(&cmds)
		return cmds
	}

	// Per-column navigation / actions on the focused column.
	switch mm.focus {
	case colKeys:
		mm.moveList(&mm.keysTbl, msg)
	case colValues:
		mm.moveList(&mm.valuesTbl, msg)
	case colResources:
		mm.handleResourceKey(msg, &cmds)
	}
	return cmds
}

// moveList handles vertical navigation within a focused list column.
func (mm *m) moveList(tbl *table.Model, msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		tbl.MoveUp(1)
	case "down", "j":
		tbl.MoveDown(1)
	case "g", "home":
		tbl.GotoTop()
	case "G", "end":
		tbl.GotoBottom()
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
		if r, ok := mm.selectedResource(); ok {
			if r.ARN != "" {
				_ = clipboard.WriteAll(r.ARN)
				mm.setToast("Copied ARN")
			} else {
				mm.setToast("No ARN to copy for this resource")
			}
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
			} else {
				mm.setToast("No console link for this resource type")
			}
			*cmds = append(*cmds, toastCmd())
		}
	}
}

// descend drills one column to the right: keys → values → resources, loading the
// next column on demand (never on scroll, §7) and moving focus into it.
func (mm *m) descend(cmds *[]tea.Cmd) {
	switch mm.focus {
	case colKeys:
		mm.openKey(cmds)
	case colValues:
		mm.openValue(cmds)
	case colResources:
		// already the deepest column; nothing to drill into.
	}
}

// ascend moves focus one column to the left without reloading (the data is
// already on screen). Returning to the keys column resumes any counts that an
// earlier values pass interrupted.
func (mm *m) ascend(cmds *[]tea.Cmd) {
	switch mm.focus {
	case colResources:
		mm.focus = colValues
	case colValues:
		mm.focus = colKeys
		mm.ensureCounts(colKeys, cmds)
	}
}

// cycleFocus moves focus to the next/previous column (Tab / Shift+Tab),
// resuming counts when landing on a list column.
func (mm *m) cycleFocus(delta int, cmds *[]tea.Cmd) {
	mm.focus = focusCol((int(mm.focus) + delta + 3) % 3)
	switch mm.focus {
	case colKeys:
		mm.ensureCounts(colKeys, cmds)
	case colValues:
		if mm.selectedKey != "" {
			mm.ensureCounts(colValues, cmds)
		}
	}
}

// openKey loads the selected key's values into column 2 (cache-first) and clears
// the now-stale resources column.
func (mm *m) openKey(cmds *[]tea.Cmd) {
	i := mm.keysTbl.Cursor()
	if i < 0 || i >= len(mm.keys) {
		return
	}
	mm.selectedKey = mm.keys[i]
	mm.focus = colValues
	mm.clearResources()
	if cached, ok := mm.valuesCache[mm.selectedKey]; ok {
		mm.values = cached
		mm.valuesErrs = nil
		mm.loadingValues = false
		mm.valuesTbl.SetRows(valueRows(cached, mm.valueCounts[mm.selectedKey]))
		mm.valuesTbl.SetCursor(0)
		mm.ensureCounts(colValues, cmds) // resume any counts not finished before
		return
	}
	mm.values = nil
	mm.valuesTbl.SetRows(nil)
	mm.loadingValues = true
	*cmds = append(*cmds, mm.loadValuesCmd(mm.selectedKey), mm.spinner.Tick)
}

// openValue loads resources for the selected key=value into column 3.
func (mm *m) openValue(cmds *[]tea.Cmd) {
	i := mm.valuesTbl.Cursor()
	if i < 0 || i >= len(mm.values) {
		return
	}
	mm.selectedValue = mm.values[i]
	mm.openResources(mm.selectedKey+"="+mm.selectedValue, []map[string][]string{{mm.selectedKey: {mm.selectedValue}}}, nil, cmds)
}

// openResources fills column 3 for the given OR-of-AND filter (cache-first) and
// moves focus to it.
func (mm *m) openResources(desc string, groups []map[string][]string, resourceTypes []string, cmds *[]tea.Cmd) {
	mm.filterDesc = desc
	mm.activeGroups = groups
	mm.activeTypes = resourceTypes
	mm.focus = colResources
	if cached, ok := mm.resCache[desc]; ok {
		mm.resources = cached
		mm.resErrs = nil
		mm.loadingResources = false
		mm.resTbl.SetRows(resourceRows(cached))
		mm.resTbl.SetCursor(0)
		return
	}
	mm.resources = nil
	mm.resTbl.SetRows(nil)
	mm.loadingResources = true
	*cmds = append(*cmds, mm.loadResourcesCmd(desc, groups, resourceTypes), mm.spinner.Tick)
}

// clearResources resets column 3 when a new key is opened so the old value's
// results aren't shown against the new key.
func (mm *m) clearResources() {
	mm.selectedValue = ""
	mm.resources = nil
	mm.filterDesc = ""
	mm.activeGroups = nil
	mm.activeTypes = nil
	mm.resErrs = nil
	mm.loadingResources = false
	mm.resTbl.SetRows(nil)
}

// refresh reloads the focused column from AWS, bypassing the cache.
func (mm *m) refresh(cmds *[]tea.Cmd) {
	mm.cancelCounts() // stop any in-flight counts; the reload restarts them
	switch mm.focus {
	case colKeys:
		mm.loadingKeys = true
		*cmds = append(*cmds, mm.loadKeysCmd(), mm.spinner.Tick)
	case colValues:
		if mm.selectedKey == "" {
			return
		}
		delete(mm.valuesCache, mm.selectedKey)
		mm.loadingValues = true
		*cmds = append(*cmds, mm.loadValuesCmd(mm.selectedKey), mm.spinner.Tick)
	case colResources:
		if mm.filterDesc == "" {
			return
		}
		delete(mm.resCache, mm.filterDesc)
		mm.loadingResources = true
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
	switch mm.focus {
	case colValues:
		return "AWS Tags › " + mm.selectedKey
	case colResources:
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
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), name, r.Type, r.Region, r.ID}
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

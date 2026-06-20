package tagstui

import (
	"context"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// countWorkers bounds how many resource counts run at once (each is itself a
// per-region fan-out), so a key/value list with many entries can't flood the
// Tagging API.
const countWorkers = 6

// countVal is a resolved count: complete is false when a region failed mid-count
// (rendered "N+"), so a partial sum is never shown as exact.
type countVal struct {
	n        int
	complete bool
}

// countMsg carries one resolved count back to the update loop; countsDoneMsg
// signals the pass drained. Both carry the generation that produced them so a
// stale pass (after navigation/refresh) is ignored.
type countMsg struct {
	gen      int
	pn       pane
	key      string // parent key (values pane)
	item     string // the key (keys pane) or value (values pane)
	n        int
	complete bool
}

type countsDoneMsg struct{ gen int }

// cancelCounts stops any in-flight count pass and invalidates its pending reads
// by bumping the generation.
func (mm *m) cancelCounts() {
	if mm.countCancel != nil {
		mm.countCancel()
		mm.countCancel = nil
	}
	mm.countGen++
}

// ensureCounts (re)starts background counting for the items in the current pane
// that don't already have a cached count. It is best-effort: counts fill in
// progressively and never block navigation. No-op outside the keys/values panes.
func (mm *m) ensureCounts(cmds *[]tea.Cmd) {
	mm.cancelCounts()

	var items []string
	pn := mm.pane
	parent := mm.selectedKey
	switch pn {
	case paneKeys:
		for _, k := range mm.keys {
			if _, ok := mm.keyCounts[k]; !ok {
				items = append(items, k)
			}
		}
	case paneValues:
		existing := mm.valueCounts[parent]
		for _, v := range mm.values {
			if _, ok := existing[v]; !ok {
				items = append(items, v)
			}
		}
	default:
		return
	}
	if len(items) == 0 {
		return
	}

	gen := mm.countGen
	ctx, cancel := context.WithCancel(mm.ctx)
	mm.countCancel = cancel
	ch := make(chan countMsg, len(items)) // buffered so a stopped reader can't block senders
	mm.countCh = ch
	client := mm.client

	go func() {
		sem := make(chan struct{}, countWorkers)
		var wg sync.WaitGroup
		for _, it := range items {
			it := it
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()

				filters := map[string][]string{}
				if pn == paneKeys {
					filters[it] = nil // key present, any value
				} else {
					filters[parent] = []string{it}
				}
				n, complete := client.CountResources(ctx, filters)
				if ctx.Err() != nil {
					return
				}
				ch <- countMsg{gen: gen, pn: pn, key: parent, item: it, n: n, complete: complete}
			}()
		}
		wg.Wait()
		close(ch)
	}()

	*cmds = append(*cmds, mm.readCountCmd(gen, ch))
}

// readCountCmd reads one resolved count from the pass channel; on close it
// reports the pass done. Re-issued by onCount so reads stay one-at-a-time on the
// update loop (the actual counting happens in the worker pool above).
func (mm *m) readCountCmd(gen int, ch <-chan countMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return countsDoneMsg{gen: gen}
		}
		return msg
	}
}

// onCount records a count and refreshes the visible column, then pulls the next.
// Stale-generation messages (after navigation/refresh) are dropped.
func (mm *m) onCount(msg countMsg) tea.Cmd {
	if msg.gen != mm.countGen {
		return nil
	}
	switch msg.pn {
	case paneKeys:
		mm.keyCounts[msg.item] = countVal{n: msg.n, complete: msg.complete}
		if mm.pane == paneKeys {
			mm.rebuildKeyRows()
		}
	case paneValues:
		if mm.valueCounts[msg.key] == nil {
			mm.valueCounts[msg.key] = map[string]countVal{}
		}
		mm.valueCounts[msg.key][msg.item] = countVal{n: msg.n, complete: msg.complete}
		if mm.pane == paneValues && mm.selectedKey == msg.key {
			mm.rebuildValueRows()
		}
	}
	return mm.readCountCmd(msg.gen, mm.countCh)
}

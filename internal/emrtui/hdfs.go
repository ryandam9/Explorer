package emrtui

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

// HDFS / NameNode browser (Layer 2): reads the NameNode's JMX (port 9870) over
// the on-cluster connection layer and surfaces cluster-wide HDFS health plus the
// per-DataNode picture. The parse functions are pure over the JMX payload so
// they are fixture-tested; the fetch wrapper degrades to the connect helper when
// the NameNode can't be reached (same contract as the YARN/HBase/Oozie browsers).

// HDFSStatus is the NameNode's view of the filesystem.
type HDFSStatus struct {
	CapacityTotal     int64      `json:"capacityTotal"`
	CapacityUsed      int64      `json:"capacityUsed"`
	CapacityRemaining int64      `json:"capacityRemaining"`
	PercentUsed       float64    `json:"percentUsed"`
	LiveDataNodes     int        `json:"liveDataNodes"`
	DeadDataNodes     int        `json:"deadDataNodes"`
	FilesTotal        int64      `json:"filesTotal"`
	BlocksTotal       int64      `json:"blocksTotal"`
	MissingBlocks     int64      `json:"missingBlocks"`
	UnderReplicated   int64      `json:"underReplicatedBlocks"`
	CorruptBlocks     int64      `json:"corruptBlocks"`
	Safemode          string     `json:"safemode"` // "" = off (operational); otherwise the message
	Version           string     `json:"version"`
	DataNodes         []DataNode `json:"dataNodes"`
}

// DataNode is one DataNode from the NameNode's LiveNodes/DeadNodes JMX maps.
type DataNode struct {
	Name        string `json:"name"`  // host:port
	State       string `json:"state"` // In Service / Decommissioned / Dead
	Capacity    int64  `json:"capacity"`
	Used        int64  `json:"used"`
	Remaining   int64  `json:"remaining"`
	NumBlocks   int64  `json:"numBlocks"`
	LastContact int64  `json:"lastContactSecs"`
}

// --- JMX wire parsing --------------------------------------------------------

type jmxDump struct {
	Beans []map[string]json.RawMessage `json:"beans"`
}

// parseHDFS extracts HDFS health from a NameNode /jmx payload. Pure. A bean that
// isn't present simply leaves its fields zero (best-effort), never an error.
func parseHDFS(body []byte) (HDFSStatus, error) {
	var dump jmxDump
	if err := json.Unmarshal(body, &dump); err != nil {
		return HDFSStatus{}, err
	}
	beans := map[string]map[string]json.RawMessage{}
	for _, b := range dump.Beans {
		if raw, ok := b["name"]; ok {
			var name string
			if json.Unmarshal(raw, &name) == nil {
				beans[name] = b
			}
		}
	}

	var s HDFSStatus
	if state := beans["Hadoop:service=NameNode,name=FSNamesystemState"]; state != nil {
		s.CapacityTotal = rawInt64(state, "CapacityTotal")
		s.CapacityUsed = rawInt64(state, "CapacityUsed")
		s.CapacityRemaining = rawInt64(state, "CapacityRemaining")
		s.LiveDataNodes = int(rawInt64(state, "NumLiveDataNodes"))
		s.DeadDataNodes = int(rawInt64(state, "NumDeadDataNodes"))
		s.FilesTotal = rawInt64(state, "FilesTotal")
		s.BlocksTotal = rawInt64(state, "BlocksTotal")
	}
	if fs := beans["Hadoop:service=NameNode,name=FSNamesystem"]; fs != nil {
		s.MissingBlocks = rawInt64(fs, "MissingBlocks")
		s.UnderReplicated = rawInt64(fs, "UnderReplicatedBlocks")
		s.CorruptBlocks = rawInt64(fs, "CorruptBlocks")
	}
	if info := beans["Hadoop:service=NameNode,name=NameNodeInfo"]; info != nil {
		s.PercentUsed = rawFloat(info, "PercentUsed")
		s.Safemode = rawString(info, "Safemode")
		s.Version = rawString(info, "Version")
		s.DataNodes = append(s.DataNodes, parseDataNodes(rawString(info, "LiveNodes"))...)
		for _, dn := range parseDataNodes(rawString(info, "DeadNodes")) {
			dn.State = "Dead"
			s.DataNodes = append(s.DataNodes, dn)
		}
	}
	sort.SliceStable(s.DataNodes, func(i, j int) bool { return s.DataNodes[i].Name < s.DataNodes[j].Name })
	return s, nil
}

// dnInfo is one entry in the NameNode's LiveNodes/DeadNodes JSON-in-a-string.
type dnInfo struct {
	InfoAddr    string `json:"infoAddr"`
	LastContact int64  `json:"lastContact"`
	UsedSpace   int64  `json:"usedSpace"`
	Capacity    int64  `json:"capacity"`
	Remaining   int64  `json:"remaining"`
	NumBlocks   int64  `json:"numBlocks"`
	AdminState  string `json:"adminState"`
}

// parseDataNodes decodes the NameNode's LiveNodes/DeadNodes value — a JSON
// object (host:port -> info) that the JMX bean delivers as a quoted string.
func parseDataNodes(s string) []DataNode {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]dnInfo
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	out := make([]DataNode, 0, len(m))
	for host, in := range m {
		state := in.AdminState
		if state == "" {
			state = "In Service"
		}
		out = append(out, DataNode{
			Name: host, State: state, Capacity: in.Capacity, Used: in.UsedSpace,
			Remaining: in.Remaining, NumBlocks: in.NumBlocks, LastContact: in.LastContact,
		})
	}
	return out
}

// FetchHDFS fetches HDFS health from a cluster's NameNode via the on-cluster
// connection layer. Returns an ErrUnreachable-wrapped error when the NameNode
// can't be reached, so callers render the connect helper.
func FetchHDFS(ctx context.Context, d *emrconn.Dialer, masterDNS string) (HDFSStatus, error) {
	if d == nil {
		return HDFSStatus{}, emrconn.ErrDisabled
	}
	if masterDNS == "" {
		return HDFSStatus{}, fmt.Errorf("%w: cluster has no primary-node DNS (not running?)", emrconn.ErrUnreachable)
	}
	body, err := d.GetRaw(ctx, emrconn.ServiceNameNode, masterDNS, "/jmx")
	if err != nil {
		return HDFSStatus{}, err
	}
	return parseHDFS(body)
}

// SafemodeOn reports whether HDFS is in safe mode (read-only).
func (s HDFSStatus) SafemodeOn() bool { return strings.TrimSpace(s.Safemode) != "" }

// --- CLI rendering -----------------------------------------------------------

// RenderHDFS writes HDFS status as table / json / ndjson / csv — the CLI twin of
// the dashboard's HDFS browser.
func RenderHDFS(w io.Writer, s HDFSStatus, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, s)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, dn := range s.DataNodes {
			if err := enc.Encode(dn); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Node", "State", "Used", "Capacity", "UsedPct", "Blocks", "LastContactSecs"})
		}
		for _, dn := range s.DataNodes {
			_ = cw.Write([]string{dn.Name, dn.State, itoa64(dn.Used), itoa64(dn.Capacity),
				dnUsedPct(dn), itoa64(dn.NumBlocks), itoa64(dn.LastContact)})
		}
		cw.Flush()
		return cw.Error()
	default:
		return renderHDFSTable(w, s)
	}
}

func renderHDFSTable(w io.Writer, s HDFSStatus) error {
	var b strings.Builder
	fmt.Fprintf(&b, "HDFS — NameNode %s\n", dash(s.Version))
	fmt.Fprintf(&b, "  Capacity      %s used / %s total (%.1f%%), %s free\n",
		humanBytes(s.CapacityUsed), humanBytes(s.CapacityTotal), s.PercentUsed, humanBytes(s.CapacityRemaining))
	fmt.Fprintf(&b, "  DataNodes     %d live · %d dead\n", s.LiveDataNodes, s.DeadDataNodes)
	fmt.Fprintf(&b, "  Files/blocks  %s files · %s blocks\n", itoa64(s.FilesTotal), itoa64(s.BlocksTotal))
	fmt.Fprintf(&b, "  Block health  %s missing · %s under-replicated · %s corrupt\n",
		itoa64(s.MissingBlocks), itoa64(s.UnderReplicated), itoa64(s.CorruptBlocks))
	fmt.Fprintf(&b, "  Safe mode     %s\n", safemodeLabel(s))
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}

	if len(s.DataNodes) == 0 {
		_, err := fmt.Fprintln(w, "\n  (no DataNodes reported)")
		return err
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NODE\tSTATE\tUSED\tCAPACITY\tUSED%\tBLOCKS\tLAST CONTACT")
	for _, dn := range s.DataNodes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%ss\n",
			dn.Name, dn.State, humanBytes(dn.Used), humanBytes(dn.Capacity),
			dnUsedPct(dn), itoa64(dn.NumBlocks), itoa64(dn.LastContact))
	}
	return tw.Flush()
}

func safemodeLabel(s HDFSStatus) string {
	if s.SafemodeOn() {
		return "ON — " + strings.TrimSpace(s.Safemode)
	}
	return "off"
}

func dnUsedPct(dn DataNode) string {
	if dn.Capacity <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(dn.Used)/float64(dn.Capacity)*100)
}

// --- small helpers -----------------------------------------------------------

func rawInt64(m map[string]json.RawMessage, key string) int64 {
	raw, ok := m[key]
	if !ok {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int64(f)
	}
	return 0
}

func rawFloat(m map[string]json.RawMessage, key string) float64 {
	raw, ok := m[key]
	if !ok {
		return 0
	}
	var f float64
	_ = json.Unmarshal(raw, &f)
	return f
}

func rawString(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func itoa64(n int64) string { return fmt.Sprintf("%d", n) }

// humanBytes renders a byte count in binary units (KiB/MiB/GiB/TiB/PiB).
func humanBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

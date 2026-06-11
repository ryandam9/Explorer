// Package summary renders a flat, numbered inventory of every collected AWS
// resource. It powers the `aws_explorer summary` command, presenting each
// resource as a single row: serial number, name, resource type, ARN, and
// region/AZ.
package summary

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/user/aws_explorer/internal/model"
)

// Row is one line of the summary inventory.
type Row struct {
	SNO      int    `json:"sno"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	ARN      string `json:"arn"`
	RegionAZ string `json:"regionAZ"`
}

// placeholder is shown for any field a resource doesn't provide.
const placeholder = "-"

// BuildRows converts collected resources into numbered summary rows. Resources
// sharing an ARN are deduplicated (richer entry wins) so the broad Tagging API
// sweep and the rich typed collectors can be merged freely. The result is sorted
// by service, type, name and region so serial numbers are stable regardless of
// collection order.
func BuildRows(resources []model.Resource) []Row {
	deduped := Dedupe(resources)

	sorted := make([]model.Resource, len(deduped))
	copy(sorted, deduped)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Region < b.Region
	})

	rows := make([]Row, 0, len(sorted))
	for i, r := range sorted {
		rows = append(rows, Row{
			SNO:      i + 1,
			Name:     dash(r.Name),
			Type:     resourceType(r),
			ARN:      dash(r.ARN),
			RegionAZ: regionAZ(r),
		})
	}
	return rows
}

// Dedupe collapses resources that share a (non-empty) ARN, keeping the richer
// entry. Resources without an ARN cannot be matched and are all retained. This
// lets the universal Tagging API sweep be merged with the typed collectors: when
// both describe the same ARN, the typed entry (with state, AZ, summary fields)
// wins.
func Dedupe(resources []model.Resource) []model.Resource {
	byARN := make(map[string]int) // ARN -> index into out
	out := make([]model.Resource, 0, len(resources))
	for _, r := range resources {
		if r.ARN == "" {
			out = append(out, r)
			continue
		}
		if idx, seen := byARN[r.ARN]; seen {
			if Richness(r) > Richness(out[idx]) {
				out[idx] = r
			}
			continue
		}
		byARN[r.ARN] = len(out)
		out = append(out, r)
	}
	return out
}

// Richness scores how much detail a resource carries, used to pick a winner when
// two entries share an ARN. Typed collectors populate state/AZ/summary/detail and
// therefore outscore the ARN-and-tags-only entries from the Tagging API. Exported
// so streaming consumers (the TUI) can apply the same rule incrementally.
func Richness(r model.Resource) int {
	score := 0
	if r.State != "" {
		score++
	}
	if r.AZ != "" {
		score++
	}
	if r.CreatedAt != nil {
		score++
	}
	score += len(r.Summary)
	score += len(r.Details)
	return score
}

// resourceType renders the resource type as "service/type" (e.g. "ec2/instance",
// "s3/bucket"), which uniquely identifies what the resource is.
func resourceType(r model.Resource) string {
	switch {
	case r.Service == "" && r.Type == "":
		return placeholder
	case r.Type == "":
		return r.Service
	case r.Service == "":
		return r.Type
	default:
		return r.Service + "/" + r.Type
	}
}

// regionAZ combines region and availability zone. AZ is only shown when present
// and distinct from the region (most resources are region-scoped and have none).
func regionAZ(r model.Resource) string {
	if r.Region == "" {
		return placeholder
	}
	if r.AZ != "" && r.AZ != r.Region {
		return r.Region + " / " + r.AZ
	}
	return r.Region
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return placeholder
	}
	return s
}

// Render writes the rows to w in the requested format (table, json, csv).
// Unknown formats fall back to table.
func Render(w io.Writer, rows []Row, format string) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, rows)
	case "csv":
		return renderCSV(w, rows)
	default:
		return renderTable(w, rows)
	}
}

func renderTable(w io.Writer, rows []Row) error {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "SNO\tNAME\tTYPE\tARN\tREGION/AZ")
	for _, r := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", r.SNO, r.Name, r.Type, r.ARN, r.RegionAZ)
	}
	return tw.Flush()
}

func renderJSON(w io.Writer, rows []Row) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func renderCSV(w io.Writer, rows []Row) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"SNO", "Name", "Type", "ARN", "Region/AZ"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write([]string{fmt.Sprintf("%d", r.SNO), r.Name, r.Type, r.ARN, r.RegionAZ}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

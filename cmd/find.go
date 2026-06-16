package cmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/discovery"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/fuzzy"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/summary"
)

var findLimit int

var findCmd = &cobra.Command{
	Use:   "find <fragment>",
	Short: "Fuzzy-find any resource by name, ID, ARN or type",
	Long: `Find scans the configured regions (typed collectors plus the all-services
Tagging API sweep) and fuzzy-matches every resource against the fragment —
name, ID, ARN, type and region all count. Best matches print first.

The match is an in-order subsequence, so separators can be skipped:
"eni0abc" finds eni-0abc12, "prodweb" finds prod-web-3.

This is the CLI twin of the summary TUI's Ctrl+P jump palette.`,
	Example: `  # What is this ENI from an error message?
  aws_explorer find eni-0abc

  # Find by name fragment across every region
  aws_explorer find prodweb --all-regions

  # Machine-readable
  aws_explorer find payments -o json | jq '.[0].arn'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.TrimSpace(args[0])
		if query == "" {
			return fmt.Errorf("the search fragment must not be empty")
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
		SilenceScanLogs()

		result, err := eng.Run(ctx)
		if err != nil {
			return fmt.Errorf("collecting resources: %w", err)
		}
		resources := result.Resources
		errs := result.Errors

		// The Tagging API sweep gives the long tail of services, exactly the
		// resources a mystery ID tends to belong to.
		discovered, dErrs := discovery.Discover(
			ctx, eng.AWSConfig, eng.EffectiveRegions(), AppConfig.App.MaxConcurrency)
		resources = append(resources, discovered...)
		errs = append(errs, dErrs...)
		output.PrintErrors(os.Stderr, errs)

		matched := matchResources(query, summary.Dedupe(resources), findLimit)
		if len(matched) == 0 {
			fmt.Println("No resources match.")
			return nil
		}
		return renderFindResults(os.Stdout, matched, outputFormat, noHeader)
	},
}

// matchResources ranks resources against the query, best first, capped at
// limit.
func matchResources(query string, resources []model.Resource, limit int) []model.Resource {
	candidates := make([]string, len(resources))
	for i, r := range resources {
		candidates[i] = strings.ToLower(strings.Join([]string{
			r.Name, r.ID, r.ARN, r.Service, r.Type, r.Region,
		}, " "))
	}
	hits := fuzzy.Rank(query, candidates, limit)
	matched := make([]model.Resource, 0, len(hits))
	for _, h := range hits {
		matched = append(matched, resources[h.Index])
	}
	return matched
}

// findRow is one printed match, in rank order.
type findRow struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	ID     string `json:"id"`
	Region string `json:"region"`
	ARN    string `json:"arn"`
}

func toFindRows(matched []model.Resource) []findRow {
	rows := make([]findRow, 0, len(matched))
	for _, r := range matched {
		typ := r.Service
		if r.Type != "" {
			typ += "/" + r.Type
		}
		name := r.Name
		if name == "" {
			name = "-"
		}
		arn := r.ARN
		if arn == "" {
			arn = "-"
		}
		rows = append(rows, findRow{Name: name, Type: typ, ID: r.ID, Region: r.Region, ARN: arn})
	}
	return rows
}

// renderFindResults prints matches in rank order (unlike summary's
// service-sorted inventory, ranking is the point here).
func renderFindResults(w io.Writer, matched []model.Resource, format string, noHeader bool) error {
	rows := toFindRows(matched)
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			if err := cw.Write([]string{"Name", "Type", "ID", "Region", "ARN"}); err != nil {
				return err
			}
		}
		for _, r := range rows {
			if err := cw.Write(csvexport.SanitizeRow([]string{r.Name, r.Type, r.ID, r.Region, r.ARN})); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "SNO\tNAME\tTYPE\tID\tREGION\tARN")
		}
		for i, r := range rows {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", i+1, r.Name, r.Type, r.ID, r.Region, r.ARN)
		}
		return tw.Flush()
	}
}

func init() {
	findCmd.Flags().IntVar(&findLimit, "limit", 25, "maximum number of matches to print")
	rootCmd.AddCommand(findCmd)
}

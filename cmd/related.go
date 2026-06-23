package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/relatedtui"
	"github.com/ryandam9/aws_explorer/internal/ui"
	"github.com/ryandam9/aws_explorer/internal/xref"
)

const relatedMaxDepth = 5

var (
	relatedDepth     int
	relatedDirection string
	relatedTUI       bool
)

var relatedCmd = &cobra.Command{
	Use:   "related <arn-or-id>",
	Short: "Related resources — what a resource uses and what uses it",
	Long: `Related shows everything linked to a resource, in both directions:

  Uses (depends on) →   what the resource references (a Lambda's execution role,
                        a volume's KMS key, an instance's security groups …)
  Used by ←             what references the resource (the where-used answer)

It scans the account for the linking fields the inventory does not keep and
walks them from the resource you name. Pass a full ARN or a bare ID (an IAM
role name, an sg-… id, etc.). With --depth it follows links several hops out
(e.g. a Lambda → its role → that role's trust principals).

This is a resource-to-resource reference graph, not a container inventory:
querying a VPC/subnet/route-table id won't list what lives inside it (use
'aws_explorer vpc' for that). Identifiers it doesn't recognize as a supported
kind are still queried as raw links, flagged with a note.

Both directions are read-only and best-effort: a denied or failed API call
narrows what was checked (reported on stderr) and never aborts the run. The
report only reflects the relationship types this tool extracts, so an empty
side means "none of the collected link types" — not "this resource is isolated".

This generalizes 'whereused' (which answers only the "used by" direction).`,
	Example: `  # Everything linked to a Lambda function (both directions)
  aws_explorer related arn:aws:lambda:us-east-1:123456789012:function:checkout

  # Only what this security group is attached to
  aws_explorer related sg-0abc123 --direction usedby -r eu-west-1

  # Follow links two hops out, across all regions
  aws_explorer related arn:aws:iam::123456789012:role/app --depth 2 --all-regions

  # Machine-readable
  aws_explorer related sg-0abc123 -o json | jq '.uses'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		showUses, showUsedBy, err := parseDirection(relatedDirection)
		if err != nil {
			return err
		}
		depth := relatedDepth
		if depth < 1 {
			depth = 1
		}
		if depth > relatedMaxDepth {
			return fmt.Errorf("--depth %d too large; maximum is %d", relatedDepth, relatedMaxDepth)
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
		SilenceScanLogs()

		regions := eng.EffectiveRegions()
		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second

		if relatedTUI {
			ui.InitFromConfig(AppConfig.UI)
			if idx, ok := ui.LookupTheme(resolveTheme(cmd, "")); ok {
				ui.SetActiveTheme(idx)
			}
			model := relatedtui.NewModel(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout, allRegions || AppConfig.AWS.AllRegions, args[0])
			p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("running related explorer: %w", err)
			}
			return nil
		}

		fmt.Fprintf(os.Stderr, "Scanning %d region(s) for resources related to %s…\n", len(regions), args[0])
		// The per-role IAM policy sweep is only needed when the query can land on
		// a role: the target itself is a role, or a multi-hop walk could reach
		// one. Skipping it for the common non-role, depth-1 case avoids the
		// expensive (and previously deadline-storming) sweep (§7).
		includeRolePolicies := depth > 1 || xref.Classify(args[0]).Kind == xref.KindIAMRole
		edges, errs := xref.Collect(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout, includeRolePolicies)
		output.PrintErrors(os.Stderr, errs)

		result := xref.Related(args[0], xref.BuildForwardIndex(edges), xref.BuildIndex(edges), depth)
		if err := xref.RenderRelated(os.Stdout, result, outputFormat, noHeader, showUses, showUsedBy, len(errs) > 0); err != nil {
			return fmt.Errorf("rendering report: %w", err)
		}
		return nil
	},
}

// parseDirection maps the --direction flag to which sections to show.
func parseDirection(d string) (showUses, showUsedBy bool, err error) {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "", "both":
		return true, true, nil
	case "uses", "forward":
		return true, false, nil
	case "usedby", "used-by", "reverse":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("invalid --direction %q; want both, uses, or usedby", d)
	}
}

func init() {
	relatedCmd.Flags().IntVar(&relatedDepth, "depth", 1, fmt.Sprintf("how many hops to follow (1-%d)", relatedMaxDepth))
	relatedCmd.Flags().StringVar(&relatedDirection, "direction", "both", "which links to show: both, uses, usedby")
	relatedCmd.Flags().BoolVar(&relatedTUI, "tui", false, "open the interactive related-resources explorer")
	rootCmd.AddCommand(relatedCmd)
}

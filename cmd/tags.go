package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/tagstui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	tagsTheme  string
	tagsKey    string
	tagsFilter string
	tagsType   string
)

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Explore AWS resources by tag",
	Long: `Explore AWS resources by tag. Browse the account's tag keys, drill into a
key's values, and press Enter to list every resource carrying that tag — or
press f to type one or more Key=Value filters directly (comma-separated; repeat
a key to OR its values; a bare key matches any value).

Data comes from the Resource Groups Tagging API, so only tagged resources on
services that integrate with it are shown (IAM, for example, is not). On a
resource, y copies its ARN and o opens it in the AWS console.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region; otherwise the config's aws.regions list is
used (defaulting to us-east-1).`,
	Example: `  # Browse tags in the configured region
  aws_explorer tags

  # Sweep every region
  aws_explorer tags --all-regions`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, tagsTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		SilenceScanLogs()

		regions, scanAll := tagsRegionScope()
		client, err := tagstui.NewClient(ctx, tuiAWSConfig(), regions, scanAll)
		if err != nil {
			return fmt.Errorf("initializing tags dashboard: %w", err)
		}

		model := tagstui.NewModel(ctx, client, scanAll)
		p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running tags dashboard: %w", err)
		}
		return nil
	},
}

// tagsRegionScope resolves the region list and all-regions flag the same way the
// other dashboards do.
func tagsRegionScope() ([]string, bool) {
	switch {
	case awsRegion != "":
		return []string{awsRegion}, false
	case allRegions || (AppConfig != nil && AppConfig.AWS.AllRegions):
		return nil, true
	case AppConfig != nil && len(AppConfig.AWS.Regions) > 0:
		return AppConfig.AWS.Regions, false
	default:
		return []string{"us-east-1"}, false
	}
}

// newTagsClient builds the shared tags client for the CLI twins.
func newTagsClient(ctx context.Context) (*tagstui.Client, error) {
	regions, scanAll := tagsRegionScope()
	return tagstui.NewClient(ctx, tuiAWSConfig(), regions, scanAll)
}

// warnTagErrors reports per-region failures (and the coverage caveat) on stderr,
// so stdout stays clean for scripts.
func warnTagErrors(errs []model.ExploreError) {
	fmt.Fprintln(os.Stderr, tagstui.CoverageNote)
	if len(errs) == 0 {
		return
	}
	regions := make([]string, 0, len(errs))
	for _, e := range errs {
		regions = append(regions, e.Region)
	}
	fmt.Fprintf(os.Stderr, "warning: %d region(s) failed (%s) — results may be incomplete\n",
		len(errs), strings.Join(regions, ", "))
}

var tagsKeysCmd = &cobra.Command{
	Use:     "keys",
	Short:   "List the tag keys in use in the account",
	Example: "  aws_explorer tags keys --all-regions -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newTagsClient(ctx)
		if err != nil {
			return err
		}
		keys, errs := client.TagKeys(ctx)
		warnTagErrors(errs)
		return tagstui.RenderStrings(os.Stdout, keys, "Tag key", outputFormat, noHeader)
	},
}

var tagsValuesCmd = &cobra.Command{
	Use:     "values",
	Short:   "List the values configured for a tag key",
	Example: "  aws_explorer tags values --key Environment -o csv",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		if strings.TrimSpace(tagsKey) == "" {
			return fmt.Errorf("specify a tag key with --key")
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newTagsClient(ctx)
		if err != nil {
			return err
		}
		vals, errs := client.TagValues(ctx, tagsKey)
		warnTagErrors(errs)
		return tagstui.RenderStrings(os.Stdout, vals, "Value", outputFormat, noHeader)
	},
}

var tagsResourcesCmd = &cobra.Command{
	Use:   "resources",
	Short: "List resources matching one or more tag filters",
	Long: `List resources matching a tag filter, e.g. --filter "Environment=prod,Team=payments".

Different keys are ANDed; repeating a key ORs its values; a bare key matches any
value (e.g. --filter Owner). Separate AND-groups with "||" to OR them, e.g.
--filter "Team=payments || Team=billing". Scope to resource types with --type
(or a type:ec2:instance term in --filter).

Note: the Resource Groups Tagging API can only match *tagged* resources, so
there is no "untagged" / negation filter.`,
	Example: `  aws_explorer tags resources --filter Environment=prod --all-regions -o json
  aws_explorer tags resources --filter "Team=payments || Team=billing" -o csv
  aws_explorer tags resources --filter Environment=prod --type ec2:instance,s3:bucket`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		groups, types := tagstui.ParseQuery(tagsFilter)
		types = append(types, splitCSV(tagsType)...)
		if len(groups) == 0 && len(types) == 0 {
			return fmt.Errorf("specify a tag filter with --filter Key=Value[,Key2=Value2] (and/or --type)")
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newTagsClient(ctx)
		if err != nil {
			return err
		}
		res, errs := client.Resources(ctx, groups, types)
		warnTagErrors(errs)
		return tagstui.RenderResources(os.Stdout, res, outputFormat, noHeader)
	},
}

// splitCSV splits a comma-separated flag value into trimmed, non-empty items.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func init() {
	tagsCmd.Flags().StringVar(&tagsTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(tagsCmd)
	registerThemeCompletion(tagsCmd, ui.ThemeNames())

	tagsValuesCmd.Flags().StringVar(&tagsKey, "key", "", "Tag key to list values for (required)")
	_ = tagsValuesCmd.MarkFlagRequired("key")
	tagsResourcesCmd.Flags().StringVar(&tagsFilter, "filter", "", `Tag filter, e.g. "Key=Value,Key2=Value2" ("||" to OR groups)`)
	tagsResourcesCmd.Flags().StringVar(&tagsType, "type", "", "Comma-separated resource types to scope to, e.g. ec2:instance,s3:bucket")

	tagsCmd.AddCommand(tagsKeysCmd, tagsValuesCmd, tagsResourcesCmd)
	rootCmd.AddCommand(tagsCmd)
}

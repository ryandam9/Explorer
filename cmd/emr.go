package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/emrconn"
	"github.com/ryandam9/aws_explorer/internal/emrtui"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var emrTheme string

var emrCmd = &cobra.Command{
	Use:   "emr",
	Short: "Start the Amazon EMR dashboard TUI",
	Long: `Start an interactive dashboard for Amazon EMR: clusters (with their release
label, installed applications, size and state) and a per-cluster step history.
Press Enter on a cluster to drill into its steps — state, duration and
action-on-failure, with the failure reason inline on a failed step. Press d for
the cluster detail (log URI, role, EC2 attributes).

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region and adds a Region column; otherwise the
config's aws.regions list is used.`,
	Example: `  # Browse EMR in the configured regions
  aws_explorer emr

  # Pin one region
  aws_explorer emr --region us-east-1`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		emrCfg := tuiAWSConfig()

		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, emrTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		SilenceScanLogs()

		regions, scanAll := emrRegionScope()

		model, err := emrtui.NewModel(ctx, emrCfg, regions, scanAll, AppConfig, configFilePath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing EMR dashboard: %v\n", err)
			os.Exit(1)
		}

		p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running EMR dashboard: %v\n", err)
			os.Exit(1)
		}
	},
}

var (
	emrStepsLimit   int
	emrStepsStatus  string
	emrClusterState string
)

// emrRegionScope resolves the region list and all-regions flag the same way the
// dashboard does, so the CLI twins honour --region / --all-regions / config.
func emrRegionScope() ([]string, bool) {
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

// newEMRClient builds the shared EMR client for the CLI twins.
func newEMRClient(ctx context.Context) (*emrtui.Client, error) {
	regions, scanAll := emrRegionScope()
	return emrtui.NewClient(ctx, tuiAWSConfig(), regions, scanAll)
}

var emrClustersCmd = &cobra.Command{
	Use:     "clusters",
	Short:   "List EMR clusters with their release, applications and state",
	Example: "  aws_explorer emr clusters --all-regions -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		clusters := emrtui.FilterClustersByState(inv.Clusters, emrClusterState)
		return emrtui.RenderClusters(os.Stdout, clusters, outputFormat, noHeader)
	},
}

var emrStepsCmd = &cobra.Command{
	Use:   "steps <cluster-id>",
	Short: "Show an EMR cluster's step history (state, duration, failure reason)",
	Args:  cobra.ExactArgs(1),
	Example: `  aws_explorer emr steps j-1A2B3C4D5 -r us-east-1
  aws_explorer emr steps j-1A2B3C4D5 --status FAILED -o json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		// Steps are region-specific: use --region when given, else the first
		// region in scope.
		region := awsRegion
		if region == "" && len(client.Regions()) > 0 {
			region = client.Regions()[0]
		}
		steps, err := client.Steps(ctx, region, args[0], emrStepsLimit)
		if err != nil {
			return fmt.Errorf("failed to get steps for cluster %q in %s: %w", args[0], region, err)
		}
		steps = emrtui.FilterStepsByStatus(steps, emrStepsStatus)
		return emrtui.RenderSteps(os.Stdout, steps, outputFormat, noHeader)
	},
}

var emrInstancesLimit int

// emrRegionForCommand resolves the region a per-cluster twin should query.
func emrRegionForCommand(client *emrtui.Client) string {
	region := awsRegion
	if region == "" && len(client.Regions()) > 0 {
		region = client.Regions()[0]
	}
	return region
}

var emrInstancesCmd = &cobra.Command{
	Use:     "instances <cluster-id>",
	Short:   "List an EMR cluster's EC2 instances",
	Args:    cobra.ExactArgs(1),
	Example: "  aws_explorer emr instances j-1A2B3C4D5 -r us-east-1 -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)
		instances, err := client.Instances(ctx, region, args[0], emrInstancesLimit)
		if err != nil {
			return fmt.Errorf("failed to get instances for cluster %q in %s: %w", args[0], region, err)
		}
		return emrtui.RenderInstances(os.Stdout, instances, outputFormat, noHeader)
	},
}

var emrAppsCmd = &cobra.Command{
	Use:     "apps <cluster-id>",
	Short:   "List an EMR cluster's installed applications and versions",
	Args:    cobra.ExactArgs(1),
	Example: "  aws_explorer emr apps j-1A2B3C4D5 -r us-east-1",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)
		apps, err := client.Apps(ctx, region, args[0])
		if err != nil {
			return fmt.Errorf("failed to get applications for cluster %q in %s: %w", args[0], region, err)
		}
		return emrtui.RenderApps(os.Stdout, apps, outputFormat, noHeader)
	},
}

var emrYarnCmd = &cobra.Command{
	Use:   "yarn <cluster-id>",
	Short: "List a cluster's live YARN applications (requires on-cluster access)",
	Long: `List the live YARN applications running on an EMR cluster, read from the
ResourceManager REST API on the cluster's primary node.

This needs on-cluster access (emr.onCluster in config) because YARN has no AWS
API — it runs on the cluster's primary node, reachable only from inside the VPC
(directly, or through a SOCKS proxy such as an 'ssh -D' dynamic tunnel).`,
	Args:    cobra.ExactArgs(1),
	Example: "  aws_explorer emr yarn j-1A2B3C4D5 -r us-east-1 -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()

		var onCluster config.OnClusterConfig
		if AppConfig != nil {
			onCluster = AppConfig.EMR.OnCluster
		}
		dialer, err := emrconn.New(onCluster)
		if err != nil {
			return fmt.Errorf("on-cluster access not available: %w\n\nEnable it in config.yaml under emr.onCluster (mode: socks|direct)", err)
		}

		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)
		dns, err := client.MasterDNS(ctx, region, args[0])
		if err != nil {
			return fmt.Errorf("failed to resolve cluster %q primary DNS in %s: %w", args[0], region, err)
		}
		apps, _, err := emrtui.FetchYARN(ctx, dialer, dns)
		if err != nil {
			return fmt.Errorf("failed to query YARN on cluster %q: %w", args[0], err)
		}
		return emrtui.RenderYARNApps(os.Stdout, apps, outputFormat, noHeader)
	},
}

var emrHBaseCmd = &cobra.Command{
	Use:   "hbase <cluster-id>",
	Short: "List a cluster's HBase tables (requires on-cluster access)",
	Long: `List the HBase tables on an EMR cluster — namespace, derived state, region
counts and column families — read from the HBase REST server on the cluster's
primary node.

This needs on-cluster access (emr.onCluster in config) because HBase has no AWS
API — it runs on the cluster's primary node, reachable only from inside the VPC
(directly, or through a SOCKS proxy such as an 'ssh -D' dynamic tunnel).`,
	Args:    cobra.ExactArgs(1),
	Example: "  aws_explorer emr hbase j-1A2B3C4D5 -r us-east-1 -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()

		var onCluster config.OnClusterConfig
		if AppConfig != nil {
			onCluster = AppConfig.EMR.OnCluster
		}
		dialer, err := emrconn.New(onCluster)
		if err != nil {
			return fmt.Errorf("on-cluster access not available: %w\n\nEnable it in config.yaml under emr.onCluster (mode: socks|direct)", err)
		}

		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)
		dns, err := client.MasterDNS(ctx, region, args[0])
		if err != nil {
			return fmt.Errorf("failed to resolve cluster %q primary DNS in %s: %w", args[0], region, err)
		}
		tables, err := emrtui.FetchHBase(ctx, dialer, dns)
		if err != nil {
			return fmt.Errorf("failed to query HBase on cluster %q: %w", args[0], err)
		}
		return emrtui.RenderHBaseTables(os.Stdout, tables, outputFormat, noHeader)
	},
}

func init() {
	emrCmd.Flags().StringVar(&emrTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(emrCmd)
	registerThemeCompletion(emrCmd, ui.ThemeNames())

	emrClustersCmd.Flags().StringVar(&emrClusterState, "state", "", "only show clusters in these states (comma-separated, e.g. RUNNING,WAITING)")

	emrStepsCmd.Flags().IntVar(&emrStepsLimit, "limit", 50, "maximum number of steps to fetch")
	emrStepsCmd.Flags().StringVar(&emrStepsStatus, "status", "", "only show steps in this state (e.g. FAILED, COMPLETED)")

	emrInstancesCmd.Flags().IntVar(&emrInstancesLimit, "limit", 0, "maximum number of instances to fetch (0 = all)")

	emrCmd.AddCommand(emrClustersCmd, emrStepsCmd, emrInstancesCmd, emrAppsCmd, emrYarnCmd, emrHBaseCmd)
	rootCmd.AddCommand(emrCmd)
}

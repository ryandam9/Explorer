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
action-on-failure, with the failure reason inline on a failed step. Press d to
describe the cluster in full: configuration, OS, compute layout (with memory,
vCPU and EBS storage), running EC2 instances, services and VPC networking.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region and adds a Region column; otherwise the
config's aws.regions list is used.`,
	Example: `  # Browse EMR in the configured regions
  aws_explorer emr

  # Pin one region
  aws_explorer emr --region us-east-1`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
			return fmt.Errorf("initializing EMR dashboard: %w", err)
		}

		p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running EMR dashboard: %w", err)
		}
		return nil
	},
}

var (
	emrStepsLimit   int
	emrStepsStatus  string
	emrClusterState string
	emrAllStates    bool
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
		// Fetch the terminated tail when the user asks for it explicitly, either
		// via --all-states or by naming states with --state (so e.g.
		// --state TERMINATED still works); otherwise list only live clusters.
		includeTerminated := emrAllStates || emrClusterState != ""
		inv, err := client.LoadInventory(ctx, includeTerminated)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		if inv.EnrichFailures > 0 {
			fmt.Fprintf(os.Stderr, "warning: %d cluster(s) could not be enriched (DescribeCluster denied/throttled); some columns are blank\n", inv.EnrichFailures)
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

var emrDescribeCmd = &cobra.Command{
	Use:   "describe <cluster-id>",
	Short: "Describe an EMR cluster (configuration, OS, compute, storage and networking)",
	Long: `Describe one EMR cluster in full: its configuration and OS, its compute layout
(instance groups/fleets with per-instance memory, vCPU and EBS storage), its
running EC2 instances, the services installed on it, and its VPC networking —
subnet, security-group rules, route table and network ACL.

Every section is best-effort: a denied API call degrades that one section with a
note and never aborts the describe. Networking and instance-type specs use
read-only EC2 describe calls in addition to the EMR API.`,
	Args: cobra.ExactArgs(1),
	Example: `  aws_explorer emr describe j-1A2B3C4D5 -r us-east-1
  aws_explorer emr describe j-1A2B3C4D5 -o json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format := strings.ToLower(outputFormat)
		switch format {
		case "", "table", "json", "ndjson":
		default:
			return fmt.Errorf("emr describe supports table (text), json or ndjson output, not %q", outputFormat)
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)
		desc, err := client.Describe(ctx, region, args[0])
		if err != nil {
			return fmt.Errorf("failed to describe cluster %q in %s: %w", args[0], region, err)
		}
		return emrtui.RenderDescribe(os.Stdout, desc, outputFormat)
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
			return fmt.Errorf("on-cluster access not available: %w\n\nEnable it in config.yaml under emr.onCluster (mode: socks|direct|tunnel)", err)
		}
		defer dialer.Close()

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
			return fmt.Errorf("on-cluster access not available: %w\n\nEnable it in config.yaml under emr.onCluster (mode: socks|direct|tunnel)", err)
		}
		defer dialer.Close()

		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)
		dns, err := client.MasterDNS(ctx, region, args[0])
		if err != nil {
			return fmt.Errorf("failed to resolve cluster %q primary DNS in %s: %w", args[0], region, err)
		}
		// --count runs an explicit full-table row scan instead of listing.
		if emrHBaseCount != "" {
			count, capped, cerr := emrtui.CountHBaseRows(ctx, dialer, dns, emrHBaseCount)
			if cerr != nil {
				return fmt.Errorf("failed to count rows in %q: %w", emrHBaseCount, cerr)
			}
			suffix := ""
			if capped {
				suffix = "+ (capped)"
			}
			fmt.Printf("%d%s\n", count, suffix)
			return nil
		}
		tables, err := emrtui.FetchHBase(ctx, dialer, dns)
		if err != nil {
			return fmt.Errorf("failed to query HBase on cluster %q: %w", args[0], err)
		}
		return emrtui.RenderHBaseTables(os.Stdout, tables, outputFormat, noHeader)
	},
}

var emrHBaseCount string

var emrOozieCoordinators bool

var emrOozieCmd = &cobra.Command{
	Use:   "oozie <cluster-id>",
	Short: "List a cluster's Oozie workflows or coordinators (requires on-cluster access)",
	Long: `List the Oozie workflow jobs (or, with --coordinators, the coordinator jobs)
on an EMR cluster, read from the Oozie REST API on the cluster's primary node.

This needs on-cluster access (emr.onCluster in config) because Oozie has no AWS
API — it runs on the cluster's primary node, reachable only from inside the VPC
(directly, or through a SOCKS proxy such as an 'ssh -D' dynamic tunnel).`,
	Args: cobra.ExactArgs(1),
	Example: `  aws_explorer emr oozie j-1A2B3C4D5 -r us-east-1
  aws_explorer emr oozie j-1A2B3C4D5 --coordinators -o json`,
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
			return fmt.Errorf("on-cluster access not available: %w\n\nEnable it in config.yaml under emr.onCluster (mode: socks|direct|tunnel)", err)
		}
		defer dialer.Close()

		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)
		dns, err := client.MasterDNS(ctx, region, args[0])
		if err != nil {
			return fmt.Errorf("failed to resolve cluster %q primary DNS in %s: %w", args[0], region, err)
		}
		workflows, coords, err := emrtui.FetchOozie(ctx, dialer, dns)
		if err != nil {
			return fmt.Errorf("failed to query Oozie on cluster %q: %w", args[0], err)
		}
		if emrOozieCoordinators {
			return emrtui.RenderOozieCoordinators(os.Stdout, coords, outputFormat, noHeader)
		}
		return emrtui.RenderOozieWorkflows(os.Stdout, workflows, outputFormat, noHeader)
	},
}

func init() {
	emrCmd.Flags().StringVar(&emrTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(emrCmd)
	registerThemeCompletion(emrCmd, ui.ThemeNames())

	emrClustersCmd.Flags().StringVar(&emrClusterState, "state", "", "only show clusters in these states (comma-separated, e.g. RUNNING,WAITING)")
	emrClustersCmd.Flags().BoolVar(&emrAllStates, "all-states", false, "include terminated clusters (default lists only active clusters)")

	emrStepsCmd.Flags().IntVar(&emrStepsLimit, "limit", 50, "maximum number of steps to fetch")
	emrStepsCmd.Flags().StringVar(&emrStepsStatus, "status", "", "only show steps in this state (e.g. FAILED, COMPLETED)")

	emrInstancesCmd.Flags().IntVar(&emrInstancesLimit, "limit", 0, "maximum number of instances to fetch (0 = all)")

	emrHBaseCmd.Flags().StringVar(&emrHBaseCount, "count", "", "count rows in this table (full scan) instead of listing tables; takes a qualified name like ns:table")

	emrOozieCmd.Flags().BoolVar(&emrOozieCoordinators, "coordinators", false, "list coordinator jobs instead of workflows")

	emrCmd.AddCommand(emrClustersCmd, emrStepsCmd, emrInstancesCmd, emrAppsCmd, emrDescribeCmd, emrYarnCmd, emrHBaseCmd, emrOozieCmd)
	rootCmd.AddCommand(emrCmd)
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/clilog"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/emrconn"
	"github.com/ryandam9/aws_explorer/internal/emrdoctor"
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
config's aws.regions list is used.

Clusters, steps, instances, apps and describe use the AWS API and need no extra
setup. The live YARN / HBase / Oozie views read daemons that run on the cluster
itself and have no AWS API, so they need opt-in on-cluster access (an SSH tunnel
or SOCKS proxy into the VPC) — run 'aws_explorer emr hbase --help' for a full,
worked explanation, and 'aws_explorer emr connect-check <id>' to diagnose a
connection step by step when something doesn't work.`,
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

// emrOnClusterHelp returns the shared "why this needs setup, and how" section
// appended to every live (YARN/HBase/Oozie) command's help. These daemons have
// no AWS API — they answer only on the cluster's primary node, inside the VPC —
// so the text explains, for someone new to SSH tunnelling, the three ways to
// bridge in. service is the daemon's display name; port is its default REST port.
func emrOnClusterHelp(service string, port int) string {
	return fmt.Sprintf(`Why this needs extra setup:
  %[1]s has no AWS API. It answers on a REST server that runs ON the cluster's
  primary node, which sits inside the cluster's private VPC — so your laptop
  cannot reach it directly. You bridge into the VPC over SSH, then point the tool
  at that bridge. This is opt-in and OFF by default; turn it on in config.yaml
  under emr.onCluster by choosing ONE mode:

  • socks  — you run an SSH "dynamic tunnel", which opens a local SOCKS5 proxy
             (a little local port that forwards traffic into the VPC); the tool
             sends its requests through it. This is the pattern AWS documents for
             the EMR web UIs. In a separate terminal, leave this running:

                 ssh -i <key.pem> -N -D 8157 hadoop@<primary-dns>

             ( -D 8157 opens the SOCKS proxy on 127.0.0.1:8157; -N keeps it
               open with no remote shell; <primary-dns> is the cluster's
               primary-node public DNS. ) Then in config.yaml:

                 emr:
                   onCluster:
                     mode: socks
                     socksProxy: 127.0.0.1:8157

  • tunnel — the tool opens its OWN SSH connection to the primary node and dials
             the daemon through it, so there is no separate ssh command to run.
             In config.yaml:

                 emr:
                   onCluster:
                     mode: tunnel
                     ssh:
                       user: hadoop
                       keyFile: ~/.ssh/emr.pem   # unencrypted private key

  • direct — only when the tool itself already runs inside the VPC (a bastion
             host, an in-VPC CloudShell, or a peered network); plain HTTP, no SSH.

  Also required: the cluster must be running, and its security group must allow
  the %[1]s REST port (%[2]d) from where your SSH session lands. Every request is
  a read-only HTTP GET with a timeout; if the daemon can't be reached you get a
  "how to connect" helper, never a crash.`, service, port)
}

// emrTunnelExamplePreamble is the shared "open a tunnel, point config at it"
// setup shown before each live command's own invocation lines, so the examples
// are self-contained for someone setting this up for the first time.
const emrTunnelExamplePreamble = `  # 1. Find the cluster's primary-node DNS:
  aws_explorer emr describe j-1A2B3C4D5 -r us-east-1     # see "Primary node DNS"

  # 2. In a SEPARATE terminal, open an SSH dynamic tunnel (a SOCKS proxy) to it
  #    and leave it running:
  ssh -i ~/.ssh/emr.pem -N -D 8157 hadoop@<primary-dns>

  # 3. Point config.yaml at the proxy (one time):
  #      emr:
  #        onCluster:
  #          mode: socks
  #          socksProxy: 127.0.0.1:8157
`

var emrYarnCmd = &cobra.Command{
	Use:   "yarn <cluster-id>",
	Short: "List a cluster's live YARN applications (requires on-cluster access)",
	Long: `List the live YARN applications running on an EMR cluster — id, name, type,
state, progress and allocated resources — read from the ResourceManager REST API
on the cluster's primary node.

` + emrOnClusterHelp("YARN", emrconn.DefaultYARNPort),
	Args: cobra.ExactArgs(1),
	Example: emrTunnelExamplePreamble + `
  # 4. List the live YARN applications:
  aws_explorer emr yarn j-1A2B3C4D5 -r us-east-1
  aws_explorer emr yarn j-1A2B3C4D5 -r us-east-1 -o json`,
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

var emrHDFSCmd = &cobra.Command{
	Use:   "hdfs <cluster-id>",
	Short: "Show a cluster's HDFS / NameNode status (requires on-cluster access)",
	Long: `Show the HDFS NameNode's view of the filesystem — capacity used/free/total,
live vs dead DataNodes, file and block totals, missing / under-replicated /
corrupt blocks, safe-mode state, and the per-DataNode breakdown — read from the
NameNode's JMX endpoint on the cluster's primary node.

` + emrOnClusterHelp("HDFS NameNode", emrconn.DefaultNameNodePort),
	Args: cobra.ExactArgs(1),
	Example: emrTunnelExamplePreamble + `
  # 4. Show HDFS / NameNode status:
  aws_explorer emr hdfs j-1A2B3C4D5 -r us-east-1
  aws_explorer emr hdfs j-1A2B3C4D5 -r us-east-1 -o json`,
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
		status, err := emrtui.FetchHDFS(ctx, dialer, dns)
		if err != nil {
			return fmt.Errorf("failed to query HDFS on cluster %q: %w", args[0], err)
		}
		return emrtui.RenderHDFS(os.Stdout, status, outputFormat, noHeader)
	},
}

var emrHBaseCmd = &cobra.Command{
	Use:   "hbase <cluster-id>",
	Short: "List a cluster's HBase tables (requires on-cluster access)",
	Long: `List the HBase tables on an EMR cluster — namespace, derived state (ENABLED /
DISABLED / PARTIAL, inferred from how many of a table's regions are assigned),
region counts, online regions and column families — read from the HBase REST
server on the cluster's primary node.

With --count <ns:table> it instead runs an exact, read-only full-table row scan
(bounded at 5M rows and confirmation-gated) and prints just the row count.

` + emrOnClusterHelp("HBase", emrconn.DefaultHBasePort),
	Args: cobra.ExactArgs(1),
	Example: emrTunnelExamplePreamble + `
  # 4. List the HBase tables:
  aws_explorer emr hbase j-1A2B3C4D5 -r us-east-1
  aws_explorer emr hbase j-1A2B3C4D5 -r us-east-1 -o json

  # Count rows in one table — exact full scan, prompts before it runs:
  aws_explorer emr hbase j-1A2B3C4D5 --count default:events -r us-east-1

  # Already inside the VPC (bastion / in-VPC CloudShell)? Skip the tunnel and set
  # mode: direct in config.yaml, then just:
  aws_explorer emr hbase j-1A2B3C4D5 -r us-east-1`,
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

` + emrOnClusterHelp("Oozie", emrconn.DefaultOoziePort),
	Args: cobra.ExactArgs(1),
	Example: emrTunnelExamplePreamble + `
  # 4. List Oozie workflows (or coordinators with --coordinators):
  aws_explorer emr oozie j-1A2B3C4D5 -r us-east-1
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

var (
	emrConfigClassification string
	emrConfigEffective      bool
)

var emrConfigCmd = &cobra.Command{
	Use:   "config <cluster-id>",
	Short: "Browse an EMR cluster's configuration files (core-site, hdfs-site, spark-defaults, …)",
	Long: `Show the cluster's setup as the on-disk configuration files it becomes —
core-site.xml, hdfs-site.xml, yarn-site.xml, spark-defaults.conf, hive-site.xml,
emrfs-site.xml and so on — with every property key/value.

By default this reads the configuration classifications EMR returns from
DescribeCluster (the *declared* setup); it needs no on-cluster access.

With --effective it instead reads the NameNode's /conf endpoint — the *merged*
configuration the cluster is actually running (every property after all site
files and defaults are applied, tagged with the source file it came from). That
needs on-cluster access (emr.onCluster); see 'aws_explorer emr hbase --help'.

Use --classification to scope to one file. For the interactive browser, press
'c' on a cluster in 'aws_explorer emr' ('e' there toggles declared/effective).`,
	Args: cobra.ExactArgs(1),
	Example: `  aws_explorer emr config j-1A2B3C4D5 -r us-east-1
  aws_explorer emr config j-1A2B3C4D5 --classification hdfs-site
  aws_explorer emr config j-1A2B3C4D5 --effective          # live merged config (on-cluster)
  aws_explorer emr config j-1A2B3C4D5 -o json | jq '.[] | select(.classification=="core-site")'`,
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

		rows, err := emrConfigRows(ctx, client, region, args[0])
		if err != nil {
			return err
		}
		if emrConfigClassification != "" {
			rows = emrtui.FilterConfigRows(rows, emrConfigClassification)
		}
		return emrtui.RenderConfig(os.Stdout, rows, outputFormat, noHeader)
	},
}

// emrConfigRows resolves the config rows for the config command: the declared
// classifications (EMR API) by default, or the effective merged config from the
// NameNode's /conf (on-cluster) under --effective.
func emrConfigRows(ctx context.Context, client *emrtui.Client, region, clusterID string) ([]emrtui.ConfigRow, error) {
	if !emrConfigEffective {
		cfgs, err := client.Configurations(ctx, region, clusterID)
		if err != nil {
			return nil, fmt.Errorf("failed to get configuration for cluster %q in %s: %w", clusterID, region, err)
		}
		return emrtui.FlattenConfigRows(cfgs), nil
	}

	var onCluster config.OnClusterConfig
	if AppConfig != nil {
		onCluster = AppConfig.EMR.OnCluster
	}
	dialer, err := emrconn.New(onCluster)
	if err != nil {
		return nil, fmt.Errorf("on-cluster access not available: %w\n\nEnable it in config.yaml under emr.onCluster (mode: socks|direct|tunnel)", err)
	}
	defer dialer.Close()
	dns, err := client.MasterDNS(ctx, region, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve cluster %q primary DNS in %s: %w", clusterID, region, err)
	}
	rows, err := emrtui.FetchEffectiveConfig(ctx, dialer, dns)
	if err != nil {
		return nil, fmt.Errorf("failed to read effective config from cluster %q: %w", clusterID, err)
	}
	return rows, nil
}

var emrConnCheckService string

var emrConnCheckCmd = &cobra.Command{
	Use:   "connect-check <cluster-id>",
	Short: "Diagnose on-cluster access step by step (HDFS/YARN/HBase/Oozie/Hive)",
	Long: `Verify, one layer at a time, why a live HDFS / YARN / HBase / Oozie / Hive
connection does or doesn't work — and tell you exactly what to fix. It walks the same path a
real connection uses and prints a pass/fail line with a concrete next step for
each layer:

  1. config   — is emr.onCluster configured, and does the dialer build?
  2. cluster  — does DescribeCluster work, is the cluster running, is the
                primary-node DNS resolved?
  3. bridge   — the link into the VPC: is the SOCKS proxy listening (socks), can
                the tool SSH to the primary node (tunnel), or is it in-VPC
                (direct)? Authentication vs network failures are distinguished.
  4. service  — for each daemon: is its port reachable through the bridge, and
                does it answer its health endpoint?

A failure short-circuits the layers that depend on it (shown as "skipped"), so
the report never implies it verified something it couldn't reach. Every probe is
read-only and bounded by emr.onCluster.timeoutSeconds.

Note on Hive: HiveServer2 speaks Thrift (port 10000), not HTTP, so it gets a
TCP port-reachability check only — the port can be confirmed open, but not that
the service is healthy (use beeline for a full check). YARN, HBase and Oozie
expose REST daemons and get a true protocol-level health check.

Configure on-cluster access first — run 'aws_explorer emr hbase --help' for a
full, worked setup of the socks / tunnel / direct modes.`,
	Args: cobra.ExactArgs(1),
	Example: `  # Check every service on a cluster:
  aws_explorer emr connect-check j-1A2B3C4D5 -r us-east-1

  # Just HBase (or a comma list):
  aws_explorer emr connect-check j-1A2B3C4D5 --service hbase
  aws_explorer emr connect-check j-1A2B3C4D5 --service hbase,oozie`,
	RunE: func(cmd *cobra.Command, args []string) error {
		services, err := emrdoctor.ParseServices(emrConnCheckService)
		if err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()

		var onCluster config.OnClusterConfig
		if AppConfig != nil {
			onCluster = AppConfig.EMR.OnCluster
		}

		client, err := newEMRClient(ctx)
		if err != nil {
			return err
		}
		region := emrRegionForCommand(client)

		// One DescribeCluster up front; a failure is fed into the report (the
		// cluster check reports it) rather than aborting — the config layer is
		// still worth verifying.
		dns, state, derr := client.ClusterConn(ctx, region, args[0])
		report := emrdoctor.Run(ctx, onCluster, emrdoctor.ClusterInfo{
			State: state, PrimaryDNS: dns, DescribeErr: derr,
		}, services)

		fmt.Printf("EMR connect-check — %s [%s]\n\n", args[0], region)
		color := clilog.ColorEnabled(isatty.IsTerminal(os.Stdout.Fd()))
		emrdoctor.Render(os.Stdout, report, color)
		if report.Failed() {
			// Non-zero exit so scripts/CI can gate on a clean connection, but
			// suppress Cobra's usage dump — the report already explains the failure.
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			return fmt.Errorf("connect-check found %s", failSummary(report))
		}
		return nil
	},
}

// failSummary is the terse reason attached to connect-check's non-zero exit.
func failSummary(r *emrdoctor.Report) string {
	_, fail, _, _ := r.Counts()
	if fail == 1 {
		return "1 failed check"
	}
	return fmt.Sprintf("%d failed checks", fail)
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

	emrConnCheckCmd.Flags().StringVar(&emrConnCheckService, "service", "all", "which services to check: all, or a comma list of hdfs,hbase,yarn,oozie,hive")

	emrConfigCmd.Flags().StringVar(&emrConfigClassification, "classification", "", "show only this classification/file (e.g. hdfs-site, core-site)")
	emrConfigCmd.Flags().BoolVar(&emrConfigEffective, "effective", false, "read the live merged config from the NameNode /conf (on-cluster) instead of the declared classifications")

	emrCmd.AddCommand(emrClustersCmd, emrStepsCmd, emrInstancesCmd, emrAppsCmd, emrDescribeCmd, emrYarnCmd, emrHDFSCmd, emrHBaseCmd, emrOozieCmd, emrConnCheckCmd, emrConfigCmd)
	rootCmd.AddCommand(emrCmd)
}

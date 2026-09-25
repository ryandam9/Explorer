package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/downloads"
	"github.com/ryandam9/aws_explorer/internal/lambdatui"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var lambdaTheme string

var lambdaCmd = &cobra.Command{
	Use:   "lambda",
	Short: "Start the AWS Lambda dashboard TUI",
	Long: `Start an interactive dashboard for AWS Lambda: functions (with their runtime,
memory, timeout and state), layers (latest version and compatible runtimes) and
event-source mappings (source, state and batch size). Press Enter on a function
to drill into its full configuration — role, layers, VPC, dead-letter queue,
reserved concurrency, environment-variable keys (values never shown), code
location and tags, fetched on demand. Press f for the findings panel:
deterministic runtime/health checks (deprecated runtimes, missing dead-letter
queues, failed-state functions). On a function, L opens its CloudWatch logs.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region and adds a Region column; otherwise the
config's aws.regions list is used.`,
	Example: `  # Browse Lambda in the configured regions
  aws_explorer lambda

  # Pin one region
  aws_explorer lambda --region us-east-1`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		lambdaCfg := tuiAWSConfig()

		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, lambdaTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		SilenceScanLogs()

		regions, scanAll := lambdaRegionScope()

		model, err := lambdatui.NewModel(ctx, lambdaCfg, regions, scanAll, AppConfig, configFilePath())
		if err != nil {
			return fmt.Errorf("initializing Lambda dashboard: %w", err)
		}

		p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running Lambda dashboard: %w", err)
		}
		return nil
	},
}

// lambdaRegionScope resolves the region list and all-regions flag the same way
// the dashboard does, so the CLI twins honour --region / --all-regions / config.
func lambdaRegionScope() ([]string, bool) {
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

// newLambdaClient builds the shared Lambda client for the CLI twins.
func newLambdaClient(ctx context.Context) (*lambdatui.Client, error) {
	regions, scanAll := lambdaRegionScope()
	return lambdatui.NewClient(ctx, tuiAWSConfig(), regions, scanAll)
}

var lambdaFunctionsCmd = &cobra.Command{
	Use:     "functions",
	Short:   "List Lambda functions with their runtime, memory, timeout and state",
	Example: "  aws_explorer lambda functions --all-regions -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newLambdaClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return lambdatui.RenderFunctions(os.Stdout, inv.Functions, outputFormat, noHeader)
	},
}

var lambdaLayersCmd = &cobra.Command{
	Use:   "layers",
	Short: "List Lambda layers with their latest version and compatible runtimes",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newLambdaClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return lambdatui.RenderLayers(os.Stdout, inv.Layers, outputFormat, noHeader)
	},
}

var lambdaEventSourcesCmd = &cobra.Command{
	Use:     "event-sources",
	Short:   "List Lambda event-source mappings (source, state, batch size)",
	Aliases: []string{"event-source-mappings", "esm"},
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newLambdaClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return lambdatui.RenderEventSources(os.Stdout, inv.EventSources, outputFormat, noHeader)
	},
}

var (
	lambdaActivityDate     string
	lambdaActivityPattern  string
	lambdaActivityFilter   string
	lambdaActivityLimit    int
	lambdaActivityUTC      bool
	lambdaActivityLogGroup string
	lambdaActivityAll      bool
	lambdaActivityOut      string
)

var lambdaActivityCmd = &cobra.Command{
	Use:         "activity <function>",
	Annotations: map[string]string{extraFormatsAnnotation: "xlsx"},
	Short:       "Count a function's invocations on a day and grep that day's logs with a regex",
	Long: `Answer "what did this function do on this day?".

Invocations: the day's AWS/Lambda Invocations, Errors and Throttles (Sum per
hour) from CloudWatch metrics — one batched GetMetricData call. The count is
every invocation, including retries of failed async (e.g. S3-triggered) events.

Log scan (with --pattern, or --all-events to list every event): reads the
day's events from the function's log group
(its LoggingConfig group, else /aws/lambda/<name>) and keeps those matching the
Go regular expression, pulling the request ID and level out of each line and
each capture group into its own column. It also counts START lines — the
invocations the logs saw — as a cross-check on the metric. The scan is bounded
(--limit matches, and a page budget); when a bound ends it early the output says
so. --filter adds a server-side CloudWatch Logs filter pattern to read less
data (START lines and the REPORT-based performance figures are then not
counted). The scan also summarises the REPORT lines — p50/p95/max duration
against the timeout, memory used, cold starts and an estimated compute cost —
and counts invocations that failed (an ERROR line, a runtime exit or a timeout).

The window is one or more calendar days in your local time zone (--utc for
UTC): --date takes YYYY-MM-DD, today (the default), yesterday, a range A..B
(inclusive, up to 31 days) or Nd for the last N days including today.

Output: table prints the summary, the hourly (or, for a range, daily) breakdown
and the matches; json the whole report; ndjson and csv one row per match (per
hour without a scan); xlsx (this command only) writes an Excel workbook of the
matches — to the downloads directory, or --out FILE — and prints its path.`,
	Example: `  # How many times did it run yesterday?
  aws_explorer lambda activity copy-object --date yesterday

  # Which objects did it copy on the 24th? (capture groups become columns)
  aws_explorer lambda activity copy-object --date 2026-09-24 \
      --pattern 'Copied s3://(?P<src>\S+) to s3://(?P<dst>\S+)'

  # Errors only, case-insensitive, as CSV
  aws_explorer lambda activity copy-object -p '(?i)error|exception' -o csv

  # Every log event of the last 7 days, as an Excel workbook
  aws_explorer lambda activity copy-object --date 7d --all-events -o xlsx`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		xlsx := strings.EqualFold(outputFormat, "xlsx")
		if !xlsx {
			if err := output.ValidateFormat(outputFormat); err != nil {
				return err
			}
		} else if lambdaActivityPattern == "" && !lambdaActivityAll {
			return fmt.Errorf("-o xlsx exports the log rows: add --pattern REGEX or --all-events")
		}
		if lambdaActivityOut != "" && !xlsx {
			return fmt.Errorf("--out is for -o xlsx; other formats write to stdout")
		}
		loc := time.Local
		if lambdaActivityUTC {
			loc = time.UTC
		}
		now := time.Now()
		day, err := lambdatui.ParseDay(lambdaActivityDate, now, loc)
		if err != nil {
			return err
		}
		var re *regexp.Regexp
		switch {
		case lambdaActivityAll && lambdaActivityPattern != "":
			return fmt.Errorf("--all-events lists every event; drop --pattern (or drop --all-events to search)")
		case lambdaActivityAll:
			re = lambdatui.MatchAll()
		case lambdaActivityPattern != "":
			if re, err = regexp.Compile(lambdaActivityPattern); err != nil {
				return fmt.Errorf("invalid --pattern: %w", err)
			}
		case lambdaActivityFilter != "":
			return fmt.Errorf("--filter narrows the log scan, which needs --pattern or --all-events")
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		SilenceScanLogs()
		client, err := newLambdaClient(ctx)
		if err != nil {
			return err
		}
		fn, warning, err := client.ResolveFunction(ctx, args[0])
		if err != nil {
			return err
		}
		if warning != "" {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
		}
		if lambdaActivityLogGroup != "" {
			fn.LogGroup = lambdaActivityLogGroup
		}

		q := lambdatui.NewActivityQuery(fn, day, re, lambdaActivityFilter, lambdaActivityLimit)
		report := lambdatui.ActivityReport{Query: q, Now: now}
		report.Stats, report.StatsErr = client.InvocationStats(ctx, fn.Region, fn.Name, day)
		if report.StatsErr != nil {
			fmt.Fprintf(os.Stderr, "warning: invocation metrics unavailable: %v\n", report.StatsErr)
		}
		if re != nil {
			var progress func(*lambdatui.LogScan)
			if isatty.IsTerminal(os.Stderr.Fd()) {
				progress = func(s *lambdatui.LogScan) {
					fmt.Fprintf(os.Stderr, "\rscanning %s … %d events, %d matches", fn.LogGroup, s.Events, len(s.Matches))
				}
			}
			report.Scan = client.ScanLogs(ctx, q, progress)
			if progress != nil {
				fmt.Fprint(os.Stderr, "\r\033[K")
			}
			if note := report.Scan.ScanNote(); note != "" {
				fmt.Fprintf(os.Stderr, "warning: %s\n", note)
			}
		}
		if xlsx {
			path := lambdaActivityOut
			if path == "" {
				dir, err := downloads.Dir()
				if err != nil {
					return err
				}
				path = filepath.Join(dir, lambdatui.ActivityWorkbookName(q, now))
			}
			if err := lambdatui.WriteActivityWorkbook(path, q, report.Stats, report.StatsErr, report.Scan, now); err != nil {
				return fmt.Errorf("writing %s: %w", path, err)
			}
			fmt.Fprintln(os.Stdout, path)
		} else if err := lambdatui.RenderActivity(os.Stdout, report, outputFormat, noHeader); err != nil {
			return err
		}
		if report.StatsErr != nil && (report.Scan == nil || report.Scan.Err != nil) {
			return fmt.Errorf("no activity data could be read for %s", fn.Name)
		}
		return nil
	},
}

func init() {
	lambdaActivityCmd.Flags().StringVar(&lambdaActivityDate, "date", "today", "day or days to report: YYYY-MM-DD, today, yesterday, A..B (≤31 days) or Nd for the last N days (local time; see --utc)")
	lambdaActivityCmd.Flags().StringVarP(&lambdaActivityPattern, "pattern", "p", "", "Go regular expression to scan the day's log events for; capture groups become columns")
	lambdaActivityCmd.Flags().StringVar(&lambdaActivityFilter, "filter", "", "server-side CloudWatch Logs filter pattern applied before --pattern (reads less data)")
	lambdaActivityCmd.Flags().IntVar(&lambdaActivityLimit, "limit", lambdatui.DefaultMaxMatches, "stop the log scan after this many matches")
	lambdaActivityCmd.Flags().BoolVar(&lambdaActivityUTC, "utc", false, "interpret --date as a UTC day and print times in UTC")
	lambdaActivityCmd.Flags().StringVar(&lambdaActivityLogGroup, "log-group", "", "log group to scan (default: the function's configured group, else /aws/lambda/<name>)")
	lambdaActivityCmd.Flags().BoolVar(&lambdaActivityAll, "all-events", false, "list every log event of the window (after --filter, when given) instead of searching with --pattern")
	lambdaActivityCmd.Flags().StringVar(&lambdaActivityOut, "out", "", "with -o xlsx: the workbook's path (default: the downloads directory, named after the function and window)")
}

func init() {
	lambdaCmd.Flags().StringVar(&lambdaTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(lambdaCmd)
	registerThemeCompletion(lambdaCmd, ui.ThemeNames())

	lambdaCmd.AddCommand(lambdaFunctionsCmd, lambdaLayersCmd, lambdaEventSourcesCmd, lambdaActivityCmd)
	rootCmd.AddCommand(lambdaCmd)
}

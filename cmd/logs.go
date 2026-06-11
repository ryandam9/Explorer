package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/user/aws_explorer/internal/auth"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/logs"
	"github.com/user/aws_explorer/internal/logstui"
	"github.com/user/aws_explorer/internal/ui"
)

var (
	logsGroup   string
	logsSince   time.Duration
	logsPattern string
	logsLimit   int
	logsRegion  string
	logsTUI     bool
	logsTheme   string
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Browse CloudWatch log groups and fetch log events",
	Long: `Without --group, lists the CloudWatch log groups in the region.
With --group, fetches log events from that group across all its streams
(oldest first) using FilterLogEvents, bounded by --since and an optional
--filter pattern.

Pass --tui to browse groups and events interactively instead.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		awscfg, region, err := buildLogsAWSConfig(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		client := cloudwatchlogs.NewFromConfig(awscfg)

		if logsTUI {
			activeTheme := logsTheme
			if AppConfig != nil && AppConfig.UI.Theme != "" && logsTheme == "spotted-pardalote" {
				activeTheme = AppConfig.UI.Theme
			}
			ui.InitFromConfig(AppConfig.UI)
			// The TUI owns the screen; keep scan logs from corrupting it.
			SilenceLogsForTUI()

			m := logstui.NewModel(ctx, client, region, activeTheme, logsGroup, logsSince, logsPattern)
			p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running logs TUI: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if logsGroup == "" {
			runListGroups(ctx, client, region)
			return
		}
		runFetchEvents(ctx, client)
	},
}

// buildLogsAWSConfig resolves credentials and the target region for the logs
// command: --region wins, then the first configured region, then whatever
// region the SDK chain resolves, then us-east-1.
func buildLogsAWSConfig(ctx context.Context) (cfg aws.Config, region string, err error) {
	awsCfg := &config.AWSConfig{}
	if AppConfig != nil {
		*awsCfg = AppConfig.AWS
	}
	if awsProfile != "" {
		awsCfg.Profile = awsProfile
	}
	if awsAuthMethod != "" {
		awsCfg.AuthMethod = awsAuthMethod
	}
	if awsRoleARN != "" {
		awsCfg.STS.RoleARN = awsRoleARN
		if awsCfg.AuthMethod == "" || awsCfg.AuthMethod == "auto" {
			awsCfg.AuthMethod = "sts"
		}
	}

	region = logsRegion
	if region == "" && len(awsCfg.Regions) > 0 && !strings.EqualFold(awsCfg.Regions[0], "all") {
		region = awsCfg.Regions[0]
	}

	cfg, err = auth.BuildAWSConfig(ctx, awsCfg, region)
	if err != nil {
		return cfg, "", fmt.Errorf("unable to load AWS config: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return cfg, cfg.Region, nil
}

func runListGroups(ctx context.Context, client *cloudwatchlogs.Client, region string) {
	groups, err := logs.ListGroups(ctx, client, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing log groups: %v\n", err)
		if len(groups) == 0 {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Showing the groups listed before the failure.")
	}
	if len(groups) == 0 {
		fmt.Println("No log groups found.")
		return
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })

	if strings.EqualFold(outputFormat, "json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(groups); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to marshal JSON: %v\n", err)
			os.Exit(1)
		}
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tRETENTION\tSTORED\tCREATED")
	for _, g := range groups {
		created := "-"
		if !g.CreatedAt.IsZero() {
			created = g.CreatedAt.Format("2006-01-02")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			g.Name, logs.FormatRetention(g.RetentionDays), logs.FormatBytes(g.StoredBytes), created)
	}
	w.Flush()
}

func runFetchEvents(ctx context.Context, client *cloudwatchlogs.Client) {
	start := time.Now().Add(-logsSince)
	in := logs.FetchInput{
		Group:   logsGroup,
		Start:   start,
		Pattern: logsPattern,
	}

	jsonOut := strings.EqualFold(outputFormat, "json")
	var w *tabwriter.Writer
	var enc *json.Encoder
	if jsonOut {
		enc = json.NewEncoder(os.Stdout)
	} else {
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TIMESTAMP\tSTREAM\tMESSAGE")
	}

	// FilterLogEvents is throttled at ~5 TPS account-wide, so pages are
	// fetched sequentially and printed as they arrive.
	total := 0
	for {
		page, err := logs.FetchEvents(ctx, client, in)
		for _, e := range page.Events {
			if logsLimit > 0 && total >= logsLimit {
				break
			}
			total++
			if jsonOut {
				_ = enc.Encode(e)
			} else {
				fmt.Fprintf(w, "%s\t%s\t%s\n",
					e.Timestamp.Format("2006-01-02 15:04:05"), e.Stream,
					strings.TrimRight(e.Message, "\n"))
			}
		}
		if w != nil {
			w.Flush()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching events: %v\n", err)
			if total == 0 {
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Showing the %d events fetched before the failure.\n", total)
			return
		}
		if page.NextToken == nil || (logsLimit > 0 && total >= logsLimit) {
			break
		}
		in.NextToken = page.NextToken
	}

	if total == 0 {
		fmt.Println("No events found in the requested window.")
	} else if !jsonOut {
		fmt.Fprintf(os.Stderr, "\n%d events from %s (last %s)\n", total, logsGroup, logsSince)
	}
}

func init() {
	logsCmd.Flags().StringVarP(&logsGroup, "group", "g", "", "Log group to fetch events from (omit to list groups)")
	logsCmd.Flags().DurationVar(&logsSince, "since", 15*time.Minute, "How far back to fetch events (e.g. 15m, 2h, 48h)")
	logsCmd.Flags().StringVarP(&logsPattern, "filter", "f", "", "CloudWatch Logs filter pattern (e.g. \"ERROR\", \"{ $.level = \\\"error\\\" }\")")
	logsCmd.Flags().IntVar(&logsLimit, "limit", 1000, "Maximum events to fetch (0 = no limit)")
	logsCmd.Flags().StringVarP(&logsRegion, "region", "r", "", "AWS region (defaults to the configured/profile region)")
	logsCmd.Flags().BoolVar(&logsTUI, "tui", false, "Browse log groups and events interactively")
	logsCmd.Flags().StringVar(&logsTheme, "theme", "spotted-pardalote", "Color theme for --tui ("+strings.Join(ui.ThemeNames(), ", ")+")")
	rootCmd.AddCommand(logsCmd)
}

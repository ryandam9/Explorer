package cmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/laketui"
	"github.com/ryandam9/aws_explorer/internal/traillake"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	lakeStore         string
	lakeListStores    bool
	lakeSQL           string
	lakeTopPrincipals bool
	lakeTopEvents     bool
	lakeBy            string
	lakeEvent         string
	lakeSource        string
	lakeErrorsOnly    bool
	lakeSince         string
	lakeLimit         int
	lakeMaxWait       string
	lakeTUI           bool
)

var lakeCmd = &cobra.Command{
	Use:   "lake",
	Short: "Query CloudTrail Lake — years of history, data events, aggregation (SQL)",
	Long: `Lake queries a CloudTrail Lake event data store with SQL. Unlike the trail
command (cloudtrail:LookupEvents — 90 days, management events only), a Lake
event data store can hold years of history and data events (S3 object access,
Lambda invokes, …) and supports aggregation — but it must be created first.

Pick a store with --store (the only store is used automatically; --list-stores
prints what is available). Then either run a built-in query or your own SQL:

  • (default)         recent activity, newest first,
  • --top-principals  who is most active (count per principal),
  • --top-events      which API calls are most frequent,
  • --sql "<query>"   any CloudTrail Lake SQL (you supply the FROM clause).

The --by / --event / --source / --errors-only / --since / --limit filters shape
the built-in queries. Add --tui to explore the results interactively.

If no event data store exists, this prints a short note and exits cleanly — use
the trail command for the zero-setup 90-day feed.`,
	Example: `  # What stores can I query?
  aws_explorer lake --list-stores

  # Recent activity in the last 30 days
  aws_explorer lake --since 30d

  # Who has been the busiest principal this quarter?
  aws_explorer lake --top-principals --since 90d

  # Most frequent API calls, interactively
  aws_explorer lake --top-events --tui

  # Your own SQL
  aws_explorer lake --sql "SELECT eventName, COUNT(*) c FROM <eds-id> GROUP BY eventName ORDER BY c DESC LIMIT 20"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		applyGlobalAWSOverrides()
		ui.InitFromConfig(AppConfig.UI) // theme the status messages
		ctx := context.Background()

		region := "us-east-1"
		if awsRegion != "" {
			region = awsRegion
		} else if len(AppConfig.AWS.Regions) > 0 {
			region = AppConfig.AWS.Regions[0]
		}
		awscfg, err := auth.BuildAWSConfig(ctx, &AppConfig.AWS, region)
		if err != nil {
			if hint, ok := awserr.LoginHint(err, AppConfig.AWS.Profile); ok {
				return errors.New(hint)
			}
			return fmt.Errorf("unable to load AWS config: %w", err)
		}

		stores, err := traillake.ListDataStores(ctx, awscfg)
		if err != nil {
			if awserr.IsAuthError(err) {
				return fmt.Errorf("not authorized — grant cloudtrail:ListEventDataStores (and StartQuery/GetQueryResults to query)")
			}
			return fmt.Errorf("listing CloudTrail Lake event data stores failed: %w", err)
		}
		if len(stores) == 0 {
			fmt.Println(warnStyle().Render(fmt.Sprintf("No CloudTrail Lake event data store found in %s.", region)))
			fmt.Println(ui.MutedStyle().Render(
				"Create one (aws cloudtrail create-event-data-store …), or use `aws_explorer trail` " +
					"for the zero-setup 90-day LookupEvents feed."))
			return nil
		}
		if lakeListStores {
			return renderStores(os.Stdout, stores, outputFormat)
		}

		store, err := pickStore(stores, lakeStore)
		if err != nil {
			return err
		}

		since, err := parseSince(lakeSince)
		if err != nil {
			return err
		}
		maxWait, err := parseMaxWait(lakeMaxWait)
		if err != nil {
			return err
		}

		preset := traillake.Preset{
			Since: since, Limit: lakeLimit,
			Principal: lakeBy, EventName: lakeEvent, EventSource: lakeSource,
			ErrorsOnly: lakeErrorsOnly,
		}
		sql, title := buildLakeQuery(store.ID, preset)
		opts := traillake.QueryOptions{MaxWait: maxWait, MaxRows: lakeLimit}

		if lakeTUI {
			ui.InitFromConfig(AppConfig.UI)
			SilenceScanLogs()
			m := laketui.New(ctx, awscfg, sql, opts, title, store.Name, region)
			p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("error running lake TUI: %w", err)
			}
			return nil
		}

		fmt.Fprintln(os.Stderr, ui.InfoStyle().Render(
			fmt.Sprintf("Running CloudTrail Lake query (%s) on %s in %s…", title, store.Name, region)))
		res, err := traillake.RunQuery(ctx, awscfg, sql, opts)
		if err != nil {
			if awserr.IsAuthError(err) {
				return fmt.Errorf("not authorized — grant cloudtrail:StartQuery and cloudtrail:GetQueryResults")
			}
			return fmt.Errorf("CloudTrail Lake query failed: %w", err)
		}
		if len(res.Rows) == 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Println(warnStyle().Render("The query returned no rows."))
			return nil
		}
		return renderLakeResult(os.Stdout, res, outputFormat, noHeader)
	},
}

// buildLakeQuery selects the SQL and a human-readable title from the flags. A
// raw --sql wins; otherwise the preset selectors pick a built-in query.
func buildLakeQuery(edsID string, p traillake.Preset) (sql, title string) {
	switch {
	case lakeSQL != "":
		return lakeSQL, "custom query"
	case lakeTopPrincipals:
		return traillake.TopPrincipalsSQL(edsID, p), "top principals"
	case lakeTopEvents:
		return traillake.TopEventsSQL(edsID, p), "top events"
	default:
		return traillake.RecentSQL(edsID, p), "recent activity"
	}
}

// pickStore resolves the store to query. With no selector it uses the only
// store (and errors if there are several); otherwise it matches by ID, ARN, or
// name (case-insensitive).
func pickStore(stores []traillake.DataStore, want string) (traillake.DataStore, error) {
	if want == "" {
		if len(stores) == 1 {
			return stores[0], nil
		}
		return traillake.DataStore{}, fmt.Errorf("%d event data stores found — pass --store <id|arn|name> (see --list-stores)", len(stores))
	}
	w := strings.ToLower(want)
	for _, s := range stores {
		if strings.ToLower(s.ID) == w || strings.ToLower(s.ARN) == w || strings.ToLower(s.Name) == w {
			return s, nil
		}
	}
	return traillake.DataStore{}, fmt.Errorf("no event data store matches %q (see --list-stores)", want)
}

func parseMaxWait(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid --max-wait %q (use a duration like 90s or 2m)", s)
	}
	return d, nil
}

func renderStores(w io.Writer, stores []traillake.DataStore, format string) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(stores)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tID\tARN")
		for _, s := range stores {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.ID, s.ARN)
		}
		return tw.Flush()
	}
}

func renderLakeResult(w io.Writer, res traillake.Result, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		objs := make([]map[string]string, 0, len(res.Rows))
		for _, row := range res.Rows {
			obj := make(map[string]string, len(res.Columns))
			for i, c := range res.Columns {
				if i < len(row) {
					obj[c] = row[i]
				}
			}
			objs = append(objs, obj)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(objs)
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			if err := cw.Write(res.Columns); err != nil {
				return err
			}
		}
		for _, row := range res.Rows {
			if err := cw.Write(row); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, strings.Join(res.Columns, "\t"))
		}
		for _, row := range res.Rows {
			fmt.Fprintln(tw, strings.Join(row, "\t"))
		}
		return tw.Flush()
	}
}

func init() {
	lakeCmd.Flags().BoolVar(&lakeListStores, "list-stores", false, "list available event data stores and exit")
	lakeCmd.Flags().StringVar(&lakeStore, "store", "", "event data store to query (id, ARN, or name; default: the only store)")
	lakeCmd.Flags().StringVar(&lakeSQL, "sql", "", "raw CloudTrail Lake SQL (you supply the FROM clause)")
	lakeCmd.Flags().BoolVar(&lakeTopPrincipals, "top-principals", false, "built-in query: principals ranked by event count")
	lakeCmd.Flags().BoolVar(&lakeTopEvents, "top-events", false, "built-in query: API calls ranked by frequency")
	lakeCmd.Flags().StringVar(&lakeBy, "by", "", "filter built-in queries to a principal (substring of the ARN)")
	lakeCmd.Flags().StringVar(&lakeEvent, "event", "", "filter built-in queries to one API call")
	lakeCmd.Flags().StringVar(&lakeSource, "source", "", "filter built-in queries to one service (e.g. s3.amazonaws.com)")
	lakeCmd.Flags().BoolVar(&lakeErrorsOnly, "errors-only", false, "filter built-in queries to failed/denied calls")
	lakeCmd.Flags().StringVar(&lakeSince, "since", "", "only events after this long ago (e.g. 30d, 12h)")
	lakeCmd.Flags().IntVar(&lakeLimit, "limit", 50, "maximum number of rows to return")
	lakeCmd.Flags().StringVar(&lakeMaxWait, "max-wait", "60s", "how long to wait for the query to finish")
	lakeCmd.Flags().BoolVar(&lakeTUI, "tui", false, "explore the results interactively")
	rootCmd.AddCommand(lakeCmd)
}

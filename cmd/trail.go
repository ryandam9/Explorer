package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/trail"
	"github.com/ryandam9/aws_explorer/internal/trailtui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	trailSince       string
	trailLimit       int
	trailIncludeRead bool
	trailBy          string
	trailEvent       string
	trailSource      string
	trailErrorsOnly  bool
	trailTUI         bool
)

var trailCmd = &cobra.Command{
	Use:   "trail [resource-id-or-arn]",
	Short: `CloudTrail activity feed — who did what, and who changed this`,
	Long: `Trail lists recent CloudTrail management events: when, which API call, which
principal, from which source IP, and whether the call failed. It answers both
"who changed this resource" and "what has been happening in this account".

It uses cloudtrail:LookupEvents, which covers the last 90 days of management
events with no trail or S3 bucket setup required. Events are newest first.

Scope (at most one — LookupEvents accepts a single filter):
  • a resource (bare ID like i-0abc…, sg-0abc…, a name, or a full ARN — ARNs
    are reduced to the resource name CloudTrail records),
  • --by <principal>   every event by an IAM user / role session name,
  • --event <name>     every call of one API (e.g. TerminateInstances),
  • --source <service> every event from one service (e.g. ec2.amazonaws.com),
  • nothing            the account-wide activity feed.

By default only mutating events are shown. For the account-wide feed this is a
server-side filter (CloudTrail ReadOnly=false), so the scan reaches real
changes instead of paging through Describe*/List*/Get* noise; --read-events
drops the filter and includes the reads. --errors-only keeps just failed/denied
calls (a burst of these is a recon or misconfiguration signal).

The --tui feed streams events in per region (it doesn't wait for the slowest
region) and keeps the newest trail.maxEvents (default 200) — raise it with
--limit.

To suppress specific events on top of the read-only filter (e.g. noisy
mutations like AssumeRole or ConsoleLogin), list them under trail.hideEvents in
the config file; they are dropped server-side so they never eat into the cap.
Matching is case-insensitive and a trailing "*" is a prefix wildcard. An
explicit --event lookup is never hidden.

CloudTrail records events in the region where the activity happened (global
services such as IAM record in us-east-1) — use -r to pick the region.

This is the CLI twin of the summary TUI's 't' CloudTrail timeline.`,
	Example: `  # Who touched this security group?
  aws_explorer trail sg-0abc123

  # What has been happening in the account in the last 2 hours?
  aws_explorer trail --since 2h

  # Everything a principal did
  aws_explorer trail --by alice

  # Every instance-termination call, in a specific region
  aws_explorer trail --event TerminateInstances -r eu-west-1

  # Failed / denied calls only (recon & misconfig triage)
  aws_explorer trail --errors-only --since 24h

  # Explore the feed interactively
  aws_explorer trail --since 24h --tui

  # Machine-readable
  aws_explorer trail my-bucket -o json | jq '.[0]'`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filter, scope, err := buildTrailFilter(args)
		if err != nil {
			return err
		}

		since, err := parseSince(trailSince)
		if err != nil {
			return err
		}

		applyGlobalAWSOverrides()
		ui.InitFromConfig(AppConfig.UI) // theme the status messages
		ctx := context.Background()

		regions := trailRegions()
		awscfg, err := auth.BuildAWSConfig(ctx, &AppConfig.AWS, regions[0])
		if err != nil {
			if hint, ok := awserr.LoginHint(err, AppConfig.AWS.Profile); ok {
				return errors.New(hint)
			}
			return fmt.Errorf("unable to load AWS config: %w", err)
		}

		opts := trail.Options{
			Since:           since,
			Limit:           trailLimit,
			IncludeReadOnly: trailIncludeRead,
			ErrorsOnly:      trailErrorsOnly,
			HideEvents:      AppConfig.Trail.HideEvents,
		}

		// The account-wide feed (no pivot) excludes read-only events
		// server-side via ReadOnly=false, so it returns mutations directly
		// instead of paging through Describe*/List*/Get* noise. The deeper page
		// cap then only matters for --read-events (which drops the server-side
		// filter and must page past the reads). Pivoted lookups match few
		// events, so they keep their shallow cap.
		if filter == (trail.Filter{}) {
			opts.MaxPages = trail.DeepFeedPageCap
		}

		if trailTUI {
			// The feed keeps the newest events that survive the filters; the cap
			// counts only what is shown (reads are excluded server-side). --limit
			// wins; otherwise use config trail.maxEvents, then the built-in
			// default.
			if !cmd.Flags().Changed("limit") {
				opts.Limit = AppConfig.Trail.MaxEvents
				if opts.Limit <= 0 {
					opts.Limit = 200
				}
			}
			SilenceScanLogs()
			m := trailtui.New(ctx, awscfg, regions, filter, opts, scope)
			p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("error running trail TUI: %w", err)
			}
			return nil
		}

		fmt.Fprintln(os.Stderr, ui.InfoStyle().Render(
			fmt.Sprintf("Looking up CloudTrail events for %s across %s (last 90 days max)…",
				scope, trailRegionScope(regions))))

		events, truncated, err := trail.LookupFilteredRegions(ctx, awscfg, regions, filter, opts)
		if err != nil {
			switch {
			case awserr.IsExpiredCreds(err):
				hint, _ := awserr.LoginHint(err, AppConfig.AWS.Profile)
				return errors.New(hint)
			case awserr.IsAuthError(err):
				return fmt.Errorf("not authorized — grant the cloudtrail:LookupEvents IAM permission")
			default:
				return fmt.Errorf("CloudTrail lookup failed: %w", err)
			}
		}

		table := strings.EqualFold(outputFormat, "table")
		if len(events) == 0 {
			if table {
				printNoTrailEvents(scope, trailRegionScope(regions), truncated)
			}
			return nil
		}
		if err := trail.Render(os.Stdout, events, outputFormat, noHeader); err != nil {
			return err
		}
		if truncated && table {
			fmt.Fprintln(os.Stderr, warnStyle().Render(
				"Note: results truncated at the scan cap — older events exist. "+
					"Narrow with --since, pivot with --event/--source/--by, or use `lake` for full history."))
		}
		return nil
	},
}

// trailRegions resolves the regions to query. -r pins a single region;
// otherwise --all-regions (or an "all" entry / multiple entries in
// aws.regions) fans out, defaulting to one region.
func trailRegions() []string {
	if awsRegion != "" {
		return []string{awsRegion}
	}
	if AppConfig.AWS.AllRegions {
		return awsutil.FallbackRegions
	}
	var rs []string
	for _, r := range AppConfig.AWS.Regions {
		if strings.EqualFold(r, "all") {
			return awsutil.FallbackRegions
		}
		rs = append(rs, r)
	}
	if len(rs) > 0 {
		return rs
	}
	return []string{"us-east-1"}
}

// trailRegionScope describes the region set for status messages.
func trailRegionScope(regions []string) string {
	if len(regions) == 1 {
		return regions[0]
	}
	return fmt.Sprintf("%d regions", len(regions))
}

func warnStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))
}

// printNoTrailEvents explains an empty result and points at the levers that
// usually surface the missing events, in color.
func printNoTrailEvents(scope, regionScope string, truncated bool) {
	fmt.Println(warnStyle().Render(
		fmt.Sprintf("No matching CloudTrail events for %s across %s in the scan window.", scope, regionScope)))
	if truncated {
		fmt.Println(warnStyle().Render(
			"The feed scans the most recent events newest-first and stopped at the scan cap — " +
				"in a busy account these can be entirely read-only, hiding older mutations."))
	}
	fmt.Println(ui.MutedStyle().Render("Try one of:"))
	for _, hint := range []string{
		"--event <Name> / --source <svc> / --by <principal>  pivot so the API filters server-side",
		"--read-events                                        include Describe*/List*/Get* calls",
		"--since 7d                                            bound the window",
		"-r <region> / --all-regions                          widen or pin the region",
		"lake --since 90d                                      query CloudTrail Lake for older history",
	} {
		fmt.Println(ui.MutedStyle().Render("  • " + hint))
	}
}

// buildTrailFilter turns the positional resource arg and the --by/--event/
// --source flags into a single trail.Filter, enforcing that at most one is set
// (LookupEvents accepts only one lookup attribute). It also returns a
// human-readable scope description for the progress and empty-result messages.
func buildTrailFilter(args []string) (trail.Filter, string, error) {
	var f trail.Filter
	var scopes []string

	if len(args) == 1 {
		resource := trail.LookupValue(args[0])
		if resource == "" {
			return f, "", fmt.Errorf("the resource ID must not be empty")
		}
		f.ResourceName = resource
		scopes = append(scopes, "resource "+resource)
	}
	if trailBy != "" {
		f.Principal = trailBy
		scopes = append(scopes, "principal "+trailBy)
	}
	if trailEvent != "" {
		f.EventName = trailEvent
		scopes = append(scopes, "event "+trailEvent)
	}
	if trailSource != "" {
		f.EventSource = trailSource
		scopes = append(scopes, "source "+trailSource)
	}

	if len(scopes) > 1 {
		return f, "", fmt.Errorf("CloudTrail LookupEvents accepts only one filter at a time — " +
			"pass just one of: a resource, --by, --event, or --source")
	}
	if len(scopes) == 0 {
		return f, "account-wide activity", nil
	}
	return f, scopes[0], nil
}

// parseSince accepts a day count as "7", "7d", or any Go duration ("36h"),
// returning the cutoff time. Empty means the full LookupEvents window
// (zero time).
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return time.Time{}, nil
	}
	days := s
	if strings.HasSuffix(days, "d") {
		days = strings.TrimSuffix(days, "d")
	}
	if n, err := strconv.Atoi(days); err == nil {
		if n < 0 {
			return time.Time{}, fmt.Errorf("--since must not be negative")
		}
		return time.Now().AddDate(0, 0, -n), nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return time.Time{}, fmt.Errorf("--since must not be negative")
		}
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q (use a day count like 7 or 7d, or a duration like 36h)", s)
}

func init() {
	trailCmd.Flags().StringVar(&trailSince, "since", "",
		"only events after this long ago (e.g. 7d, 36h; default: full 90-day window)")
	trailCmd.Flags().IntVar(&trailLimit, "limit", trail.DefaultLimit,
		"maximum number of events to print")
	trailCmd.Flags().BoolVar(&trailIncludeRead, "read-events", false,
		"include read-only (Describe*/List*/Get*) events")
	trailCmd.Flags().StringVar(&trailBy, "by", "",
		"only events by this principal (IAM user or role session name)")
	trailCmd.Flags().StringVar(&trailEvent, "event", "",
		"only this API call (e.g. TerminateInstances)")
	trailCmd.Flags().StringVar(&trailSource, "source", "",
		"only events from this service (e.g. ec2.amazonaws.com)")
	trailCmd.Flags().BoolVar(&trailErrorsOnly, "errors-only", false,
		"only failed/denied calls (events carrying an errorCode)")
	trailCmd.Flags().BoolVar(&trailTUI, "tui", false,
		"explore the feed interactively (filter, sort, failed-only toggle, per-event detail)")
	rootCmd.AddCommand(trailCmd)
}

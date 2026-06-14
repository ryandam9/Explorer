package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/trail"
)

var (
	trailSince       string
	trailLimit       int
	trailIncludeRead bool
	trailBy          string
	trailEvent       string
	trailSource      string
	trailErrorsOnly  bool
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

By default only mutating events are shown; --read-events includes the
Describe*/List*/Get* noise too. --errors-only keeps just failed/denied calls
(a burst of these is a recon or misconfiguration signal).

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

		fmt.Fprintf(os.Stderr, "Looking up CloudTrail events for %s in %s (last 90 days max)…\n",
			scope, region)

		events, truncated, err := trail.LookupFiltered(ctx, awscfg, region, filter, trail.Options{
			Since:           since,
			Limit:           trailLimit,
			IncludeReadOnly: trailIncludeRead,
			ErrorsOnly:      trailErrorsOnly,
		})
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

		if len(events) == 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Printf("No management events recorded for %s in %s in the lookup window.\n",
				scope, region)
			return nil
		}
		if err := trail.Render(os.Stdout, events, outputFormat, noHeader); err != nil {
			return err
		}
		if truncated && strings.EqualFold(outputFormat, "table") {
			fmt.Fprintf(os.Stderr,
				"\nNote: results truncated at the %d-event scan cap — older events exist. "+
					"Narrow the window with --since to see them.\n", len(events))
		}
		return nil
	},
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
	rootCmd.AddCommand(trailCmd)
}

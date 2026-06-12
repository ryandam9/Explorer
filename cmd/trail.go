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
)

var trailCmd = &cobra.Command{
	Use:   "trail <resource-id-or-arn>",
	Short: `CloudTrail "who changed this" — recent events for a resource`,
	Long: `Trail lists recent CloudTrail management events that reference a resource:
when, which API call, which principal, from which source IP — the "who
changed this and when" of an incident.

It uses cloudtrail:LookupEvents, which covers the last 90 days of management
events with no trail or S3 bucket setup required. Pass a bare resource ID
(i-0abc…, sg-0abc…, a bucket or function name) or a full ARN — ARNs are
reduced to the resource name CloudTrail records.

By default only mutating events are shown; --read-events includes the
Describe*/List*/Get* noise too. Events are newest first.

CloudTrail records events in the region where the resource lives (global
services such as IAM record in us-east-1) — use -r to pick the region.

This is the CLI twin of the summary TUI's 't' CloudTrail timeline.`,
	Example: `  # Who touched this security group?
  aws_explorer trail sg-0abc123

  # Changes to an instance in the last 7 days, in a specific region
  aws_explorer trail i-0abc12345 --since 7d -r eu-west-1

  # ARNs work too; IAM events live in us-east-1
  aws_explorer trail arn:aws:iam::123456789012:role/app -r us-east-1

  # Machine-readable
  aws_explorer trail my-bucket -o json | jq '.[0]'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resource := trail.LookupValue(args[0])
		if resource == "" {
			return fmt.Errorf("the resource ID must not be empty")
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
			resource, region)

		events, err := trail.Lookup(ctx, awscfg, region, resource, trail.Options{
			Since:           since,
			Limit:           trailLimit,
			IncludeReadOnly: trailIncludeRead,
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
				resource, region)
			return nil
		}
		return trail.Render(os.Stdout, events, outputFormat, noHeader)
	},
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
	rootCmd.AddCommand(trailCmd)
}

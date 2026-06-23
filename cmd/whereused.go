package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/clilog"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/xref"
)

var whereUsedCmd = &cobra.Command{
	Use:   "whereused <arn-or-id>",
	Short: `Where-used / blast radius — "can I delete this?"`,
	Long: `Whereused answers "can I delete this?" for the resources people actually ask
about: IAM roles, KMS keys, ACM certificates and security groups. It scans the
account for the linking fields the inventory does not keep — a Lambda's
execution role, a volume's KMS key, a listener's certificate, an ENI's
security groups — and lists every resource that references the target.

Pass a full ARN or a bare ID:

  - IAM role     arn:aws:iam::123456789012:role/app   (or just the role name)
  - KMS key      arn:aws:kms:us-east-1:…:key/<uuid>
  - ACM cert     arn:aws:acm:us-east-1:…:certificate/<id>
  - Security grp sg-0abc123                            (or its ARN)

Crucially, a "not referenced" answer is scoped: the report always lists the
reference types it checked, so absence of evidence is never presented as proof
of absence. The report is read-only and best-effort — a denied or failed API
call narrows what was checked (reported on stderr) and never aborts the run.

This is the CLI generalization of the summary TUI's 'x' cross-reference.`,
	Example: `  # What uses this IAM role?
  aws_explorer whereused arn:aws:iam::123456789012:role/app-task

  # What encrypts with this KMS key, across all regions?
  aws_explorer whereused arn:aws:kms:us-east-1:123456789012:key/abcd-… --all-regions

  # What is this security group attached to?
  aws_explorer whereused sg-0abc123 -r eu-west-1

  # Machine-readable
  aws_explorer whereused sg-0abc123 -o json | jq '.references'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := xref.Classify(args[0])
		if target.Kind == xref.KindUnknown {
			return fmt.Errorf("unrecognized resource %q — pass an IAM role, KMS key, ACM certificate, or security group (ARN or ID)", args[0])
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
		SilenceScanLogs()

		// Status/diagnostic lines go to stderr, tinted by level (and with the
		// user's input highlighted) when stderr is a terminal.
		color := clilog.ColorEnabled(isatty.IsTerminal(os.Stderr.Fd()))

		regions := eng.EffectiveRegions()
		clilog.Statusf(os.Stderr, color, "INFO", "Scanning %d region(s) for references to %s…", len(regions), clilog.Highlight(args[0], color))

		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second
		// whereused is reverse-only at one hop: it asks "what references this",
		// which never needs a role's own attached/inline policy edges. Skip the
		// expensive per-role policy sweep (§7).
		edges, errs := xref.Collect(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout, false, nil)

		// Results first, then the diagnostics (ambiguity warning, collection-error
		// summary), so the report isn't buried under the error block.
		result := xref.WhereUsed(target, xref.BuildIndex(edges))
		if err := xref.Render(os.Stdout, result, outputFormat, noHeader); err != nil {
			return fmt.Errorf("rendering report: %w", err)
		}
		warnAmbiguousTarget(os.Stderr, args[0], edges, color)
		output.PrintErrors(os.Stderr, errs)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whereUsedCmd)
}

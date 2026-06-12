package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/authzmsg"
	"github.com/ryandam9/aws_explorer/internal/awserr"
)

var iamCmd = &cobra.Command{
	Use:   "iam",
	Short: "IAM / access debugging helpers",
	Long: `Helpers for the most common AWS support question: "why am I denied?".

Currently: decode — turn an "Encoded authorization failure message" blob into
a readable verdict.`,
}

var iamDecodeCmd = &cobra.Command{
	Use:   "decode [encoded-message]",
	Short: `Decode an "Encoded authorization failure message"`,
	Long: `Services like EC2 redact the reason for an authorization failure into an
opaque blob ("Encoded authorization failure message: <blob>"). decode calls
sts:DecodeAuthorizationMessage and prints a human summary — the principal,
the denied action, the resource, and whether it was an explicit deny or a
missing allow — followed by the full decoded JSON document.

The message is read from the argument, or from stdin when the argument is "-"
or omitted. Pasting the entire error message works; the blob is extracted.

Requires the sts:DecodeAuthorizationMessage IAM permission.`,
	Example: `  # Decode a blob directly
  aws_explorer iam decode AQoDYXdzEJr...

  # Pipe the whole error message in
  pbpaste | aws_explorer iam decode

  # Just the decoded JSON, for jq
  aws_explorer iam decode AQoDYXdzEJr... -o json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		encoded, err := readDecodeInput(args, os.Stdin)
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

		out, err := sts.NewFromConfig(awscfg).DecodeAuthorizationMessage(ctx,
			&sts.DecodeAuthorizationMessageInput{EncodedMessage: aws.String(encoded)})
		if err != nil {
			switch {
			case awserr.IsExpiredCreds(err):
				hint, _ := awserr.LoginHint(err, AppConfig.AWS.Profile)
				return errors.New(hint)
			case awserr.IsAuthError(err):
				return fmt.Errorf("not authorized to decode — grant the sts:DecodeAuthorizationMessage IAM permission")
			default:
				return fmt.Errorf("decode failed (is the blob complete and unmodified?): %w", err)
			}
		}

		decoded := []byte(aws.ToString(out.DecodedMessage))
		return renderDecoded(os.Stdout, decoded, outputFormat)
	},
}

// readDecodeInput returns the encoded blob from the argument or stdin,
// stripping any surrounding error-message boilerplate.
func readDecodeInput(args []string, stdin io.Reader) (string, error) {
	raw := ""
	if len(args) == 1 && args[0] != "-" {
		raw = args[0]
	} else {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		raw = string(data)
	}
	encoded := authzmsg.StripPrefix(raw)
	if encoded == "" {
		return "", fmt.Errorf("no encoded message given (pass it as an argument or on stdin)")
	}
	return encoded, nil
}

// renderDecoded prints the decoded document: JSON only for -o json, otherwise
// the human summary followed by the pretty-printed document.
func renderDecoded(w io.Writer, decoded []byte, format string) error {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, decoded, "", "  "); err != nil {
		// Not JSON? Print verbatim rather than failing — the decode worked.
		pretty.Write(decoded)
	}

	if format == "json" || format == "ndjson" {
		fmt.Fprintln(w, pretty.String())
		return nil
	}

	summary, err := authzmsg.Summarize(decoded)
	if err == nil {
		fmt.Fprintln(w, authzmsg.Render(summary))
	}
	fmt.Fprintln(w, "Full decoded document:")
	fmt.Fprintln(w, pretty.String())
	return nil
}

func init() {
	iamCmd.AddCommand(iamDecodeCmd)
	rootCmd.AddCommand(iamCmd)
}

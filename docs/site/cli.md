[← Documentation index](index.md)

# aws_explorer

Discover and list AWS resources across accounts and regions

AWS Explorer discovers, monitors and lists AWS resources — EC2, S3, RDS,
Lambda and a dozen more services — across accounts and regions.

Run with no arguments to scan the enabled services and stream results to
stdout as they arrive, or use a subcommand for an interactive TUI.

Configuration is optional: when no config.yaml exists in the current
directory or in the user config directory, built-in defaults are used.
Run "aws_explorer config init" to write a starter file.

## Usage

```
aws_explorer [flags]
```

## Examples

```bash
# Scan the configured services and regions
aws_explorer

# Scan a single region with a named profile
aws_explorer --profile prod --region eu-west-1

# Machine-readable output
aws_explorer -o json | jq '.[].id'
aws_explorer -o ndjson | head
aws_explorer -o csv --no-header > resources.csv

# Scan every available region
aws_explorer --all-regions
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--all-regions` | — | scan all available AWS regions |
| `--auth-method` | — | auth method: auto, profile, env, static, sts (overrides aws.authMethod in config) |
| `--config` | — | config file (default: ./config.yaml, then the user config dir, then built-in defaults) |
| `--no-header` | — | omit the header row in table and csv output |
| `--output` / `-o` | table | output format: table, json, ndjson, csv |
| `--profile` | — | AWS named profile (overrides aws.profile in config) |
| `--region` / `-r` | — | scan only this region (overrides aws.regions, --all-regions and region filters) |
| `--role-arn` | — | IAM role ARN to assume via STS (sets auth method to sts) |

## Subcommands

- [`aws_explorer audit`](audit.md) — Scan the account for cost waste and security risks (findings linter)
- [`aws_explorer bill`](bill.md) — Show the account's bill from Cost Explorer (live --tui)
- [`aws_explorer config`](config.md) — Manage the configuration file
- [`aws_explorer cw`](cw.md) — Start the CloudWatch Logs Explorer TUI
- [`aws_explorer ecs`](ecs.md) — ECS triage helpers
- [`aws_explorer expiring`](expiring.md) — List everything that breaks on a calendar date
- [`aws_explorer find`](find.md) — Fuzzy-find any resource by name, ID, ARN or type
- [`aws_explorer iam`](iam.md) — IAM / access debugging helpers
- [`aws_explorer s3`](s3.md) — Start the S3 Explorer TUI
- [`aws_explorer snapshot-diff`](snapshot-diff.md) — Browse a saved inventory snapshot, or diff two snapshots, offline
- [`aws_explorer summary`](summary.md) — List every AWS resource across all regions
- [`aws_explorer trail`](trail.md) — CloudTrail "who changed this" — recent events for a resource
- [`aws_explorer vpc`](vpc.md) — Start the VPC Explorer TUI
- [`aws_explorer whereused`](whereused.md) — Where-used / blast radius — "can I delete this?"

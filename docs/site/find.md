[← Documentation index](index.md)

# aws_explorer find

Fuzzy-find any resource by name, ID, ARN or type

Find scans the configured regions (typed collectors plus the all-services
Tagging API sweep) and fuzzy-matches every resource against the fragment —
name, ID, ARN, type and region all count. Best matches print first.

The match is an in-order subsequence, so separators can be skipped:
"eni0abc" finds eni-0abc12, "prodweb" finds prod-web-3.

This is the CLI twin of the summary TUI's Ctrl+P jump palette.

## Usage

```
aws_explorer find <fragment> [flags]
```

## Examples

```bash
# What is this ENI from an error message?
aws_explorer find eni-0abc

# Find by name fragment across every region
aws_explorer find prodweb --all-regions

# Machine-readable
aws_explorer find payments -o json | jq '.[0].arn'
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--limit` | 25 | maximum number of matches to print |

## Global flags

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

---

_Part of [`aws_explorer`](cli.md)._

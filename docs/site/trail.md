[← Documentation index](index.md)

# aws_explorer trail

CloudTrail "who changed this" — recent events for a resource

Trail lists recent CloudTrail management events that reference a resource:
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

This is the CLI twin of the summary TUI's 't' CloudTrail timeline.

## Usage

```
aws_explorer trail <resource-id-or-arn> [flags]
```

## Examples

```bash
# Who touched this security group?
aws_explorer trail sg-0abc123

# Changes to an instance in the last 7 days, in a specific region
aws_explorer trail i-0abc12345 --since 7d -r eu-west-1

# ARNs work too; IAM events live in us-east-1
aws_explorer trail arn:aws:iam::123456789012:role/app -r us-east-1

# Machine-readable
aws_explorer trail my-bucket -o json | jq '.[0]'
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--limit` | 50 | maximum number of events to print |
| `--read-events` | — | include read-only (Describe*/List*/Get*) events |
| `--since` | — | only events after this long ago (e.g. 7d, 36h; default: full 90-day window) |

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

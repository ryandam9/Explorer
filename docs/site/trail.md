[← Documentation index](index.md)

# aws_explorer trail

CloudTrail activity feed — who did what, and who changed this

Trail lists recent CloudTrail management events: when, which API call, which
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

The --tui feed shows every event (read-only included) and lets you toggle
read-only events (o) and the failed-only view (x) live, so nothing is dropped
behind your back.

To permanently suppress noisy events (e.g. AssumeRole, ConsoleLogin) without
re-passing flags, list them under trail.hideEvents in the config file —
matching is case-insensitive and a trailing "*" is a prefix wildcard
("Describe*"). Hidden events are dropped from this CLI output; in the TUI they
are hidden by default and revealed with the "h" key.

CloudTrail records events in the region where the activity happened (global
services such as IAM record in us-east-1) — use -r to pick the region.

This is the CLI twin of the summary TUI's 't' CloudTrail timeline.

## Usage

```
aws_explorer trail [resource-id-or-arn] [flags]
```

## Examples

```bash
# Who touched this security group?
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
aws_explorer trail my-bucket -o json | jq '.[0]'
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--by` | — | only events by this principal (IAM user or role session name) |
| `--errors-only` | — | only failed/denied calls (events carrying an errorCode) |
| `--event` | — | only this API call (e.g. TerminateInstances) |
| `--limit` | 50 | maximum number of events to print |
| `--read-events` | — | include read-only (Describe*/List*/Get*) events |
| `--since` | — | only events after this long ago (e.g. 7d, 36h; default: full 90-day window) |
| `--source` | — | only events from this service (e.g. ec2.amazonaws.com) |
| `--tui` | — | explore the feed interactively (filter, sort, failed-only toggle, per-event detail) |

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

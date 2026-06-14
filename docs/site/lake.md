[← Documentation index](index.md)

# aws_explorer lake

Query CloudTrail Lake — years of history, data events, aggregation (SQL)

Lake queries a CloudTrail Lake event data store with SQL. Unlike the trail
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
the trail command for the zero-setup 90-day feed.

## Usage

```
aws_explorer lake [flags]
```

## Examples

```bash
# What stores can I query?
aws_explorer lake --list-stores

# Recent activity in the last 30 days
aws_explorer lake --since 30d

# Who has been the busiest principal this quarter?
aws_explorer lake --top-principals --since 90d

# Most frequent API calls, interactively
aws_explorer lake --top-events --tui

# Your own SQL
aws_explorer lake --sql "SELECT eventName, COUNT(*) c FROM <eds-id> GROUP BY eventName ORDER BY c DESC LIMIT 20"
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--by` | — | filter built-in queries to a principal (substring of the ARN) |
| `--errors-only` | — | filter built-in queries to failed/denied calls |
| `--event` | — | filter built-in queries to one API call |
| `--limit` | 50 | maximum number of rows to return |
| `--list-stores` | — | list available event data stores and exit |
| `--max-wait` | 60s | how long to wait for the query to finish |
| `--since` | — | only events after this long ago (e.g. 30d, 12h) |
| `--source` | — | filter built-in queries to one service (e.g. s3.amazonaws.com) |
| `--sql` | — | raw CloudTrail Lake SQL (you supply the FROM clause) |
| `--store` | — | event data store to query (id, ARN, or name; default: the only store) |
| `--top-events` | — | built-in query: API calls ranked by frequency |
| `--top-principals` | — | built-in query: principals ranked by event count |
| `--tui` | — | explore the results interactively |

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

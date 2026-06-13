[← Documentation index](index.md)

# aws_explorer bill

Show the account's bill from Cost Explorer (live --tui)

Bill shows the account's cost for a billing period, grouped by service and
usage type, with the usage quantity for each line and a grand total — the
numbers the Billing console shows, pulled from the AWS Cost Explorer API.

By default it reports the current month to date (today's charges are
estimated and flagged as such); --month YYYY-MM reports a past month.

--tui opens a live screen that re-fetches on a fixed interval (--interval,
default 5m), so any activity that incurs cost surfaces without restarting.
A Δ column shows what moved since the previous refresh, and pressing x on a
line lists that service's per-resource costs (resource ID / ARN, usage,
amount) when the account has resource-level data enabled.

Cost note: Cost Explorer is a paid API — AWS bills every request at $0.01,
including each automatic refresh in --tui. The live screen names the cadence
so the cost is visible; raise --interval to spend less.

Required IAM: ce:GetCostAndUsage (and ce:GetCostAndUsageWithResources for the
per-resource drill-down). The call is read-only.

## Usage

```
aws_explorer bill [flags]
```

## Examples

```bash
# Current month to date, grouped by service and usage type
aws_explorer bill

# A past month, machine-readable
aws_explorer bill --month 2026-05 -o json

# Live screen, refreshing every 10 minutes
aws_explorer bill --tui --interval 10m

# CSV for a spreadsheet
aws_explorer bill -o csv --no-header > bill.csv
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--interval` | 5m0s | auto-refresh cadence for --tui (each refresh is a paid Cost Explorer request) |
| `--month` | — | billing period as YYYY-MM (default: current month to date) |
| `--tui` | — | open the live bill screen instead of printing once |

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

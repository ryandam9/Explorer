[← Documentation index](index.md)

# aws_explorer summary

List every AWS resource across all regions

Summary lists all discoverable AWS resources across every configured
region as a single numbered inventory. Each row shows the serial number, the
resource name (or "-" when it has none), the resource type, the ARN, and the
region (with availability zone when the resource is zonal).

By default the inventory is printed as a table (use -o json|ndjson|csv for
other formats). Pass --tui to explore the same data interactively.

--baseline saves the inventory as the account's baseline snapshot
(~/.aws_explorer/account-snapshots/<account-id>/); --diff scans again later
and reports what was added, removed, or modified since — "what changed in
this account since yesterday". Baselines are keyed by account and region
scope, and only stable fields (name, state, tags) are compared, so an
unchanged account diffs clean.

## Usage

```
aws_explorer summary [flags]
```

## Examples

```bash
# Full inventory of every region
aws_explorer summary --all-regions

# One region, exported as CSV
aws_explorer summary -r us-east-1 -o csv > inventory.csv

# Explore interactively
aws_explorer summary --tui

# What changed in this account since yesterday?
aws_explorer summary --baseline            # yesterday
aws_explorer summary --diff                # today
aws_explorer summary --diff -o json        # for automation
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--baseline` | — | Save this scan as the account's baseline snapshot |
| `--diff` | — | Diff this scan against the saved baseline (what changed since) |
| `--tui` | — | Explore the inventory interactively instead of printing a table |
| `--typed-only` | — | Only use the built-in typed collectors; skip the all-services Tagging API sweep |

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

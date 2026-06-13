[← Documentation index](index.md)

# aws_explorer expiring

List everything that breaks on a calendar date

Expiring reports every deadline in the account, sorted by days remaining —
already-passed items first, with negative day counts:

  - ACM certificates approaching expiry (and whether they are in use)
  - Legacy IAM server certificates approaching expiry
  - Lambda functions on runtimes with an announced deprecation date
  - EKS clusters whose Kubernetes version is reaching end of standard support
  - RDS instances pinned to an expired CA certificate, and pending
    maintenance actions with an apply date
  - Secrets Manager secrets whose rotation is overdue

--within bounds the horizon (default 90 days); items already past are always
shown. Runtime/version end-of-life tables reflect AWS's published schedules
as of this release and are reviewed each release.

The report is read-only and best-effort: a denied API call skips that source
(reported on stderr) and never aborts the run.

## Usage

```
aws_explorer expiring [flags]
```

## Examples

```bash
# Everything breaking in the next 90 days, all regions
aws_explorer expiring --all-regions

# A tighter horizon
aws_explorer expiring --within 30d

# Machine-readable, e.g. page on anything within two weeks
aws_explorer expiring -o json | jq '[.[] | select(.days <= 14)]'
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--within` | 90d | horizon for upcoming deadlines (e.g. 30d); already-passed items always show |

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

[← Documentation index](index.md)

# aws_explorer quotas

Service-quota dashboard — limits closest to exhaustion

Quotas reports a curated set of the AWS limits that actually cause incidents
— vCPUs, Elastic IPs, VPCs, network interfaces, Lambda concurrency, RDS
instances, EBS storage, load balancers, EKS clusters, IAM roles — with their
real limits and current usage, sorted so the ones nearest the ceiling lead.

Limits come from the Service Quotas API's applied value, so account-specific
increases are reflected (the VPC linter uses hardcoded defaults; this does
not). When a quota has never been adjusted, the AWS default is shown instead.

Usage comes from each quota's CloudWatch usage metric where AWS publishes one;
quotas without a usage metric are listed with their limit but no percentage
(shown only with --threshold 0) rather than a guess.

The report is read-only and best-effort: a denied or failed lookup skips that
quota (reported on stderr) and never aborts the run.

## Usage

```
aws_explorer quotas [flags]
```

## Examples

```bash
# Quotas at or above 80% utilization (default), current region
aws_explorer quotas

# Tighter alerting threshold, across all regions
aws_explorer quotas --threshold 90 --all-regions

# Everything, including quotas with no usage metric
aws_explorer quotas --threshold 0

# Machine-readable; page on anything critical
aws_explorer quotas --threshold 0 -o json | jq '[.[] | select(.status=="critical")]'
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--threshold` | 80 | only show quotas at or above this % utilization (0 = show all, including those with no usage metric) |

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

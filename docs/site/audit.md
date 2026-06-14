[← Documentation index](index.md)

# aws_explorer audit

Scan the account for cost waste and security risks (findings linter)

Audit scans the configured regions and reports findings in two categories
(both run by default; --only narrows):

cost — unattached EBS volumes, gp2 volumes that could be gp3, unassociated
Elastic IPs, idle NAT gateways, load balancers with no healthy targets or no
traffic, stopped instances still paying for EBS storage, old unreferenced
snapshots and AMIs, and over-provisioned DynamoDB tables. Each cost finding
carries an approximate monthly cost, with a total at the bottom (us-east-1
on-demand list prices; order-of-magnitude, not a bill).

security — public S3 buckets and missing Public Access Blocks, buckets
without default encryption, unencrypted EBS volumes and regions without
EBS encryption-by-default, publicly shared EBS/RDS snapshots, publicly
accessible or unencrypted RDS instances, EC2 instances still allowing
IMDSv1, security groups opening sensitive ports (SSH, RDP, databases) to
the internet, Lambda function URLs with no auth, SQS/SNS policies that
allow everyone, and alarms stuck in INSUFFICIENT_DATA.

iam — account-global hygiene via the credential report and policy scan:
root access keys, console users without MFA, access keys older than 90
days or active-but-unused, roles unused for 90+ days, customer policies
granting */*, trust policies allowing any AWS principal, and policies
attached directly to users.

messaging — broken async plumbing: queues filling with no consumers,
redrive policies pointing at queues that no longer exist, dead-letter
queues with messages waiting, SNS subscriptions stuck unconfirmed, and
topics with zero subscriptions.

cloudtrail — account-global audit-trail posture: no multi-region trail
actively logging, trails without log file validation, logs not encrypted
with a customer KMS key, trails not delivering to CloudWatch Logs, and
trails that don't record all management events.

Every finding carries a stable check ID (e.g. COST-EBS-001, SEC-S3-001,
IAM-KEY-001, MSG-SQS-001, CT-TRAIL-001).

For CI pipelines, --fail-on <severity> exits 2 when findings at or above the
threshold exist (0 below it, 1 on operational errors), --ignore suppresses
individual checks by ID, and -o sarif emits SARIF 2.1.0 for upload to GitHub
code scanning.

The audit is read-only and best-effort: a denied API call skips the affected
checks (reported on stderr) and never aborts the run. Traffic-based checks
(idle load balancers, DynamoDB utilization) additionally need
cloudwatch:GetMetricData and are skipped without it.

## Usage

```
aws_explorer audit [flags]
```

## Examples

```bash
# Audit the configured regions
aws_explorer audit

# Audit every region, machine-readable
aws_explorer audit --all-regions -o json

# Explore interactively
aws_explorer audit --all-regions --tui

# Security category only
aws_explorer audit --only security

# IAM hygiene only (account-global; region flags don't matter)
aws_explorer audit --only iam

# Async plumbing only
aws_explorer audit --only messaging

# CI gate: exit 2 if any warning-or-worse finding exists
aws_explorer audit --fail-on warning --ignore COST-EBS-002,SEC-EC2-001

# SARIF for GitHub code scanning
aws_explorer audit -o sarif > audit.sarif
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--fail-on` | — | exit with code 2 if findings at or above this severity exist: critical, warning, info |
| `--ignore` | — | suppress findings by check ID (e.g. COST-EBS-002) |
| `--only` | — | restrict to these finding categories (available: cost, security, iam, messaging, cloudtrail) |
| `--tui` | — | explore the findings interactively instead of printing |

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

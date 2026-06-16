# Quotas (service-quota dashboard)

`quotas` reports a curated set of the AWS limits that actually cause incidents
— vCPUs, Elastic IPs, VPCs, network interfaces, Lambda concurrency, RDS
instances, EBS storage, load balancers, EKS clusters, IAM roles — with their
real limits and current usage, sorted so the ones nearest the ceiling lead.

```bash
./bin/aws_explorer quotas [--threshold 80] [--all-regions] [-o table|json|ndjson|csv]
```

```
SNO  QUOTA                                 REGION      USED / LIMIT  %     STATUS
1    VPCs per Region                       us-east-1   5 / 5         100%  CRITICAL
2    Running On-Demand Standard instances  us-east-1   58 / 64       91%   WARN
```

Limits come from the **Service Quotas API's applied value**, so
account-specific increases are reflected — unlike the VPC linter, which uses
hardcoded defaults. When a quota has never been adjusted, the AWS default is
shown instead (the real ceiling either way). Usage comes from each quota's
CloudWatch usage metric where AWS publishes one; quotas without a usage metric
are listed with their limit but no percentage (visible only with
`--threshold 0`) rather than a guess.

| Flag | Default | Description |
|------|---------|-------------|
| `--threshold` | `80` | Only show quotas at or above this % utilization; `0` shows all, including those with no usage metric |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

> **Scope.** The curated registry covers ~17 high-impact quotas across EC2,
> VPC, Lambda, RDS, EBS, ELBv2, EKS and IAM rather than dumping the thousands
> AWS exposes. A quota whose code is unknown to your account simply degrades to
> a skipped, reported entry.

**IAM permissions.** Read-only: `servicequotas:{GetServiceQuota,GetAWSDefaultServiceQuota}`
and `cloudwatch:GetMetricStatistics`. Any denial skips that quota with a note
on stderr and never aborts the run.

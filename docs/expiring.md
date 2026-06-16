# Expiring Usage

`expiring` answers "what breaks next?" — one report of every calendar-driven
deadline, sorted by days remaining. Already-passed items lead with negative
day counts, so the things currently broken are impossible to miss.

```bash
./bin/aws_explorer expiring [--within 90d] [--all-regions] [-o table|json|ndjson|csv]
```

```
SNO   DAYS  WHAT                                 RESOURCE                  REGION      DETAIL
1       -3  Lambda runtime deprecated            payments-fn (python3.9)   us-east-1   runtime python3.9 was deprecated 2025-12-15 — update the function's runtime
2       12  ACM certificate expires              *.example.com             us-east-1   certificate is in use — renew or re-issue before it expires (expires 2026-06-24)
3       61  EKS version end of standard support  prod-cluster (1.33)       eu-west-1   standard support for 1.33 ends 2026-07-29 — upgrade the cluster (extended support bills extra)
```

What it checks:

| Source | Deadline |
|--------|----------|
| ACM certificates | `NotAfter` expiry, noting whether the certificate is in use |
| IAM server certificates | Expiry of legacy certificates (migrate to ACM) |
| Lambda functions | The runtime's published deprecation date |
| EKS clusters | The Kubernetes version's end of standard support (extended support bills extra) |
| RDS instances | Expired CA certificates (e.g. `rds-ca-2019`) and pending maintenance actions with an apply date |
| Secrets Manager | Rotation-enabled secrets whose next rotation is overdue |

| Flag | Default | Description |
|------|---------|-------------|
| `--within` | `90d` | Horizon for upcoming deadlines (`30`, `30d`); already-passed items always show |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

> **About the EOL tables.** Lambda runtime and EKS version schedules are
> static tables baked into the binary (`internal/expiry/eol.go`, sources
> commented), reviewed each release. A runtime or version missing from the
> table produces no row — the report under-warns rather than mis-warns.

**IAM permissions.** Read-only: `acm:ListCertificates`,
`iam:ListServerCertificates`, `lambda:ListFunctions`, `eks:{ListClusters,
DescribeCluster}`, `rds:{DescribeDBInstances,DescribePendingMaintenanceActions}`,
`secretsmanager:ListSecrets`. Any denial skips that source with a note.

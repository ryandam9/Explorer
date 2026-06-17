# Audit Usage

`audit` scans the configured regions for findings in four categories —
**cost/waste**, **security**, **IAM hygiene**, and **messaging plumbing**
(all run by default; `--only cost,security,iam,messaging` narrows) — and
prints them as a ranked table, with an estimated monthly cost per cost
finding and a total at the bottom. Like everything else in the tool, the audit
is **deterministic, read-only and best-effort**: a denied API call skips the
affected checks (reported on stderr) and never aborts the run.

```bash
./bin/aws_explorer audit [flags]
```

Accepts the same global flags as the CLI command (`--config`, `--profile`,
`--auth-method`, `--role-arn`, `--region`, `--all-regions`, `-o`, `--no-header`).

```
SNO  SEVERITY    ID            RESOURCE        REGION     ISSUE                                  EST/MO   FIX
1    🟡 WARNING  COST-EBS-001  vol-0abc        us-east-1  Unattached EBS volume (gp2, 1024 GiB)  $102.40  Snapshot the volume and delete it, …
2    🟡 WARNING  COST-NAT-001  nat-01 (spare)  us-east-1  NAT gateway not referenced by any route $32.85  Delete the NAT gateway, …
3    🔵 INFO     COST-EBS-002  vol-0def        us-east-1  gp2 volume could be gp3 (500 GiB)      $10.00   Modify the volume type to gp3 …

0 critical, 2 warning, 1 info — estimated potential savings ≈ $145.25/month
```

### The checks

Every check has a **stable ID** (never renumbered, safe to reference in
runbooks and scripts):

| ID | Finding | Severity |
|----|---------|----------|
| `COST-EBS-001` | Unattached EBS volume (status `available`) | 🟡 warning |
| `COST-EBS-002` | gp2 volume that could be gp3 (~20% cheaper, online migration) | 🔵 info |
| `COST-EIP-001` | Elastic IP not associated with anything | 🟡 warning |
| `COST-NAT-001` | NAT gateway no route table routes through (idle, still billing) | 🟡 warning |
| `COST-ELB-001` | Load balancer with target groups but zero healthy targets | 🟡 warning |
| `COST-ELB-002` | Load balancer with zero requests/flows in 14 days † | 🟡 warning |
| `COST-EC2-001` | Stopped instance whose attached EBS volumes keep billing | 🔵 info |
| `COST-SNAP-001` | Snapshot >180 days old, not referenced by any AMI in the account | 🔵 info |
| `COST-AMI-001` | AMI >180 days old that no instance uses (backing snapshots still bill) | 🔵 info |
| `COST-DDB-001` | Provisioned DynamoDB table consuming <10% of its capacity † | 🟡 warning |

**Security category** (`--only security`; check IDs `SEC-*`):

| ID | Finding | Severity |
|----|---------|----------|
| `SEC-S3-001` | S3 bucket whose policy status reports it as public | 🔴 critical |
| `SEC-S3-002` | S3 bucket without all four Public Access Block settings on | 🔴 critical |
| `SEC-S3-003` | S3 bucket with no default encryption configuration | 🟡 warning |
| `SEC-EBS-001` | EBS volume not encrypted | 🟡 warning |
| `SEC-EBS-002` | Region with EBS encryption-by-default disabled | 🟡 warning |
| `SEC-SNAP-001` | EBS snapshot restorable by **any** AWS account | 🔴 critical |
| `SEC-RDS-001` | RDS instance with `PubliclyAccessible` on | 🔴 critical |
| `SEC-RDS-002` | RDS instance without storage encryption | 🟡 warning |
| `SEC-RDS-003` | RDS manual snapshot restorable by **any** AWS account | 🔴 critical |
| `SEC-EC2-001` | EC2 instance allowing IMDSv1 (`HttpTokens` ≠ `required`) | 🟡 warning |
| `SEC-SG-001` | Security group opening a sensitive port (SSH, RDP, MySQL, PostgreSQL, SQL Server, MongoDB, Redis, Elasticsearch, memcached) to `0.0.0.0/0` or `::/0` | 🔴 critical |
| `SEC-LAMBDA-001` | Lambda function URL with `AuthType: NONE` | 🔴 critical |
| `SEC-SQS-001` | SQS queue policy with an unconditioned `Allow` for `Principal: "*"` | 🔴 critical |
| `SEC-SNS-001` | SNS topic policy with an unconditioned `Allow` for `Principal: "*"` | 🔴 critical |
| `SEC-CW-001` | CloudWatch alarm in `INSUFFICIENT_DATA` for >7 days (broken monitoring) | 🔵 info |

Security-category notes:

- **Under-warn, never mis-warn**: posture facts the audit could not determine
  (a denied `GetBucketPolicyStatus`, for example) simply don't fire checks —
  the denial appears in the errors summary instead.
- S3 is account-global, so the bucket sweep runs once (in the first scanned
  region); each bucket's posture calls go to the bucket's own region.
- Per-resource sweeps (bucket posture, RDS snapshot attributes, function
  URLs, queue/topic policies) are capped (100–200 each) so huge accounts
  audit in bounded time; hitting a cap is reported as a `Truncated` note.
- Extra IAM needed beyond the cost category:
  `s3:{ListAllMyBuckets,GetBucketLocation,GetBucketPolicyStatus,GetBucketPublicAccessBlock,GetEncryptionConfiguration}`,
  `ec2:{GetEbsEncryptionByDefault}`, `rds:{DescribeDBSnapshots,DescribeDBSnapshotAttributes}`,
  `lambda:{ListFunctions,ListFunctionUrlConfigs}`, `sqs:{ListQueues,GetQueueAttributes}`,
  `sns:{ListTopics,GetTopicAttributes}`, `cloudwatch:DescribeAlarms`.

**IAM hygiene category** (`--only iam`; check IDs `IAM-*`; account-global —
runs once per audit and reports with region `global`):

| ID | Finding | Severity |
|----|---------|----------|
| `IAM-ROOT-001` | Root account has an active access key | 🔴 critical |
| `IAM-USER-001` | Console user (password enabled) without MFA | 🔴 critical |
| `IAM-KEY-001` | Access key not rotated in over 90 days | 🟡 warning |
| `IAM-KEY-002` | Active access key unused for 90+ days (or never used and 90+ days old) | 🔴 critical |
| `IAM-ROLE-001` | Role not assumed in 90+ days (`RoleLastUsed`; service-linked and <90-day-old roles exempt) | 🔵 info |
| `IAM-POLICY-001` | Customer-managed policy granting `Action "*"` on `Resource "*"` | 🔴 critical |
| `IAM-POLICY-002` | Policy attached directly to users instead of groups/roles | 🔵 info |
| `IAM-TRUST-001` | Role trust policy with an unconditioned `Allow` for `"AWS": "*"` | 🔴 critical |

IAM-category notes:

- User and key checks come from the **credential report**; generation is
  asynchronous, so the audit polls briefly (~16s max) and skips those
  checks with a note if the report isn't ready — rerun a minute later.
- Role usage needs one `GetRole` per role and policy documents one
  `GetPolicyVersion` per customer policy; both sweeps are capped at 200
  (reported as `Truncated` when hit). Denied calls leave facts unknown and
  fire nothing.
- Extra IAM needed: `iam:{GenerateCredentialReport,GetCredentialReport,
  ListRoles,GetRole,ListPolicies,GetPolicyVersion,ListEntitiesForPolicy}`.

**Messaging category** (`--only messaging`; check IDs `MSG-*`) — broken
async plumbing is silent until something downstream pages:

| ID | Finding | Severity |
|----|---------|----------|
| `MSG-SQS-001` | Queue accumulating messages with nothing in flight and zero receive calls in 24h ‡ | 🟡 warning |
| `MSG-SQS-002` | Redrive policy whose dead-letter target queue doesn't exist (poisoned messages are dropped) | 🔴 critical |
| `MSG-SQS-003` | Dead-letter queue with failed messages waiting (names the queues redriving to it) | 🟡 warning |
| `MSG-SNS-001` | Topic with subscriptions stuck in `PendingConfirmation` (endpoint receives nothing) | 🟡 warning |
| `MSG-SNS-002` | Topic with zero subscriptions — everything published is discarded | 🔵 info |

Messaging-category notes:

- ‡ The no-consumers check needs `cloudwatch:GetMetricData` for the 24h
  receive-activity lookback (queried only for queues that already look
  suspicious); without it the check is skipped, never guessed.
- The dangling-redrive check only fires from a **complete** queue listing —
  a truncated or failed listing produces no `MSG-SQS-002` findings.
- The API does not report how long a subscription has been pending, so
  `MSG-SNS-001` notes that a just-created subscription can be ignored.
- Uses `sqs:{ListQueues,GetQueueAttributes}`, `sns:{ListTopics,GetTopicAttributes}` —
  the same calls as the security category.

**CloudTrail category** (`--only cloudtrail`; check IDs `CT-*`; account-global —
trails are enumerated once and reported with region `global`) — audits the
audit trail itself, so an incident always has a record to investigate:

| ID | Finding | Severity |
|----|---------|----------|
| `CT-TRAIL-001` | No multi-region trail is actively logging with global service events (the account has no usable audit trail) | 🔴 critical |
| `CT-TRAIL-002` | Trail without log file validation — delivered logs could be tampered with undetected | 🟡 warning |
| `CT-TRAIL-003` | Trail logs encrypted with SSE-S3 only, not a customer-managed KMS key | 🟡 warning |
| `CT-TRAIL-004` | Trail not delivering to CloudWatch Logs — no metric filters or alarms on its events | 🔵 info |
| `CT-TRAIL-005` | Trail not recording all management read/write events, leaving gaps in the record | 🟡 warning |

CloudTrail-category notes:

- **Under-warn, never mis-warn**: `CT-TRAIL-001` fires only from a successful
  `DescribeTrails`; a denied listing leaves coverage unknown and fires nothing.
  Per-trail status and event-selector checks likewise stay silent when their
  `GetTrailStatus` / `GetEventSelectors` call is denied.
- A multi-region trail is enumerated once (it surfaces from any region as a
  shadow entry), so duplicate findings across regions are avoided.
- Uses `cloudtrail:{DescribeTrails,GetTrailStatus,GetEventSelectors}` — distinct
  from the `cloudtrail:LookupEvents` permission the `trail` command uses.

**Glue category** (`--only glue`; check IDs `GLU-*`) — data-integration health
for AWS Glue jobs and crawlers:

| ID | Finding | Severity |
|----|---------|----------|
| `GLU-JOB-001` | Job whose last several runs (≥3) all ended in a failure state | 🔴 critical |
| `GLU-JOB-002` | Job that has never run, or has not run in over 30 days | 🔵 info |
| `GLU-JOB-003` | Job whose most recent run failed (without a sustained streak) | 🟡 warning |
| `GLU-COST-001` | Job burning DPU-hours on failed runs (carries an estimated `$`) | 🟡 warning |
| `GLU-COST-002` | Job with many workers but a very short successful run time (over-provisioned) | 🔵 info |
| `GLU-SEC-001` | Job without a security configuration (logs/output/bookmarks unencrypted) | 🟡 warning |
| `GLU-CRAWL-001` | Crawler whose last crawl ended in `FAILED` (catalog may be stale) | 🟡 warning |
| `GLU-CRAWL-002` | Crawler `RUNNING` for over 6 hours — likely stuck | 🟡 warning |
| `GLU-CONN-001` | Connection referencing a subnet or security group that no longer exists | 🔵 info |

Glue-category notes:

- The run-based checks (`GLU-JOB-*`, `GLU-COST-*`) read each job's recent run
  history via `glue:GetJobRuns`; a denied call leaves run health unknown and
  fires nothing for that job (under-warn). `GLU-SEC-001` only needs `GetJobs`.
- `GLU-COST-001`'s estimate is the wasted spend across the **observed run
  window** (not a monthly figure), at the same `$0.44`/DPU-hour rate the `glue`
  dashboard uses.
- `GLU-CONN-001` cross-references each connection's VPC requirements against the
  region's live subnets/security groups; it fires only from a **complete** EC2
  inventory (a denied `DescribeSubnets`/`DescribeSecurityGroups` leaves it
  silent), and the EC2 calls run only when a connection actually has VPC config.
- Uses `glue:{GetJobs,GetJobRuns,GetCrawlers,GetConnections}` plus
  `ec2:{DescribeSubnets,DescribeSecurityGroups}` (for `GLU-CONN-001`).

**EMR category** (`--only emr`; check IDs `EMR-*`) — health and cost for Amazon
EMR clusters:

| ID | Finding | Severity |
|----|---------|----------|
| `EMR-STEP-002` | Cluster terminated with errors (bootstrap/step/hardware failure) | 🔴 critical |
| `EMR-COST-001` | Cluster idle in `WAITING` for over 24h — provisioned but doing no work | 🟡 warning |
| `EMR-STEP-001` | Cluster whose most recent step ended in a failure state | 🟡 warning |
| `EMR-LOG-001` | Cluster with no S3 log URI — logs are lost when nodes terminate | 🟡 warning |
| `EMR-SEC-001` | Cluster without a security configuration (encryption not enforced) | 🟡 warning |
| `EMR-COST-002` | Long-running cluster (up over 7 days) with no auto-termination policy | 🔵 info |

EMR-category notes:

- A `TERMINATED_WITH_ERRORS` cluster gets only `EMR-STEP-002`; a cleanly
  `TERMINATED` cluster is silent (it's gone). The live-posture checks apply only
  to running/waiting clusters.
- `EMR-STEP-001` reads each live cluster's latest step via `ListSteps`; a denied
  call leaves step health unknown and fires nothing for that cluster
  (under-warn).
- Uses `elasticmapreduce:{ListClusters,DescribeCluster,ListSteps}`.

**Lambda category** (`--only lambda`; check IDs `LAM-*`) — runtime and health
for AWS Lambda functions:

| ID | Finding | Severity |
|----|---------|----------|
| `LAM-RUN-001` | Function on a deprecated runtime — updates are blocked | 🟡 warning |
| `LAM-CFG-002` | Function in a failed state, or whose last update failed | 🟡 warning |
| `LAM-RUN-002` | Function's runtime is scheduled for deprecation within 90 days | 🔵 info |
| `LAM-CFG-001` | Function with no dead-letter queue for failed async invocations | 🔵 info |

Lambda-category notes:

- Runtime checks read the shared end-of-life table (`internal/expiry/eol.go`);
  a runtime missing from it fires nothing (under-warn), and container-image
  functions (no runtime identifier) are skipped.
- `LAM-CFG-002` fires only when the list response reported a state; a sparse
  response silences it rather than guessing.
- `LAM-CFG-001` is informational and worded honestly — an on-failure
  destination (which `ListFunctions` doesn't expose) is a valid alternative to
  a dead-letter queue, so the finding reports what is known, not that events are
  definitely dropped.
- Everything comes from one paginated `lambda:ListFunctions` call — no
  per-function fan-out. The same checks back the `lambda` dashboard's `f`
  findings panel.

† Traffic-based checks use CloudWatch metrics over a 14-day window and need
`cloudwatch:GetMetricData`; without it they are skipped (with a note) while
the rest of the audit runs. Resources younger than 14 days are never flagged
by them.

> **About the estimates.** Each finding's `EST/MO` comes from a static table of
> us-east-1 on-demand list prices (`internal/costs`, sources commented). They
> are order-of-magnitude estimates to rank waste and justify action — not a
> bill: regional price differences, discounts and data-transfer charges are not
> modeled, and snapshot estimates are upper bounds (snapshots are incremental).

### Audit Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--only` | all | Restrict to finding categories (currently: `cost`); more categories are planned |
| `--tui` | `false` | Explore the findings interactively instead of printing |
| `--fail-on` | — | Exit with code 2 if findings at or above this severity exist: `critical`, `warning`, `info` |
| `--ignore` | — | Suppress findings by check ID (e.g. `--ignore COST-EBS-002,COST-SNAP-001`); unknown IDs are rejected |
| `--output` / `-o` | `table` | `table`, `json` (findings + total), `ndjson`, `csv`, `sarif` (SARIF 2.1.0) |

```bash
# Audit every region
./bin/aws_explorer audit --all-regions

# Machine-readable, e.g. total potential savings
./bin/aws_explorer audit -o json | jq .totalMonthlyUSD

# One finding per line for scripting
./bin/aws_explorer audit -o ndjson | jq -r 'select(.id=="COST-EBS-001") | .resource'

# Explore interactively
./bin/aws_explorer audit --all-regions --tui
```

### Using audit as a CI gate

`--fail-on` gives the audit meaningful exit codes, and `-o sarif` emits
[SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html)
that GitHub code scanning ingests directly — check IDs become rules, findings
become alerts.

| Exit code | Meaning |
|-----------|---------|
| `0` | No findings at or above the `--fail-on` threshold (or no threshold set) |
| `1` | Operational error (bad flags, engine failed to start, rendering failed) |
| `2` | Findings at or above the threshold exist |

```bash
# Fail the pipeline on any warning-or-worse waste, tolerating gp2 volumes
aws_explorer audit --all-regions --fail-on warning --ignore COST-EBS-002

# Upload to GitHub code scanning
aws_explorer audit --all-regions -o sarif > audit.sarif
```

```yaml
# .github/workflows/cost-audit.yml
- name: Cost audit
  run: aws_explorer audit --all-regions -o sarif > audit.sarif
- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: audit.sarif
```

`--fail-on` is for scripting and cannot be combined with `--tui`; `--ignore`
works everywhere (CLI, TUI, all output formats).

### Audit TUI

`--tui` opens the findings in an interactive table that fills in **while the
scan runs** — each region's findings appear as soon as that region completes,
with a live progress meter in the header alongside the severity tally and the
running savings total. The table uses the same theme, pinned-column horizontal
scrolling and context-aware status bar as every other TUI in the app.

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate findings |
| `Enter` | Detail overlay — full explanation, fix, ARN and estimate for the selected finding |
| `/` | Quick filter (matches any field, live `matched/total` count) |
| `s` / `R` | Sort by the next column / reverse the direction |
| `r` | Reset filter and sort |
| `<` / `>` (or `,` / `.`) | Scroll table columns on narrow terminals |
| `y` | Copy the selected finding's ARN (or resource ID) to the clipboard |
| `C` | Export the current (filtered, sorted) view to CSV under `~/.aws_explorer/exports/` |
| `e` | Collection-errors overlay (shown as a `⚠ n errors` badge in the header) |
| `?` | Help overlay |
| `q` / `Ctrl+C` | Quit |

**IAM permissions.** Read-only describes: `ec2:Describe{Volumes,Addresses,
NatGateways,RouteTables,Instances,Snapshots,Images}`,
`elasticloadbalancing:Describe{LoadBalancers,TargetGroups,TargetHealth}`,
`dynamodb:{ListTables,DescribeTable}` and (for the traffic-based checks)
`cloudwatch:GetMetricData`. Any denial degrades only the checks that need it.

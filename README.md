# AWS Explorer

Discover, monitor, and display AWS resources across accounts and regions via CLI or TUI.

## Features

- **Four modes**: CLI (streaming table/JSON output), TUI (interactive exploration), VPC Explorer TUI (drill into a VPC's networking), S3 TUI (dedicated S3 browser)
- **29 services**: EC2, S3, RDS, IAM, DynamoDB, Lambda, EMR, ECS, EKS, ELBv2, Secrets Manager, SQS, SNS, CloudWatch, CloudFront, Route53, API Gateway, Step Functions, EventBridge, ElastiCache, EFS, Kinesis, Redshift, KMS, ECR, ACM, CloudFormation, Glue, Athena
- **VPC Explorer**: browse a VPC's subnets, security groups, network interfaces, route tables, gateways, endpoints, NACLs, peering, flow logs, and attached compute/services in a three-pane TUI
- **VPC debugging toolkit** (no AI, deterministic): a findings linter, a connectivity path tracer, plain-English SG/NACL rule explanations, cross-reference ("where used"), merged effective security rules, DNS diagnostics, a public-exposure audit, snapshot diffing, Markdown export, and AWS Reachability Analyzer integration — see [VPC Debugging Toolkit](#vpc-debugging-toolkit)
- **Cost/waste audit**: `aws_explorer audit` scans for the classic sources of silent spend — unattached EBS volumes, idle Elastic IPs and NAT gateways, load balancers with no healthy targets or no traffic, gp2→gp3 candidates, forgotten snapshots/AMIs, over-provisioned DynamoDB tables — each finding with a stable check ID and an estimated monthly cost, printable or explored in an interactive TUI (`--tui`) — see [Audit Usage](#audit-usage)
- **Live bill**: `aws_explorer bill` shows the actual bill from the Cost Explorer API — every service and usage type with its usage quantity, price and a grand total (the Billing console's numbers, not list-price estimates); `--tui` is a live screen that re-fetches on an interval, flags what moved since the last refresh, and drills into per-resource costs (resource ID/ARN) for a service — see [Bill Usage](#bill-usage)
- **IAM debugging**: `aws_explorer iam decode` turns an "Encoded authorization failure message" into a readable verdict, and `aws_explorer iam can <principal> <action> [resource]` simulates IAM policy ("can X do Y on Z?") with the matched statements named and the simulator's blind spots stated — see [IAM Tools](#iam-tools)
- **CloudTrail activity feed**: `aws_explorer trail [resource]` lists recent CloudTrail management events — when, which API call, which principal, from which IP, and whether it failed — scoped to a resource, a principal (`--by`), an API (`--event`), a service (`--source`), or the whole account, with `--errors-only` for failed/denied calls; uses the zero-setup 90-day LookupEvents window; the summary TUI's `t` timeline is the interactive twin — see [Trail Usage](#trail-usage)
- **CloudTrail Lake (SQL)**: `aws_explorer lake` queries a CloudTrail Lake event data store for years of history, data events and aggregations — built-in `--top-principals` / `--top-events` queries or your own `--sql`, with `--tui` to explore results — see [Lake Usage](#lake-usage)
- **Account snapshot diff**: `summary --baseline` / `summary --diff` answers "what changed in this account since yesterday?" — added/removed/modified resources across the whole merged-by-ARN inventory, deterministic and volatile-field-free; `D` in the summary TUI is the interactive twin — see [Account snapshot diff](#account-snapshot-diff--what-changed-since-yesterday)
- **Open in AWS console**: `o` in every TUI (summary, VPC explorer, S3, CloudWatch logs) copies a console deep link for the selection — ARN-aware coverage for all 15 services and every VPC resource type, with an ARN-search fallback for the long tail — and opens it in your browser when the session is local
- **Global fuzzy finder**: `Ctrl+P` in the summary TUI jumps to any resource by name/ID/ARN fragment ("I have `eni-0abc` from an error — what is it?"); `aws_explorer find <fragment>` is the CLI twin — see [Find Usage](#find-usage)
- **SSO-aware errors**: an expired AWS SSO session prints the exact fix (`run: aws sso login --profile prod`) instead of an SDK error chain, in the CLI and every TUI
- **Expiry watchlist**: `aws_explorer expiring` lists everything that breaks on a calendar date — ACM/IAM certificate expiry, Lambda runtime deprecations, EKS end-of-support, RDS CA certs & pending maintenance, overdue secret rotations — sorted by days remaining — see [Expiring Usage](#expiring-usage)
- **ECS stopped-task triage**: `aws_explorer ecs stopped` answers "why did my task stop?" — the task-level stop reason plus the failing container's exit code, glossed in plain English (137 → possible OOM-kill, 139 → segfault) — see [ECS Stopped-Task Triage](#ecs-stopped-task-triage)
- **Where-used / blast radius**: `aws_explorer whereused <arn-or-id>` answers "can I delete this?" for IAM roles, KMS keys, ACM certificates and security groups — every resource that references the target, with the scanned reference types listed so "not referenced" is a scoped answer — see [Whereused (blast radius)](#whereused-blast-radius)
- **Service-quota dashboard**: `aws_explorer quotas` reports the AWS limits that actually cause incidents (vCPUs, EIPs, VPCs, ENIs, Lambda concurrency, RDS, EBS storage…) with real account-specific limits and current usage, sorted closest-to-exhaustion first — see [Quotas (service-quota dashboard)](#quotas-service-quota-dashboard)
- **Config-driven**: YAML configuration for services, regions, filters, output, and per-resource display columns
- **5 auth methods**: auto (SDK default chain), profile, env vars, static credentials, STS AssumeRole
- **Output formats**: Table (default), JSON, NDJSON, CSV — with `--no-header` for scripting and colored states on terminals
- **Filtering**: By region, state, and tags
- **Concurrent**: Bounded goroutine pool (default 8) for parallel collection across services and regions; collectors stream results page-by-page, so the first resources appear after a single API round-trip instead of after the last page
- **Resilient**: Best-effort collection — a throttle, timeout, or denied call mid-scan keeps everything already gathered (flagged as partial) instead of dropping the service/region, with configurable retry attempts and adaptive backoff
- **Themes**: 12 built-in bird-themed color schemes with 24 individually customizable color roles (table header, borders, status bar, alerts, …) — editable live in the in-app settings panel
- **Context-aware shortcuts**: the status bar in every TUI shows only the keys that work on the current screen
- **About every page** (`i`): a short overlay in each TUI explaining what the screen is for, so a newcomer who opens it cold knows what they are looking at before reaching for `?` help
- **Color-coded logs**: the CLI's own stderr logs (`level=INFO`/`WARN`/`ERROR`) are tinted by level on a terminal — WARN/ERROR lines in full color, INFO/DEBUG with just their level token — and so are the CloudWatch Logs viewer and the in-app debug activity overlay; errors jump out at a glance (disabled by [`NO_COLOR`](https://no-color.org/) or when piped)
- **Unique page titles**: every screen names itself in the terminal window/tab title (e.g. `VPC Explorer › my-vpc › Subnets`), so "which page are you on?" has one answer when several people use or discuss the tool
- **Uniform tables**: every table shares one theme and scrolls horizontally (`<` / `>`) when columns don't fit

## Prerequisites

- Go 1.26.1 or later
- AWS credentials configured (see [Authentication](#authentication))

## Quick Start

```bash
# Install
go install github.com/ryandam9/aws_explorer@latest

# …or clone and build
git clone https://github.com/ryandam9/aws_explorer.git
cd aws_explorer
make build          # produces bin/aws_explorer with version info embedded

# Run CLI (streams table to stdout; works from any directory, no config needed)
./bin/aws_explorer

# Explore resources interactively
./bin/aws_explorer summary --tui

# List every resource across all regions (SNO, Name, Type, ARN, Region/AZ)
./bin/aws_explorer summary --all-regions

# Run the VPC Explorer TUI
./bin/aws_explorer vpc --region us-east-1

# Run S3 browser TUI
./bin/aws_explorer s3 --bucket my-bucket --region us-east-1
```

### Shell completion

Tab completion for commands, flags and values (output formats, themes, auth
methods, and the profiles from your `~/.aws/config`) is built in:

```bash
# bash (add to ~/.bashrc)
source <(aws_explorer completion bash)

# zsh (add to ~/.zshrc)
source <(aws_explorer completion zsh)

# fish
aws_explorer completion fish | source
```

## Build

```bash
# Build binary
make build
# or
go build -o bin/aws_explorer main.go

# Format, vet, test, and build
make all

# Run tests
make test

# Tidy modules
make tidy

# Lint (requires golangci-lint)
make lint

# Clean binary
make clean
```

## CLI Usage

The default command streams discovered resources to stdout as a table, JSON,
NDJSON or CSV.

```bash
./bin/aws_explorer [flags]
```

While the scan runs, a live progress meter (`⠿ scanning 12/56 tasks · 340
resources`) is shown on stderr — only when stderr is a terminal, so piping
stdout stays clean. Collection errors are summarized after the run,
deduplicated across regions. Resource states are colored when stdout is a
terminal (disable with [`NO_COLOR`](https://no-color.org/) or by piping).

### Global flags (work on every command)

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | auto-discovered | Path to config file (default search: `./config.yaml`, then the user config dir, then built-in defaults) |
| `--profile` | `default` | AWS named profile |
| `--auth-method` | `auto` | Auth method: `auto`, `profile`, `env`, `static`, `sts` |
| `--role-arn` | — | IAM role ARN to assume (sets auth method to `sts`) |
| `--region` / `-r` | — | Scan only this region (overrides `aws.regions`, `--all-regions` and region filters) |
| `--output` / `-o` | `table` | Output format: `table`, `json`, `ndjson`, `csv` |
| `--no-header` | `false` | Omit the header row in `table`/`csv` output (for scripting) |
| `--all-regions` | `false` | Scan all available AWS regions |
| `--version` | — | Print version, commit and build date |

### Examples

```bash
# Use a named AWS profile
./bin/aws_explorer --profile prod

# Pin to one region
./bin/aws_explorer -r eu-west-1

# Machine-readable output
./bin/aws_explorer -o json | jq '.[].id'
./bin/aws_explorer -o ndjson | head
./bin/aws_explorer -o csv --no-header > resources.csv

# Scan all regions
./bin/aws_explorer --all-regions

# Assume an IAM role
./bin/aws_explorer --role-arn arn:aws:iam::123456789012:role/MyRole

# Custom config file
./bin/aws_explorer --config /path/to/config.yaml
```

## TUI Usage

Interactive terminal UI with sidebar navigation, resource table, and detail
panel. Launch it over your **live** AWS resources with `summary --tui`:

```bash
./bin/aws_explorer summary --tui [flags]
```

Accepts the same global flags as the CLI command (`--config`, `--profile`,
`--auth-method`, `--role-arn`, `--region`, `--all-regions`). To browse a saved
snapshot offline instead, see [Offline snapshot browsing](#offline-snapshot-browsing-snapshot-diff).

### TUI Keyboard Shortcuts

The status bar at the bottom is **context-aware**: it lists only the shortcuts
that are usable on the current screen (and with the current panel focus), so
what you see in the bar is always what works right now.

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate rows |
| `[` / `]` | Move through the service sidebar / scroll the detail panel |
| `Tab` / `Shift+Tab` | Switch focus between sidebar, table and detail panel |
| `<` / `>` (or `,` / `.`) | Scroll table columns when the table is wider than the panel |
| `Enter` | Select service / open the detail panel for the selected resource |
| `/` | Quick text filter (matches any column; shows a live `matched/total` count) |
| `Ctrl+P` | **Jump to any resource**: fuzzy-search every collected resource (name, ID, ARN, type, region) across all services; `Enter` selects its service, lands on its row and opens the detail panel |
| `f` | Advanced filter (region / state) |
| `r` | Reset all filters |
| `s` / `R` | Sort by the next column / reverse the sort direction (`↑`/`↓` shown in the header) |
| `y` / `Y` | Copy the selected resource's ARN / ID to the clipboard |
| `o` / `k` | Open the resource in the AWS console (`o` copies the deep-link URL and opens a browser when the session is local; ARN-search fallback for unmapped types) / copy an AWS CLI reproduction command |
| `J` | Toggle a raw-JSON view in the detail panel (`y` then copies the JSON) |
| `t` / `l` / `g` / `x` | In the detail panel: CloudTrail timeline / inline recent ERROR logs / headline-metric **sparkline** (now·max·min over the last hour) / cross-references |
| `L` | In the detail panel: **open the CloudWatch Logs explorer** pre-filtered to this resource's log group (Lambda → `/aws/lambda/…`, RDS → `/aws/rds/instance/…`, EKS → `/aws/eks/…/cluster`); `q` returns you here with selection and scroll intact |
| `C` | Export the current (filtered, sorted) view to CSV under `~/.aws_explorer/exports/` |
| `D` | **What changed**: first press saves an account baseline snapshot, later presses diff the live inventory against it (`b` inside the overlay re-baselines) |
| `P` | Switch AWS profile and/or region scope, then rescan — no restart needed |
| `e` | Open the scan-errors overlay (services with errors also carry a `⚠n` badge in the sidebar) |
| `~` | **Debug activity overlay**: a live, scrollable view of what the tool is doing — regions, services, API calls and access errors — so you can see progress instead of a blank screen (available during the initial scan too) |
| `S` | Settings panel (themes & colors) |
| `i` | **About this page**: a short overlay explaining what the screen is for (every TUI has one) |
| `?` | Help overlay |
| `Esc` | Close detail panel / overlay |
| `q` / `Ctrl+C` | Quit |

While a scan is running, the header shows real progress (`scanning 23/60` with
the last pending `service@region` tasks named) instead of a generic spinner,
and collection errors are surfaced inline: a red `⚠ n errors` badge in the
header plus per-service warning badges in the sidebar.

### Offline snapshot browsing (`snapshot-diff`)

`snapshot-diff` opens the same interactive TUI over **saved** inventory
snapshots — no AWS credentials, STS calls or region discovery needed. Snapshots
are just the JSON written by `summary -o json`.

```bash
# Browse a single saved snapshot offline
./bin/aws_explorer snapshot-diff --snapshot inventory.json

# Diff two snapshots and explore what was added / removed / modified
./bin/aws_explorer snapshot-diff --diff before.json,after.json
```

It needs one of `--snapshot` or `--diff` (they are mutually exclusive); to
explore **live** resources interactively, use `summary --tui` instead.

## Summary Usage

`summary` produces a single, numbered inventory of **every** discovered resource
across all configured regions, spanning **all AWS services** — not just the ones
with a built-in collector.

It combines two sources and merges them by ARN:

1. **The 29 typed collectors** (EC2, S3, RDS, …) for rich data — state,
   availability zone, and service-specific summary fields.
2. **A universal sweep via the [Resource Groups Tagging API]** (`tag:GetResources`),
   which returns ARNs and tags for taggable resources across hundreds of
   services in each region. This is what gives the long tail (KMS keys, subnets,
   EBS volumes, Step Functions, API Gateways, CloudFront, …) coverage without a
   bespoke collector per service.

When both sources describe the same ARN, the richer typed entry wins. Use
`--typed-only` to skip the universal sweep.

> **Coverage & permissions.** The Tagging API only returns resources that
> support tagging and are registered with the tagging service — broad, but not
> literally 100% of every service. The sweep needs the `tag:GetResources` IAM
> permission; if it's denied, the typed-collector results are still shown.

Under the table (and behind `c` in the `--tui`), summary lists the common
services that produced nothing, as a reminder that an untagged — or simply
absent — resource can be missing. That list is configurable: add your own
services under `summary.commonservices` in `config.yaml` (merged on top of the
built-in list), keyed by the AWS service name with a friendly label:

```yaml
summary:
  commonservices:
    apprunner: App Runner
    sagemaker: SageMaker
  hideservices:        # drop entries (built-in or added) that are just noise
    - glue
    - athena
```

[Resource Groups Tagging API]: https://docs.aws.amazon.com/resourcegroupstagging/latest/APIReference/API_GetResources.html

Each row carries five columns:

| Column | Description |
|--------|-------------|
| `SNO` | Serial number (1-based, assigned after sorting) |
| `NAME` | Resource name (bucket name, EC2 `Name` tag, VPC name, …) or `-` when none |
| `TYPE` | Resource type as `service/type` (e.g. `ec2/instance`, `s3/bucket`) |
| `ARN` | Full ARN — returned by AWS where available, otherwise constructed |
| `REGION/AZ` | Region, plus the availability zone for zonal resources (e.g. `us-east-1 / us-east-1a`) |

```bash
./bin/aws_explorer summary [flags]
```

Accepts the same global flags as the CLI command (`--config`, `--profile`,
`--auth-method`, `--role-arn`, `--region`, `--all-regions`).

### Summary Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output` / `-o` | `table` | Output format: `table`, `json`, `ndjson`, or `csv` |
| `--tui` | `false` | Explore the same inventory interactively instead of printing |
| `--typed-only` | `false` | Skip the all-services Tagging API sweep; use only the built-in typed collectors |
| `--baseline` | `false` | Save this scan as the account's baseline snapshot |
| `--diff` | `false` | Diff this scan against the saved baseline — "what changed since" |

### Examples

```bash
# Table of every resource in every region
./bin/aws_explorer summary --all-regions

# Export the inventory as CSV
./bin/aws_explorer summary --all-regions -o csv > inventory.csv

# As JSON
./bin/aws_explorer summary -o json

# Explore interactively
./bin/aws_explorer summary --tui
```

> Constructing ARNs for resources AWS doesn't return them for (EC2, S3, SQS, …)
> requires the account ID, which is resolved once via `sts:GetCallerIdentity`.
> If that call is denied, those ARNs are shown as `-` while AWS-provided ARNs
> still appear.

### Account snapshot diff — "what changed since yesterday?"

The account-level twin of the VPC explorer's snapshot diff: baseline the
whole merged-by-ARN inventory, then diff a later scan against it.

```bash
aws_explorer summary --baseline          # save the baseline
aws_explorer summary --diff              # later: what changed?
aws_explorer summary --diff -o json      # for automation
```

```
Changes since baseline 2026-06-11 09:00 UTC — 2 added, 1 removed, 1 modified
+ ec2/instance      i-0abc (web-3)        us-east-1
+ lambda/function   new-payments-fn       us-east-1
- s3/bucket         old-logs-bucket       global
~ ec2/instance      i-0def (web-2)        us-east-1   state: running → stopped; tag Env: dev → prod
```

- Baselines are stored under `~/.aws_explorer/account-snapshots/<account-id>/`,
  one file per **region scope** — diffing with a different `-r`/`--all-regions`
  scope than the baseline refuses with a hint instead of reporting everything
  as removed.
- Only stable fields are compared (name, state, tags); volatile detail fields
  are excluded, so an unchanged account always diffs clean and the output is
  deterministic.
- In the summary TUI the same feature lives behind **`D`**: the first press
  saves a baseline, later presses open the "what changed" overlay, and `b`
  inside it re-baselines.

## Audit Usage

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

Glue-category notes:

- The run-based checks (`GLU-JOB-*`, `GLU-COST-*`) read each job's recent run
  history via `glue:GetJobRuns`; a denied call leaves run health unknown and
  fires nothing for that job (under-warn). `GLU-SEC-001` only needs `GetJobs`.
- `GLU-COST-001`'s estimate is the wasted spend across the **observed run
  window** (not a monthly figure), at the same `$0.44`/DPU-hour rate the `glue`
  dashboard uses.
- Uses `glue:{GetJobs,GetJobRuns,GetCrawlers}`.

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

## Expiring Usage

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

## ECS Stopped-Task Triage

`ecs stopped` answers the perennial "why did my task stop?" ticket without
spelunking through the console. For each recently stopped task it prints the
task-level stop reason and the failing container's exit code, with the exit
code translated into plain English.

```bash
./bin/aws_explorer ecs stopped [--cluster <name-or-arn>] [--all-regions] [-o table|json|ndjson|csv]
```

```
SNO  STOPPED AT (UTC)  CLUSTER     TASK    REASON                              CONTAINER  EXIT
1    2026-06-12 01:14  prod        3f9a…   Essential container in task exited  app        137 (possible OOM-kill (137 = 128+9, SIGKILL))
2    2026-06-12 01:10  prod        77b2…   Task failed to start                web        CannotPullContainerError: pull rate limit
```

Exit-code glosses (hedged — the same code can have benign causes):

| Signal | Exit code | Note |
|--------|-----------|------|
| SIGKILL | `137` (128+9) | possible OOM-kill — raise memory or fix the leak |
| SIGSEGV | `139` (128+11) | segfault |
| SIGABRT | `134` (128+6) | likely an `abort()`/assert |
| SIGTERM | `143` (128+15) | stopped by a signal, often a normal shutdown |

A container reason mentioning memory (`OutOfMemoryError`, `memory limit`) is
treated as a stronger OOM signal than the exit code alone.

| Flag | Default | Description |
|------|---------|-------------|
| `--cluster` | _(all)_ | Limit to one cluster (name or ARN); default scans every cluster in scope |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

> **About the retention window.** ECS keeps stopped tasks for roughly one
> hour. An empty report means nothing stopped in that window, not that nothing
> ever fails — run it soon after the failure.

**IAM permissions.** Read-only: `ecs:ListClusters`, `ecs:ListTasks`,
`ecs:DescribeTasks`. Any denial skips that region with a note on stderr and
never aborts the run.

## Quotas (service-quota dashboard)

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

## AWS Glue dashboard

`glue` opens an interactive dashboard for AWS Glue. Tab across **Jobs**,
**Crawlers**, **Triggers**, **Workflows**, **Connections** and the **Catalog**
(databases); each row shows health at a glance — a job's last run state and
duration, a crawler's last-crawl status. Press **Enter** on a job to drill into
its **run history**: state, duration, DPU-hours and an estimated cost per run,
with the error message inline on failures.

```bash
./bin/aws_explorer glue [--region us-east-1 | --all-regions] [--theme <name>]
```

```
 Glue ▸ Jobs (4)  Crawlers (2)  Triggers (3)  Workflows (1)  Connections (2)  Catalog (5)

 NAME                  LAST RUN          STATE         DURATION   WORKER      VERSION
 nightly-orders-etl    2026-06-15 01:14  ✓ SUCCEEDED   12m 22s    G.1X ×10    4.0
 customer-dedupe       2026-06-15 01:14  ✗ FAILED      2m 41s     G.2X ×5     4.0
 clickstream-flatten   2026-06-14 22:00  ● RUNNING     —          G.1X ×20    4.0
```

Run history (Enter on a job):

```
 Runs — nightly-orders-etl [us-east-1]
 STARTED           STATE         DURATION   DPU-HRS  EST     WORKER      ATTEMPT
 2026-06-15 01:14  ✓ SUCCEEDED   12m 22s    2.06     $0.91   G.1X ×10    1
 2026-06-14 01:14  ✗ FAILED      2m 41s     0.45     $0.20   G.1X ×10    1
   ✗ AnalysisException: Table or view not found: orders_raw
                                          3 runs · 4.50 DPU-hrs ≈ $1.99 (estimate)
```

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch pane |
| `↑/↓` (`j/k`) | Move selection |
| `Enter` | Open the selected job's run history |
| `d` | Show the selected job's definition (role, version, worker, script, connections, args — secrets redacted) |
| `/` | Filter the current pane |
| `o` | Open the selected resource in the AWS console |
| `r` | Refresh |
| `y` | (run history) copy the selected run's error |
| `i` | About this page · `q` quit |

The DPU-hour cost is an **estimate** (`$0.44`/DPU-hour, us-east-1 ETL rate);
runs that report no `DPUSeconds` (still running, or legacy jobs) show no figure
rather than `$0.00`.

### Scriptable twins

Every pane has a non-interactive command for pipelines and `jq`:

```bash
aws_explorer glue jobs       [--all-regions] [-o table|json|ndjson|csv]
aws_explorer glue crawlers   [-o …]
aws_explorer glue triggers   [-o …]
aws_explorer glue workflows  [-o …]
aws_explorer glue runs <job> [-r us-east-1] [--limit 20] [--status FAILED] [-o …]
```

```bash
# Which jobs failed their last run?
aws_explorer glue jobs -o json | jq '[.[] | select(.lastRunState=="FAILED") | .name]'

# Failed runs of one job, with cost
aws_explorer glue runs nightly-orders-etl --status FAILED -o json | jq '.[] | {started, estUsd}'
```

The runs JSON exposes machine-readable `durationSeconds`, `dpuHours`, `estUsd`
and ISO-8601 `started`/`completed`. `runs` is region-specific: it uses
`--region` when given, otherwise the first region in scope.

**IAM permissions.** Read-only:
`glue:{GetJobs,GetJob,GetJobRuns,GetCrawlers,GetTriggers,ListWorkflows,GetConnections,GetDatabases}`
and `sts:GetCallerIdentity` (for ARN/console links). Per-region or per-listing
denials degrade that part of the dashboard with a logged note and never abort
the session.

## Bill Usage

`bill` shows the account's actual cost from the AWS Cost Explorer API, grouped
by service and usage type, each line carrying its usage quantity and a grand
total at the bottom — the numbers the Billing console shows, not the
list-price estimates the [audit](#audit-usage) linter attaches to waste
findings. By default it reports the current month to date; today's partial
charges are estimated and flagged as such.

```bash
# Current month to date, grouped by service and usage type
./bin/aws_explorer bill

# A past month, machine-readable
./bin/aws_explorer bill --month 2026-05 -o json

# Live screen, re-fetching every 10 minutes
./bin/aws_explorer bill --tui --interval 10m

# CSV for a spreadsheet
./bin/aws_explorer bill -o csv --no-header > bill.csv
```

```
SNO  SERVICE                  USAGE TYPE                  USAGE     UNIT    COST
1    Amazon EC2               EBS:VolumeUsage.gp3         100       GB-Mo   $8.00
2    Amazon EC2               BoxUsage:t3.micro           744       Hrs     $1.50
3    Amazon S3                TimedStorage-ByteHrs        10        GB-Mo   $0.25
     TOTAL (estimated)        2026-06-01 → 2026-06-13                       $9.75
```

### Live screen (`--tui`)

`--tui` opens a live bill that re-fetches on a fixed interval (`--interval`,
default 5m), so activity that incurs cost surfaces without restarting — this
is the "Live screen" the page is meant to be. A `Δ` column shows what each
line moved since the previous refresh, and the header timestamps the last
update.

| Key | Action |
|-----|--------|
| `↑`/`↓` | Navigate bill lines |
| `Enter` | Detail overlay for the selected line |
| `x` | Per-resource breakdown for the selected service (resource ID/ARN, usage, amount) |
| `u` | Refresh now |
| `/` | Filter by service, usage type or unit |
| `s` / `R` | Sort by the next column / reverse |
| `y` | Copy the selected service and usage type |
| `C` | Export the current view to CSV |
| `?` / `q` | Help / quit |

The per-resource drill-down (`x`) uses Cost Explorer's resource-level data,
which AWS keeps for the trailing **14 days** and only when the account has
opted in (Billing → Cost Management Preferences → "Daily granularity
resource-level data"). Without it, the overlay says so instead of failing.

| Flag | Default | Description |
|------|---------|-------------|
| `--month` | current month | Billing period as `YYYY-MM`; past months cover the full month, the current month clamps to month-to-date |
| `--tui` | off | Open the live screen instead of printing once |
| `--interval` | `5m` | Auto-refresh cadence for `--tui` (minimum 1m) |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

> **Cost note.** Cost Explorer is a paid API — AWS bills every request
> (`GetCostAndUsage`, `GetCostAndUsageWithResources`) at **$0.01**, including
> each automatic refresh in `--tui`. The live screen names the cadence and its
> per-refresh cost in the header; raise `--interval` to spend less. The
> minimum interval is 1 minute because the numbers only move every few
> minutes anyway.

**IAM permissions.** Read-only: `ce:GetCostAndUsage`, plus
`ce:GetCostAndUsageWithResources` for the per-resource drill-down. Cost
Explorer is a global service; the region flags don't affect it.

## IAM Tools

Helpers for the most common AWS support question: *"why am I denied?"*

Two tools: `iam decode` explains a denial you already hit; `iam can` predicts
one before you hit it.

### Decode authorization failure messages

Services like EC2 redact *why* a request was denied into an opaque blob:

```
An error occurred (UnauthorizedOperation): You are not authorized to perform
this operation. Encoded authorization failure message: AQoDYXdzEJr…
```

`iam decode` calls `sts:DecodeAuthorizationMessage` and answers the three
questions that matter — who, what, on which resource — and whether it was an
**explicit deny** (a policy forbids it) or an **implicit deny** (no policy
allows it), which determines the fix:

```bash
# Pass the blob — or paste the entire error message; the blob is extracted
aws_explorer iam decode AQoDYXdzEJr…
pbpaste | aws_explorer iam decode

# Decoded JSON only, for jq
aws_explorer iam decode AQoDYXdzEJr… -o json
```

```
❌ Implicit deny — no policy allows this request (missing allow, not an explicit deny)
  Principal  arn:aws:iam::123456789012:user/bob
  Action     ec2:RunInstances
  Resource   arn:aws:ec2:us-east-1:123456789012:instance/*

  Fix: grant the principal an identity or resource policy that allows the action on the resource.

Full decoded document:
{ … }
```

Requires the `sts:DecodeAuthorizationMessage` IAM permission (a denial tells
you exactly that). The global `--profile`, `--auth-method`, `--role-arn` and
`--region` flags apply.

### Simulate policy — "can X do Y on Z?"

`iam can` runs `iam:SimulatePrincipalPolicy` and renders the verdict in the
path tracer's step-by-step style:

```bash
aws_explorer iam can arn:aws:iam::123456789012:role/app s3:GetObject arn:aws:s3:::my-bucket/key
```

```
❌ Denied: s3:GetObject on arn:aws:s3:::my-bucket/key for role/app — implicit deny (no policy allows it)
  ✗ Identity policies      no attached or inline policy allows this action
    Fix: grant an identity policy that allows s3:GetObject on arn:aws:s3:::my-bucket/key
  ✗ Permissions boundary   the boundary does not include this action — the boundary, not the identity policies, is the blocker

Caveats — the simulator does not evaluate:
  • Resource-based policies (bucket/queue/key/secret policies) — …
```

The three outcomes render distinctly: **allowed** (named the policy that
allows it), **implicit deny** (nothing allows it — add an allow), and
**explicit deny** (a policy forbids it — removing an allow elsewhere will
not help), with permissions-boundary and SCP effects called out when AWS
reports them. The action accepts a comma-separated list to check several at
once; `-o json` emits the verdicts for automation.

The caveats are printed with **every** verdict, because the simulator's
blind spots (resource-based policies, session policies, unsupplied condition
keys) are exactly what makes "but the simulator said allowed!" tickets.

Requires `iam:SimulatePrincipalPolicy`. Read-only — simulation never touches
the real resource.

## Trail Usage

`trail` is a CloudTrail activity feed. It answers both *who changed this
resource, and when?* and *what has been happening in this account?* For each
event it prints when, which API call, which principal (short form —
`role/deploy-pipeline`, `user/alice`, `root`), from which source IP, and
whether the call failed. Events are newest first.

Scope is **one** filter at a time (LookupEvents accepts a single lookup
attribute): a resource, `--by`, `--event`, `--source`, or nothing for the
account-wide feed.

```bash
# Who touched this security group?  (resource-scoped)
aws_explorer trail sg-0abc123

# What has been happening in the account in the last 2 hours?  (account feed)
aws_explorer trail --since 2h

# Everything a principal did
aws_explorer trail --by alice

# Every instance-termination call, in a specific region
aws_explorer trail --event TerminateInstances -r eu-west-1

# Failed / denied calls only — recon & misconfiguration triage
aws_explorer trail --errors-only --since 24h

# ARNs work too — reduced to the resource name CloudTrail records.
aws_explorer trail arn:aws:iam::123456789012:role/app -r us-east-1

# Machine-readable
aws_explorer trail my-bucket -o json | jq '.[0]'
```

```
SNO  TIME                 EVENT                          PRINCIPAL             SOURCE IP     OUTCOME
1    2026-06-11 14:02:11  AuthorizeSecurityGroupIngress  role/deploy-pipeline  203.0.113.7   ok
2    2026-06-09 09:15:42  RunInstances                   user/alice            198.51.100.2  AccessDenied
```

| Flag | Default | Description |
|------|---------|-------------|
| `--by` | — | Only events by this principal (IAM user or role session name) |
| `--event` | — | Only this API call (e.g. `TerminateInstances`) |
| `--source` | — | Only events from this service (e.g. `ec2.amazonaws.com`) |
| `--errors-only` | off | Only failed/denied calls (events carrying an `errorCode`) |
| `--since` | full window | Only events after this long ago (`7d`, `36h`, or a plain day count) |
| `--limit` | `50` | Maximum number of events to print (`--tui` defaults to 200) |
| `--read-events` | off | Include read-only (`Describe*`/`List*`/`Get*`) events, marked `(read)` in the table |
| `--tui` | off | Explore the feed interactively (filter, sort, failed-only toggle, per-event detail) |

Notes:

- Uses `cloudtrail:LookupEvents`, which covers the **last 90 days** of
  management events with **no trail or S3 bucket setup required** — that one
  permission is all it needs. (Distinct from the `audit --only cloudtrail`
  category, which inspects trail *configuration*.)
- A resource, `--by`, `--event` and `--source` are **mutually exclusive** —
  the API matches a single attribute per query.
- CloudTrail records events in the region where the activity happened; pick it
  with `-r` (default: the first configured region). **`--all-regions`** fans the
  lookup out across regions (queried in parallel) and merges them newest-first.
  Global services such as IAM and CloudFront record in `us-east-1`.
- By default only **mutating** events are shown — the `Describe*` noise would
  drown out the changes you're looking for.
- The API is rate-limited (2 TPS); pages are fetched serially and capped. The
  account-wide feed scans deeper than a pivoted lookup, but on a busy account
  its newest events can still be all read-only — if you see nothing, **pivot**
  (`--event`/`--source`/`--by`) so CloudTrail filters server-side, add
  `--read-events`, narrow with `--since`, or use **`lake`** for older history.
  The tool says when a result was truncated and suggests these levers.

Add `--tui` to explore the feed interactively: quick filter, column sort, a
**failed-only toggle** (`x`), and a per-event detail overlay — the same
interaction language as the other TUIs. The scope (resource / `--by` /
`--event` / `--source` / account-wide) and region are set by the flags above;
the TUI then makes that feed navigable.

The resource-scoped view also lives in the summary TUI: press **`t`** on a
resource's detail panel for its CloudTrail timeline (failed calls flagged in
red).

## Lake Usage

`lake` queries a **CloudTrail Lake event data store** with SQL. Where `trail`
uses `cloudtrail:LookupEvents` (90 days, management events only), a Lake store
can hold **years** of history and **data events** (S3 object access, Lambda
invokes, …) and supports **aggregation** — at the cost of having to create a
store first.

```bash
# What stores can I query?
aws_explorer lake --list-stores

# Recent activity in the last 30 days
aws_explorer lake --since 30d

# Who has been the busiest principal this quarter?
aws_explorer lake --top-principals --since 90d

# Most frequent API calls, explored interactively
aws_explorer lake --top-events --tui

# Your own SQL (you supply the FROM clause / event-data-store id)
aws_explorer lake --sql "SELECT eventName, COUNT(*) c FROM <eds-id> GROUP BY eventName ORDER BY c DESC LIMIT 20"
```

```
NAME         ID         ARN
audit-store  abcd-1234  arn:aws:cloudtrail:us-east-1:123456789012:eventdatastore/abcd-1234
```

| Flag | Default | Description |
|------|---------|-------------|
| `--list-stores` | off | List available event data stores and exit |
| `--store` | the only store | Event data store to query (id, ARN, or name) |
| `--top-principals` | off | Built-in query: principals ranked by event count |
| `--top-events` | off | Built-in query: API calls ranked by frequency |
| `--sql` | — | Raw CloudTrail Lake SQL (you supply the `FROM`) |
| `--by` / `--event` / `--source` | — | Narrow the built-in queries (principal substring / API / service) |
| `--errors-only` | off | Narrow the built-in queries to failed/denied calls |
| `--since` | full store | Only events after this long ago (`30d`, `12h`) |
| `--limit` | `50` | Maximum number of rows |
| `--max-wait` | `60s` | How long to wait for the query to finish |
| `--tui` | off | Explore the results interactively (filter, numeric-aware sort, detail, CSV) |

Notes:

- Needs `cloudtrail:{ListEventDataStores,StartQuery,GetQueryResults}` (and
  `DescribeQuery` for failure detail). The query runs server-side; the tool
  polls `GetQueryResults` until it finishes or `--max-wait` elapses.
- **If no event data store exists**, the command prints a short note and exits
  cleanly — use `aws_explorer trail` for the zero-setup 90-day feed.
- CloudTrail Lake is regional; a multi-region store is queried from its home
  region. Pick the region with `-r`.

## Find Usage

`find` answers "what is this thing?" for a mystery identifier — an ENI from
an error message, half a resource name from a ticket. It scans the configured
regions (typed collectors **plus** the all-services Tagging API sweep, so the
long tail of services is covered) and fuzzy-matches every resource against
the fragment; best matches print first.

The match is an in-order subsequence, so separators can be skipped:
`eni0abc` finds `eni-0abc12`, `prodweb` finds `prod-web-3`. Exact substrings,
word-start hits and shorter names rank higher.

```bash
# What is this ENI?
aws_explorer find eni-0abc

# Find by name fragment across every region
aws_explorer find prodweb --all-regions

# Machine-readable
aws_explorer find payments -o json | jq '.[0].arn'
```

```
SNO  NAME        TYPE           ID                     REGION      ARN
1    prod-web-3  ec2/instance   i-0abc12def34567890    us-east-1   arn:aws:ec2:us-east-1:…
2    prod-web    elbv2/loadb…   arn:aws:elasticloadb…  us-east-1   arn:aws:elasticloadb…
```

| Flag | Default | Description |
|------|---------|-------------|
| `--limit` | `25` | Maximum number of matches to print |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` — always in best-match order |

The same search lives in the summary TUI behind **`Ctrl+P`**: a palette
fuzzy-matches as you type, `↑`/`↓` select, and `Enter` jumps straight to the
resource — its service selected in the sidebar, its row under the cursor, and
the detail panel open. Any active filters that would hide the target are
cleared.

## Whereused (blast radius)

`whereused` answers "can I delete this?" for the resources people actually ask
about — IAM roles, KMS keys, ACM certificates and security groups. It scans
the account for the linking fields the inventory does not keep (a Lambda's
execution role, a volume's KMS key, a listener's certificate, an ENI's
security groups) and lists every resource that references the target. It is
the CLI generalization of the VPC explorer's `x` cross-reference.

```bash
./bin/aws_explorer whereused <arn-or-id> [--all-regions] [-o table|json|ndjson|csv]
```

```
Where-used: app-task (iam-role)

SNO  SERVICE  TYPE             RESOURCE           REGION      VIA
1    ecs      task-definition  checkout:7         us-east-1   ECS task role
2    lambda   function         payments           us-east-1   execution role

Reference types checked: Lambda execution roles, EC2 instance profiles, ECS task and execution roles, EKS cluster and node-group roles, IAM role trust policies.
(Absence above means none of these reference it — not that nothing anywhere does.)
```

Accepted targets (full ARN or bare ID):

| Target | Reference types checked |
|--------|-------------------------|
| **IAM role** (`arn:…:role/app` or `app`) | Lambda execution roles, EC2 instance profiles, ECS task & execution roles, EKS cluster & node-group roles, IAM role trust policies |
| **KMS key** (`arn:…:key/<uuid>`) | EBS volume / RDS instance / Secrets Manager / SQS queue / Lambda environment encryption |
| **ACM certificate** (`arn:…:certificate/<id>`) | ELBv2 (ALB/NLB) listeners |
| **Security group** (`sg-…` or its ARN) | Elastic network interface attachments (account-wide) |

> **Scoped "not referenced".** The report always prints the reference types it
> checked. Absence of evidence is therefore explicitly bounded — it means none
> of *those* reference types point at the target, not that nothing anywhere
> does. A denied or failed API call narrows what was checked (reported on
> stderr) and never aborts the run. KMS keys referenced by alias are matched on
> the raw alias string rather than resolved to the key.

| Flag | Default | Description |
|------|---------|-------------|
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

**IAM permissions.** Read-only: `iam:{ListRoles,ListInstanceProfiles}`,
`lambda:ListFunctions`, `ec2:{DescribeInstances,DescribeVolumes,DescribeNetworkInterfaces}`,
`rds:DescribeDBInstances`, `secretsmanager:ListSecrets`,
`sqs:{ListQueues,GetQueueAttributes}`, `ecs:{ListTaskDefinitions,DescribeTaskDefinition}`,
`eks:{ListClusters,DescribeCluster,ListNodegroups,DescribeNodegroup}`,
`elasticloadbalancing:{DescribeLoadBalancers,DescribeListeners}`. Any denial
skips that source with a note.

## VPC Explorer TUI Usage

An interactive, three-pane TUI for drilling into a single VPC's networking and
attached resources. Pick a VPC on the left, a resource category in the middle,
and browse the matching resources on the right.

```bash
./bin/aws_explorer vpc [flags]
```

If `--region` is omitted, all regions are scanned for VPCs.

### VPC Flags

The global `--profile`, `--auth-method`, `--role-arn`, `--region` and
`--all-regions` flags apply. With no region flags, all regions are scanned.

| Flag | Default | Description |
|------|---------|-------------|
| `--theme` | `spotted-pardalote` | Color theme |

### Layout

```
┌─ VPCs ──────┬─ Resources ─────┬─ Subnets ─────────────────────────────┐
│ vpc-0a1b... │ ▸ NETWORK       │  #  Name   CIDR          AZ    Public  │
│ vpc-2c3d... │   Subnets       │  1  -      172.31.0.0/20 ...   Yes     │
│ my-vpc      │   Security Grps │  2  -      172.31.16.0/20 ...  Yes     │
│ default     │   Route Tables  │                                       │
│             │ ▸ COMPUTE       │                                       │
│             │   EC2 Instances │                                       │
│             │ ▸ SERVICES      │                                       │
└─────────────┴─────────────────┴───────────────────────────────────────┘
```

### Resource categories

The middle pane groups the resource types a VPC contains. Selecting one (with
`Enter`) loads it into the right-hand table.

- **NETWORK** — Subnets, Security Groups, **Network Interfaces** (ENIs), Route Tables, Internet Gateways, NAT Gateways, VPC Endpoints, Network ACLs, Peering, Flow Logs
- **COMPUTE** — EC2 Instances, Lambda Functions
- **SERVICES** — RDS Instances, Load Balancers

Each table shows a default set of columns; the full attribute set (plus tags,
rule lists, etc.) appears in the **detail overlay** when you press `Enter` on a
row. Which columns and detail fields are shown can be overridden per resource
type in `config.yaml` — see [Customizing displayed columns](#customizing-displayed-columns).

### Keyboard shortcuts

**Navigation**

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Move within the VPC list, category sidebar, or resource table |
| `Enter` | Open a VPC · load a category · open the selected row's detail overlay |
| `Tab` | Switch focus between the category sidebar and the resource table |
| `<` / `>` (or `,` / `.`) | Scroll table columns left/right when a table is wider than the panel |
| `/` | Filter the VPC list by name or ID · quick-filter the resource table (matches any column, live `matched/total` count; `Enter` keeps the filter, `Esc` clears it) |
| `s` / `R` | Sort the resource table by the next column / reverse the direction |
| `c` / `y` | Copy the selected resource's ID to the clipboard |
| `o` | Open the selected resource in the AWS console — copies the deep-link URL, opens a browser when local |
| `C` | Export the current resource table to CSV under `~/.aws_explorer/exports/` |
| `r` | Refresh the VPC list or the current resource list |
| `Esc` | Go back one level (overlay → table → sidebar → VPC list) |
| `S` | Open the settings panel (themes & colors) |
| `i` | About this page — a short overlay explaining what the VPC Explorer does |
| `?` | Toggle the help overlay |
| `q` / `Ctrl+C` | Quit |

**Debugging toolkit** (available in the resource browser)

| Key | Action | Cost |
|-----|--------|------|
| `F` | **Findings** — run the VPC linter and list ranked issues | free |
| `t` | **Trace** — connectivity path tracer from the selected network interface | free |
| `x` | **Where used** — cross-reference the selected resource | free |
| `e` | **Effective rules** — merged security-group rules for the selected ENI | free |
| `D` | **DNS** — the VPC's DNS resolution / hostnames / DHCP options | free |
| `P` | **Public exposure** — everything reachable from the internet | free |
| `w` | **What changed** — baseline the VPC, then diff against it later | free |
| `E` | **Export** — write a Markdown report of resources + findings | free |
| `A` | **Reachability Analyzer** — list AWS Network Insights analyses; create new ones | listing free; creating ~$0.10/analysis |
| `L` | **Logs** — jump to the CloudWatch Logs explorer for the selected Lambda function or RDS instance | free |

Inside any overlay, `↑` / `↓` scroll and `Esc` (or the same trigger key) closes it.

### Horizontal column scrolling

Wide tables (e.g. Security Groups, with ~106 columns of data) don't truncate or
drop columns on narrow terminals. The leading identifier columns stay **pinned**
while the remaining columns scroll with `<` / `>`; a `◀ N more cols ▶` indicator
shows when columns are hidden off either edge, and the status bar advertises
`</>` only while there is something to scroll to. This works the same in every
table of the application — the summary TUI, the S3 browser and the VPC
explorer.

---

## VPC Debugging Toolkit

The VPC Explorer is built for the questions a cloud/support engineer actually
asks. Every analysis below is **deterministic** — computed locally from the
resources AWS returns, with no AI — and the one feature that can change anything
in AWS (`A`) is read-only by default and asks for confirmation before any paid
call. Most overlays fetch a one-shot *snapshot* of the VPC's networking
(subnets, security groups, ENIs, route tables, gateways, NACLs, peerings,
endpoints) and reason over it.

### Plain-English rule explanations

Opening the detail overlay (`Enter`) for a **Security Group** or **Network ACL**
adds an "In plain English" section that translates each rule into a readable
sentence:

```
  In plain English:
  • Allow inbound HTTPS (TCP 443) from anywhere on the internet (0.0.0.0/0)
  • Allow inbound SSH (TCP 22) from anywhere on the internet (0.0.0.0/0)  ⚠ remote admin access open to the entire internet
  • Allow inbound MySQL/Aurora (TCP 3306) from resources in security group sg-0abc123
```

- **Ports** are named from a table of ~60 well-known services (22→SSH, 443→HTTPS, 3306→MySQL/Aurora, 5432→PostgreSQL, 6379→Redis, 3389→RDP, …).
- **Sources/destinations** are classified: public (`0.0.0.0/0`), IPv6 (`::/0`), single host (`/32`), RFC1918 private networks, security-group references (`sg-…`), and prefix lists (`pl-…`).
- **Risk flags (`⚠`)** are added only for genuinely dangerous exposure to the public internet — remote-admin ports (SSH/RDP/VNC/Telnet), database/cache ports, all-ports/all-traffic, or a port range spanning sensitive ports. Ordinary public web ports (HTTP/HTTPS) are intentionally **not** flagged, to avoid alert fatigue.
- **NACL** explanations additionally show the rule number and allow/deny action, label the catch-all as `Rule * (default)`, and note that NACLs are **stateless** and evaluated in ascending rule-number order (first match wins).

### Findings linter (`F`)

Scans the whole VPC and opens a scrollable table of issues sorted most-severe
first — severity (`🔴 critical`, `🟡 warning`, `🔵 info`), the impacted
resource, the issue and why it fired, and the suggested fix:

```
VPC Findings — 1 critical, 2 warning, 0 info

SEVERITY     RESOURCE     ISSUE                                  FIX
─────────────────────────────────────────────────────────────────────────────────────
🔴 CRITICAL  sg-0a1       Security group exposes a sensitive     Restrict the source to
                          port to the internet                   specific CIDRs or a
                          sg-0a1 (default): Allow inbound SSH    security group instead
                          (TCP 22) from anywhere on the          of 0.0.0.0/0.
                          internet (0.0.0.0/0)
```

The checks:

| Area | Finding | Severity |
|------|---------|----------|
| Security groups | Sensitive port (admin/database/all) open **inbound** to `0.0.0.0/0` — ranges covering a sensitive port rank the same as the port itself | 🔴 critical |
| Security groups | Rule references a security group not in this VPC | 🔵 info |
| Route tables | Blackhole route (target deleted) | 🟡 warning |
| Subnets | Low available IPs / >90% utilization | 🟡 warning |
| Subnets | Auto-assign public IP but no IPv4 internet-gateway route | 🟡 warning |
| Subnets | No outbound internet path (no IGW/NAT/eigw/TGW/peering/NAT-instance default route) | 🔵 info |
| NAT gateways | Available but unreferenced by any route (idle, still billing) | 🟡 warning |
| Internet gateways | Detached from the VPC | 🔵 info |
| Network ACLs | Stateless return-traffic gap (ephemeral ports not allowed back) | 🟡 warning |
| Peering | Overlapping CIDRs (all CIDR blocks, including secondaries) · not active | 🟡 / 🔵 |
| VPC endpoints | Gateway endpoint with no route-table association | 🟡 warning |
| VPC endpoints | Interface endpoint SGs don't allow inbound 443 · private DNS off | 🟡 / 🔵 |
| **Capacity** | Rules per SG (limit 60), routes per route table (50), rules per NACL (20), SGs per ENI (5), subnets per VPC (200) | 🟡 ≥80%, 🔴 at limit |
| **Orphans** | Security group attached to nothing & unreferenced · empty subnet | 🔵 info |

The NACL stateless check evaluates rules in rule-number order with
first-match-wins (a broad deny shadows later allows, exactly like AWS), is
careful to *not* flag the correct "inbound 443 + outbound ephemeral" pattern,
and also covers the default NACL — its rules are editable, so a hardened
default NACL is linted like any other. Capacity limits are AWS defaults
(adjustable via Service Quotas; account-specific increases are not reflected).
Orphan checks are skipped if ENI data is unavailable.

### Connectivity path tracer (`t`)

The "can't connect" doctor. From a selected **Network Interface**, press `t` and
enter a destination as `IP[:port]` (or `internet:443`). It walks the path the
way AWS evaluates it and reports the **first hop that blocks** the connection:

```
❌ Blocked at: Destination security group ingress

• Source                              eni-web (10.0.0.10) in subnet subnet-pub
✓ Security group egress               sg-web allows all traffic
✓ Source NACL egress                  acl-default rule 100 allows it
✓ Route table                         10.0.0.0/16 → local (local)
✓ Destination NACL ingress            acl-default rule 100 allows it
✗ Destination security group ingress  no ingress rule on sg-db allows TCP 5432 from 10.0.0.10
```

It evaluates, in order: source security-group **egress** (stateful) → source
NACL **egress** (stateless, ordered, first-match-wins) → **route-table**
longest-prefix lookup (local / IGW / NAT / blackhole) → for in-VPC
destinations, the destination NACL **ingress** and security-group **ingress**
(resolving `sg-` references against the peer ENI) → and the **stateless return
path** (ephemeral ports 1024–65535). Internet via an internet gateway requires
the source to hold a public IP/EIP; via a NAT gateway it's treated as private
egress — and both internet paths also verify the source NACL lets the
**stateless replies** back in on ephemeral ports. A NAT gateway that is not in
the `available` state blocks the path. Traffic between two interfaces in the
**same subnet** correctly skips the NACL hops (NACLs apply only at the subnet
boundary), and destination IPs are matched against ENIs' **secondary private
IPs** as well as primaries.

Known limitations: IPv4 only (IPv6 routes and `::/0` rules are not evaluated),
and managed prefix lists (`pl-…`) in rules or routes cannot be expanded — the
trace flags a caveat when one is present, since the verdict may be incomplete.
Paths into peered VPCs or transit gateways are evaluated up to the gateway and
reported as "open up to" that target.

### Cross-reference — "where used" (`x`)

`x` shows everything that references the selected resource and what it
references, turning the flat tables into a navigable graph. It works on
**security groups, subnets, route tables, network interfaces, NAT gateways,
internet gateways, network ACLs, VPC endpoints, and peering connections** —
the `x` hint appears in the status bar only on those categories, and pressing
it elsewhere says so explicitly instead of showing an empty result:

```
Where used: subnet-priv
Route table  (1)                    • rtb-priv
Network ACL  (1)                    • acl-priv
Network interfaces in subnet  (1)   • eni-b
```

Covered: **security groups** (attached ENIs + their instances, and other SGs
referencing them), **subnets** (route table & NACL — including the implicit
main/default when unassociated — plus ENIs and NAT gateways), **route tables**
(associated subnets and non-local targets), **network interfaces**
(instance/subnet/SGs), **NAT & internet gateways** (route tables routing to
them), and **network ACLs** (associated subnets).

### Effective security rules (`e`)

An ENI can carry several security groups, and AWS evaluates the **union** of
their rules. On a Network Interface, `e` shows the merged, de-duplicated
inbound/outbound rules in plain English, annotated with the contributing
group(s):

```
Effective rules: eni-app
Security groups: sg-a, sg-b

Inbound  (3)
  • Allow inbound HTTPS (TCP 443) from anywhere on the internet (0.0.0.0/0)
      via sg-a, sg-b          ← identical rule in both groups, collapsed
Network ACL acl-1 also applies to this subnet (stateless, evaluated separately).
```

### DNS & VPC attributes (`D`)

For the "DNS doesn't resolve in my VPC" case. Shows the attributes plus
diagnostic notes:

```
DNS resolution        Enabled
DNS hostnames         Disabled
DHCP options set      dopt-abc
Domain name servers   10.0.0.2, 8.8.8.8
Domain name           corp.internal

Notes
  🟡 DNS hostnames disabled — interface VPC endpoints' private DNS will not resolve.
  • Custom DNS servers bypass the Amazon Route 53 Resolver; private hosted zones /
    endpoint private DNS may not resolve unless those servers forward to it.
```

`enableDnsSupport` off is flagged critical, `enableDnsHostnames` off is a
warning, and custom DHCP DNS servers are noted as info.

### Public exposure (`P`)

A one-screen audit of the VPC's internet-facing surface:

```
Public exposure — internet-facing surface
⚠ Internet-reachable interfaces (public IP + IGW route + open security group)
                                                                 (1)  • eni-pub (52.1.1.1) → i-web — HTTPS (TCP 443)
Public subnets (route to an internet gateway)                    (1)  • subnet-pub
Security groups open to the internet (inbound from 0.0.0.0/0)    (1)  • sg-web (web) — HTTPS (TCP 443)
Network interfaces with a public IP                              (1)  • eni-pub (52.1.1.1) → i-web
```

The first group **correlates** the three ingredients of real exposure — an ENI
holding a public IP, in a subnet routing to an internet gateway, with a
security group open to the internet — and lists the ports actually reachable,
so a permissive-but-unrouted security group doesn't read as an incident. The
remaining groups list each ingredient on its own: public subnets (IPv4 or IPv6
default route to an IGW), SGs with their internet-open ports in plain English
(SG-to-SG references excluded), and ENIs holding a public IP/EIP.

### Snapshot diff — "what changed" (`w`)

For "it worked yesterday". The first `w` on a VPC saves a baseline snapshot;
later, `w` diffs the live VPC against it and shows exactly what changed —
added/removed resources and, for resources that still exist, the specific facts
(rules, routes, attributes) that were added or removed:

```
Changes since baseline — 1 added, 1 removed, 1 modified
+ Security group sg-new
- Security group sg-old
~ Security group sg-web
    by role/deploy-pipeline — AuthorizeSecurityGroupIngress, 2026-06-11 14:02 UTC
    + inbound|tcp|22|10.0.0.0/8
```

Inside the overlay, **`t` attributes each change to its likely actor**: the
most recent CloudTrail mutation event for every changed resource (when, which
API call, which principal), via the zero-setup 90-day `LookupEvents` window —
the same source as `aws_explorer trail`. Lookups run serially (the API allows
2/s) and are capped at the first 15 changed resources; a denied
`cloudtrail:LookupEvents` degrades to a one-line note. The actor shown is the
*latest* to touch the resource — the likely, not guaranteed, author of the
diff.

Baselines are stored as JSON in `~/.aws_explorer/vpc-snapshots/<vpc-id>.json`.
Inside the overlay, `b` re-baselines to the current state. Volatile fields (like
available-IP counts) are deliberately excluded so they don't create noise.
Tracked facts include SG rules, routes and route-table associations, **NACL
rules and subnet associations**, subnet attributes, NAT gateway state/subnet/
**public IP**, IGW state, peering status, and endpoint state/private-DNS/route
tables/**security groups/subnets** — covering the classic silent breakers like
a NACL re-association or an endpoint SG swap.

### Markdown export (`E`)

Writes a self-contained Markdown report — a resource-count summary, all findings
grouped by severity with fixes, and inventory tables (subnets, security groups,
route tables, NAT gateways, endpoints, network interfaces) — to
`~/.aws_explorer/exports/<vpc-id>-<timestamp>.md`. Ideal for pasting into a
support case or runbook. The status bar shows the path.

### AWS Reachability Analyzer (`A`)

Integrates the authoritative AWS [Reachability Analyzer](https://docs.aws.amazon.com/vpc/latest/reachability/what-is-reachability-analyzer.html).
**Read-only by default** — `A` lists the Network Insights analyses that already
exist in the account, each as `source → destination:port` with a
`reachable` / `not reachable` / `running` / `failed` verdict:

```
Reachability Analyzer
✓ eni-web → eni-db:3306 (tcp)  [reachable]       2026-06-09 10:00
✗ eni-web → igw-1 (tcp)  [not reachable]         2026-06-09 11:30
```

Creating a new analysis is **opt-in**: press `n`, enter
`source -> destination[:port]` (prefilled with the selected network interface),
then confirm a prompt that **states the cost** before anything is created:

```
⚠ This creates AWS resources and incurs a per-analysis charge (~$0.10).
  eni-web → eni-db:3306
y = create and run  •  n/Esc = cancel
```

On confirmation it creates a Network Insights path, starts the analysis, polls
until it completes (up to ~2 minutes), and prepends the result. This is the only
VPC Explorer feature that mutates AWS or incurs a charge.

> **Files written by the toolkit.** Snapshots: `~/.aws_explorer/vpc-snapshots/`.
> Exports: `~/.aws_explorer/exports/`. Both directories are created on demand.
> All other features are purely in-memory.

## S3 TUI Usage

A dedicated S3 browser with bucket listing, object navigation, metadata viewing, and optional delete support.

```bash
./bin/aws_explorer s3 [flags]
```

### S3 Flags

The global `--profile`, `--auth-method`, `--role-arn` and `--region` flags
apply.

| Flag | Default | Description |
|------|---------|-------------|
| `--bucket` | — | Bucket name to open directly |
| `--prefix` | — | Key prefix to start browsing from |
| `--theme` | `spotted-pardalote` | UI theme name |
| `--allow-delete` | `false` | Enable object deletion |
| `--endpoint-url` | — | Custom endpoint (LocalStack, MinIO, etc.) |

Press `o` anywhere in the browser to open the selection in the AWS console —
the current bucket on the bucket list, the bucket + prefix or the selected
object in the object list. The URL is copied to the clipboard, and a browser
opens when the session is local.

### S3 Examples

```bash
# Browse all buckets
./bin/aws_explorer s3

# Open a specific bucket
./bin/aws_explorer s3 --bucket my-bucket --region us-east-1

# Browse with a prefix filter
./bin/aws_explorer s3 --bucket my-bucket --prefix logs/2024/

# Use a different theme
./bin/aws_explorer s3 --theme oriole

# Enable deletion (use with caution)
./bin/aws_explorer s3 --bucket my-bucket --allow-delete

# Point to a local MinIO or LocalStack instance
./bin/aws_explorer s3 --endpoint-url http://localhost:9000
```

## CloudWatch Logs TUI Usage

An interactive explorer for CloudWatch log groups, streams and events, with
filtering, search and live tailing.

```bash
./bin/aws_explorer cw [flags]
```

The global `--profile`, `--auth-method`, `--role-arn`, `--region` and
`--all-regions` flags apply: `--region` pins a single region, `--all-regions`
sweeps every enabled region and adds a Region column to the group list, and
otherwise the config's `aws.regions` list is used.

| Flag | Default | Description |
|------|---------|-------------|
| `--group` / `-g` | — | Initial log group filter/pattern |
| `--stream` / `-s` | — | Initial log stream filter |
| `--filter` / `-f` | — | Initial query pattern for log events |
| `--theme` | `spotted-pardalote` | UI theme name |

```bash
# Browse log groups in one region
./bin/aws_explorer cw --region us-east-1

# Open a group and search for errors
./bin/aws_explorer cw -g /aws/lambda/my-fn -f ERROR
```

Press `o` on a log group to open it in the CloudWatch console (URL copied;
browser opened when the session is local).

### Full log viewer

Pressing `Enter` on a log event opens the **full log viewer**: a full-screen
page with the entire log (24-hour lookback, most recent 2000 events) for the
selected stream — or the whole group in group-level search — that streams new
events live as they arrive. Each line is tinted by severity (error/fail/panic
in red, warnings amber, info/notice in the info color, debug/trace muted) so
errors stand out while you scroll.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `PgUp`/`PgDn`, `Ctrl+U`/`Ctrl+D` | Scroll (scrolling up pauses tailing) |
| `g` / `G` | Jump to top / jump to bottom and resume tailing |
| `f` | Toggle follow (auto-scroll as new events stream in) |
| `J` | Toggle JSON formatting: pretty-prints JSON objects/arrays embedded in log messages (a `{} json` badge shows while on) |
| `/` | Search within the log (case-insensitive, matches highlighted; search works on the formatted lines when `J` is on) |
| `&` | Grep filter (as in `less`): enter a regex and only matching lines are rendered, with a `kept/total` count; `Enter` keeps the filter, `Esc` clears it. Invalid patterns are flagged while the last valid filter stays applied |
| `n` / `N` | Jump to next / previous match |
| `y` | Copy the entire log to the clipboard — or only the matching lines while a grep filter is applied |
| `s` | Export the log to `~/.aws_explorer/logs/` — or only the matching lines (file suffixed `-grep`) while a filter is applied |
| `Esc` / `q` | Close the viewer |

## Makefile Targets

```bash
make build           # Build binary to bin/aws_explorer
make run             # Build and run CLI mode
make run-all-regions # Build and run with --all-regions
make test            # Run all tests
make fmt             # Format source code (go fmt)
make vet             # Run go vet
make lint            # Run golangci-lint (skipped if not installed)
make tidy            # Tidy go modules
make clean           # Remove binary
make docs            # Generate Markdown + HTML docs into docs/site
make man             # Generate man pages into ./man
make all             # fmt + vet + test + build
make help            # Show available targets
```

## Documentation site

This README is the hand-written tour. There is also a **generated documentation
site** under [`docs/site/`](docs/site/) — start at
[`docs/site/README.md`](docs/site/README.md), or open `docs/site/index.html` in
a browser for the same content with a navigation sidebar.

It is produced by the tool itself, so it never drifts from the binary:

```bash
aws_explorer docs --format html --dir docs/site       # browsable HTML site
aws_explorer docs --format markdown --dir docs/site   # Markdown (renders on GitHub)
aws_explorer docs --format man --dir man              # troff man pages
make docs                                             # Markdown + HTML in one step
```

The site has two parts: a **command reference** generated from the live command
tree (every command, flag and example, regenerated as features land) and
hand-written **guides** for the interactive TUIs — their screens and complete
keyboard-shortcut tables, which a command tree can't describe. Regenerate it
whenever commands change.

## Configuration

Configuration is **optional** — the binary embeds the default config and runs
from any directory with zero setup. When a `config.yaml` exists it is
discovered in this order:

1. The `--config` flag
2. `./config.yaml` in the current directory
3. The user config directory (`~/.config/aws_explorer/config.yaml` on Linux,
   `~/Library/Application Support/aws_explorer/config.yaml` on macOS)
4. The built-in defaults embedded in the binary

CLI flags override config file values at runtime.

```bash
# Scaffold a starter config in the current directory
aws_explorer config init

# Scaffold the per-user config used from any directory
aws_explorer config init --path ~/.config/aws_explorer/config.yaml

# Show which config file is active
aws_explorer config path
```

Theme edits saved from the in-app settings panel (`Ctrl+S`) are written to the
active config file; when running on built-in defaults the file is created in
the user config directory on first save.

### Full Configuration Reference

```yaml
app:
  defaultOutput: table        # table | json
  defaultMode: cli            # cli | tui
  timeoutSeconds: 30          # per-collector timeout
  maxConcurrency: 8           # max parallel collectors
  downloadDir: ""             # S3 browser download target ("D"); ~ expands to home,
                              # empty = current dir; created automatically if missing

aws:
  # Auth method: auto | profile | env | static | sts
  authMethod: auto
  profile: default

  # STS AssumeRole (used when authMethod: sts)
  sts:
    roleArn: ""               # required: arn:aws:iam::123456789012:role/MyRole
    sessionName: ""           # optional; defaults to "aws-explorer"
    externalId: ""            # optional; for cross-account trust policies
    mfaSerial: ""             # optional; ARN of your MFA device
    durationSeconds: 0        # optional; 0 = AWS default (3600s)

  # Static credentials (used when authMethod: static)
  static:
    accessKeyId: ""
    secretAccessKey: ""
    sessionToken: ""          # optional; for temporary credentials

  # Retry tuning for every AWS API call (applies to all auth methods)
  retry:
    maxAttempts: 0            # total attempts per call (1 = no retries); 0 = SDK default (3)
    mode: ""                  # standard (default) | adaptive (adds client-side
                              # rate limiting; best for accounts that hit throttling)

  allRegions: false           # true = query all available regions
  regions:
    - us-east-1

services:
  ec2:           { enabled: true }
  s3:            { enabled: true }
  rds:           { enabled: true }
  iam:           { enabled: true }
  dynamodb:      { enabled: true }
  lambda:        { enabled: true }
  emr:           { enabled: true }
  ecs:           { enabled: true }
  eks:           { enabled: true }
  elbv2:         { enabled: true }
  secretsmanager: { enabled: true }
  sqs:           { enabled: true }
  sns:           { enabled: true }
  cloudwatch:    { enabled: true }
  cloudfront:    { enabled: true }
  route53:       { enabled: true }

filters:
  regions: []                 # restrict to these regions (empty = use aws.regions)
  tags: {}                    # key: value tag filters
  states: []                  # filter by resource state (e.g. running, stopped)

output:
  format: table               # table | json
  includeDetails: false       # include extended resource details

ui:
  theme: spotted-pardalote    # active theme name (see themes below)
```

### Customizing displayed columns

The VPC Explorer ships sensible default columns for each resource type, but you
can override which fields appear as table `columns` and which appear in the
`detail` overlay under `display.vpc.<resource>`. Resource keys match the service
keys (`subnets`, `security_groups`, `route_tables`, `internet_gateways`,
`nat_gateways`, `endpoints`, `network_acls`, `peering`, `flow_logs`,
`ec2_instances`, `lambda`, `rds`, `load_balancers`).

```yaml
display:
  vpc:
    subnets:
      columns: [name, cidr, az, available_ips, public]   # table columns, left→right
      detail:  [subnet_id, vpc_id, state, map_public_ip] # fields in the detail overlay
    security_groups:
      columns: [sg_id, name, inbound, outbound, description]
```

Any resource type you omit keeps its built-in defaults.

### Resilient scanning (retries & partial results)

Collection is **best-effort**. When a service/region fails partway through —
a later page throttles, or a per-item describe call is denied — everything
collected before the failure is kept and shown, and the error is reported as
*partial* (`partial results kept` on the CLI, `"partial": true` in JSON
errors, and a note in the TUI errors overlay). Previously a single failed page
discarded the whole service/region.

For large accounts that hit AWS throttling (`RequestLimitExceeded`,
`ThrottlingException`), tune the SDK retry behaviour under `aws.retry`:

```yaml
aws:
  retry:
    maxAttempts: 8       # keep retrying longer than the default 3 attempts
    mode: adaptive       # client-side rate limiting that backs off automatically
```

`adaptive` mode wraps the standard exponential backoff with a client-side
token bucket that slows the request rate after throttle responses — usually
the right choice for `--all-regions` sweeps of busy accounts. Leave the block
unset to keep the AWS SDK defaults (3 attempts, standard mode).

## Authentication

Five methods are supported, configured via `authMethod` in `config.yaml` or `--auth-method` on the CLI:

| Method | Description |
|--------|-------------|
| `auto` | AWS SDK default chain: env vars → `~/.aws` credentials/config → EC2/ECS instance metadata |
| `profile` | Named profile from `~/.aws/credentials` or `~/.aws/config` |
| `env` | Only `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` environment variables |
| `static` | Plaintext credentials in `config.yaml` under `aws.static` (avoid committing real keys) |
| `sts` | Assume an IAM role via STS; base credentials come from profile/env/default chain |

### Expired SSO sessions

When an AWS SSO (IAM Identity Center) session expires — or you were never
logged in — every command surfaces the exact fix instead of the SDK's raw
error chain:

```
✗ AWS SSO session for profile 'prod' is expired or missing — run: aws sso login --profile prod
```

The same one-liner appears in TUI error overlays and as `ExpiredCredentials`
errors in `-o json` output. Plain expired STS/session tokens get an analogous
"credentials have expired" hint. Genuinely *missing* credentials (e.g. no
IMDS role, no env vars) are deliberately **not** rewritten — you see the real
error.

### STS AssumeRole Example

```yaml
aws:
  authMethod: sts
  sts:
    roleArn: arn:aws:iam::123456789012:role/ReadOnlyRole
    sessionName: aws-explorer
    externalId: my-external-id    # if required by the trust policy
    durationSeconds: 3600
```

Or via CLI flag:

```bash
./bin/aws_explorer --auth-method sts --role-arn arn:aws:iam::123456789012:role/ReadOnlyRole
```

## Supported Services

| Service Key | Resources Collected | Scope |
|-------------|--------------------|----|
| `ec2` | Instances, VPCs | Regional |
| `s3` | Buckets | Global |
| `rds` | DB instances | Regional |
| `iam` | Roles, users, policies, groups | Global |
| `dynamodb` | Tables | Regional |
| `lambda` | Functions | Regional |
| `emr` | Clusters | Regional |
| `ecs` | Clusters, services | Regional |
| `eks` | Clusters | Regional |
| `elbv2` | Load balancers | Regional |
| `secretsmanager` | Secrets | Regional |
| `sqs` | Queues | Regional |
| `sns` | Topics | Regional |
| `cloudwatch` | Alarms | Regional |
| `cloudfront` | Distributions | Global |
| `route53` | Hosted zones | Global |
| `apigateway` | REST, HTTP & WebSocket APIs | Regional |
| `stepfunctions` | State machines | Regional |
| `eventbridge` | Rules, custom event buses | Regional |
| `elasticache` | Cache clusters | Regional |
| `efs` | File systems | Regional |
| `kinesis` | Data streams | Regional |
| `redshift` | Clusters | Regional |
| `kms` | Customer-managed keys | Regional |
| `ecr` | Repositories | Regional |
| `acm` | Certificates | Regional |
| `cloudformation` | Stacks | Regional |
| `glue` | Jobs (with last-run state), crawlers, databases, triggers, workflows, connections | Regional |
| `athena` | Workgroups | Regional |

Global services (S3, IAM, CloudFront, Route53) are collected once regardless of the regions list.

## Themes

The TUI supports 12 built-in color themes, all named after Australian birds.
Their colors come straight from the [feathers](https://github.com/shandiya/feathers)
palettes (the same data rendered at
[ryandam.net/demos/feathers_palettes](https://ryandam.net/demos/feathers_palettes/index.html)).
Set the active theme in `config.yaml` under `ui.theme` or with the `--theme`
flag on the S3 subcommand.

| Theme Name | Palette feel |
|------------|--------------|
| `spotted-pardalote` | Warm yellow, orange and red |
| `plains-wanderer` | Cream, tan and golden brown |
| `bee-eater` | Cyan, blue and amber |
| `rose-crowned-fruit-dove` | Magenta, coral and green |
| `eastern-rosella` | Yellow, lime and red |
| `oriole` | Gold, salmon and lavender |
| `princess-parrot` | Green, blue and pink (default) |
| `superb-fairy-wren` | Rust, tan and cream |
| `cassowary` | Teal, gold and pink |
| `yellow-robin` | Bright yellow, slate and amber |
| `galah` | Pink, blush and slate |
| `blue-winged-kookaburra` | Light cyan, teal and orange |

### Color roles

Each theme configures granular color roles so that changing one part of the UI
never bleeds into another. Set only the roles you want to change — any role you
leave out falls back to a sensible related role (noted below).

**General**

| Role | Used for | Fallback |
|------|----------|----------|
| `heading` | Titles and section headers | — |
| `text` | Body / foreground text | — |
| `background` | Panel backgrounds (empty = terminal default) | — |
| `muted` | De-emphasised / secondary text | — |
| `accent` | Decorative rails, input prompts and cursors | `heading` |
| `border` | Borders of unfocused panels | — |
| `borderFocus` | Border of the focused panel | `heading` |
| `highlight` | Selected item background (lists, menus) | — |
| `highlightText` | Text on the selected item | — |

**Tables** (every table in the app shares these, so all tables look identical)

| Role | Used for | Fallback |
|------|----------|----------|
| `tableHeader` | Table column header text | `muted` |
| `tableHeaderBg` | Table column header background | `background` |
| `tableHeaderLine` | Rule drawn under table headers | `border` |
| `tableText` | Table cell text | `text` |
| `tableBorder` | Border drawn around table panels | `border` |
| `tableSelectedBg` | Selected table-row background | `highlight` |
| `tableSelectedText` | Text on the selected table row | `highlightText` |

**Status bar & shortcut hints**

| Role | Used for | Fallback |
|------|----------|----------|
| `statusBarBg` | Status bar background | `highlight` |
| `statusBarText` | Status bar text | `highlightText` |
| `hintKey` | Shortcut keys (e.g. `Enter`) in the status bar hints | `statusBarText` |
| `hintText` | Shortcut descriptions (e.g. *open*) in the hints | `statusBarText` |

**Alerts**

| Role | Used for | Fallback |
|------|----------|----------|
| `error` | Error messages and indicators | — |
| `warning` | Warning messages and indicators | — |
| `success` | Success / confirmation messages (e.g. *reachable*, *no issues*) | `accent` |
| `info` | Informational messages and indicators | `muted` |

(The authoritative list lives in the `Roles` registry in
`internal/ui/theme.go`; role names in `config.yaml` are matched
case-insensitively.)

Override any role in `config.yaml` — for example, to recolor just the table
header of the `oriole` theme without touching anything else:

```yaml
ui:
  theme: oriole
  themes:
    oriole:
      tableHeader: "#34E0A1"   # only the table header changes
      error: "#FF0000"         # override just this role
```

### The theme console

The in-app settings panel (press `S`) is styled as a sci-fi mission console.
It **floats over the live app** (the UI stays visible around it), it has a
**fixed size** that never changes with the terminal, tab or mode, and every
row is a control: `↑`/`↓` selects a row, `←`/`→` changes its value —
**instantly**.

- **Theme selector** — the top row. With it selected, `←`/`→` cycles the 12
  built-in themes and the whole app restyles in real time around the console.
- **Subsystem tabs** — the roles are grouped into segmented `GENERAL` /
  `TABLES` / `STATUS BAR` / `ALERTS` tabs (`Tab` or `1`–`4` to switch).
- **Slider rows** — every role renders as a fader: the knob position is the
  color's hue, the track glows in the color itself, and the hex value and a
  swatch sit at the end of the row. Roles on `auto` show a dimmed dashed
  track.
- **Quick palette** — with a role selected, `←`/`→` steps it through a swatch
  ring (the theme's own colors, a hue wheel and a gray ramp), applied
  immediately — changing a color is one keystroke. `a` resets it to `auto`.
- **HUE / SAT / LUM tuner** — `Enter` opens three knobs for fine control
  (`↑`/`↓` picks a knob, `←`/`→` turns it, `Shift+←/→` turns it coarsely),
  plus a `HEX` field for typing an exact value. `Enter` applies, `Esc`
  cancels.
- **Signal monitor** — a live preview strip (mini header, table row, status
  bar and alert glyphs) that follows every knob turn *before* you apply.

All changes apply live to the running app; `Ctrl+S` persists the theme and
every role edit back to `config.yaml`.

## Architecture

```
CLI (cobra)     ┐
                ├── Engine ──┬── Collector Registry ──┬── EC2        ┐
TUI (bubbletea) ┘            │                        ├── S3         │
                            │                        ├── RDS        │
                            ├── Auth (5 methods)      ├── IAM        │ 15 service
                            ├── Config (viper + YAML) ├── DynamoDB   ├ collectors
                            ├── Filtering (reg/tag)   ├── Lambda     │ (EMR, ECS,
                            └── Output (table / JSON) ├── ELBv2      │  EKS, SQS,
                                                      └── ...        ┘  SNS, etc.)

VPC TUI (bubbletea) ──┐
                      ├── Auth (5 methods) ──── EC2 / VPC, RDS, Lambda, ELBv2 APIs
S3 TUI (bubbletea) ───┘                          S3 API
```

The CLI and main TUI share the **`Engine`**, which orchestrates concurrent collection via a bounded goroutine pool, running each `(service, region)` pair in parallel. Global services run once. Results stream back incrementally via a channel so the CLI can print and the TUI can render as data arrives.

The **VPC Explorer** and **S3** TUIs are standalone: they build credentials through the same auth layer but call the relevant AWS APIs directly rather than going through the collector engine.

Each service collector implements:

```go
type Collector interface {
    Name()     string
    IsGlobal() bool
    Collect(ctx context.Context, input CollectInput) ([]model.Resource, error)
}
```

Adding a new AWS service requires only a new package under `internal/services/` that implements this interface, plus registering it in `internal/services/registry.go`.

## Project Structure

```
aws_explorer/
├── cmd/
│   ├── root.go          # Default CLI command (streaming output)
│   ├── snapshotdiff.go  # Offline snapshot browser / diff launcher
│   ├── vpc.go           # VPC Explorer TUI launcher
│   └── s3.go            # S3 browser TUI launcher
├── internal/
│   ├── auth/            # AWS credential building (5 auth methods)
│   ├── awserr/          # AWS error mapping + IAM permission hints
│   ├── config/          # Configuration structs (YAML marshaling)
│   ├── display/         # Per-resource column/detail field registries (VPC, S3)
│   ├── engine/          # Orchestration: concurrent collection + streaming
│   ├── model/           # Data models: Resource, Result, Filter, ExploreError
│   ├── output/          # Table/JSON formatting + streaming writer
│   ├── services/        # Collector interface, registry, 15 service implementations
│   │   ├── ec2/
│   │   ├── s3/
│   │   ├── rds/
│   │   ├── iam/
│   │   ├── dynamodb/
│   │   ├── lambda/
│   │   ├── emr/
│   │   ├── ecs/
│   │   ├── eks/
│   │   ├── elbv2/
│   │   ├── secretsmanager/
│   │   ├── sqs/
│   │   ├── sns/
│   │   ├── cloudwatch/
│   │   ├── cloudfront/
│   │   ├── route53/
│   │   ├── apigateway/
│   │   ├── stepfunctions/
│   │   ├── eventbridge/
│   │   ├── elasticache/
│   │   ├── efs/
│   │   ├── kinesis/
│   │   ├── redshift/
│   │   ├── kms/
│   │   ├── ecr/
│   │   ├── acm/
│   │   ├── cloudformation/
│   │   ├── glue/
│   │   ├── athena/
│   │   └── service.go   # Collector interface + CollectInput
│   ├── table/           # Terminal table component (selection, horizontal column scrolling)
│   ├── tui/             # Main TUI model (sidebar, table, detail panel, search)
│   ├── ui/              # Shared TUI theming, settings panel, help overlay
│   ├── vpctui/          # VPC Explorer TUI (VPC list, resource browser, SG/NACL rule explanations)
│   └── s3tui/           # S3 browser TUI (bucket list, object tree, metadata)
├── main.go              # Entry point: logger init + cmd.Execute()
├── config.yaml          # Default configuration
├── Makefile             # Build, test, lint, run targets
├── go.mod
└── go.sum
```

## Dependencies

| Package | Purpose |
|---------|---------|
| [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS SDK (15 service modules + STS/SSO) |
| [cobra](https://github.com/spf13/cobra) | CLI framework |
| [viper](https://github.com/spf13/viper) | Configuration loading |
| [bubbletea](https://github.com/charmbracelet/bubbletea) | TUI framework |
| [bubbles](https://github.com/charmbracelet/bubbles) | TUI components (spinner, list, viewport) |
| [huh](https://github.com/charmbracelet/huh) | TUI forms |
| [lipgloss](https://github.com/charmbracelet/lipgloss) | Terminal styling |
| [bubble-table](https://github.com/evertras/bubble-table) | TUI table component |
| [clipboard](https://github.com/atotto/clipboard) | Copy resource IDs to clipboard |
| [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync) | Bounded goroutine pool (errgroup) |

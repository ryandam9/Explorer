# AWS Explorer

Discover, monitor, and display AWS resources across accounts and regions via CLI or TUI.

## Features

- **Four modes**: CLI (streaming table/JSON output), TUI (interactive exploration), VPC Explorer TUI (drill into a VPC's networking), S3 TUI (dedicated S3 browser)
- **15 services**: EC2, S3, RDS, IAM, DynamoDB, Lambda, EMR, ECS, EKS, ELBv2, Secrets Manager, SQS, SNS, CloudWatch, Route53
- **VPC Explorer**: browse a VPC's subnets, security groups, network interfaces, route tables, gateways, endpoints, NACLs, peering, flow logs, and attached compute/services in a three-pane TUI
- **VPC debugging toolkit** (no AI, deterministic): a findings linter, a connectivity path tracer, plain-English SG/NACL rule explanations, cross-reference ("where used"), merged effective security rules, DNS diagnostics, a public-exposure audit, snapshot diffing, Markdown export, and AWS Reachability Analyzer integration — see [VPC Debugging Toolkit](#vpc-debugging-toolkit)
- **Cost/waste audit**: `aws_explorer audit` scans for the classic sources of silent spend — unattached EBS volumes, idle Elastic IPs and NAT gateways, load balancers with no healthy targets or no traffic, gp2→gp3 candidates, forgotten snapshots/AMIs, over-provisioned DynamoDB tables — each finding with a stable check ID and an estimated monthly cost, printable or explored in an interactive TUI (`--tui`) — see [Audit Usage](#audit-usage)
- **IAM debugging**: `aws_explorer iam decode` turns an "Encoded authorization failure message" into a readable verdict (principal, action, resource, explicit vs implicit deny) — see [IAM Tools](#iam-tools)
- **CloudTrail "who changed this"**: `aws_explorer trail <resource-id-or-arn>` lists recent CloudTrail management events for a resource — when, which API call, which principal, from which IP — using the zero-setup 90-day LookupEvents window; the summary TUI's `t` timeline is the interactive twin — see [Trail Usage](#trail-usage)
- **Account snapshot diff**: `summary --baseline` / `summary --diff` answers "what changed in this account since yesterday?" — added/removed/modified resources across the whole merged-by-ARN inventory, deterministic and volatile-field-free; `D` in the summary TUI is the interactive twin — see [Account snapshot diff](#account-snapshot-diff--what-changed-since-yesterday)
- **Global fuzzy finder**: `Ctrl+P` in the summary TUI jumps to any resource by name/ID/ARN fragment ("I have `eni-0abc` from an error — what is it?"); `aws_explorer find <fragment>` is the CLI twin — see [Find Usage](#find-usage)
- **SSO-aware errors**: an expired AWS SSO session prints the exact fix (`run: aws sso login --profile prod`) instead of an SDK error chain, in the CLI and every TUI
- **Expiry watchlist**: `aws_explorer expiring` lists everything that breaks on a calendar date — ACM/IAM certificate expiry, Lambda runtime deprecations, EKS end-of-support, RDS CA certs & pending maintenance, overdue secret rotations — sorted by days remaining — see [Expiring Usage](#expiring-usage)
- **Config-driven**: YAML configuration for services, regions, filters, output, and per-resource display columns
- **5 auth methods**: auto (SDK default chain), profile, env vars, static credentials, STS AssumeRole
- **Output formats**: Table (default), JSON, NDJSON, CSV — with `--no-header` for scripting and colored states on terminals
- **Filtering**: By region, state, tags, name, and IDs
- **Concurrent**: Bounded goroutine pool (default 8) for parallel collection across services and regions; collectors stream results page-by-page, so the first resources appear after a single API round-trip instead of after the last page
- **Resilient**: Best-effort collection — a throttle, timeout, or denied call mid-scan keeps everything already gathered (flagged as partial) instead of dropping the service/region, with configurable retry attempts and adaptive backoff
- **Themes**: 12 built-in bird-themed color schemes with 24 individually customizable color roles (table header, borders, status bar, alerts, …) — editable live in the in-app settings panel
- **Context-aware shortcuts**: the status bar in every TUI shows only the keys that work on the current screen
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

# Run interactive TUI
./bin/aws_explorer tui

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

Interactive terminal UI with sidebar navigation, resource table, and detail panel.

```bash
./bin/aws_explorer tui [flags]
```

Accepts the same global flags as the CLI command (`--config`, `--profile`,
`--auth-method`, `--role-arn`, `--region`, `--all-regions`).

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
| `J` | Toggle a raw-JSON view in the detail panel (`y` then copies the JSON) |
| `C` | Export the current (filtered, sorted) view to CSV under `~/.aws_explorer/exports/` |
| `D` | **What changed**: first press saves an account baseline snapshot, later presses diff the live inventory against it (`b` inside the overlay re-baselines) |
| `P` | Switch AWS profile and/or region scope, then rescan — no restart needed |
| `e` | Open the scan-errors overlay (services with errors also carry a `⚠n` badge in the sidebar) |
| `S` | Settings panel (themes & colors) |
| `?` | Help overlay |
| `Esc` | Close detail panel / overlay |
| `q` / `Ctrl+C` | Quit |

While a scan is running, the header shows real progress (`scanning 23/60` with
the last pending `service@region` tasks named) instead of a generic spinner,
and collection errors are surfaced inline: a red `⚠ n errors` badge in the
header plus per-service warning badges in the sidebar.

## Summary Usage

`summary` produces a single, numbered inventory of **every** discovered resource
across all configured regions, spanning **all AWS services** — not just the ones
with a built-in collector.

It combines two sources and merges them by ARN:

1. **The 15 typed collectors** (EC2, S3, RDS, …) for rich data — state,
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

`audit` scans the configured regions for findings — currently the **cost/waste**
category — and prints them as a ranked table with an estimated monthly cost per
finding and a total at the bottom. Like everything else in the tool, the audit
is **deterministic, read-only and best-effort**: a denied API call skips the
affected checks (reported on stderr) and never aborts the run.

```bash
./bin/aws_explorer audit [flags]
```

Accepts the same global flags as the CLI command (`--config`, `--profile`,
`--auth-method`, `--role-arn`, `--region`, `--all-regions`, `-o`, `--no-header`).

```
SEVERITY    ID            RESOURCE        REGION     ISSUE                                  EST/MO   FIX
🟡 WARNING  COST-EBS-001  vol-0abc        us-east-1  Unattached EBS volume (gp2, 1024 GiB)  $102.40  Snapshot the volume and delete it, …
🟡 WARNING  COST-NAT-001  nat-01 (spare)  us-east-1  NAT gateway not referenced by any route $32.85  Delete the NAT gateway, …
🔵 INFO     COST-EBS-002  vol-0def        us-east-1  gp2 volume could be gp3 (500 GiB)      $10.00   Modify the volume type to gp3 …

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
 DAYS  WHAT                                 RESOURCE                  REGION      DETAIL
   -3  Lambda runtime deprecated            payments-fn (python3.9)   us-east-1   runtime python3.9 was deprecated 2025-12-15 — update the function's runtime
   12  ACM certificate expires              *.example.com             us-east-1   certificate is in use — renew or re-issue before it expires (expires 2026-06-24)
   61  EKS version end of standard support  prod-cluster (1.33)       eu-west-1   standard support for 1.33 ends 2026-07-29 — upgrade the cluster (extended support bills extra)
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

## IAM Tools

Helpers for the most common AWS support question: *"why am I denied?"*

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

## Trail Usage

`trail` answers the most useful question in an incident: *who changed this
resource, and when?* It looks up recent CloudTrail management events that
reference the resource and prints when, which API call, which principal
(short form — `role/deploy-pipeline`, `user/alice`, `root`), and from which
source IP. Events are newest first.

```bash
# Who touched this security group?
aws_explorer trail sg-0abc123

# Changes to an instance in the last 7 days, in a specific region
aws_explorer trail i-0abc12345 --since 7d -r eu-west-1

# ARNs work too — they're reduced to the resource name CloudTrail records.
# Global services (IAM, …) record their events in us-east-1.
aws_explorer trail arn:aws:iam::123456789012:role/app -r us-east-1

# Machine-readable
aws_explorer trail my-bucket -o json | jq '.[0]'
```

```
TIME                 EVENT                          PRINCIPAL             SOURCE IP
2026-06-11 14:02:11  AuthorizeSecurityGroupIngress  role/deploy-pipeline  203.0.113.7
2026-06-09 09:15:42  ModifySecurityGroupRules       user/alice            198.51.100.2
```

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | full window | Only events after this long ago (`7d`, `36h`, or a plain day count) |
| `--limit` | `50` | Maximum number of events to print |
| `--read-events` | off | Include read-only (`Describe*`/`List*`/`Get*`) events, marked `(read)` in the table |

Notes:

- Uses `cloudtrail:LookupEvents`, which covers the **last 90 days** of
  management events with **no trail or S3 bucket setup required** — that one
  permission is all it needs.
- CloudTrail records events in the region where the resource lives; pick it
  with `-r` (default: the first configured region).
- By default only **mutating** events are shown — the `Describe*` noise would
  drown out the changes you're looking for.
- The API is rate-limited (2 TPS); pages are fetched serially and capped, so
  very chatty resources return the most recent events rather than everything.

The same view lives in the summary TUI: press **`t`** on a resource's detail
panel for its CloudTrail timeline.

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
NAME        TYPE           ID                     REGION      ARN
prod-web-3  ec2/instance   i-0abc12def34567890    us-east-1   arn:aws:ec2:us-east-1:…
prod-web    elbv2/loadb…   arn:aws:elasticloadb…  us-east-1   arn:aws:elasticloadb…
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
| `C` | Export the current resource table to CSV under `~/.aws_explorer/exports/` |
| `r` | Refresh the VPC list or the current resource list |
| `Esc` | Go back one level (overlay → table → sidebar → VPC list) |
| `S` | Open the settings panel (themes & colors) |
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

### Full log viewer

Pressing `Enter` on a log event opens the **full log viewer**: a full-screen
page with the entire log (24-hour lookback, most recent 2000 events) for the
selected stream — or the whole group in group-level search — that streams new
events live as they arrive.

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
make all             # fmt + vet + test + build
make help            # Show available targets
```

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
| `route53` | Hosted zones | Global |

Global services (S3, IAM, Route53) are collected once regardless of the regions list.

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
│   ├── tui.go           # Interactive TUI launcher
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
│   │   ├── route53/
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

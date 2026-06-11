# AWS Explorer

Discover, monitor, and display AWS resources across accounts and regions via CLI or TUI.

## Features

- **Five modes**: CLI (streaming table/JSON output), TUI (interactive exploration), VPC Explorer TUI (drill into a VPC's networking), S3 TUI (dedicated S3 browser), Logs TUI (CloudWatch Logs browser)
- **16 services**: EC2, S3, RDS, IAM, DynamoDB, Lambda, EMR, ECS, EKS, ELBv2, Secrets Manager, SQS, SNS, CloudWatch, CloudWatch Logs, Route53
- **CloudWatch Logs fetching**: list log groups, fetch/search events with filter patterns and time windows from the CLI, or browse interactively — see [Logs Usage](#logs-usage)
- **VPC Explorer**: browse a VPC's subnets, security groups, network interfaces, route tables, gateways, endpoints, NACLs, peering, flow logs, and attached compute/services in a three-pane TUI
- **VPC debugging toolkit** (no AI, deterministic): a findings linter, a connectivity path tracer, plain-English SG/NACL rule explanations, cross-reference ("where used"), merged effective security rules, DNS diagnostics, a public-exposure audit, snapshot diffing, Markdown export, and AWS Reachability Analyzer integration — see [VPC Debugging Toolkit](#vpc-debugging-toolkit)
- **Config-driven**: YAML configuration for services, regions, filters, output, and per-resource display columns
- **5 auth methods**: auto (SDK default chain), profile, env vars, static credentials, STS AssumeRole
- **Output formats**: Table (default), JSON
- **Filtering**: By region, state, tags, name, and IDs
- **Concurrent**: Bounded goroutine pool (default 8) for parallel collection across services and regions
- **Resilient**: Best-effort collection — a throttle or denied call mid-scan keeps everything already gathered (flagged as partial) instead of dropping the service/region, with configurable retry attempts and adaptive backoff
- **Themes**: 12 built-in bird-themed color schemes with 24 individually customizable color roles (table header, borders, status bar, alerts, …) — editable live in the in-app settings panel
- **Context-aware shortcuts**: the status bar in every TUI shows only the keys that work on the current screen
- **Uniform tables**: every table shares one theme and scrolls horizontally (`<` / `>`) when columns don't fit

## Prerequisites

- Go 1.26.1 or later
- AWS credentials configured (see [Authentication](#authentication))

## Quick Start

```bash
# Clone and build
git clone https://github.com/ryandam9/aws_explorer.git
cd aws_explorer
make build          # produces bin/aws_explorer

# Run CLI (streams table to stdout)
./bin/aws_explorer

# Run interactive TUI
./bin/aws_explorer tui

# List every resource across all regions (SNO, Name, Type, ARN, Region/AZ)
./bin/aws_explorer summary --all-regions

# Run the VPC Explorer TUI
./bin/aws_explorer vpc --region us-east-1

# Run S3 browser TUI
./bin/aws_explorer s3 --bucket my-bucket --region us-east-1

# Fetch the last hour of ERROR events from a log group
./bin/aws_explorer logs --group /aws/lambda/my-fn --since 1h --filter ERROR

# Browse CloudWatch Logs interactively
./bin/aws_explorer logs --tui
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

The default command streams discovered resources as a table or JSON to stdout.

```bash
./bin/aws_explorer [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Path to config file |
| `--profile` | `default` | AWS named profile |
| `--auth-method` | `auto` | Auth method: `auto`, `profile`, `env`, `static`, `sts` |
| `--role-arn` | — | IAM role ARN to assume (for `sts` auth) |
| `--output` | `table` | Output format: `table` or `json` |
| `--all-regions` | `false` | Scan all available AWS regions |

### Examples

```bash
# Use a named AWS profile
./bin/aws_explorer --profile prod

# Output JSON
./bin/aws_explorer --output json

# Scan all regions
./bin/aws_explorer --all-regions

# Assume an IAM role
./bin/aws_explorer --auth-method sts --role-arn arn:aws:iam::123456789012:role/MyRole

# Custom config file
./bin/aws_explorer --config /path/to/config.yaml

# Combine flags
./bin/aws_explorer --profile dev --output json --all-regions
```

## TUI Usage

Interactive terminal UI with sidebar navigation, resource table, and detail panel.

```bash
./bin/aws_explorer tui [flags]
```

Accepts the same `--config`, `--profile`, `--auth-method`, `--role-arn`, and `--all-regions` flags as the CLI command.

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
| `/` | Quick text filter (matches any column) |
| `f` | Advanced filter (region / state) |
| `r` | Reset all filters |
| `S` | Settings panel (themes & colors) |
| `?` | Help overlay |
| `Esc` | Close detail panel / overlay |
| `q` / `Ctrl+C` | Quit |

## Summary Usage

`summary` produces a single, numbered inventory of **every** discovered resource
across all configured regions, spanning **all AWS services** — not just the ones
with a built-in collector.

It combines two sources and merges them by ARN:

1. **The 16 typed collectors** (EC2, S3, RDS, …) for rich data — state,
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

Accepts the same `--config`, `--profile`, `--auth-method`, `--role-arn`, and
`--all-regions` flags as the CLI command.

### Summary Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output` / `-o` | `table` | Output format: `table`, `json`, or `csv` |
| `--tui` | `false` | Explore the same inventory interactively instead of printing |
| `--typed-only` | `false` | Skip the all-services Tagging API sweep; use only the built-in typed collectors |

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

## Logs Usage

`logs` browses CloudWatch Logs: list log groups, fetch/search log events from
the CLI, or explore both interactively with `--tui`.

Fetching uses [FilterLogEvents] — the best general-purpose retrieval API: it
searches **all streams in a group** within a time window and supports
CloudWatch [filter patterns]. The API is throttled at ~5 requests/second per
account/region, so pages are fetched sequentially; if a page fails mid-fetch,
the events retrieved so far are still shown (the same best-effort behaviour as
the resource collectors). Log groups also appear as `cloudwatchlogs/log_group`
resources in the main CLI/TUI/summary inventory.

[FilterLogEvents]: https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_FilterLogEvents.html
[filter patterns]: https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/FilterAndPatternSyntax.html

```bash
./bin/aws_explorer logs [flags]
```

### Logs Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--group` / `-g` | — | Log group to fetch events from; omit to list log groups |
| `--since` | `15m` | How far back to fetch (Go duration: `15m`, `2h`, `48h`) |
| `--filter` / `-f` | — | CloudWatch Logs filter pattern (e.g. `ERROR`, `{ $.level = "error" }`) |
| `--limit` | `1000` | Maximum events to fetch (`0` = no limit) |
| `--region` / `-r` | (configured) | AWS region (defaults to the configured/profile region) |
| `--tui` | `false` | Browse groups and events interactively |
| `--theme` | `spotted-pardalote` | Color theme for `--tui` |

Also accepts the global `--profile`, `--auth-method`, `--role-arn`, and
`-o table|json` flags.

### Logs Examples

```bash
# List log groups in the default region
./bin/aws_explorer logs

# Last 15 minutes of events from a Lambda function's group
./bin/aws_explorer logs -g /aws/lambda/my-fn

# Search 48 hours of events for a JSON filter pattern, as JSON
./bin/aws_explorer logs -g /ecs/web --since 48h -f '{ $.level = "error" }' -o json

# Fetch everything in the window (no event cap)
./bin/aws_explorer logs -g /aws/lambda/my-fn --since 1h --limit 0

# Browse interactively, starting on a group
./bin/aws_explorer logs --tui -g /aws/lambda/my-fn
```

### Logs TUI

A two-pane browser: log groups on the left, events on the right. The status
bar shows only the keys usable right now.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate groups / scroll events |
| `Enter` | Fetch events for the selected group |
| `Tab` | Switch focus between groups and events |
| `/` | Filter the group list |
| `f` | Edit the event filter pattern |
| `t` | Cycle the look-back window (15m → 1h → 3h → 12h → 24h → 3d → 7d) |
| `m` | Load more events (when the window has more pages) |
| `r` | Refresh |
| `q` / `Ctrl+C` | Quit |

> **Cost note.** FilterLogEvents itself has no per-GB query charge (unlike
> Logs Insights), making it the right default for interactive fetching.

## VPC Explorer TUI Usage

An interactive, three-pane TUI for drilling into a single VPC's networking and
attached resources. Pick a VPC on the left, a resource category in the middle,
and browse the matching resources on the right.

```bash
./bin/aws_explorer vpc [flags]
```

If `--region` is omitted, all regions are scanned for VPCs.

### VPC Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--profile` | (global) | AWS named profile (overrides global `--profile`) |
| `--auth-method` | (global) | Auth method: `auto`, `profile`, `env`, `static`, `sts` |
| `--role-arn` | — | IAM role ARN to assume via STS |
| `--region` | — | AWS region (defaults to all regions if omitted) |
| `--theme` | `spotted-pardalote` | Color theme |
| `--all-regions` | `false` | Scan all AWS regions |

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
| `/` | Filter the VPC list by name or ID (VPC list only) |
| `c` | Copy the selected resource's ID to the clipboard |
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

Scans the whole VPC and opens a scrollable, severity-grouped list of issues
(`🔴 critical`, `🟡 warning`, `🔵 info`), sorted most-severe first:

```
VPC Findings — 1 critical, 2 warning, 0 info

🔴 CRITICAL  Security group exposes a sensitive port to the internet  [sg-0a1]
   sg-0a1 (default): Allow inbound SSH (TCP 22) from anywhere on the internet (0.0.0.0/0)
   Fix: Restrict the source to specific CIDRs or a security group instead of 0.0.0.0/0.
```

Each finding has a **title**, the **resource** it concerns, a **detail** of why
it fired, and a suggested **fix**. The checks:

| Area | Finding | Severity |
|------|---------|----------|
| Security groups | Sensitive port (admin/database/all) open to `0.0.0.0/0` | 🔴 critical |
| Security groups | Rule references a security group not in this VPC | 🔵 info |
| Route tables | Blackhole route (target deleted) | 🟡 warning |
| Subnets | Low available IPs / >90% utilization | 🟡 warning |
| Subnets | Auto-assign public IP but no internet-gateway route | 🟡 warning |
| Subnets | No outbound internet path (no IGW/NAT route) | 🔵 info |
| NAT gateways | Available but unreferenced by any route (idle, still billing) | 🟡 warning |
| Internet gateways | Detached from the VPC | 🔵 info |
| Network ACLs | Stateless return-traffic gap (ephemeral ports not allowed back) | 🟡 warning |
| Peering | Overlapping CIDRs · not active | 🟡 / 🔵 |
| VPC endpoints | Gateway endpoint with no route-table association | 🟡 warning |
| VPC endpoints | Interface endpoint SGs don't allow inbound 443 · private DNS off | 🟡 / 🔵 |
| **Capacity** | Rules per SG (limit 60), routes per route table (50), SGs per ENI (5), subnets per VPC (200) | 🟡 ≥80%, 🔴 at limit |
| **Orphans** | Security group attached to nothing & unreferenced · empty subnet | 🔵 info |

The NACL stateless check is careful to *not* flag the correct "inbound 443 +
outbound ephemeral" pattern. Capacity limits are AWS defaults (adjustable via
Service Quotas). Orphan checks are skipped if ENI data is unavailable.

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
egress.

### Cross-reference — "where used" (`x`)

On any resource, `x` shows everything that references it and what it
references, turning the flat tables into a navigable graph:

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
Public subnets (route to an internet gateway)                    (1)  • subnet-pub
Security groups open to the internet (inbound from 0.0.0.0/0)    (1)  • sg-web (web) — HTTPS (TCP 443)
Network interfaces with a public IP                              (1)  • eni-pub (52.1.1.1) → i-web
```

Public subnets are those routing to an internet gateway; SGs list their
internet-open ports in plain English (SG-to-SG references excluded); ENIs are
those holding a public IP/EIP.

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
    + inbound|tcp|22|10.0.0.0/8
```

Baselines are stored as JSON in `~/.aws_explorer/vpc-snapshots/<vpc-id>.json`.
Inside the overlay, `b` re-baselines to the current state. Volatile fields (like
available-IP counts) are deliberately excluded so they don't create noise.

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

| Flag | Default | Description |
|------|---------|-------------|
| `--bucket` | — | Bucket name to open directly |
| `--prefix` | — | Key prefix to start browsing from |
| `--region` | — | AWS region of the bucket |
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

All settings live in `config.yaml`. CLI flags override config file values at runtime.

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
| `cloudwatchlogs` | Log groups | Regional |
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

The in-app settings panel (press `S`) lets you edit every role live and saves
your changes back to `config.yaml`.

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
│   ├── s3.go            # S3 browser TUI launcher
│   └── logs.go          # CloudWatch Logs CLI + TUI launcher
├── internal/
│   ├── auth/            # AWS credential building (5 auth methods)
│   ├── awserr/          # AWS error mapping + IAM permission hints
│   ├── config/          # Configuration structs (YAML marshaling)
│   ├── display/         # Per-resource column/detail field registries (VPC, S3)
│   ├── engine/          # Orchestration: concurrent collection + streaming
│   ├── logs/            # CloudWatch Logs API wrappers (groups, FilterLogEvents)
│   ├── logstui/         # CloudWatch Logs browser TUI (group list, event viewer)
│   ├── model/           # Data models: Resource, Result, Filter, ExploreError
│   ├── output/          # Table/JSON formatting + streaming writer
│   ├── services/        # Collector interface, registry, 16 service implementations
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
│   │   ├── cloudwatchlogs/
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

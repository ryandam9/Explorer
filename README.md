# AWS Explorer

Discover, monitor, and display AWS resources across accounts and regions via CLI or TUI.

## Features

- **Four modes**: CLI (streaming table/JSON output), TUI (interactive exploration), VPC Explorer TUI (drill into a VPC's networking), S3 TUI (dedicated S3 browser)
- **29 services**: EC2, S3, RDS, IAM, DynamoDB, Lambda, EMR, ECS, EKS, ELBv2, Secrets Manager, SQS, SNS, CloudWatch, CloudFront, Route53, API Gateway, Step Functions, EventBridge, ElastiCache, EFS, Kinesis, Redshift, KMS, ECR, ACM, CloudFormation, Glue, Athena
- **VPC Explorer**: browse a VPC's subnets, security groups, network interfaces, route tables, gateways, endpoints, NACLs, peering, flow logs, and attached compute/services in a three-pane TUI
- **VPC debugging toolkit** (no AI, deterministic): a findings linter, a connectivity path tracer, plain-English SG/NACL rule explanations, cross-reference ("where used"), merged effective security rules, DNS diagnostics, a public-exposure audit, snapshot diffing, Markdown export, and AWS Reachability Analyzer integration — see [VPC Debugging Toolkit](docs/vpc.md#vpc-debugging-toolkit)
- **Cost/waste audit**: `aws_explorer audit` scans for the classic sources of silent spend — unattached EBS volumes, idle Elastic IPs and NAT gateways, load balancers with no healthy targets or no traffic, gp2→gp3 candidates, forgotten snapshots/AMIs, over-provisioned DynamoDB tables — each finding with a stable check ID and an estimated monthly cost, printable or explored in an interactive TUI (`--tui`) — see [Audit Usage](docs/audit.md)
- **Live bill**: `aws_explorer bill` shows the actual bill from the Cost Explorer API — every service and usage type with its usage quantity, price and a grand total (the Billing console's numbers, not list-price estimates); `--tui` is a live screen that re-fetches on an interval, flags what moved since the last refresh, and drills into per-resource costs (resource ID/ARN) for a service — see [Bill Usage](docs/bill.md)
- **IAM debugging**: `aws_explorer iam decode` turns an "Encoded authorization failure message" into a readable verdict, and `aws_explorer iam can <principal> <action> [resource]` simulates IAM policy ("can X do Y on Z?") with the matched statements named and the simulator's blind spots stated — see [IAM Tools](docs/iam.md)
- **CloudTrail activity feed**: `aws_explorer trail [resource]` lists recent CloudTrail management events — when, which API call, which principal, from which IP, and whether it failed — scoped to a resource, a principal (`--by`), an API (`--event`), a service (`--source`), or the whole account, with `--errors-only` for failed/denied calls; uses the zero-setup 90-day LookupEvents window; the summary TUI's `t` timeline is the interactive twin — see [Trail Usage](docs/trail.md)
- **CloudTrail Lake (SQL)**: `aws_explorer lake` queries a CloudTrail Lake event data store for years of history, data events and aggregations — built-in `--top-principals` / `--top-events` queries or your own `--sql`, with `--tui` to explore results — see [Lake Usage](docs/trail.md#lake-usage)
- **Account snapshot diff**: `summary --baseline` / `summary --diff` answers "what changed in this account since yesterday?" — added/removed/modified resources across the whole merged-by-ARN inventory, deterministic and volatile-field-free; `D` in the summary TUI is the interactive twin — see [Account snapshot diff](docs/summary.md#account-snapshot-diff--what-changed-since-yesterday)
- **Open in AWS console**: `o` in every TUI (summary, VPC explorer, S3, CloudWatch logs) copies a console deep link for the selection — ARN-aware coverage for all 15 services and every VPC resource type, with an ARN-search fallback for the long tail — and opens it in your browser when the session is local
- **Global fuzzy finder**: `Ctrl+P` in the summary TUI jumps to any resource by name/ID/ARN fragment ("I have `eni-0abc` from an error — what is it?"); `aws_explorer find <fragment>` is the CLI twin — see [Find Usage](docs/find.md)
- **SSO-aware errors**: an expired AWS SSO session prints the exact fix (`run: aws sso login --profile prod`) instead of an SDK error chain, in the CLI and every TUI
- **Expiry watchlist**: `aws_explorer expiring` lists everything that breaks on a calendar date — ACM/IAM certificate expiry, Lambda runtime deprecations, EKS end-of-support, RDS CA certs & pending maintenance, overdue secret rotations — sorted by days remaining — see [Expiring Usage](docs/expiring.md)
- **ECS stopped-task triage**: `aws_explorer ecs stopped` answers "why did my task stop?" — the task-level stop reason plus the failing container's exit code, glossed in plain English (137 → possible OOM-kill, 139 → segfault) — see [ECS Stopped-Task Triage](docs/ecs.md)
- **Where-used / blast radius**: `aws_explorer whereused <arn-or-id>` answers "can I delete this?" for IAM roles, KMS keys, ACM certificates and security groups — every resource that references the target, with the scanned reference types listed so "not referenced" is a scoped answer — see [Whereused (blast radius)](docs/find.md#whereused-blast-radius)
- **Related resources (bidirectional)**: `aws_explorer related <arn-or-id>` shows everything linked to a resource in *both* directions — what it uses (its role, KMS key, security groups…) and what uses it — with `--depth` to follow links several hops out, or `--tui` for an interactive explorer that walks the graph hop by hop — see [Related (bidirectional)](docs/find.md#related-bidirectional)
- **Service-quota dashboard**: `aws_explorer quotas` reports the AWS limits that actually cause incidents (vCPUs, EIPs, VPCs, ENIs, Lambda concurrency, RDS, EBS storage…) with real account-specific limits and current usage, sorted closest-to-exhaustion first — see [Quotas (service-quota dashboard)](docs/quotas.md)
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
- AWS credentials configured (see [Authentication](docs/authentication.md))

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

## Commands & guides

Each command and TUI has a focused guide under [`docs/`](docs/):

| Guide | What it covers |
|-------|----------------|
| [CLI usage](docs/cli.md) | Global flags, output formats (table/JSON/NDJSON/CSV), filtering |
| [Summary](docs/summary.md) | Account-wide inventory, baseline/diff, coverage advisory |
| [TUI](docs/tui.md) | The interactive resource explorer |
| [Audit](docs/audit.md) | Security/cost checks, `--fail-on`, SARIF, ignore lists |
| [Expiring](docs/expiring.md) | Upcoming certificate/secret/grant deadlines |
| [ECS triage](docs/ecs.md) | Recently stopped ECS tasks |
| [Quotas](docs/quotas.md) | Service-quota utilization dashboard |
| [Glue](docs/glue.md) · [EMR](docs/emr.md) | Data-platform dashboards |
| [Lambda](docs/lambda.md) | Functions, layers & event-source mappings dashboard |
| [Bill](docs/bill.md) | Cost Explorer summary (paid API) |
| [IAM tools](docs/iam.md) | `iam decode`, `iam can` |
| [Trail](docs/trail.md) | CloudTrail event feed and Lake queries |
| [Find / whereused](docs/find.md) | Fuzzy resource search and blast-radius lookups |
| [Related resources](docs/related.md) | Bidirectional, multi-hop relationship discovery — design, coverage & limits |
| [VPC explorer](docs/vpc.md) | VPC TUI and the debugging toolkit |
| [Tags explorer](docs/tags.md) | Find resources by tag (browse keys/values or filter) |
| [S3](docs/s3.md) · [CloudWatch Logs](docs/cloudwatch.md) | Storage and log browsers |
| [Themes](docs/themes.md) | Color themes |
| [Architecture](docs/architecture.md) | Internals, project layout, dependencies |

## Configuration & authentication

Configuration is optional — the tool ships with built-in defaults and works
from any directory. See [docs/configuration.md](docs/configuration.md) for the
full config reference and [docs/authentication.md](docs/authentication.md) for
profiles, SSO, env vars, and STS role assumption.

## Supported services

29 typed collectors plus a Resource Groups Tagging API sweep for the long tail.
See the [collector coverage matrix](docs/collectors.md) for exactly which
resource types each collector covers, its required IAM, and known gaps.

## Safety

AWS Explorer is **read-only by default** — it lists and explains resources
without modifying them. The only destructive capability is S3 object deletion in
the S3 TUI, which is disabled unless explicitly enabled with `--allow-delete`
and confirmed interactively. Paid APIs (Cost Explorer) and any expensive scans
are opt-in and state their cost before running.

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

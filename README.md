# AWS Explorer

Discover, monitor, and display AWS resources across accounts and regions via CLI or TUI.

## Features

- **Four modes**: CLI (streaming table/JSON output), TUI (interactive exploration), VPC Explorer TUI (drill into a VPC's networking), S3 TUI (dedicated S3 browser)
- **15 services**: EC2, S3, RDS, IAM, DynamoDB, Lambda, EMR, ECS, EKS, ELBv2, Secrets Manager, SQS, SNS, CloudWatch, Route53
- **VPC Explorer**: browse a VPC's subnets, security groups, route tables, gateways, endpoints, NACLs, peering, flow logs, and attached compute/services in a three-pane TUI
- **Plain-English rule explanations**: Security Group and Network ACL rules are translated into readable sentences with `⚠` risk flags for sensitive ports exposed to the internet — no AI required
- **Config-driven**: YAML configuration for services, regions, filters, output, and per-resource display columns
- **5 auth methods**: auto (SDK default chain), profile, env vars, static credentials, STS AssumeRole
- **Output formats**: Table (default), JSON
- **Filtering**: By region, state, tags, name, and IDs
- **Concurrent**: Bounded goroutine pool (default 8) for parallel collection across services and regions
- **Themes**: 12 built-in bird-themed color schemes for the TUI

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

# Run the VPC Explorer TUI
./bin/aws_explorer vpc --region us-east-1

# Run S3 browser TUI
./bin/aws_explorer s3 --bucket my-bucket --region us-east-1
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

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate rows |
| `←` / `→` | Switch sidebar panels (services / regions) |
| `Enter` | Select / expand item |
| `/` | Search / filter |
| `c` | Copy selected resource ID to clipboard |
| `q` / `Ctrl+C` | Quit |

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

- **NETWORK** — Subnets, Security Groups, Route Tables, Internet Gateways, NAT Gateways, VPC Endpoints, Network ACLs, Peering, Flow Logs
- **COMPUTE** — EC2 Instances, Lambda Functions
- **SERVICES** — RDS Instances, Load Balancers

### VPC Explorer Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate the VPC list, category sidebar, or resource table |
| `Enter` | Open a VPC / load a category / open the resource detail overlay |
| `Tab` | Switch focus between the category sidebar and the resource table |
| `<` / `>` (or `,` / `.`) | Scroll table columns left/right when a table is wider than the panel |
| `/` | Filter the VPC list by name or ID |
| `c` | Copy the selected resource ID to the clipboard |
| `r` | Refresh the VPC list or the current resource list |
| `Esc` | Go back (detail → table → VPC list) |
| `S` | Open the settings panel (themes & colors) |
| `?` | Toggle help |
| `q` / `Ctrl+C` | Quit |

### Horizontal column scrolling

Wide tables (e.g. Security Groups) don't truncate or drop columns on narrow
terminals. The leading identifier columns stay pinned while the rest scroll with
`<` / `>`; a `◀ N more cols ▶` indicator shows when columns are off-screen.

### Plain-English rule explanations

Opening the detail overlay (`Enter`) for a **Security Group** or **Network ACL**
adds an "In plain English" section that translates each rule into a readable
sentence, for example:

```
  In plain English:
  • Allow inbound HTTPS (TCP 443) from anywhere on the internet (0.0.0.0/0)
  • Allow inbound SSH (TCP 22) from anywhere on the internet (0.0.0.0/0)  ⚠ remote admin access open to the entire internet
  • Allow inbound MySQL/Aurora (TCP 3306) from resources in security group sg-0abc123
```

Well-known ports are named, CIDRs are classified (public / IPv6 / single host /
RFC1918 private), and security-group / prefix-list references are resolved.
Rules that expose sensitive ports (remote-admin, databases, or all ports) to the
public internet are flagged with `⚠`; ordinary public web ports are not, to
avoid alert fatigue. NACL explanations additionally show the rule number, the
allow/deny action, and a reminder that NACLs are stateless and evaluated in
ascending rule-number order (first match wins).

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

| Role | Used for | Fallback |
|------|----------|----------|
| `heading` | Titles and section headers | — |
| `text` | Body / foreground text | — |
| `background` | Panel backgrounds (empty = terminal default) | — |
| `border` | Borders of unfocused panels | — |
| `borderFocus` | Border of the focused panel | `heading` |
| `highlight` | Selected table-row background | — |
| `highlightText` | Text on the selected row | — |
| `muted` | De-emphasised / secondary text | — |
| `tableHeader` | Table column header text | `muted` |
| `tableHeaderLine` | Rule drawn under table headers | `border` |
| `statusBarBg` | Status bar background | `highlight` |
| `statusBarText` | Status bar text | `highlightText` |
| `accent` | Decorative rails, input prompts and cursors | `heading` |
| `error` | Error messages and indicators | — |
| `warning` | Warning messages and indicators | — |

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

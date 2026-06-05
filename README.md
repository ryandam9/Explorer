# AWS Explorer

Discover, monitor, and display AWS resources across accounts and regions via CLI or TUI.

## Features

- **Three modes**: CLI (streaming table/JSON output), TUI (interactive exploration), S3 TUI (dedicated S3 browser)
- **15 services**: EC2, S3, RDS, IAM, DynamoDB, Lambda, EMR, ECS, EKS, ELBv2, Secrets Manager, SQS, SNS, CloudWatch, Route53
- **Config-driven**: YAML configuration for services, regions, filters, and output
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

The TUI supports 12 built-in color themes, all named after birds. Set the active theme in `config.yaml` under `ui.theme` or with the `--theme` flag on the S3 subcommand.

| Theme Name | Description |
|------------|-------------|
| `spotted-pardalote` | Purple/violet (default) |
| `plains-wanderer` | Green on dark |
| `bee-eater` | Teal/cyan |
| `rose-crowned-fruit-dove` | Navy and rose |
| `eastern-rosella` | Yellow on dark navy |
| `oriole` | Mint green on black |
| `princess-parrot` | Pink on forest green |
| `superb-fairy-wren` | Dodger blue on brown |
| `cassowary` | Midnight blue and crimson |
| `yellow-robin` | Gold on grey |
| `galah` | Pink on grey |
| `blue-winged-kookaburra` | Royal blue on chocolate |

Each theme configures nine color roles: `heading`, `text`, `background`, `border`, `highlight`, `highlightText`, `muted`, `error`, `warning`. You can override any role in `config.yaml`:

```yaml
ui:
  theme: oriole
  themes:
    oriole:
      heading: "#34E0A1"
      error: "#FF0000"    # override just this role
```

## Architecture

```
CLI (cobra)          ┐
                     ├─── Engine ─────┬─── Collector Registry ──┬─── EC2
TUI (bubbletea)      ┘                │                         ├─── S3
                                      │                         ├─── RDS
S3 TUI (bubbletea)  ─────────────────┤                         ├─── IAM
                                      │                         ├─── DynamoDB
                                      │                         ├─── Lambda
                                      │                         ├─── EMR
                                      │                         ├─── ECS / EKS
                                      │                         ├─── ELBv2
                                      │                         ├─── Secrets Mgr
                                      │                         ├─── SQS / SNS
                                      │                         ├─── CloudWatch
                                      │                         └─── Route53
                                      │
                                      ├─── Auth (5 methods)
                                      ├─── Config (viper + YAML)
                                      ├─── Filtering (region/tag/state)
                                      └─── Output (table / JSON streaming)
```

The `Engine` orchestrates concurrent collection via a bounded goroutine pool, running each `(service, region)` pair in parallel. Global services run once. Results stream back incrementally via a channel so the CLI can print and the TUI can render as data arrives.

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
│   └── s3.go            # S3 browser TUI launcher
├── internal/
│   ├── auth/            # AWS credential building (5 auth methods)
│   ├── awserr/          # AWS error mapping + IAM permission hints
│   ├── config/          # Configuration structs (YAML marshaling)
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
│   ├── table/           # Terminal table rendering utilities
│   ├── tui/             # Main TUI model (sidebar, table, detail panel, search)
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

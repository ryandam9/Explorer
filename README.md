# AWS Explorer

Discover, monitor, and display AWS resources across accounts and regions via CLI or TUI.

## Features

- **Two modes**: CLI (for automation/CI) and TUI (for interactive exploration)
- **Multi-service**: EC2 (instances, VPCs), S3 (buckets), RDS (DB instances), IAM (roles)
- **Config-driven**: YAML configuration for services, regions, filters, and output
- **Flexible auth**: Supports profiles, environment variables, SSO, and credential files
- **Output formats**: Table, JSON (CSV planned)
- **Filtering**: By region, state, tags, name, and IDs
- **Concurrent**: Bounded goroutine pool for parallel collection

## Quick Start

```bash
# Build
go build -o bin/aws_explorer main.go

# Run CLI (default)
./bin/aws_explorer

# Run with options
./bin/aws_explorer --config config.yaml --profile dev --output json

# Run TUI
./bin/aws_explorer tui
```

## Configuration

Edit `config.yaml` to control which services, regions, and filters are active:

```yaml
aws:
  regions:
    - us-east-1
    - us-west-2
services:
  ec2:
    enabled: true
    resources:
      - instances
      - vpcs
  s3:
    enabled: true
  rds:
    enabled: true
  iam:
    enabled: true
```

## Architecture

```
CLI (cobra) ──┐
              ├── Engine ──┬── Collector Registry ──┬── EC2
TUI (bubbletea) ──┘         │                       ├── S3
                            │                       ├── RDS
                            │                       └── IAM
                            ├── Config (viper + yaml)
                            ├── Filtering
                            └── Output (table/json)
```

Collectors implement a common `Collector` interface. Adding a new AWS service requires only a new package under `internal/services/` implementing that interface.

## Project Structure

```
├── cmd/            # CLI commands (root, tui)
├── internal/
│   ├── config/     # Configuration structs
│   ├── engine/     # Orchestration engine
│   ├── model/      # Data models (resource, result, filter)
│   ├── output/     # Output formatting
│   ├── services/   # Collector interface + per-service implementations
│   └── tui/        # Bubble Tea TUI model
├── main.go         # Entry point
├── config.yaml     # Default configuration
├── requirements.md # Project requirements
└── specification.md# Detailed specification
```

## Dependencies

- [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) — AWS SDK
- [cobra](https://github.com/spf13/cobra) — CLI framework
- [viper](https://github.com/spf13/viper) — Configuration
- [bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [bubbles](https://github.com/charmbracelet/bubbles) — TUI components
- [lipgloss](https://github.com/charmbracelet/lipgloss) — Terminal styling

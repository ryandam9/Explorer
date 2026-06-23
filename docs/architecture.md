# Architecture

```
CLI (cobra)     ┐
                ├── Engine ──┬── Collector Registry ──┬── EC2        ┐
TUI (bubbletea) ┘            │                        ├── S3         │
                            │                        ├── RDS        │
                            ├── Auth (5 methods)      ├── IAM        │ 29 service
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


# Project Structure

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
│   ├── services/        # Collector interface, registry, 29 service implementations
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


# Dependencies

| Package | Purpose |
|---------|---------|
| [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS SDK for Go v2 (per-service modules + STS/SSO) |
| [cobra](https://github.com/spf13/cobra) | CLI framework |
| [viper](https://github.com/spf13/viper) | Configuration loading |
| [bubbletea](https://github.com/charmbracelet/bubbletea) | TUI framework |
| [bubbles](https://github.com/charmbracelet/bubbles) | TUI components (spinner, list, viewport) |
| [huh](https://github.com/charmbracelet/huh) | TUI forms |
| [lipgloss](https://github.com/charmbracelet/lipgloss) | Terminal styling |
| [bubble-table](https://github.com/evertras/bubble-table) | TUI table component |
| [clipboard](https://github.com/atotto/clipboard) | Copy resource IDs to clipboard |
| [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync) | Bounded goroutine pool (errgroup) |

[← Documentation index](index.md)

# aws_explorer ecs

ECS triage helpers

ECS-specific subcommands. Currently: "stopped" triages recently stopped tasks.

## Usage

```
aws_explorer ecs
```

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--all-regions` | — | scan all available AWS regions |
| `--auth-method` | — | auth method: auto, profile, env, static, sts (overrides aws.authMethod in config) |
| `--config` | — | config file (default: ./config.yaml, then the user config dir, then built-in defaults) |
| `--no-header` | — | omit the header row in table and csv output |
| `--output` / `-o` | table | output format: table, json, ndjson, csv |
| `--profile` | — | AWS named profile (overrides aws.profile in config) |
| `--region` / `-r` | — | scan only this region (overrides aws.regions, --all-regions and region filters) |
| `--role-arn` | — | IAM role ARN to assume via STS (sets auth method to sts) |

## Subcommands

- [`aws_explorer ecs stopped`](ecs_stopped.md) — Triage recently stopped ECS tasks ("why did my task stop?")

---

_Part of [`aws_explorer`](cli.md)._

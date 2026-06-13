[← Documentation index](index.md)

# aws_explorer iam

IAM / access debugging helpers

Helpers for the most common AWS support question: "why am I denied?".

Currently: decode — turn an "Encoded authorization failure message" blob into
a readable verdict.

## Usage

```
aws_explorer iam
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

- [`aws_explorer iam can`](iam_can.md) — Simulate IAM policy: "can X do Y on Z?"
- [`aws_explorer iam decode`](iam_decode.md) — Decode an "Encoded authorization failure message"

---

_Part of [`aws_explorer`](cli.md)._

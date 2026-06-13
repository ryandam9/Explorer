[← Documentation index](index.md)

# aws_explorer config init

Write a starter config.yaml with the built-in defaults

## Usage

```
aws_explorer config init [flags]
```

## Examples

```bash
# Scaffold ./config.yaml in the current directory
aws_explorer config init

# Scaffold the per-user config used from any directory
aws_explorer config init --path ~/.config/aws_explorer/config.yaml
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--force` | — | overwrite an existing file |
| `--path` | — | where to write the config file (default ./config.yaml) |

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

---

_Part of [`aws_explorer`](cli.md)._

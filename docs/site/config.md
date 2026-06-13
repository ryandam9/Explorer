[← Documentation index](index.md)

# aws_explorer config

Manage the configuration file

Inspect or scaffold the aws_explorer configuration.

The configuration is searched in this order: the --config flag, ./config.yaml,
the user config directory (e.g. ~/.config/aws_explorer/config.yaml), and
finally the defaults built into the binary.

## Usage

```
aws_explorer config
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

- [`aws_explorer config init`](config_init.md) — Write a starter config.yaml with the built-in defaults
- [`aws_explorer config path`](config_path.md) — Print the path of the active configuration file

---

_Part of [`aws_explorer`](cli.md)._

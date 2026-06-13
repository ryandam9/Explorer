[← Documentation index](index.md)

# aws_explorer tui

Start the interactive TUI mode

Start the Text User Interface (TUI) for interactive exploration of AWS
resources, with live scanning, filtering, sorting, detail views, CSV export
and snapshot diffing.

## Usage

```
aws_explorer tui [flags]
```

## Examples

```bash
# Explore live AWS resources
aws_explorer tui --profile prod

# Browse a saved snapshot offline (no credentials needed)
aws_explorer tui --snapshot inventory.json

# Diff two snapshots
aws_explorer tui --diff before.json,after.json
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--diff` | — | Paths to two saved snapshots to diff (comma-separated or multiple flags) |
| `--snapshot` | — | Path to a saved inventory snapshot JSON to view offline |

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

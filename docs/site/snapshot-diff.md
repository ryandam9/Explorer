[← Documentation index](index.md)

# aws_explorer snapshot-diff

Browse a saved inventory snapshot, or diff two snapshots, offline

snapshot-diff opens the interactive TUI over saved inventory snapshots —
no AWS credentials, STS calls or region discovery needed.

Pass --snapshot to browse a single saved snapshot, or --diff to compare two
snapshots and explore what was added, removed or modified between them.
Snapshots are the JSON written by 'summary -o json'.

To explore live AWS resources interactively, use 'summary --tui' instead.

## Usage

```
aws_explorer snapshot-diff [flags]
```

## Examples

```bash
# Browse a saved snapshot offline (no credentials needed)
aws_explorer snapshot-diff --snapshot inventory.json

# Diff two snapshots
aws_explorer snapshot-diff --diff before.json,after.json
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

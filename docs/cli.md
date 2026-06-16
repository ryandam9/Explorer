# CLI Usage

The default command streams discovered resources to stdout as a table, JSON,
NDJSON or CSV.

```bash
./bin/aws_explorer [flags]
```

While the scan runs, a live progress meter (`⠿ scanning 12/56 tasks · 340
resources`) is shown on stderr — only when stderr is a terminal, so piping
stdout stays clean. Collection errors are summarized after the run,
deduplicated across regions. Resource states are colored when stdout is a
terminal (disable with [`NO_COLOR`](https://no-color.org/) or by piping).

### Global flags (work on every command)

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | auto-discovered | Path to config file (default search: `./config.yaml`, then the user config dir, then built-in defaults) |
| `--profile` | `default` | AWS named profile |
| `--auth-method` | `auto` | Auth method: `auto`, `profile`, `env`, `static`, `sts` |
| `--role-arn` | — | IAM role ARN to assume (sets auth method to `sts`) |
| `--region` / `-r` | — | Scan only this region (overrides `aws.regions`, `--all-regions` and region filters) |
| `--output` / `-o` | `table` | Output format: `table`, `json`, `ndjson`, `csv` |
| `--no-header` | `false` | Omit the header row in `table`/`csv` output (for scripting) |
| `--all-regions` | `false` | Scan all available AWS regions |
| `--version` | — | Print version, commit and build date |

### Examples

```bash
# Use a named AWS profile
./bin/aws_explorer --profile prod

# Pin to one region
./bin/aws_explorer -r eu-west-1

# Machine-readable output
./bin/aws_explorer -o json | jq '.[].id'
./bin/aws_explorer -o ndjson | head
./bin/aws_explorer -o csv --no-header > resources.csv

# Scan all regions
./bin/aws_explorer --all-regions

# Assume an IAM role
./bin/aws_explorer --role-arn arn:aws:iam::123456789012:role/MyRole

# Custom config file
./bin/aws_explorer --config /path/to/config.yaml
```

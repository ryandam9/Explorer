[← Documentation index](index.md)

# aws_explorer cw

Start the CloudWatch Logs Explorer TUI

Start an interactive TUI for exploring, filtering, searching and tailing
CloudWatch log groups, streams and events.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region and adds a Region column to the group
list; otherwise the config's aws.regions list is used.

## Usage

```
aws_explorer cw [flags]
```

## Examples

```bash
# Browse log groups in one region
aws_explorer cw --region us-east-1

# Open a group and tail events matching a pattern
aws_explorer cw -g /aws/lambda/my-fn -f ERROR
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--filter` / `-f` | — | Initial query pattern for log events |
| `--group` / `-g` | — | Initial CloudWatch log group filter/pattern |
| `--stream` / `-s` | — | Initial CloudWatch log stream filter |
| `--theme` | spotted-pardalote | Color theme (spotted-pardalote, plains-wanderer, bee-eater, rose-crowned-fruit-dove, eastern-rosella, oriole, princess-parrot, superb-fairy-wren, cassowary, yellow-robin, galah, blue-winged-kookaburra) |

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

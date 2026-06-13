[← Documentation index](index.md)

# aws_explorer vpc

Start the VPC Explorer TUI

Start an interactive TUI for exploring VPCs and their associated resources
across regions: subnets, security groups, route tables, gateways, endpoints,
NACLs, peering, flow logs and attached compute, plus the VPC debugging
toolkit (findings linter, path tracer, exposure audit, snapshot diff).

## Usage

```
aws_explorer vpc [flags]
```

## Examples

```bash
# Explore VPCs in one region
aws_explorer vpc --region us-east-1

# Sweep every region with a named profile
aws_explorer vpc --all-regions --profile prod
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
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

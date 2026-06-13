[← Documentation index](index.md)

# aws_explorer ecs stopped

Triage recently stopped ECS tasks ("why did my task stop?")

Stopped answers the perennial "why did my task stop?" ticket. For each
recently stopped task it prints the task-level stop reason and the failing
container's exit code, with the exit code translated into plain English:

  - 137 → possible OOM-kill (128+SIGKILL); raise memory or fix the leak
  - 139 → segfault (128+SIGSEGV)
  - 143 → SIGTERM (often a normal shutdown)
  - container reason mentioning memory → out-of-memory

It scans every cluster in scope by default, or just one with --cluster (name
or ARN). The report is read-only and best-effort: a denied or failed API call
skips that region (reported on stderr) and never aborts the run.

Note: ECS retains stopped tasks for roughly one hour. An empty report means
nothing stopped in that window, not that nothing ever fails — run it soon
after the failure.

Needs ecs:ListClusters, ecs:ListTasks and ecs:DescribeTasks.

## Usage

```
aws_explorer ecs stopped [flags]
```

## Examples

```bash
# Triage stopped tasks across all in-scope regions
aws_explorer ecs stopped --all-regions

# Just one cluster
aws_explorer ecs stopped --cluster my-cluster -r us-east-1

# Machine-readable; find the OOM-kills
aws_explorer ecs stopped -o json | jq '[.[] | select(.exit_code == 137)]'
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--cluster` | — | limit to one cluster (name or ARN); default scans every cluster in scope |

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

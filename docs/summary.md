# Summary Usage

`summary` produces a single, numbered inventory of **every** discovered resource
across all configured regions, spanning **all AWS services** — not just the ones
with a built-in collector.

It combines two sources and merges them by ARN:

1. **The 29 typed collectors** (EC2, S3, RDS, …) for rich data — state,
   availability zone, and service-specific summary fields. See
   [collectors.md](collectors.md) for exactly which resource types
   each collector covers, its required IAM, and known gaps.
2. **A universal sweep via the [Resource Groups Tagging API]** (`tag:GetResources`),
   which returns ARNs and tags for taggable resources across hundreds of
   services in each region. This is what gives the long tail (KMS keys, subnets,
   EBS volumes, Step Functions, API Gateways, CloudFront, …) coverage without a
   bespoke collector per service.

When both sources describe the same ARN, the richer typed entry wins. Use
`--typed-only` to skip the universal sweep.

In **multi-account** mode (a `config.accounts` list), the universal sweep runs
**per account** — the same fan-out the typed collectors use — so the inventory
is complete across every configured account and discovered resources carry the
same account label as typed results. A bad/denied account is flagged and skipped
without hiding the others.

> **Coverage & permissions.** The Tagging API only returns resources that
> support tagging and are registered with the tagging service — broad, but not
> literally 100% of every service. The sweep needs the `tag:GetResources` IAM
> permission; if it's denied, the typed-collector results are still shown.

Under the table (and behind `c` in the `--tui`), summary lists the common
services that produced nothing, as a reminder that an untagged — or simply
absent — resource can be missing. That list is configurable: add your own
services under `summary.commonservices` in `config.yaml` (merged on top of the
built-in list), keyed by the AWS service name with a friendly label:

```yaml
summary:
  commonservices:
    apprunner: App Runner
    sagemaker: SageMaker
  hideservices:        # drop entries (built-in or added) that are just noise
    - glue
    - athena
```

[Resource Groups Tagging API]: https://docs.aws.amazon.com/resourcegroupstagging/latest/APIReference/API_GetResources.html

Each row carries five columns:

| Column | Description |
|--------|-------------|
| `SNO` | Serial number (1-based, assigned after sorting) |
| `NAME` | Resource name (bucket name, EC2 `Name` tag, VPC name, …) or `-` when none |
| `TYPE` | Resource type as `service/type` (e.g. `ec2/instance`, `s3/bucket`) |
| `ARN` | Full ARN — returned by AWS where available, otherwise constructed |
| `REGION/AZ` | Region, plus the availability zone for zonal resources (e.g. `us-east-1 / us-east-1a`) |

```bash
./bin/aws_explorer summary [flags]
```

Accepts the same global flags as the CLI command (`--config`, `--profile`,
`--auth-method`, `--role-arn`, `--region`, `--all-regions`).

### Summary Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output` / `-o` | `table` | Output format: `table`, `json`, `ndjson`, or `csv` |
| `--tui` | `false` | Explore the same inventory interactively instead of printing |
| `--typed-only` | `false` | Skip the all-services Tagging API sweep; use only the built-in typed collectors |
| `--baseline` | `false` | Save this scan as the account's baseline snapshot |
| `--diff` | `false` | Diff this scan against the saved baseline — "what changed since" |

### Examples

```bash
# Table of every resource in every region
./bin/aws_explorer summary --all-regions

# Export the inventory as CSV
./bin/aws_explorer summary --all-regions -o csv > inventory.csv

# As JSON
./bin/aws_explorer summary -o json

# Explore interactively
./bin/aws_explorer summary --tui
```

> Constructing ARNs for resources AWS doesn't return them for (EC2, S3, SQS, …)
> requires the account ID, which is resolved once via `sts:GetCallerIdentity`.
> If that call is denied, those ARNs are shown as `-` while AWS-provided ARNs
> still appear.

### Account snapshot diff — "what changed since yesterday?"

The account-level twin of the VPC explorer's snapshot diff: baseline the
whole merged-by-ARN inventory, then diff a later scan against it.

```bash
aws_explorer summary --baseline          # save the baseline
aws_explorer summary --diff              # later: what changed?
aws_explorer summary --diff -o json      # for automation
```

```
Changes since baseline 2026-06-11 09:00 UTC — 2 added, 1 removed, 1 modified
+ ec2/instance      i-0abc (web-3)        us-east-1
+ lambda/function   new-payments-fn       us-east-1
- s3/bucket         old-logs-bucket       global
~ ec2/instance      i-0def (web-2)        us-east-1   state: running → stopped; tag Env: dev → prod
```

- Baselines are stored under `~/.aws_explorer/account-snapshots/<account-id>/`,
  one file per **region scope** — diffing with a different `-r`/`--all-regions`
  scope than the baseline refuses with a hint instead of reporting everything
  as removed.
- Only stable fields are compared (name, state, tags); volatile detail fields
  are excluded, so an unchanged account always diffs clean and the output is
  deterministic.
- In the summary TUI the same feature lives behind **`D`**: the first press
  saves a baseline, later presses open the "what changed" overlay, and `b`
  inside it re-baselines.

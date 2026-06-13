[← Documentation index](index.md)

# aws_explorer iam can

Simulate IAM policy: "can X do Y on Z?"

Can runs iam:SimulatePrincipalPolicy for a principal (role or user ARN) and
renders the verdict step by step — allowed, implicit deny (no policy allows
it), or explicit deny (a policy forbids it; removing an allow elsewhere will
not help) — naming the matched policy statements and whether a permissions
boundary is the limiting factor.

The action accepts a comma-separated list ("s3:GetObject,s3:PutObject") to
check several at once. The resource ARN is optional; without it the action
is simulated against "*".

The simulator's blind spots are printed with every verdict: resource-based
policies, session policies, and unsupplied condition keys are NOT evaluated,
so a real request can still differ.

Requires the iam:SimulatePrincipalPolicy (and iam:GetRole/GetUser for ARN
resolution) IAM permissions. IAM is global; no region needed.

## Usage

```
aws_explorer iam can <principal-arn> <action> [resource-arn]
```

## Examples

```bash
# Why can't this role read the bucket?
aws_explorer iam can arn:aws:iam::123456789012:role/app s3:GetObject arn:aws:s3:::my-bucket/key

# Several actions at once, no specific resource
aws_explorer iam can arn:aws:iam::123456789012:role/app ec2:StartInstances,ec2:StopInstances

# Machine-readable
aws_explorer iam can arn:aws:iam::123456789012:user/alice s3:PutObject -o json
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

---

_Part of [`aws_explorer`](cli.md)._

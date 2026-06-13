[← Documentation index](index.md)

# aws_explorer whereused

Where-used / blast radius — "can I delete this?"

Whereused answers "can I delete this?" for the resources people actually ask
about: IAM roles, KMS keys, ACM certificates and security groups. It scans the
account for the linking fields the inventory does not keep — a Lambda's
execution role, a volume's KMS key, a listener's certificate, an ENI's
security groups — and lists every resource that references the target.

Pass a full ARN or a bare ID:

  - IAM role     arn:aws:iam::123456789012:role/app   (or just the role name)
  - KMS key      arn:aws:kms:us-east-1:…:key/<uuid>
  - ACM cert     arn:aws:acm:us-east-1:…:certificate/<id>
  - Security grp sg-0abc123                            (or its ARN)

Crucially, a "not referenced" answer is scoped: the report always lists the
reference types it checked, so absence of evidence is never presented as proof
of absence. The report is read-only and best-effort — a denied or failed API
call narrows what was checked (reported on stderr) and never aborts the run.

This is the CLI generalization of the summary TUI's 'x' cross-reference.

## Usage

```
aws_explorer whereused <arn-or-id>
```

## Examples

```bash
# What uses this IAM role?
aws_explorer whereused arn:aws:iam::123456789012:role/app-task

# What encrypts with this KMS key, across all regions?
aws_explorer whereused arn:aws:kms:us-east-1:123456789012:key/abcd-… --all-regions

# What is this security group attached to?
aws_explorer whereused sg-0abc123 -r eu-west-1

# Machine-readable
aws_explorer whereused sg-0abc123 -o json | jq '.references'
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

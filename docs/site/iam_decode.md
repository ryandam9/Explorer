[← Documentation index](index.md)

# aws_explorer iam decode

Decode an "Encoded authorization failure message"

Services like EC2 redact the reason for an authorization failure into an
opaque blob ("Encoded authorization failure message: <blob>"). decode calls
sts:DecodeAuthorizationMessage and prints a human summary — the principal,
the denied action, the resource, and whether it was an explicit deny or a
missing allow — followed by the full decoded JSON document.

The message is read from the argument, or from stdin when the argument is "-"
or omitted. Pasting the entire error message works; the blob is extracted.

Requires the sts:DecodeAuthorizationMessage IAM permission.

## Usage

```
aws_explorer iam decode [encoded-message]
```

## Examples

```bash
# Decode a blob directly
aws_explorer iam decode AQoDYXdzEJr...

# Pipe the whole error message in
pbpaste | aws_explorer iam decode

# Just the decoded JSON, for jq
aws_explorer iam decode AQoDYXdzEJr... -o json
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

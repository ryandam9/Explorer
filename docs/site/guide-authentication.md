[← Documentation index](index.md)

# Authentication

AWS Explorer never stores credentials of its own — it uses the AWS SDK's
credential resolution, configured by a single `authMethod` choice. Set it in
`config.yaml` under `aws.authMethod`, or override per-run with the global
`--auth-method` flag.

## The five methods

| Method | Description |
|--------|-------------|
| `auto` | AWS SDK default chain: env vars → `~/.aws` credentials/config → EC2/ECS instance metadata. The default. |
| `profile` | A named profile from `~/.aws/credentials` or `~/.aws/config` (`--profile NAME`) |
| `env` | Only `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` environment variables |
| `static` | Plaintext credentials in `config.yaml` under `aws.static` (avoid committing real keys) |
| `sts` | Assume an IAM role via STS; base credentials come from the profile/env/default chain |

The relevant global flags, usable on every command:

| Flag | Purpose |
|------|---------|
| `--profile` | AWS named profile (overrides `aws.profile`) |
| `--auth-method` | `auto`, `profile`, `env`, `static`, `sts` |
| `--role-arn` | IAM role ARN to assume — implies `--auth-method sts` |
| `--region` / `-r` | Pin a single region (overrides config and `--all-regions`) |
| `--all-regions` | Scan every available region |

## STS AssumeRole

```yaml
aws:
  authMethod: sts
  sts:
    roleArn: arn:aws:iam::123456789012:role/ReadOnlyRole
    sessionName: aws-explorer
    externalId: my-external-id    # if required by the trust policy
    durationSeconds: 3600
```

Or entirely from flags:

```bash
aws_explorer --auth-method sts --role-arn arn:aws:iam::123456789012:role/ReadOnlyRole
```

## Expired SSO sessions

When an AWS SSO (IAM Identity Center) session expires — or you were never
logged in — every command surfaces the exact fix instead of the SDK's raw
error chain:

```
✗ AWS SSO session for profile 'prod' is expired or missing — run: aws sso login --profile prod
```

The same one-liner appears in TUI error overlays and as `ExpiredCredentials`
errors in `-o json` output. Plain expired STS/session tokens get an analogous
"credentials have expired" hint. Genuinely *missing* credentials (no IMDS
role, no env vars) are deliberately **not** rewritten — you see the real error.

## Least privilege

Every command is read-only against your account (the one paid, opt-in
exception is the VPC Reachability Analyzer, which can *create* analyses, and
Cost Explorer requests in `bill`, which AWS bills at $0.01 each). Collection
is best-effort: a denied API call skips just the affected data and is reported,
rather than aborting the run.

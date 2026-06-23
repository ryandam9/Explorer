# Authentication

Five methods are supported, configured via `authMethod` in `config.yaml` or `--auth-method` on the CLI:

| Method | Description |
|--------|-------------|
| `auto` | AWS SDK default chain: env vars → `~/.aws` credentials/config → EC2/ECS instance metadata |
| `profile` | Named profile from `~/.aws/credentials` or `~/.aws/config` |
| `env` | Only `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` environment variables |
| `static` | Plaintext credentials in `config.yaml` under `aws.static` (avoid committing real keys) |
| `sts` | Assume an IAM role via STS; base credentials come from profile/env/default chain |

### Choosing a method (prefer short-lived credentials)

Prefer, in order: **SSO** (IAM Identity Center) → **named profiles** → **env vars
/ assumed roles (STS)**. These give short-lived, rotatable credentials.

> ⚠️ **`static` is a last resort.** Long-lived access keys in `config.yaml` are
> plaintext and easy to leak. Use them only when no other method is available,
> and **never commit them** — the repository's `.gitignore` excludes the common
> local secret files (`.env`, `*.local.yaml`, …), and `config.yaml` should not
> contain real keys. See [`../SECURITY.md`](../SECURITY.md) for the full policy.

### Expired SSO sessions

When an AWS SSO (IAM Identity Center) session expires — or you were never
logged in — every command surfaces the exact fix instead of the SDK's raw
error chain:

```
✗ AWS SSO session for profile 'prod' is expired or missing — run: aws sso login --profile prod
```

The same one-liner appears in TUI error overlays and as `ExpiredCredentials`
errors in `-o json` output. Plain expired STS/session tokens get an analogous
"credentials have expired" hint. Genuinely *missing* credentials (e.g. no
IMDS role, no env vars) are deliberately **not** rewritten — you see the real
error.

### STS AssumeRole Example

```yaml
aws:
  authMethod: sts
  sts:
    roleArn: arn:aws:iam::123456789012:role/ReadOnlyRole
    sessionName: aws-explorer
    externalId: my-external-id    # if required by the trust policy
    durationSeconds: 3600
```

Or via CLI flag:

```bash
./bin/aws_explorer --auth-method sts --role-arn arn:aws:iam::123456789012:role/ReadOnlyRole
```

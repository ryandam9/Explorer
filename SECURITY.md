# Security Policy

## Reporting a vulnerability

Please report security issues **privately** — do not open a public issue for
anything exploitable.

- Preferred: open a [GitHub private security advisory](https://github.com/ryandam9/aws_explorer/security/advisories/new)
  ("Report a vulnerability").
- Alternative: email the maintainer at **ryandam.explorer@gmail.com** with
  details and reproduction steps.

Please include the version (`aws_explorer --version`), the command/TUI involved,
and the smallest steps that reproduce the problem. We'll acknowledge receipt and
keep you updated on the fix.

## Scope & design posture

`aws_explorer` is a **read-only** explorer by design (see `CLAUDE.md` §2/§14):

- Anything that mutates AWS, leaves the AWS API, or incurs a charge is opt-in
  and gated behind an explicit, cost-stating confirmation (e.g. the guarded S3
  delete behind `--allow-delete`).
- Secret-looking values are redacted in rendered config/args.

If you find a path that mutates, exfiltrates, or spends without that explicit
opt-in, treat it as a security issue and report it as above.

## Handling AWS credentials safely

This tool uses your local AWS credentials via the standard SDK chain. Prefer, in
order:

1. **AWS IAM Identity Center (SSO)** — `aws sso login`, then a profile.
2. **Named profiles** in `~/.aws/config` / `~/.aws/credentials`.
3. **Environment variables** / **assumed roles (STS)** for short-lived creds.

**Do not store long-lived access keys in this project's `config.yaml`.** Static
credentials are supported only as a last resort; when you must use them, keep
them out of version control. The repository's `.gitignore` excludes the common
local secret files (`.env`, `.env.*`, `*.local.yaml`, …), and enabling GitHub
secret scanning / push protection on forks is recommended.

See [`docs/authentication.md`](docs/authentication.md) for the full auth guide.

# aws_explorer documentation

Discover and list AWS resources across accounts and regions

This documentation is generated from the tool itself — the command reference below is built from the live command tree, so it always matches the binary you are running. New in this release: regenerate it any time with `aws_explorer docs --format html` (or `markdown`).

## Guides

- [Getting started](guide-getting-started.md) — AWS Explorer discovers, monitors and lists AWS resources — EC2, S3, RDS,.
- [Authentication](guide-authentication.md) — AWS Explorer never stores credentials of its own — it uses the AWS SDK's.
- [Configuration](guide-configuration.md) — Configuration is **optional**.
- [The summary TUI](guide-summary.md) — Start it with `aws_explorer summary --tui`.
- [VPC explorer TUI](guide-vpc.md) — and attached resources: pick a VPC on the left, a resource category in the.
- [S3 browser TUI](guide-s3.md) — navigation, metadata and version viewing, preview, download, presigned URLs,.
- [CloudWatch Logs TUI](guide-cloudwatch.md) — streams and events, with filtering, search and live tailing.
- [Audit & Bill TUIs](guide-reports.md) — Two report commands have a `--tui` mode that turns a one-shot report into an.
- [Keyboard shortcut cheat sheet](guide-shortcuts.md) — Every TUI shares the same interaction language: a context-aware status bar.

## Command reference

- [`aws_explorer`](cli.md) — Discover and list AWS resources across accounts and regions.
- [`aws_explorer audit`](audit.md) — Scan the account for cost waste and security risks (findings linter).
- [`aws_explorer bill`](bill.md) — Show the account's bill from Cost Explorer (live --tui).
- [`aws_explorer config`](config.md) — Manage the configuration file.
- [`aws_explorer config init`](config_init.md) — Write a starter config.yaml with the built-in defaults.
- [`aws_explorer config path`](config_path.md) — Print the path of the active configuration file.
- [`aws_explorer cw`](cw.md) — Start the CloudWatch Logs Explorer TUI.
- [`aws_explorer ecs`](ecs.md) — ECS triage helpers.
- [`aws_explorer ecs stopped`](ecs_stopped.md) — Triage recently stopped ECS tasks ("why did my task stop?").
- [`aws_explorer expiring`](expiring.md) — List everything that breaks on a calendar date.
- [`aws_explorer find`](find.md) — Fuzzy-find any resource by name, ID, ARN or type.
- [`aws_explorer iam`](iam.md) — IAM / access debugging helpers.
- [`aws_explorer iam can`](iam_can.md) — Simulate IAM policy: "can X do Y on Z?".
- [`aws_explorer iam decode`](iam_decode.md) — Decode an "Encoded authorization failure message".
- [`aws_explorer s3`](s3.md) — Start the S3 Explorer TUI.
- [`aws_explorer snapshot-diff`](snapshot-diff.md) — Browse a saved inventory snapshot, or diff two snapshots, offline.
- [`aws_explorer summary`](summary.md) — List every AWS resource across all regions.
- [`aws_explorer trail`](trail.md) — CloudTrail "who changed this" — recent events for a resource.
- [`aws_explorer vpc`](vpc.md) — Start the VPC Explorer TUI.

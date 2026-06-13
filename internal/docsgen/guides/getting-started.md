AWS Explorer discovers, monitors and lists AWS resources — EC2, S3, RDS,
Lambda and a dozen more services — across accounts and regions, from the
command line or one of several interactive terminal UIs (TUIs).

This documentation is generated from the tool itself: the command reference
below is built from the live command tree, so it always matches the binary
you are running. The guides cover the interactive screens and their keyboard
shortcuts, which the command reference cannot show on its own.

## Install

```bash
# Install the latest release
go install github.com/ryandam9/aws_explorer@latest

# …or clone and build
git clone https://github.com/ryandam9/aws_explorer.git
cd aws_explorer
make build          # produces bin/aws_explorer
```

## The four modes

| Mode | How to start | What it is |
|------|--------------|------------|
| **CLI** | `aws_explorer` (and most subcommands) | Streams a table / JSON / NDJSON / CSV to stdout — scriptable, no UI |
| **Summary TUI** | `aws_explorer tui` | One interactive, filterable inventory of every resource — see [The summary TUI](guide-summary.md) |
| **Focused TUIs** | `aws_explorer vpc` · `s3` · `cw` | Dedicated explorers for a VPC's networking, S3, and CloudWatch Logs |
| **Report TUIs** | `aws_explorer audit --tui` · `bill --tui` | Live, explorable cost/security findings and the account bill |

## First runs

```bash
# Stream every resource in the configured regions to the terminal
aws_explorer

# The same inventory, but interactive
aws_explorer tui

# Scan every region and write the inventory to CSV
aws_explorer summary --all-regions -o csv > inventory.csv

# Audit the account for cost waste and security risks
aws_explorer audit
```

Configuration is optional — with no `config.yaml` present the tool runs on
built-in defaults, so it works from any directory with zero setup. When you
do want to pin services, regions, columns or themes, see
[Configuration](guide-configuration.md). For credentials and profiles, see
[Authentication](guide-authentication.md).

## Where to go next

- [Authentication](guide-authentication.md) — profiles, SSO, env vars, AssumeRole
- [Configuration](guide-configuration.md) — the `config.yaml` reference and themes
- [Keyboard shortcut cheat sheet](guide-shortcuts.md) — every TUI's keys in one place
- The **Command reference** (in the navigation) — one page per command, with flags and examples

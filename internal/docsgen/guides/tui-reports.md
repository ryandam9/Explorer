Two report commands have a `--tui` mode that turns a one-shot report into an
explorable, filterable screen: the cost/security **audit** and the live
**bill**.

## Audit TUI

`aws_explorer audit --tui` streams findings in per region as the scan runs.
Each finding carries a stable check ID (e.g. `COST-EBS-001`, `SEC-S3-001`), a
severity, the resource, and — for cost findings — an estimated monthly cost,
with a running total of potential savings in the header. See the
[`audit` command reference](audit.md) for categories and flags.

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate findings |
| `Enter` | Detail overlay for the selected finding (why + how to fix) |
| `/` | Filter (matches any field, live `matched/total`) |
| `s` / `R` | Sort by the next column / reverse |
| `r` | Reset filter and sort |
| `<` / `>` | Scroll columns on narrow terminals |
| `y` | Copy the finding's ARN / resource |
| `C` | Export the current view to CSV |
| `e` | Show collection errors (skipped checks) |
| `?` / `q` | Help / quit |

## Bill TUI

`aws_explorer bill --tui` is a **live** screen: it re-fetches the account's
cost from the AWS Cost Explorer API on a fixed interval (`--interval`, default
5m), so activity that incurs cost surfaces without restarting. A `CHANGE`
column shows what each line moved since the previous refresh, and `x` drills into a
service's per-resource costs (resource ID/ARN) when the account has
resource-level data enabled. See the [`bill` command reference](bill.md).

> **PAID feature.** Cost Explorer bills **$0.01 per request**, including every
> automatic refresh. The live screen carries a `PAID` badge and names the
> cadence; the minimum interval is 1 minute. Raise `--interval` to spend less.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate bill lines |
| `Enter` | Detail overlay for the selected line |
| `x` | Per-resource breakdown for the selected service |
| `u` | Refresh now (one paid request) |
| `/` | Filter by service, usage type or unit |
| `s` / `R` | Sort by the next column / reverse |
| `y` | Copy the selected service and usage type |
| `C` | Export the current view to CSV |
| `?` / `q` | Help / quit |

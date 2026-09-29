# Bill Usage

`bill` shows the account's actual cost from the AWS Cost Explorer API, grouped
by service and usage type, each line carrying its usage quantity and a grand
total at the bottom — the numbers the Billing console shows, not the
list-price estimates the [audit](audit.md) linter attaches to waste
findings. By default it reports the current month to date; today's partial
charges are estimated and flagged as such.

```bash
# Current month to date, grouped by service and usage type
./bin/aws_explorer bill

# A past month, machine-readable
./bin/aws_explorer bill --month 2026-05 -o json

# Live screen, re-fetching every 10 minutes
./bin/aws_explorer bill --tui --interval 10m

# CSV for a spreadsheet
./bin/aws_explorer bill -o csv --no-header > bill.csv
```

```
SNO  SERVICE                  USAGE TYPE                  USAGE     UNIT    COST
1    Amazon EC2               EBS:VolumeUsage.gp3         100       GB-Mo   $8.00
2    Amazon EC2               BoxUsage:t3.micro           744       Hrs     $1.50
3    Amazon S3                TimedStorage-ByteHrs        10        GB-Mo   $0.25
     TOTAL (estimated)        2026-06-01 → 2026-06-13                       $9.75
```

### Live screen (`--tui`)

`--tui` opens a live bill that re-fetches on a fixed interval (`--interval`,
default 5m), so activity that incurs cost surfaces without restarting — this
is the "Live screen" the page is meant to be. A `Δ` column shows what each
line moved since the previous refresh, and the header timestamps the last
update.

| Key | Action |
|-----|--------|
| `↑`/`↓` | Navigate bill lines |
| `Enter` | Detail overlay for the selected line (in the summary: filter down to that service's lines) |
| `T` | Toggle the per-service summary |
| `z` | Hide / show the lines that carry no cost |
| `x` | Per-resource breakdown for the selected service (resource ID/ARN, usage, amount) |
| `u` | Refresh now |
| `/` | Filter by service, usage type or unit |
| `s` / `R` | Sort by the next column / reverse |
| `y` | Copy the selected service and usage type |
| `C` | Export the current view to CSV |
| `?` / `q` | Help / quit |

#### Summary by service (`T`)

Cost Explorer returns one line per (service, usage type), so a real account's
bill is hundreds of rows and most of them are free tier or metered-but-not-
charged — usage with a `$0.00` amount. `T` folds them into one row per
service:

```
#   SERVICE                                    COST      SHARE
1   Amazon Elastic Compute Cloud - Compute     $4.15     41.7%
2   Amazon Elastic Container Service           $1.38     13.9%
3   AWS Secrets Manager                        $1.11     11.2%
4   Tax                                        $1.00     10.1%

8 service(s) · total $9.95 of $11.02 billed · hiding 3 services that cost nothing — z shows them
```

**Rows that cost nothing are hidden by default**, in both views: the question
this screen answers is what you are being charged for. `z` brings them back,
and the footer always says how many are out of sight — a table quietly shorter
than the bill would misreport it.

In the summary a service is hidden when its *total* is zero, which also
catches one whose charge and credit cancel out — not only one whose lines are
all free. `Enter` on a row drops back into the detailed view filtered to that
service.

Both the `SHARE` column and the footer total describe **the rows on screen**.
Under a filter (or with the free rows hidden) that is less than the whole bill,
and the footer then names the whole-bill total alongside (`total $9.95 of
$11.02 billed`) so neither number can be read as the other. `C` exports
whichever of the two tables is showing.

| Flag | Default | Description |
|------|---------|-------------|
| `--month` | current month | Billing period as `YYYY-MM`; past months cover the full month, the current month clamps to month-to-date |
| `--tui` | off | Open the live screen instead of printing once |
| `--interval` | `5m` | Auto-refresh cadence for `--tui` (minimum 1m) |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

> **Cost note.** Cost Explorer is a paid API — AWS bills every request
> (`GetCostAndUsage`, `GetCostAndUsageWithResources`) at **$0.01**, including
> each automatic refresh in `--tui`. The live screen names the cadence and its
> per-refresh cost in the header; raise `--interval` to spend less. The
> minimum interval is 1 minute because the numbers only move every few
> minutes anyway.

**IAM permissions.** Read-only: `ce:GetCostAndUsage`, plus
`ce:GetCostAndUsageWithResources` for the per-resource drill-down. Cost
Explorer is a global service; the region flags don't affect it.

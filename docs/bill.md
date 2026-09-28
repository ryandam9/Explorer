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
#   SERVICE                      COST      SHARE   LINES  NO COST
1   Amazon EC2                   $8.00     80.0%   14     11
2   Amazon S3                    $1.75     17.5%   9      7
3   AWS Lambda                   $0.25      2.5%   4      3
4   Amazon CloudWatch            $0.00      0.0%   6      6

4 service(s) · total $10.00 · 27 line(s) carry no cost — z hides them
```

`LINES` and `NO COST` are why the detailed view is long: a service can
contribute a dozen usage types and one dollar. `z` hides the zero-cost lines
in either view, and the footer always says how many are hidden — a shorter
table never passes for a shorter bill. `Enter` on a summary row drops back
into the detailed view filtered to that service.

Both the `SHARE` column and the footer total describe **the rows on screen**.
Under a filter (or with `z` on) that is less than the whole bill, and the
footer then names the whole-bill total alongside (`total $5.00 of $10.00
billed`) so neither number can be read as the other. `C` exports whichever
of the two tables is showing.

The per-resource drill-down (`x`) uses Cost Explorer's resource-level data,
which AWS keeps for the trailing **14 days** and only when the account has
opted in (Billing → Cost Management Preferences → "Daily granularity
resource-level data"). Without it, the overlay says so instead of failing.

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

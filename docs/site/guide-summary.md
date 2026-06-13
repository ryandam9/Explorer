[← Documentation index](index.md)

# The summary TUI

Start it with `aws_explorer summary --tui`. It is one interactive, filterable
table of **every** discovered
resource across all configured regions and services, with a service sidebar on
the left and a detail panel on the right.

While a scan runs, the header shows real progress (`scanning 23/60` with the
pending `service@region` tasks named) and surfaces collection errors inline: a
red `⚠ n errors` badge in the header plus per-service warning badges in the
sidebar.

## Layout

```
┌─ Services ──┬─ Resources ───────────────────────────────┬─ Detail ──────┐
│ ▸ EC2   (12)│  NAME        TYPE          STATE   REGION  │ id   i-0abc…  │
│   S3     (4)│  prod-web-3   ec2/instance running us-east │ type t3.micro │
│   RDS    (2)│  …                                         │ …             │
│   IAM  ⚠1   │                                            │               │
└─────────────┴────────────────────────────────────────────┴──────────────┘
```

## Keyboard shortcuts

The status bar at the bottom is **context-aware**: it lists only the shortcuts
usable on the current screen and panel focus, so what you see is always what
works right now.

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate rows |
| `[` / `]` | Move through the service sidebar / scroll the detail panel |
| `Tab` / `Shift+Tab` | Switch focus between sidebar, table and detail panel |
| `<` / `>` (or `,` / `.`) | Scroll table columns when wider than the panel |
| `Enter` | Select service / open the detail panel for the selected resource |
| `/` | Quick text filter (matches any column; live `matched/total` count) |
| `Ctrl+P` | **Jump to any resource**: fuzzy-search every resource (name, ID, ARN, type, region) across all services; `Enter` lands on its row |
| `f` | Advanced filter (region / state) |
| `r` | Reset all filters |
| `s` / `R` | Sort by the next column / reverse the direction |
| `y` / `Y` | Copy the selected resource's ARN / ID |
| `o` | Open the resource in the AWS console (copies a deep link; opens a browser when local) |
| `J` | Toggle a raw-JSON view in the detail panel (`y` then copies the JSON) |
| `C` | Export the current (filtered, sorted) view to CSV under `~/.aws_explorer/exports/` |
| `D` | **What changed**: first press saves a baseline snapshot, later presses diff the live inventory against it (`b` re-baselines) |
| `t` | Timeline — recent CloudTrail "who changed this" events for the selected resource |
| `P` | Switch AWS profile and/or region scope, then rescan — no restart |
| `e` | Open the scan-errors overlay |
| `S` | Settings panel (themes & colors) |
| `?` | Help overlay |
| `Esc` | Close detail panel / overlay |
| `q` / `Ctrl+C` | Quit |

## Related CLI twins

Several TUI features have a non-interactive equivalent for scripting:

- `Ctrl+P` jump → [`aws_explorer find <fragment>`](find.md)
- `D` what-changed → [`aws_explorer summary --baseline` / `--diff`](summary.md)
- `t` timeline → [`aws_explorer trail <resource>`](trail.md)

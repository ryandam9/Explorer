# TUI Usage

Interactive terminal UI with sidebar navigation, resource table, and detail
panel. Launch it over your **live** AWS resources with `summary --tui`:

```bash
./bin/aws_explorer summary --tui [flags]
```

Accepts the same global flags as the CLI command (`--config`, `--profile`,
`--auth-method`, `--role-arn`, `--region`, `--all-regions`). To browse a saved
snapshot offline instead, see [Offline snapshot browsing](#offline-snapshot-browsing-snapshot-diff).

### TUI Keyboard Shortcuts

The status bar at the bottom is **context-aware**: it lists only the shortcuts
that are usable on the current screen (and with the current panel focus), so
what you see in the bar is always what works right now.

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate rows |
| `[` / `]` | Move through the service sidebar / scroll the detail panel |
| `Tab` / `Shift+Tab` | Switch focus between sidebar, table and detail panel |
| `<` / `>` (or `,` / `.`) | Scroll table columns when the table is wider than the panel |
| `Enter` | Select service / open the detail panel for the selected resource |
| `/` | Quick text filter (matches any column; shows a live `matched/total` count) |
| `Ctrl+P` | **Jump to any resource**: fuzzy-search every collected resource (name, ID, ARN, type, region) across all services; `Enter` selects its service, lands on its row and opens the detail panel |
| `f` | Advanced filter (region / state) |
| `r` | Reset all filters |
| `s` / `R` | Sort by the next column / reverse the sort direction (`↑`/`↓` shown in the header) |
| `y` / `Y` | Copy the selected resource's ARN / ID to the clipboard |
| `o` / `k` | Open the resource in the AWS console (`o` copies the deep-link URL and opens a browser when the session is local; ARN-search fallback for unmapped types) / copy an AWS CLI reproduction command |
| `J` | Toggle a raw-JSON view in the detail panel (`y` then copies the JSON) |
| `t` / `l` / `g` / `x` | In the detail panel: CloudTrail timeline / inline recent ERROR logs / headline-metric **sparkline** (now·max·min over the last hour) / cross-references |
| `L` | In the detail panel: **open the CloudWatch Logs explorer** pre-filtered to this resource's log group (Lambda → `/aws/lambda/…`, RDS → `/aws/rds/instance/…`, EKS → `/aws/eks/…/cluster`); `q` returns you here with selection and scroll intact |
| `C` | Export the current (filtered, sorted) view to CSV under `~/.aws_explorer/exports/` |
| `D` | **What changed**: first press saves an account baseline snapshot, later presses diff the live inventory against it (`b` inside the overlay re-baselines) |
| `P` | Switch AWS profile and/or region scope, then rescan — no restart needed |
| `e` | Open the scan-errors overlay (services with errors also carry a `⚠n` badge in the sidebar) |
| `~` | **Debug activity overlay**: a live, scrollable view of what the tool is doing — regions, services, API calls and access errors — so you can see progress instead of a blank screen (available during the initial scan too) |
| `S` | Settings panel (themes & colors) |
| `i` | **About this page**: a short overlay explaining what the screen is for (every TUI has one) |
| `?` | Help overlay |
| `Esc` | Close detail panel / overlay |
| `q` / `Ctrl+C` | Quit |

While a scan is running, the header shows real progress (`scanning 23/60` with
the last pending `service@region` tasks named) instead of a generic spinner,
and collection errors are surfaced inline: a red `⚠ n errors` badge in the
header plus per-service warning badges in the sidebar.

### Offline snapshot browsing (`snapshot-diff`)

`snapshot-diff` opens the same interactive TUI over **saved** inventory
snapshots — no AWS credentials, STS calls or region discovery needed. Snapshots
are just the JSON written by `summary -o json`.

```bash
# Browse a single saved snapshot offline
./bin/aws_explorer snapshot-diff --snapshot inventory.json

# Diff two snapshots and explore what was added / removed / modified
./bin/aws_explorer snapshot-diff --diff before.json,after.json
```

It needs one of `--snapshot` or `--diff` (they are mutually exclusive); to
explore **live** resources interactively, use `summary --tui` instead.
